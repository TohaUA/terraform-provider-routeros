package routeros

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// DatasourceInterfaceBridgeHost returns a read-only data source querying live RouterOS bridge host entries at /interface/bridge/host.
func DatasourceInterfaceBridgeHost() *schema.Resource {
	resSchema := map[string]*schema.Schema{
		MetaResourcePath: PropResourcePath("/interface/bridge/host"),
		MetaId:           PropId(Id),

		KeyFilter: PropFilterRw,
		"hosts": {
			Type:        schema.TypeList,
			Computed:    true,
			Description: "Live bridge host table observations reported by `/interface/bridge/host` at the time of the Terraform read.",
			Elem: &schema.Resource{
				Schema: map[string]*schema.Schema{
					"id":          {Type: schema.TypeString, Computed: true},
					"bridge":      {Type: schema.TypeString, Computed: true},
					"interface":   {Type: schema.TypeString, Computed: true},
					"mac_address": {Type: schema.TypeString, Computed: true},
					"vid":         {Type: schema.TypeInt, Computed: true},
					"age":         {Type: schema.TypeString, Computed: true},
					"local":       {Type: schema.TypeBool, Computed: true},
					"dynamic":     {Type: schema.TypeBool, Computed: true},
					"external":    {Type: schema.TypeBool, Computed: true},
					"disabled":    {Type: schema.TypeBool, Computed: true},
					"invalid":     {Type: schema.TypeBool, Computed: true},
					"comment":     {Type: schema.TypeString, Computed: true},
				},
			},
		},
	}

	return &schema.Resource{
		ReadContext: datasourceInterfaceBridgeHostRead,
		Schema:      resSchema,
	}
}

func datasourceInterfaceBridgeHostRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	path := DatasourceInterfaceBridgeHost().Schema[MetaResourcePath].Default.(string)

	res, err := ReadItemsFiltered(buildReadFilter(d.Get(KeyFilter).(map[string]interface{})), path, m.(Client))
	if err != nil {
		return diag.FromErr(err)
	}

	items := make([]map[string]interface{}, 0, len(*res))
	for _, row := range *res {
		norm := make(map[string]string, len(row))
		for k, v := range row {
			norm[strings.ToLower(k)] = v
		}

		vid, err := parseOptionalInt(norm["vid"], "vid")
		if err != nil {
			return diag.FromErr(fmt.Errorf("failed to parse /interface/bridge/host field %q (%q): %w", "vid", norm["vid"], err))
		}

		iface := norm["interface"]
		if iface == "" {
			iface = norm["on-interface"]
		}

		items = append(items, map[string]interface{}{
			"id":          norm[".id"],
			"bridge":      norm["bridge"],
			"interface":   iface,
			"mac_address": norm["mac-address"],
			"vid":         vid,
			"age":         norm["age"],
			"local":       BoolFromMikrotikJSON(norm["local"]),
			"dynamic":     BoolFromMikrotikJSON(norm["dynamic"]),
			"external":    BoolFromMikrotikJSON(norm["external"]),
			"disabled":    BoolFromMikrotikJSON(norm["disabled"]),
			"invalid":     BoolFromMikrotikJSON(norm["invalid"]),
			"comment":     norm["comment"],
		})
	}

	d.SetId(UniqueId())
	if err := d.Set("hosts", items); err != nil {
		return diag.FromErr(err)
	}
	return nil
}
