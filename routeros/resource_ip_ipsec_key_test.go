package routeros

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testIpIpsecKey = "routeros_ip_ipsec_key.test"

// RouterOS removed the /ip/ipsec/key menu outright. It answers HTTP 200 with an
// empty list on 7.16 and "no such command" on 7.24, so this is a removal rather
// than a rename and there is nothing for the resource to talk to.
const testIpIpsecKeyMaxVersion = "7.23"

func TestAccIpIpsecKeyTest_basic(t *testing.T) {
	if !testCheckMaxVersion(t, testIpIpsecKeyMaxVersion) {
		t.Skipf("Test skipped, /ip/ipsec/key was removed after RouterOS %v", testIpIpsecKeyMaxVersion)
	}

	// t.Parallel()
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/ip/ipsec/key", "routeros_ip_ipsec_key"),
				Steps: []resource.TestStep{
					{
						Config: testAccIpIpsecKeyConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testIpIpsecKey),
							resource.TestCheckResourceAttr(testIpIpsecKey, "name", "test-key"),
							resource.TestCheckResourceAttr(testIpIpsecKey, "key_size", "2048"),
						),
					},
				},
			})
		})
	}
}

func testAccIpIpsecKeyConfig() string {
	return fmt.Sprintf(`%v

resource "routeros_ip_ipsec_key" "test" {
  name     = "test-key"
  key_size = 2048
}
`, providerConfig)
}
