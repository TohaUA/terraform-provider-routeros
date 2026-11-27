package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/terraform-routeros/terraform-provider-routeros/routeros"
	"github.com/zclconf/go-cty/cty"
)

func TestParseExport(t *testing.T) {
	input := strings.Join([]string{
		"# comment line",
		"",
		"   ",
		"/interface bridge",
		"/interface bridge add name=bridge1 comment=\"\" empty=\r",
		"/interface ethernet set [ find default-name=ether1 ] disable-running-check=no comment=\"say \\\"hi\\\"\\\\\"",
		"/ip service set \"my service\" certificate=ssl",
		"/ip service set \"a=b\" port=8443",
		"/system script set s1 source=\"/ip address add address=1.2.3.4/24\"",
		"/ip firewall filter add action=accept chain=input comment=\"/ip address set 0 disabled=yes\"",
		"/interface wireguard peers add allowed-address=10.0.0.2/32,10.0.0.3/32 interface=wg1 public-key=\"abc=def=\" rawval=foo=bar", // trailing line without newline
	}, "\n")

	cmds := ParseExport(input)
	if len(cmds) != 7 {
		t.Fatalf("expected 7 commands, got %d: %#v", len(cmds), cmds)
	}

	// 1. bridge add with empty quotes and empty unquoted value + \r
	if cmds[0].Path != "/interface bridge" || cmds[0].Action != "add" {
		t.Fatalf("cmd[0] mismatch: %#v", cmds[0])
	}
	wantAttrs0 := []RawAttr{
		{Key: "name", RawValue: "bridge1"},
		{Key: "comment", RawValue: `""`},
		{Key: "empty", RawValue: ""},
	}
	if !reflect.DeepEqual(cmds[0].Attrs, wantAttrs0) {
		t.Fatalf("cmd[0].Attrs = %#v, want %#v", cmds[0].Attrs, wantAttrs0)
	}

	// 2. ethernet set [ find ... ] with escaped quotes and escaped trailing backslash
	if cmds[1].Path != "/interface ethernet" || cmds[1].Action != "set" || cmds[1].Target != "" {
		t.Fatalf("cmd[1] header mismatch: %#v", cmds[1])
	}
	if !reflect.DeepEqual(cmds[1].FindArgs, []RawAttr{{Key: "default-name", RawValue: "ether1"}}) {
		t.Fatalf("cmd[1].FindArgs = %#v", cmds[1].FindArgs)
	}
	wantAttrs1 := []RawAttr{
		{Key: "disable-running-check", RawValue: "no"},
		{Key: "comment", RawValue: `"say \"hi\"\\"`},
	}
	if !reflect.DeepEqual(cmds[1].Attrs, wantAttrs1) {
		t.Fatalf("cmd[1].Attrs = %#v, want %#v", cmds[1].Attrs, wantAttrs1)
	}

	// 3. quoted set target with space
	if cmds[2].Target != "my service" || !reflect.DeepEqual(cmds[2].Attrs, []RawAttr{{Key: "certificate", RawValue: "ssl"}}) {
		t.Fatalf("cmd[2] mismatch: %#v", cmds[2])
	}

	// 4. quoted set target containing '='
	if cmds[3].Target != "a=b" || !reflect.DeepEqual(cmds[3].Attrs, []RawAttr{{Key: "port", RawValue: "8443"}}) {
		t.Fatalf("cmd[3] mismatch: %#v", cmds[3])
	}

	// 5. cross-verb " add " inside quoted string on `set`
	if cmds[4].Path != "/system script" || cmds[4].Action != "set" || cmds[4].Target != "s1" {
		t.Fatalf("cmd[4] mismatch: %#v", cmds[4])
	}
	if !reflect.DeepEqual(cmds[4].Attrs, []RawAttr{{Key: "source", RawValue: `"/ip address add address=1.2.3.4/24"`}}) {
		t.Fatalf("cmd[4].Attrs = %#v", cmds[4].Attrs)
	}

	// 6. cross-verb " set " inside quoted string on `add`
	if cmds[5].Path != "/ip firewall filter" || cmds[5].Action != "add" {
		t.Fatalf("cmd[5] mismatch: %#v", cmds[5])
	}

	// 7. EOF without trailing newline + '=' in quoted and unquoted values
	wantAttrs6 := []RawAttr{
		{Key: "allowed-address", RawValue: "10.0.0.2/32,10.0.0.3/32"},
		{Key: "interface", RawValue: "wg1"},
		{Key: "public-key", RawValue: `"abc=def="`},
		{Key: "rawval", RawValue: "foo=bar"},
	}
	if !reflect.DeepEqual(cmds[6].Attrs, wantAttrs6) {
		t.Fatalf("cmd[6].Attrs = %#v, want %#v", cmds[6].Attrs, wantAttrs6)
	}
}

