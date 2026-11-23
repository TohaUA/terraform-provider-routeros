package routeros

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func DatasourceWifiMonitor() *schema.Resource {
	r := newAPI725Datasource("/interface/wifi/monitor", true)
	r.Description += " Uses once to read a finite snapshot, including MLD link ID, channel/puncturing display, and AFC status when the driver exposes them. Select an interface with arguments = { numbers = \"wifi1\" }."
	return r
}
