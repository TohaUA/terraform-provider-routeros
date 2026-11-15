package routeros

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

// ResourceInterfacePwrLine manages /interface/pwr-line on RouterOS 7.25+.
// https://manual.mikrotik.com/docs/7.25/cli-reference/interface/pwr-line/
func ResourceInterfacePwrLine() *schema.Resource {
	return newAPI725Resource("/interface/pwr-line", "builtin")
}
