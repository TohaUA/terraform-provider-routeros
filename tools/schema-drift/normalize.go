package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/terraform-routeros/terraform-provider-routeros/routeros"
)

// DefaultStatusFields are RouterOS keys that only ever describe state and are never accepted by
// `add`/`set` (verified against the restraml inspect tree for 7.24: none of them is an add/set
// argument in any menu). A device key from this list that the provider does not expose is reported
// as read-only instead of missing. `disabled` is deliberately NOT here: it is a configurable
// property in 120+ menus.
var DefaultStatusFields = []string{
	"active", "builtin", "default", "dynamic", "inactive", "invalid", "running", "status",
}

// Row classes.
const (
	ClassCovered    = "covered"     // device field has a provider attribute
	ClassSkipped    = "skipped"     // device field is deliberately ignored via MetaSkipFields
	ClassMissing    = "missing"     // field without a provider attribute (writable or unknown)
	ClassReadOnly   = "read-only"   // device field without a provider attribute, but read-only on RouterOS
	ClassSchemaOnly = "schema-only" // provider attribute the device did not return
)

// Row sources.
const (
	SourceDevice  = "device"  // the field was returned by the device
	SourceInspect = "inspect" // the field is an add/set argument in the inspect tree but the device did not return it
	SourceSchema  = "schema"  // the field exists only as a provider attribute
)

// Row is one compared field.
type Row struct {
	Field       string `json:"field"`                  // RouterOS key (kebab, as returned by the device or expected from the schema)
	Attr        string `json:"attr"`                   // provider attribute (snake, "block.attr" for nested blocks)
	Class       string `json:"class"`                  // one of the Class* constants
	Source      string `json:"source"`                 // one of the Source* constants
	Writable    string `json:"writable"`               // "yes" / "no" / "unknown" (from the inspect tree)
	DynamicOnly bool   `json:"dynamic_only,omitempty"` // seen only in rows with dynamic=true
	Note        string `json:"note,omitempty"`
}

// Comparison is the drift result for one menu.
type Comparison struct {
	Path        string   `json:"path"`
	Resources   []string `json:"resources"`
	Rows        int      `json:"rows"`         // items returned by the device
	DynamicRows int      `json:"dynamic_rows"` // items with dynamic=true
	Fields      []Row    `json:"fields"`
	AliasDrift  []string `json:"alias_drift,omitempty"`
	Skipped     string   `json:"skipped,omitempty"` // reason the menu was not compared
	Failed      bool     `json:"failed,omitempty"`  // the menu was unreadable (transport/HTTP error), not absent
	Note        string   `json:"note,omitempty"`
}

// Count returns the number of rows of the given class.
func (c *Comparison) Count(class string) int {
	n := 0
	for _, r := range c.Fields {
		if r.Class == class {
			n++
		}
	}
	return n
}

// CountSource returns the number of rows of the given class and source.
func (c *Comparison) CountSource(class, source string) int {
	n := 0
	for _, r := range c.Fields {
		if r.Class == class && r.Source == source {
			n++
		}
	}
	return n
}

// Comparer holds the per-run context shared by all menus.
type Comparer struct {
	Version      string // RouterOS version of the device, e.g. "7.24"
	Inspect      *Inspect
	StatusFields map[string]struct{}
}

// NewComparer builds a comparer; extraStatus extends DefaultStatusFields.
func NewComparer(version string, inspect *Inspect, extraStatus []string) *Comparer {
	c := &Comparer{Version: version, Inspect: inspect, StatusFields: map[string]struct{}{}}
	for _, f := range DefaultStatusFields {
		c.StatusFields[f] = struct{}{}
	}
	for _, f := range extraStatus {
		if f = strings.TrimSpace(f); f != "" {
			c.StatusFields[routeros.KebabToSnake(f)] = struct{}{}
		}
	}
	return c
}

// deviceKey is one RouterOS key as observed across the items of a menu.
type deviceKey struct {
	static bool // seen in at least one item that is not dynamic
}

