package routeros

import (
	"context"
	"errors"
	"maps"
	"sort"
	"sync"
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
// update crash: the update handler wrote Schema[MetaSkipFields].Default, which
// is a nil dereference unless the resource declares the key. It panicked on
// both resources before that fix.
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
				t.Errorf("%s: %s default is %q after the update, the update changed the shared schema", tt.name, MetaSkipFields, got)
			}
		})
	}
}

// recordingClient answers the id lookup an update performs and records every
// item a handler sends; everything other than a read fails. When release is
// set, the first read blocks until the test closes it, which holds an update
// in flight after it has serialized its item and before it returns.
type recordingClient struct {
	sent    map[crudMethod][]MikrotikItem
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (*recordingClient) GetExtraParams() *ExtraParams { return &ExtraParams{} }

func (*recordingClient) GetTransport() TransportType { return TransportREST }

func (c *recordingClient) SendRequest(method crudMethod, url *URL, item MikrotikItem, result interface{}) error {
	if method == crudRead {
		if c.release != nil {
			c.once.Do(func() { close(c.entered) })
			<-c.release
		}
		*result.(*[]MikrotikItem) = []MikrotikItem{{".id": "*1"}}
		return nil
	}

	if c.sent == nil {
		c.sent = map[crudMethod][]MikrotikItem{}
	}
	c.sent[method] = append(c.sent[method], maps.Clone(item))
	return errors.New("no device in a unit test")
}

// TestAccessListCreateKeepsPlaceBeforeDuringUpdate guards a race on the shared
// schema. The update handlers used to hide place_before by writing
// resSchema[MetaSkipFields].Default for the duration of the call. Terraform
// applies resources in parallel and the SDK does not serialize them, so a
// create that serialized its item inside that window read the same schema and
// dropped place-before, and RouterOS appended the new entry to the end of a
// first-match list without any error.
//
// The first half parks an update mid-call and runs a create, which fails
// deterministically against the old handlers. The second half runs creates and
// updates with no coordination at all; that proves nothing on its own, but
// under -race it is what lets the race detector see the shared write, since
// the channel hand-off in the first half orders the two accesses.
func TestAccessListCreateKeepsPlaceBeforeDuringUpdate(t *testing.T) {
	old := RouterOSVersion
	RouterOSVersion = "7.24"
	defer func() { RouterOSVersion = old }()

	tests := []struct {
		name string
		res  *schema.Resource
	}{
		{"routeros_wifi_access_list", ResourceWifiAccessList()},
		{"routeros_capsman_access_list", ResourceCapsManAccessList()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updateData := func() *schema.ResourceData {
				return resourceDataWithRawConfig(tt.res, "*1", map[string]cty.Value{
					"comment":      cty.StringVal("x"),
					"place_before": cty.StringVal("*5"),
				})
			}
			createData := func() *schema.ResourceData {
				return resourceDataWithRawConfig(tt.res, "", map[string]cty.Value{
					"comment":      cty.StringVal("new"),
					"place_before": cty.StringVal("*5"),
				})
			}
			checkCreate := func(c *recordingClient) {
				t.Helper()
				creates := c.sent[crudCreate]
				if len(creates) != 1 {
					t.Fatalf("%s: expected one create request, got %v", tt.name, c.sent)
				}
				if got, ok := creates[0]["place-before"]; !ok || got != "*5" {
					t.Errorf("%s: a create during an in-flight update sent %v, want place-before=*5", tt.name, creates[0])
				}
			}

			upd := &recordingClient{entered: make(chan struct{}), release: make(chan struct{})}
			done := make(chan struct{})
			go func() {
				defer close(done)
				tt.res.UpdateContext(context.Background(), updateData(), upd)
			}()

			select {
			case <-upd.entered:
				// The update is parked in its id lookup, mid-call.
			case <-done:
				t.Fatalf("%s: the update returned before looking up its id", tt.name)
			}

			cr := &recordingClient{}
			tt.res.CreateContext(context.Background(), createData(), cr)

			close(upd.release)
			<-done

			checkCreate(cr)

			// The skip must still apply to the update itself.
			updates := upd.sent[crudUpdate]
			if len(updates) != 1 {
				t.Fatalf("%s: expected one update request, got %v", tt.name, upd.sent)
			}
			if _, ok := updates[0]["place-before"]; ok {
				t.Errorf("%s: the update sent place-before: %v", tt.name, updates[0])
			}
			if updates[0][KeyComment] != "x" {
				t.Errorf("%s: the update did not send the changed comment: %v", tt.name, updates[0])
			}

			// Uncoordinated creates and updates, for the race detector.
			const pairs = 8
			creators := make([]*recordingClient, pairs)
			var wg sync.WaitGroup
			for i := range creators {
				creators[i] = &recordingClient{}
				ud, cd := updateData(), createData()
				wg.Add(2)
				go func() {
					defer wg.Done()
					tt.res.UpdateContext(context.Background(), ud, &recordingClient{})
				}()
				go func(c *recordingClient) {
					defer wg.Done()
					tt.res.CreateContext(context.Background(), cd, c)
				}(creators[i])
			}
			wg.Wait()

			for _, c := range creators {
				checkCreate(c)
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
