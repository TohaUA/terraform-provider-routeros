package routeros

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

// https://manual.mikrotik.com/docs/7.25/cli-reference/ip/cloud/back-to-home-file/settings/remove-certificate/
func ResourceIpCloudBackToHomeRemoveCertificate() *schema.Resource {
	return newAPI725Action("/ip/cloud/back-to-home-file/settings/remove-certificate")
}
