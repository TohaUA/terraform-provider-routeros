package routeros

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

/*
  {
    ".id": "*2",
    "default": "true",
    "name": "default"
  }
*/

// https://help.mikrotik.com/docs/display/ROS/IPsec#IPsec-Groups
func ResourceIpIpsecPolicyGroup() *schema.Resource {
	resSchema := map[string]*schema.Schema{
		MetaResourcePath:   PropResourcePath("/ip/ipsec/policy/group"),
		MetaId:             PropId(Id),
		MetaSetUnsetFields: PropSetUnsetFields("interface"),

		KeyDefault: PropDefaultRo,
		"interface": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Interface associated with the IPsec policy group. Requires RouterOS 7.25.1 or later.",
		},
		KeyName:    PropName(""),
		KeyComment: PropCommentRw,
	}

	addAPI725Fields(resSchema)
	return &schema.Resource{
		CreateContext: DefaultCreate(resSchema),
		ReadContext:   DefaultRead(resSchema),
		UpdateContext: DefaultUpdate(resSchema),
		DeleteContext: DefaultDelete(resSchema),

		Importer: &schema.ResourceImporter{
			StateContext: ImportStateCustomContext(resSchema),
		},

		Schema: resSchema,
	}
}
