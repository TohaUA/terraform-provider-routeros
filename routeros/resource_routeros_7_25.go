package routeros

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

// api725Field retains the distinction between configurable arguments and status
// fields from the versioned CLI reference. The checked-in catalog is generated
// from that reference; defaults belong to the router, not to this table.
type api725Field struct {
	Wire                          string
	Kind                          string
	ReadOnly, Required, Sensitive bool
	Enum                          []string
}

func api725Name(wire string) string {
	return strings.NewReplacer("-", "_", ".", "_").Replace(wire)
}

func api725Property(f api725Field) *schema.Schema {
	p := &schema.Schema{Sensitive: f.Sensitive}
	switch f.Kind {
	case "bool":
		p.Type = schema.TypeBool
	case "int":
		p.Type = schema.TypeInt
	case "set":
		p.Type = schema.TypeSet
		p.Elem = &schema.Schema{Type: schema.TypeString}
	default:
		p.Type = schema.TypeString
	}
	p.Description = fmt.Sprintf("RouterOS `%s` property. See the linked RouterOS 7.25 CLI reference for its syntax and device requirements.", f.Wire)
	if f.ReadOnly {
		p.Computed = true
	} else if f.Required {
		p.Required = true
	} else {
		p.Optional = true
		p.DiffSuppressFunc = AlwaysPresentNotUserProvided
	}
	if p.Type == schema.TypeString && len(f.Enum) != 0 && !f.ReadOnly {
		p.ValidateFunc = validation.StringInSlice(f.Enum, false)
	}
	return p
}

func api725Schema(path string) map[string]*schema.Schema {
	s := map[string]*schema.Schema{
		MetaResourcePath: PropResourcePath(path),
		MetaId:           PropId(Id),
	}
	var transforms []string
	for _, f := range routerOS725Reference[path] {
		name := api725Name(f.Wire)
		s[name] = api725Property(f)
		if SnakeToKebab(name) != f.Wire {
			transforms = append(transforms, name+":"+f.Wire)
		}
	}
	if len(transforms) != 0 {
		s[MetaTransformSet] = PropTransformSet(transforms...)
	}
	return s
}

// Add only the release delta to existing schemas. A declared prefix map (for
// example WiFi configuration.*) already covers that group and is kept intact.
func addAPI725Fields(s map[string]*schema.Schema) {
	path := s[MetaResourcePath].Default.(string)
	wanted := make(map[string]bool)
	for _, wire := range routerOS725Additions[path] {
		wanted[wire] = true
	}
	var transforms []string
	if old := s[MetaTransformSet]; old != nil {
		for from, to := range loadTransformSet(old.Default.(string), false) {
			transforms = append(transforms, from+":"+to)
		}
	}
	for _, f := range routerOS725Reference[path] {
		if !wanted[f.Wire] {
			continue
		}
		// Versioned wire renames keep their original Terraform attribute. A
		// second attribute for the new wire name would steal deserialized state.
		renamed := false
		for from, to := range driftAttributeSlice.GetDriftMap("7.25", path, false) {
			if to == f.Wire && s[from] != nil {
				renamed = true
				break
			}
		}
		if renamed {
			continue
		}
		name := api725Name(f.Wire)
		if s[name] != nil {
			continue
		}
		if prefix, _, ok := strings.Cut(f.Wire, "."); ok {
			if p := s[api725Name(prefix)]; p != nil && p.Type == schema.TypeMap {
				continue
			}
		}
		f.Required = false // existing Terraform configurations can omit new options
		s[name] = api725Property(f)
		if SnakeToKebab(name) != f.Wire {
			transforms = append(transforms, name+":"+f.Wire)
		}
	}
	if len(transforms) != 0 {
		s[MetaTransformSet] = PropTransformSet(transforms...)
	}
}

func requireAPI725() diag.Diagnostics {
	version, err := parseRouterOSVersion(RouterOSVersion)
	if err != nil {
		return diag.FromErr(err)
	}
	minimum, _ := parseRouterOSVersion(routerOS725MinimumVersion)
	if version < minimum {
		return diag.Errorf("This resource requires RouterOS 7.25.1 or later; configured version is %s", RouterOSVersion)
	}
	return nil
}

func api725Guard(f schema.CreateContextFunc) schema.CreateContextFunc {
	return func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
		if diags := requireAPI725(); diags.HasError() {
			return diags
		}
		return f(ctx, d, m)
	}
}

