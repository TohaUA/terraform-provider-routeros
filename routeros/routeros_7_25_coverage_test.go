package routeros

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// The delta is taken independently from MikroTik's public cli-compare.json.
// Every newly documented property must reach a configuration schema, a prefix
// map, a read-only data source, or an executable action. No missing allowlist.
func TestRouterOS725FullDeltaCoverage(t *testing.T) {
	var manifest struct {
		Menus map[string]struct {
			Flags map[string]string `json:"flags"`
		} `json:"menus"`
		Delta struct {
			Changed []struct {
				Path  string                           `json:"path"`
				Added struct{ Params, Flags []string } `json:"added"`
			} `json:"changedCommands"`
			Added []string `json:"addedCommands"`
		} `json:"delta"`
	}
	data, err := os.ReadFile("../tools/schema_changes/routeros-7.25-api.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	p := Provider()
	byPath := map[string][]map[string]*schema.Schema{}
	for _, registry := range []map[string]*schema.Resource{p.ResourcesMap, p.DataSourcesMap} {
		for _, res := range registry {
			if path := res.Schema[MetaResourcePath]; path != nil {
				byPath[path.Default.(string)] = append(byPath[path.Default.(string)], res.Schema)
			}
		}
	}
	for _, change := range manifest.Delta.Changed {
		for _, wire := range change.Added.Params {
			t.Run(change.Path+"/"+wire, func(t *testing.T) {
				for _, s := range byPath["/"+change.Path] {
					if api725CoversWire(s, wire) {
						return
					}
				}
				t.Errorf("uncovered RouterOS 7.25 API property %s/%s", change.Path, wire)
			})
		}
		for _, symbol := range change.Added.Flags {
			wire := manifest.Menus[change.Path].Flags[symbol]
			covered := false
			for _, s := range byPath["/"+change.Path] {
				covered = covered || api725CoversWire(s, wire)
			}
			if wire == "" || !covered {
				t.Errorf("uncovered flag %s at %s (wire name %s)", symbol, change.Path, wire)
			}
		}
	}
	for _, path := range manifest.Delta.Added {
		// /ip/ipsec/key is a namespace containing the existing RSA/PSK command
		// menus, not an independently creatable or printable configuration item.
		if path == "ip/ipsec/key" {
			continue
		}
		if len(byPath["/"+path]) == 0 {
			t.Errorf("uncovered new API path /%s", path)
		}
	}
}

func api725CoversWire(s map[string]*schema.Schema, wire string) bool {
	if data := s["data"]; data != nil && data.Type == schema.TypeList {
		fields := data.Elem.(*schema.Resource).Schema
		copy := make(map[string]*schema.Schema, len(fields)+1)
		for k, f := range fields {
			copy[k] = f
		}
		if s[MetaTransformSet] != nil {
			copy[MetaTransformSet] = s[MetaTransformSet]
		}
		s = copy
	}
	if field := s[KebabToSnake(wire)]; field != nil {
		return true
	}
	if prefix, _, ok := strings.Cut(wire, "."); ok {
		if f := s[KebabToSnake(prefix)]; f != nil && f.Type == schema.TypeMap {
			return true
		}
	}
	if ts := s[MetaTransformSet]; ts != nil {
		for from, to := range loadTransformSet(ts.Default.(string), false) {
			if to == wire && s[from] != nil {
				return true
			}
		}
	}
	// Versioned renames preserve older Terraform attribute names.
	if path := s[MetaResourcePath]; path != nil {
		for name, target := range driftAttributeSlice.GetDriftMap(path.Default.(string), false) {
			if target == wire && s[name] != nil {
				return true
			}
		}
	}
	return false
}

func TestRouterOS725ProviderSchema(t *testing.T) {
	if err := Provider().InternalValidate(); err != nil {
		t.Fatal(err)
	}
}

func TestIPsecKey7251(t *testing.T) {
	r := ResourceIpIpsecKey()
	if r.Schema[MetaResourcePath].Default != "/ip/ipsec/key/rsa" {
		t.Fatal("RSA operations must use the RouterOS 7.25.1 menu")
	}
	validate := r.Schema["key_size"].ValidateFunc
	for _, size := range []int{2048, 4096, 8192} {
		if _, errs := validate(size, "key_size"); len(errs) != 0 {
			t.Fatal(errs)
		}
	}
	if _, errs := validate(1024, "key_size"); len(errs) == 0 {
		t.Fatal("unsupported 1024-bit RSA key accepted")
	}
}

func TestBGPAddressFamilies725(t *testing.T) {
	for _, r := range []*schema.Resource{ResourceRoutingBgpConnection(), ResourceRoutingBgpTemplate()} {
		f := r.Schema["address_families"]
		if diags := f.ValidateDiagFunc("ip,ipv6,vpnv6,evpn", nil); diags.HasError() {
			t.Fatal(diags)
		}
		if diags := f.ValidateDiagFunc("invalid-family", nil); !diags.HasError() {
			t.Fatal("unknown BGP family accepted")
		}
	}
}
