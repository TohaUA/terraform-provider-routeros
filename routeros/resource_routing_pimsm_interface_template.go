package routeros

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

// ResourceRoutingPimsmInterfaceTemplate manages /routing/pimsm/interface-template on RouterOS 7.25.1+.
// https://manual.mikrotik.com/docs/7.25/cli-reference/routing/pimsm/interface-template/
func ResourceRoutingPimsmInterfaceTemplate() *schema.Resource {
	return newAPI725Resource("/routing/pimsm/interface-template", "list")
}
