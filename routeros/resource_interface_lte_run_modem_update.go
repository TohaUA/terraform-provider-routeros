package routeros

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

// https://manual.mikrotik.com/docs/7.25/cli-reference/interface/lte/run-modem-update/
func ResourceInterfaceLteRunModemUpdate() *schema.Resource {
	return newAPI725Action("/interface/lte/run-modem-update")
}
