package routeros

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

type deviceModeFeatureSpec struct {
	name        string
	description string
}

var deviceModeTypedFeatures = []deviceModeFeatureSpec{
	{name: "scheduler", description: "Whether `/system/scheduler` scripts are permitted. Null if unavailable on the target router, distinct from explicit `false`."},
	{name: "fetch", description: "Whether `/tool/fetch` transfers are permitted. Null if unavailable on the target router, distinct from explicit `false`."},
	{name: "socks", description: "Whether SOCKS proxy (`/ip/socks`) is permitted. Null if unavailable on the target router, distinct from explicit `false`."},
	{name: "pptp", description: "Whether PPTP client and server functionality is permitted. Null if unavailable on the target router, distinct from explicit `false`."},
	{name: "l2tp", description: "Whether L2TP client and server functionality is permitted. Null if unavailable on the target router, distinct from explicit `false`."},
	{name: "bandwidth_test", description: "Whether `/tool/bandwidth-test` and speed tests are permitted. Null if unavailable on the target router, distinct from explicit `false`."},
	{name: "traffic_gen", description: "Whether traffic generator and flood ping tools are permitted. Null if unavailable on the target router, distinct from explicit `false`."},
	{name: "sniffer", description: "Whether `/tool/sniffer` packet capture is permitted. Null if unavailable on the target router, distinct from explicit `false`."},
	{name: "ipsec", description: "Whether IPsec tunnels and policies are permitted. Null if unavailable on the target router, distinct from explicit `false`."},
	{name: "romon", description: "Whether RoMON discovery and forwarding are permitted. Null if unavailable on the target router, distinct from explicit `false`."},
	{name: "proxy", description: "Whether the HTTP web proxy (`/ip/proxy`) is permitted. Null if unavailable on the target router, distinct from explicit `false`."},
	{name: "hotspot", description: "Whether Hotspot services are permitted. Null if unavailable on the target router, distinct from explicit `false`."},
	{name: "smb", description: "Whether SMB file sharing is permitted. Null if unavailable on the target router, distinct from explicit `false`."},
	{name: "email", description: "Whether `/tool/e-mail` is permitted. Null if unavailable on the target router, distinct from explicit `false`."},
	{name: "zerotier", description: "Whether ZeroTier interfaces and controllers are permitted. Null if unavailable on the target router, distinct from explicit `false`."},
	{name: "container", description: "Whether OCI containers (`/container`) are permitted. Null if unavailable on the target router, distinct from explicit `false`."},
	{name: "install_any_version", description: "Whether installing RouterOS packages outside `allowed_versions` is permitted. Null if unavailable on the target router, distinct from explicit `false`."},
	{name: "partitions", description: "Whether disk partition management is permitted. Null if unavailable on the target router, distinct from explicit `false`."},
	{name: "routerboard", description: "Whether RouterBOARD firmware/boot settings changes are permitted. Null if unavailable on the target router, distinct from explicit `false`."},
	{name: "rose", description: "Whether ROSE storage features are permitted. Null if unavailable on the target router, distinct from explicit `false`."},
}

var deviceModeTypedFeatureSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(deviceModeTypedFeatures))
	for _, spec := range deviceModeTypedFeatures {
		set[spec.name] = struct{}{}
	}
	return set
}()

// DatasourceSystemDeviceMode returns a read-only data source exposing `/system/device-mode`.
func DatasourceSystemDeviceMode() *schema.Resource {
	resSchema := map[string]*schema.Schema{
		MetaResourcePath: PropResourcePath("/system/device-mode"),
		MetaId:           PropId(Id),

		"mode": {
			Type:        schema.TypeString,
			Computed:    true,
			Description: "Active device mode profile reported by RouterOS (for example `advanced`, `home`, `basic`, or `enterprise`). Null if unavailable.",
		},
		"allowed_versions": {
			Type:        schema.TypeString,
			Computed:    true,
			Description: "Comma-separated RouterOS version constraints allowed by the current device mode. Null if unavailable.",
		},
		"flagged": {
			Type:        schema.TypeBool,
			Computed:    true,
			Description: "Whether RouterOS startup checks flagged the device as potentially compromised. Null if unavailable on the target router, distinct from explicit `false`.",
		},
		"flagging_enabled": {
			Type:        schema.TypeBool,
			Computed:    true,
			Description: "Whether RouterOS device-mode flagging checks are enabled. Null if unavailable on the target router, distinct from explicit `false`.",
		},
		"attempt_count": {
			Type:        schema.TypeInt,
			Computed:    true,
			Description: "Reported device-mode change attempt counter. Null if unavailable.",
		},
		"features": {
			Type:        schema.TypeMap,
			Computed:    true,
			Elem:        &schema.Schema{Type: schema.TypeBool},
			Description: "Map of all boolean feature flags reported by `/system/device-mode` (snake_case keys). Unavailable flags are absent from the map, distinct from explicit `false`.",
		},
	}

	for _, spec := range deviceModeTypedFeatures {
		resSchema[spec.name] = &schema.Schema{
			Type:        schema.TypeBool,
			Computed:    true,
			Description: spec.description,
		}
	}

	return &schema.Resource{
		ReadContext: datasourceSystemDeviceModeRead,
		Schema:      resSchema,
	}
}

