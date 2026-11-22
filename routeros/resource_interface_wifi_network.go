package routeros

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

// ResourceWifiNetwork manages /interface/wifi/network on RouterOS 7.25.1+.
// https://manual.mikrotik.com/docs/7.25/cli-reference/interface/wifi/network/
func ResourceWifiNetwork() *schema.Resource {
	return newAPI725Resource("/interface/wifi/network", "list")
}
