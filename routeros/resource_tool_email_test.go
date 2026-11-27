package routeros

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testToolEmail = "routeros_tool_email.email"

func TestAccToolEmailTest_basic(t *testing.T) {
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			var email MikrotikItem
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/tool/e-mail", "routeros_email"),
				Steps: []resource.TestStep{
					{
						Config: testAccToolEmailConfig("password", 1),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testToolEmail),
							testCheckResourceExists(testToolEmail, "/tool/e-mail", &email),
							testCheckMikrotikItemAttr(testToolEmail, &email, "password", "password"),
							resource.TestCheckResourceAttr(testToolEmail, "from", "John Doe <jdoe@example.com>"),
							resource.TestCheckNoResourceAttr(testToolEmail, "password"),
							resource.TestCheckResourceAttr(testToolEmail, "password_wo_version", "1"),
							resource.TestCheckResourceAttr(testToolEmail, "port", "25"),
							resource.TestCheckResourceAttr(testToolEmail, "server", "smtp.example.com"),
							resource.TestCheckResourceAttr(testToolEmail, "tls", "yes"),
							resource.TestCheckResourceAttr(testToolEmail, "user", "jdoe"),
							resource.TestCheckResourceAttr(testToolEmail, "vrf", "main"),
						),
					},
					{
						Config: testAccToolEmailConfig("password2", 2),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testToolEmail),
							testCheckResourceExists(testToolEmail, "/tool/e-mail", &email),
							testCheckMikrotikItemAttr(testToolEmail, &email, "password", "password2"),
							resource.TestCheckNoResourceAttr(testToolEmail, "password"),
							resource.TestCheckResourceAttr(testToolEmail, "password_wo_version", "2"),
						),
					},
				},
			})

		})
	}
}

func testAccToolEmailConfig(password string, version int) string {
	return providerConfig + fmt.Sprintf(`
resource "routeros_tool_email" "email" {
	from                = "John Doe <jdoe@example.com>"
	password            = %q
	password_wo_version = %d
	port                = "25"
	server              = "smtp.example.com"
	tls                 = "yes"
	user                = "jdoe"
	vrf                 = "main"
}
`, password, version)
}
