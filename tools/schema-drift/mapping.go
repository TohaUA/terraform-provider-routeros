package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/terraform-routeros/terraform-provider-routeros/routeros"
)

// Attr is one provider attribute in the flattened form the comparison works on.
// Attributes of nested blocks are named "block.attr" (RouterOS uses the same dotted keys).
type Attr struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // string | number | bool | list | set | map
	Optional bool   `json:"optional,omitempty"`
	Required bool   `json:"required,omitempty"`
	Computed bool   `json:"computed,omitempty"`
}

// ReadOnly reports whether the provider exposes the attribute as computed-only
// (the Prop*Ro convention: Computed without Optional/Required).
func (a Attr) ReadOnly() bool { return a.Computed && !a.Optional && !a.Required }

// Resource is the provider-side description of one Terraform resource:
// its RouterOS menu path plus the metadata the serializer uses to translate field names.
type Resource struct {
	Name         string
	Path         string // RouterOS menu, e.g. "/ip/service"; "" or "local" for resources without a menu
	IdType       int    // routeros.Id (1, keyed by .id) or routeros.Name (2, singleton/settings menu)
	TransformSet string // raw MetaTransformSet default
	SkipFields   string // raw MetaSkipFields default
	Attrs        []Attr
}

// HasMenu reports whether the resource maps to a RouterOS menu that can be fetched with a GET.
func (r *Resource) HasMenu() bool { return strings.HasPrefix(r.Path, "/") }

// ProviderResources derives resource -> menu mapping and attributes from the compiled provider.
// Every resource declares its menu as the Default of the MetaResourcePath attribute, so this map can
// never go stale relative to the provider it is built with.
func ProviderResources(p *schema.Provider) map[string]*Resource {
	out := make(map[string]*Resource, len(p.ResourcesMap))
	for name, r := range p.ResourcesMap {
		out[name] = resourceFromSDK(name, r)
	}
	return out
}

func resourceFromSDK(name string, r *schema.Resource) *Resource {
	res := &Resource{Name: name}
	res.Path = metaString(r.Schema, routeros.MetaResourcePath)
	if v, ok := r.Schema[routeros.MetaId]; ok {
		if id, ok := v.Default.(int); ok {
			res.IdType = id
		}
	}
	res.TransformSet = metaString(r.Schema, routeros.MetaTransformSet)
	res.SkipFields = metaString(r.Schema, routeros.MetaSkipFields)
	res.Attrs = attrsFromSDK(r.Schema, "")
	sortAttrs(res.Attrs)
	return res
}

func metaString(s map[string]*schema.Schema, key string) string {
	if v, ok := s[key]; ok && v.Default != nil {
		return fmt.Sprint(v.Default)
	}
	return ""
}

func attrsFromSDK(s map[string]*schema.Schema, prefix string) []Attr {
	var out []Attr
	for k, v := range s {
		if prefix == "" && isMetaAttr(k) {
			continue
		}
		if nested, ok := v.Elem.(*schema.Resource); ok {
			out = append(out, attrsFromSDK(nested.Schema, prefix+k+".")...)
			continue
		}
		out = append(out, Attr{
			Name:     prefix + k,
			Type:     sdkType(v.Type),
			Optional: v.Optional,
			Required: v.Required,
			Computed: v.Computed,
		})
	}
	return out
}

func sdkType(t schema.ValueType) string {
	switch t {
	case schema.TypeBool:
		return "bool"
	case schema.TypeInt, schema.TypeFloat:
		return "number"
	case schema.TypeList:
		return "list"
	case schema.TypeSet:
		return "set"
	case schema.TypeMap:
		return "map"
	default:
		return "string"
	}
}

func isMetaAttr(name string) bool {
	return strings.HasPrefix(name, "___") && strings.HasSuffix(name, "___")
}

func sortAttrs(a []Attr) {
	sort.Slice(a, func(i, j int) bool { return a[i].Name < a[j].Name })
}

// ---------------------------------------------------------------------------------------------
// `terraform providers schema -json` input.