func TestUnescapeRouterOSValue(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{`plain`, `plain`},
		{`""`, ``},
		{`"a\"b\\c"`, `a"b\c`},
		{`"line1\nline2\r\t\$var\?end\_here"`, "line1\nline2\r\t$var?end here"},
		{`"\a\b\f\v"`, "\a\b\f\v"},
		{`"\C3\A4\07"`, "ä\x07"},
		{`"foo=bar=baz"`, "foo=bar=baz"},
	}

	for _, tc := range tests {
		if got := unescapeRouterOSValue(tc.raw); got != tc.want {
			t.Errorf("unescapeRouterOSValue(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestSplitRouterOSList(t *testing.T) {
	tests := []struct {
		raw  string
		want []string
	}{
		{"", nil},
		{`""`, nil},
		{"{}", nil},
		{"1.1.1.1,8.8.8.8", []string{"1.1.1.1", "8.8.8.8"}},
		{`"1.1.1.1,8.8.8.8"`, []string{"1.1.1.1", "8.8.8.8"}},
		{`ether1,"ether 2","a \"b\",c"`, []string{"ether1", "ether 2", `a "b",c`}},
		{`{a; "b c"; "d\"e"}`, []string{"a", "b c", `d"e`}},
	}

	for _, tc := range tests {
		got := splitRouterOSList(tc.raw)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitRouterOSList(%q) = %#v, want %#v", tc.raw, got, tc.want)
		}
	}
}

func assertNoTemplateInterpolation(t *testing.T, expr hclsyntax.Expression) {
	t.Helper()
	if vars := expr.Variables(); len(vars) != 0 {
		t.Fatalf("unexpected HCL template variables in expression: %#v", vars)
	}
	if tmpl, ok := expr.(*hclsyntax.TemplateExpr); ok {
		for _, part := range tmpl.Parts {
			if _, isWrap := part.(*hclsyntax.TemplateWrapExpr); isWrap {
				t.Fatalf("unexpected TemplateWrapExpr in %#v", tmpl)
			}
		}
	}
}

func TestHCLRoundTrip(t *testing.T) {
	// RouterOS /export terse representation of literal strings containing ", \, ${...}, %{...}, =, newlines, and hex escapes.
	export := strings.Join([]string{
		`/ip firewall filter add action=accept chain=input comment="q=\"hello\" bs=\\ interp=\${env.FOO} dir=%{if true}hi%{endif} eq=a=b=c nl=1\n2 utf8=\C3\A4"`,
		`/interface wifi provisioning add slave-configurations="" supported-bands=2ghz-ax,5ghz-ax`,
		`/ip dhcp-server network add address=192.168.88.0/24 dns-server=1.1.1.1,"8.8.8.8" comment="a\${b}%{c}=d"`,
	}, "\n")

	provider := routeros.NewProvider()
	hclText := generateHCL(export, provider, func(path string, filter []string) string {
		// Also return an ID with special characters to verify importTemplate id escaping.
		return `*1"a\b${x}%{y}`
	}, `router"1`, `ad\min${u}%{v}`)

	file, diags := hclsyntax.ParseConfig([]byte(hclText), "autoimport.tf", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		t.Fatalf("generated HCL failed to parse: %s\nHCL:\n%s", diags.Error(), hclText)
	}

	body := file.Body.(*hclsyntax.Body)

	// Verify all attributes in all blocks evaluate cleanly with nil EvalContext (no interpolation)
	var gotFilterComment string
	var gotImportID string
	var gotSupportedBands []string
	var gotSlaveConfigs []string
	var gotDNSServers []string
	var gotProviderHost string
	var gotProviderUser string

	for _, blk := range body.Blocks {
		for name, attr := range blk.Body.Attributes {
			assertNoTemplateInterpolation(t, attr.Expr)
			val, valDiags := attr.Expr.Value(nil)
			if valDiags.HasErrors() {
				t.Fatalf("failed to evaluate %s.%s: %s", blk.Type, name, valDiags.Error())
			}

			switch blk.Type {
			case "provider":
				if name == "hosturl" {
					gotProviderHost = val.AsString()
				} else if name == "username" {
					gotProviderUser = val.AsString()
				}
			case "import":
				if name == "id" && gotImportID == "" {
					gotImportID = val.AsString()
				}
			case "resource":
				if len(blk.Labels) > 0 && blk.Labels[0] == "routeros_ip_firewall_filter" && name == "comment" {
					gotFilterComment = val.AsString()
				}
				if len(blk.Labels) > 0 && blk.Labels[0] == "routeros_wifi_provisioning" {
					if name == "supported_bands" {
						for _, el := range val.AsValueSlice() {
							gotSupportedBands = append(gotSupportedBands, el.AsString())
						}
					} else if name == "slave_configurations" {
						if !val.Type().Equals(cty.EmptyTuple) || val.LengthInt() != 0 {
							t.Fatalf("expected empty slave_configurations, got %#v", val)
						}
						gotSlaveConfigs = []string{}
					}
				}
				if len(blk.Labels) > 0 && blk.Labels[0] == "routeros_ip_dhcp_server_network" && name == "dns_server" {
					for _, el := range val.AsValueSlice() {
						gotDNSServers = append(gotDNSServers, el.AsString())
					}
				}
			}
		}
	}

	wantComment := "q=\"hello\" bs=\\ interp=${env.FOO} dir=%{if true}hi%{endif} eq=a=b=c nl=1\n2 utf8=ä"
	if gotFilterComment != wantComment {
		t.Fatalf("filter comment round-trip mismatch:\n got: %q\nwant: %q", gotFilterComment, wantComment)
	}
	if gotImportID != `*1"a\b${x}%{y}` {
		t.Fatalf("import id round-trip mismatch: got %q", gotImportID)
	}
	if gotProviderHost != `https://router"1` || gotProviderUser != `ad\min${u}%{v}` {
		t.Fatalf("provider block round-trip mismatch: host=%q user=%q", gotProviderHost, gotProviderUser)
	}
	if !reflect.DeepEqual(gotSupportedBands, []string{"2ghz-ax", "5ghz-ax"}) {
		t.Fatalf("supported_bands mismatch: %#v", gotSupportedBands)
	}
	if gotSlaveConfigs == nil {
		t.Fatalf("slave_configurations attribute missing")
	}
	if !reflect.DeepEqual(gotDNSServers, []string{"1.1.1.1", "8.8.8.8"}) {
		t.Fatalf("dns_server mismatch: %#v", gotDNSServers)
	}
}
