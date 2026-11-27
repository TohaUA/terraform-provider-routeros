package routeros

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/hashicorp/go-cty/cty"
)

func TestRouterOS725WifiProfileFeatures(t *testing.T) {
	r := ResourceWifiConfiguration()
	for _, tc := range []struct {
		field, wire string
		accepted    []string
		rejected    []string
	}{
		{"mode", "mode", []string{"ap", "station", "station-bridge", "station-pseudobridge"}, []string{"invalid"}},
		{"distance", "distance", []string{"0", "+1", ".1", "0.1", "1.5", "100", "30000", "4294967.295", "indoors", "dynamic"}, []string{"-1", "1.", "far", "4294967.296", "4294967295", "NaN", "Inf"}},
		{"hw_protection_mode", "hw-protection-mode", []string{"none", "rts-cts", "cts-to-self"}, []string{"invalid"}},
	} {
		t.Run(tc.field, func(t *testing.T) {
			field := r.Schema[tc.field]
			if field == nil {
				t.Fatalf("missing WiFi profile feature %s", tc.field)
			}
			for _, value := range tc.accepted {
				if _, errs := field.ValidateFunc(value, tc.field); len(errs) != 0 {
					t.Errorf("valid %s rejected: %v", value, errs)
				}
				d := resourceDataWithRawConfig(r, "*1", map[string]cty.Value{tc.field: cty.StringVal(value)})
				item, _ := TerraformResourceDataToMikrotik(r.Schema, d)
				if item[tc.wire] != value {
					t.Fatalf("wire value = %q, want %q", item[tc.wire], value)
				}
				read := r.TestResourceData()
				if diags := MikrotikResourceDataToTerraform(MikrotikItem{tc.wire: value}, r.Schema, read); diags.HasError() {
					t.Fatal(diags)
				}
				if !reflect.DeepEqual(read.Get(tc.field), value) {
					t.Fatalf("read value = %v, want %s", read.Get(tc.field), value)
				}
			}
			for _, value := range tc.rejected {
				if _, errs := field.ValidateFunc(value, tc.field); len(errs) == 0 {
					t.Errorf("invalid %q accepted", value)
				}
			}
		})
	}
}

func TestRouterOS725WifiReferenceVariants(t *testing.T) {
	var manifest struct {
		Menus map[string]struct {
			Fields []struct {
				Wire     string `json:"wire"`
				Variants []struct {
					Type string `json:"type"`
				} `json:"api_variants"`
			} `json:"fields"`
		} `json:"menus"`
	}
	raw, err := os.ReadFile("../tools/schema_changes/routeros-7.25-api.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, f := range manifest.Menus["interface/wifi/configuration"].Fields {
		if f.Wire != "distance" && f.Wire != "mode" && f.Wire != "hw-protection-mode" {
			continue
		}
		if len(f.Variants) != 2 {
			t.Errorf("%s: expected both documented driver variants, got %v", f.Wire, f.Variants)
		}
	}
	var distance api725Field
	for _, f := range routerOS725Reference["/interface/wifi/configuration"] {
		if f.Wire == "distance" {
			distance = f
		}
	}
	if !distance.NumericAlternative {
		t.Fatal("distance lost its numeric variant")
	}
	validate := api725Property(distance).ValidateFunc
	for _, value := range []string{"0.1", "1.5", "100", "4294967.295", "indoors", "dynamic"} {
		if _, errs := validate(value, "distance"); len(errs) != 0 {
			t.Fatal(errs)
		}
	}
	for _, value := range []string{"-1", "4294967.296", "4294967295", "far"} {
		if _, errs := validate(value, "distance"); len(errs) == 0 {
			t.Errorf("invalid distance %q accepted", value)
		}
	}
}

func TestRouterOS725WifiDistanceCanonicalization(t *testing.T) {
	r := ResourceWifiConfiguration()
	for _, tc := range []struct {
		old, next string
		equal     bool
	}{
		{"0.1", "0.100", true}, {"1.234", "1.2345", true},
		{"1", "+1", true}, {"0.1", ".1", true}, {"1", "1.", false},
		{"4294967.295", "4294967.2959", true}, {"0", "0.0009", true},
		{"1", "1.001", false}, {"4294967.295", "4294967.296", false},
		{"indoors", "dynamic", false}, {"0", "invalid", false},
	} {
		d := resourceDataWithRawConfig(r, "*1", map[string]cty.Value{"distance": cty.StringVal(tc.next)})
		if got := r.Schema["distance"].DiffSuppressFunc("distance", tc.old, tc.next, d); got != tc.equal {
			t.Errorf("%s -> %s: equal=%v, want %v", tc.old, tc.next, got, tc.equal)
		}
	}
}
