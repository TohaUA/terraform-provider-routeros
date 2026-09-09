package routeros

import (
	"reflect"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

// testResourceDataWithConfig builds ResourceData for the schema with the given attributes
// set, and a raw config that carries the same attributes (every other attribute null), so
// TerraformResourceDataToMikrotik can inspect the config the way it does under Terraform.
func testResourceDataWithConfig(t *testing.T, s map[string]*schema.Schema, id string, attrs map[string]string) *schema.ResourceData {
	t.Helper()

	ty := schema.InternalMap(s).CoreConfigSchema().ImpliedType()
	vals := map[string]cty.Value{}
	for name, at := range ty.AttributeTypes() {
		vals[name] = cty.NullVal(at)
	}
	for name, v := range attrs {
		vals[name] = cty.StringVal(v)
	}

	d := (&schema.Resource{Schema: s}).Data(&terraform.InstanceState{ID: id, RawConfig: cty.ObjectVal(vals)})
	for name, v := range attrs {
		if err := d.Set(name, v); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

// RouterOS 7.24 renamed `address` to `available-from` in /ip/service. The rename is
// compensated through the drift table, so the schema keeps the single `address` attribute
// and the serializer translates it in both directions once the router reports >= 7.24.
func Test_driftIpServiceAvailableFrom(t *testing.T) {
	originalVersion := RouterOSVersion
	defer func() { RouterOSVersion = originalVersion }()

	t.Run("drift map", func(t *testing.T) {
		tests := []struct {
			ros     string
			reverse bool
			want    map[string]string
		}{
			{"7.23", false, map[string]string{}},
			{"7.23", true, map[string]string{}},
			{"7.24", false, map[string]string{"address": "available-from"}},
			{"7.24", true, map[string]string{"available-from": "address"}},
			{"7.24.1", true, map[string]string{"available-from": "address"}},
		}
		for _, tt := range tests {
			got := driftAttributeSlice.GetDriftMap(tt.ros, "/ip/service", tt.reverse)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetDriftMap(%q, /ip/service, reverse=%v) = %#v, want %#v", tt.ros, tt.reverse, got, tt.want)
			}
		}
	})

	t.Run("resource read maps available-from to address", func(t *testing.T) {
		RouterOSVersion = "7.24"
		s := ResourceIpService().Schema
		d := (&schema.Resource{Schema: s}).Data(&terraform.InstanceState{ID: "telnet"})

		item := MikrotikItem{".id": "*0", "name": "telnet", "port": "23", "available-from": "10.0.0.0/8"}
		for _, diag := range MikrotikResourceDataToTerraform(item, s, d) {
			t.Errorf("unexpected diagnostic: %s: %s", diag.Summary, diag.Detail)
		}
		if got := d.Get("address").(string); got != "10.0.0.0/8" {
			t.Errorf("address = %q, want %q", got, "10.0.0.0/8")
		}
	})

	t.Run("resource write sends available-from", func(t *testing.T) {
		RouterOSVersion = "7.24"
		s := ResourceIpService().Schema
		d := testResourceDataWithConfig(t, s, "telnet", map[string]string{"address": "10.0.0.0/8"})

		item, _ := TerraformResourceDataToMikrotik(s, d)
		if got := item["available-from"]; got != "10.0.0.0/8" {
			t.Errorf("available-from = %q, want %q (item: %#v)", got, "10.0.0.0/8", item)
		}
		if _, ok := item["address"]; ok {
			t.Errorf("address must not be sent alongside available-from on 7.24 (item: %#v)", item)
		}
	})

	t.Run("resource write keeps address before 7.24", func(t *testing.T) {
		RouterOSVersion = "7.23"
		s := ResourceIpService().Schema
		d := testResourceDataWithConfig(t, s, "telnet", map[string]string{"address": "10.0.0.0/8"})

		item, _ := TerraformResourceDataToMikrotik(s, d)
		if got := item["address"]; got != "10.0.0.0/8" {
			t.Errorf("address = %q, want %q (item: %#v)", got, "10.0.0.0/8", item)
		}
		if _, ok := item["available-from"]; ok {
			t.Errorf("available-from must not be sent before 7.24 (item: %#v)", item)
		}
	})

	t.Run("datasource maps available-from to address without warnings", func(t *testing.T) {
		RouterOSVersion = "7.24"
		s := DatasourceIPServices().Schema
		d := (&schema.Resource{Schema: s}).Data(&terraform.InstanceState{ID: "services"})

		items := []MikrotikItem{{".id": "*0", "name": "telnet", "port": "23", "available-from": "10.0.0.0/8"}}
		diags := MikrotikResourceDataToTerraformDatasource(&items, "services", s, d)
		for _, diag := range diags {
			t.Errorf("unexpected diagnostic: %s: %s", diag.Summary, diag.Detail)
		}

		services := d.Get("services").([]interface{})
		if len(services) != 1 {
			t.Fatalf("services = %#v, want one entry", services)
		}
		if got := services[0].(map[string]interface{})["address"]; got != "10.0.0.0/8" {
			t.Errorf("services[0].address = %#v, want %q", got, "10.0.0.0/8")
		}
	})
}
