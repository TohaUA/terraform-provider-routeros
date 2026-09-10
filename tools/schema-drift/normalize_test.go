package main

import (
	"strings"
	"testing"
)

func rowsByField(c *Comparison) map[string]Row {
	out := map[string]Row{}
	for _, r := range c.Fields {
		out[r.Field] = r
	}
	return out
}

// compareOne compares a group that has a single schema and returns its only comparison.
func compareOne(t *testing.T, cmp *Comparer, g *MenuGroup, items []map[string]string) (*Comparison, error) {
	t.Helper()
	got, err := cmp.Compare(g, items)
	if err != nil {
		return nil, err
	}
	if len(got) != 1 {
		t.Fatalf("%s: %d comparisons, want 1", g.Path, len(got))
	}
	return got[0], nil
}

func TestCollectKeys(t *testing.T) {
	items := []map[string]string{
		{".id": "*1", ".nextid": "*2", "name": "a", "ret": "x", "port": "22"},
		{".id": "*2", "name": "b", "dynamic": "true", "connection": "1.1.1.1"},
	}
	keys, dyn := CollectKeys(items)
	if dyn != 1 {
		t.Errorf("dynamic rows = %d, want 1", dyn)
	}
	if _, ok := keys[".id"]; ok {
		t.Errorf(".id must be dropped")
	}
	if _, ok := keys["ret"]; ok {
		t.Errorf("ret must be dropped")
	}
	if !keys["name"].static || keys["connection"].static {
		t.Errorf("static tracking wrong: name=%v connection=%v", keys["name"].static, keys["connection"].static)
	}
}

func TestCompareBasicClasses(t *testing.T) {
	g := &MenuGroup{Path: "/interface/bridge", Resources: []*Resource{{
		Name:   "routeros_interface_bridge",
		Path:   "/interface/bridge",
		IdType: 1,
		Attrs: []Attr{
			{Name: "id", Type: "string", Computed: true},
			{Name: "name", Type: "string", Required: true},
			{Name: "fast_forward", Type: "bool", Optional: true, Computed: true},
			{Name: "running", Type: "bool", Computed: true},
			{Name: "actual_mtu", Type: "number", Computed: true},
			{Name: "igmp_snooping", Type: "bool", Optional: true}, // not returned by the device
			{Name: "place_before", Type: "string", Optional: true},
		},
	}}}
	items := []map[string]string{{
		".id": "*1", "name": "bridge", "fast-forward": "true", "running": "true", "actual-mtu": "1500",
		"dhcpv6-snooping": "false", "managed": "false", "dynamic": "false", "invalid": "false",
	}}
	c := NewComparer("7.24", nil, nil)
	got, err := compareOne(t, c, g, items)
	if err != nil {
		t.Fatal(err)
	}
	rows := rowsByField(got)
	want := map[string]string{
		"name": ClassCovered, "fast-forward": ClassCovered, "running": ClassCovered, "actual-mtu": ClassCovered,
		"dhcpv6-snooping": ClassMissing, "managed": ClassMissing,
		"dynamic": ClassReadOnly, "invalid": ClassReadOnly,
		"igmp-snooping": ClassSchemaOnly, "place-before": ClassSchemaOnly,
	}
	for field, class := range want {
		r, ok := rows[field]
		if !ok {
			t.Errorf("no row for %s", field)
			continue
		}
		if r.Class != class {
			t.Errorf("%s: class %s, want %s", field, r.Class, class)
		}
		if r.Writable != "unknown" {
			t.Errorf("%s: writable %q without inspect, want unknown", field, r.Writable)
		}
	}
	if _, ok := rows["id"]; ok {
		t.Errorf("id attribute must not be reported")
	}
	if rows["dhcpv6-snooping"].Attr != "dhcpv6_snooping" {
		t.Errorf("kebab->snake: %q", rows["dhcpv6-snooping"].Attr)
	}
	if rows["running"].Note != "computed-only in provider" {
		t.Errorf("running note = %q", rows["running"].Note)
	}
	if got.Count(ClassMissing) != 2 || got.Count(ClassSchemaOnly) != 2 || got.Count(ClassReadOnly) != 2 {
		t.Errorf("counts: missing %d schema-only %d read-only %d", got.Count(ClassMissing), got.Count(ClassSchemaOnly), got.Count(ClassReadOnly))
	}
	// Ordering: missing first, then read-only, schema-only, covered.
	if got.Fields[0].Class != ClassMissing || got.Fields[len(got.Fields)-1].Class != ClassCovered {
		t.Errorf("rows not ordered by class: %v", got.Fields)
	}
}

