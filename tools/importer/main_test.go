package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/terraform-routeros/terraform-provider-routeros/routeros"
)

func TestResolvePassword(t *testing.T) {
	t.Run("ROS_PASSWORD takes precedence over MIKROTIK_PASSWORD", func(t *testing.T) {
		promptCalled := false
		env := map[string]string{
			"ROS_PASSWORD":      "ros-secret",
			"MIKROTIK_PASSWORD": "mk-secret",
		}
		got, err := resolvePassword(
			func(k string) (string, bool) { v, ok := env[k]; return v, ok },
			func(string) (string, error) { promptCalled = true; return "prompt", nil },
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "ros-secret" {
			t.Fatalf("got %q, want %q", got, "ros-secret")
		}
		if promptCalled {
			t.Fatal("prompt should not be called when ROS_PASSWORD is set")
		}
	})

	t.Run("falls back to MIKROTIK_PASSWORD when ROS_PASSWORD unset or empty", func(t *testing.T) {
		promptCalled := false
		env := map[string]string{
			"ROS_PASSWORD":      "",
			"MIKROTIK_PASSWORD": "mk-secret",
		}
		got, err := resolvePassword(
			func(k string) (string, bool) { v, ok := env[k]; return v, ok },
			func(string) (string, error) { promptCalled = true; return "prompt", nil },
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "mk-secret" {
			t.Fatalf("got %q, want %q", got, "mk-secret")
		}
		if promptCalled {
			t.Fatal("prompt should not be called when MIKROTIK_PASSWORD is set")
		}
	})

	t.Run("falls back to prompt when both env vars empty or unset", func(t *testing.T) {
		got, err := resolvePassword(
			func(string) (string, bool) { return "", false },
			func(prompt string) (string, error) {
				if prompt == "" {
					t.Fatal("expected non-empty prompt")
				}
				return "  space pass  ", nil
			},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "  space pass  " {
			t.Fatalf("got %q, want spaces preserved", got)
		}
	})

	t.Run("propagates prompt error", func(t *testing.T) {
		wantErr := errors.New("tty read failed")
		_, err := resolvePassword(
			func(string) (string, bool) { return "", false },
			func(string) (string, error) { return "", wantErr },
		)
		if !errors.Is(err, wantErr) {
			t.Fatalf("got %v, want %v", err, wantErr)
		}
	})
}

func TestWriteTFFileMode0600(t *testing.T) {
	dir := t.TempDir()

	t.Run("new file gets 0600", func(t *testing.T) {
		p := filepath.Join(dir, "new.tf")
		if err := writeTFFile(p, []byte("resource {}\n")); err != nil {
			t.Fatalf("writeTFFile failed: %v", err)
		}
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("Stat failed: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("got mode %#o, want 0600", info.Mode().Perm())
		}
	})

	t.Run("pre-existing looser file is tightened to 0600", func(t *testing.T) {
		p := filepath.Join(dir, "existing.tf")
		if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := writeTFFile(p, []byte("new\n")); err != nil {
			t.Fatalf("writeTFFile failed: %v", err)
		}
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("Stat failed: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("got mode %#o, want 0600", info.Mode().Perm())
		}
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "new\n" {
			t.Fatalf("got content %q, want %q", string(data), "new\n")
		}
	})
}

func TestRenderProviderBlock(t *testing.T) {
	out := renderProviderBlock("2001:db8::1", `admin"1${foo}%{bar}`)
	if strings.Contains(out, "insecure") {
		t.Fatalf("provider block must not contain insecure: %s", out)
	}
	if !strings.Contains(out, "# ca_certificate") {
		t.Fatalf("provider block must contain # ca_certificate comment: %s", out)
	}

	file, diags := hclsyntax.ParseConfig([]byte(out), "provider.tf", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		t.Fatalf("provider block failed to parse: %s\n%s", diags.Error(), out)
	}
	body := file.Body.(*hclsyntax.Body)
	for _, blk := range body.Blocks {
		if blk.Type != "provider" {
			continue
		}
		hostVal, d1 := blk.Body.Attributes["hosturl"].Expr.Value(nil)
		userVal, d2 := blk.Body.Attributes["username"].Expr.Value(nil)
		if d1.HasErrors() || d2.HasErrors() {
			t.Fatalf("eval errors: %v / %v", d1, d2)
		}
		if hostVal.AsString() != "https://[2001:db8::1]" {
			t.Fatalf("hosturl = %q, want %q", hostVal.AsString(), "https://[2001:db8::1]")
		}
		if userVal.AsString() != `admin"1${foo}%{bar}` {
			t.Fatalf("username = %q", userVal.AsString())
		}
		assertNoTemplateInterpolation(t, blk.Body.Attributes["hosturl"].Expr)
		assertNoTemplateInterpolation(t, blk.Body.Attributes["username"].Expr)
	}
}

func TestImporterSchemaAndEdgeCases(t *testing.T) {
	provider := routeros.NewProvider()

	t.Run("TypeList with Elem *schema.Resource does not panic", func(t *testing.T) {
		res := provider.ResourcesMap["routeros_routing_bgp_evpn"]
		if res == nil {
			t.Skip("routeros_routing_bgp_evpn not present")
		}
		cmd := ExportCommand{
			Path:   "/routing bgp evpn",
			Action: "add",
			Attrs: []RawAttr{
				{Key: "name", RawValue: "evpn1"},
				{Key: "export", RawValue: "foo"},
			},
		}
		hclAttrs, _, _ := GetAttributes(provider, "routeros_routing_bgp_evpn", cmd)
		if len(hclAttrs) == 0 {
			t.Fatal("expected attributes to be emitted")
		}
	})

	t.Run("interface_ethernet explicit name overrides TransformMap without duplicate", func(t *testing.T) {
		export := "/interface ethernet set [ find default-name=ether1 ] disable-running-check=no name=wan1\n"
		var capturedFilter []string
		hclText := generateHCL(export, provider, func(path string, filter []string) string {
			capturedFilter = filter
			return "*1"
		}, "192.168.88.1", "admin")

		file, diags := hclsyntax.ParseConfig([]byte(hclText), "eth.tf", hcl.Pos{Line: 1, Column: 1})
		if diags.HasErrors() {
			t.Fatalf("duplicate or invalid HCL: %s\n%s", diags.Error(), hclText)
		}
		body := file.Body.(*hclsyntax.Body)
		for _, blk := range body.Blocks {
			if blk.Type == "resource" {
				nameVal, _ := blk.Body.Attributes["name"].Expr.Value(nil)
				if nameVal.AsString() != "wan1" {
					t.Fatalf("got name %q, want wan1", nameVal.AsString())
				}
				factoryVal, _ := blk.Body.Attributes["factory_name"].Expr.Value(nil)
				if factoryVal.AsString() != "ether1" {
					t.Fatalf("got factory_name %q, want ether1", factoryVal.AsString())
				}
			}
		}
		if !reflect.DeepEqual(capturedFilter, []string{"disable-running-check=no", "name=wan1"}) {
			t.Fatalf("unexpected filter: %#v", capturedFilter)
		}
	})

	t.Run("multiple system-* resources receive distinct numbered HCL names", func(t *testing.T) {
		export := strings.Join([]string{
			`/system scheduler add name=sched1 on-event="/log info 1"`,
			`/system scheduler add name=sched2 on-event="/log info 2"`,
		}, "\n")
		hclText := generateHCL(export, provider, func(string, []string) string { return "*1" }, "192.168.88.1", "admin")
		file, diags := hclsyntax.ParseConfig([]byte(hclText), "sched.tf", hcl.Pos{Line: 1, Column: 1})
		if diags.HasErrors() {
			t.Fatalf("invalid HCL for multiple system-* resources: %s\n%s", diags.Error(), hclText)
		}
		var names []string
		for _, blk := range file.Body.(*hclsyntax.Body).Blocks {
			if blk.Type == "resource" {
				names = append(names, blk.Labels[1])
			}
		}
		if !reflect.DeepEqual(names, []string{"system-scheduler-1", "system-scheduler-2"}) {
			t.Fatalf("got resource block names %#v", names)
		}
	})

	t.Run("missing required non-string attributes emit valid HCL placeholders", func(t *testing.T) {
		// Create a synthetic provider resource with required non-string attributes to verify placeholder generation.
		testProv := &schema.Provider{
			ResourcesMap: map[string]*schema.Resource{
				"routeros_test_req": {
					Schema: map[string]*schema.Schema{
						routeros.MetaResourcePath: routeros.PropResourcePath("/test/req"),
						"req_str":                 {Type: schema.TypeString, Required: true},
						"req_int":                 {Type: schema.TypeInt, Required: true},
						"req_bool":                {Type: schema.TypeBool, Required: true},
						"req_list":                {Type: schema.TypeList, Required: true, Elem: &schema.Schema{Type: schema.TypeString}},
						"req_map":                 {Type: schema.TypeMap, Required: true, Elem: &schema.Schema{Type: schema.TypeString}},
					},
				},
			},
		}
		hclText := generateHCL("/test req add\n", testProv, func(string, []string) string { return "*1" }, "192.168.88.1", "admin")
		file, diags := hclsyntax.ParseConfig([]byte(hclText), "req.tf", hcl.Pos{Line: 1, Column: 1})
		if diags.HasErrors() {
			t.Fatalf("missing required placeholders produced invalid HCL: %s\n%s", diags.Error(), hclText)
		}
		var resBlk *hclsyntax.Block
		for _, blk := range file.Body.(*hclsyntax.Body).Blocks {
			if blk.Type == "resource" {
				resBlk = blk
			}
		}
		if resBlk == nil {
			t.Fatal("missing resource block")
		}
		if v, _ := resBlk.Body.Attributes["req_str"].Expr.Value(nil); v.AsString() != "?" {
			t.Fatalf("req_str = %#v", v)
		}
		if v, _ := resBlk.Body.Attributes["req_int"].Expr.Value(nil); v.AsBigFloat().String() != "0" {
			t.Fatalf("req_int = %#v", v)
		}
		if v, _ := resBlk.Body.Attributes["req_bool"].Expr.Value(nil); v.True() {
			t.Fatalf("req_bool = %#v", v)
		}
		if v, _ := resBlk.Body.Attributes["req_list"].Expr.Value(nil); v.LengthInt() != 0 {
			t.Fatalf("req_list = %#v", v)
		}
		if v, _ := resBlk.Body.Attributes["req_map"].Expr.Value(nil); v.LengthInt() != 0 {
			t.Fatalf("req_map = %#v", v)
		}
	})

	t.Run("GetResourceId filters use raw RouterOS values without HCL escaping or '=' truncation", func(t *testing.T) {
		export := strings.Join([]string{
			`/ip firewall filter add action=accept chain=input comment="a=b \${x} %{y}" disabled=no`,
			`/ip service set api-ssl certificate=ssl port=8729`,
			`/ip ipsec proposal set [ find default=yes ] auth-algorithms=sha256`,
		}, "\n")
		var filters [][]string
		_ = generateHCL(export, provider, func(path string, filter []string) string {
			filters = append(filters, filter)
			return "*1"
		}, "192.168.88.1", "admin")

		if len(filters) != 3 {
			t.Fatalf("expected 3 lookup calls, got %d", len(filters))
		}
		// 1. firewall filter must preserve '=' and must not contain '$${x}' or '%%{y}'
		joined0 := strings.Join(filters[0], " ")
		if strings.Contains(joined0, "$${") || strings.Contains(joined0, "%%{") {
			t.Fatalf("HCL escapes leaked into RouterOS find filter: %q", joined0)
		}
		if !strings.Contains(joined0, `comment="a=b \${x} %{y}"`) {
			t.Fatalf("expected un-truncated RouterOS value in filter, got %q", joined0)
		}
		// 2. ip service maps numbers -> name and keeps port when non-placeholder
		joined1 := strings.Join(filters[1], " ")
		if !strings.Contains(joined1, "name=api-ssl") || !strings.Contains(joined1, "port=8729") {
			t.Fatalf("unexpected ip service filter: %q", joined1)
		}
		// 3. ipsec proposal maps missing name placeholder -> name=default
		joined2 := strings.Join(filters[2], " ")
		if !strings.Contains(joined2, "name=default") {
			t.Fatalf("expected name=default in ipsec proposal filter, got %q", joined2)
		}
	})
}
