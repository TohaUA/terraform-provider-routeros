package routeros

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testToolNetwatch = "routeros_tool_netwatch.test"

func TestAccToolNetwatchTest_dns(t *testing.T) {
	if !testCheckMinVersion(t, "7.16") {
		t.Skip("DNS probe options require RouterOS 7.16 or later")
	}
	testAccToolNetwatchProbeType(t, "dns", []string{"example.com", "example.net"}, `
  dns_server  = "8.8.8.8"
  record_type = "A"
`)
}

func TestAccToolNetwatchTest_httpsGet(t *testing.T) {
	testAccToolNetwatchProbeType(t, "https-get", []string{"1.1.1.1", "1.0.0.1"}, `
  port = 443
`)
}

func testAccToolNetwatchProbeType(t *testing.T, probeType string, hosts []string, options string) {
	t.Helper()
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			steps := make([]resource.TestStep, 0, len(hosts))
			for _, host := range hosts {
				checks := []resource.TestCheckFunc{
					testResourcePrimaryInstanceId(testToolNetwatch),
					resource.TestCheckResourceAttr(testToolNetwatch, "type", probeType),
					resource.TestCheckResourceAttr(testToolNetwatch, "host", host),
				}
				if probeType == "dns" {
					checks = append(checks,
						resource.TestCheckResourceAttr(testToolNetwatch, "dns_server", "8.8.8.8"),
						resource.TestCheckResourceAttr(testToolNetwatch, "record_type", "A"))
				} else {
					checks = append(checks, resource.TestCheckResourceAttr(testToolNetwatch, "port", "443"))
				}
				steps = append(steps, resource.TestStep{
					Config: providerConfig + fmt.Sprintf(`
resource "routeros_tool_netwatch" "test" {
  name     = "probe-type-test"
  type     = %q
  host     = %q
  interval = "30s"
%s
}
`, probeType, host, options),
					Check: resource.ComposeTestCheckFunc(checks...),
				})
			}
			// Probe health may be down in isolated CI; only configuration and
			// clean subsequent plans are asserted, not external connectivity.
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/tool/netwatch", "routeros_tool_netwatch"),
				Steps:             steps,
			})
		})
	}
}

func TestAccToolNetwatchTest_basic(t *testing.T) {
	// t.Parallel()
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				Steps: []resource.TestStep{
					{
						Config: testAccToolNetwatchConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testToolNetwatch),
							resource.TestCheckResourceAttr(testToolNetwatch, "name", "watch-google-pdns"),
							resource.TestCheckResourceAttr(testToolNetwatch, "host", "8.8.8.8"),
							resource.TestCheckResourceAttr(testToolNetwatch, "interval", "30s"),
							resource.TestCheckResourceAttr(testToolNetwatch, "up_script", ":log info \"Ping to 8.8.8.8 successful\""),
						),
					},
					{
						Config: testAccToolNetwatchConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testToolNetwatch),
							resource.TestCheckResourceAttr(testToolNetwatch, "name", "watch-google-pdns"),
							resource.TestCheckResourceAttr(testToolNetwatch, "host", "8.8.8.8"),
							resource.TestCheckResourceAttr(testToolNetwatch, "interval", "30s"),
							resource.TestCheckResourceAttr(testToolNetwatch, "up_script", ":log info \"Ping to 8.8.8.8 successful\""),
						),
					},
				},
			})

		})
	}
}

func testAccToolNetwatchConfig() string {
	return fmt.Sprintf(`%v
resource "routeros_tool_netwatch" "test" {
  name      = "watch-google-pdns"
  type      = "icmp"
  host      = "8.8.8.8"
  interval  = "30s"
  up_script = ":log info \"Ping to 8.8.8.8 successful\""
  thr_max   = "400ms"
}
`, providerConfig)
}
