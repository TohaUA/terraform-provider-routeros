package routeros

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testBGPConnectionMinVersion = "7.12"
const testBGPConnectionAddress = "routeros_routing_bgp_connection.test"

const testBGPConnectionInput = `
	input {
		accept_communities        = "111"
		accept_ext_communities    = "222"
		accept_large_communities  = "444"
		accept_nlri               = ""
		affinity                  = "alone"
		allow_as                  = "0"
		filter                    = ""
		limit_process_routes_ipv4 = 5
		limit_process_routes_ipv6 = 2
	}`

func TestAccBGPConnectionTest_basic(t *testing.T) {
	if !testCheckMinVersion(t, testBGPConnectionMinVersion) {
		t.Logf("Test skipped, the minimum required version is %v", testBGPConnectionMinVersion)
		return
	}

	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/routing/bgp/connection", "routeros_routing_bgp_connection"),
				Steps: []resource.TestStep{
					{
						Config: testAccBGPConnectionConfig(testBGPConnectionInput),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testBGPConnectionAddress),
							resource.TestCheckResourceAttr(testBGPConnectionAddress, "name", "neighbor-test"),
						),
					},
					{
						// Removing the `input` block must unset its properties on the router: RouterOS keeps
						// whatever a `set` leaves out, so the post-apply plan is only empty when Read no longer
						// reports any `input.*` property.
						Config: testAccBGPConnectionConfig(""),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testBGPConnectionAddress),
							resource.TestCheckResourceAttr(testBGPConnectionAddress, "input.#", "0"),
						),
					},
				},
			})
		})
	}
}

func testAccBGPConnectionConfig(input string) string {
	return fmt.Sprintf(`%v
resource "routeros_routing_bgp_instance" "test" {
	as   = "65550"
	name = "bgp-instance-conn"
}

resource "routeros_routing_bgp_connection" "test" {
	instance                = resource.routeros_routing_bgp_instance.test.name
	address_families        = "ip"
	as                      = "65550"
	cisco_vpls_nlri_len_fmt = "auto-bits"
	connect                 = true
	hold_time               = "infinity"%v
	keepalive_time = "4m"
	listen         = true
	local {
		address = "127.0.0.1"
		port    = 22334
		role    = "ebgp"
		ttl     = "5"
	}
	multihop       = true
	name           = "neighbor-test"
	nexthop_choice = "default"
	output {
		affinity                       = "alone"
		as_override                    = true
		default_originate              = "never"
		default_prepend                = "1"
		filter_chain                   = ""
		filter_select                  = ""
		keep_sent_attributes           = true
		network                        = ""
		no_client_to_client_reflection = true
		no_early_cut                   = true
		redistribute                   = "rip,bgp"
		remove_private_as              = true
	}
	remote {
		address    = "172.17.0.1"
		allowed_as = "1111"
		as         = "12345"
		port       = "11223"
		ttl        = "5"
	}
	routing_table = "main"
	save_to       = "bgp.dump"
	tcp_md5_key   = "poipoipoipoipoi"
	templates     = []
	use_bfd       = "true"
	vrf           = "main"
}
`, providerConfig, input)
}
