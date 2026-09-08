package main

import (
	"strings"
	"testing"

	"github.com/terraform-routeros/terraform-provider-routeros/routeros"
)

// Resources that legitimately have no RouterOS menu.
var noMenuAllowlist = map[string]struct{}{
	"routeros_wireguard_keys": {}, // generates key pairs locally, path "local"
}

// Every resource must declare a menu path via MetaResourcePath, otherwise the drift tool silently
// skips it. Add a resource to noMenuAllowlist only when it truly has no menu.
func TestEveryResourceMapsToAMenu(t *testing.T) {
	mapping := ProviderResources(routeros.Provider())
	if len(mapping) < 200 {
		t.Fatalf("only %d resources found, expected the full provider", len(mapping))
	}
	for name, r := range mapping {
		if _, ok := noMenuAllowlist[name]; ok {
			if r.HasMenu() {
				t.Errorf("%s is allowlisted as having no menu but declares %q; drop it from the allowlist", name, r.Path)
			}
			continue
		}
		if !r.HasMenu() {
			t.Errorf("%s has no RouterOS menu path (MetaResourcePath=%q)", name, r.Path)
		}
		if r.IdType != int(routeros.Id) && r.IdType != int(routeros.Name) {
			t.Errorf("%s has an unexpected id type %d", name, r.IdType)
		}
		if strings.ContainsAny(r.Path, " \t") || strings.HasSuffix(r.Path, "/") {
			t.Errorf("%s has a malformed menu path %q", name, r.Path)
		}
		if len(r.Attrs) == 0 {
			t.Errorf("%s has no attributes", name)
		}
		for _, a := range r.Attrs {
			if isMetaAttr(a.Name) {
				t.Errorf("%s: meta attribute %s leaked into the attribute list", name, a.Name)
			}
		}
	}
}

func TestKnownMappings(t *testing.T) {
	mapping := ProviderResources(routeros.Provider())
	cases := map[string]string{
		"routeros_interface_bridge":       "/interface/bridge",
		"routeros_bridge":                 "/interface/bridge",
		"routeros_ip_service":             "/ip/service",
		"routeros_ip_settings":            "/ip/settings",
		"routeros_routing_bgp_connection": "/routing/bgp/connection",
	}
	for name, want := range cases {
		r, ok := mapping[name]
		if !ok {
			t.Errorf("%s not in mapping", name)
			continue
		}
		if r.Path != want {
			t.Errorf("%s: path %q, want %q", name, r.Path, want)
		}
	}
	if mapping["routeros_ip_service"].IdType != int(routeros.Name) {
		t.Errorf("routeros_ip_service should be keyed by name (Name id type)")
	}
	// Nested blocks are flattened to dotted names, as RouterOS reports them.
	found := false
	for _, a := range mapping["routeros_routing_bgp_connection"].Attrs {
		if a.Name == "output.default_originate" {
			found = true
		}
	}
	if !found {
		t.Errorf("routeros_routing_bgp_connection: nested block attribute output.default_originate not flattened")
	}
	// CAPsMAN-style inline settings are maps.
	for _, a := range mapping["routeros_capsman_configuration"].Attrs {
		if a.Name == "channel" && a.Type != "map" {
			t.Errorf("routeros_capsman_configuration.channel: type %q, want map", a.Type)
		}
	}
}

func TestGroupByMenuAliases(t *testing.T) {
	groups, noMenu := GroupByMenu(ProviderResources(routeros.Provider()))
	byPath := map[string]*MenuGroup{}
	for _, g := range groups {
		byPath[g.Path] = g
	}
	g, ok := byPath["/interface/bridge"]
	if !ok {
		t.Fatal("/interface/bridge group missing")
	}
	if got := strings.Join(g.Names(), ","); got != "routeros_bridge,routeros_interface_bridge" {
		t.Errorf("/interface/bridge aliases: %s", got)
	}
	if d := g.AliasDrift(); len(d) != 0 {
		t.Errorf("/interface/bridge aliases should share a schema, got drift %v", d)
	}
	if len(noMenu) != len(noMenuAllowlist) {
		t.Errorf("resources without menu: %d, allowlist has %d", len(noMenu), len(noMenuAllowlist))
	}
	if !g.MatchesFilter([]string{"routeros_bridge"}) || !g.MatchesFilter([]string{"/interface/bridge"}) ||
		g.MatchesFilter([]string{"/ip/service"}) || !g.MatchesFilter(nil) {
		t.Errorf("MatchesFilter mismatch")
	}
}

func TestAliasDriftDetectsDivergence(t *testing.T) {
	g := &MenuGroup{Path: "/x", Resources: []*Resource{
		{Name: "routeros_a", Attrs: []Attr{{Name: "one"}, {Name: "two"}}},
		{Name: "routeros_b", Attrs: []Attr{{Name: "one"}, {Name: "three"}}},
	}}
	got := strings.Join(g.AliasDrift(), " ")
	if got != "+three (routeros_b) -two (routeros_b)" {
		t.Errorf("AliasDrift = %q", got)
	}
}

func TestParseSchemaJSON(t *testing.T) {
	raw := []byte(`{"provider_schemas":{"registry.terraform.io/terraform-routeros/routeros":{"resource_schemas":{
	  "routeros_x":{"block":{"attributes":{
	     "___path___":{"type":"string","optional":true},
	     "id":{"type":"string","computed":true},
	     "name":{"type":"string","required":true},
	     "running":{"type":"bool","computed":true},
	     "channel":{"type":["map","string"],"optional":true},
	     "tags":{"type":["set","string"],"optional":true}},
	   "block_types":{
	     "output":{"nesting_mode":"list","block":{"attributes":{"add_path":{"type":"string","optional":true}}}},
	     "timeouts":{"nesting_mode":"single","block":{"attributes":{"create":{"type":"string","optional":true}}}}}}}}}}}`)
	got, err := parseSchemaJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	attrs := got["routeros_x"]
	want := []Attr{
		{Name: "channel", Type: "map", Optional: true},
		{Name: "id", Type: "string", Computed: true},
		{Name: "name", Type: "string", Required: true},
		{Name: "output.add_path", Type: "string", Optional: true},
		{Name: "running", Type: "bool", Computed: true},
		{Name: "tags", Type: "set", Optional: true},
	}
	if len(attrs) != len(want) {
		t.Fatalf("got %d attrs %+v, want %d", len(attrs), attrs, len(want))
	}
	for i := range want {
		if attrs[i] != want[i] {
			t.Errorf("attr %d: got %+v, want %+v", i, attrs[i], want[i])
		}
	}
	if !attrs[4].ReadOnly() || attrs[1].ReadOnly() == false || attrs[2].ReadOnly() {
		t.Errorf("ReadOnly classification wrong: %+v", attrs)
	}
}

// The JSON produced by `terraform providers schema -json` must flatten to the same attribute
// names as the compiled provider, otherwise -schema and the default mode would disagree.
func TestSDKAndJSONAttrShapesAgree(t *testing.T) {
	mapping := ProviderResources(routeros.Provider())
	r := mapping["routeros_routing_bgp_connection"]
	var nested, maps int
	for _, a := range r.Attrs {
		if strings.Contains(a.Name, ".") {
			nested++
		}
		if a.Type == "map" {
			maps++
		}
	}
	if nested == 0 {
		t.Errorf("expected nested attributes for bgp connection")
	}
	if maps != 0 {
		t.Errorf("bgp connection has %d map attributes, expected none", maps)
	}
}
