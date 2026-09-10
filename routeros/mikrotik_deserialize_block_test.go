package routeros

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

// RouterOS reports a nested block's properties whether or not anything set them, so Read must not
// rebuild a non-Computed block the resource data does not carry. After a configuration dropped a BGP
// connection's `input` block, CI run 34485108679 on 7.24 read back input.allow-as=0,
// input.ignore-as-path-len=false and input.limit-process-routes-ipv4/ipv6=0, and every plan tried
// to remove the rebuilt block again. A BGP template reports defaults that are not zero values at all
// (input.affinity=0, input.ignore-as-path-len=true, recorded in resource_routing_bgp_template.go), so
// telling defaults apart by value cannot work. A block the resource data holds is still read, and a
// value changed on the router shows as drift.
func Test_mikrotikResourceDataToTerraform_BlockAtRouterDefaults(t *testing.T) {
	blockTestSetVersion(t, "7.24")

	connAtDefaults := MikrotikItem{
		".id": "*1", "name": "neighbor-test",
		"input.allow-as": "0", "input.ignore-as-path-len": "false",
		"input.limit-process-routes-ipv4": "0", "input.limit-process-routes-ipv6": "0",
	}
	connWithValue := MikrotikItem{
		".id": "*1", "name": "neighbor-test",
		"input.allow-as": "0", "input.limit-process-routes-ipv4": "5",
	}
	// As documented at the top of resource_routing_bgp_template.go.
	templateDefaults := MikrotikItem{
		".id": "*2", "name": "temp1",
		"input.accept-communities": "", "input.accept-ext-communities": "", "input.accept-large-communities": "",
		"input.accept-nlri": "", "input.accept-unknown": "", "input.affinity": "0", "input.allow-as": "0",
		"input.filter": "", "input.ignore-as-path-len": "true",
	}

	for _, tc := range []struct {
		name  string
		res   func() *schema.Resource
		id    string
		state map[string]string
		item  MikrotikItem
		want  int
		field string // when set, the input field whose value is checked
		value interface{}
	}{
		{"connection: removed block at router defaults stays removed", ResourceRoutingBgpConnection, "*1",
			map[string]string{"input.#": "0"}, connAtDefaults, 0, "", nil},
		{"connection: import reads no block", ResourceRoutingBgpConnection, "*1", nil, connAtDefaults, 0, "", nil},
		{"connection: declared block at defaults is kept", ResourceRoutingBgpConnection, "*1",
			map[string]string{"input.#": "1", "input.0.allow_as": "0"}, connAtDefaults, 1, "", nil},
		{"connection: a block not in state stays out whatever its values", ResourceRoutingBgpConnection, "*1",
			map[string]string{"input.#": "0"}, connWithValue, 0, "", nil},
		{"connection: a block in state picks up a value changed on the router", ResourceRoutingBgpConnection, "*1",
			map[string]string{"input.#": "1", "input.0.allow_as": "0", "input.0.limit_process_routes_ipv4": "2"},
			connWithValue, 1, "limit_process_routes_ipv4", 5},
		{"template: removed block at non-zero router defaults stays removed", ResourceRoutingBgpTemplate, "*2",
			map[string]string{"input.#": "0"}, templateDefaults, 0, "", nil},
		{"template: declared block is kept and round-trips", ResourceRoutingBgpTemplate, "*2",
			map[string]string{"input.#": "1", "input.0.ignore_as_path_len": "true"}, templateDefaults, 1,
			"ignore_as_path_len", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attrs := map[string]string{"id": tc.id, "name": tc.item["name"]}
			for k, v := range tc.state {
				attrs[k] = v
			}
			res := tc.res()
			d := res.Data(&terraform.InstanceState{ID: tc.id, Attributes: attrs})

			if diags := MikrotikResourceDataToTerraform(tc.item, res.Schema, d); diags.HasError() {
				t.Fatalf("read: %v", diags)
			}
			blocks := d.Get("input").([]interface{})
			if len(blocks) != tc.want {
				t.Fatalf("input blocks = %d, want %d (input: %v)", len(blocks), tc.want, blocks)
			}
			if tc.field != "" {
				if got := blocks[0].(map[string]interface{})[tc.field]; got != tc.value {
					t.Errorf("input.%s = %v, want %v", tc.field, got, tc.value)
				}
			}
		})
	}
}
