package routeros

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

// https://manual.mikrotik.com/docs/7.25/cli-reference/caps-man/actual-interface-configuration/
func DatasourceCapsManActualInterfaceConfiguration() *schema.Resource {
	return newAPI725Datasource("/caps-man/actual-interface-configuration", false)
}
