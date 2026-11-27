// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package routeros

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/terraform-routeros/terraform-provider-routeros/internal/macboot"
)

var discoverMNDPFunc = macboot.DiscoverMNDP

// DatasourceMNDPNeighbors returns a read-only data source that performs local
// layer-2 MikroTik Neighbor Discovery Protocol (MNDP) probing on a host
// network interface.
func DatasourceMNDPNeighbors() *schema.Resource {
	resSchema := map[string]*schema.Schema{
		"interface": {
			Type:        schema.TypeString,
			Required:    true,
			Description: "Local network interface name used to send MNDP probes and listen for layer-2 neighbor announcements.",
		},
		"timeout": {
			Type:        schema.TypeString,
			Optional:    true,
			Default:     "5s",
			Description: "Discovery timeout as a Go duration string (for example `5s`).",
			ValidateFunc: func(v interface{}, k string) (ws []string, errs []error) {
				d, err := time.ParseDuration(v.(string))
				if err != nil || d <= 0 {
					errs = append(errs, fmt.Errorf("%q must be a positive Go duration string", k))
				}
				return
			},
		},
		"neighbors": {
			Type:        schema.TypeList,
			Computed:    true,
			Description: "Discovered RouterOS neighbors deduplicated and sorted ascending by `mac_address`.",
			Elem: &schema.Resource{
				Schema: map[string]*schema.Schema{
					"mac_address": {
						Type:        schema.TypeString,
						Computed:    true,
						Description: "Uppercase colon-separated neighbor MAC address.",
					},
					"identity": {
						Type:        schema.TypeString,
						Computed:    true,
						Description: "RouterOS system identity reported by MNDP.",
					},
					"version": {
						Type:        schema.TypeString,
						Computed:    true,
						Description: "RouterOS version string reported by MNDP.",
					},
					"platform": {
						Type:        schema.TypeString,
						Computed:    true,
						Description: "Platform string reported by MNDP.",
					},
					"uptime_seconds": {
						Type:        schema.TypeInt,
						Computed:    true,
						Description: "Router uptime in seconds reported by MNDP.",
					},
					"software_id": {
						Type:        schema.TypeString,
						Computed:    true,
						Description: "RouterOS software ID reported by MNDP.",
					},
					"board": {
						Type:        schema.TypeString,
						Computed:    true,
						Description: "Board name reported by MNDP.",
					},
					"unpack": {
						Type:        schema.TypeInt,
						Computed:    true,
						Description: "MNDP unpack flag value.",
					},
					"ipv6_address": {
						Type:        schema.TypeString,
						Computed:    true,
						Description: "IPv6 address advertised by MNDP.",
					},
					"interface_name": {
						Type:        schema.TypeString,
						Computed:    true,
						Description: "Neighbor interface name advertised by MNDP.",
					},
					"ipv4_address": {
						Type:        schema.TypeString,
						Computed:    true,
						Description: "IPv4 address advertised by MNDP (empty when unaddressed).",
					},
					"source_ip": {
						Type:        schema.TypeString,
						Computed:    true,
						Description: "UDP source IP address observed for the MNDP packet.",
					},
				},
			},
		},
	}

	return &schema.Resource{
		Description: "Discovers MikroTik RouterOS devices on a local layer-2 network segment via MNDP (UDP/5678) without requiring an IP address on the router.",
		ReadContext: datasourceMNDPNeighborsRead,
		Schema:      resSchema,
	}
}

func datasourceMNDPNeighborsRead(ctx context.Context, d *schema.ResourceData, _ interface{}) diag.Diagnostics {
	iface := d.Get("interface").(string)
	timeout, err := time.ParseDuration(d.Get("timeout").(string))
	if err != nil || timeout <= 0 {
		return diag.Errorf("invalid timeout %q: must be a positive duration", d.Get("timeout").(string))
	}

	neighbors, err := discoverMNDPFunc(ctx, macboot.DiscoverOptions{
		Interface: iface,
		Timeout:   timeout,
	})
	if err != nil {
		return diag.FromErr(err)
	}

	items := make([]map[string]interface{}, 0, len(neighbors))
	for _, n := range neighbors {
		items = append(items, map[string]interface{}{
			"mac_address":    n.MACAddress,
			"identity":       n.Identity,
			"version":        n.Version,
			"platform":       n.Platform,
			"uptime_seconds": int(n.UptimeSeconds),
			"software_id":    n.SoftwareID,
			"board":          n.Board,
			"unpack":         int(n.Unpack),
			"ipv6_address":   n.IPv6Address,
			"interface_name": n.InterfaceName,
			"ipv4_address":   n.IPv4Address,
			"source_ip":      n.SourceIP,
		})
	}

	d.SetId("mndp:" + iface)
	if err := d.Set("neighbors", items); err != nil {
		return diag.FromErr(err)
	}
	return nil
}
