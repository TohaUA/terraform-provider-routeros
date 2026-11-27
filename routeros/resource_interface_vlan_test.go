package routeros

import (
	"context"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testVlanAddress = "routeros_interface_vlan.vlan900"
const testVlanName = "VLAN_900_TEST"

func TestAccInterfaceVlanTest(t *testing.T) {
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/interface/vlan", "routeros_interface_vlan"),
				Steps: []resource.TestStep{
					{
						Config: testAccInterfaceVlanConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testVlanAddress),
							resource.TestCheckResourceAttr(testVlanAddress, "name", testVlanName),
						),
					},
				},
			})

		})
	}
}

func testAccInterfaceVlanConfig() string {
	return providerConfig + `

resource "routeros_interface_vlan" "vlan900" {
	name      = "VLAN_900_TEST"
	vlan_id   = 900
	disabled  = true
	interface = "bridge"
}
`
}

func TestResourceInterfaceVlanForcedMacUnsetAndDiffSuppress(t *testing.T) {
	t.Run("removing_forced_mac_address_plans_and_unsets", func(t *testing.T) {
		res := ResourceInterfaceVlan()
		_, attrs := blockTestNullRawConfig(res)
		attrs["name"] = cty.StringVal("vlan725")
		attrs["interface"] = cty.StringVal("bridge")
		attrs["vlan_id"] = cty.NumberIntVal(725)
		state := &terraform.InstanceState{
			ID: "*1",
			Attributes: map[string]string{
				"id":                 "*1",
				"name":               "vlan725",
				"interface":          "bridge",
				"vlan_id":            "725",
				"forced_mac_address": "02:00:00:00:07:25",
			},
			RawConfig: cty.ObjectVal(attrs),
		}
		cfg := terraform.NewResourceConfigRaw(map[string]interface{}{
			"name":      "vlan725",
			"interface": "bridge",
			"vlan_id":   725,
		})
		cfg.CtyValue = cty.ObjectVal(attrs)

		var item MikrotikItem
		res.UpdateContext = func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			item, _ = TerraformResourceDataToMikrotik(res.Schema, d)
			return nil
		}
		diff, err := res.Diff(context.Background(), state, cfg, nil)
		if err != nil {
			t.Fatalf("diff: %v", err)
		}
		if diff == nil || diff.Attributes["forced_mac_address"] == nil {
			t.Fatalf("expected diff on forced_mac_address when removed, got %#v", diff)
		}
		if _, diags := res.Apply(context.Background(), state, diff, nil); diags.HasError() {
			t.Fatalf("apply: %v", diags)
		}
		if got, ok := item["!forced-mac-address"]; !ok || got != "" {
			t.Fatalf("expected !forced-mac-address=\"\", got item=%#v", item)
		}
		if _, ok := item["forced-mac-address"]; ok {
			t.Fatalf("did not expect forced-mac-address in item=%#v", item)
		}
	})

	t.Run("case_only_mac_diff_suppressed", func(t *testing.T) {
		res := ResourceInterfaceVlan()
		state := &terraform.InstanceState{
			ID: "*1",
			Attributes: map[string]string{
				"id":                 "*1",
				"name":               "vlan725",
				"interface":          "bridge",
				"vlan_id":            "725",
				"forced_mac_address": "02:aa:bb:cc:dd:ee",
			},
		}
		_, attrs := blockTestNullRawConfig(res)
		attrs["name"] = cty.StringVal("vlan725")
		attrs["interface"] = cty.StringVal("bridge")
		attrs["vlan_id"] = cty.NumberIntVal(725)
		attrs["forced_mac_address"] = cty.StringVal("02:AA:BB:CC:DD:EE")
		cfg := terraform.NewResourceConfigRaw(map[string]interface{}{
			"name":               "vlan725",
			"interface":          "bridge",
			"vlan_id":            725,
			"forced_mac_address": "02:AA:BB:CC:DD:EE",
		})
		cfg.CtyValue = cty.ObjectVal(attrs)

		diff, err := res.Diff(context.Background(), state, cfg, nil)
		if err != nil {
			t.Fatalf("diff: %v", err)
		}
		if diff != nil && !diff.Empty() {
			t.Fatalf("expected empty diff for case-only MAC difference, got %#v", diff.Attributes)
		}
	})

	t.Run("unrelated_update_never_set_omits_forced_mac", func(t *testing.T) {
		res := ResourceInterfaceVlan()
		_, attrs := blockTestNullRawConfig(res)
		attrs["name"] = cty.StringVal("vlan725")
		attrs["interface"] = cty.StringVal("bridge")
		attrs["vlan_id"] = cty.NumberIntVal(725)
		attrs["comment"] = cty.StringVal("new")
		state := &terraform.InstanceState{
			ID: "*1",
			Attributes: map[string]string{
				"id":        "*1",
				"name":      "vlan725",
				"interface": "bridge",
				"vlan_id":   "725",
				"comment":   "old",
			},
			RawConfig: cty.ObjectVal(attrs),
		}
		cfg := terraform.NewResourceConfigRaw(map[string]interface{}{
			"name":      "vlan725",
			"interface": "bridge",
			"vlan_id":   725,
			"comment":   "new",
		})
		cfg.CtyValue = cty.ObjectVal(attrs)

		var item MikrotikItem
		res.UpdateContext = func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			item, _ = TerraformResourceDataToMikrotik(res.Schema, d)
			return nil
		}
		diff, err := res.Diff(context.Background(), state, cfg, nil)
		if err != nil {
			t.Fatalf("diff: %v", err)
		}
		if _, diags := res.Apply(context.Background(), state, diff, nil); diags.HasError() {
			t.Fatalf("apply: %v", diags)
		}
		if item["comment"] != "new" {
			t.Fatalf("expected comment=new, got item=%#v", item)
		}
		if _, ok := item["forced-mac-address"]; ok {
			t.Fatalf("did not expect forced-mac-address, got item=%#v", item)
		}
		if _, ok := item["!forced-mac-address"]; ok {
			t.Fatalf("did not expect !forced-mac-address, got item=%#v", item)
		}
	})
}
