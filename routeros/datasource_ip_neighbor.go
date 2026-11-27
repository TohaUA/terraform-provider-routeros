package routeros

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// DatasourceIpNeighbor returns a read-only data source querying live RouterOS neighbor table entries at /ip/neighbor.
func DatasourceIpNeighbor() *schema.Resource {
	resSchema := map[string]*schema.Schema{
		MetaResourcePath: PropResourcePath("/ip/neighbor"),
		MetaId:           PropId(Id),

		KeyFilter: PropFilterRw,
		"neighbors": {
			Type:        schema.TypeList,
			Computed:    true,
			Description: "Live neighbor observations reported by `/ip/neighbor` at the time of the Terraform read.",
			Elem: &schema.Resource{
				Schema: map[string]*schema.Schema{
					"id":                  {Type: schema.TypeString, Computed: true},
					"interface":           {Type: schema.TypeString, Computed: true},
					"address":             {Type: schema.TypeString, Computed: true},
					"address4":            {Type: schema.TypeString, Computed: true},
					"address6":            {Type: schema.TypeString, Computed: true},
					"mac_address":         {Type: schema.TypeString, Computed: true},
					"identity":            {Type: schema.TypeString, Computed: true},
					"platform":            {Type: schema.TypeString, Computed: true},
					"version":             {Type: schema.TypeString, Computed: true},
					"board":               {Type: schema.TypeString, Computed: true},
					"interface_name":      {Type: schema.TypeString, Computed: true},
					"uptime":              {Type: schema.TypeString, Computed: true},
					"age":                 {Type: schema.TypeString, Computed: true},
					"software_id":         {Type: schema.TypeString, Computed: true},
					"discovered_by":       {Type: schema.TypeString, Computed: true},
					"system_description":  {Type: schema.TypeString, Computed: true},
					"system_caps":         {Type: schema.TypeString, Computed: true},
					"system_caps_enabled": {Type: schema.TypeString, Computed: true},
				},
			},
		},
	}

	return &schema.Resource{
		ReadContext: datasourceIpNeighborRead,
		Schema:      resSchema,
	}
}

func datasourceIpNeighborRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	path := DatasourceIpNeighbor().Schema[MetaResourcePath].Default.(string)

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

		items = append(items, map[string]interface{}{
			"id":                  norm[".id"],
			"interface":           norm["interface"],
			"address":             norm["address"],
			"address4":            norm["address4"],
			"address6":            norm["address6"],
			"mac_address":         norm["mac-address"],
			"identity":            norm["identity"],
			"platform":            norm["platform"],
			"version":             norm["version"],
			"board":               norm["board"],
			"interface_name":      norm["interface-name"],
			"uptime":              norm["uptime"],
			"age":                 norm["age"],
			"software_id":         norm["software-id"],
			"discovered_by":       norm["discovered-by"],
			"system_description":  norm["system-description"],
			"system_caps":         norm["system-caps"],
			"system_caps_enabled": norm["system-caps-enabled"],
		})
	}

	d.SetId(UniqueId())
	if err := d.Set("neighbors", items); err != nil {
		return diag.FromErr(err)
	}
	return nil
}
