package routeros

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

// https://manual.mikrotik.com/docs/7.25/cli-reference/interface/lte/show-capabilities/
func DatasourceInterfaceLteCapabilities() *schema.Resource {
	return newAPI725Datasource("/interface/lte/show-capabilities", true)
}
