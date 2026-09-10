package routeros

import (
	"context"
	"errors"
	"maps"
	"sort"
	"strings"
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

// recordingClient answers reads with readItem (or a bare {".id": "*1"}) and
// records every item a handler sends; everything other than a read fails. When
// release is set, the first read blocks until the test closes it, which holds
// an update in flight after it has serialized its item and before it returns.
type recordingClient struct {
	readItem MikrotikItem
	sent     map[crudMethod][]MikrotikItem
	entered  chan struct{}
	release  chan struct{}
	once     sync.Once
}

func (*recordingClient) GetExtraParams() *ExtraParams { return &ExtraParams{} }

func (*recordingClient) GetTransport() TransportType { return TransportREST }

func (c *recordingClient) SendRequest(method crudMethod, url *URL, item MikrotikItem, result interface{}) error {
	if method == crudRead {
		if c.release != nil {
			c.once.Do(func() { close(c.entered) })
			<-c.release
		}
		read := c.readItem
		if read == nil {
			read = MikrotikItem{".id": "*1"}
		}
		*result.(*[]MikrotikItem) = []MikrotikItem{maps.Clone(read)}
		return nil
	}

	if c.sent == nil {
		c.sent = map[crudMethod][]MikrotikItem{}
	}
	c.sent[method] = append(c.sent[method], maps.Clone(item))
	return errors.New("no device in a unit test")
}

// TestCreateKeepsPlaceBeforeDuringUpdate guards a race on the shared schema, in
// every resource whose update hides place_before. Those handlers used to write
// resSchema[MetaSkipFields].Default for the duration of the call. Terraform
// applies resources in parallel and the SDK does not serialize them, so a
// create that serialized its item inside that window read the same schema and
// dropped place-before, and RouterOS appended the new rule to the end of a
// first-match list without any error. The firewall and bridge filter handlers
// also restored the list each call saw on entry, so two overlapping updates
// could leave place_before in the shared list and every later create lost it.
//
// The first two parts are deterministic and fail against the old handlers. The
// last part runs creates and updates with no coordination at all; on its own
// that proves nothing, but under -race it is what lets the race detector see
// the shared write, since the channel hand-offs above order every access.
func TestCreateKeepsPlaceBeforeDuringUpdate(t *testing.T) {
	old := RouterOSVersion
	RouterOSVersion = "7.24"
	defer func() { RouterOSVersion = old }()

	tests := []struct {
		name string
		res  *schema.Resource
	}{
		{"routeros_wifi_access_list", ResourceWifiAccessList()},
		{"routeros_capsman_access_list", ResourceCapsManAccessList()},
		{"routeros_ip_firewall_filter", ResourceIPFirewallFilter()},
		{"routeros_ip_firewall_mangle", ResourceIPFirewallMangle()},
		{"routeros_ip_firewall_nat", ResourceIPFirewallNat()},
		{"routeros_ip_firewall_raw", ResourceIPFirewallRaw()},
		{"routeros_ipv6_firewall_mangle", ResourceIPv6FirewallMangle()},
		{"routeros_ipv6_firewall_nat", ResourceIPv6FirewallNat()},
		{"routeros_interface_bridge_filter", ResourceInterfaceBridgeFilter()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			declared := tt.res.Schema[MetaSkipFields].Default

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
			checkCreate := func(when string, c *recordingClient) {
				t.Helper()
				creates := c.sent[crudCreate]
				if len(creates) != 1 {
					t.Fatalf("%s: expected one create request %s, got %v", tt.name, when, c.sent)
				}
				if got, ok := creates[0]["place-before"]; !ok || got != "*5" {
					t.Errorf("%s: a create %s sent %v, want place-before=*5", tt.name, when, creates[0])
				}
			}
			// parkUpdate starts an update and returns once it is blocked in its
			// id lookup, mid-call.
			parkUpdate := func() (*recordingClient, chan struct{}) {
				t.Helper()
				c := &recordingClient{entered: make(chan struct{}), release: make(chan struct{})}
				d := updateData()
				done := make(chan struct{})
				go func() {
					defer close(done)
					tt.res.UpdateContext(context.Background(), d, c)
				}()
				select {
				case <-c.entered:
				case <-done:
					t.Fatalf("%s: the update returned before looking up its id", tt.name)
				}
				return c, done
			}

			// 1. A create while an update is in flight.
			upd, done := parkUpdate()
			cr := &recordingClient{}
			tt.res.CreateContext(context.Background(), createData(), cr)
			close(upd.release)
			<-done

			checkCreate("during an in-flight update", cr)

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

			// 2. Two overlapping updates where the later one finishes last, then a
			// create with nothing else in flight.
			first, firstDone := parkUpdate()
			second, secondDone := parkUpdate()
			close(first.release)
			<-firstDone
			close(second.release)
			<-secondDone

			later := &recordingClient{}
			tt.res.CreateContext(context.Background(), createData(), later)
			checkCreate("after two overlapping updates", later)
			if got := tt.res.Schema[MetaSkipFields].Default; got != declared {
				t.Errorf("%s: the shared skip list is %q after the updates, declared %q", tt.name, got, declared)
			}

			// 3. Uncoordinated creates and updates, for the race detector.
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
				checkCreate("alongside concurrent updates", c)
			}
			if got := tt.res.Schema[MetaSkipFields].Default; got != declared {
				t.Errorf("%s: the shared skip list is %q after concurrent updates, declared %q", tt.name, got, declared)
			}
		})
	}
}

