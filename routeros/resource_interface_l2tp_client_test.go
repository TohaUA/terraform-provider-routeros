package routeros

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testInterfaceL2tpClient = "routeros_interface_l2tp_client.test"

func TestAccInterfaceL2tpClientTest_basic(t *testing.T) {
	t.Parallel()
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			var client MikrotikItem
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/interface/l2tp-client", "routeros_interface_l2tp_client"),
				Steps: []resource.TestStep{
					{
						Config: testAccInterfaceL2tpClientConfig("bbb", 1),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testInterfaceL2tpClient),
							testCheckResourceExists(testInterfaceL2tpClient, "/interface/l2tp-client", &client),
							testCheckMikrotikItemAttr(testInterfaceL2tpClient, &client, "password", "bbb"),
							resource.TestCheckResourceAttr(testInterfaceL2tpClient, "name", "l2tp-test-out"),
							resource.TestCheckResourceAttr(testInterfaceL2tpClient, "connect_to", "127.0.0.1"),
							resource.TestCheckResourceAttr(testInterfaceL2tpClient, "user", "aaa"),
							resource.TestCheckNoResourceAttr(testInterfaceL2tpClient, "password"),
							resource.TestCheckResourceAttr(testInterfaceL2tpClient, "password_wo_version", "1"),
						),
					},
					{
						Config: testAccInterfaceL2tpClientConfig("ccc", 2),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testInterfaceL2tpClient),
							testCheckResourceExists(testInterfaceL2tpClient, "/interface/l2tp-client", &client),
							testCheckMikrotikItemAttr(testInterfaceL2tpClient, &client, "password", "ccc"),
							resource.TestCheckNoResourceAttr(testInterfaceL2tpClient, "password"),
							resource.TestCheckResourceAttr(testInterfaceL2tpClient, "password_wo_version", "2"),
						),
					},
				},
			})

		})
	}
}

func testAccInterfaceL2tpClientConfig(password string, version int) string {
	return fmt.Sprintf(`%v

resource "routeros_interface_l2tp_client" "test" {
  name                = "l2tp-test-out"
  connect_to          = "127.0.0.1"
  user                = "aaa"
  password            = %q
  password_wo_version = %d
}
`, providerConfig, password, version)
}
