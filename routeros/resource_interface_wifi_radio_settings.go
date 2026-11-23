package routeros

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

func ResourceWifiRadioSettings() *schema.Resource {
	r := newAPI725Resource("/interface/wifi/radio/settings", "singleton")
	r.Schema["wifi_band"].ValidateFunc = validation.Any(validation.StringInSlice([]string{""}, false), r.Schema["wifi_band"].ValidateFunc)
	r.Schema[MetaSetUnsetFields] = PropSetUnsetFields("wifi_band")
	return protectWifi7Inputs(r, []string{"external_antenna", "wifi_band"})
}
