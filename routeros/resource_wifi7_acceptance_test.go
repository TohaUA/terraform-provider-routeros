package routeros

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// CHR covers API configuration workflows, not physical WiFi 7 radio operation.
func TestAccWifi7ProfilesAndMLD(t *testing.T) {
	testAccPreCheck(t)
	for _, transport := range testNames {
		t.Run(transport, func(t *testing.T) {
			config := func(group, puncturing, distance string) string {
				return providerConfig + fmt.Sprintf(`
resource "routeros_wifi" "mld" {
  name = "tf-acc-wifi7-mld"
  mac_address = "02:00:00:72:51:01"
  mld_name = %q
  disabled = true
  configuration = { ssid = "tf-wifi7", mode = "ap" }
}
resource "routeros_wifi_channel" "channel" {
  name = "tf-acc-wifi7-channel"
  band = "6ghz-be"
  width = "20/40/80/160/320mhz"
  preamble_puncturing = %q
  afc = false
}
resource "routeros_wifi_security" "security" {
  name = "tf-acc-wifi7-security"
  authentication_types = ["wpa3-psk", "wpa2-psk-sha2"]
  passphrase = "test-wifi7-password"
  beacon_protection = "enabled"
  ft_r0_key_lifetime = "60m"
}
resource "routeros_wifi_configuration" "profile" {
  name = "tf-acc-wifi7-profile"
  installation = "indoor"
  max_clients = 12
  station_roaming = false
}
resource "routeros_wifi_provisioning" "provision" {
  action = "use-network-config"
  multi_link_mode = "all"
  supported_hw_caps = ["mlo"]
  disabled = true
}
resource "routeros_interface_wifi_network_radio" "radio" {
  labels = "tf-wifi7"
  extra_labels = ["tf-wifi7-extra"]
  channel_band = "6ghz-be"
  channel_width = "20/40/80/160/320mhz"
  channel_preamble_puncturing = %q
  channel_afc = false
  configuration_distance = %q
  security_authentication_types = ["wpa3-psk"]
  disabled = true
}
data "routeros_interface_wifi_radios" "radios" {}
data "routeros_interface_wifi_monitor" "mld" {
  arguments = { numbers = routeros_wifi.mld.id }
}
`, group, puncturing, puncturing, distance)
			}
			resource.Test(t, resource.TestCase{
				PreCheck: func() { testSetTransportEnv(t, transport) }, ProviderFactories: testAccProviderFactories,
				CheckDestroy: resource.ComposeTestCheckFunc(testCheckResourceDestroy("/interface/wifi", "routeros_wifi"), testCheckResourceDestroy("/interface/wifi/network/radio", "routeros_interface_wifi_network_radio")),
				Steps: []resource.TestStep{
					{Config: config("tf-acc-wifi7-group", "yes", "0.1"), Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("routeros_wifi.mld", "mld_name", "tf-acc-wifi7-group"),
						resource.TestCheckResourceAttr("routeros_wifi_channel.channel", "afc", "false"),
						resource.TestCheckResourceAttr("routeros_wifi_security.security", "beacon_protection", "enabled"),
						resource.TestCheckResourceAttr("routeros_wifi_configuration.profile", "max_clients", "12"),
						resource.TestCheckResourceAttr("routeros_wifi_configuration.profile", "station_roaming", "false"),
						resource.TestCheckResourceAttr("routeros_wifi_provisioning.provision", "multi_link_mode", "all"),
						resource.TestCheckResourceAttr("routeros_interface_wifi_network_radio.radio", "channel_preamble_puncturing", "yes"),
						resource.TestCheckResourceAttr("data.routeros_interface_wifi_monitor.mld", "data.0.state", "disabled"),
					)},
					{Config: config("tf-acc-wifi7-group-next", "no", "1.2345"), Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("routeros_wifi.mld", "mld_name", "tf-acc-wifi7-group-next"),
						resource.TestCheckResourceAttr("routeros_interface_wifi_network_radio.radio", "configuration_distance", "1.234"),
					)},
					{Config: config("tf-acc-wifi7-group-next", "no", "1.234500"), PlanOnly: true, ExpectNonEmptyPlan: false},
					{ResourceName: "routeros_wifi.mld", ImportState: true, ImportStateVerify: true},
					{ResourceName: "routeros_wifi_channel.channel", ImportState: true, ImportStateVerify: true},
					{ResourceName: "routeros_wifi_security.security", ImportState: true, ImportStateVerify: true},
					{ResourceName: "routeros_wifi_configuration.profile", ImportState: true, ImportStateVerify: true},
					{ResourceName: "routeros_wifi_provisioning.provision", ImportState: true, ImportStateVerify: true},
					{ResourceName: "routeros_interface_wifi_network_radio.radio", ImportState: true, ImportStateVerify: true},
					{Config: strings.ReplaceAll(strings.ReplaceAll(config("tf-acc-wifi7-group-next", "", ""), `extra_labels = ["tf-wifi7-extra"]`, `extra_labels = []`), `supported_hw_caps = ["mlo"]`, `supported_hw_caps = []`), Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("routeros_interface_wifi_network_radio.radio", "configuration_distance", ""),
						resource.TestCheckResourceAttr("routeros_interface_wifi_network_radio.radio", "extra_labels.#", "0"),
						resource.TestCheckResourceAttr("routeros_wifi_provisioning.provision", "supported_hw_caps.#", "0"),
					)},
				},
			})
		})
	}
}

func TestAccWifi7CAP(t *testing.T) {
	testAccPreCheck(t)
	for _, transport := range testNames {
		t.Run(transport, func(t *testing.T) {
			config := func(value bool) string {
				return providerConfig + fmt.Sprintf(`
resource "routeros_wifi_datapath" "test" { name = "tf-acc-wifi7-datapath" }
resource "routeros_wifi_cap" "test" {
  enabled = false
  mld_static = %t
  mld_datapath = routeros_wifi_datapath.test.name
}
`, value)
			}
			clear := providerConfig + `
resource "routeros_wifi_datapath" "test" { name = "tf-acc-wifi7-datapath" }
resource "routeros_wifi_cap" "test" {
  enabled = false
  mld_static = false
  mld_datapath = ""
}
`
			resource.Test(t, resource.TestCase{PreCheck: func() { testSetTransportEnv(t, transport) }, ProviderFactories: testAccProviderFactories, Steps: []resource.TestStep{
				{Config: config(true), Check: resource.TestCheckResourceAttr("routeros_wifi_cap.test", "mld_static", "true")},
				{Config: config(false), Check: resource.TestCheckResourceAttr("routeros_wifi_cap.test", "mld_static", "false")},
				{ResourceName: "routeros_wifi_cap.test", ImportState: true, ImportStateVerify: true},
				{Config: clear},
			}})
		})
	}
}
