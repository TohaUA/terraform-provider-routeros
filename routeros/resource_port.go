package routeros

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

// ResourcePort manages /port on RouterOS 7.25.1+.
// https://manual.mikrotik.com/docs/7.25/cli-reference/port/
func ResourcePort() *schema.Resource {
	return newAPI725Resource("/port", "builtin")
}
