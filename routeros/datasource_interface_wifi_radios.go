package routeros

import (
	"sort"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func DatasourceWifiRadios() *schema.Resource {
	r := newAPI725Datasource("/interface/wifi/radio", false)
	fields := r.Schema["data"].Elem.(*schema.Resource).Schema
	var transforms []string
	for wire, name := range map[string]string{"2g-channels": "channels_2ghz", "5g-channels": "channels_5ghz", "6g-channels": "channels_6ghz", "60g-channels": "channels_60ghz", "s1g-channels": "channels_s1ghz"} {
		fields[name] = fields[api725Name(wire)]
		delete(fields, api725Name(wire))
		transforms = append(transforms, name+":"+wire)
	}
	sort.Strings(transforms)
	r.Schema[MetaTransformSet] = PropTransformSet(transforms...)
	return r
}
