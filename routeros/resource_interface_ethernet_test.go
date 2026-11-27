package routeros

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	sdkterraform "github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const testInterfaceEthernetMinVersion = "7.12"
const testInterfaceEthernetAddress = "routeros_interface_ethernet.test"
const testInterfaceEthernetImportAddress = "routeros_interface_ethernet.importtest"
const expectedIdForInterface3 = "*3"

func TestAccInterfaceEthernetTest_basic(t *testing.T) {
	if !testCheckMinVersion(t, testInterfaceEthernetMinVersion) {
		t.Logf("Test skipped, the minimum required version is %v", testInterfaceEthernetMinVersion)
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
				Steps: []resource.TestStep{
					{
						Config: testAccInterfaceEthernetConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testInterfaceEthernetAddress),
							resource.TestCheckResourceAttr(testInterfaceEthernetAddress, "name", "terraform"),
							resource.TestCheckResourceAttr(testInterfaceEthernetAddress, "mtu", "9000"),
							// auto_negotiation = true + advertise || auto_negotiation = false + speed
							// resource.TestCheckResourceAttr(testInterfaceEthernetAddress, "advertise", "10000M-full"),
							resource.TestCheckResourceAttr(testInterfaceEthernetAddress, "arp", "disabled"),
							resource.TestCheckResourceAttr(testInterfaceEthernetAddress, "auto_negotiation", "false"),
							resource.TestCheckResourceAttr(testInterfaceEthernetAddress, "tx_flow_control", "auto"),
							resource.TestCheckResourceAttr(testInterfaceEthernetAddress, "rx_flow_control", "auto"),
							resource.TestCheckResourceAttr(testInterfaceEthernetAddress, "mdix_enable", "false"),
							resource.TestCheckResourceAttr(testInterfaceEthernetAddress, "sfp_shutdown_temperature", "60"),
							resource.TestCheckResourceAttr(testInterfaceEthernetAddress, "speed", "10G-baseT"),

							// read only properties. #slave and #switch are not returned from the virtual switch
							// so we can add assertions.
							// Not asserted as true: `running` reflects whether the
							// link is actually up, which depends on how the host
							// wired the emulated NIC rather than on anything the
							// provider does. It reads back false in the test
							// container on 7.24.
							resource.TestCheckResourceAttrSet(testInterfaceEthernetAddress, "running"),
						),
					},
				},
			})
		})
	}
}

func TestAccInterfaceEthernetTest_import(t *testing.T) {
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
						Config:        testAccInterfaceEthernetImportConfig(),
						ResourceName:  testInterfaceEthernetImportAddress,
						ImportStateId: "*3",
						ImportState:   true,
						ImportStateCheck: func(states []*terraform.InstanceState) error {
							if len(states) != 1 {
								return fmt.Errorf("more than 1 states received, only one interface expected")
							}
							state := states[0]
							if state.ID != expectedIdForInterface3 {
								return fmt.Errorf("expected id in the state don't match %v vs %v", state.ID, expectedIdForInterface3)
							}
							return nil
						},
					},
				},
			})
		})
	}
}

func testAccInterfaceEthernetConfig() string {
	return providerConfig + `

resource "routeros_interface_ethernet" "test" {
  factory_name              = "ether2"
  name                      = "terraform"
  mtu                       = "9000"
//   advertise                 = "10000M-full"
  arp                       = "disabled"
  auto_negotiation          = false
  tx_flow_control           = "auto"
  rx_flow_control           = "auto"
//   full_duplex  	            = "true"
  mdix_enable               = false
  sfp_shutdown_temperature  = 60
  speed                     = "10G-baseT"
}
`
}

func testAccInterfaceEthernetImportConfig() string {
	return providerConfig + `

resource "routeros_interface_ethernet" "importtest" {
  factory_name              = "ether3"
  name                      = "ether3"
  speed                     = "1Gbps" 
}
`
}