func newAPI725Resource(path, mode string) *schema.Resource {
	s := api725Schema(path)
	if path == "/ip/ssh/known-hosts" {
		// This menu has add/remove/print, but no set operation.
		for name, field := range s {
			if !strings.HasPrefix(name, "___") && !field.Computed {
				field.ForceNew = true
			}
		}
	}
	r := &schema.Resource{
		Description: fmt.Sprintf("Manage `%s`. Requires RouterOS 7.25.1 or later and the corresponding hardware/package. [CLI reference](https://manual.mikrotik.com/docs/7.25/cli-reference%s/).", path, path),
		Schema:      s,
		Importer:    &schema.ResourceImporter{StateContext: ImportStateCustomContext(s)},
	}
	switch mode {
	case "singleton":
		r.Importer.StateContext = schema.ImportStatePassthroughContext
		r.CreateContext = api725Guard(DefaultSystemCreate(s))
		r.ReadContext = schema.ReadContextFunc(api725Guard(schema.CreateContextFunc(DefaultSystemRead(s))))
		r.UpdateContext = schema.UpdateContextFunc(api725Guard(schema.CreateContextFunc(DefaultSystemUpdate(s))))
		r.DeleteContext = DefaultSystemDelete(s)
	case "builtin":
		r.Description += " Manages an existing hardware entry by name; deleting the Terraform resource only forgets it."
		r.CreateContext = api725Guard(DefaultCreateUpdate(s))
		r.ReadContext = DefaultRead(s)
		r.UpdateContext = schema.UpdateContextFunc(api725Guard(schema.CreateContextFunc(DefaultUpdate(s))))
		r.DeleteContext = DefaultSystemDelete(s)
	default:
		r.CreateContext = api725Guard(DefaultCreate(s))
		r.ReadContext = DefaultRead(s)
		r.UpdateContext = schema.UpdateContextFunc(api725Guard(schema.CreateContextFunc(DefaultUpdate(s))))
		r.DeleteContext = DefaultDelete(s)
	}
	if mode == "singleton" {
		r.Description += " Deleting the Terraform resource leaves the settings on the router. Import using any non-empty ID."
	}
	return r
}

func newAPI725Datasource(path string, command bool) *schema.Resource {
	fields := api725Schema(path)
	delete(fields, MetaResourcePath)
	delete(fields, MetaId)
	transforms := fields[MetaTransformSet]
	delete(fields, MetaTransformSet)
	for _, f := range fields {
		f.Optional, f.Required, f.Computed = false, false, true
		f.ValidateFunc, f.DiffSuppressFunc = nil, nil
	}
	fields["id"] = &schema.Schema{Type: schema.TypeString, Computed: true}
	s := map[string]*schema.Schema{
		MetaResourcePath: PropResourcePath(path), MetaId: PropId(Id),
		"data": {Type: schema.TypeList, Computed: true, Elem: &schema.Resource{Schema: fields}},
	}
	if transforms != nil {
		s[MetaTransformSet] = transforms
	}
	if command {
		s["arguments"] = &schema.Schema{Type: schema.TypeMap, Optional: true, Sensitive: true,
			Elem: &schema.Schema{Type: schema.TypeString}, Description: "Command selectors, such as `.id` or `numbers`. The command runs only to read operational status."}
	} else {
		s[KeyFilter] = PropFilterRw
	}
	return &schema.Resource{
		Description: fmt.Sprintf("Read `%s` on RouterOS 7.25.1+. [CLI reference](https://manual.mikrotik.com/docs/7.25/cli-reference%s/).", path, path),
		Schema:      s,
		ReadContext: func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			if diags := requireAPI725(); diags.HasError() {
				return diags
			}
			var rows []MikrotikItem
			if command {
				args := MikrotikItem{}
				for k, v := range d.Get("arguments").(map[string]interface{}) {
					args[k] = v.(string)
				}
				if strings.HasSuffix(path, "/monitor") {
					args["once"] = ""
				}
				if err := m.(Client).SendRequest(crudExecute, &URL{Path: path}, args, &rows); err != nil {
					return diag.FromErr(err)
				}
			} else {
				result, err := ReadItemsFiltered(buildReadFilter(d.Get(KeyFilter).(map[string]interface{})), path, m.(Client))
				if err != nil {
					return diag.FromErr(err)
				}
				rows = *result
			}
			if diags := MikrotikResourceDataToTerraformDatasource(&rows, "data", s, d); diags.HasError() {
				return diags
			}
			d.SetId(path)
			return nil
		},
	}
}

