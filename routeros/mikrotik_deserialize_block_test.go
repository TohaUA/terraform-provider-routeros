package routeros

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

// After a configuration drops the `input` block of a BGP connection, the apply unsets its
// properties, but RouterOS 7.24 keeps reporting some of them at their defaults. CI run
// 34485108679 read back input.allow-as=0, input.ignore-as-path-len=false,
// input.limit-process-routes-ipv4=0 and input.limit-process-routes-ipv6=0, so Read rebuilt
// the block and every plan tried to remove it again. A block the resource data already
// holds, and one carrying a non-default value, must still be read.
func Test_mikrotikResourceDataToTerraform_BlockAtRouterDefaults(t *testing.T) {
	blockTestSetVersion(t, "7.24")

	atDefaults := MikrotikItem{
		".id": "*1", "name": "neighbor-test",
		"input.allow-as": "0", "input.ignore-as-path-len": "false",
		"input.limit-process-routes-ipv4": "0", "input.limit-process-routes-ipv6": "0",
	}
	withValue := MikrotikItem{
		".id": "*1", "name": "neighbor-test",
		"input.allow-as": "0", "input.limit-process-routes-ipv4": "5",
	}

	for _, tc := range []struct {
		name  string
		state map[string]string
		item  MikrotikItem
		want  int
	}{
		{"removed block at router defaults stays removed", map[string]string{"input.#": "0"}, atDefaults, 0},
		{"import at router defaults reads no block", nil, atDefaults, 0},
		{"declared block at defaults is kept", map[string]string{"input.#": "1", "input.0.allow_as": "0"}, atDefaults, 1},
		{"non-default value is read as a block", map[string]string{"input.#": "0"}, withValue, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attrs := map[string]string{"id": "*1", "name": "neighbor-test"}
			for k, v := range tc.state {
				attrs[k] = v
			}
			res := ResourceRoutingBgpConnection()
			d := res.Data(&terraform.InstanceState{ID: "*1", Attributes: attrs})

			if diags := MikrotikResourceDataToTerraform(tc.item, res.Schema, d); diags.HasError() {
				t.Fatalf("read: %v", diags)
			}
			if got := len(d.Get("input").([]interface{})); got != tc.want {
				t.Errorf("input blocks = %d, want %d (input: %v)", got, tc.want, d.Get("input"))
			}
		})
	}
}