func TestCompareWithInspectAndStatusFields(t *testing.T) {
	inspect, err := ParseInspect(strings.NewReader(`{"interface":{"_type":"dir","bridge":{"_type":"dir",
	 "add":{"_type":"cmd","name":{"_type":"arg"},"dhcpv6-snooping":{"_type":"arg"},"copy-from":{"_type":"arg"}},
	 "set":{"_type":"cmd","numbers":{"_type":"arg"},"mlag-priority":{"_type":"arg"}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	g := &MenuGroup{Path: "/interface/bridge", Resources: []*Resource{{
		Name: "routeros_interface_bridge", Path: "/interface/bridge",
		Attrs: []Attr{{Name: "name", Type: "string", Required: true}, {Name: "igmp_snooping", Type: "bool", Optional: true}},
	}}}
	items := []map[string]string{{"name": "b", "dhcpv6-snooping": "false", "mlag-priority": "128", "managed": "false", "l2mtu": "1500", "custom": "x"}}
	c := NewComparer("7.24", inspect, []string{"custom"})
	got, err := compareOne(t, c, g, items)
	if err != nil {
		t.Fatal(err)
	}
	rows := rowsByField(got)
	if rows["dhcpv6-snooping"].Class != ClassMissing || rows["dhcpv6-snooping"].Writable != "yes" {
		t.Errorf("dhcpv6-snooping: %+v", rows["dhcpv6-snooping"])
	}
	if rows["mlag-priority"].Class != ClassMissing || rows["mlag-priority"].Writable != "yes" {
		t.Errorf("mlag-priority (set-only arg): %+v", rows["mlag-priority"])
	}
	if rows["managed"].Class != ClassReadOnly || rows["managed"].Writable != "no" {
		t.Errorf("managed (not an add/set arg): %+v", rows["managed"])
	}
	if rows["l2mtu"].Class != ClassReadOnly {
		t.Errorf("l2mtu: %+v", rows["l2mtu"])
	}
	if rows["custom"].Class != ClassReadOnly {
		t.Errorf("extra status field: %+v", rows["custom"])
	}
	if rows["igmp-snooping"].Class != ClassSchemaOnly || rows["igmp-snooping"].Writable != "no" ||
		!strings.Contains(rows["igmp-snooping"].Note, "removal") {
		t.Errorf("igmp-snooping: %+v", rows["igmp-snooping"])
	}
	if rows["name"].Writable != "yes" {
		t.Errorf("name: %+v", rows["name"])
	}

	// Menu absent from the inspect tree -> writable unknown, status list still applies.
	g.Path = "/zerotier"
	got, _ = compareOne(t, c, g, []map[string]string{{"name": "z", "dynamic": "true", "foo": "1"}})
	rows = rowsByField(got)
	if rows["foo"].Class != ClassMissing || rows["foo"].Writable != "unknown" || !rows["foo"].DynamicOnly {
		t.Errorf("foo: %+v", rows["foo"])
	}
	if rows["dynamic"].Class != ClassReadOnly {
		t.Errorf("dynamic: %+v", rows["dynamic"])
	}
	if got.DynamicRows != 1 {
		t.Errorf("dynamic rows = %d", got.DynamicRows)
	}
}

func TestCompareTransformSetMapsAndBlocks(t *testing.T) {
	c := NewComparer("7.24", nil, nil)

	// CAPsMAN: "channel.config: channel" plus map attributes for channel.* keys.
	g := &MenuGroup{Path: "/caps-man/configuration", Resources: []*Resource{{
		Name: "routeros_capsman_configuration", Path: "/caps-man/configuration",
		TransformSet: `"channel.config: channel","datapath.config: datapath"`,
		Attrs: []Attr{
			{Name: "name", Type: "string", Required: true},
			{Name: "channel", Type: "map", Optional: true},
			{Name: "datapath", Type: "map", Optional: true},
		},
	}}}
	got, err := compareOne(t, c, g, []map[string]string{{"name": "c", "channel": "ch1", "channel.band": "2ghz-b", "channel.width": "20"}})
	if err != nil {
		t.Fatal(err)
	}
	rows := rowsByField(got)
	for _, f := range []string{"channel", "channel.band", "channel.width"} {
		if rows[f].Class != ClassCovered || rows[f].Attr != "channel" {
			t.Errorf("%s: %+v", f, rows[f])
		}
	}
	if rows["datapath"].Class != ClassSchemaOnly {
		t.Errorf("datapath map not returned: %+v", rows["datapath"])
	}

	// Wireless: "basic_rates_ag: basic-rates-a/g".
	g = &MenuGroup{Path: "/interface/wireless", Resources: []*Resource{{
		Name: "routeros_interface_wireless", Path: "/interface/wireless",
		TransformSet: `"basic_rates_ag: basic-rates-a/g"`,
		SkipFields:   `".about","pci_info"`,
		Attrs:        []Attr{{Name: "basic_rates_ag", Type: "string", Optional: true}, {Name: "name", Type: "string"}},
	}}}
	got, _ = compareOne(t, c, g, []map[string]string{{"name": "w", "basic-rates-a/g": "6Mbps", "pci-info": "x"}})
	rows = rowsByField(got)
	if rows["basic-rates-a/g"].Class != ClassCovered || rows["basic-rates-a/g"].Attr != "basic_rates_ag" {
		t.Errorf("basic-rates-a/g: %+v", rows["basic-rates-a/g"])
	}
	if rows["pci-info"].Class != ClassSkipped {
		t.Errorf("pci-info: %+v", rows["pci-info"])
	}

	// Zerotier: "identity_public: identity.public" (dotted device key flattened by the transform set).
	g = &MenuGroup{Path: "/zerotier", Resources: []*Resource{{
		Name: "routeros_zerotier", Path: "/zerotier",
		TransformSet: `"identity_public: identity.public"`,
		Attrs:        []Attr{{Name: "identity_public", Type: "string", Computed: true}, {Name: "name", Type: "string"}},
	}}}
	got, _ = compareOne(t, c, g, []map[string]string{{"name": "z", "identity.public": "abc"}})
	rows = rowsByField(got)
	if rows["identity.public"].Class != ClassCovered || rows["identity.public"].Attr != "identity_public" {
		t.Errorf("identity.public: %+v", rows["identity.public"])
	}
	if got.Count(ClassSchemaOnly) != 0 {
		t.Errorf("reverse transform for schema-only failed: %v", got.Fields)
	}

	// BGP: dotted keys map onto nested blocks; an unknown sub-field is missing, not covered.
	g = &MenuGroup{Path: "/routing/bgp/connection", Resources: []*Resource{{
		Name: "routeros_routing_bgp_connection", Path: "/routing/bgp/connection",
		Attrs: []Attr{
			{Name: "name", Type: "string", Required: true},
			{Name: "output.default_originate", Type: "string", Optional: true},
			{Name: "output.network", Type: "string", Optional: true},
		},
	}}}
	got, _ = compareOne(t, c, g, []map[string]string{{"name": "b", "output.default-originate": "never", "output.add-path": "all"}})
	rows = rowsByField(got)
	if rows["output.default-originate"].Class != ClassCovered || rows["output.default-originate"].Attr != "output.default_originate" {
		t.Errorf("output.default-originate: %+v", rows["output.default-originate"])
	}
	if rows["output.add-path"].Class != ClassMissing || rows["output.add-path"].Attr != "output.add_path" {
		t.Errorf("output.add-path: %+v", rows["output.add-path"])
	}
	if rows["output.network"].Class != ClassSchemaOnly {
		t.Errorf("output.network: %+v", rows["output.network"])
	}
}

func TestCompareAppliesVersionDrift(t *testing.T) {
	// /ip/dhcp-server: src_address <-> server-address since 7.1 (mikrotik_resource_drift.go).
	g := &MenuGroup{Path: "/ip/dhcp-server", Resources: []*Resource{{
		Name: "routeros_ip_dhcp_server", Path: "/ip/dhcp-server",
		Attrs: []Attr{{Name: "name", Type: "string", Required: true}, {Name: "src_address", Type: "string", Optional: true}},
	}}}
	items := []map[string]string{{"name": "d", "server-address": "10.0.0.1"}}

	got, err := compareOne(t, NewComparer("7.24", nil, nil), g, items)
	if err != nil {
		t.Fatal(err)
	}
	rows := rowsByField(got)
	if rows["server-address"].Class != ClassCovered || rows["server-address"].Attr != "src_address" {
		t.Errorf("7.24 server-address: %+v", rows["server-address"])
	}
	if got.Count(ClassSchemaOnly) != 0 {
		t.Errorf("7.24 src_address should be matched via the drift map: %v", got.Fields)
	}

	got, _ = compareOne(t, NewComparer("7.0", nil, nil), g, items)
	rows = rowsByField(got)
	if rows["server-address"].Class != ClassMissing || rows["src-address"].Class != ClassSchemaOnly {
		t.Errorf("7.0 must not apply the 7.1 rename: %v", got.Fields)
	}

	if _, err := NewComparer("not-a-version", nil, nil).Compare(g, items); err == nil {
		t.Errorf("bad version must be an error")
	}
}

func TestCompareInspectOnlyPassAndEmptyMenus(t *testing.T) {
	inspect, err := ParseInspect(strings.NewReader(`{"ip":{"_type":"dir","dhcp-server":{"_type":"dir",
	 "add":{"_type":"cmd","name":{"_type":"arg"},"add-dns-entries":{"_type":"arg"},"place-before":{"_type":"arg"},
	         "lease-time":{"_type":"arg"},"server-address":{"_type":"arg"},"copy-from":{"_type":"arg"}},
	 "set":{"_type":"cmd","numbers":{"_type":"arg"},"comment":{"_type":"arg"}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	g := &MenuGroup{Path: "/ip/dhcp-server", Resources: []*Resource{{
		Name: "routeros_ip_dhcp_server", Path: "/ip/dhcp-server",
		Attrs: []Attr{
			{Name: "name", Type: "string", Required: true},
			{Name: "lease_time", Type: "string", Optional: true},
			{Name: "src_address", Type: "string", Optional: true}, // server-address via the drift map
			{Name: "comment", Type: "string", Optional: true},
		},
	}}}
	c := NewComparer("7.24", inspect, nil)

	// Device has one item that does not set lease-time or comment; add-dns-entries is unset too.
	got, err := compareOne(t, c, g, []map[string]string{{"name": "d"}})
	if err != nil {
		t.Fatal(err)
	}
	rows := rowsByField(got)
	if r := rows["add-dns-entries"]; r.Class != ClassMissing || r.Source != SourceInspect || r.Writable != "yes" || r.Attr != "add_dns_entries" {
		t.Errorf("add-dns-entries: %+v", r)
	}
	if _, ok := rows["place-before"]; ok {
		t.Errorf("positional add argument must not be reported")
	}
	for _, f := range []string{"lease-time", "comment", "server-address"} {
		if r := rows[f]; r.Class != ClassSchemaOnly || r.Source != SourceSchema || r.Writable != "yes" {
			t.Errorf("%s must be a schema-only row, not an inspect-only one: %+v", f, r)
		}
	}
	if got.CountSource(ClassMissing, SourceInspect) != 1 || got.Count(ClassMissing) != 1 {
		t.Errorf("counts: %v", got.Fields)
	}

	// No items at all: the schema-only pass is skipped, the inspect pass still runs.
	got, _ = compareOne(t, c, g, nil)
	if got.Count(ClassSchemaOnly) != 0 || got.Note == "" {
		t.Errorf("empty menu must skip the schema-only pass: note=%q fields=%v", got.Note, got.Fields)
	}
	if got.Count(ClassMissing) != 1 || got.Fields[0].Field != "add-dns-entries" {
		t.Errorf("empty menu inspect pass: %v", got.Fields)
	}

	// Without an inspect tree there is no inspect pass and no note beyond the empty-menu one.
	got, _ = compareOne(t, NewComparer("7.24", nil, nil), g, nil)
	if len(got.Fields) != 0 {
		t.Errorf("no inspect, no items: expected no rows, got %v", got.Fields)
	}
}

func TestCompareSkipFieldsCoverNestedAndSchemaOnly(t *testing.T) {
	g := &MenuGroup{Path: "/certificate", Resources: []*Resource{{
		Name: "routeros_system_certificate", Path: "/certificate",
		SkipFields: `"import","sign","cert_file_content"`,
		Attrs: []Attr{
			{Name: "name", Type: "string", Required: true},
			{Name: "import.passphrase", Type: "string", Optional: true},
			{Name: "cert_file_content", Type: "string", Optional: true},
		},
	}}}
	got, _ := compareOne(t, NewComparer("7.24", nil, nil), g, []map[string]string{{"name": "c"}})
	if got.Count(ClassSchemaOnly) != 0 {
		t.Errorf("skip-field attributes must not be reported as schema-only: %v", got.Fields)
	}
}

// Two resources with different schemas on one menu (like the CRS and non-CRS switch VLAN resources)
// must each be classified against the device, never one through the other's schema; an alias of one
// of them joins that resource's comparison.
func TestCompareDistinctSchemasOnOneMenu(t *testing.T) {
	const path = "/interface/ethernet/switch/vlan"
	crs := &Resource{Name: "routeros_switch_crs_vlan", Path: path, Attrs: []Attr{
		{Name: "vlan_id", Type: "number", Required: true},
		{Name: "ports", Type: "set", Optional: true},
		{Name: "learn", Type: "bool", Optional: true},
		{Name: "flood", Type: "bool", Optional: true},
	}}
	plain := &Resource{Name: "routeros_switch_vlan", Path: path, Attrs: []Attr{
		{Name: "vlan_id", Type: "number", Required: true},
		{Name: "ports", Type: "set", Optional: true},
		{Name: "independent_learning", Type: "bool", Optional: true},
		{Name: "switch", Type: "string", Required: true},
	}}
	legacy := &Resource{Name: "routeros_switch_vlan_legacy", Path: path, Attrs: plain.Attrs}
	g := &MenuGroup{Path: path, Resources: []*Resource{crs, plain, legacy}}
	// A non-CRS switch chip: it returns the fields of routeros_switch_vlan, not of the CRS resource.
	items := []map[string]string{{".id": "*1", "vlan-id": "10", "ports": "ether1", "independent-learning": "yes", "switch": "switch1"}}

	comparer := NewComparer("7.24", nil, nil)
	got, err := comparer.Compare(g, items)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d comparisons, want one per distinct schema", len(got))
	}
	for i, want := range []struct {
		resources, sharedWith string
		classes               map[string]string
	}{
		{
			resources:  "routeros_switch_crs_vlan",
			sharedWith: "routeros_switch_vlan,routeros_switch_vlan_legacy",
			classes: map[string]string{
				"vlan-id": ClassCovered, "ports": ClassCovered,
				"independent-learning": ClassMissing, "switch": ClassMissing,
				"learn": ClassSchemaOnly, "flood": ClassSchemaOnly,
			},
		},
		{
			resources:  "routeros_switch_vlan,routeros_switch_vlan_legacy",
			sharedWith: "routeros_switch_crs_vlan",
			classes: map[string]string{
				"vlan-id": ClassCovered, "ports": ClassCovered, "independent-learning": ClassCovered, "switch": ClassCovered,
			},
		},
	} {
		c := got[i]
		if c.Path != path || c.Rows != 1 {
			t.Errorf("comparison %d: path %q rows %d", i, c.Path, c.Rows)
		}
		if r := strings.Join(c.Resources, ","); r != want.resources {
			t.Errorf("comparison %d: resources %s, want %s", i, r, want.resources)
		}
		if s := strings.Join(c.SharedWith, ","); s != want.sharedWith {
			t.Errorf("comparison %d (%s): shared with %s, want %s", i, want.resources, s, want.sharedWith)
		}
		rows := rowsByField(c)
		if len(rows) != len(want.classes) {
			t.Errorf("%s: rows %v, want %v", want.resources, c.Fields, want.classes)
		}
		for field, class := range want.classes {
			if rows[field].Class != class {
				t.Errorf("%s: %s is %q, want %s", want.resources, field, rows[field].Class, class)
			}
		}
	}
}

// sharedSwitchMenu is one menu mapped by two schemas: routeros_switch with its alias
// routeros_switch_legacy, and routeros_switch_crs.
func sharedSwitchMenu() *MenuGroup {
	const path = "/interface/ethernet/switch"
	plain := &Resource{Name: "routeros_switch", Path: path, Attrs: []Attr{{Name: "switch", Type: "string", Required: true}}}
	crs := &Resource{Name: "routeros_switch_crs", Path: path, Attrs: []Attr{{Name: "learn", Type: "bool", Optional: true}}}
	legacy := &Resource{Name: "routeros_switch_legacy", Path: path, Attrs: plain.Attrs}
	return &MenuGroup{Path: path, Resources: []*Resource{plain, crs, legacy}}
}

// A -resources filter naming one resource on a shared menu compares that resource's schema only. The
// other schema is named in shared_with but not compared, so its rows reach neither the report nor
// -fail-on-missing.
func TestCompareOnlyTheSelectedSchema(t *testing.T) {
	g := sharedSwitchMenu()
	items := []map[string]string{{".id": "*1", "switch": "switch1", "learn": "yes"}}

	got, err := NewComparer("7.24", nil, nil).Compare(g.Select([]string{"routeros_switch_crs"}), items)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d comparisons, want only the selected schema's", len(got))
	}
	c := got[0]
	if r, s := strings.Join(c.Resources, ","), strings.Join(c.SharedWith, ","); r != "routeros_switch_crs" ||
		s != "routeros_switch,routeros_switch_legacy" {
		t.Errorf("resources %s shared with %s; want routeros_switch_crs shared with routeros_switch,routeros_switch_legacy", r, s)
	}
	rows := rowsByField(c)
	if rows["learn"].Class != ClassCovered || rows["switch"].Class != ClassMissing {
		t.Errorf("rows %v; want learn covered and switch missing against the CRS schema", c.Fields)
	}
}

// A shared menu the device lacks, or that could not be read, keeps one entry per selected schema, so
// shared_with and summary.shared_menus describe it the same way as a compared one.
func TestSkippedSharedMenuKeepsItsSchemas(t *testing.T) {
	g := sharedSwitchMenu()
	for _, tc := range []struct {
		name    string
		entries []*Comparison
		failed  int
	}{
		{"absent", SkippedComparisons(g, "menu absent on device"), 0},
		{"failed", FailedComparisons(g, "HTTP 500"), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, c := range tc.entries {
				got = append(got, strings.Join(c.Resources, "+")+" shared with "+strings.Join(c.SharedWith, "+"))
			}
			want := []string{
				"routeros_switch+routeros_switch_legacy shared with routeros_switch_crs",
				"routeros_switch_crs shared with routeros_switch+routeros_switch_legacy",
			}
			if strings.Join(got, "; ") != strings.Join(want, "; ") {
				t.Errorf("entries = %q, want %q", got, want)
			}
			r := &Report{Menus: tc.entries}
			r.Summarize()
			if want := (Summary{Menus: 1, Skipped: 1, Failed: tc.failed, SharedMenus: 1}); r.Summary != want {
				t.Errorf("summary = %+v, want %+v", r.Summary, want)
			}
		})
	}

	narrowed := SkippedComparisons(g.Select([]string{"routeros_switch_legacy"}), "menu absent on device")
	if len(narrowed) != 1 || strings.Join(narrowed[0].Resources, ",") != "routeros_switch,routeros_switch_legacy" ||
		strings.Join(narrowed[0].SharedWith, ",") != "routeros_switch_crs" {
		t.Errorf("narrowed skip = %+v; want only the named resource's schema, still shared with routeros_switch_crs", narrowed)
	}
}
