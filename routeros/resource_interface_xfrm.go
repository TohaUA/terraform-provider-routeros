package routeros

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

// ResourceInterfaceXfrm manages /interface/xfrm on RouterOS 7.25.1+.
// https://manual.mikrotik.com/docs/7.25/cli-reference/interface/xfrm/
func ResourceInterfaceXfrm() *schema.Resource {
	return newAPI725Resource("/interface/xfrm", "list")
}
