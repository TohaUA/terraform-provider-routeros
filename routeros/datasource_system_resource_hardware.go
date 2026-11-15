package routeros

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

// https://manual.mikrotik.com/docs/7.25/cli-reference/system/resource/hardware/
func DatasourceSystemResourceHardware() *schema.Resource {
	return newAPI725Datasource("/system/resource/hardware", false)
}
