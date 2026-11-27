package routeros

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testInterfaceWireguardAddress = "routeros_interface_wireguard.test_wg_interface"

func TestAccInterfaceWireguardTest_basic(t *testing.T) {
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/interface/wireguard", "routeros_interface_wireguard"),
				Steps: []resource.TestStep{
					{
						Config: testAccInterfaceWireguardConfig("", 0),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testInterfaceWireguardAddress),
							resource.TestCheckResourceAttr(testInterfaceWireguardAddress, "listen_port", "13231"),
							resource.TestCheckResourceAttrSet(testInterfaceWireguardAddress, "public_key"),
							resource.TestCheckNoResourceAttr(testInterfaceWireguardAddress, "private_key"),
						),
					},
					{
						Config: testAccInterfaceWireguardConfig("gLP306E2BCZBeyZ0ILrS5Ubdg4VjkFYiWkg7HpKYM10=", 1),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testInterfaceWireguardAddress),
							resource.TestCheckResourceAttr(testInterfaceWireguardAddress, "listen_port", "13231"),
							resource.TestCheckResourceAttr(testInterfaceWireguardAddress, "private_key_wo_version", "1"),
							resource.TestCheckResourceAttrSet(testInterfaceWireguardAddress, "public_key"),
							resource.TestCheckNoResourceAttr(testInterfaceWireguardAddress, "private_key"),
						),
					},
				},
			})

		})
	}
}

func testAccInterfaceWireguardConfig(privateKey string, version int) string {
	extra := ""
	if privateKey != "" {
		extra = fmt.Sprintf("\tprivate_key            = %q\n\tprivate_key_wo_version = %d\n", privateKey, version)
	}
	return providerConfig + fmt.Sprintf(`

resource "routeros_interface_wireguard" "test_wg_interface" {
	name        = "test_wg_interface"
	listen_port = "13231"
%s}

`, extra)
}
