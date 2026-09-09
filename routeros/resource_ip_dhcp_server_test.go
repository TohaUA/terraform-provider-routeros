package routeros

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testIpDhcpServerAddress = "routeros_ip_dhcp_server.test_dhcp"

func TestAccIpDhcpServerTest_basic(t *testing.T) {
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/ip/dhcp-server", "routeros_ip_dhcp_server"),
				Steps: []resource.TestStep{
					{
						Config: testAccIpDhcpServerConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testIpDhcpServerAddress),
							resource.TestCheckResourceAttr(testIpDhcpServerAddress, "interface", "bridge"),
						),
					},
				},
			})

		})
	}
}

// RouterOS 7.23 introduced add-dns-entries together with add-dns-entries-suffix;
// the suffix is meaningless unless the switch that enables the feature is writable too.
func TestIpDhcpServerSchema_addDnsEntriesPair(t *testing.T) {
	s := ResourceDhcpServer().Schema

	for name, typ := range map[string]schema.ValueType{
		"add_dns_entries":        schema.TypeBool,
		"add_dns_entries_suffix": schema.TypeString,
	} {
		f, ok := s[name]
		if !ok {
			t.Fatalf("%s: attribute missing from routeros_ip_dhcp_server schema", name)
		}
		if f.Type != typ {
			t.Errorf("%s: type = %v, want %v", name, f.Type, typ)
		}
		if !f.Optional {
			t.Errorf("%s: must be optional", name)
		}
		if f.DiffSuppressFunc == nil {
			t.Errorf("%s: must suppress the diff when not user provided (pre-7.23 devices do not report it)", name)
		}
	}
}

func testAccIpDhcpServerConfig() string {
	return providerConfig + `
resource "routeros_ip_dhcp_server" "test_dhcp" {
	name	     = "test_dhcp_server"
	interface    = "bridge"
	disabled     = true
	address_pool = "dhcp"
  }

`
}
