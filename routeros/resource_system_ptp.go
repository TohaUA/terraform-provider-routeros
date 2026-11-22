package routeros

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

// ResourceSystemPtp manages /system/ptp on RouterOS 7.25.1+.
// https://manual.mikrotik.com/docs/7.25/cli-reference/system/ptp/
func ResourceSystemPtp() *schema.Resource {
	return newAPI725Resource("/system/ptp", "list")
}
