package routeros

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

/*
  {
    ".id": "*1",
    "DHCP": "false",
    "address": "192.0.2.50",
    "complete": "true",
    "disabled": "false",
    "dynamic": "false",
    "interface": "ether2",
    "invalid": "false",
    "mac-address": "00:11:22:33:44:55",
    "published": "false",
    "status": "permanent"
  }
*/

// ResourceIpArp manages static, non-DHCP entries in /ip/arp.
func ResourceIpArp() *schema.Resource {
	resSchema := map[string]*schema.Schema{
		MetaResourcePath: PropResourcePath("/ip/arp"),
		MetaId:           PropId(Id),
		MetaTransformSet: PropTransformSet("dhcp: DHCP"),

		"address": {
			Type:         schema.TypeString,
			Required:     true,
			ValidateFunc: validation.IsIPv4Address,
			Description:  "The IPv4 address to be mapped.",
		},
		KeyComment: PropCommentRw,
		"complete": {
			Type:        schema.TypeBool,
			Computed:    true,
			Description: "Whether the ARP entry is complete.",
		},
		"dhcp": {
			Type:        schema.TypeBool,
			Computed:    true,
			Description: "Whether the ARP entry was created by DHCP.",
		},
		KeyDisabled:  PropDisabledRw,
		KeyDynamic:   PropDynamicRo,
		KeyInterface: PropInterfaceRw,
		KeyInvalid:   PropInvalidRo,
		"mac_address": {
			Type:             schema.TypeString,
			Optional:         true,
			ValidateFunc:     validation.IsMACAddress,
			DiffSuppressFunc: MacAddressEqual,
			Description:      "MAC address to map to the IPv4 address.",
		},
		"published": {
			Type:        schema.TypeBool,
			Optional:    true,
			Description: "Static proxy-ARP entry for an individual IP address. When an ARP query is received for the specific IP address, the device will respond with its own MAC address.",
		},
		"status": {
			Type:        schema.TypeString,
			Computed:    true,
			Description: "Current ARP entry status reported by RouterOS (for example `permanent`, `reachable`, `stale`, or `failed`).",
		},
	}

	readStatic := func(ctx context.Context, d *schema.ResourceData, m interface{}) (MikrotikItem, diag.Diagnostics) {
		metadata := GetMetadata(resSchema)
		res, err := ReadItems(&ItemId{metadata.IdType, d.Id()}, metadata.Path, m.(Client))
		if err != nil {
			return nil, diag.FromErr(err)
		}
		if len(*res) == 0 {
			return nil, nil
		}
		item := (*res)[0]
		if err := validateStaticArpRow(item); err != nil {
			return nil, diag.FromErr(err)
		}
		return item, nil
	}

	return &schema.Resource{
		CreateContext: DefaultCreate(resSchema),
		ReadContext: func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			item, diags := readStatic(ctx, d, m)
			if diags.HasError() {
				ColorizedDebug(ctx, fmt.Sprintf(ErrorMsgGet, diags))
				return diags
			}
			if item == nil {
				d.SetId("")
				return nil
			}
			d.SetId(item.GetID(GetMetadata(resSchema).IdType))
			return MikrotikResourceDataToTerraform(item, resSchema, d)
		},
		UpdateContext: func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			if _, diags := readStatic(ctx, d, m); diags.HasError() {
				return diags
			}
			return ResourceUpdate(ctx, resSchema, d, m)
		},
		DeleteContext: func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			if _, diags := readStatic(ctx, d, m); diags.HasError() {
				return diags
			}
			return ResourceDelete(ctx, resSchema, d, m)
		},
		Importer: &schema.ResourceImporter{
			StateContext: importIpArpState(resSchema),
		},

		Schema: resSchema,
	}
}

func validateStaticArpRow(item MikrotikItem) error {
	id := item.GetID(Id)
	addr := item["address"]
	iface := item["interface"]

	if BoolFromMikrotikJSON(item["dynamic"]) {
		return fmt.Errorf("refusing to manage dynamic /ip/arp entry %s (address=%s, interface=%s): routeros_ip_arp only manages static, non-DHCP entries", id, addr, iface)
	}
	if BoolFromMikrotikJSON(item["DHCP"]) || BoolFromMikrotikJSON(item["dhcp"]) {
		return fmt.Errorf("refusing to manage DHCP-created /ip/arp entry %s (address=%s, interface=%s): routeros_ip_arp only manages static, non-DHCP entries", id, addr, iface)
	}
	return nil
}

func parseIpArpImportFilter(raw string) (byID string, filter []string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil, fmt.Errorf("empty import ID: specify RouterOS .id, '<address>,<interface>', or 'address=...,interface=...'")
	}
	if strings.HasPrefix(raw, "*") {
		return raw, nil, nil
	}
	if strings.Contains(raw, ",") {
		parts := strings.Split(raw, ",")
		if len(parts) == 2 && !strings.Contains(raw, "=") {
			addr := strings.TrimSpace(parts[0])
			iface := strings.TrimSpace(parts[1])
			if addr == "" || iface == "" {
				return "", nil, fmt.Errorf("invalid import ID %q: expected '<address>,<interface>'", raw)
			}
			return "", []string{"address=" + addr, "interface=" + iface}, nil
		}
		for _, part := range parts {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) != 2 {
				return "", nil, fmt.Errorf("invalid import filter segment %q in %q: expected key=value", part, raw)
			}
			k := strings.TrimSpace(kv[0])
			v := strings.TrimSpace(kv[1])
			if k == "" || v == "" {
				return "", nil, fmt.Errorf("invalid import filter segment %q in %q: key and value must be non-empty", part, raw)
			}
			filter = append(filter, SnakeToKebab(k)+"="+v)
		}
		return "", filter, nil
	}
	if strings.Contains(raw, "=") {
		kv := strings.SplitN(raw, "=", 2)
		k := strings.TrimSpace(kv[0])
		v := strings.TrimSpace(kv[1])
		if k == "" || v == "" {
			return "", nil, fmt.Errorf("invalid import ID %q: key and value must be non-empty", raw)
		}
		return "", []string{SnakeToKebab(k) + "=" + v}, nil
	}
	return "", []string{"address=" + raw}, nil
}

func importIpArpState(s map[string]*schema.Schema) schema.StateContextFunc {
	return func(ctx context.Context, d *schema.ResourceData, m interface{}) ([]*schema.ResourceData, error) {
		byID, filter, err := parseIpArpImportFilter(d.Id())
		if err != nil {
			return nil, err
		}

		path := s[MetaResourcePath].Default.(string)
		client := m.(Client)

		var res *[]MikrotikItem
		if byID != "" {
			res, err = ReadItems(&ItemId{Id, byID}, path, client)
		} else {
			res, err = ReadItemsFiltered(filter, path, client)
		}
		if err != nil {
			return nil, err
		}

		switch len(*res) {
		case 0:
			return nil, fmt.Errorf("no /ip/arp entry found for %q", d.Id())
		case 1:
			item := (*res)[0]
			if err := validateStaticArpRow(item); err != nil {
				return nil, err
			}
			retId, ok := item[Id.String()]
			if !ok || retId == "" {
				return nil, fmt.Errorf("attribute %v not found in /ip/arp response", Id.String())
			}
			d.SetId(retId)
			return []*schema.ResourceData{d}, nil
		default:
			return nil, fmt.Errorf("ambiguous import ID %q: matched %d /ip/arp entries; specify '<address>,<interface>' or the RouterOS .id", d.Id(), len(*res))
		}
	}
}
