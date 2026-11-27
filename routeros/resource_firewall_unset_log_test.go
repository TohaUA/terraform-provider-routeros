package routeros

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func allFirewallResources() []struct {
	name string
	mk   func() *schema.Resource
} {
	return []struct {
		name string
		mk   func() *schema.Resource
	}{
		{"ip_firewall_filter", ResourceIPFirewallFilter},
		{"ip_firewall_mangle", ResourceIPFirewallMangle},
		{"ip_firewall_nat", ResourceIPFirewallNat},
		{"ip_firewall_raw", ResourceIPFirewallRaw},
		{"ipv6_firewall_filter", ResourceIPv6FirewallFilter},
		{"ipv6_firewall_mangle", ResourceIPv6FirewallMangle},
		{"ipv6_firewall_nat", ResourceIPv6FirewallNat},
	}
}

func TestFirewallOptionalStringUnsetFields(t *testing.T) {
	expectedMatchers := []string{
		"limit", "dst_limit", "connection_limit", "connection_rate", "connection_bytes",
		"nth", "time", "packet_size", "tcp_mss", "dst_port", "src_port", "port", "protocol", "content",
	}
	excludedFields := []string{
		KeyComment, "log_prefix", KeyPlaceBefore,
		"log", "fragment", "passthrough", "hw_offload", "randomise_ports", "same_not_by_dst",
	}

	for _, tc := range allFirewallResources() {
		t.Run(tc.name, func(t *testing.T) {
			res := tc.mk()
			suf, ok := res.Schema[MetaSetUnsetFields]
			if !ok {
				t.Fatalf("missing %s", MetaSetUnsetFields)
			}
			unset := loadSkipFields(suf.Default.(string))

			for name, prop := range res.Schema {
				if strings.HasPrefix(name, "___") {
					if _, found := unset[name]; found {
						t.Errorf("metadata field %q must not be in MetaSetUnsetFields", name)
					}
					continue
				}
				if name == KeyComment || name == "log_prefix" || name == KeyPlaceBefore {
					continue
				}
				if prop.Optional && !prop.Computed && prop.Type == schema.TypeString {
					if _, found := unset[name]; !found {
						t.Errorf("optional string field %q missing from MetaSetUnsetFields", name)
					}
				}
			}

			for _, matcher := range expectedMatchers {
				if prop, exists := res.Schema[matcher]; exists && prop.Optional && !prop.Computed {
					if _, found := unset[matcher]; !found {
						t.Errorf("expected matcher %q in MetaSetUnsetFields", matcher)
					}
				}
			}

			for _, excluded := range excludedFields {
				if _, found := unset[excluded]; found {
					t.Errorf("field %q must not be in MetaSetUnsetFields", excluded)
				}
			}
		})
	}
}

func TestFirewallRemoveOptionalMatcherEmitsUnset(t *testing.T) {
	cases := []struct {
		name   string
		mk     func() *schema.Resource
		chain  string
		action string
	}{
		{"ip_firewall_nat", ResourceIPFirewallNat, "srcnat", "masquerade"},
		{"ip_firewall_filter", ResourceIPFirewallFilter, "forward", "accept"},
		{"ipv6_firewall_nat", ResourceIPv6FirewallNat, "srcnat", "masquerade"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := tc.mk()
			_, attrs := blockTestNullRawConfig(res)
			attrs["chain"] = cty.StringVal(tc.chain)
			attrs["action"] = cty.StringVal(tc.action)
			attrs["protocol"] = cty.StringVal("tcp")

			state := &terraform.InstanceState{
				ID: "*1",
				Attributes: map[string]string{
					"id":         "*1",
					"chain":      tc.chain,
					"action":     tc.action,
					"protocol":   "tcp",
					"limit":      "10/1m,5:packet",
					"dst_port":   "80",
					"log_prefix": "NAT:",
					"comment":    "test",
					"log":        "false",
				},
				RawConfig: cty.ObjectVal(attrs),
			}

			item, recovered := blockTestApplyUpdate(t, res, state, map[string]interface{}{
				"chain":    tc.chain,
				"action":   tc.action,
				"protocol": "tcp",
			})
			if recovered != nil {
				t.Fatalf("TerraformResourceDataToMikrotik panicked: %v", recovered)
			}

			for _, k := range []string{"!limit", "!dst-port"} {
				v, ok := item[k]
				if !ok || v != "" {
					t.Errorf("expected %s=\"\", got ok=%v val=%q (item=%v)", k, ok, v, item)
				}
			}
			for _, k := range []string{"limit", "dst-port", "!log-prefix", "!comment", "nth", "time", "psd", "!nth", "!time", "!psd"} {
				if _, ok := item[k]; ok {
					t.Errorf("unexpected key %q in serialized item: %v", k, item)
				}
			}
			for _, k := range []string{"log-prefix", "comment"} {
				v, ok := item[k]
				if !ok || v != "" {
					t.Errorf("expected %s=\"\", got ok=%v val=%q (item=%v)", k, ok, v, item)
				}
			}
		})
	}
}

