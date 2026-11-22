package routeros

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"golang.org/x/crypto/ssh"
)

// Portable configuration resources are exercised on CHR. Hardware-specific
// menus have independent schema/wire tests and need their corresponding device.
func TestAccRouterOS725Resources(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("requires a disposable RouterOS instance")
	}
	if !testCheckMinVersion(t, "7.25.1") {
		t.Skip("requires RouterOS 7.25.1+")
	}
	private := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	public, err := ssh.NewPublicKey(private.Public())
	if err != nil {
		t.Fatal(err)
	}
	key := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(public)))
	for _, tc := range []struct{ name, kind, path, body, checkField, checkValue string }{
		{"eoipv6", "routeros_interface_eoipv6", "/interface/eoipv6", `
  name = "tf-725-eoipv6"
  remote_address = "2001:db8::2"
  tunnel_id = 725
  arp_accept = false
  disabled = true
`, "arp_accept", "false"},
		{"xfrm", "routeros_interface_xfrm", "/interface/xfrm", `
  name = "tf-725-xfrm"
  mtu = 1400
  disabled = true
`, "mtu", "1400"},
		{"pim_instance", "routeros_routing_pimsm_instance", "/routing/pimsm/instance", `
  name = "tf-725-pim"
  vrf = "main"
  afi = "ipv6"
  disabled = true
`, "afi", "ipv6"},
		{"binding", "routeros_ipv6_dhcp_server_binding", "/ipv6/dhcp-server/binding", `
  address = "2001:db8::725/128"
  duid = "0x00030001aabbccddee01"
  iaid = 725
  server = "all"
  disabled = true
`, "iaid", "725"},
		{"known_host", "routeros_ip_ssh_known_hosts", "/ip/ssh/known-hosts", fmt.Sprintf(`
  host = "192.0.2.1"
  key = %q
`, key), "host", "192.0.2.1"},
		{"wifi_network", "routeros_interface_wifi_network", "/interface/wifi/network", `
  ssid = "tf-725-network"
  mode = "ap"
  security_authentication_types = ["wpa2-psk"]
  security_passphrase = "testpass725"
  interworking_domain_names = ["example.test"]
  interworking_internet = true
  steering_rrm = true
  aaa_nas_identifier = "tf-725"
`, "ssid", "tf-725-network"},
		{"ipsec_key", "routeros_ip_ipsec_key", "/ip/ipsec/key/rsa", `
  name = "tf-725-key"
  key_size = 2048
`, "key_size", "2048"},
		{"port", "routeros_port", "/port", `
  name = "serial0"
`, "baud_rate", "115200"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, transport := range testNames {
				t.Run(transport, func(t *testing.T) {
					address := tc.kind + ".test725"
					config := func(comment string) string {
						if tc.name == "ipsec_key" {
							return providerConfig + fmt.Sprintf("\nresource %q \"test725\" {\n%s\n}\n", tc.kind, tc.body)
						}
						if tc.name == "pim_instance" {
							return providerConfig + fmt.Sprintf("\nresource %q \"test725\" {\n%s\nswitch_to_spt = %t\n}\n", tc.kind, tc.body, comment == "create")
						}
						if tc.name == "port" {
							baud := "115200"
							if comment == "update" {
								baud = "57600"
							}
							return providerConfig + fmt.Sprintf("\nresource %q \"test725\" {\n%s\nbaud_rate = %q\n}\n", tc.kind, tc.body, baud)
						}
						return providerConfig + fmt.Sprintf("\nresource %q \"test725\" {\n%s\ncomment = %q\n}\n", tc.kind, tc.body, comment)
					}
					updateField, updateValue := "comment", "update"
					if tc.name == "ipsec_key" {
						updateField, updateValue = "key_size", "2048"
					}
					if tc.name == "pim_instance" {
						updateField, updateValue = "switch_to_spt", "false"
					}
					if tc.name == "port" {
						updateField, updateValue = "baud_rate", "57600"
					}
					checkDestroy := testCheckResourceDestroy(tc.path, tc.kind)
					if tc.name == "port" {
						checkDestroy = nil
					}
					resource.Test(t, resource.TestCase{
						PreCheck: func() { testAccPreCheck(t); testSetTransportEnv(t, transport) }, ProviderFactories: testAccProviderFactories,
						CheckDestroy: checkDestroy,
						Steps: []resource.TestStep{
							{Config: config("create"), Check: resource.TestCheckResourceAttr(address, tc.checkField, tc.checkValue)},
							{Config: config("update"), Check: resource.TestCheckResourceAttr(address, updateField, updateValue)},
							{ResourceName: address, ImportState: true, ImportStateVerify: true},
						},
					})
				})
			}
		})
	}
}