// CollectKeys unions the keys of all items, remembering whether each key was seen on a static
// (non-dynamic) item. Service keys (".id", ".nextid", ...) and "ret" are dropped here, exactly
// like the provider's deserializer does.
func CollectKeys(items []map[string]string) (keys map[string]*deviceKey, dynamicRows int) {
	keys = make(map[string]*deviceKey)
	for _, it := range items {
		dyn := it["dynamic"] == "true"
		if dyn {
			dynamicRows++
		}
		for k := range it {
			if strings.HasPrefix(k, ".") || k == "ret" {
				continue
			}
			dk, ok := keys[k]
			if !ok {
				dk = &deviceKey{}
				keys[k] = dk
			}
			if !dyn {
				dk.static = true
			}
		}
	}
	return keys, dynamicRows
}

// menuContext is the name-translation state for one menu, mirroring what
// MikrotikResourceDataToTerraform does with the resource metadata.
type menuContext struct {
	toTF, toMT map[string]string
	skip       map[string]struct{}
	attrs      map[string]Attr
	mapAttrs   map[string]struct{}
}

func (c *Comparer) newMenuContext(path string, res *Resource) (*menuContext, error) {
	m := &menuContext{
		toTF:     routeros.MetaTransformSetMap(res.TransformSet, true),
		toMT:     routeros.MetaTransformSetMap(res.TransformSet, false),
		skip:     routeros.MetaSkipFieldsSet(res.SkipFields),
		attrs:    make(map[string]Attr, len(res.Attrs)),
		mapAttrs: make(map[string]struct{}),
	}
	if c.Version != "" {
		drift, err := routeros.ResourceDriftMap(c.Version, path, true)
		if err != nil {
			return nil, err
		}
		for k, v := range drift {
			m.toTF[k] = v
		}
		drift, _ = routeros.ResourceDriftMap(c.Version, path, false)
		for k, v := range drift {
			m.toMT[k] = v
		}
	}
	for _, a := range res.Attrs {
		m.attrs[a.Name] = a
		if a.Type == "map" {
			m.mapAttrs[a.Name] = struct{}{}
		}
	}
	return m, nil
}

// classify translates a RouterOS field name the way the deserializer does and finds the provider
// attribute for it. class is ClassCovered, ClassSkipped or "" (no attribute).
func (m *menuContext) classify(field string) (attr, class, note string) {
	name := field
	if t, ok := m.toTF[name]; ok {
		name = t
	}
	snake := routeros.KebabToSnake(name)
	parent := snake
	if i := strings.Index(snake, "."); i >= 0 {
		parent = snake[:i]
	}
	switch {
	case inSet(m.skip, snake) || inSet(m.skip, parent):
		return snake, ClassSkipped, ""
	case hasAttr(m.attrs, snake):
		if m.attrs[snake].ReadOnly() {
			return snake, ClassCovered, "computed-only in provider"
		}
		return snake, ClassCovered, ""
	case inSet(m.mapAttrs, parent):
		return parent, ClassCovered, "map attribute"
	}
	return snake, "", ""
}

// expectedField returns the RouterOS key the provider would read an attribute from.
func (m *menuContext) expectedField(a Attr) string {
	parent := a.Name
	if i := strings.Index(parent, "."); i >= 0 {
		parent = parent[:i]
	}
	if t, ok := m.toMT[a.Name]; ok {
		return t
	}
	if t, ok := m.toMT[parent]; ok && a.Type == "map" {
		return t
	}
	return routeros.SnakeToKebab(a.Name)
}

