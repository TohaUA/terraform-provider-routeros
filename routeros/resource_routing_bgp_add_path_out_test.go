package routeros

import (
	"context"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

// testCreatePayload serializes a create request for the resource the same way DefaultCreate does: the
// schema diff applies defaults, and the raw config is a fully typed object (null for every attribute not
// in raw) so both the diff suppressors and the serializer can tell "unset" from "set to the zero value".
func testCreatePayload(t *testing.T, res *schema.Resource, raw map[string]interface{}) MikrotikItem {
	t.Helper()

	attrs := map[string]cty.Value{}
	for name, ty := range res.CoreConfigSchema().ImpliedType().AttributeTypes() {
		if v, ok := raw[name]; ok && ty == cty.String {
			attrs[name] = cty.StringVal(v.(string))
			continue
		}
		attrs[name] = cty.NullVal(ty)
	}
	rawConfig := cty.ObjectVal(attrs)

	cfg := terraform.NewResourceConfigRaw(raw)
	cfg.CtyValue = rawConfig

	diff, err := res.Diff(context.Background(), nil, cfg, nil)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	diff.RawConfig = rawConfig

	d, err := schema.InternalMap(res.Schema).Data(nil, diff)
	if err != nil {
		t.Fatalf("data: %v", err)
	}

	item, _ := TerraformResourceDataToMikrotik(res.Schema, d)
	return item
}

// RouterOS 7.22 removed 'add-path-out' and rejects any request carrying it (upstream #959), so the
// attribute must reach the wire only when the user configures it explicitly.
func Test_routingBgpAddPathOutNotInjected(t *testing.T) {
	prev := RouterOSVersion
	RouterOSVersion = "7.24"
	t.Cleanup(func() { RouterOSVersion = prev })

	for name, res := range map[string]*schema.Resource{
		"routing_bgp_connection": ResourceRoutingBgpConnection(),
		"routing_bgp_template":   ResourceRoutingBgpTemplate(),
	} {
		t.Run(name, func(t *testing.T) {
			item := testCreatePayload(t, res, map[string]interface{}{"name": "test", "as": "65550"})
			if v, ok := item["add-path-out"]; ok {
				t.Errorf("unset add_path_out must not be serialized, got add-path-out=%q in %v", v, item)
			}

			item = testCreatePayload(t, res, map[string]interface{}{"name": "test", "as": "65550", "add_path_out": "all"})
			if item["add-path-out"] != "all" {
				t.Errorf("explicit add_path_out must be serialized, got %v", item)
			}

			// Existing states carry the injected default and updates PATCH the whole item from state,
			// so dropping the default is only half of the fix.
			if res.SchemaVersion != 1 || len(res.StateUpgraders) != 1 || res.StateUpgraders[0].Version != 0 {
				t.Errorf("expected SchemaVersion 1 with a v0 upgrader, got version %d and %d upgraders",
					res.SchemaVersion, len(res.StateUpgraders))
			}
		})
	}
}

// Existing states recorded the injected default; updates PATCH the whole item from state, so the
// upgrader has to clear it while leaving a user-chosen value alone.
func Test_stateMigrationClearInjectedDefault(t *testing.T) {
	upgrade := stateMigrationClearInjectedDefault("add_path_out", "none")

	for _, tc := range []struct {
		name     string
		in, want interface{}
	}{
		{"injected default is cleared", "none", ""},
		{"explicit value is kept", "all", "all"},
		{"missing key stays missing", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := map[string]interface{}{"name": "test"}
			if tc.in != nil {
				raw["add_path_out"] = tc.in
			}

			out, err := upgrade(context.Background(), raw, nil)
			if err != nil {
				t.Fatalf("upgrade: %v", err)
			}
			if got := out["add_path_out"]; got != tc.want {
				t.Fatalf("add_path_out: got %#v, want %#v", got, tc.want)
			}
			if out["name"] != "test" {
				t.Fatalf("other attributes must be untouched, got %v", out)
			}
		})
	}
}