func TestAccRouterOS725PimTemplates(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("requires a disposable RouterOS instance")
	}
	if !testCheckMinVersion(t, "7.25.1") {
		t.Skip("requires RouterOS 7.25.1+")
	}
	for _, transport := range testNames {
		t.Run(transport, func(t *testing.T) {
			config := providerConfig + `
resource "routeros_routing_pimsm_instance" "test725" {
  name = "tf-725-pim-templates"
  vrf = "main"
  afi = "ipv6"
  disabled = true
}
resource "routeros_routing_pimsm_interface_template" "test725" {
  instance = routeros_routing_pimsm_instance.test725.name
  interfaces = ["ether4"]
  disabled = true
}
resource "routeros_routing_pimsm_igmp_interface_template" "test725" {
  instance = routeros_routing_pimsm_instance.test725.name
  interfaces = ["ether4"]
  robustness = 3
  query_interval = "2m"
  query_response_interval = "10s"
  last_member_query_interval = "1s"
  disabled = true
}
resource "routeros_routing_pimsm_static_rp" "test725" {
  instance = routeros_routing_pimsm_instance.test725.name
  group = "ff3e::/16"
  address = "2001:db8::1"
  disabled = true
}
data "routeros_routing_pimsm_interface" "test725" {}
data "routeros_routing_pimsm_neighbor" "test725" {}
data "routeros_routing_pimsm_uib_g" "test725" {}
`
			resource.Test(t, resource.TestCase{
				PreCheck: func() { testAccPreCheck(t); testSetTransportEnv(t, transport) }, ProviderFactories: testAccProviderFactories,
				CheckDestroy: testCheckResourceDestroy("/routing/pimsm/instance", "routeros_routing_pimsm_instance"),
				Steps:        []resource.TestStep{{Config: config, Check: resource.TestCheckResourceAttr("routeros_routing_pimsm_igmp_interface_template.test725", "robustness", "3")}},
			})
		})
	}
}

func TestAccRouterOS725FetchAction(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("requires a disposable RouterOS instance")
	}
	if !testCheckMinVersion(t, "7.25.1") {
		t.Skip("requires RouterOS 7.25.1+")
	}
	for _, transport := range testNames {
		t.Run(transport, func(t *testing.T) {
			// Fetch only this disposable router's identity through its own REST API.
			// No external service or production router is contacted.
			config := providerConfig + `
resource "routeros_tool_fetch" "test725" {
  url = "https://127.0.0.1/rest/system/identity"
  user = "admin"
  password = ""
  check_certificate = "no"
  output = "user"
}

`
			resource.Test(t, resource.TestCase{
				PreCheck: func() { testAccPreCheck(t); testSetTransportEnv(t, transport) }, ProviderFactories: testAccProviderFactories,
				Steps: []resource.TestStep{{Config: config, Check: resource.TestCheckResourceAttr("routeros_tool_fetch.test725", "status", "finished")}},
			})
		})
	}
}

func TestAccRouterOS725BridgeSecurity(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("requires a disposable RouterOS instance")
	}
	if !testCheckMinVersion(t, "7.25.1") {
		t.Skip("requires RouterOS 7.25.1+")
	}
	for _, transport := range testNames {
		t.Run(transport, func(t *testing.T) {
			config := providerConfig + `
resource "routeros_interface_bridge" "security725" {
  name = "tf-725-security"
  dhcp_snooping = true
  arp_inspection = true
  ip_source_guard = true
  mlag_init_delay = "1m"
  mlag_lacp_fallback = false
}
resource "routeros_interface_bridge_port" "security725" {
  bridge = routeros_interface_bridge.security725.name
  interface = "ether4"
  trusted_arp = true
  disabled = true
}
`
			resource.Test(t, resource.TestCase{
				PreCheck: func() { testAccPreCheck(t); testSetTransportEnv(t, transport) }, ProviderFactories: testAccProviderFactories,
				CheckDestroy: testCheckResourceDestroy("/interface/bridge", "routeros_interface_bridge"),
				Steps: []resource.TestStep{{Config: config, Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("routeros_interface_bridge.security725", "arp_inspection", "true"),
					resource.TestCheckResourceAttr("routeros_interface_bridge_port.security725", "trusted_arp", "true"),
				)}},
			})
		})
	}
}

func TestAccRouterOS725Settings(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("requires a disposable RouterOS instance")
	}
	if !testCheckMinVersion(t, "7.25.1") {
		t.Skip("requires RouterOS 7.25.1+")
	}
	for _, transport := range testNames {
		t.Run(transport, func(t *testing.T) {
			config := func(robustness int) string {
				return providerConfig + fmt.Sprintf(`
resource "routeros_interface_lte_settings" "test725" {
  mode = "auto"
  esim_channel = "auto"
  link_recovery_timer = 120
}
resource "routeros_ip_tftp_settings" "test725" {
  vrf = "main"
  max_block_size = 4096
}
resource "routeros_routing_igmp_proxy" "test725" {
  robustness = %d
  query_interval = "2m5s"
  query_response_interval = "10s"
  last_member_query_interval = "1s"
}
data "routeros_system_script_environment" "test725" {}
data "routeros_system_resource_hardware" "test725" {}
data "routeros_interface_wifi_registration_table" "test725" {}
`, robustness)
			}
			resource.Test(t, resource.TestCase{
				PreCheck: func() { testAccPreCheck(t); testSetTransportEnv(t, transport) }, ProviderFactories: testAccProviderFactories,
				Steps: []resource.TestStep{
					{Config: config(3), Check: resource.TestCheckResourceAttr("routeros_routing_igmp_proxy.test725", "robustness", "3")},
					{Config: config(2), Check: resource.TestCheckResourceAttr("routeros_routing_igmp_proxy.test725", "robustness", "2")},
					{ResourceName: "routeros_routing_igmp_proxy.test725", ImportState: true, ImportStateVerify: true},
					{ResourceName: "routeros_ip_tftp_settings.test725", ImportState: true, ImportStateVerify: true},
				},
			})
		})
	}
}
