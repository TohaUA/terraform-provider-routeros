package routeros

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testIpIpsecModeConfig = "routeros_ip_ipsec_mode_config.test"

func TestAccIpIpsecModeConfigTest_basic(t *testing.T) {
	// t.Parallel()
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/ip/ipsec/mode-config", "routeros_ip_ipsec_mode_config"),
				Steps: []resource.TestStep{
					{
						Config: testAccIpIpsecModeConfigConfig("example.com"),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testIpIpsecModeConfig),
							resource.TestCheckResourceAttr(testIpIpsecModeConfig, "name", "test-cfg"),
							resource.TestCheckResourceAttr(testIpIpsecModeConfig, "address", "1.2.3.4"),
							resource.TestCheckResourceAttr(testIpIpsecModeConfig, "split_include.0", "0.0.0.0/0"),
							resource.TestCheckTypeSetElemAttr(testIpIpsecModeConfig, "split_dns.*", "example.com"),
						),
					},
					{
						Config: testAccIpIpsecModeConfigConfig("corp.local"),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(testIpIpsecModeConfig, "name", "test-cfg"),
							resource.TestCheckResourceAttr(testIpIpsecModeConfig, "split_dns.#", "1"),
							resource.TestCheckTypeSetElemAttr(testIpIpsecModeConfig, "split_dns.*", "corp.local"),
						),
					},
				},
			})

		})
	}
}

func testAccIpIpsecModeConfigConfig(domain string) string {
	return fmt.Sprintf(`%v

resource "routeros_ip_ipsec_mode_config" "test" {
  name          = "test-cfg"
  address       = "1.2.3.4"
  split_include = ["0.0.0.0/0"]
  split_dns     = [%q]
}
`, providerConfig, domain)
}
