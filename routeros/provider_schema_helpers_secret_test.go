package routeros

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// A length validator on a sensitive field must not echo the value: the SDK copies the validator's error text
// into the diagnostic, so `validation.StringLenBetween` (which prints `got %s`) shows the secret in plain text
// on every `terraform plan` that rejects it.
func TestSecretLenValidatorDoesNotEchoValue(t *testing.T) {
	const hex64 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	cases := []struct {
		name   string
		schema map[string]*schema.Schema
		field  string
		value  string
		length string
	}{
		{"capsman_security", ResourceCapsManSecurity().Schema, "passphrase", hex64, "64"},
		{"capsman_security_v0", ResourceCapsManSecurityV0().Schema, "passphrase", hex64, "64"},
		{"wireless_security_profiles", ResourceInterfaceWirelessSecurityProfiles().Schema, "wpa_pre_shared_key", hex64 + "z", "65"},
		{"wireless_security_profiles", ResourceInterfaceWirelessSecurityProfiles().Schema, "wpa2_pre_shared_key", hex64 + "z", "65"},
		{"wifi_security_multi_passphrase", ResourceWifiSecurityMultiPassphrase().Schema, "passphrase", hex64 + "z", "65"},
		{"capsman_security_short", ResourceCapsManSecurity().Schema, "passphrase", "s3cr3t!", "7"},
	}

	for _, tc := range cases {
		t.Run(tc.name+"."+tc.field, func(t *testing.T) {
			fieldSchema := tc.schema[tc.field]
			if !fieldSchema.Sensitive {
				t.Fatalf("%s is not Sensitive", tc.field)
			}
			if fieldSchema.ValidateFunc == nil {
				t.Fatalf("%s has no ValidateFunc", tc.field)
			}

			_, errs := fieldSchema.ValidateFunc(tc.value, tc.field)
			if len(errs) == 0 {
				t.Fatalf("expected a length error for a %d-char value", len(tc.value))
			}

			for _, err := range errs {
				msg := err.Error()
				// No run of 4 or more consecutive characters of the value may appear in the error.
				for i := 0; i+4 <= len(tc.value); i++ {
					if part := tc.value[i : i+4]; strings.Contains(msg, part) {
						t.Errorf("error %q contains part %q of the secret value", msg, part)
						break
					}
				}
				if !strings.Contains(msg, tc.length) {
					t.Errorf("error %q does not report the length %s", msg, tc.length)
				}
			}
		})
	}
}

func TestValidationSecretLenBetween(t *testing.T) {
	v := ValidationSecretLenBetween(8, 63)
	cases := []struct {
		value    interface{}
		hasError bool
	}{
		{"1234567", true},
		{"12345678", false},
		{strings.Repeat("a", 63), false},
		{strings.Repeat("a", 64), true},
		{42, true},
	}
	for _, tc := range cases {
		_, errs := v(tc.value, "passphrase")
		if (len(errs) > 0) != tc.hasError {
			t.Errorf("value of length/type %T %v: got errors %v, want error %v", tc.value, tc.value, errs, tc.hasError)
		}
	}
}
