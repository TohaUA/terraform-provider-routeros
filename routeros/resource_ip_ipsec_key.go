package routeros

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

/*
  {
    ".id": "*1",
    "key-size": "1024", <<<< !!! /ip/ipsec/key/generate-key name=new-key key-size=   2048     4096     8192
    "name": "new-key",
    "private-key": "true",
    "rsa": "true"
  }
*/

// Keep the Terraform resource address stable across the RouterOS 7.20 menu move.
// Resolve at operation time, after provider configuration determines the version.
func ipsecKeySchema(s map[string]*schema.Schema) map[string]*schema.Schema {
	version, err := parseRouterOSVersion(RouterOSVersion)
	minimum, _ := parseRouterOSVersion("7.20")
	if err != nil || version < minimum {
		return s
	}
	modern := make(map[string]*schema.Schema, len(s))
	for k, v := range s {
		modern[k] = v
	}
	modern[MetaResourcePath] = PropResourcePath("/ip/ipsec/key/rsa")
	return modern
}

// https://help.mikrotik.com/docs/display/ROS/IPsec#IPsec-Keys
func ResourceIpIpsecKey() *schema.Resource {
	resSchema := map[string]*schema.Schema{
		MetaResourcePath: PropResourcePath("/ip/ipsec/key"),
		MetaId:           PropId(Id),
		MetaSkipFields:   PropSkipFields("private_key", "rsa"),

		"key_size": {
			Type:             schema.TypeInt,
			Required:         true,
			ForceNew:         true,
			Description:      "Size of this key.",
			ValidateFunc:     validation.IntInSlice([]int{1024, 2048, 4096, 8192}),
			DiffSuppressFunc: AlwaysPresentNotUserProvided,
		},
		KeyName: PropName(""),
	}

	return &schema.Resource{
		CreateContext: func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			return ResourceCreateAndWait(ctxSetCrudMethod(ctx, crudGenerateKey), ipsecKeySchema(resSchema), d, m, d.Timeout(schema.TimeoutCreate))
		},
		ReadContext: func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			return DefaultRead(ipsecKeySchema(resSchema))(ctx, d, m)
		},
		UpdateContext: func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			return DefaultUpdate(ipsecKeySchema(resSchema))(ctx, d, m)
		},
		DeleteContext: func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			return DefaultDelete(ipsecKeySchema(resSchema))(ctx, d, m)
		},

		Importer: &schema.ResourceImporter{
			StateContext: func(ctx context.Context, d *schema.ResourceData, m interface{}) ([]*schema.ResourceData, error) {
				return ImportStateCustomContext(ipsecKeySchema(resSchema))(ctx, d, m)
			},
		},

		Schema: resSchema,
	}
}
