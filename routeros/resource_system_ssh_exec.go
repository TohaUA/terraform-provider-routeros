package routeros

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

// https://manual.mikrotik.com/docs/7.25/cli-reference/system/ssh-exec/
func ResourceSystemSSHExec() *schema.Resource {
	return newAPI725Action("/system/ssh-exec")
}
