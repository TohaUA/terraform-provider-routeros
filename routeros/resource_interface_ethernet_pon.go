package routeros

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

// ResourceInterfaceEthernetPon manages /interface/ethernet/pon on RouterOS 7.25.1+.
// https://manual.mikrotik.com/docs/7.25/cli-reference/interface/ethernet/pon/
func ResourceInterfaceEthernetPon() *schema.Resource {
	return newAPI725Resource("/interface/ethernet/pon", "builtin")
}
