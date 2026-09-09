package routeros

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testGre6Address = "routeros_interface_gre6.gre_v6"

func TestAccInterfaceGre6Test_basic(t *testing.T) {
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/interface/gre6", "routeros_interface_gre6"),
				Steps: []resource.TestStep{
					{
						Config: testAccInterfaceGre6Config(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testGre6Address),
							resource.TestCheckResourceAttr(testGre6Address, "name", "gre_v6"),
						),
					},
				},
			})

		})
	}

}

func testAccInterfaceGre6Config() string {
	return providerConfig + `

# A bridge rather than a veth: CHR stopped shipping /interface/veth after 7.16,
# and this only ever needed an interface to exist alongside the tunnel. No
# address is attached: the tunnel is created disabled, so it never has to come
# up, and routeros_ipv6_address carries its own drift on the advertise field,
# which has nothing to do with what this test is checking.
resource "routeros_interface_bridge" "gre6_v6" {
  name = "gre6_v6"
}

resource "routeros_interface_gre6" "gre_v6" {
  name           = "gre_v6"
  remote_address = "2a02::2"
  disabled       = true
  depends_on  = [routeros_interface_bridge.gre6_v6]
}
`
}
