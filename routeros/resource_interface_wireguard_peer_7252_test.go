package routeros

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const wireguard7252Key = "QxC+CTcrDdU5+ny0+2ChUH3NegTrwoVCv53TllI5T0I="

func TestWireguardPeerPublicKeyWhitespacePlanning(t *testing.T) {
	for _, tc := range []struct {
		name, key string
		change    bool
	}{
		{"unchanged", wireguard7252Key, false},
		{"surrounding_whitespace", " \t\r\n\v\f" + wireguard7252Key + "\f\v\n\t ", false},
		{"different_key", "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=", true},
		{"internal_whitespace", wireguard7252Key[:8] + " " + wireguard7252Key[8:], true},
		{"unicode_whitespace", "\u00a0" + wireguard7252Key + "\u2003", true},
		{"case_sensitive", strings.ToLower(wireguard7252Key), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := ResourceInterfaceWireguardPeer()
			values := map[string]cty.Value{
				"interface":       cty.StringVal("wg1"),
				"allowed_address": cty.ListVal([]cty.Value{cty.StringVal("192.0.2.1/32")}),
				"public_key":      cty.StringVal(wireguard7252Key),
			}
			d := resourceDataWithRawConfig(r, "*1", values)
			if err := d.Set("allowed_address", []interface{}{"192.0.2.1/32"}); err != nil {
				t.Fatal(err)
			}
			state := d.State()
			values["public_key"] = cty.StringVal(tc.key)
			config := terraform.NewResourceConfigRaw(map[string]interface{}{
				"interface": "wg1", "allowed_address": []interface{}{"192.0.2.1/32"}, "public_key": tc.key,
			})
			config.CtyValue = resourceDataWithRawConfig(r, "*1", values).GetRawConfig()
			diff, err := r.Diff(context.Background(), state, config, nil)
			if err != nil {
				t.Fatal(err)
			}
			changed := diff != nil && diff.Attributes["public_key"] != nil
			if changed != tc.change {
				t.Fatalf("public key change = %t, want %t: %#v", changed, tc.change, diff)
			}
			if !tc.change && diff != nil && !diff.Empty() {
				t.Fatalf("equivalent key produced a plan: %#v", diff.Attributes)
			}
		})
	}
}

func TestAccInterfaceWireguardPeer7252Whitespace(t *testing.T) {
	testAccPreCheck(t)
	for _, transport := range testNames {
		t.Run(transport, func(t *testing.T) {
			config := func(key string) string {
				return providerConfig + fmt.Sprintf(`
resource "routeros_interface_wireguard" "whitespace" {
  name = "tf-7252-wireguard"
  listen_port = 51872
}
resource "routeros_interface_wireguard_peer" "whitespace" {
  interface = routeros_interface_wireguard.whitespace.name
  allowed_address = ["192.0.2.1/32"]
  public_key = %q
  disabled = true
}
`, key)
			}
			address := "routeros_interface_wireguard_peer.whitespace"
			next := "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
			resource.Test(t, resource.TestCase{
				PreCheck: func() { testSetTransportEnv(t, transport) }, ProviderFactories: testAccProviderFactories,
				CheckDestroy: testCheckResourceDestroy("/interface/wireguard/peers", "routeros_interface_wireguard_peer"),
				Steps: []resource.TestStep{
					{Config: config(" \t" + wireguard7252Key + "\n"), Check: resource.TestCheckResourceAttr(address, "public_key", wireguard7252Key)},
					{Config: config(" \t" + wireguard7252Key + "\n"), PlanOnly: true, ExpectNonEmptyPlan: false},
					{Config: config(" " + next + " "), Check: resource.TestCheckResourceAttr(address, "public_key", next)},
					{Config: config(next), PlanOnly: true, ExpectNonEmptyPlan: false},
					{ResourceName: address, ImportState: true, ImportStateVerify: true},
				},
			})
		})
	}
}
