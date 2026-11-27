package routeros

import (
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// These tests require a disposable RouterOS 7.25.1+ instance. New DHCP servers and
// authentication entries stay disabled; no clients or remote peers are needed.
func TestAccRouterOS725Fields(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("requires TF_ACC and a disposable RouterOS instance")
	}
	if !testCheckMinVersion(t, "7.25.1") {
		t.Skip("requires RouterOS 7.25.1 or later")
	}
	for _, tc := range []struct {
		name, resourceType, path, base, field string
		values                                []string
		testUnset                             bool
		unsetWant                             string
	}{
		{"bridge", "routeros_interface_bridge", "/interface/bridge", `
  name = "tf-725-bridge"
  igmp_snooping = true
`, "last_member_interval", []string{`"5s"`, `"10s"`}, false, ""},
		{"dot1x", "routeros_interface_dot1x_server", "/interface/dot1x/server", `
  interface = "ether1"
  disabled = true
`, "reauth_timeout", []string{`"5s"`, `"10s"`}, false, ""},
		{"bgp", "routeros_routing_bgp_instance", "/routing/bgp/instance", `
  name = "tf-725-bgp"
  as = "65000"
  disabled = true
`, "always_compare_med", []string{"true", "false"}, false, ""},
		{"dhcp", "routeros_ip_dhcp_server", "/ip/dhcp-server", `
  name = "tf-725-dhcp"
  interface = "bridge"
  address_pool = "static-only"
  disabled = true
`, "ipv6_only_preferred", []string{"true", "false"}, false, ""},
		{"dhcpv6", "routeros_ipv6_dhcp_server", "/ipv6/dhcp-server", `
  name = "tf-725-dhcpv6"
  interface = "bridge"
  address_pool = "static-only"
  prefix_pool = "static-only"
  disabled = true
  add_dns_entries_suffix = "example.test"
`, "add_dns_entries", []string{"true", "false"}, false, ""},
		{"vlan", "routeros_interface_vlan", "/interface/vlan", `
  name = "tf-725-vlan"
  interface = "bridge"
  vlan_id = 725
  disabled = true
`, "forced_mac_address", []string{`"02:00:00:00:07:25"`, `"02:00:00:00:07:26"`}, true, ""},
		{"wireguard", "routeros_interface_wireguard_peer", "/interface/wireguard/peers", `
  interface = "wg1"
  public_key = "QxC+CTcrDdU5+ny0+2ChUH3NegTrwoVCv53TllI5T0I="
  allowed_address = ["192.0.2.0/30"]
  disabled = true
`, "client_mtu", []string{"1380", "1420"}, false, ""},
		{"snmp", "routeros_snmp_community", "/snmp/community", `
  name = "tf-725-snmp"
  security = "authorized"
  authentication_password = "testauth725"
  disabled = true
`, "authentication_protocol", []string{`"SHA256"`, `"SHA384"`, `"SHA512"`}, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, transport := range testNames {
				t.Run(transport, func(t *testing.T) {
					address := tc.resourceType + ".test_725"
					steps := make([]resource.TestStep, 0, len(tc.values)+1)
					for _, value := range tc.values {
						want := value
						if unquoted, err := strconv.Unquote(value); err == nil {
							want = unquoted
						}
						steps = append(steps, resource.TestStep{
							Config: providerConfig + fmt.Sprintf("\nresource %q \"test_725\" {\n%s\n%s = %s\n}\n", tc.resourceType, tc.base, tc.field, value),
							Check: resource.ComposeTestCheckFunc(
								testResourcePrimaryInstanceId(address),
								resource.TestCheckResourceAttr(address, tc.field, want),
							),
						})
					}
					if tc.testUnset {
						steps = append(steps, resource.TestStep{
							Config: providerConfig + fmt.Sprintf("\nresource %q \"test_725\" {\n%s\n}\n", tc.resourceType, tc.base),
							Check: resource.ComposeTestCheckFunc(
								testResourcePrimaryInstanceId(address),
								resource.TestCheckResourceAttr(address, tc.field, tc.unsetWant),
							),
						})
					}
					resource.Test(t, resource.TestCase{
						PreCheck: func() {
							testAccPreCheck(t)
							testSetTransportEnv(t, transport)
						},
						ProviderFactories: testAccProviderFactories,
						CheckDestroy:      testCheckResourceDestroy(tc.path, tc.resourceType),
						Steps:             steps,
					})
				})
			}
		})
	}
}
