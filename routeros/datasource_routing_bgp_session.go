package routeros

import (
	"context"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// DatasourceRoutingBgpSession returns a read-only data source querying live BGP sessions at /routing/bgp/session.
func DatasourceRoutingBgpSession() *schema.Resource {
	resSchema := map[string]*schema.Schema{
		MetaResourcePath: PropResourcePath("/routing/bgp/session"),
		MetaId:           PropId(Id),

		KeyFilter: PropFilterRw,
		"sessions": {
			Type:        schema.TypeList,
			Computed:    true,
			Description: "Live BGP session observations at the time of the Terraform read.",
			Elem: &schema.Resource{
				Schema: map[string]*schema.Schema{
					"id":             {Type: schema.TypeString, Computed: true},
					"name":           {Type: schema.TypeString, Computed: true},
					"established":    {Type: schema.TypeBool, Computed: true},
					"disabled":       {Type: schema.TypeBool, Computed: true},
					"inactive":       {Type: schema.TypeBool, Computed: true},
					"dynamic":        {Type: schema.TypeBool, Computed: true},
					"remote_address": {Type: schema.TypeString, Computed: true},
					"remote_as":      {Type: schema.TypeInt, Computed: true},
					"remote_id":      {Type: schema.TypeString, Computed: true},
					"remote_hold_time": {
						Type:     schema.TypeString,
						Computed: true,
					},
					"local_address": {Type: schema.TypeString, Computed: true},
					"local_as":      {Type: schema.TypeInt, Computed: true},
					"local_id":      {Type: schema.TypeString, Computed: true},
					"local_role":    {Type: schema.TypeString, Computed: true},
					"uptime":        {Type: schema.TypeString, Computed: true},
					"prefix_count":  {Type: schema.TypeInt, Computed: true},
					"keepalive_time": {
						Type:     schema.TypeString,
						Computed: true,
					},
					"routing_table": {Type: schema.TypeString, Computed: true},
					"vrf":           {Type: schema.TypeString, Computed: true},
					"comment":       {Type: schema.TypeString, Computed: true},
				},
			},
		},
	}

	return &schema.Resource{
		ReadContext: datasourceRoutingBgpSessionRead,
		Schema:      resSchema,
	}
}

func datasourceRoutingBgpSessionRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	s := DatasourceRoutingBgpSession().Schema
	path := s[MetaResourcePath].Default.(string)

	res, err := ReadItemsFiltered(buildReadFilter(d.Get(KeyFilter).(map[string]interface{})), path, m.(Client))
	if err != nil {
		return diag.FromErr(err)
	}

	items, diags := decodeBgpSessionItems(*res)
	if diags.HasError() {
		return diags
	}

	d.SetId(UniqueId())
	if err := d.Set("sessions", items); err != nil {
		return append(diags, diag.FromErr(err)...)
	}
	return diags
}

func decodeBgpSessionItems(rows []MikrotikItem) ([]map[string]interface{}, diag.Diagnostics) {
	var diags diag.Diagnostics
	out := make([]map[string]interface{}, 0, len(rows))

	for _, row := range rows {
		norm := make(map[string]string, len(row))
		for k, v := range row {
			norm[strings.ToLower(k)] = v
		}

		remoteAS, err := parseOptionalInt(norm["remote.as"], "remote_as")
		if err != nil {
			diags = append(diags, diag.FromErr(err)...)
			continue
		}
		localAS, err := parseOptionalInt(norm["local.as"], "local_as")
		if err != nil {
			diags = append(diags, diag.FromErr(err)...)
			continue
		}
		prefixCount, err := parseOptionalInt(norm["prefix-count"], "prefix_count")
		if err != nil {
			diags = append(diags, diag.FromErr(err)...)
			continue
		}

		out = append(out, map[string]interface{}{
			"id":               norm[".id"],
			"name":             norm["name"],
			"established":      BoolFromMikrotikJSON(norm["established"]),
			"disabled":         BoolFromMikrotikJSON(norm["disabled"]),
			"inactive":         BoolFromMikrotikJSON(norm["inactive"]),
			"dynamic":          BoolFromMikrotikJSON(norm["dynamic"]),
			"remote_address":   norm["remote.address"],
			"remote_as":        remoteAS,
			"remote_id":        norm["remote.id"],
			"remote_hold_time": norm["remote.hold-time"],
			"local_address":    norm["local.address"],
			"local_as":         localAS,
			"local_id":         norm["local.id"],
			"local_role":       norm["local.role"],
			"uptime":           norm["uptime"],
			"prefix_count":     prefixCount,
			"keepalive_time":   norm["keepalive-time"],
			"routing_table":    norm["routing-table"],
			"vrf":              norm["vrf"],
			"comment":          norm["comment"],
		})
	}

	return out, diags
}

func parseOptionalInt(raw, field string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	return v, nil
}
