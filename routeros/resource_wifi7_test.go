package routeros

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func wifi7SchemaShape(f *schema.Schema) map[string]interface{} {
	m := map[string]interface{}{"type": int(f.Type), "optional": f.Optional, "required": f.Required, "computed": f.Computed, "force_new": f.ForceNew, "sensitive": f.Sensitive, "default": f.Default}
	if e, ok := f.Elem.(*schema.Schema); ok {
		m["elem"] = wifi7SchemaShape(e)
	}
	return m
}

func TestWifi7ExistingSchemaCompatibility(t *testing.T) {
	raw, err := os.ReadFile("testdata/wifi_2_1_schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var baseline map[string]map[string]interface{}
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatal(err)
	}
	p := Provider()
	for name, fields := range baseline {
		r := p.ResourcesMap[name]
		if r == nil {
			t.Fatalf("existing resource removed: %s", name)
		}
		if r.SchemaVersion != 0 || len(r.StateUpgraders) != 0 {
			t.Errorf("%s unexpectedly requires state migration", name)
		}
		for field, old := range fields {
			f := r.Schema[field]
			if f == nil {
				t.Errorf("%s.%s removed", name, field)
				continue
			}
			shape := wifi7SchemaShape(f)
			if field == MetaTransformSet {
				// The existing helper builds this string from a Go map; ordering
				// is immaterial, but every old wire binding must remain identical.
				bindings := func(v interface{}) map[string]interface{} {
					result := map[string]interface{}{}
					for k, value := range loadTransformSet(v.(string), false) {
						result[k] = value
					}
					return result
				}
				old.(map[string]interface{})["default"] = bindings(old.(map[string]interface{})["default"])
				shape["default"] = bindings(shape["default"])
			}
			encoded, _ := json.Marshal(shape)
			var now interface{}
			_ = json.Unmarshal(encoded, &now)
			if !reflect.DeepEqual(old, now) {
				t.Errorf("existing schema changed: %s.%s\nold=%v\nnew=%v", name, field, old, now)
			}
		}
		for field, f := range r.Schema {
			if _, old := fields[field]; old || strings.HasPrefix(field, "___") {
				continue
			}
			if !f.Optional || f.Required || f.Default != nil || f.ForceNew {
				t.Errorf("new field changes legacy behavior: %s.%s", name, field)
			}
		}
	}
}

func TestWifi7FullDocumentedControls(t *testing.T) {
	for path, wires := range wifi7Fields {
		var r *schema.Resource
		for _, candidate := range Provider().ResourcesMap {
			if candidate.Schema[MetaResourcePath].Default == path {
				r = candidate
				break
			}
		}
		if r == nil {
			t.Errorf("missing menu %s", path)
			continue
		}
		for _, wire := range wires {
			if r.Schema[api725Name(wire)] == nil {
				t.Errorf("missing %s/%s", path, wire)
			}
		}
	}
	for _, name := range []string{"routeros_interface_wifi_network_radio", "routeros_interface_wifi_radio_settings"} {
		if Provider().ResourcesMap[name] == nil {
			t.Errorf("missing resource %s", name)
		}
	}
	if Provider().ResourcesMap["routeros_interface_wifi_network"].Schema["mlo"].Default != nil {
		t.Fatal("network MLO acquired a provider default")
	}
	if err := Provider().InternalValidate(); err != nil {
		t.Fatal(err)
	}
}

type wifi7Call struct {
	method crudMethod
	path   string
	body   MikrotikItem
}
type wifi7Recorder struct {
	calls []wifi7Call
	row   MikrotikItem
	err   error
}

func (c *wifi7Recorder) GetTransport() TransportType  { return TransportREST }
func (c *wifi7Recorder) GetExtraParams() *ExtraParams { return &ExtraParams{SuppressSysODelWarn: true} }
func (c *wifi7Recorder) SendRequest(method crudMethod, u *URL, body MikrotikItem, out interface{}) error {
	c.calls = append(c.calls, wifi7Call{method, u.Path, body})
	if c.err != nil {
		return c.err
	}
	switch result := out.(type) {
	case *MikrotikItem:
		for k, v := range c.row {
			(*result)[k] = v
		}
	case *[]MikrotikItem:
		if c.row != nil {
			*result = append(*result, c.row)
		}
	}
	return nil
}

