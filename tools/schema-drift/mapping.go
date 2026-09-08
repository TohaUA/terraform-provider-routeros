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

// MenuGroup is one RouterOS menu together with every resource that maps to it
// (legacy aliases such as routeros_bridge / routeros_interface_bridge share a menu).
type MenuGroup struct {
	Path      string
	Resources []*Resource // sorted by name; Resources[0] is the one compared
}

// Primary returns the resource whose attributes are compared for the menu.
func (g *MenuGroup) Primary() *Resource { return g.Resources[0] }

// Names lists the resource names of the group.
func (g *MenuGroup) Names() []string {
	out := make([]string, 0, len(g.Resources))
	for _, r := range g.Resources {
		out = append(out, r.Name)
	}
	return out
}

// AliasDrift reports attribute names present in one alias but not in another, as
// "+name" / "-name" relative to the primary resource. Empty when all aliases agree.
func (g *MenuGroup) AliasDrift() []string {
	if len(g.Resources) < 2 {
		return nil
	}
	base := attrNames(g.Primary().Attrs)
	var out []string
	for _, r := range g.Resources[1:] {
		other := attrNames(r.Attrs)
		for n := range other {
			if _, ok := base[n]; !ok {
				out = append(out, "+"+n+" ("+r.Name+")")
			}
		}
		for n := range base {
			if _, ok := other[n]; !ok {
				out = append(out, "-"+n+" ("+r.Name+")")
			}
		}
	}
	sort.Strings(out)
	return out
}

func attrNames(a []Attr) map[string]struct{} {
	out := make(map[string]struct{}, len(a))
	for _, x := range a {
		out[x.Name] = struct{}{}
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

// MatchesFilter reports whether the group is selected by a list of resource names and/or menu paths.
func (g *MenuGroup) MatchesFilter(filter []string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, f := range filter {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if f == g.Path {
			return true
		}
		for _, r := range g.Resources {
			if f == r.Name {
				return true
			}
		}
	}
	return false
}