type tfSchemaJSON struct {
	ProviderSchemas map[string]struct {
		ResourceSchemas map[string]struct {
			Block tfBlock `json:"block"`
		} `json:"resource_schemas"`
	} `json:"provider_schemas"`
}

type tfBlock struct {
	Attributes map[string]struct {
		Type     json.RawMessage `json:"type"`
		Optional bool            `json:"optional"`
		Required bool            `json:"required"`
		Computed bool            `json:"computed"`
	} `json:"attributes"`
	BlockTypes map[string]struct {
		Block tfBlock `json:"block"`
	} `json:"block_types"`
}

// LoadSchemaJSON reads the output of `terraform providers schema -json` and returns the attribute
// sets of the routeros resources it contains, keyed by resource name.
func LoadSchemaJSON(file string) (map[string][]Attr, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return parseSchemaJSON(raw)
}

func parseSchemaJSON(raw []byte) (map[string][]Attr, error) {
	var doc tfSchemaJSON
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("schema json: %w", err)
	}
	var keys []string
	for k := range doc.ProviderSchemas {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var chosen string
	for _, k := range keys {
		if strings.Contains(k, "routeros") {
			chosen = k
			break
		}
	}
	if chosen == "" {
		if len(keys) == 0 {
			return nil, fmt.Errorf("schema json: no provider_schemas entry")
		}
		chosen = keys[0]
	}
	out := make(map[string][]Attr)
	for name, r := range doc.ProviderSchemas[chosen].ResourceSchemas {
		attrs := attrsFromJSON(r.Block, "")
		sortAttrs(attrs)
		out[name] = attrs
	}
	return out, nil
}

func attrsFromJSON(b tfBlock, prefix string) []Attr {
	var out []Attr
	for k, v := range b.Attributes {
		if prefix == "" && isMetaAttr(k) {
			continue
		}
		out = append(out, Attr{
			Name:     prefix + k,
			Type:     jsonType(v.Type),
			Optional: v.Optional,
			Required: v.Required,
			Computed: v.Computed,
		})
	}
	for k, v := range b.BlockTypes {
		if prefix == "" && k == "timeouts" { // SDK-managed block, not a RouterOS field
			continue
		}
		out = append(out, attrsFromJSON(v.Block, prefix+k+".")...)
	}
	return out
}

// jsonType reduces a cty type ("string" or ["list","string"]) to its outer kind.
func jsonType(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var l []json.RawMessage
	if err := json.Unmarshal(raw, &l); err == nil && len(l) > 0 {
		if err := json.Unmarshal(l[0], &s); err == nil {
			return s
		}
	}
	return "string"
}

// ---------------------------------------------------------------------------------------------
// Menu groups.

// MenuGroup is one RouterOS menu together with every resource that maps to it. Resources share a
// menu either as legacy aliases with the same schema (routeros_bridge / routeros_interface_bridge)
// or as different resources with different schemas (routeros_interface_ethernet_switch and
// routeros_interface_ethernet_switch_crs on /interface/ethernet/switch); Schemas tells them apart.
// The menu is fetched once per group either way.
type MenuGroup struct {
	Path      string
	Resources []*Resource // sorted by name

	selected map[string]struct{} // resource names Select narrowed the group to; nil keeps every schema
}

// MenuSchema is one distinct schema on a menu: the resources whose attributes and name-translation
// metadata are identical, so that comparing any one of them classifies every field the same way.
type MenuSchema struct {
	Resources []*Resource // in group order; Resources[0] is the one compared
}

// Compared returns the resource whose attributes are compared for the schema.
func (s *MenuSchema) Compared() *Resource { return s.Resources[0] }

// Names lists the resource names of the schema.
func (s *MenuSchema) Names() []string { return resourceNames(s.Resources) }

// Names lists the resource names of the group.
func (g *MenuGroup) Names() []string { return resourceNames(g.Resources) }

