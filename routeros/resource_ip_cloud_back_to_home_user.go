package routeros

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

// ResourceIpCloudBackToHomeUser manages /ip/cloud/back-to-home-user on RouterOS 7.25+.
// https://manual.mikrotik.com/docs/7.25/cli-reference/ip/cloud/back-to-home-user/
func ResourceIpCloudBackToHomeUser() *schema.Resource {
	return newAPI725Resource("/ip/cloud/back-to-home-user", "list")
}
