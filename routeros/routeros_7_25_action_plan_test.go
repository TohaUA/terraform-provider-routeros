package routeros

import (
	"context"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

// Populate the protocol's typed raw config as well as its legacy SDK view.
// Calling UpdateContext directly cannot catch a change suppressed by planning.
func api725ActionConfig(r *schema.Resource, values map[string]cty.Value) *terraform.ResourceConfig {
	raw := map[string]cty.Value{}
	legacy := map[string]interface{}{}
	for name, typ := range r.CoreConfigSchema().ImpliedType().AttributeTypes() {
		raw[name] = cty.NullVal(typ)
	}
	for name, value := range values {
		raw[name] = value
		switch value.Type() {
		case cty.String:
			legacy[name] = value.AsString()
		case cty.Bool:
			legacy[name] = value.True()
		case cty.Number:
			n, _ := value.AsBigFloat().Int64()
			legacy[name] = int(n)
		default:
			m := map[string]interface{}{}
			for k, v := range value.AsValueMap() {
				m[k] = v.AsString()
			}
			legacy[name] = m
		}
	}
	c := terraform.NewResourceConfigRaw(legacy)
	c.CtyValue = cty.ObjectVal(raw)
	return c
}

func TestAPI725ActionArgumentPlanning(t *testing.T) {
	for _, tc := range []struct {
		name, field, wire, old, want string
		next                         cty.Value
	}{
		{"remove_bool", "known_hosts_ignore", "known-hosts-ignore", "true", "", cty.NilVal},
		{"remove_string", "password", "password", "previous-secret", "", cty.NilVal},
		{"remove_number", "port", "port", "2222", "", cty.NilVal},
		{"explicit_false", "known_hosts_ignore", "known-hosts-ignore", "true", "no", cty.False},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := ResourceSystemSSHExec()
			values := map[string]cty.Value{
				"address": cty.StringVal("192.0.2.10"),
				"user":    cty.StringVal("automation"),
				"command": cty.StringVal("/system/identity/print"),
			}
			if tc.next != cty.NilVal {
				values[tc.field] = tc.next
			}
			config := api725ActionConfig(r, values)
			state := &terraform.InstanceState{ID: "test-action", Attributes: map[string]string{
				"id": "test-action", "address": "192.0.2.10", "user": "automation",
				"command": "/system/identity/print", tc.field: tc.old,
			}, RawConfig: config.CtyValue}
			diff, err := r.Diff(context.Background(), state, config, nil)
			if err != nil {
				t.Fatal(err)
			}
			if diff == nil || diff.Attributes[tc.field] == nil {
				t.Fatal("argument change was suppressed by Terraform planning")
			}
			client := &api725Recorder{}
			_, diags := r.Apply(context.Background(), state, diff, client)
			if diags.HasError() {
				t.Fatal(diags)
			}
			if client.calls != 1 || client.method != crudExecute {
				t.Fatal("planned change did not execute exactly once")
			}
			if tc.next == cty.NilVal {
				if value, present := client.body[tc.wire]; present {
					t.Fatalf("removed argument reached RouterOS: %s=%q", tc.wire, value)
				}
			} else if got := client.body[tc.wire]; got != tc.want {
				t.Fatalf("explicit argument = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAPI725ActionTriggerPlanning(t *testing.T) {
	r := ResourceSystemSSHExec()
	for _, next := range []string{"first", "second"} {
		config := api725ActionConfig(r, map[string]cty.Value{
			"address": cty.StringVal("192.0.2.10"), "user": cty.StringVal("automation"),
			"command":  cty.StringVal("/system/identity/print"),
			"triggers": cty.MapVal(map[string]cty.Value{"request": cty.StringVal(next)}),
		})
		state := &terraform.InstanceState{ID: "test-action", Attributes: map[string]string{
			"id": "test-action", "address": "192.0.2.10", "user": "automation",
			"command": "/system/identity/print", "triggers.%": "1", "triggers.request": "first",
		}, RawConfig: config.CtyValue}
		diff, err := r.Diff(context.Background(), state, config, nil)
		if err != nil {
			t.Fatal(err)
		}
		if next == "first" {
			if diff != nil && !diff.Empty() {
				t.Fatal("unchanged configuration planned another execution")
			}
			continue
		}
		if diff == nil || diff.Attributes["triggers.request"] == nil {
			t.Fatal("trigger change did not plan another execution")
		}
		client := &api725Recorder{}
		if _, diags := r.Apply(context.Background(), state, diff, client); diags.HasError() {
			t.Fatal(diags)
		}
		if client.calls != 1 {
			t.Fatal("trigger change did not execute exactly once")
		}
	}
}
