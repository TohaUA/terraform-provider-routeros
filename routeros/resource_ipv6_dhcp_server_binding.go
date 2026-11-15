package routeros

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

// ResourceIpv6DhcpServerBinding manages /ipv6/dhcp-server/binding on RouterOS 7.25+.
// https://manual.mikrotik.com/docs/7.25/cli-reference/ipv6/dhcp-server/binding/
func ResourceIpv6DhcpServerBinding() *schema.Resource {
	return newAPI725Resource("/ipv6/dhcp-server/binding", "list")
}
