package routeros

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// Band and width values documented for /interface/wifi/channel and
// /interface/wifi/provisioning (https://help.mikrotik.com/docs/spaces/ROS/pages/224559120/WiFi).
// These are pure schema tests: no device is needed, so a validator regression
// (a value dropped from a StringInSlice list) fails in `go test` rather than at
// `terraform plan` time.

var testWifiBands = []string{
	"2ghz-g", "2ghz-n", "2ghz-ax", "2ghz-be",
	"5ghz-a", "5ghz-ac", "5ghz-an", "5ghz-ax", "5ghz-be", "5ghz-n",
	"6ghz-ax", "6ghz-be",
}

var testWifiWidths = []string{
	"20mhz", "20/40mhz", "20/40mhz-Ce", "20/40mhz-eC",
	"20/40/80mhz", "20/40/80+80mhz", "20/40/80/160mhz", "20/40/80/160/320mhz",
}

var testWifiInvalidValues = []string{"", "2ghz-b", "2ghz-b/g/n", "5ghz-a/n/ac", "6ghz-n", "7ghz-be", "40mhz", "320mhz"}

func testValidateFunc(t *testing.T, name string, f schema.SchemaValidateFunc, valid, invalid []string) {
	t.Helper()
	if f == nil {
		t.Fatalf("%s: ValidateFunc is nil", name)
	}
	for _, v := range valid {
		if _, errs := f(v, name); len(errs) != 0 {
			t.Errorf("%s: %q rejected: %v", name, v, errs)
		}
	}
	for _, v := range invalid {
		if _, errs := f(v, name); len(errs) == 0 {
			t.Errorf("%s: %q accepted, want an error", name, v)
		}
	}
}

func Test_resourceWifiChannelValidators(t *testing.T) {
	s := ResourceWifiChannel().Schema
	testValidateFunc(t, "band", s["band"].ValidateFunc, testWifiBands, testWifiInvalidValues)
	testValidateFunc(t, "width", s["width"].ValidateFunc, testWifiWidths, testWifiInvalidValues)
}

func Test_resourceWifiProvisioningSupportedBandsValidator(t *testing.T) {
	elem, ok := ResourceWifiProvisioning().Schema["supported_bands"].Elem.(*schema.Schema)
	if !ok {
		t.Fatal("supported_bands: Elem is not *schema.Schema")
	}
	testValidateFunc(t, "supported_bands", elem.ValidateFunc, testWifiBands, testWifiInvalidValues)
}
