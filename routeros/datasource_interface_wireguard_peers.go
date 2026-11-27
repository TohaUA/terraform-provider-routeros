package routeros

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// wireguardPeerProplist omits private-key and preshared-key so secret material is
// neither requested on the wire nor exposed in Terraform state.
var wireguardPeerProplist = []string{
	".id",
	"interface",
	"name",
	"public-key",
	"allowed-address",
	"endpoint-address",
	"endpoint-port",
	"current-endpoint-address",
	"current-endpoint-port",
	"last-handshake",
	"persistent-keepalive",
	"rx",
	"tx",
	"disabled",
	"dynamic",
	"is-responder",
	"comment",
}

// DatasourceInterfaceWireguardPeers returns a read-only data source querying live WireGuard peer status.
func DatasourceInterfaceWireguardPeers() *schema.Resource {
	resSchema := map[string]*schema.Schema{
		MetaResourcePath: PropResourcePath("/interface/wireguard/peers"),
		MetaId:           PropId(Id),

		KeyFilter: PropFilterRw,
		"peers": {
			Type:        schema.TypeList,
			Computed:    true,
			Description: "Live WireGuard peer status observations at the time of the Terraform read. Private keys and preshared keys are omitted from both the router projection and Terraform schema.",
			Elem: &schema.Resource{
				Schema: map[string]*schema.Schema{
					"id":           {Type: schema.TypeString, Computed: true},
					"interface":    {Type: schema.TypeString, Computed: true},
					"name":         {Type: schema.TypeString, Computed: true},
					"public_key":   {Type: schema.TypeString, Computed: true},
					"comment":      {Type: schema.TypeString, Computed: true},
					"disabled":     {Type: schema.TypeBool, Computed: true},
					"dynamic":      {Type: schema.TypeBool, Computed: true},
					"is_responder": {Type: schema.TypeBool, Computed: true},
					"allowed_address": {
						Type:     schema.TypeList,
						Computed: true,
						Elem:     &schema.Schema{Type: schema.TypeString},
					},
					"endpoint_address":         {Type: schema.TypeString, Computed: true},
					"endpoint_port":            {Type: schema.TypeString, Computed: true},
					"current_endpoint_address": {Type: schema.TypeString, Computed: true},
					"current_endpoint_port":    {Type: schema.TypeInt, Computed: true},
					"last_handshake": {
						Type:        schema.TypeString,
						Computed:    true,
						Description: "Time elapsed since the last successful handshake as reported by RouterOS (empty string \"\" if the peer has never completed a handshake).",
					},
					"persistent_keepalive": {Type: schema.TypeString, Computed: true},
					"rx":                   {Type: schema.TypeString, Computed: true},
					"tx":                   {Type: schema.TypeString, Computed: true},
				},
			},
		},
	}

	return &schema.Resource{
		ReadContext: datasourceInterfaceWireguardPeersRead,
		Schema:      resSchema,
	}
}

func datasourceInterfaceWireguardPeersRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	s := DatasourceInterfaceWireguardPeers().Schema
	path := s[MetaResourcePath].Default.(string)

	res, err := ReadItemsFilteredWithProplist(
		buildReadFilter(d.Get(KeyFilter).(map[string]interface{})),
		wireguardPeerProplist,
		path,
		m.(Client),
	)
	if err != nil {
		return diag.FromErr(err)
	}

	items, diags := decodeWireguardPeerItems(*res)
	if diags.HasError() {
		return diags
	}

	d.SetId(UniqueId())
	if err := d.Set("peers", items); err != nil {
		return append(diags, diag.FromErr(err)...)
	}
	return diags
}

func decodeWireguardPeerItems(rows []MikrotikItem) ([]map[string]interface{}, diag.Diagnostics) {
	var diags diag.Diagnostics
	out := make([]map[string]interface{}, 0, len(rows))

	for _, row := range rows {
		norm := make(map[string]string, len(row))
		for k, v := range row {
			norm[strings.ToLower(k)] = v
		}

		currentEndpointPort, err := parseOptionalInt(norm["current-endpoint-port"], "current_endpoint_port")
		if err != nil {
			diags = append(diags, diag.FromErr(err)...)
			continue
		}

		var allowed []interface{}
		if raw := strings.TrimSpace(norm["allowed-address"]); raw != "" {
			for _, part := range strings.Split(raw, ",") {
				if p := strings.TrimSpace(part); p != "" {
					allowed = append(allowed, p)
				}
			}
		}

		out = append(out, map[string]interface{}{
			"id":                       norm[".id"],
			"interface":                norm["interface"],
			"name":                     norm["name"],
			"public_key":               norm["public-key"],
			"allowed_address":          allowed,
			"endpoint_address":         norm["endpoint-address"],
			"endpoint_port":            norm["endpoint-port"],
			"current_endpoint_address": norm["current-endpoint-address"],
			"current_endpoint_port":    currentEndpointPort,
			"last_handshake":           norm["last-handshake"],
			"persistent_keepalive":     norm["persistent-keepalive"],
			"rx":                       norm["rx"],
			"tx":                       norm["tx"],
			"disabled":                 BoolFromMikrotikJSON(norm["disabled"]),
			"dynamic":                  BoolFromMikrotikJSON(norm["dynamic"]),
			"is_responder":             BoolFromMikrotikJSON(norm["is-responder"]),
			"comment":                  norm["comment"],
		})
	}

	return out, diags
}
