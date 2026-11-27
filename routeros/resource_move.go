package routeros

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

func ResourceMoveItems() *schema.Resource {
	resSchema := map[string]*schema.Schema{
		MetaResourcePath: PropResourcePath("/path"),
		MetaId:           PropId(Id),

		"resource_name": {
			Type:         schema.TypeString,
			Optional:     true,
			Description:  "Resource name in the notation ```routeros_ip_firewall_filter```.",
			ValidateFunc: validation.StringMatch(regexp.MustCompile(`^routeros(_\w+)+$`), ""),
			AtLeastOneOf: []string{"resource_name", "resource_path"},
		},
		"resource_path": {
			Type:         schema.TypeString,
			Optional:     true,
			Description:  "URL path of the resource in the notation ```/ip/firewall/filter```.",
			ValidateFunc: validation.StringMatch(regexp.MustCompile(`^(/\w+)+$`), ""),
			AtLeastOneOf: []string{"resource_name", "resource_path"},
		},
		"sequence": {
			Type:     schema.TypeList,
			Required: true,
			MinItems: 1,
			Description: "List identifiers in the required sequence. To locate the ```sequence``` before an " +
				"existing rule, set ```anchor_rule``` or add the target rule ```id``` as the last element of ```sequence```.",
			Elem: &schema.Schema{
				Type: schema.TypeString,
			},
		},
		"anchor_rule": {
			Type:     schema.TypeString,
			Optional: true,
			Description: "The rule before which the ```sequence``` of rules will be placed. If this field is not specified, " +
				"the rules will be placed before the last element of the ```sequence```.",
		},
	}

	resRead := func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
		path := moveItemsResourcePath(d)
		res, err := ReadItems(nil, path, m.(Client))
		if err != nil {
			ColorizedDebug(ctx, fmt.Sprintf(ErrorMsgGet, err))
			return diag.FromErr(err)
		}

		// Resource not found.
		if len(*res) == 0 {
			d.SetId("")
			return nil
		}

		pos := moveItemsPositionMap(*res)
		conf := make(map[string]struct{})
		for _, v := range d.Get("sequence").([]any) {
			conf[v.(string)] = struct{}{}
		}

		var list []string
		for _, r := range *res {
			if id, ok := r[".id"]; ok {
				if _, ok := conf[id]; ok {
					list = append(list, id)
				}
			}
		}

		if err := d.Set("sequence", list); err != nil {
			return diag.FromErr(err)
		}

		if anchor := strings.TrimSpace(d.Get("anchor_rule").(string)); anchor != "" {
			anchorPos, anchorExists := pos[anchor]
			inOrder := anchorExists
			if inOrder {
				for _, id := range list {
					if pos[id] >= anchorPos {
						inOrder = false
						break
					}
				}
			}
			if !inOrder {
				if err := d.Set("anchor_rule", ""); err != nil {
					return diag.FromErr(err)
				}
			}
		}
		return nil
	}

	resCreateUpdate := func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
		var list []string
		for _, v := range d.Get("sequence").([]any) {
			list = append(list, v.(string))
		}
		anchor := strings.TrimSpace(d.Get("anchor_rule").(string))
		if anchor == "" && len(list) < 2 {
			return diag.Errorf("sequence must contain at least 2 items when anchor_rule is not set")
		}
		if len(list) == 0 {
			return diag.Errorf("sequence must contain at least 1 item")
		}

		path := moveItemsResourcePath(d)
		c := m.(Client)

		res, err := ReadItems(nil, path, c)
		if err != nil {
			ColorizedDebug(ctx, fmt.Sprintf(ErrorMsgGet, err))
			return diag.FromErr(err)
		}
		pos := moveItemsPositionMap(*res)

		for _, id := range list {
			if _, ok := pos[id]; !ok {
				var diags diag.Diagnostics
				if d.Id() != "" {
					diags = resRead(ctx, d, m)
				}
				return append(diags, diag.Errorf("sequence item %q not found in %s", id, path)...)
			}
			if anchor != "" && id == anchor {
				return diag.Errorf("anchor_rule %q must not appear in sequence", anchor)
			}
		}
		if anchor != "" {
			if _, ok := pos[anchor]; !ok {
				var diags diag.Diagnostics
				if d.Id() != "" {
					diags = resRead(ctx, d, m)
				}
				return append(diags, diag.Errorf("anchor_rule %q not found in %s", anchor, path)...)
			}
		}

		var item MikrotikItem
		if anchor != "" {
			item = MikrotikItem{
				"numbers":     strings.Join(list, ","),
				"destination": anchor,
			}
		} else {
			item = MikrotikItem{
				"numbers":     strings.Join(list[:len(list)-1], ","),
				"destination": list[len(list)-1],
			}
		}

		movePath := path
		if c.GetTransport() == TransportREST {
			movePath = path + "/move"
		}
		if err := c.SendRequest(crudMove, &URL{Path: movePath}, item, nil); err != nil {
			ColorizedDebug(ctx, fmt.Sprintf(ErrorMsgPut, err))
			var diags diag.Diagnostics
			if d.Id() != "" {
				diags = resRead(ctx, d, m)
			}
			return append(diags, diag.FromErr(err)...)
		}

		res, err = ReadItems(nil, path, c)
		if err != nil {
			ColorizedDebug(ctx, fmt.Sprintf(ErrorMsgGet, err))
			var diags diag.Diagnostics
			if d.Id() != "" {
				diags = resRead(ctx, d, m)
			}
			return append(diags, diag.FromErr(err)...)
		}
		if !moveItemsInOrder(*res, list, anchor) {
			var diags diag.Diagnostics
			if d.Id() != "" {
				diags = resRead(ctx, d, m)
			}
			if anchor != "" {
				return append(diags, diag.Errorf("sequence %v was not ordered before anchor_rule %q in %s after move", list, anchor, path)...)
			}
			return append(diags, diag.Errorf("sequence %v was not in expected order in %s after move", list, path)...)
		}

		d.SetId(strings.ReplaceAll(strings.TrimLeft(path, "/"), "/", "."))

		return resRead(ctx, d, m)
	}

	return &schema.Resource{
		CreateContext: resCreateUpdate,
		ReadContext:   resRead,
		UpdateContext: resCreateUpdate,
		DeleteContext: DefaultSystemDelete(resSchema),

		Schema: resSchema,
	}
}

func moveItemsResourcePath(d *schema.ResourceData) string {
	if v, ok := d.GetOk("resource_path"); ok {
		return v.(string)
	}
	name := strings.TrimSpace(d.Get("resource_name").(string))
	if name == "" {
		return ""
	}
	trimmed := strings.TrimPrefix(name, "routeros_")
	return "/" + strings.TrimPrefix(strings.ReplaceAll(trimmed, "_", "/"), "/")
}

func moveItemsPositionMap(items []MikrotikItem) map[string]int {
	pos := make(map[string]int, len(items))
	for idx, r := range items {
		if id, ok := r[".id"]; ok && id != "" {
			pos[id] = idx
		}
	}
	return pos
}

func moveItemsInOrder(items []MikrotikItem, list []string, anchor string) bool {
	pos := moveItemsPositionMap(items)
	for i, id := range list {
		idx, ok := pos[id]
		if !ok {
			return false
		}
		if i > 0 && pos[list[i-1]] >= idx {
			return false
		}
	}
	if anchor != "" {
		anchorPos, ok := pos[anchor]
		if !ok {
			return false
		}
		for _, id := range list {
			if pos[id] >= anchorPos {
				return false
			}
		}
	}
	return true
}