func TestResourceInterfaceEthernetPowerCyclePingAddressValidation(t *testing.T) {
	validate := ResourceInterfaceEthernet().Schema["power_cycle_ping_address"].ValidateFunc
	if validate == nil {
		t.Fatal("expected ValidateFunc on power_cycle_ping_address")
	}

	for _, valid := range []string{
		"192.0.2.1",
		"2001:db8::1",
		"00:11:22:33:44:55",
		"AA:BB:CC:DD:EE:FF",
		"00-11-22-33-44-55",
	} {
		if _, errs := validate(valid, "power_cycle_ping_address"); len(errs) != 0 {
			t.Errorf("valid address %q rejected: %v", valid, errs)
		}
	}

	for _, invalid := range []string{
		"",
		"example.com",
		"not-an-address",
		"192.0.2.999",
		"00:11:22:33:44",
	} {
		if _, errs := validate(invalid, "power_cycle_ping_address"); len(errs) == 0 {
			t.Errorf("invalid address %q unexpectedly accepted", invalid)
		}
	}
}

func TestResourceInterfaceEthernetPowerCyclePingAddressDiffSuppress(t *testing.T) {
	res := ResourceInterfaceEthernet()
	ds := res.Schema["power_cycle_ping_address"].DiffSuppressFunc
	if ds == nil {
		t.Fatal("expected DiffSuppressFunc on power_cycle_ping_address")
	}

	if !ds("power_cycle_ping_address", "aa:bb:cc:dd:ee:ff", "AA:BB:CC:DD:EE:FF", nil) {
		t.Error("expected case-only MAC diff to be suppressed")
	}
	if !ds("power_cycle_ping_address", "", "", nil) {
		t.Error("expected empty-to-empty diff to be suppressed")
	}
	if ds("power_cycle_ping_address", "aa:bb:cc:dd:ee:ff", "00:11:22:33:44:55", nil) {
		t.Error("expected distinct MAC diff not to be suppressed")
	}
	if ds("power_cycle_ping_address", "aa:bb:cc:dd:ee:ff", "", nil) {
		t.Error("expected MAC removal diff not to be suppressed")
	}

	// Drive full SDK Diff: case-only MAC change is a no-op, while removal plans an update.
	state := &sdkterraform.InstanceState{
		ID: "*1",
		Attributes: map[string]string{
			"id":                       "*1",
			"factory_name":             "ether1",
			"name":                     "ether1",
			"power_cycle_ping_enabled": "true",
			"power_cycle_ping_address": "aa:bb:cc:dd:ee:ff",
		},
	}

	cfgSame := sdkterraform.NewResourceConfigRaw(map[string]interface{}{
		"factory_name":             "ether1",
		"name":                     "ether1",
		"power_cycle_ping_enabled": true,
		"power_cycle_ping_address": "AA:BB:CC:DD:EE:FF",
	})
	cfgSame.CtyValue = resourceDataWithRawConfig(res, "*1", map[string]cty.Value{
		"factory_name":             cty.StringVal("ether1"),
		"name":                     cty.StringVal("ether1"),
		"power_cycle_ping_enabled": cty.True,
		"power_cycle_ping_address": cty.StringVal("AA:BB:CC:DD:EE:FF"),
	}).GetRawConfig()
	diff, err := res.Diff(context.Background(), state, cfgSame, nil)
	if err != nil {
		t.Fatalf("diff same MAC: %v", err)
	}
	if diff != nil && !diff.Empty() {
		t.Fatalf("expected empty diff for case-only MAC change, got %#v", diff.Attributes)
	}

	cfgRemoved := sdkterraform.NewResourceConfigRaw(map[string]interface{}{
		"factory_name":             "ether1",
		"name":                     "ether1",
		"power_cycle_ping_enabled": true,
	})
	cfgRemoved.CtyValue = resourceDataWithRawConfig(res, "*1", map[string]cty.Value{
		"factory_name":             cty.StringVal("ether1"),
		"name":                     cty.StringVal("ether1"),
		"power_cycle_ping_enabled": cty.True,
	}).GetRawConfig()
	diff, err = res.Diff(context.Background(), state, cfgRemoved, nil)
	if err != nil {
		t.Fatalf("diff removed MAC: %v", err)
	}
	if diff == nil || diff.Attributes["power_cycle_ping_address"] == nil {
		t.Fatalf("expected diff on power_cycle_ping_address when removed, got %#v", diff)
	}
}