func TestWifi7OmittedControlsNeverWritten(t *testing.T) {
	for _, r := range []*schema.Resource{ResourceWifi(), ResourceWifiChannel(), ResourceWifiCap(), ResourceWifiProvisioning(), ResourceWifiSecurity(), ResourceWifiConfiguration()} {
		path := r.Schema[MetaResourcePath].Default.(string)
		t.Run(path, func(t *testing.T) {
			d := resourceDataWithRawConfig(r, "*old", map[string]cty.Value{})
			for _, wire := range wifi7Fields[path] {
				name := api725Name(wire)
				f := r.Schema[name]
				var value interface{} = "read-back-value"
				if f.Type == schema.TypeBool {
					value = true
				}
				if f.Type == schema.TypeInt {
					value = 42
				}
				if f.Type == schema.TypeSet {
					value = []interface{}{"mlo"}
				}
				if err := d.Set(name, value); err != nil {
					t.Fatal(err)
				}
			}
			c := &wifi7Recorder{row: MikrotikItem{".id": "*old"}}
			if ds := r.UpdateContext(context.Background(), d, c); ds.HasError() {
				t.Fatal(ds)
			}
			for _, call := range c.calls {
				for _, wire := range wifi7Fields[path] {
					if _, present := call.body[wire]; present {
						t.Errorf("unconfigured %s written during legacy update", wire)
					}
				}
			}
		})
	}
}

func TestWifi7ExplicitFalseAndStringsWritten(t *testing.T) {
	r := ResourceWifiChannel()
	d := resourceDataWithRawConfig(r, "*old", map[string]cty.Value{"afc": cty.False, "preamble_puncturing": cty.StringVal("no")})
	c := &wifi7Recorder{row: MikrotikItem{".id": "*old"}}
	if ds := r.UpdateContext(context.Background(), d, c); ds.HasError() {
		t.Fatal(ds)
	}
	last := c.calls[len(c.calls)-1]
	if last.method != crudUpdate || last.body["afc"] != "no" || last.body["preamble-puncturing"] != "no" {
		t.Fatalf("explicit values lost: %v", c.calls)
	}
}

func TestWifi7OperationalState(t *testing.T) {
	old := RouterOSVersion
	RouterOSVersion = "7.25.1"
	t.Cleanup(func() { RouterOSVersion = old })
	r := DatasourceWifiRadios()
	d := r.TestResourceData()
	c := &wifi7Recorder{row: MikrotikItem{".id": "*R", "hw-caps": "mlo,beacon-protection", "ml-group": "AA:BB:CC:DD:EE:FF", "2g-channels": "2412,2437", "bands": "6ghz-be:20/40/80/160/320mhz"}}
	if ds := r.ReadContext(context.Background(), d, c); ds.HasError() {
		t.Fatal(ds)
	}
	if c.calls[0].method != crudRead || d.Get("data.0.ml_group") != "AA:BB:CC:DD:EE:FF" || d.Get("data.0.channels_2ghz").(*schema.Set).Len() != 2 {
		t.Fatalf("radio status not decoded: %v", d.Get("data"))
	}
	r = DatasourceWifiMonitor()
	d = r.TestResourceData()
	if err := d.Set("arguments", map[string]interface{}{"numbers": "*M", "once": "no"}); err != nil {
		t.Fatal(err)
	}
	c = &wifi7Recorder{row: MikrotikItem{"state": "running", "mld-link-id": "1", "channel": "6115/be/oooo", "afc-status": "authorized"}}
	if ds := r.ReadContext(context.Background(), d, c); ds.HasError() {
		t.Fatal(ds)
	}
	if c.calls[0].method != crudExecute || c.calls[0].body["once"] != "" || c.calls[0].body["numbers"] != "*M" || d.Get("data.0.mld_link_id") != "1" {
		t.Fatalf("monitor did not take finite MLD snapshot: %v", c.calls)
	}
}

