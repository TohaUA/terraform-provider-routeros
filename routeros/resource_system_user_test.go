package routeros

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testUserAddress = "routeros_system_user.test"

func TestAccUserTest_basic(t *testing.T) {
	// t.Parallel()
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/user", "routeros_system_user"),
				Steps: []resource.TestStep{
					{
						Config: testAccUserConfig("secret", 1),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testUserAddress),
							resource.TestCheckResourceAttr(testUserAddress, "name", "test-user-1"),
							resource.TestCheckResourceAttr(testUserAddress, "password_wo_version", "1"),
							resource.TestCheckNoResourceAttr(testUserAddress, "password"),
						),
					},
					{
						Config: testAccUserConfig("secret2", 2),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testUserAddress),
							resource.TestCheckResourceAttr(testUserAddress, "password_wo_version", "2"),
							resource.TestCheckNoResourceAttr(testUserAddress, "password"),
						),
					},
				},
			})

		})
	}
}

func testAccUserConfig(password string, version int) string {
	return providerConfig + fmt.Sprintf(`

resource "routeros_system_user" "test" {
	name                = "test-user-1"
	address             = "0.0.0.0/0"
	group               = "read"
	password            = %q
	password_wo_version = %d
	comment             = "Test User"
}
`, password, version)
}
