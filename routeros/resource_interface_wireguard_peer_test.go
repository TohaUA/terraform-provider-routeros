package routeros

import (
	"context"
	"reflect"
	"regexp"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testInterfaceWireguardPeerMinVersion = "7.12"
const testInterfaceWireguardPeerAddress = "routeros_interface_wireguard_peer.wg_peer"

func TestAccInterfaceWireguardPeerTest_basic(t *testing.T) {
	if !testCheckMinVersion(t, testInterfaceWireguardPeerMinVersion) {
		t.Logf("Test skipped, the minimum required version is %v", testInterfaceWireguardPeerMinVersion)
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
				CheckDestroy:      testCheckResourceDestroy("/interface/wireguard/peers", "routeros_interface_wireguard_peer"),
				Steps: []resource.TestStep{
					{
						Config: testAccInterfaceWireguardPeerConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testInterfaceWireguardPeerAddress),
							resource.TestCheckResourceAttr(testInterfaceWireguardPeerAddress, "interface", "wg1"),
						),
					},
				},
			})

		})
	}
}

func testAccInterfaceWireguardPeerConfig() string {
	return providerConfig + `

resource "routeros_interface_wireguard_peer" "wg_peer" {
	allowed_address  = ["1.2.3.0/30"]
	interface        = "wg1"
	public_key       = "QxC+CTcrDdU5+ny0+2ChUH3NegTrwoVCv53TllI5T0I="
	client_keepalive = "85s"
  }

`
}

func Test_resourceInterfaceWireguardPeerAllowedIPRegexp(t *testing.T) {
	type args struct {
		ip string
	}
	tests := []struct {
		name string
		args
		want bool
	}{
		{
			"empty",
			args{""},
			true,
		}, {
			"10.0.0.1",
			args{"10.0.0.1"},
			true,
		},
		{
			"10.0.0.1/32",
			args{"10.0.0.1/32"},
			true,
		},
		{
			"10.0.0.1/33",
			args{"10.0.0.1/33"},
			false,
		},
	}

	re := regexp.MustCompile(`^$|^(\b(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)(/([1-2][0-9]|3[0-2]))?)$`)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := re.MatchString(tt.args.ip); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Test_resourceInterfaceWireguardPeerAllowedIPRegexp() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResourceInterfaceWireguardPeerClientMtuUnset(t *testing.T) {
	const sampleKey = "QxC+CTcrDdU5+ny0+2ChUH3NegTrwoVCv53TllI5T0I="

	t.Run("removing_client_mtu_plans_and_unsets", func(t *testing.T) {
		res := ResourceInterfaceWireguardPeer()
		_, attrs := blockTestNullRawConfig(res)
		attrs["interface"] = cty.StringVal("wg1")
		attrs["public_key"] = cty.StringVal(sampleKey)
		attrs["allowed_address"] = cty.ListVal([]cty.Value{cty.StringVal("192.0.2.0/30")})
		state := &terraform.InstanceState{
			ID: "*1",
			Attributes: map[string]string{
				"id":                "*1",
				"interface":         "wg1",
				"public_key":        sampleKey,
				"allowed_address.#": "1",
				"allowed_address.0": "192.0.2.0/30",
				"client_mtu":        "1380",
			},
			RawConfig: cty.ObjectVal(attrs),
		}
		cfg := terraform.NewResourceConfigRaw(map[string]interface{}{
			"interface":       "wg1",
			"public_key":      sampleKey,
			"allowed_address": []interface{}{"192.0.2.0/30"},
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
		if diff == nil || diff.Attributes["client_mtu"] == nil {
			t.Fatalf("expected diff on client_mtu when removed, got %#v", diff)
		}
		if _, diags := res.Apply(context.Background(), state, diff, nil); diags.HasError() {
			t.Fatalf("apply: %v", diags)
		}
		if got, ok := item["!client-mtu"]; !ok || got != "" {
			t.Fatalf("expected !client-mtu=\"\", got item=%#v", item)
		}
		if _, ok := item["client-mtu"]; ok {
			t.Fatalf("did not expect client-mtu in item=%#v", item)
		}
	})

	t.Run("explicit_zero_client_mtu_sends_zero", func(t *testing.T) {
		res := ResourceInterfaceWireguardPeer()
		_, attrs := blockTestNullRawConfig(res)
		attrs["interface"] = cty.StringVal("wg1")
		attrs["public_key"] = cty.StringVal(sampleKey)
		attrs["allowed_address"] = cty.ListVal([]cty.Value{cty.StringVal("192.0.2.0/30")})
		attrs["client_mtu"] = cty.NumberIntVal(0)
		state := &terraform.InstanceState{
			ID: "*1",
			Attributes: map[string]string{
				"id":                "*1",
				"interface":         "wg1",
				"public_key":        sampleKey,
				"allowed_address.#": "1",
				"allowed_address.0": "192.0.2.0/30",
				"client_mtu":        "1380",
			},
			RawConfig: cty.ObjectVal(attrs),
		}
		cfg := terraform.NewResourceConfigRaw(map[string]interface{}{
			"interface":       "wg1",
			"public_key":      sampleKey,
			"allowed_address": []interface{}{"192.0.2.0/30"},
			"client_mtu":      0,
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
		if diff == nil || diff.Attributes["client_mtu"] == nil {
			t.Fatalf("expected diff on client_mtu for explicit 0, got %#v", diff)
		}
		if _, diags := res.Apply(context.Background(), state, diff, nil); diags.HasError() {
			t.Fatalf("apply: %v", diags)
		}
		if got, ok := item["client-mtu"]; !ok || got != "0" {
			t.Fatalf("expected client-mtu=\"0\", got item=%#v", item)
		}
		if _, ok := item["!client-mtu"]; ok {
			t.Fatalf("did not expect !client-mtu when explicit 0, got item=%#v", item)
		}
	})

	t.Run("unrelated_update_never_set_omits_client_mtu", func(t *testing.T) {
		res := ResourceInterfaceWireguardPeer()
		_, attrs := blockTestNullRawConfig(res)
		attrs["interface"] = cty.StringVal("wg1")
		attrs["public_key"] = cty.StringVal(sampleKey)
		attrs["allowed_address"] = cty.ListVal([]cty.Value{cty.StringVal("192.0.2.0/30")})
		attrs["comment"] = cty.StringVal("new")
		state := &terraform.InstanceState{
			ID: "*1",
			Attributes: map[string]string{
				"id":                "*1",
				"interface":         "wg1",
				"public_key":        sampleKey,
				"allowed_address.#": "1",
				"allowed_address.0": "192.0.2.0/30",
				"comment":           "old",
			},
			RawConfig: cty.ObjectVal(attrs),
		}
		cfg := terraform.NewResourceConfigRaw(map[string]interface{}{
			"interface":       "wg1",
			"public_key":      sampleKey,
			"allowed_address": []interface{}{"192.0.2.0/30"},
			"comment":         "new",
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
		if _, ok := item["client-mtu"]; ok {
			t.Fatalf("did not expect client-mtu, got item=%#v", item)
		}
		if _, ok := item["!client-mtu"]; ok {
			t.Fatalf("did not expect !client-mtu, got item=%#v", item)
		}
	})
}