func TestWifi7UpgradeNoOpPlans(t *testing.T) {
	for _, r := range []*schema.Resource{ResourceWifi(), ResourceWifiChannel(), ResourceWifiCap(), ResourceWifiProvisioning(), ResourceWifiSecurity(), ResourceWifiConfiguration(), ResourceWifiNetwork()} {
		path := r.Schema[MetaResourcePath].Default.(string)
		t.Run(path, func(t *testing.T) {
			values := map[string]cty.Value{}
			if r.Schema["name"] != nil {
				values["name"] = cty.StringVal("legacy-wifi")
			}
			config := api725ActionConfig(r, values)
			d := r.TestResourceData()
			d.SetId("*legacy")
			for name, f := range r.Schema {
				if f.Default != nil {
					if err := d.Set(name, f.Default); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, ok := values["name"]; ok {
				if err := d.Set("name", "legacy-wifi"); err != nil {
					t.Fatal(err)
				}
			}
			state := d.State()
			state.RawConfig = config.CtyValue
			// An old state has none of the newly introduced properties/metadata.
			for _, wire := range wifi7Fields[path] {
				name := api725Name(wire)
				for key := range state.Attributes {
					if key == name || strings.HasPrefix(key, name+".") {
						delete(state.Attributes, key)
					}
				}
			}
			delete(state.Attributes, MetaSetUnsetFields)
			diff, err := r.Diff(context.Background(), state, config, nil)
			if err != nil {
				t.Fatal(err)
			}
			if diff != nil && !diff.Empty() {
				t.Fatalf("v2.1.0 configuration gained changes: %#v", diff.Attributes)
			}
			// Refresh can discover device defaults or values configured outside
			// Terraform. Omitted additions must remain unmanaged after refresh.
			for _, wire := range wifi7Fields[path] {
				name := api725Name(wire)
				var value interface{} = "device-value"
				switch r.Schema[name].Type {
				case schema.TypeBool:
					value = true
				case schema.TypeInt:
					value = 12
				case schema.TypeSet:
					value = []interface{}{"mlo"}
				}
				if err := d.Set(name, value); err != nil {
					t.Fatal(err)
				}
			}
			state = d.State()
			state.RawConfig = config.CtyValue
			diff, err = r.Diff(context.Background(), state, config, nil)
			if err != nil {
				t.Fatal(err)
			}
			if diff != nil && !diff.Empty() {
				t.Fatalf("refresh introduced unmanaged changes: %#v", diff.Attributes)
			}
		})
	}
}

func TestWifi7PinnedWriteContracts(t *testing.T) {
	var inventory struct {
		Contracts map[string]struct {
			Arguments []string `json:"arguments"`
		} `json:"runtime_contracts"`
	}
	raw, err := os.ReadFile("../tools/schema_changes/routeros-7.25-wifi7.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &inventory); err != nil {
		t.Fatal(err)
	}
	for path, r := range map[string]*schema.Resource{
		"interface-wifi-network-radio-add":  ResourceWifiNetworkRadio(),
		"interface-wifi-network-radio-set":  ResourceWifiNetworkRadio(),
		"interface-wifi-radio-settings-set": ResourceWifiRadioSettings(),
	} {
		args := map[string]bool{}
		for _, wire := range inventory.Contracts[path].Arguments {
			args[wire] = true
		}
		for name, f := range r.Schema {
			if strings.HasPrefix(name, "___") || f.Computed {
				continue
			}
			wire := SnakeToKebab(name)
			if ts := r.Schema[MetaTransformSet]; ts != nil {
				if to, ok := loadTransformSet(ts.Default.(string), false)[name]; ok {
					wire = to
				}
			}
			if !args[wire] {
				t.Errorf("%s: invented writable property %s", path, wire)
			}
		}
		for wire := range args {
			if wire == "numbers" || wire == "copy-from" {
				continue
			}
			if !api725CoversWire(r.Schema, wire) {
				t.Errorf("%s: omitted documented argument %s", path, wire)
			}
		}
	}
}

func TestWifi7ReadClearsRemovedNewControls(t *testing.T) {
	r := ResourceWifiChannel()
	d := resourceDataWithRawConfig(r, "*old", map[string]cty.Value{"name": cty.StringVal("old-channel")})
	_ = d.Set("afc", true)
	_ = d.Set("preamble_puncturing", "yes")
	c := &wifi7Recorder{row: MikrotikItem{".id": "*old", "name": "old-channel"}}
	if ds := r.ReadContext(context.Background(), d, c); ds.HasError() {
		t.Fatal(ds)
	}
	if d.Get("afc") != false || d.Get("preamble_puncturing") != "" || d.Get("name") != "old-channel" {
		t.Fatal("new control remained stale or legacy field changed")
	}
	clear := resourceDataWithRawConfig(r, "*old", map[string]cty.Value{"preamble_puncturing": cty.StringVal("")})
	c.calls = nil
	if ds := r.UpdateContext(context.Background(), clear, c); ds.HasError() {
		t.Fatal(ds)
	}
	if _, ok := c.calls[len(c.calls)-1].body["!preamble-puncturing"]; !ok {
		t.Fatal("explicit string reset was not sent as unset")
	}
}

func TestWifi7RadioSettingsUnsupported(t *testing.T) {
	old := RouterOSVersion
	RouterOSVersion = "7.25.1"
	t.Cleanup(func() { RouterOSVersion = old })
	r := ResourceWifiRadioSettings()
	d := resourceDataWithRawConfig(r, "", map[string]cty.Value{"external_antenna": cty.False})
	c := &wifi7Recorder{err: errors.New("Not supported")}
	if ds := r.CreateContext(context.Background(), d, c); !ds.HasError() {
		t.Fatal("unsupported hardware was silently accepted")
	}
	if len(c.calls) != 1 || c.calls[0].method != crudPost || c.calls[0].path != "/interface/wifi/radio/settings/set" || c.calls[0].body["external-antenna"] != "no" {
		t.Fatalf("wrong singleton write: %v", c.calls)
	}
}
