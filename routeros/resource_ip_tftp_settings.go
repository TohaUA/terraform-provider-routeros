package routeros

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

/*
  {"max-block-size": "4096"}
*/

// ResourceIPTFTPSettings https://wiki.mikrotik.com/Manual:IP/TFTP
func ResourceIpTFTPSettings() *schema.Resource {
	resSchema := map[string]*schema.Schema{
		MetaResourcePath: PropResourcePath("/ip/tftp/settings"),
		MetaId:           PropId(Name),

		"vrf": {
			Type:             schema.TypeString,
			Optional:         true,
			Description:      "VRF used by the TFTP server. Requires RouterOS 7.25 or later.",
			DiffSuppressFunc: AlwaysPresentNotUserProvided,
		},
		"max_block_size": {
			Type:     schema.TypeInt,
			Optional: true,
			Description: "Maximum accepted block size value. During transfer " +
				"negotiation phase, RouterOS device will not negotiate larger value " +
				"than this.",
			DiffSuppressFunc: AlwaysPresentNotUserProvided,
		},
	}
	addAPI725Fields(resSchema)
	return &schema.Resource{
		CreateContext: DefaultSystemCreate(resSchema),
		ReadContext:   DefaultSystemRead(resSchema),
		UpdateContext: DefaultSystemUpdate(resSchema),
		DeleteContext: DefaultSystemDelete(resSchema),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: resSchema,
	}
}
