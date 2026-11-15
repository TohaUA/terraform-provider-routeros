package routeros

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

func DatasourceSystemScriptEnvironment() *schema.Resource {
	// Use the read-only print command so arbitrary global values never reach
	// transport body logs, even if their names do not resemble credentials.
	r := newAPI725Datasource("/system/script/environment/print", true)
	r.Description = "Read global script variables visible to the API user on RouterOS 7.25+. Values are sensitive and are stored in Terraform state."
	return r
}
