package routeros

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

// ResourceInterfaceLteSettings manages /interface/lte/settings on RouterOS 7.25.1+.
// https://manual.mikrotik.com/docs/7.25/cli-reference/interface/lte/settings/
func ResourceInterfaceLteSettings() *schema.Resource {
	return newAPI725Resource("/interface/lte/settings", "singleton")
}
