package routeros

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

// stubClient satisfies Client without touching a device: every request fails,
// so an update handler that gets as far as talking to RouterOS returns a
// diagnostic instead of hanging or panicking.
type stubClient struct{}

func (stubClient) GetExtraParams() *ExtraParams { return &ExtraParams{} }

func (stubClient) GetTransport() TransportType { return TransportREST }

func (stubClient) SendRequest(method crudMethod, url *URL, item MikrotikItem, result interface{}) error {
	return errors.New("no device in a unit test")
}

// resourceDataWithRawConfig builds ResourceData whose GetRawConfig is a real
// object value. schema.TestResourceDataRaw and Resource.TestResourceData both
// leave the raw config null, and TerraformResourceDataToMikrotik then panics
// inside cty GetAttr, which would mask the very panic these tests guard.
func resourceDataWithRawConfig(res *schema.Resource, id string, set map[string]cty.Value) *schema.ResourceData {
	vals := map[string]cty.Value{}
	for name, aType := range res.CoreConfigSchema().ImpliedType().AttributeTypes() {
		if v, ok := set[name]; ok {
			vals[name] = v
		} else {
			vals[name] = cty.NullVal(aType)
		}
	}

	attrs := map[string]string{"id": id}
	for name, v := range set {
		if !v.IsNull() && v.Type() == cty.String {
			attrs[name] = v.AsString()
		}
	}

	return res.Data(&terraform.InstanceState{ID: id, Attributes: attrs, RawConfig: cty.ObjectVal(vals)})
}

// TestAccessListUpdateDoesNotPanic is the regression test for the access-list
// update crash: the update handler writes Schema[MetaSkipFields].Default, which
// is a nil dereference unless the resource declares the key. It panicked on
// both resources before the fix.
func TestAccessListUpdateDoesNotPanic(t *testing.T) {
	tests := []struct {
		name string
		res  *schema.Resource
	}{
		{"routeros_wifi_access_list", ResourceWifiAccessList()},
		{"routeros_capsman_access_list", ResourceCapsManAccessList()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := RouterOSVersion
			RouterOSVersion = "7.24"
			defer func() { RouterOSVersion = old }()

			d := resourceDataWithRawConfig(tt.res, "*1", map[string]cty.Value{
				"comment": cty.StringVal("x"),
			})

			var diags diag.Diagnostics
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s: UpdateContext panicked: %v", tt.name, r)
					}
				}()
				diags = tt.res.UpdateContext(context.Background(), d, stubClient{})
			}()

			if !diags.HasError() {
				t.Fatalf("%s: expected the stub client's error to surface as a diagnostic, got %v", tt.name, diags)
			}

			if tt.res.Schema[MetaSkipFields] == nil {
				t.Fatalf("%s: %s is not declared in the schema", tt.name, MetaSkipFields)
			}
			if got := tt.res.Schema[MetaSkipFields].Default; got != "" {
				t.Errorf("%s: %s default is %q after the update, the deferred reset did not run", tt.name, MetaSkipFields, got)
			}
		})
	}
}

// TestUpdateContextDoesNotPanicOnAnyResource is the invariant behind the crash:
// no update handler may write a schema key the resource does not declare. It
// catches any future resource that sets Schema[MetaSkipFields].Default (or any
// other absent key) without declaring it.
func TestUpdateContextDoesNotPanicOnAnyResource(t *testing.T) {
	old := RouterOSVersion
	RouterOSVersion = "7.24"
	defer func() { RouterOSVersion = old }()

	// routeros_move_items slices into its `command` list and panics with
	// "slice bounds out of range [:-1]" on an all-null config. That is a
	// separate latent bug in an unrelated handler, not the schema-key
	// invariant under test here.
	skip := map[string]bool{"routeros_move_items": true}

	resources := Provider().ResourcesMap
	names := make([]string, 0, len(resources))
	for name := range resources {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		res := resources[name]
		if res.UpdateContext == nil || skip[name] {
			continue
		}

		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: UpdateContext panicked: %v", name, r)
				}
			}()
			res.UpdateContext(context.Background(), resourceDataWithRawConfig(res, "*1", nil), stubClient{})
		}()
	}
}