// Schemas splits the group into its distinct schemas, ordered by their first resource. A menu used
// only by aliases yields one schema; every schema needs its own comparison against the device.
func (g *MenuGroup) Schemas() []*MenuSchema {
	var out []*MenuSchema
	byKey := make(map[string]*MenuSchema)
	for _, r := range g.Resources {
		k := schemaKey(r)
		s, ok := byKey[k]
		if !ok {
			s = &MenuSchema{}
			byKey[k] = s
			out = append(out, s)
		}
		s.Resources = append(s.Resources, r)
	}
	return out
}

// schemaKey renders everything Compare reads from a resource: every attribute with its type and
// flags, the transform set and the skip fields, independent of their order. Resources with the same
// key classify every device field identically. The id type is not part of it; Compare ignores it.
func schemaKey(r *Resource) string {
	parts := make([]string, 0, len(r.Attrs))
	for _, a := range r.Attrs {
		parts = append(parts, fmt.Sprintf("attr %q %s optional=%t required=%t computed=%t", a.Name, a.Type, a.Optional, a.Required, a.Computed))
	}
	for k, v := range routeros.MetaTransformSetMap(r.TransformSet, false) {
		parts = append(parts, fmt.Sprintf("transform %q %q", k, v))
	}
	for k := range routeros.MetaSkipFieldsSet(r.SkipFields) {
		parts = append(parts, fmt.Sprintf("skip %q", k))
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n")
}

func resourceNames(rs []*Resource) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Name)
	}
	return out
}

// GroupByMenu groups resources that map to a RouterOS menu, sorted by path.
// Resources without a menu are returned separately.
func GroupByMenu(resources map[string]*Resource) (groups []*MenuGroup, noMenu []*Resource) {
	byPath := make(map[string]*MenuGroup)
	for _, r := range resources {
		if !r.HasMenu() {
			noMenu = append(noMenu, r)
			continue
		}
		g, ok := byPath[r.Path]
		if !ok {
			g = &MenuGroup{Path: r.Path}
			byPath[r.Path] = g
		}
		g.Resources = append(g.Resources, r)
	}
	for _, g := range byPath {
		sort.Slice(g.Resources, func(i, j int) bool { return g.Resources[i].Name < g.Resources[j].Name })
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Path < groups[j].Path })
	sort.Slice(noMenu, func(i, j int) bool { return noMenu[i].Name < noMenu[j].Name })
	return groups, noMenu
}

// Select returns the part of the group that a list of resource names and/or menu paths asks for, or
// nil when the list selects nothing on this menu. No list, or the menu's path, selects every schema.
// A resource name selects the schema that resource belongs to, aliases included, but not a different
// schema that only shares the menu: its rows were not asked for, and its missing fields must not
// decide -fail-on-missing. The group itself is left as it is.
func (g *MenuGroup) Select(filter []string) *MenuGroup {
	if len(filter) == 0 {
		return g
	}
	names := make(map[string]struct{})
	for _, f := range filter {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if f == g.Path {
			return g
		}
		for _, r := range g.Resources {
			if f == r.Name {
				names[f] = struct{}{}
			}
		}
	}
	if len(names) == 0 {
		return nil
	}
	sel := *g
	sel.selected = names
	return &sel
}

// SelectedSchemas returns the schemas Select kept, in Schemas order; every schema when the group was
// not narrowed to named resources.
func (g *MenuGroup) SelectedSchemas() []*MenuSchema {
	schemas := g.Schemas()
	if g.selected == nil {
		return schemas
	}
	var out []*MenuSchema
	for _, s := range schemas {
		for _, r := range s.Resources {
			if _, ok := g.selected[r.Name]; ok {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

// SharedWith lists the resources of every other schema on the menu, whether or not Select kept it:
// the menu is shared with them either way.
func (g *MenuGroup) SharedWith(s *MenuSchema) []string {
	var out []string
	for _, other := range g.Schemas() {
		if other.Compared().Name != s.Compared().Name {
			out = append(out, other.Names()...)
		}
	}
	return out
}
