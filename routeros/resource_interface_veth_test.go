package routeros

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testInterfaceVethAddress = "routeros_interface_veth.test"

// /interface/veth belongs to RouterOS's container feature, and the CHR image
// stopped carrying it: 7.16 answers HTTP 200 with an empty list, 7.24 answers
// "no such command or directory (veth)". The exact version it left is not
// established -- 7.16 is simply the newest one confirmed to have it -- so raise
// this bound if a later image turns out to still ship the menu. The resource
// itself is untouched and still works against a device that has the feature.
const testInterfaceVethMaxVersion = "7.16"

func TestAccInterfaceVethTest_basic(t *testing.T) {
	if !testCheckMaxVersion(t, testInterfaceVethMaxVersion) {
		t.Skipf("Test skipped, the CHR image stopped shipping /interface/veth after RouterOS %v",
			testInterfaceVethMaxVersion)
	}

	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/interface/veth", "routeros_interface_veth"),
				Steps: []resource.TestStep{
					{
						Config: testAccInterfaceVethConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testInterfaceVethAddress),
							resource.TestCheckResourceAttr(testInterfaceVethAddress, "name", "veth-test"),
						),
					},
				},
			})
		})
	}
}

func testAccInterfaceVethConfig() string {
	return providerConfig + `

resource "routeros_interface_veth" "test" {
  name    = "veth-test"
  address = ["192.168.120.2/24"]
  gateway = "192.168.120.1"
  comment = "Virtual interface"
}
`
}
