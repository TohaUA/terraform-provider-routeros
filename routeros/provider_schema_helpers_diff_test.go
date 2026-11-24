package routeros

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func TestDiffSuppressorsRejectUnparseableValuesWithoutPanicking(t *testing.T) {
	ipAddrListTimeout := ResourceIPFirewallAddrList().Schema["timeout"].DiffSuppressFunc
	ipv6AddrListTimeout := ResourceIPv6FirewallAddrList().Schema["timeout"].DiffSuppressFunc

	cases := []struct {
		name     string
		suppress schema.SchemaDiffSuppressFunc
		old, new string
	}{
		{"bits/old", BitsEqual, "30m", "30M"},
		{"bits/new", BitsEqual, "30M", "30m"},
		{"bytes/old", BytesEqual, "64m", "64M"},
		{"bytes/new", BytesEqual, "64M", "64m"},
		{"duration/old", TimeEqual, "invalid", "1m"},
		{"duration/new", TimeEqual, "1m", "invalid"},
		{"duration_milliseconds/old", TimeEqualU(time.Millisecond), "invalid", "1000"},
		{"duration_milliseconds/new", TimeEqualU(time.Millisecond), "1000", "invalid"},
		{"hex/old", HexEqual, "invalid", "0x10"},
		{"hex/new", HexEqual, "0x10", "invalid"},
		{"keepalive_format/old", PropKeepaliveRw.DiffSuppressFunc, "10s", "10s,3"},
		{"keepalive_format/new", PropKeepaliveRw.DiffSuppressFunc, "10s,3", "10s"},
		{"keepalive_format/old_10", PropKeepaliveRw.DiffSuppressFunc, "10s", "10s,10"},
		{"keepalive_interval/old", PropKeepaliveRw.DiffSuppressFunc, "invalid,3", "10s,3"},
		{"keepalive_interval/new", PropKeepaliveRw.DiffSuppressFunc, "10s,3", "invalid,3"},
		{"ip_firewall_addr_list_timeout/old", ipAddrListTimeout, "invalid", "5m"},
		{"ip_firewall_addr_list_timeout/new", ipAddrListTimeout, "5m", "invalid"},
		{"ip_firewall_addr_list_timeout/both", ipAddrListTimeout, "bad-old", "bad-new"},
		{"ipv6_firewall_addr_list_timeout/old", ipv6AddrListTimeout, "invalid", "5m"},
		{"ipv6_firewall_addr_list_timeout/new", ipv6AddrListTimeout, "5m", "invalid"},
		{"ipv6_firewall_addr_list_timeout/both", ipv6AddrListTimeout, "bad-old", "bad-new"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if err := recover(); err != nil {
					t.Fatalf("diff suppression panicked instead of leaving the diff visible: %v", err)
				}
			}()
			d := diffSuppressorTestData(cty.StringVal(tc.new))
			if tc.suppress("value", tc.old, tc.new, d) {
				t.Fatal("unparseable configured value suppressed the diff")
			}
		})
	}
}

func TestDiffSuppressorsPreserveValidComparisons(t *testing.T) {
	ipAddrListTimeout := ResourceIPFirewallAddrList().Schema["timeout"].DiffSuppressFunc
	ipv6AddrListTimeout := ResourceIPv6FirewallAddrList().Schema["timeout"].DiffSuppressFunc

	cases := []struct {
		name     string
		suppress schema.SchemaDiffSuppressFunc
		old, new string
		want     bool
	}{
		{"bits/equal", BitsEqual, "30M", "30000000", true},
		{"bits/different", BitsEqual, "30M", "31M", false},
		{"bytes/equal", BytesEqual, "64M", "67108864", true},
		{"bytes/different", BytesEqual, "64M", "65M", false},
		{"bytes/unlimited", BytesEqual, "unlimited", "unlimited", true},
		{"duration/equal", TimeEqual, "1m", "60s", true},
		{"duration/different", TimeEqual, "1m", "61s", false},
		{"duration/control_word", TimeEqual, "infinity", "infinity", true},
		{"duration_milliseconds/equal", TimeEqualU(time.Millisecond), "1000", "1s", true},
		{"hex/equal", HexEqual, "0x10", "16", true},
		{"hex/different", HexEqual, "0x10", "17", false},
		{"keepalive/equal", PropKeepaliveRw.DiffSuppressFunc, "60s,3", "1m,3", true},
		{"keepalive/different_interval", PropKeepaliveRw.DiffSuppressFunc, "10s,3", "20s,3", false},
		{"keepalive/different_retries", PropKeepaliveRw.DiffSuppressFunc, "10s,3", "10s,4", false},
		{"ip_firewall_addr_list_timeout/countdown", ipAddrListTimeout, "4m59s", "5m", true},
		{"ip_firewall_addr_list_timeout/equal_string", ipAddrListTimeout, "5m", "5m", true},
		{"ip_firewall_addr_list_timeout/equal_duration_diff_string", ipAddrListTimeout, "300s", "5m", false},
		{"ip_firewall_addr_list_timeout/shortened", ipAddrListTimeout, "5m", "4m", false},
		{"ip_firewall_addr_list_timeout/empty_old", ipAddrListTimeout, "", "5m", false},
		{"ip_firewall_addr_list_timeout/empty_new", ipAddrListTimeout, "5m", "", false},
		{"ip_firewall_addr_list_timeout/both_empty", ipAddrListTimeout, "", "", true},
		{"ipv6_firewall_addr_list_timeout/countdown", ipv6AddrListTimeout, "4m59s", "5m", true},
		{"ipv6_firewall_addr_list_timeout/equal_string", ipv6AddrListTimeout, "5m", "5m", true},
		{"ipv6_firewall_addr_list_timeout/equal_duration_diff_string", ipv6AddrListTimeout, "300s", "5m", false},
		{"ipv6_firewall_addr_list_timeout/shortened", ipv6AddrListTimeout, "5m", "4m", false},
		{"ipv6_firewall_addr_list_timeout/empty_old", ipv6AddrListTimeout, "", "5m", false},
		{"ipv6_firewall_addr_list_timeout/empty_new", ipv6AddrListTimeout, "5m", "", false},
		{"ipv6_firewall_addr_list_timeout/both_empty", ipv6AddrListTimeout, "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.suppress("value", tc.old, tc.new, diffSuppressorTestData(cty.StringVal(tc.new))); got != tc.want {
				t.Errorf("suppress(%q, %q) = %t, want %t", tc.old, tc.new, got, tc.want)
			}
		})
	}
}

