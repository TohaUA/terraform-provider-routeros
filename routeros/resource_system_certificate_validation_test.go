package routeros

import (
	"testing"

	"github.com/hashicorp/go-cty/cty"
)

// The trust-store value list tracks the MikroTik Certificates manual
// (manual.mikrotik.com/docs/authentication-authorization-accounting/certificates/).
func TestSystemCertificateTrustStoreValidation(t *testing.T) {
	validate := ResourceSystemCertificate().Schema["trust_store"].ValidateDiagFunc
	if validate == nil {
		t.Fatal("trust_store has no ValidateDiagFunc")
	}

	cases := []struct {
		value    string
		hasError bool
	}{
		{"all", false},
		{"www", false},
		{"sstp", false},
		{"wiliot", false},
		{"logging", false},
		{"www,sstp", false},
		{"bogus", true},
		{"www,bogus", true},
		{"", true},
	}
	for _, c := range cases {
		result := validate(c.value, *new(cty.Path))
		if result.HasError() != c.hasError {
			t.Errorf("trust_store validation of %q: hasError = %t, want %t. Diagnostics: %v.",
				c.value, result.HasError(), c.hasError, result)
		}
	}
}
