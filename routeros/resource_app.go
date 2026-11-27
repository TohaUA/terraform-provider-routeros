package routeros

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"gopkg.in/yaml.v3"
)

// ResourceApp manages custom definitions, rather than adopting a catalog entry
// that RouterOS owns. The YAML remains open to service-level fields such as
// privileged and runtime placeholders; the router interprets those fields.
func ResourceApp() *schema.Resource {
	s := map[string]*schema.Schema{
		MetaResourcePath: PropResourcePath("/app"), MetaId: PropId(Id),
		"yaml": {
			Type: schema.TypeString, Required: true, Sensitive: true,
			Description:  "Custom App composition, including name and services. Supports RouterOS service options and runtime placeholders. Stored in Terraform state.",
			ValidateFunc: validateAppYAML, DiffSuppressFunc: appYAMLEqual, DiffSuppressOnRefresh: true,
		},
		"disabled":     {Type: schema.TypeBool, Optional: true, Default: true, Description: "Keep the definition disabled. Set false to start asynchronous deployment."},
		"network":      {Type: schema.TypeString, Optional: true, ValidateFunc: validation.StringInSlice([]string{"default", "lan", "internal"}, false), DiffSuppressFunc: AlwaysPresentNotUserProvided, Description: "App network: default, lan, or internal."},
		"network_pvid": {Type: schema.TypeInt, Optional: true, ValidateFunc: validation.IntBetween(1, 4094), DiffSuppressFunc: AlwaysPresentNotUserProvided, Description: "Port VLAN ID for the application's virtual Ethernet interface."},
	}
	for _, name := range []string{"auto_update", "network_outgoing_access", "use_https"} {
		s[name] = api725Property(api725Field{Wire: SnakeToKebab(name), Kind: "bool"})
	}
	for _, name := range []string{"container_command_lines", "devices", "environment", "extra_mounts", "firewall_redirects", "required_hw_devices", "required_mounts", "secrets"} {
		s[name] = api725Property(api725Field{Wire: SnakeToKebab(name), Kind: "string", Sensitive: name == "environment" || name == "secrets" || name == "container_command_lines"})
	}
	for _, name := range []string{"name", "description", "category", "project_page", "default_credentials", "default_network", "app_store_url", "interface", "ip_address", "ui_url", "status", "memory_current", "cpu_usage", "app_size", "data_size", "variables_to_use_in_environment"} {
		s[name] = api725Property(api725Field{Wire: SnakeToKebab(name), Kind: "string", ReadOnly: true, Sensitive: name == "default_credentials"})
	}
	for _, name := range []string{"running", "custom", "from_app_store"} {
		s[name] = api725Property(api725Field{Wire: SnakeToKebab(name), Kind: "bool", ReadOnly: true})
	}
	read := func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
		if ds := ResourceRead(ctx, s, d, m); ds.HasError() {
			return ds
		}
		if d.Id() != "" && !d.Get("custom").(bool) {
			return diag.Errorf("routeros_app manages custom Apps only; this ID belongs to a catalog App")
		}
		return nil
	}
	r := &schema.Resource{
		Description: "Manage a custom App on RouterOS 7.25.1 with the container package and container device mode enabled. Disabled by default. Enabling starts deployment asynchronously; status and running expose progress. Destroy removes the custom definition using /app/remove; cleanup is a separate explicit action. YAML, environment, and secrets remain in Terraform state. [Apps documentation](https://manual.mikrotik.com/docs/7.25/containers/apps/).",
		Schema:      s,
		CreateContext: api725Guard(func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			if ds := ResourceCreate(ctx, s, d, m); ds.HasError() {
				return ds
			}
			return read(ctx, d, m)
		}),
		ReadContext: read,
		UpdateContext: func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			if ds := requireAPI725(m); ds.HasError() {
				return ds
			}
			rows, err := ReadItems(&ItemId{Id, d.Id()}, "/app", m.(Client))
			if err != nil {
				return diag.FromErr(err)
			}
			if len(*rows) == 0 {
				return diag.Errorf("App no longer exists")
			}
			if (*rows)[0]["custom"] != "true" {
				return diag.Errorf("routeros_app manages custom Apps only")
			}
			if ds := ResourceUpdate(ctx, s, d, m); ds.HasError() {
				return ds
			}
			return read(ctx, d, m)
		},
		DeleteContext: func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			if ds := read(ctx, d, m); ds.HasError() {
				return ds
			}
			if d.Id() == "" {
				return nil
			}
			return ResourceDelete(ctx, s, d, m)
		},
		Importer: &schema.ResourceImporter{StateContext: ImportStateCustomContext(s)},
	}
	return r
}

func decodeAppYAML(s string) (map[string]interface{}, error) {
	decoder := yaml.NewDecoder(strings.NewReader(s))
	var value map[string]interface{}
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("expected one YAML document")
	}
	return value, nil
}

func validateAppYAML(v interface{}, key string) ([]string, []error) {
	value, err := decodeAppYAML(v.(string))
	if err == nil {
		name, _ := value["name"].(string)
		services, _ := value["services"].(map[string]interface{})
		if strings.TrimSpace(name) != "" && len(services) != 0 {
			return nil, nil
		}
	}
	// Parser errors can quote a source line containing credentials.
	return nil, []error{fmt.Errorf("%s must be one YAML mapping with a non-empty name and services mapping", key)}
}

func appYAMLEqual(_ string, old, new string, _ *schema.ResourceData) bool {
	a, err := decodeAppYAML(old)
	if err != nil {
		return false
	}
	b, err := decodeAppYAML(new)
	return err == nil && reflect.DeepEqual(a, b)
}

func ResourceAppSettings() *schema.Resource {
	s := map[string]*schema.Schema{MetaResourcePath: PropResourcePath("/app/settings"), MetaId: PropId(Id)}
	for _, name := range []string{"disk", "router_ip", "lan_bridge", "media_path", "download_path", "registry_mirrors", "app_store_urls"} {
		s[name] = api725Property(api725Field{Wire: SnakeToKebab(name), Kind: "string", Sensitive: name == "registry_mirrors" || name == "app_store_urls"})
	}
	for _, name := range []string{"auto_update", "show_in_webfig"} {
		s[name] = api725Property(api725Field{Wire: SnakeToKebab(name), Kind: "bool"})
	}
	for _, name := range []string{"assumed_router_ip", "assumed_lan_bridge", "assumed_media_path", "assumed_download_path", "certificate_status", "certificate"} {
		s[name] = api725Property(api725Field{Wire: SnakeToKebab(name), Kind: "string", ReadOnly: true})
	}
	return &schema.Resource{
		Description: "Manage global App storage, networking, and registry settings on RouterOS 7.25.1 with the container package. Does not run the interactive setup wizard or format disks. Destroy leaves settings on the router. Import with any non-empty ID.",
		Schema:      s, CreateContext: api725Guard(DefaultSystemCreate(s)), ReadContext: DefaultSystemRead(s), UpdateContext: schema.UpdateContextFunc(api725Guard(schema.CreateContextFunc(DefaultSystemUpdate(s)))), DeleteContext: DefaultSystemDelete(s),
		Importer: &schema.ResourceImporter{StateContext: schema.ImportStatePassthroughContext},
	}
}
