package routeros

import (
	"context"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/go-cty/cty/msgpack"
	"github.com/hashicorp/terraform-plugin-go/tfprotov5"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// Exercise the registered upgrader through the same SDK protocol handler that
// Terraform calls, including decoding legacy state and encoding the new list.
func TestDNSStateUpgradePreservesServers(t *testing.T) {
	for name, raw := range map[string]*tfprotov5.RawState{
		"json": {JSON: []byte(`{"id":"dns","servers":"1.1.1.1,8.8.8.8","allow_remote_requests":true}`)},
		"flatmap": {Flatmap: map[string]string{
			"id": "dns", "servers": "1.1.1.1,8.8.8.8", "allow_remote_requests": "true",
		}},
	} {
		t.Run(name, func(t *testing.T) {
			p := Provider()
			res := p.ResourcesMap["routeros_ip_dns"]
			response, err := schema.NewGRPCProviderServer(p).UpgradeResourceState(context.Background(), &tfprotov5.UpgradeResourceStateRequest{
				TypeName: "routeros_ip_dns",
				Version:  0,
				RawState: raw,
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range response.Diagnostics {
				if diagnostic.Severity == tfprotov5.DiagnosticSeverityError {
					t.Fatalf("state upgrade: %s: %s", diagnostic.Summary, diagnostic.Detail)
				}
			}
			if response.UpgradedState == nil {
				t.Fatal("state upgrade returned no state")
			}
			state, err := msgpack.Unmarshal(response.UpgradedState.MsgPack, res.CoreConfigSchema().ImpliedType())
			if err != nil {
				t.Fatal(err)
			}
			want := cty.ListVal([]cty.Value{cty.StringVal("1.1.1.1"), cty.StringVal("8.8.8.8")})
			if got := state.GetAttr("servers"); !got.RawEquals(want) {
				t.Errorf("servers = %s, want %s", got.GoString(), want.GoString())
			}
			if got := state.GetAttr("id"); !got.RawEquals(cty.StringVal("dns")) {
				t.Errorf("id was changed: %s", got.GoString())
			}
			if got := state.GetAttr("allow_remote_requests"); !got.RawEquals(cty.True) {
				t.Errorf("unrelated setting was changed: %s", got.GoString())
			}
		})
	}
}
