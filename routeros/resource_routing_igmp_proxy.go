package routeros

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

// ResourceRoutingIgmpProxy manages /routing/igmp-proxy on RouterOS 7.25.1+.
// https://manual.mikrotik.com/docs/7.25/cli-reference/routing/igmp-proxy/
func ResourceRoutingIgmpProxy() *schema.Resource {
	return newAPI725Resource("/routing/igmp-proxy", "singleton")
}
