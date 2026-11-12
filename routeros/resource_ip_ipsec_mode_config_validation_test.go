package routeros

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestIpsecModeConfigSplitDNSValidation(t *testing.T) {
	for _, tt := range []struct {
		name      string
		splitDNS  []interface{}
		address   string
		include   []interface{}
		wantError bool
	}{
		{name: "public domain", splitDNS: []interface{}{"example.com"}},
		{name: "private domain", splitDNS: []interface{}{"corp.local"}},
		{name: "multiple domains", splitDNS: []interface{}{"corp.local", "vpn.example.com"}},
		{name: "hyphenated and internationalized domains", splitDNS: []interface{}{"internal-app.example", "xn--bcher-kva.example"}},
		{name: "legacy address-shaped string", splitDNS: []interface{}{"1.1.1.1"}},
		{name: "address remains IPv4 validated", splitDNS: []interface{}{}, address: "corp.local", wantError: true},
		{name: "split include remains CIDR validated", splitDNS: []interface{}{}, include: []interface{}{"corp.local"}, wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config := map[string]interface{}{"name": "split-dns", "split_dns": tt.splitDNS}
			if tt.address != "" {
				config["address"] = tt.address
			}
			if tt.include != nil {
				config["split_include"] = tt.include
			}
			diagnostics := ResourceIpIpsecModeConfig().Validate(terraform.NewResourceConfigRaw(config))
			if diagnostics.HasError() != tt.wantError {
				t.Errorf("validation errors = %t, want %t: %v", diagnostics.HasError(), tt.wantError, diagnostics)
			}
		})
	}
}
