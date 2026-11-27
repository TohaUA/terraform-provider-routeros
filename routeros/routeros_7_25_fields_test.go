package routeros

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func api725Resolve(s map[string]*schema.Schema, wire string) (string, *schema.Schema) {
	if f := s[KebabToSnake(wire)]; f != nil {
		return KebabToSnake(wire), f
	}
	if ts := s[MetaTransformSet]; ts != nil {
		for from, to := range loadTransformSet(ts.Default.(string), false) {
			if to == wire && s[from] != nil {
				return from, s[from]
			}
		}
	}
	if path := s[MetaResourcePath]; path != nil {
		for from, to := range driftAttributeSlice.GetDriftMap(path.Default.(string), false) {
			if to == wire && s[from] != nil {
				return from, s[from]
			}
		}
	}
	if prefix, _, ok := strings.Cut(wire, "."); ok {
		if f := s[KebabToSnake(prefix)]; f != nil && f.Type == schema.TypeMap {
			return KebabToSnake(prefix), f
		}
	}
	return "", nil
}

// Presence alone is insufficient: verify every new scalar can be read, every
// configurable one reaches its exact wire key, and status never reaches writes.
func TestRouterOS725DeltaFieldSerialization(t *testing.T) {
	var manifest struct {
		Delta struct {
			Changes []struct {
				Path  string
				Added struct{ Params []string }
			} `json:"changedCommands"`
		} `json:"delta"`
	}
	raw, err := os.ReadFile("../tools/schema_changes/routeros-7.25-api.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	p := Provider()
	resources := map[string]*schema.Resource{}
	datasources := map[string]*schema.Resource{}
	for _, r := range p.ResourcesMap {
		if path := r.Schema[MetaResourcePath]; path != nil {
			resources[path.Default.(string)] = r
		}
	}
	for _, r := range p.DataSourcesMap {
		if path := r.Schema[MetaResourcePath]; path != nil {
			datasources[path.Default.(string)] = r
		}
	}
	for _, change := range manifest.Delta.Changes {
		for _, wire := range change.Added.Params {
			t.Run(change.Path+"/"+wire, func(t *testing.T) {
				path := "/" + change.Path
				r := resources[path]
				ds := false
				if r == nil {
					r = datasources[path]
					ds = true
				}
				if r == nil {
					t.Fatal("unregistered menu")
				}
				s := r.Schema
				if ds {
					s = make(map[string]*schema.Schema)
					for k, v := range r.Schema["data"].Elem.(*schema.Resource).Schema {
						s[k] = v
					}
					s[MetaTransformSet] = r.Schema[MetaTransformSet]
				}
				name, f := api725Resolve(s, wire)
				if f == nil {
					t.Fatal("missing property")
				}
				value := "sample"
				var state interface{} = "sample"
				var config = cty.StringVal("sample")
				switch f.Type {
				case schema.TypeBool:
					value = "yes"
					state = true
					config = cty.True
				case schema.TypeInt:
					value = "7"
					state = 7
					config = cty.NumberIntVal(7)
				case schema.TypeSet:
					value = "sample"
					state = []interface{}{"sample"}
					config = cty.SetVal([]cty.Value{cty.StringVal("sample")})
				case schema.TypeMap:
					_, tail, _ := strings.Cut(wire, ".")
					value = "7"
					state = map[string]interface{}{KebabToSnake(tail): value}
					config = cty.MapVal(map[string]cty.Value{KebabToSnake(tail): cty.StringVal(value)})
				}
				if ds {
					d := r.TestResourceData()
					items := []MikrotikItem{{wire: value}}
					if diags := MikrotikResourceDataToTerraformDatasource(&items, "data", r.Schema, d); len(diags) != 0 {
						t.Fatal(diags)
					}
					if len(d.Get("data").([]interface{})) != 1 {
						t.Fatal("missing row")
					}
					got := d.Get("data").([]interface{})[0].(map[string]interface{})[name]
					if set, ok := got.(*schema.Set); ok {
						got = set.List()
					}
					if !reflect.DeepEqual(got, state) {
						t.Fatalf("read %s=%#v, want %#v", wire, got, state)
					}
					return
				}
				d := resourceDataWithRawConfig(r, "*1", map[string]cty.Value{name: config})
				if err := d.Set(name, state); err != nil {
					t.Fatal(err)
				}
				item, _ := TerraformResourceDataToMikrotik(r.Schema, d)
				if f.Computed && !f.Optional {
					if _, ok := item[wire]; ok {
						t.Fatalf("status sent as configuration: %s", wire)
					}
				} else if item[wire] != value {
					t.Fatalf("wire %s=%q, want %q", wire, item[wire], value)
				}
				read := r.TestResourceData()
				if diags := MikrotikResourceDataToTerraform(MikrotikItem{wire: value}, r.Schema, read); len(diags) != 0 {
					t.Fatal(diags)
				}
				got := read.Get(name)
				if set, ok := got.(*schema.Set); ok {
					got = set.List()
				}
				if !reflect.DeepEqual(got, state) {
					t.Fatalf("read %s=%#v, want %#v", wire, got, state)
				}
			})
		}
	}
}