// TestEthernetReadLeavesSharedSchemaAlone guards the same kind of race in
// routeros_interface_ethernet. Its read skips the per-router tx-queue counters,
// and used to do it by appending their names to the shared schema's skip list
// on every read, never removing them: a write that raced with every other
// ethernet interface being created, updated or read, and a list that grew for
// the life of the provider process. The read must still skip those counters.
func TestEthernetReadLeavesSharedSchemaAlone(t *testing.T) {
	old := RouterOSVersion
	RouterOSVersion = "7.24"
	defer func() { RouterOSVersion = old }()

	res := ResourceInterfaceEthernet()
	declared, _ := res.Schema[MetaSkipFields].Default.(string)
	item := MikrotikItem{
		".id":              "*1",
		"default-name":     "ether1",
		"name":             "ether1",
		"tx-queue0-packet": "5",
		"tx-queue1-byte":   "7",
	}
	data := func() *schema.ResourceData {
		return resourceDataWithRawConfig(res, "*1", map[string]cty.Value{
			"factory_name": cty.StringVal("ether1"),
			"comment":      cty.StringVal("x"),
		})
	}
	checkShared := func(when string) {
		t.Helper()
		if got, _ := res.Schema[MetaSkipFields].Default.(string); got != declared {
			t.Errorf("the shared skip list changed %s: %d bytes, declared %d; it ends %q",
				when, len(got), len(declared), got[max(0, len(got)-80):])
		}
	}

	diags := res.ReadContext(context.Background(), data(), &recordingClient{readItem: item})
	if diags.HasError() {
		t.Fatalf("read: %v", diags)
	}
	for _, d := range diags {
		if strings.Contains(d.Summary, "tx_queue") {
			t.Errorf("the read did not skip a tx-queue counter: %s", d.Summary)
		}
	}
	checkShared("after one read")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		rd, ud := data(), data()
		wg.Add(2)
		go func() {
			defer wg.Done()
			res.ReadContext(context.Background(), rd, &recordingClient{readItem: item})
		}()
		go func() {
			defer wg.Done()
			res.UpdateContext(context.Background(), ud, &recordingClient{readItem: item})
		}()
	}
	wg.Wait()
	checkShared("after concurrent reads and updates")
}

// TestSchemaWithSkipFields pins down the copy the handlers above now use: the
// declared skip list is kept, the new names are added, the result parses back
// to exactly the union, and the shared map and its entries are not touched.
func TestSchemaWithSkipFields(t *testing.T) {
	comment := PropCommentRw

	tests := []struct {
		name     string
		declared *schema.Schema // nil: the key is absent
		fields   []string
		want     string
		union    []string
	}{
		{
			name:     "declared list, as the firewall resources have",
			declared: PropSkipFields("bytes", "packets"),
			fields:   []string{KeyPlaceBefore},
			// Exactly what the old firewall handlers built by concatenation.
			want:  PropSkipFields("bytes", "packets").Default.(string) + `,"place_before"`,
			union: []string{"bytes", "packets", "place_before"},
		},
		{
			name:     "empty declared list, as the access lists have",
			declared: PropSkipFields(),
			fields:   []string{KeyPlaceBefore},
			want:     `"place_before"`,
			union:    []string{"place_before"},
		},
		{
			name:     "several added names",
			declared: PropSkipFields("factory_name"),
			fields:   []string{"tx_queue0_packet", "tx_queue1_byte"},
			want:     `"factory_name","tx_queue0_packet","tx_queue1_byte"`,
			union:    []string{"factory_name", "tx_queue0_packet", "tx_queue1_byte"},
		},
		{
			name:     "nothing added",
			declared: PropSkipFields("bytes"),
			want:     `"bytes"`,
			union:    []string{"bytes"},
		},
		{
			name:   "no declared key",
			fields: []string{KeyPlaceBefore},
			want:   `"place_before"`,
			union:  []string{"place_before"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := map[string]*schema.Schema{KeyComment: comment}
			var before interface{}
			if tt.declared != nil {
				s[MetaSkipFields] = tt.declared
				before = tt.declared.Default
			}

			got := schemaWithSkipFields(s, tt.fields...)

			if got[MetaSkipFields].Default != tt.want {
				t.Errorf("skip list %q, want %q", got[MetaSkipFields].Default, tt.want)
			}
			union := map[string]struct{}{}
			for _, f := range tt.union {
				union[f] = struct{}{}
			}
			if parsed := loadSkipFields(got[MetaSkipFields].Default.(string)); !maps.Equal(parsed, union) {
				t.Errorf("skip list parses to %v, want %v", parsed, union)
			}

			if tt.declared != nil {
				if s[MetaSkipFields] != tt.declared || tt.declared.Default != before {
					t.Errorf("the declared entry was changed: %q, was %q", tt.declared.Default, before)
				}
				if got[MetaSkipFields] == tt.declared {
					t.Errorf("the copy shares the declared *schema.Schema")
				}
			} else if _, ok := s[MetaSkipFields]; ok {
				t.Errorf("the key was added to the original map")
			}
			if got[KeyComment] != comment || len(got) != len(s)+btoi(tt.declared == nil) {
				t.Errorf("the other entries were not carried over: %v", got)
			}
		})
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
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