// Compare classifies every device key of a menu against the primary resource of the group, every
// add/set argument of the inspect tree that the device did not return, and every provider
// attribute against the device keys.
func (c *Comparer) Compare(g *MenuGroup, items []map[string]string) (*Comparison, error) {
	res := g.Primary()
	out := &Comparison{Path: g.Path, Resources: g.Names(), Rows: len(items), AliasDrift: g.AliasDrift()}
	m, err := c.newMenuContext(g.Path, res)
	if err != nil {
		return nil, err
	}

	keys, dynamicRows := CollectKeys(items)
	out.DynamicRows = dynamicRows

	// 1. Device -> provider.
	for field, dk := range keys {
		row := Row{Field: field, Source: SourceDevice, DynamicOnly: !dk.static, Writable: c.writable(g.Path, field)}
		row.Attr, row.Class, row.Note = m.classify(field)
		if row.Class == "" {
			parent := row.Attr
			if i := strings.Index(parent, "."); i >= 0 {
				parent = parent[:i]
			}
			if row.Writable == "no" || inSet(c.StatusFields, row.Attr) || inSet(c.StatusFields, parent) {
				row.Class = ClassReadOnly
			} else {
				row.Class = ClassMissing
			}
		}
		out.Fields = append(out.Fields, row)
	}

	// 2. Inspect -> provider: writable fields the device did not return (unset, or no items at all).
	if args, ok := c.Inspect.Menu(g.Path); ok {
		for field := range args {
			if _, seen := keys[field]; seen || inSet(inspectPositionalArgs, field) {
				continue
			}
			attr, class, _ := m.classify(field)
			if class != "" {
				continue // matched by an attribute or a skip field; the schema-only pass reports the attribute
			}
			out.Fields = append(out.Fields, Row{
				Field: field, Attr: attr, Class: ClassMissing, Source: SourceInspect, Writable: "yes",
				Note: "add/set argument in the inspect tree; not returned by this device",
			})
		}
	}

	// 3. Provider -> device. Pointless when the device has no items in the menu.
	if len(items) == 0 {
		out.Note = "no items on device; schema-only check skipped"
	} else {
		for _, a := range res.Attrs {
			parent := a.Name
			if i := strings.Index(parent, "."); i >= 0 {
				parent = parent[:i]
			}
			if a.Name == "id" || inSet(m.skip, a.Name) || inSet(m.skip, parent) {
				continue
			}
			mt := m.expectedField(a)
			if _, ok := keys[mt]; ok {
				continue
			}
			if a.Type == "map" && hasPrefixKey(keys, mt+".") {
				continue
			}
			row := Row{Field: mt, Attr: a.Name, Class: ClassSchemaOnly, Source: SourceSchema, Writable: c.writable(g.Path, mt)}
			switch {
			case a.ReadOnly():
				row.Note = "computed-only in provider"
			case row.Writable == "yes":
				row.Note = "still an add/set argument; unset on this device"
			case row.Writable == "no":
				row.Note = "not an add/set argument; candidate for removal"
			}
			out.Fields = append(out.Fields, row)
		}
	}

	sort.Slice(out.Fields, func(i, j int) bool {
		if out.Fields[i].Class != out.Fields[j].Class {
			return classOrder(out.Fields[i].Class) < classOrder(out.Fields[j].Class)
		}
		return out.Fields[i].Field < out.Fields[j].Field
	})
	return out, nil
}

func (c *Comparer) writable(path, field string) string {
	args, ok := c.Inspect.Menu(path)
	if !ok {
		return "unknown"
	}
	if _, ok := args[field]; ok {
		return "yes"
	}
	return "no"
}

func classOrder(class string) int {
	switch class {
	case ClassMissing:
		return 0
	case ClassReadOnly:
		return 1
	case ClassSchemaOnly:
		return 2
	case ClassSkipped:
		return 3
	default:
		return 4
	}
}

func inSet(s map[string]struct{}, k string) bool {
	_, ok := s[k]
	return ok
}

func hasAttr(attrs map[string]Attr, name string) bool {
	_, ok := attrs[name]
	return ok
}

func hasPrefixKey(keys map[string]*deviceKey, prefix string) bool {
	for k := range keys {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// SkippedComparison records a menu that could not be compared because the device does not
// have it. That is a normal outcome and does not fail the run.
func SkippedComparison(g *MenuGroup, reason string) *Comparison {
	return &Comparison{Path: g.Path, Resources: g.Names(), Skipped: reason, AliasDrift: g.AliasDrift()}
}

// FailedComparison records a menu whose GET failed for a reason other than the menu being absent
// (authentication, permissions, 5xx, timeout, TLS or a malformed body). The menu still shows up in
// the report so the operator sees what was unreadable, but the run itself is not a success:
// run reports exit 1 when any menu failed.
func FailedComparison(g *MenuGroup, reason string) *Comparison {
	c := SkippedComparison(g, "GET failed: "+reason)
	c.Failed = true
	return c
}

// String renders a row for logs.
func (r Row) String() string {
	return fmt.Sprintf("%s %s -> %s (%s, %s)", r.Class, r.Field, r.Attr, r.Writable, r.Source)
}
