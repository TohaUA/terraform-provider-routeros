package routeros

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccWifi7251Distance(t *testing.T) {
	testAccPreCheck(t)
	for _, transport := range testNames {
		t.Run(transport, func(t *testing.T) {
			config := func(distance string) string {
				return providerConfig + fmt.Sprintf(`
resource "routeros_wifi_configuration" "test" {
  name = "tf-acc-wifi-distance7251"
  mode = "station-pseudobridge"
  hw_protection_mode = "rts-cts"
  distance = %q
}
`, distance)
			}
			resource.Test(t, resource.TestCase{
				PreCheck: func() { testSetTransportEnv(t, transport) }, ProviderFactories: testAccProviderFactories,
				CheckDestroy: testCheckResourceDestroy("/interface/wifi/configuration", "routeros_wifi_configuration"),
				Steps: []resource.TestStep{
					{Config: config("0.1"), Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("routeros_wifi_configuration.test", "distance", "0.1"),
						resource.TestCheckResourceAttr("routeros_wifi_configuration.test", "mode", "station-pseudobridge"),
						resource.TestCheckResourceAttr("routeros_wifi_configuration.test", "hw_protection_mode", "rts-cts"),
					)},
					{Config: config("1.2345"), Check: resource.TestCheckResourceAttr("routeros_wifi_configuration.test", "distance", "1.234")},
					{Config: config("1.2345000"), PlanOnly: true, ExpectNonEmptyPlan: false},
					{ResourceName: "routeros_wifi_configuration.test", ImportState: true, ImportStateVerify: true},
				},
			})
		})
	}
}