func TestDiffSuppressorsLeaveUndeclaredValuesAlone(t *testing.T) {
	for name, suppress := range map[string]schema.SchemaDiffSuppressFunc{
		"bits": BitsEqual, "bytes": BytesEqual, "duration": TimeEqual,
		"duration_milliseconds": TimeEqualU(time.Millisecond), "hex": HexEqual,
		"keepalive": PropKeepaliveRw.DiffSuppressFunc,
	} {
		t.Run(name, func(t *testing.T) {
			if !suppress("value", "invalid-old", "invalid-new", diffSuppressorTestData(cty.NullVal(cty.String))) {
				t.Fatal("field absent from configuration should retain its existing suppression behavior")
			}
		})
	}
}

func TestDiffSuppressorWarningsDoNotEchoValues(t *testing.T) {
	var warnings bytes.Buffer
	previousWriter := log.Writer()
	log.SetOutput(&warnings)
	t.Cleanup(func() { log.SetOutput(previousWriter) })

	const marker = "private-input-marker"
	for _, suppress := range []schema.SchemaDiffSuppressFunc{BitsEqual, BytesEqual, TimeEqual, HexEqual, PropKeepaliveRw.DiffSuppressFunc} {
		if suppress("value", marker, "1", diffSuppressorTestData(cty.StringVal("1"))) {
			t.Fatal("unparseable value suppressed the diff")
		}
	}
	if strings.Contains(warnings.String(), marker) {
		t.Fatalf("warnings echo the input value: %s", warnings.String())
	}
	if got := strings.Count(warnings.String(), "[WARN]"); got != 5 {
		t.Errorf("got %d parse warnings, want 5: %s", got, warnings.String())
	}
}

func diffSuppressorTestData(configured cty.Value) *schema.ResourceData {
	res := &schema.Resource{Schema: map[string]*schema.Schema{
		"value": {Type: schema.TypeString, Optional: true},
	}}
	return resourceDataWithRawConfig(res, "*1", map[string]cty.Value{"value": configured})
}

func TestFirewallAddrListTimeoutDiffSuppressWarningsDoNotEchoValues(t *testing.T) {
	var warnings bytes.Buffer
	previousWriter := log.Writer()
	log.SetOutput(&warnings)
	t.Cleanup(func() { log.SetOutput(previousWriter) })

	ipAddrListTimeout := ResourceIPFirewallAddrList().Schema["timeout"].DiffSuppressFunc
	ipv6AddrListTimeout := ResourceIPv6FirewallAddrList().Schema["timeout"].DiffSuppressFunc

	const marker = "private-input-marker"
	for _, tc := range []struct {
		name     string
		suppress schema.SchemaDiffSuppressFunc
		old, new string
	}{
		{"ipv4/old", ipAddrListTimeout, marker, "5m"},
		{"ipv4/new", ipAddrListTimeout, "5m", marker},
		{"ipv6/old", ipv6AddrListTimeout, marker, "5m"},
		{"ipv6/new", ipv6AddrListTimeout, "5m", marker},
	} {
		if tc.suppress("timeout", tc.old, tc.new, nil) {
			t.Fatalf("%s: unparseable value suppressed the diff", tc.name)
		}
	}

	if strings.Contains(warnings.String(), marker) {
		t.Fatalf("warnings echo the input value: %s", warnings.String())
	}
	if got := strings.Count(warnings.String(), "[WARN]"); got != 4 {
		t.Errorf("got %d parse warnings, want 4: %s", got, warnings.String())
	}
}
