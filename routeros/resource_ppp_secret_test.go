package routeros

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testPPPSecret = "routeros_ppp_secret.test"

func TestAccPPPSecretTest_basic(t *testing.T) {
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			var secret MikrotikItem
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/ppp/secret", "routeros_ppp_secret"),
				Steps: []resource.TestStep{
					{
						Config: testAccPPPSecretConfig("12345678", 1),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testPPPSecret),
							testCheckResourceExists(testPPPSecret, "/ppp/secret", &secret),
							testCheckMikrotikItemAttr(testPPPSecret, &secret, "password", "12345678"),
							resource.TestCheckResourceAttr(testPPPSecret, "name", "user-test"),
							resource.TestCheckResourceAttr(testPPPSecret, "password_wo_version", "1"),
							resource.TestCheckNoResourceAttr(testPPPSecret, "password"),
						),
					},
					{
						Config: testAccPPPSecretConfig("87654321", 2),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testPPPSecret),
							testCheckResourceExists(testPPPSecret, "/ppp/secret", &secret),
							testCheckMikrotikItemAttr(testPPPSecret, &secret, "password", "87654321"),
							resource.TestCheckResourceAttr(testPPPSecret, "password_wo_version", "2"),
							resource.TestCheckNoResourceAttr(testPPPSecret, "password"),
						),
					},
				},
			})

		})
	}
}

func testAccPPPSecretConfig(password string, version int) string {
	return providerConfig + fmt.Sprintf(`
	resource "routeros_ppp_secret" "test" {
		name                = "user-test"
		password            = %q
		password_wo_version = %d
	}
`, password, version)
}
