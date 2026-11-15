package routeros

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

// https://manual.mikrotik.com/docs/7.25/cli-reference/routing/pimsm/uib-g/
func DatasourceRoutingPimsmUibG() *schema.Resource {
	return newAPI725Datasource("/routing/pimsm/uib-g", false)
}
