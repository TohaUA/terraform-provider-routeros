package routeros

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func DatasourceApps() *schema.Resource {
	fields := ResourceApp().Schema
	for name, field := range fields {
		if strings.HasPrefix(name, "___") {
			delete(fields, name)
			continue
		}
		field.Optional, field.Required, field.Computed = false, false, true
		field.Default, field.ValidateFunc, field.DiffSuppressFunc = nil, nil, nil
		field.DiffSuppressOnRefresh = false
	}
	fields["id"] = &schema.Schema{Type: schema.TypeString, Computed: true}
	s := map[string]*schema.Schema{
		MetaResourcePath: PropResourcePath("/app"), MetaId: PropId(Id), KeyFilter: PropFilterRw,
		"data": {Type: schema.TypeList, Computed: true, Sensitive: true, Elem: &schema.Resource{Schema: fields}, Description: "Catalog and custom Apps, including sensitive composition and environment values."},
	}
	return &schema.Resource{
		Description: "Read App definitions and deployment status on RouterOS 7.25.1 with the container package. Reading does not start or update Apps. Returned definitions may contain secrets and are stored in Terraform state.",
		Schema:      s,
		ReadContext: func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			if ds := requireAPI725(m); ds.HasError() {
				return ds
			}
			rows, err := ReadItemsFiltered(buildReadFilter(d.Get(KeyFilter).(map[string]interface{})), "/app", m.(Client))
			if err != nil {
				return diag.FromErr(err)
			}
			if ds := MikrotikResourceDataToTerraformDatasource(rows, "data", s, d); ds.HasError() {
				return ds
			}
			d.SetId("/app")
			return nil
		},
	}
}
