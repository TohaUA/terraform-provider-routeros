package routeros

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

// ResourceWifiSniffer manages /interface/wifi/sniffer on RouterOS 7.25+.
// https://manual.mikrotik.com/docs/7.25/cli-reference/interface/wifi/sniffer/
func ResourceWifiSniffer() *schema.Resource {
	return newAPI725Resource("/interface/wifi/sniffer", "singleton")
}
