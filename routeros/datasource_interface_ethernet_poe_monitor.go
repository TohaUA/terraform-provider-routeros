package routeros

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

// https://manual.mikrotik.com/docs/7.25/cli-reference/interface/ethernet/poe/monitor/
func DatasourceInterfaceEthernetPoeMonitor() *schema.Resource {
	return newAPI725Datasource("/interface/ethernet/poe/monitor", true)
}
