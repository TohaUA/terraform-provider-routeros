package routeros

import (
	"reflect"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// Exercise the 7.25.1 serializer in both directions. Existing Terraform
// configuration and state retain their attribute names.
func TestRouterOS725PropertyRenames(t *testing.T) {
	previous := RouterOSVersion
	t.Cleanup(func() { RouterOSVersion = previous })
	for _, tc := range []struct {
		name, field, legacy, current string
		resource                     func() *schema.Resource
	}{
		{"bridge", "last_member_interval", "last-member-interval", "last-member-query-interval", ResourceInterfaceBridge},
		{"dot1x", "reauth_timeout", "reauth-timeout", "reauth-period", ResourceInterfaceDot1xServer},
	} {
		for _, version := range []string{"7.25.1"} {
			t.Run(tc.name+"/"+version, func(t *testing.T) {
				RouterOSVersion = version
				key, other := tc.current, tc.legacy
				res := tc.resource()
				d := resourceDataWithRawConfig(res, "*1", map[string]cty.Value{tc.field: cty.StringVal("5s")})
				item, _ := TerraformResourceDataToMikrotik(res.Schema, d)
				if item[key] != "5s" {
					t.Fatalf("write %s = %q, want 5s (payload: %v)", key, item[key], item)
				}
				if _, present := item[other]; present {
					t.Fatalf("write includes obsolete key %s: %v", other, item)
				}
				read := res.TestResourceData()
				if diags := MikrotikResourceDataToTerraform(MikrotikItem{key: "10s"}, res.Schema, read); len(diags) != 0 {
					t.Fatalf("read: %v", diags)
				}
				if got := read.Get(tc.field); got != "10s" {
					t.Fatalf("read %s = %v, want 10s", tc.field, got)
				}
			})
		}
	}
}

func TestRouterOS725OptionalFields(t *testing.T) {
	previous := RouterOSVersion
	t.Cleanup(func() { RouterOSVersion = previous })
	for _, tc := range []struct {
		name, field, wire string
		resource          func() *schema.Resource
		value             cty.Value
		state             interface{}
	}{
		{"bgp", "always_compare_med", "yes", ResourceRoutingBgpInstance, cty.True, true},
		{"dhcp", "ipv6_only_preferred", "yes", ResourceDhcpServer, cty.True, true},
		{"dhcpv6", "add_dns_entries", "yes", ResourceIpv6DhcpServer, cty.True, true},
		{"dhcpv6_suffix", "add_dns_entries_suffix", "example.test", ResourceIpv6DhcpServer, cty.StringVal("example.test"), "example.test"},
		{"vlan", "forced_mac_address", "02:00:00:00:07:25", ResourceInterfaceVlan, cty.StringVal("02:00:00:00:07:25"), "02:00:00:00:07:25"},
		{"wireguard", "client_mtu", "1380", ResourceInterfaceWireguardPeer, cty.NumberIntVal(1380), 1380},
		{"tftp", "vrf", "test-vrf", ResourceIpTFTPSettings, cty.StringVal("test-vrf"), "test-vrf"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := tc.resource()
			field := res.Schema[tc.field]
			if field == nil {
				t.Fatalf("missing %s", tc.field)
			}
			if !field.Optional || field.Default != nil {
				t.Fatalf("%s must be optional without an injected default", tc.field)
			}
			// Omitted options must not overwrite device defaults on ordinary writes.
			RouterOSVersion = "7.25.1"
			unset := resourceDataWithRawConfig(res, "*1", nil)
			item, _ := TerraformResourceDataToMikrotik(res.Schema, unset)
			key := SnakeToKebab(tc.field)
			if _, present := item[key]; present {
				t.Fatalf("unset %s sent without configuration: %v", key, item)
			}
			RouterOSVersion = "7.25.1"
			d := resourceDataWithRawConfig(res, "*1", map[string]cty.Value{tc.field: tc.value})
			// The helper initializes strings from state; set typed values as well.
			if err := d.Set(tc.field, tc.state); err != nil {
				t.Fatal(err)
			}
			item, _ = TerraformResourceDataToMikrotik(res.Schema, d)
			if item[key] != tc.wire {
				t.Fatalf("write %s = %q, want %q", key, item[key], tc.wire)
			}
			read := res.TestResourceData()
			if diags := MikrotikResourceDataToTerraform(MikrotikItem{key: tc.wire}, res.Schema, read); len(diags) != 0 {
				t.Fatalf("read: %v", diags)
			}
			if got := read.Get(tc.field); !reflect.DeepEqual(got, tc.state) {
				t.Fatalf("read %s = %#v, want %#v", tc.field, got, tc.state)
			}
			if tc.value.Type() == cty.Bool {
				// Explicit false must disable the setting rather than disappear.
				d = resourceDataWithRawConfig(res, "*1", map[string]cty.Value{tc.field: cty.False})
				item, _ = TerraformResourceDataToMikrotik(res.Schema, d)
				if got := item[key]; got != "no" {
					t.Fatalf("explicit false = %q, want no", got)
				}
			}
		})
	}
}

func TestRouterOS725SNMPAuthentication(t *testing.T) {
	res := ResourceSNMPCommunity()
	for _, value := range []string{"MD5", "SHA1", "SHA256", "SHA384", "SHA512"} {
		t.Run(value, func(t *testing.T) {
			_, errs := res.Schema["authentication_protocol"].ValidateFunc(value, "authentication_protocol")
			if len(errs) != 0 {
				t.Fatalf("valid protocol rejected: %v", errs)
			}
		})
	}
	for _, value := range []string{"SHA224", "SHA-256", "invalid"} {
		if _, errs := res.Schema["authentication_protocol"].ValidateFunc(value, "authentication_protocol"); len(errs) == 0 {
			t.Errorf("unsupported protocol %q accepted", value)
		}
	}
}

func TestRouterOS725VLANMACValidation(t *testing.T) {
	validate := ResourceInterfaceVlan().Schema["forced_mac_address"].ValidateFunc
	for _, value := range []string{"02:00:00:00:07:25", "02:AA:BB:CC:DD:EE"} {
		if _, errs := validate(value, "forced_mac_address"); len(errs) != 0 {
			t.Errorf("valid MAC %q rejected: %v", value, errs)
		}
	}
	for _, value := range []string{"192.0.2.1", "not-a-mac", "02:00:00:00:07"} {
		if _, errs := validate(value, "forced_mac_address"); len(errs) == 0 {
			t.Errorf("invalid MAC %q accepted", value)
		}
	}
}
