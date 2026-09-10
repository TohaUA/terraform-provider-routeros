package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func sampleReport() *Report {
	r := &Report{GeneratedAt: time.Unix(0, 0), Device: "https://r", RouterOS: "7.24", SchemaSource: "compiled provider"}
	r.Menus = []*Comparison{
		{Path: "/a", Resources: []string{"routeros_a"}, Rows: 1, Fields: []Row{
			{Field: "new-field", Attr: "new_field", Class: ClassMissing, Source: SourceDevice, Writable: "yes"},
			{Field: "from-inspect", Attr: "from_inspect", Class: ClassMissing, Source: SourceInspect, Writable: "yes"},
			{Field: "dynamic", Attr: "dynamic", Class: ClassReadOnly, Source: SourceDevice, Writable: "no"},
			{Field: "unset", Attr: "unset", Class: ClassSchemaOnly, Source: SourceSchema, Writable: "yes"},
			{Field: "gone", Attr: "gone", Class: ClassSchemaOnly, Source: SourceSchema, Writable: "no", Note: "candidate"},
			{Field: "name", Attr: "name", Class: ClassCovered, Source: SourceDevice, Writable: "yes"},
		}},
		{Path: "/b", Resources: []string{"routeros_b", "routeros_b_alias"}, Rows: 2, Fields: []Row{
			{Field: "name", Attr: "name", Class: ClassCovered, Source: SourceDevice, Writable: "unknown"},
			{Field: "unset", Attr: "unset", Class: ClassSchemaOnly, Source: SourceSchema, Writable: "yes"},
		}},
		{Path: "/c", Resources: []string{"routeros_c"}, Skipped: "menu absent on device: no such command or directory (c)"},
		// One menu, two distinct schemas: an entry each, one menu in the summary.
		{Path: "/d", Resources: []string{"routeros_d"}, SharedWith: []string{"routeros_d_crs"}, Rows: 1, Fields: []Row{
			{Field: "chip", Attr: "chip", Class: ClassMissing, Source: SourceDevice, Writable: "yes"},
			{Field: "name", Attr: "name", Class: ClassCovered, Source: SourceDevice, Writable: "yes"},
		}},
		{Path: "/d", Resources: []string{"routeros_d_crs"}, SharedWith: []string{"routeros_d"}, Rows: 1, Fields: []Row{
			{Field: "chip", Attr: "chip", Class: ClassCovered, Source: SourceDevice, Writable: "yes"},
			{Field: "name", Attr: "name", Class: ClassCovered, Source: SourceDevice, Writable: "yes"},
		}},
	}
	r.NoMenu = []string{"routeros_wireguard_keys"}
	r.Summarize()
	return r
}

func TestSummarize(t *testing.T) {
	s := sampleReport().Summary
	want := Summary{Menus: 4, Compared: 3, Skipped: 1, Missing: 3, MissingInspect: 1, ReadOnly: 1, SchemaOnly: 3, SchemaOnlyUnset: 2,
		Covered: 5, SharedMenus: 1}
	if s != want {
		t.Errorf("summary = %+v, want %+v", s, want)
	}
}

func TestWriteMarkdownFoldsNoise(t *testing.T) {
	r := sampleReport()
	var b bytes.Buffer
	if err := r.WriteMarkdown(&b); err != nil {
		t.Fatal(err)
	}
	md := b.String()
	for _, want := range []string{
		"| 4 | 3 | 1 | 3 | 1 | 1 | 3 | 2 | 5 | 1 |",
		"### `/a`\n",
		"| `new-field` | `new_field` | missing | device | yes |",
		"| `from-inspect` | `from_inspect` | missing | inspect | yes |",
		"| `dynamic` | `dynamic` | read-only | device | no |",
		"| `gone` | `gone` | schema-only | schema | no | candidate |",
		"- `/b` (routeros_b, routeros_b_alias): 1 fields covered, 2 items\n",
		"| `/c` | routeros_c | menu absent on device: no such command or directory (c) |",
		"### `/d` (`routeros_d`)\n",
		"Menu shared with a different schema (`routeros_d_crs`), compared in its own entry unless `-resources` left it out.",
		"| `chip` | `chip` | missing | device | yes |",
		"- `/d` (routeros_d_crs): 2 fields covered, 1 items; menu shared with routeros_d (different schema)\n",
		"- `routeros_wireguard_keys`",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q\n%s", want, md)
		}
	}
	if strings.Contains(md, "| `unset` |") {
		t.Errorf("unset-but-writable schema-only rows must be hidden by default")
	}
	if strings.Contains(md, "### `/b`") {
		t.Errorf("a menu whose only rows are hidden must be listed as without drift")
	}
	if strings.Contains(md, "### `/d`\n") || strings.Contains(md, "### `/d` (`routeros_d_crs`)") {
		t.Errorf("a shared menu's clean schema must not get a section, and shared sections must name their resources")
	}

	r.IncludeCovered = true
	b.Reset()
	_ = r.WriteMarkdown(&b)
	if !strings.Contains(b.String(), "| `unset` |") || !strings.Contains(b.String(), "### `/b`") ||
		!strings.Contains(b.String(), "### `/d` (`routeros_d_crs`)") {
		t.Errorf("-all must show every row")
	}
}

func TestWriteJSONRoundTrip(t *testing.T) {
	var b bytes.Buffer
	if err := sampleReport().WriteJSON(&b); err != nil {
		t.Fatal(err)
	}
	var back Report
	if err := json.Unmarshal(b.Bytes(), &back); err != nil {
		t.Fatal(err)
	}
	if back.Summary.Missing != 3 || len(back.Menus) != 5 || back.Menus[0].Fields[1].Source != SourceInspect {
		t.Errorf("round trip lost data: %+v", back.Summary)
	}
	if back.Summary.SharedMenus != 1 || strings.Join(back.Menus[4].SharedWith, ",") != "routeros_d" {
		t.Errorf("round trip lost the shared-menu data: %+v %+v", back.Summary, back.Menus[4])
	}
}
