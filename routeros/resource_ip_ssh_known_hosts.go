package routeros

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

// ResourceIpSSHKnownHost manages /ip/ssh/known-hosts on RouterOS 7.25+.
// https://manual.mikrotik.com/docs/7.25/cli-reference/ip/ssh/known-hosts/
func ResourceIpSSHKnownHost() *schema.Resource {
	return newAPI725Resource("/ip/ssh/known-hosts", "list")
}