// Commands execute on creation and explicit configuration/trigger changes, never
// on refresh or deletion. Results and arguments can contain credentials.
func newAPI725Action(path string) *schema.Resource {
	s := api725Schema(path)
	for _, f := range routerOS725Reference[path] {
		if !f.ReadOnly {
			// Commands have no device defaults to retain in configuration state.
			// Removing an input is a change and must plan another execution.
			s[api725Name(f.Wire)].DiffSuppressFunc = nil
		}
	}
	if path == "/tool/fetch" {
		// RouterOS 7.25 rejects both arguments even when keep-result is false.
		s["output"].ConflictsWith = []string{"keep_result"}
		s["keep_result"].ConflictsWith = []string{"output"}
	}
	s["arguments"] = &schema.Schema{Type: schema.TypeMap, Optional: true, Sensitive: true,
		Elem: &schema.Schema{Type: schema.TypeString}, Description: "Additional command selectors such as `.id` or `numbers`; use exact RouterOS API argument names."}
	s["triggers"] = &schema.Schema{Type: schema.TypeMap, Optional: true, Elem: &schema.Schema{Type: schema.TypeString}, Description: "Changing this map explicitly runs the command again."}
	s["result_json"] = &schema.Schema{Type: schema.TypeString, Computed: true, Sensitive: true, Description: "JSON array containing the command replies. Stored in Terraform state."}
	s[MetaSkipFields] = PropSkipFields("arguments", "triggers")
	execute := func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
		if diags := requireAPI725(); diags.HasError() {
			return diags
		}
		args, _ := TerraformResourceDataToMikrotik(s, d)
		for _, f := range routerOS725Reference[path] {
			if !f.ReadOnly && d.GetRawConfig().GetAttr(api725Name(f.Wire)).IsNull() {
				// An omitted command argument uses the command's default. Do not
				// send a previous state value or the Terraform type's zero value.
				delete(args, f.Wire)
			}
		}
		for k, v := range d.Get("arguments").(map[string]interface{}) {
			if _, declared := args[k]; declared {
				return diag.Errorf("Command argument %q is configured twice", k)
			}
			args[k] = v.(string)
		}
		rows := make([]MikrotikItem, 0)
		if err := m.(Client).SendRequest(crudExecute, &URL{Path: path}, args, &rows); err != nil {
			return diag.FromErr(err)
		}
		result, err := json.Marshal(rows)
		if err != nil {
			return diag.FromErr(err)
		}
		if err := d.Set("result_json", string(result)); err != nil {
			return diag.FromErr(err)
		}
		for _, f := range routerOS725Reference[path] {
			if f.ReadOnly {
				if err := d.Set(api725Name(f.Wire), nil); err != nil {
					return diag.FromErr(err)
				}
			}
		}
		if d.Id() == "" {
			d.SetId(UniqueId())
		}
		if len(rows) != 0 {
			// Do not turn arbitrary command output into diagnostics or overwrite
			// configured arguments with echoed values. The complete response is
			// retained only in the sensitive result_json attribute.
			known := MikrotikItem{}
			for _, row := range rows {
				for _, f := range routerOS725Reference[path] {
					if value, ok := row[f.Wire]; ok && f.ReadOnly {
						known[f.Wire] = value
					}
				}
			}
			return MikrotikResourceDataToTerraform(known, s, d)
		}
		return nil
	}
	return &schema.Resource{
		Description: fmt.Sprintf("Execute `%s` on RouterOS 7.25.1+. Runs on creation or configuration/trigger changes. Refresh does not execute it; deletion only removes state and cannot undo the command. [CLI reference](https://manual.mikrotik.com/docs/7.25/cli-reference%s/).", path, path),
		Schema:      s, CreateContext: execute, UpdateContext: schema.UpdateContextFunc(execute),
		ReadContext: func(context.Context, *schema.ResourceData, interface{}) diag.Diagnostics { return nil },
		DeleteContext: func(_ context.Context, d *schema.ResourceData, _ interface{}) diag.Diagnostics {
			d.SetId("")
			return nil
		},
	}
}
