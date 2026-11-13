package routeros

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const testIPv6FirewallFilterAddress = "routeros_ipv6_firewall_filter.rule"

func TestAccIPv6FirewallFilterTest_basic(t *testing.T) {
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/ipv6/firewall/filter", "routeros_ipv6_firewall_filter"),
				Steps: []resource.TestStep{
					{
						Config: testAccIPv6FirewallFilterConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testIPv6FirewallFilterAddress),
							resource.TestCheckResourceAttr(testIPv6FirewallFilterAddress, "action", "drop"),
						),
					},
				},
			})

		})
	}
}

func TestAccIPv6FirewallFilter_placeBeforeUpdate(t *testing.T) {
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/ipv6/firewall/filter", "routeros_ipv6_firewall_filter"),
				Steps: []resource.TestStep{
					{
						Config: testAccIPv6FirewallFilterPlaceBeforeConfig("before-update"),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("routeros_ipv6_firewall_filter.positioned", "comment", "before-update"),
							testCheckIPv6FilterPrecedesAnchor,
						),
					},
					{
						Config: testAccIPv6FirewallFilterPlaceBeforeConfig("after-update"),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("routeros_ipv6_firewall_filter.positioned", "comment", "after-update"),
							testCheckIPv6FilterPrecedesAnchor,
						),
					},
				},
			})
		})
	}
}

func testCheckIPv6FilterPrecedesAnchor(s *terraform.State) error {
	positioned := s.RootModule().Resources["routeros_ipv6_firewall_filter.positioned"].Primary.ID
	anchor := s.RootModule().Resources["routeros_ipv6_firewall_filter.anchor"].Primary.ID
	items, err := ReadItems(nil, "/ipv6/firewall/filter", testAccProvider.Meta().(Client))
	if err != nil {
		return err
	}
	positionedIndex, anchorIndex := -1, -1
	for i, item := range *items {
		switch item.GetID(Id) {
		case positioned:
			positionedIndex = i
		case anchor:
			anchorIndex = i
		}
	}
	if positionedIndex < 0 || anchorIndex < 0 || positionedIndex >= anchorIndex {
		return fmt.Errorf("positioned rule index=%d must precede anchor index=%d", positionedIndex, anchorIndex)
	}
	return nil
}

func testAccIPv6FirewallFilterPlaceBeforeConfig(comment string) string {
	return providerConfig + fmt.Sprintf(`
resource "routeros_ipv6_firewall_filter" "anchor" {
  action  = "accept"
  chain   = "routeros-test-place-before"
  comment = "place-before-anchor"
}

resource "routeros_ipv6_firewall_filter" "positioned" {
  action       = "drop"
  chain        = "routeros-test-place-before"
  comment      = %q
  place_before = routeros_ipv6_firewall_filter.anchor.id
}
`, comment)
}

func testAccIPv6FirewallFilterConfig() string {
	return providerConfig + `

resource "routeros_ipv6_firewall_filter" "rule" {
	// add action=drop chain=forward comment="" hop-limit= protocol=icmpv6
	action 		= "drop"
	chain   	= "forward"
	comment 	= "Check drop hop-limit=1 + ipv6 multicast"
	src_address = "ff00::/8"
	hop_limit	= "equal:1"
	protocol 	= "icmpv6"
  }

`
}
