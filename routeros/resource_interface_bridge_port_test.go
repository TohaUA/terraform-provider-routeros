package routeros

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testInterfaceBridgePortAddress = "routeros_interface_bridge_port.test_port"

func TestAccInterfaceBridgePortTest_basic(t *testing.T) {
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/interface/bridge/port", "routeros_interface_bridge_port"),
				Steps: []resource.TestStep{
					{
						Config: testAccInterfaceBridgePortConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testInterfaceBridgePortAddress),
							resource.TestCheckResourceAttr(testInterfaceBridgePortAddress, "bridge", "bridge"),
						),
					},
				},
			})

		})
	}
}

func testAccInterfaceBridgePortConfig() string {
	return providerConfig + `
resource "routeros_interface_bridge_port" "test_port" {
	bridge    = "bridge"
	interface = "ether1"
	pvid 	  = 200
	disabled  = true
	# Hex: RouterOS reports this field as 0x80 and 7.24 rejects the plain decimal
	# form with "input does not match any value of priority". The provider's own
	# validator parses with base 0, so both notations were always accepted here.
	priority  = "0x80"
  }

`
}
