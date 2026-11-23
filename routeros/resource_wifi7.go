package routeros

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

// These controls are part of the full 7.25 reference, even when absent from
// the 7.24 -> 7.25 property-name delta. Existing schemas remain authoritative.
var wifi7Fields = map[string][]string{
	"/interface/wifi":               {"mld-name", "mld-interface"},
	"/interface/wifi/channel":       {"preamble-puncturing", "afc"},
	"/interface/wifi/cap":           {"mld-static", "mld-datapath"},
	"/interface/wifi/provisioning":  {"multi-link-mode", "supported-hw-caps"},
	"/interface/wifi/security":      {"beacon-protection", "ft-r0-key-lifetime"},
	"/interface/wifi/configuration": {"installation", "max-clients", "station-roaming"},
}

func addWifi7Fields(s map[string]*schema.Schema) {
	path := s[MetaResourcePath].Default.(string)
	var unset []string
	for _, wire := range wifi7Fields[path] {
		for _, f := range routerOS725Reference[path] {
			if f.Wire != wire {
				continue
			}
			name := api725Name(wire)
			if s[name] != nil {
				break
			}
			f.Required = false
			s[name] = api725Property(f)
			if description, ok := wifi7Descriptions[wire]; ok {
				s[name].Description = description
			}
			if s[name].Type == schema.TypeString {
				unset = append(unset, name)
				if original := s[name].ValidateFunc; original != nil {
					s[name].ValidateFunc = validation.Any(validation.StringInSlice([]string{""}, false), original)
				}
			}
			if wire == "ft-r0-key-lifetime" {
				s[name].DiffSuppressFunc = TimeEqual
			}
			break
		}
	}
	if len(unset) != 0 {
		s[MetaSetUnsetFields] = PropSetUnsetFields(unset...)
	}
}

var wifi7Descriptions = map[string]string{
	"mld-name":            "Unique identifier designating this interface as a logical Multi-Link Device (MLD). Requires compatible WiFi 7 hardware for radio operation.",
	"mld-interface":       "Name of the logical MLD interface to affiliate with. Members must use the same SSID and belong to the same device. An explicit empty string unsets the affiliation.",
	"preamble-puncturing": "WiFi 7 DFS/radar preamble puncturing: yes or no. An explicit empty string restores the router default. Support depends on radio capabilities.",
	"afc":                 "Request Automatic Frequency Coordination where supported by the hardware and regulatory domain. Omitted values are left under router control.",
	"mld-static":          "Create static MLD interfaces on this CAP. Omitted values are not changed.",
	"mld-datapath":        "Datapath profile for MLD interfaces on the CAP. An explicit empty string restores the router default.",
	"multi-link-mode":     "Multi-link provisioning mode: auto, disabled, master, or all. An explicit empty string restores the router default.",
	"supported-hw-caps":   "Required radio hardware capabilities, for example mlo. An explicit empty set clears the capability match.",
	"beacon-protection":   "Beacon integrity protection: enabled or disabled. Requires the beacon-protection radio capability. An explicit empty string restores the driver default.",
	"ft-r0-key-lifetime":  "Lifetime of the FT R0 key, as a RouterOS time interval. An explicit empty string restores the router default.",
	"installation":        "Radio installation category. Driver-specific values include indoor, outdoor, and any. An explicit empty string restores the driver default.",
	"max-clients":         "Maximum associated clients. Supported limits depend on the driver and radio.",
	"station-roaming":     "Allow station interfaces to scan for and roam to a better AP. Omitted values are not changed.",
}

// Optional fields read back from a router must not become configuration on an
// unrelated update. Scope this rule to the added controls; preserve legacy
// serialization and inline maps exactly as they were in v2.1.0.
type wifi7InputClient struct {
	Client
	data     *schema.ResourceData
	wires    map[string]string
	defaults map[string]string
}

func (c wifi7InputClient) SendRequest(method crudMethod, url *URL, item MikrotikItem, result interface{}) error {
	if item != nil && (method == crudCreate || method == crudUpdate || method == crudPost) {
		copy := MikrotikItem{}
		for wire, value := range item {
			if name, ok := c.wires[strings.TrimPrefix(wire, "!")]; ok && c.data.GetRawConfig().GetAttr(name).IsNull() {
				continue
			}
			copy[wire] = value
		}
		item = copy
	}
	if err := c.Client.SendRequest(method, url, item, result); err != nil {
		return err
	}
	if method == crudRead {
		// RouterOS omits unset optional properties. Clear only the new fields
		// so an externally removed value cannot remain stale in state.
		fill := func(row MikrotikItem) {
			if row == nil {
				return
			}
			for wire := range c.wires {
				if value, ok := row[wire]; !ok || value == "" {
					row[wire] = c.defaults[wire]
				}
			}
		}
		switch rows := result.(type) {
		case *[]MikrotikItem:
			for _, row := range *rows {
				fill(row)
			}
		case *MikrotikItem:
			fill(*rows)
		}
	}
	return nil
}

func protectWifi7Inputs(r *schema.Resource, names []string) *schema.Resource {
	wires := map[string]string{}
	defaults := map[string]string{}
	transforms := map[string]string{}
	if ts := r.Schema[MetaTransformSet]; ts != nil {
		transforms = loadTransformSet(ts.Default.(string), false)
	}
	for _, name := range names {
		wire := SnakeToKebab(name)
		if target, ok := transforms[name]; ok {
			wire = SnakeToKebab(target)
		}
		wires[wire] = name
		switch r.Schema[name].Type {
		case schema.TypeInt, schema.TypeFloat:
			defaults[wire] = "0"
		case schema.TypeBool:
			defaults[wire] = "false"
		default:
			defaults[wire] = ""
		}
	}
	wrap := func(d *schema.ResourceData, m interface{}) Client {
		return wifi7InputClient{Client: m.(Client), data: d, wires: wires, defaults: defaults}
	}
	create, update := r.CreateContext, r.UpdateContext
	read := r.ReadContext
	r.ReadContext = func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
		return read(ctx, d, wrap(d, m))
	}
	r.CreateContext = func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
		return create(ctx, d, wrap(d, m))
	}
	r.UpdateContext = func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
		return update(ctx, d, wrap(d, m))
	}
	return r
}

func protectAddedWifi7Inputs(r *schema.Resource) *schema.Resource {
	var names []string
	for _, wire := range wifi7Fields[r.Schema[MetaResourcePath].Default.(string)] {
		names = append(names, api725Name(wire))
	}
	return protectWifi7Inputs(r, names)
}

// Existing validators are extended, never narrowed, for documented 7.25 values.
func wifi7ProvisioningActions() schema.SchemaValidateFunc {
	return validation.StringInSlice([]string{"create-disabled", "create-enabled", "create-dynamic-enabled", "none", "use-network-config"}, false)
}