func TestFirewallLogDefaultAndRemovalPlan(t *testing.T) {
	for _, tc := range allFirewallResources() {
		t.Run(tc.name, func(t *testing.T) {
			res := tc.mk()
			logSchema, ok := res.Schema["log"]
			if !ok {
				t.Fatalf("missing log schema")
			}
			if !logSchema.Optional {
				t.Errorf("log.Optional = false, want true")
			}
			if logSchema.Default != false {
				t.Errorf("log.Default = %#v, want false", logSchema.Default)
			}
			if logSchema.DiffSuppressFunc != nil {
				t.Errorf("log.DiffSuppressFunc must be nil")
			}

			chain := "forward"
			action := "accept"
			if slices.Contains([]string{"ip_firewall_nat", "ipv6_firewall_nat"}, tc.name) {
				chain = "srcnat"
				action = "masquerade"
			} else if tc.name == "ip_firewall_raw" {
				chain = "prerouting"
			}

			// State log = true, config omits log -> plans change to false and sends log=no.
			_, attrs := blockTestNullRawConfig(res)
			attrs["chain"] = cty.StringVal(chain)
			attrs["action"] = cty.StringVal(action)

			stateTrue := &terraform.InstanceState{
				ID: "*1",
				Attributes: map[string]string{
					"id":     "*1",
					"chain":  chain,
					"action": action,
					"log":    "true",
				},
				RawConfig: cty.ObjectVal(attrs),
			}
			cfg := map[string]interface{}{
				"chain":  chain,
				"action": action,
			}
			diff, err := res.Diff(context.Background(), stateTrue, terraform.NewResourceConfigRaw(cfg), nil)
			if err != nil {
				t.Fatalf("diff (log=true -> omitted): %v", err)
			}
			if diff == nil || diff.Attributes["log"] == nil {
				t.Fatalf("expected diff on log when removed from config, got %#v", diff)
			}
			if diff.Attributes["log"].Old != "true" || diff.Attributes["log"].New != "false" {
				t.Fatalf("log diff = %q -> %q, want \"true\" -> \"false\"", diff.Attributes["log"].Old, diff.Attributes["log"].New)
			}

			item, recovered := blockTestApplyUpdate(t, res, stateTrue, cfg)
			if recovered != nil {
				t.Fatalf("TerraformResourceDataToMikrotik panicked: %v", recovered)
			}
			if got := item["log"]; got != "no" {
				t.Errorf("serialized log = %q, want \"no\"", got)
			}

			// State log = false, config omits log -> no diff on log.
			stateFalse := &terraform.InstanceState{
				ID: "*1",
				Attributes: map[string]string{
					"id":     "*1",
					"chain":  chain,
					"action": action,
					"log":    "false",
				},
				RawConfig: cty.ObjectVal(attrs),
			}
			diffFalse, err := res.Diff(context.Background(), stateFalse, terraform.NewResourceConfigRaw(cfg), nil)
			if err != nil {
				t.Fatalf("diff (log=false -> omitted): %v", err)
			}
			if diffFalse != nil && diffFalse.Attributes["log"] != nil {
				t.Errorf("unexpected diff on log when already false: %#v", diffFalse.Attributes["log"])
			}
		})
	}
}

func TestAccIPFirewallNatUnsetAndLog(t *testing.T) {
	const resAddr = "routeros_ip_firewall_nat.test"
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/ip/firewall/nat", "routeros_ip_firewall_nat"),
				Steps: []resource.TestStep{
					{
						Config: providerConfig + `
resource "routeros_ip_firewall_nat" "test" {
  chain      = "srcnat"
  action     = "masquerade"
  disabled   = true
  protocol   = "tcp"
  dst_port   = "8080"
  limit      = "10/1m,5:packet"
  log        = true
  log_prefix = "tf-test:"
}
`,
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(resAddr),
							resource.TestCheckResourceAttr(resAddr, "limit", "10/1m,5:packet"),
							resource.TestCheckResourceAttr(resAddr, "dst_port", "8080"),
							resource.TestCheckResourceAttr(resAddr, "log", "true"),
							resource.TestCheckResourceAttr(resAddr, "log_prefix", "tf-test:"),
						),
					},
					{
						Config: providerConfig + `
resource "routeros_ip_firewall_nat" "test" {
  chain    = "srcnat"
  action   = "masquerade"
  disabled = true
  protocol = "tcp"
}
`,
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(resAddr),
							resource.TestCheckResourceAttr(resAddr, "log", "false"),
							resource.TestCheckResourceAttr(resAddr, "limit", ""),
							resource.TestCheckResourceAttr(resAddr, "dst_port", ""),
							resource.TestCheckResourceAttr(resAddr, "log_prefix", ""),
						),
					},
				},
			})
		})
	}
}
