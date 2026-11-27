package routeros

import (
	"context"
	"testing"
)

// Existing states recorded the injected default; updates PATCH the whole item from state, so the
// upgrader has to clear it while leaving a user-chosen value alone.
func Test_stateMigrationClearInjectedDefault(t *testing.T) {
	upgrade := stateMigrationClearInjectedDefault("legacy_field", "none")

	for _, tc := range []struct {
		name     string
		in, want interface{}
	}{
		{"injected default is cleared", "none", ""},
		{"explicit value is kept", "all", "all"},
		{"missing key stays missing", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := map[string]interface{}{"name": "test"}
			if tc.in != nil {
				raw["legacy_field"] = tc.in
			}

			out, err := upgrade(context.Background(), raw, nil)
			if err != nil {
				t.Fatalf("upgrade: %v", err)
			}
			if got := out["legacy_field"]; got != tc.want {
				t.Fatalf("legacy_field: got %#v, want %#v", got, tc.want)
			}
			if out["name"] != "test" {
				t.Fatalf("other attributes must be untouched, got %v", out)
			}
		})
	}
}
