// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package routeros

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/terraform-routeros/terraform-provider-routeros/internal/macboot"
)

func TestResourceMacBootstrapSchemaAndMeta(t *testing.T) {
	p := Provider()
	if err := p.InternalValidate(); err != nil {
		t.Fatalf("Provider().InternalValidate() failed: %v", err)
	}
	res, ok := p.ResourcesMap["routeros_mac_bootstrap"]
	if !ok {
		t.Fatalf("routeros_mac_bootstrap not registered in ResourcesMap")
	}
	checkResourceSchema(res.Schema, t)
	if !res.Schema["password_wo"].WriteOnly || !res.Schema["password_wo"].Sensitive {
		t.Fatalf("password_wo must be WriteOnly and Sensitive")
	}
	if !res.Schema["password"].Sensitive {
		t.Fatalf("password must be Sensitive")
	}
}

func TestResourceMacBootstrapRejectsWithoutAllowInsecureL2(t *testing.T) {
	res := ResourceMacBootstrap()
	diff, err := res.Diff(
		context.Background(),
		nil,
		terraform.NewResourceConfigRaw(map[string]interface{}{
			"interface":         "macbootbr0",
			"mac_address":       "54:05:AB:01:02:03",
			"address":           "172.30.99.2/24",
			"target_interface":  "ether2",
			"username":          "admin",
			"allow_insecure_l2": false,
		}),
		&unconfiguredClient{},
	)
	if err == nil {
		t.Fatalf("expected CustomizeDiff error when allow_insecure_l2=false, got diff=%+v", diff)
	}
	if !strings.Contains(err.Error(), "allow_insecure_l2") {
		t.Fatalf("expected allow_insecure_l2 in error, got %v", err)
	}

	d := schema.TestResourceDataRaw(t, res.Schema, map[string]interface{}{
		"interface":         "macbootbr0",
		"mac_address":       "54:05:AB:01:02:03",
		"address":           "172.30.99.2/24",
		"target_interface":  "ether2",
		"username":          "admin",
		"allow_insecure_l2": false,
		"timeout":           "30s",
	})
	diags := res.CreateContext(context.Background(), d, &unconfiguredClient{})
	if !diags.HasError() {
		t.Fatalf("expected CreateContext error when allow_insecure_l2=false")
	}
}

func TestResourceMacBootstrapCreateWriteOnlyAndWarnings(t *testing.T) {
	res := ResourceMacBootstrap()
	origBoot := macBootstrapFunc
	defer func() { macBootstrapFunc = origBoot }()

	fp := strings.Repeat("AB", 32)
	var seenPassword string
	macBootstrapFunc = func(_ context.Context, cfg macboot.BootstrapConfig) (macboot.BootstrapResult, error) {
		seenPassword = cfg.Password
		return macboot.BootstrapResult{
			TLSFingerprint:  fp,
			Address:         cfg.Address,
			RouterAddress:   "172.30.99.2",
			Identity:        "MikroTik",
			RouterOSVersion: "7.25.2 (stable)",
		}, nil
	}

	vals := map[string]cty.Value{}
	for name, aType := range res.CoreConfigSchema().ImpliedType().AttributeTypes() {
		vals[name] = cty.NullVal(aType)
	}
	vals["interface"] = cty.StringVal("macbootbr0")
	vals["mac_address"] = cty.StringVal("54:05:ab:01:02:03")
	vals["address"] = cty.StringVal("172.30.99.2/24")
	vals["target_interface"] = cty.StringVal("ether2")
	vals["username"] = cty.StringVal("admin")
	vals["password_wo"] = cty.StringVal("write-only-secret")
	vals["password_wo_version"] = cty.NumberIntVal(1)
	vals["allow_insecure_l2"] = cty.True
	vals["certificate_name"] = cty.StringVal("macboot-tls")
	vals["enable_api_ssl"] = cty.False
	vals["timeout"] = cty.StringVal("30s")

	d := res.Data(&terraform.InstanceState{
		RawConfig: cty.ObjectVal(vals),
		Attributes: map[string]string{
			"interface":           "macbootbr0",
			"mac_address":         "54:05:ab:01:02:03",
			"address":             "172.30.99.2/24",
			"target_interface":    "ether2",
			"username":            "admin",
			"password_wo_version": "1",
			"allow_insecure_l2":   "true",
			"certificate_name":    "macboot-tls",
			"enable_api_ssl":      "false",
			"timeout":             "30s",
		},
	})

	diags := res.CreateContext(context.Background(), d, &unconfiguredClient{})
	if diags.HasError() {
		t.Fatalf("unexpected CreateContext error: %+v", diags)
	}
	if seenPassword != "write-only-secret" {
		t.Fatalf("Bootstrap did not receive password_wo value; got %q", seenPassword)
	}
	if d.Get("password_wo").(string) != "" {
		t.Fatalf("password_wo must remain empty in ResourceData state")
	}
	if d.Get("tls_fingerprint").(string) != fp || d.Get("router_address").(string) != "172.30.99.2" {
		t.Fatalf("unexpected outputs: fp=%q addr=%q", d.Get("tls_fingerprint"), d.Get("router_address"))
	}

	var warnCount int
	var sawCleartextWarn, sawTOFUWarn bool
	for _, dg := range diags {
		if dg.Severity == diag.Warning {
			warnCount++
			msg := dg.Summary + " " + dg.Detail
			if strings.Contains(msg, "cleartext") {
				sawCleartextWarn = true
			}
			if strings.Contains(msg, "Rotate the router admin password") {
				sawTOFUWarn = true
			}
		}
	}
	if warnCount < 2 || !sawCleartextWarn || !sawTOFUWarn {
		t.Fatalf("expected cleartext + TOFU warnings, got %+v", diags)
	}

	// Read and Delete must be no-op on L2.
	if rDiags := res.ReadContext(context.Background(), d, &unconfiguredClient{}); rDiags.HasError() {
		t.Fatalf("ReadContext failed: %+v", rDiags)
	}
	if dDiags := res.DeleteContext(context.Background(), d, &unconfiguredClient{}); dDiags.HasError() {
		t.Fatalf("DeleteContext failed: %+v", dDiags)
	}
	if d.Id() != "" {
		t.Fatalf("expected empty Id after DeleteContext, got %q", d.Id())
	}
}