func parseDeviceModeBool(raw string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "yes":
		return true, true
	case "false", "no":
		return false, true
	default:
		return false, false
	}
}

func normalizeDeviceModeItem(item MikrotikItem) map[string]string {
	normalized := make(map[string]string, len(item))
	for k, v := range item {
		normalized[strings.ToLower(k)] = v
	}
	return normalized
}

func datasourceSystemDeviceModeRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var items []MikrotikItem
	if err := m.(Client).SendRequest(crudRead, &URL{Path: "/system/device-mode"}, nil, &items); err != nil {
		return diag.Errorf("failed to read RouterOS device mode (/system/device-mode): %v", err)
	}
	if len(items) == 0 || len(items[0]) == 0 {
		return diag.Errorf("failed to read RouterOS device mode (/system/device-mode): router returned no device-mode data")
	}
	if len(items) > 1 {
		return diag.Errorf("failed to read RouterOS device mode (/system/device-mode): expected 1 record, got %d", len(items))
	}

	d.SetId("/system/device-mode")
	item := normalizeDeviceModeItem(items[0])
	features := map[string]interface{}{}

	for rawKey, rawVal := range item {
		if rawKey == "" || strings.HasPrefix(rawKey, ".") {
			continue
		}

		trimmed := strings.TrimSpace(rawVal)
		snake := KebabToSnake(rawKey)

		switch snake {
		case "mode", "allowed_versions":
			// Leave absent/empty values unset so Terraform state keeps them null.
			if trimmed == "" {
				continue
			}
			if err := d.Set(snake, rawVal); err != nil {
				return diag.FromErr(fmt.Errorf("failed to set /system/device-mode attribute %q: %w", snake, err))
			}

		case "attempt_count":
			if trimmed == "" {
				continue
			}
			n, err := strconv.Atoi(trimmed)
			if err != nil {
				return diag.Errorf("failed to parse /system/device-mode field %q (%q): expected integer: %v", rawKey, rawVal, err)
			}
			if err := d.Set(snake, n); err != nil {
				return diag.FromErr(fmt.Errorf("failed to set /system/device-mode attribute %q: %w", snake, err))
			}

		case "flagged", "flagging_enabled":
			if trimmed == "" {
				continue
			}
			b, ok := parseDeviceModeBool(trimmed)
			if !ok {
				return diag.Errorf("failed to parse /system/device-mode field %q (%q): expected boolean", rawKey, rawVal)
			}
			if err := d.Set(snake, b); err != nil {
				return diag.FromErr(fmt.Errorf("failed to set /system/device-mode attribute %q: %w", snake, err))
			}

		default:
			if _, isTypedFeature := deviceModeTypedFeatureSet[snake]; isTypedFeature {
				if trimmed == "" {
					continue
				}
				b, ok := parseDeviceModeBool(trimmed)
				if !ok {
					return diag.Errorf("failed to parse /system/device-mode feature %q (%q): expected boolean", rawKey, rawVal)
				}
				if err := d.Set(snake, b); err != nil {
					return diag.FromErr(fmt.Errorf("failed to set /system/device-mode feature %q: %w", snake, err))
				}
				features[snake] = b
				continue
			}

			// Capture additional RouterOS 7.25+ boolean feature flags without failing on non-boolean metadata.
			if b, ok := parseDeviceModeBool(trimmed); ok {
				features[snake] = b
			}
		}
	}

	if err := d.Set("features", features); err != nil {
		return diag.FromErr(fmt.Errorf("failed to set /system/device-mode features map: %w", err))
	}

	return nil
}
