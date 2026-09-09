package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// Report is the whole run.
type Report struct {
	GeneratedAt    time.Time     `json:"generated_at"`
	Device         string        `json:"device"`
	RouterOS       string        `json:"routeros"`
	SchemaSource   string        `json:"schema_source"`
	InspectSource  string        `json:"inspect_source,omitempty"`
	Menus          []*Comparison `json:"menus"`
	NoMenu         []string      `json:"no_menu,omitempty"`  // resources without a RouterOS menu
	Unmapped       []string      `json:"unmapped,omitempty"` // resources in the schema JSON unknown to the compiled provider
	Summary        Summary       `json:"summary"`
	IncludeCovered bool          `json:"-"`
}

// Summary totals.
type Summary struct {
	Menus           int `json:"menus"`
	Compared        int `json:"compared"`
	Skipped         int `json:"skipped"`
	Failed          int `json:"failed"` // part of Skipped: menus whose GET failed instead of being absent
	Missing         int `json:"missing"`
	MissingInspect  int `json:"missing_from_inspect"` // part of Missing: found via the inspect tree only
	ReadOnly        int `json:"read_only"`
	SchemaOnly      int `json:"schema_only"`
	SchemaOnlyUnset int `json:"schema_only_unset"` // part of SchemaOnly: writable per inspect, just unset on the device
	Covered         int `json:"covered"`
	AliasDrift      int `json:"alias_drift"`
}

// Summarize fills Summary from Menus.
func (r *Report) Summarize() {
	s := Summary{Menus: len(r.Menus)}
	for _, m := range r.Menus {
		if m.Skipped != "" {
			s.Skipped++
			if m.Failed {
				s.Failed++
			}
		} else {
			s.Compared++
		}
		s.Missing += m.Count(ClassMissing)
		s.MissingInspect += m.CountSource(ClassMissing, SourceInspect)
		s.ReadOnly += m.Count(ClassReadOnly)
		s.SchemaOnly += m.Count(ClassSchemaOnly)
		s.SchemaOnlyUnset += unsetSchemaOnly(m)
		s.Covered += m.Count(ClassCovered) + m.Count(ClassSkipped)
		if len(m.AliasDrift) > 0 {
			s.AliasDrift++
		}
	}
	r.Summary = s
}

// unsetSchemaOnly counts schema-only rows that are just unset on the device (writable per inspect).
// They are folded out of the Markdown tables unless -all is given.
func unsetSchemaOnly(m *Comparison) int {
	n := 0
	for _, row := range m.Fields {
		if isUnsetSchemaOnly(row) {
			n++
		}
	}
	return n
}

func isUnsetSchemaOnly(row Row) bool {
	return row.Class == ClassSchemaOnly && row.Writable == "yes"
}

// visibleDrift reports whether the menu has anything to show in the drift section.
func visibleDrift(m *Comparison, includeCovered bool) bool {
	if includeCovered {
		return true
	}
	return m.Count(ClassMissing)+m.Count(ClassReadOnly)+(m.Count(ClassSchemaOnly)-unsetSchemaOnly(m)) > 0 || len(m.AliasDrift) > 0
}

