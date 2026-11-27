package routeros

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	sdkterraform "github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const testIpIpsecPolicyGroup = "routeros_ip_ipsec_policy_group.test"

func TestAccIpIpsecPolicyGroupTest_basic(t *testing.T) {
	// t.Parallel()
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/ip/ipsec/policy/group", "routeros_ip_ipsec_policy_group"),
				Steps: []resource.TestStep{
					{
						Config: testAccIpIpsecPolicyGroupConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testIpIpsecPolicyGroup),
							resource.TestCheckResourceAttr(testIpIpsecPolicyGroup, "name", "test-group"),
						),
					},
					{
						Config:        testAccIpIpsecPolicyGroupConfig(),
						ResourceName:  testIpIpsecPolicyGroup,
						ImportStateId: `name=test-group`,
						ImportState:   true,
						ImportStateCheck: func(states []*terraform.InstanceState) error {
							if len(states) != 1 {
								return fmt.Errorf("more than 1 states received, only one expected")
							}
							return nil
						},
					},
				},
			})

		})
	}
}

func testAccIpIpsecPolicyGroupConfig() string {
	return fmt.Sprintf(`%v

resource "routeros_ip_ipsec_policy_group" "test" {
  name    = "test-group"
}
`, providerConfig)
}

func TestResourceIpIpsecPolicyGroupInterfaceUnset(t *testing.T) {
	res := ResourceIpIpsecPolicyGroup()
	if res.Schema["interface"] == nil {
		t.Fatal("missing interface field")
	}
	if res.Schema["interface"].DiffSuppressFunc != nil {
		t.Fatal("interface must not have DiffSuppressFunc so removal triggers unset")
	}
	if res.Schema[MetaSetUnsetFields] == nil {
		t.Fatal("missing MetaSetUnsetFields")
	}

	t.Run("removing_interface_plans_and_unsets", func(t *testing.T) {
		r := ResourceIpIpsecPolicyGroup()
		_, attrs := blockTestNullRawConfig(r)
		attrs["name"] = cty.StringVal("test-group")
		state := &sdkterraform.InstanceState{
			ID: "*1",
			Attributes: map[string]string{
				"id":        "*1",
				"name":      "test-group",
				"interface": "ether1",
			},
			RawConfig: cty.ObjectVal(attrs),
		}
		cfg := sdkterraform.NewResourceConfigRaw(map[string]interface{}{
			"name": "test-group",
		})
		cfg.CtyValue = cty.ObjectVal(attrs)

		var item MikrotikItem
		r.UpdateContext = func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			item, _ = TerraformResourceDataToMikrotik(r.Schema, d)
			return nil
		}
		diff, err := r.Diff(context.Background(), state, cfg, nil)
		if err != nil {
			t.Fatalf("diff: %v", err)
		}
		if diff == nil || diff.Attributes["interface"] == nil {
			t.Fatalf("expected diff on interface when removed, got %#v", diff)
		}
		if _, diags := r.Apply(context.Background(), state, diff, nil); diags.HasError() {
			t.Fatalf("apply: %v", diags)
		}
		if got, ok := item["!interface"]; !ok || got != "" {
			t.Fatalf("expected !interface=\"\", got item=%#v", item)
		}
		if _, ok := item["interface"]; ok {
			t.Fatalf("did not expect interface in item=%#v", item)
		}
	})

	t.Run("unrelated_update_never_set_omits_interface", func(t *testing.T) {
		r := ResourceIpIpsecPolicyGroup()
		_, attrs := blockTestNullRawConfig(r)
		attrs["name"] = cty.StringVal("test-group")
		attrs["comment"] = cty.StringVal("new")
		state := &sdkterraform.InstanceState{
			ID: "*1",
			Attributes: map[string]string{
				"id":      "*1",
				"name":    "test-group",
				"comment": "old",
			},
			RawConfig: cty.ObjectVal(attrs),
		}
		cfg := sdkterraform.NewResourceConfigRaw(map[string]interface{}{
			"name":    "test-group",
			"comment": "new",
		})
		cfg.CtyValue = cty.ObjectVal(attrs)

		var item MikrotikItem
		r.UpdateContext = func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			item, _ = TerraformResourceDataToMikrotik(r.Schema, d)
			return nil
		}
		diff, err := r.Diff(context.Background(), state, cfg, nil)
		if err != nil {
			t.Fatalf("diff: %v", err)
		}
		if _, diags := r.Apply(context.Background(), state, diff, nil); diags.HasError() {
			t.Fatalf("apply: %v", diags)
		}
		if item["comment"] != "new" {
			t.Fatalf("expected comment=new, got item=%#v", item)
		}
		if _, ok := item["interface"]; ok {
			t.Fatalf("did not expect interface, got item=%#v", item)
		}
		if _, ok := item["!interface"]; ok {
			t.Fatalf("did not expect !interface, got item=%#v", item)
		}
	})
}
