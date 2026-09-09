package routeros

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testIpSSHServerSettings = "routeros_ip_ssh_server.test"

func TestAccIpSSHServerSettingsTest_basic(t *testing.T) {
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				Steps: []resource.TestStep{
					{
						Config: testAccIpSSHServerSettingsConfig(true, "yes", "both", 1024),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testIpSSHServerSettings),
							resource.TestCheckResourceAttr(testIpSSHServerSettings, "strong_crypto", "true"),
							resource.TestCheckResourceAttr(testIpSSHServerSettings, "password_authentication", "yes"),
							resource.TestCheckResourceAttr(testIpSSHServerSettings, "forwarding_enabled", "both"),
							resource.TestCheckResourceAttr(testIpSSHServerSettings, "host_key_size", "1024"),
						),
					},
					{
						Config: testAccIpSSHServerSettingsConfig(false, "yes-if-no-key", "no", 2048),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(testIpSSHServerSettings, "strong_crypto", "false"),
							resource.TestCheckResourceAttr(testIpSSHServerSettings, "password_authentication", "yes-if-no-key"),
							resource.TestCheckResourceAttr(testIpSSHServerSettings, "forwarding_enabled", "no"),
							resource.TestCheckResourceAttr(testIpSSHServerSettings, "host_key_size", "2048"),
						),
					},
				},
			})
		})
	}
}

// RouterOS 7.21 dropped allow-none-crypto and always-allow-password-login from
// /ip/ssh. strong_crypto is the surviving half of the allow_none_crypto pair
// (they are ExactlyOneOf), and password_authentication replaced the login flag,
// so it takes a string rather than a bool.
func testAccIpSSHServerSettingsConfig(strong bool, pwAuth, fwd string, kSize int) string {
	return fmt.Sprintf(`%v

resource "routeros_ip_ssh_server" "test" {
	strong_crypto           = %v
	password_authentication = "%v"
	forwarding_enabled      = "%v"
	host_key_size           = %v
}
`, providerConfig, strong, pwAuth, fwd, kSize)
}