// WriteJSON writes the report as indented JSON.
func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteMarkdown writes the human report.
func (r *Report) WriteMarkdown(w io.Writer) error {
	b := &strings.Builder{}
	s := r.Summary
	fmt.Fprintf(b, "# RouterOS schema drift\n\n")
	fmt.Fprintf(b, "- Device: `%s` running RouterOS **%s**\n", r.Device, r.RouterOS)
	fmt.Fprintf(b, "- Provider schema: %s\n", r.SchemaSource)
	if r.InspectSource != "" {
		fmt.Fprintf(b, "- Writability oracle: %s\n", r.InspectSource)
	} else {
		fmt.Fprintf(b, "- Writability oracle: none (`-inspect` not given; writable column is `unknown`)\n")
	}
	fmt.Fprintf(b, "- Generated: %s\n\n", r.GeneratedAt.UTC().Format(time.RFC3339))

	fmt.Fprintf(b, "| menus | compared | skipped | missing | of which inspect-only | read-only | schema-only | of which unset on device | covered | alias drift |\n")
	fmt.Fprintf(b, "|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	fmt.Fprintf(b, "| %d | %d | %d | %d | %d | %d | %d | %d | %d | %d |\n\n",
		s.Menus, s.Compared, s.Skipped, s.Missing, s.MissingInspect, s.ReadOnly, s.SchemaOnly, s.SchemaOnlyUnset, s.Covered, s.AliasDrift)

	fmt.Fprintf(b, "How to read the classes:\n\n")
	fmt.Fprintf(b, "- **missing**: a RouterOS field the provider has no attribute for. Source `device` means the device "+
		"returned it; source `inspect` means it is an `add`/`set` argument in the inspect tree that this device did not return "+
		"(unset, or no items in the menu). With `-inspect` only writable fields land here; without it, every device field not in the status list does.\n")
	fmt.Fprintf(b, "- **read-only**: device field without an attribute, but RouterOS never accepts it on `add`/`set` "+
		"(status list: %s, or not an argument in the inspect tree). Informational.\n", strings.Join(DefaultStatusFields, ", "))
	fmt.Fprintf(b, "- **schema-only**: provider attribute the device did not return. RouterOS omits unset/default "+
		"properties, so this is only a removal candidate when the writable column says `no`; rows with `yes` are counted but hidden unless `-all` is given.\n")
	fmt.Fprintf(b, "- *dynamic only* marks fields that appeared only on items with `dynamic=true`.\n\n")

	// Drift first, then clean menus, then skipped.
	var clean, skipped []*Comparison
	fmt.Fprintf(b, "## Menus with drift\n\n")
	drift := 0
	for _, m := range r.Menus {
		switch {
		case m.Skipped != "":
			skipped = append(skipped, m)
			continue
		case !visibleDrift(m, r.IncludeCovered):
			clean = append(clean, m)
			continue
		}
		drift++
		writeMenu(b, m, r.IncludeCovered)
	}
	if drift == 0 {
		fmt.Fprintf(b, "None.\n\n")
	}

	fmt.Fprintf(b, "## Menus without drift\n\n")
	if len(clean) == 0 {
		fmt.Fprintf(b, "None.\n\n")
	} else {
		for _, m := range clean {
			fmt.Fprintf(b, "- `%s` (%s): %d fields covered, %d items\n", m.Path, strings.Join(m.Resources, ", "),
				m.Count(ClassCovered)+m.Count(ClassSkipped), m.Rows)
		}
		fmt.Fprintf(b, "\n")
	}

	fmt.Fprintf(b, "## Skipped menus\n\n")
	if len(skipped) == 0 {
		fmt.Fprintf(b, "None.\n\n")
	} else {
		fmt.Fprintf(b, "| menu | resources | reason |\n|---|---|---|\n")
		for _, m := range skipped {
			fmt.Fprintf(b, "| `%s` | %s | %s |\n", m.Path, strings.Join(m.Resources, ", "), mdEscape(m.Skipped))
		}
		fmt.Fprintf(b, "\n")
		if s.Failed > 0 {
			fmt.Fprintf(b, "**%d of these menus could not be read** (`GET failed` rows): this report is incomplete "+
				"and the run exits 1.\n\n", s.Failed)
		}
	}

	if len(r.NoMenu) > 0 {
		fmt.Fprintf(b, "## Resources without a RouterOS menu\n\n")
		for _, n := range r.NoMenu {
			fmt.Fprintf(b, "- `%s`\n", n)
		}
		fmt.Fprintf(b, "\n")
	}
	if len(r.Unmapped) > 0 {
		fmt.Fprintf(b, "## Resources in the schema JSON unknown to this provider build\n\n")
		fmt.Fprintf(b, "These cannot be mapped to a menu; rebuild the tool from the same source as the schema.\n\n")
		for _, n := range r.Unmapped {
			fmt.Fprintf(b, "- `%s`\n", n)
		}
		fmt.Fprintf(b, "\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func writeMenu(b *strings.Builder, m *Comparison, includeCovered bool) {
	fmt.Fprintf(b, "### `%s`\n\n", m.Path)
	fmt.Fprintf(b, "Resources: %s. Items: %d", codeList(m.Resources), m.Rows)
	if m.DynamicRows > 0 {
		fmt.Fprintf(b, " (%d dynamic)", m.DynamicRows)
	}
	unset := unsetSchemaOnly(m)
	fmt.Fprintf(b, ". missing %d (%d inspect-only), read-only %d, schema-only %d (%d unset on device%s), covered %d.\n\n",
		m.Count(ClassMissing), m.CountSource(ClassMissing, SourceInspect), m.Count(ClassReadOnly),
		m.Count(ClassSchemaOnly), unset, map[bool]string{true: ", hidden", false: ""}[unset > 0 && !includeCovered],
		m.Count(ClassCovered)+m.Count(ClassSkipped))
	if m.Note != "" {
		fmt.Fprintf(b, "Note: %s.\n\n", m.Note)
	}
	if len(m.AliasDrift) > 0 {
		fmt.Fprintf(b, "Alias resources disagree on attributes: %s\n\n", codeList(m.AliasDrift))
	}
	fmt.Fprintf(b, "| RouterOS field | provider attribute | class | source | writable | note |\n|---|---|---|---|---|---|\n")
	for _, row := range m.Fields {
		if !includeCovered && (row.Class == ClassCovered || row.Class == ClassSkipped || isUnsetSchemaOnly(row)) {
			continue
		}
		note := row.Note
		if row.DynamicOnly {
			if note != "" {
				note += "; "
			}
			note += "dynamic only"
		}
		fmt.Fprintf(b, "| `%s` | `%s` | %s | %s | %s | %s |\n", row.Field, row.Attr, row.Class, row.Source, row.Writable, mdEscape(note))
	}
	fmt.Fprintf(b, "\n")
}

func codeList(items []string) string {
	out := make([]string, 0, len(items))
	for _, s := range items {
		out = append(out, "`"+s+"`")
	}
	return strings.Join(out, ", ")
}

func mdEscape(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}
