package routeros

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

// ResourceInterfaceEthernetPoe manages /interface/ethernet/poe on RouterOS 7.25.1+.
// https://manual.mikrotik.com/docs/7.25/cli-reference/interface/ethernet/poe/
func ResourceInterfaceEthernetPoe() *schema.Resource {
	return newAPI725Resource("/interface/ethernet/poe", "builtin")
}
