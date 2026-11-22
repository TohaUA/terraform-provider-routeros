package routeros

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testOpenVPNServer = "routeros_ovpn_server.server"
const testInterfaceOpenVPNServer = "routeros_interface_ovpn_server.user1"

func TestAccOpenVPNServerTest_basic(t *testing.T) {
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy: resource.ComposeTestCheckFunc(
					testCheckResourceDestroy("/interface/ovpn-server/server", "routeros_ovpn_server"),
					testCheckResourceDestroy("/interface/ovpn-server", "routeros_interface_ovpn_server"),
				),
				Steps: []resource.TestStep{
					{
						Config: testAccOpenVPNServerConfig(1194),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testOpenVPNServer),
							testResourcePrimaryInstanceId(testInterfaceOpenVPNServer),
							resource.TestCheckResourceAttr(testOpenVPNServer, "name", "tf-ovpn-server"),
							resource.TestCheckResourceAttr(testOpenVPNServer, "disabled", "true"),
							resource.TestCheckResourceAttr(testInterfaceOpenVPNServer, "name", "ovpn-in1"),
							resource.TestCheckResourceAttr(testInterfaceOpenVPNServer, "user", "user1"),
						),
					},
					{Config: testAccOpenVPNServerConfig(1195), Check: resource.TestCheckResourceAttr(testOpenVPNServer, "port", "1195")},
					{ResourceName: testOpenVPNServer, ImportState: true, ImportStateVerify: true},
				},
			})

		})
	}
}

// Complex test for OpenVPN server resources.
func testAccOpenVPNServerConfig(port int) string {
	return providerConfig + fmt.Sprintf(`
	resource "routeros_system_certificate" "ovpn_ca" {
		name        = "OpenVPN-Root-CA"
		common_name = "OpenVPN Root CA"
		key_size    = "prime256v1"
		key_usage   = ["key-cert-sign", "crl-sign"]
		trusted     = true
		sign {
		}
	  }
	  
	  resource "routeros_system_certificate" "ovpn_server_crt" {
		name        = "OpenVPN-Server-Certificate"
		common_name = "Mikrotik OpenVPN"
		key_size    = "prime256v1"
		key_usage   = ["digital-signature", "key-encipherment", "tls-server"]
		sign {
		  ca = routeros_system_certificate.ovpn_ca.name
		}
	  }
	  
	  resource "routeros_ovpn_server" "server" {
		name             = "tf-ovpn-server"
        disabled         = true
        port             = %d
		certificate      = routeros_system_certificate.ovpn_server_crt.name
		auth             = ["sha256", "sha512"]
		redirect_gateway = ["def1", "ipv6"]
		tls_version      = "only-1.2"
	  }
	  
	  # Create the server instance before its static interfaces.
      resource "routeros_interface_ovpn_server" "user1" {
        name       = "ovpn-in1"
        user       = "user1"
        depends_on = [routeros_ovpn_server.server]
      }
`, port)
}
