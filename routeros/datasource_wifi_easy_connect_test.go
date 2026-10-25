package routeros

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testDatasourceWiFiEasyConnect = "data.routeros_wifi_easy_connect.test"

func TestAccDatasourceWiFiEasyConnectTest_basic(t *testing.T) {
	t.Run("QR Code", func(t *testing.T) {
		resource.Test(t, resource.TestCase{
			ProviderFactories: testAccProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: testAccDatasourceWiFiEasyConnectConfig(),
					Check: resource.ComposeTestCheckFunc(
						testResourcePrimaryInstanceId(testDatasourceWiFiEasyConnect),
					),
				},
			},
		})

	})
}

// easyConnectID reads the data source with raw and returns the ID it sets.
func easyConnectID(t *testing.T, raw map[string]interface{}) string {
	t.Helper()
	d := schema.TestResourceDataRaw(t, DatasourceWiFiEasyConnect().Schema, raw)
	if diags := datasourceQRGenerate(context.Background(), d, nil); diags.HasError() {
		t.Fatalf("datasourceQRGenerate returned %v", diags)
	}
	if d.Id() == "" {
		t.Fatal("datasourceQRGenerate set no ID")
	}
	return d.Id()
}

// The ID lands in the state and plan output unredacted, so it must not be
// derived from the password: a hash of the WIFI: string can be brute-forced
// offline back to the password.
func TestWifiEasyConnectIDDoesNotDependOnPassword(t *testing.T) {
	// sha1("WIFI:T:WPA2;P:password12345;S:test;;"), the ID the data source used to set
	// for the acceptance test configuration below.
	const passwordHash = "7f2303a727c5894033d46f436811373ec7457f62"

	first := easyConnectID(t, map[string]interface{}{"type": "WPA2", "ssid": "test", "password": "password12345"})
	second := easyConnectID(t, map[string]interface{}{"type": "WPA2", "ssid": "test", "password": "another-password"})

	if first != second {
		t.Errorf("the ID changes with the password: %q vs %q", first, second)
	}
	if first == passwordHash {
		t.Errorf("the ID is the SHA-1 of the WIFI: string carrying the password: %q", first)
	}
	if other := easyConnectID(t, map[string]interface{}{"type": "WPA2", "ssid": "other", "password": "password12345"}); other == first {
		t.Errorf("two SSIDs share the ID %q", first)
	}
	if other := easyConnectID(t, map[string]interface{}{"type": "WEP", "ssid": "test", "password": "password12345"}); other == first {
		t.Errorf("two types share the ID %q", first)
	}
}

func TestWifiEasyConnectDefaultTypeIsValid(t *testing.T) {
	s := DatasourceWiFiEasyConnect().Schema["type"]
	def, ok := s.Default.(string)
	if !ok {
		t.Fatalf("type has no string default: %#v", s.Default)
	}
	if s.ValidateFunc == nil {
		t.Fatal("type has no ValidateFunc")
	}
	if _, errs := s.ValidateFunc(def, "type"); len(errs) > 0 {
		t.Errorf("the default type %q fails its own validation: %v", def, errs)
	}
}

func testAccDatasourceWiFiEasyConnectConfig() string {
	return providerConfig + `

data "routeros_wifi_easy_connect" "test" {
  type     = "WPA2"
  ssid     = "test"
  password = "password12345"
}
`
}
