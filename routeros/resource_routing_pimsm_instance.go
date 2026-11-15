package routeros

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

// ResourceRoutingPimsmInstance manages /routing/pimsm/instance on RouterOS 7.25+.
// https://manual.mikrotik.com/docs/7.25/cli-reference/routing/pimsm/instance/
func ResourceRoutingPimsmInstance() *schema.Resource {
	return newAPI725Resource("/routing/pimsm/instance", "list")
}
