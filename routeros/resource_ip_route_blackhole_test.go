package routeros

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// RouterOS 7.25.2 reports a blackhole route with an empty presence marker.
// Refresh must read that as true and must also clear stale true state when
// the flag disappears, rather than hiding a route changed outside Terraform.
func TestRouteBlackholeRefresh(t *testing.T) {
	old := RouterOSVersion
	RouterOSVersion = "7.25.2"
	t.Cleanup(func() { RouterOSVersion = old })
	for _, route := range []struct {
		name string
		res  func() *schema.Resource
	}{
		{"ipv4", ResourceIPRoute},
		{"ipv6", ResourceIPv6Route},
	} {
		for _, tc := range []struct {
			name string
			item MikrotikItem
			want bool
		}{
			{"empty marker", MikrotikItem{"blackhole": ""}, true},
			{"explicit true", MikrotikItem{"blackhole": "true"}, true},
			{"explicit yes", MikrotikItem{"blackhole": "yes"}, true},
			{"explicit false", MikrotikItem{"blackhole": "false"}, false},
			{"explicit no", MikrotikItem{"blackhole": "no"}, false},
			{"absent flag", MikrotikItem{}, false},
		} {
			t.Run(route.name+"/"+tc.name, func(t *testing.T) {
				res := route.res()
				d := resourceDataWithRawConfig(res, "*1", map[string]cty.Value{
					"gateway":   cty.StringVal(""),
					"blackhole": cty.BoolVal(true),
				})
				if err := d.Set("blackhole", !tc.want); err != nil {
					t.Fatal(err)
				}
				item := MikrotikItem{".id": "*1", "gateway": "", "disabled": ""}
				for key, value := range tc.item {
					item[key] = value
				}
				client := &recordingClient{readItem: item}
				if diags := res.ReadContext(context.Background(), d, client); diags.HasError() {
					t.Fatalf("refresh: %v", diags)
				}
				if got := d.Get("blackhole"); got != tc.want {
					t.Errorf("blackhole = %v, want %v", got, tc.want)
				}
				if got := d.Get("disabled"); got != false {
					t.Errorf("unrelated empty boolean changed: disabled = %v", got)
				}
				if len(client.sent) != 0 {
					t.Fatalf("refresh sent a mutation: %v", client.sent)
				}
				// A second identical read must retain the same value.
				if diags := res.ReadContext(context.Background(), d, client); diags.HasError() || d.Get("blackhole") != tc.want {
					t.Fatalf("second refresh changed the result: %v", diags)
				}
			})
		}
	}
}

func TestRouteBlackholeNormalizationDoesNotChangeResponse(t *testing.T) {
	res := ResourceIPRoute()
	item := MikrotikItem{"blackhole": ""}
	if diags := MikrotikResourceDataToTerraform(item, res.Schema, res.TestResourceData()); diags.HasError() {
		t.Fatal(diags)
	}
	if !reflect.DeepEqual(item, MikrotikItem{"blackhole": ""}) {
		t.Fatalf("read changed the transport response: %v", item)
	}
}

func TestRouteBlackholeWriteSemanticsUnchanged(t *testing.T) {
	for _, res := range []*schema.Resource{ResourceIPRoute(), ResourceIPv6Route()} {
		for _, tc := range []struct {
			name    string
			value   cty.Value
			want    string
			present bool
		}{
			{"true", cty.BoolVal(true), "yes", true},
			{"false", cty.BoolVal(false), "no", true},
			{"omitted", cty.NullVal(cty.Bool), "", false},
		} {
			t.Run(res.Schema[MetaResourcePath].Default.(string)+"/"+tc.name, func(t *testing.T) {
				d := resourceDataWithRawConfig(res, "*1", map[string]cty.Value{
					"gateway":   cty.StringVal(""),
					"blackhole": tc.value,
				})
				if tc.present {
					if err := d.Set("blackhole", tc.value.True()); err != nil {
						t.Fatal(err)
					}
				}
				item, _ := TerraformResourceDataToMikrotik(res.Schema, d)
				value, present := item["blackhole"]
				if value != tc.want || present != tc.present {
					t.Fatalf("write blackhole = %q, present %v; want %q, present %v", value, present, tc.want, tc.present)
				}
			})
		}
	}
}
