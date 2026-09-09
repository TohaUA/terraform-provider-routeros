package routeros

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testSystemPackageUpdate = "routeros_system_package_update.test"

// RouterOS rejects a request carrying a property the menu does not accept, so the read-only properties
// must stay out of the request even though the router reports them. Only the channel may be sent.
func TestSystemPackageUpdateSendsOnlyTheChannel(t *testing.T) {
	old := RouterOSVersion
	RouterOSVersion = "7.24"
	defer func() { RouterOSVersion = old }()

	res := ResourceSystemPackageUpdate()
	d := resourceDataWithRawConfig(res, "system-package-update", map[string]cty.Value{
		"channel": cty.StringVal("stable"),
	})
	// The read fills these in; they are in the state before every update.
	for field, value := range map[string]string{
		"check_certificate": "true",
		"installed_version": "7.24",
		"ip_version":        "any",
		"latest_version":    "7.24",
		"mode":              "auto",
		"status":            "System is already up to date",
	} {
		if err := d.Set(field, value); err != nil {
			t.Fatal(err)
		}
	}

	expected := MikrotikItem{"channel": "stable"}

	actual, meta := TerraformResourceDataToMikrotik(res.Schema, d)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("bad: expected:%#v\nactual:%#v", expected, actual)
	}
	if meta.Path != "/system/package/update" {
		t.Fatalf("the request would go to %q", meta.Path)
	}
}

// Every property a RouterOS 7.24 router returns from this menu has to be declared. An undeclared one is
// not an error, it is a warning on every single read, which is how a schema quietly rots.
func TestSystemPackageUpdateReadsTheWholeMenu(t *testing.T) {
	old := RouterOSVersion
	RouterOSVersion = "7.24"
	defer func() { RouterOSVersion = old }()

	res := ResourceSystemPackageUpdate()
	item := MikrotikItem{
		"channel":           "stable",
		"check-certificate": "true",
		"installed-version": "7.24",
		"ip-version":        "any",
		"latest-version":    "7.24",
		"mode":              "auto",
		"status":            "System is already up to date",
	}

	d := res.TestResourceData()
	diags := MikrotikResourceDataToTerraform(item, res.Schema, d)
	if len(diags) != 0 {
		t.Fatalf("reading the menu was not clean: %v", diags)
	}

	for key, want := range map[string]interface{}{
		"channel":           "stable",
		"check_certificate": "true",
		"installed_version": "7.24",
		"ip_version":        "any",
		"latest_version":    "7.24",
		"mode":              "auto",
		"status":            "System is already up to date",
	} {
		if got := d.Get(key); !reflect.DeepEqual(got, want) {
			t.Fatalf("bad: (key: %v) expected:%#v\tactual:%#v", key, want, got)
		}
	}
}

// The second step leaves the container on the long-term channel. Deleting a singleton only drops it from
// state, so nothing puts the channel back, and any later test in the same job sees long-term. That is how
// every other singleton test in this repository behaves, and no test here reads the channel, but it is
// shared state and worth knowing about before another one starts depending on the default.
func TestAccSystemPackageUpdateTest_basic(t *testing.T) {
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				Steps: []resource.TestStep{
					{
						Config: testAccSystemPackageUpdateConfig("stable"),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testSystemPackageUpdate),
							resource.TestCheckResourceAttr(testSystemPackageUpdate, "channel", "stable"),
							resource.TestCheckResourceAttrSet(testSystemPackageUpdate, "installed_version"),
						),
					},
					{
						Config: testAccSystemPackageUpdateConfig("long-term"),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(testSystemPackageUpdate, "channel", "long-term"),
						),
					},
				},
			})

		})
	}
}

func testAccSystemPackageUpdateConfig(channel string) string {
	return fmt.Sprintf(`%v

resource "routeros_system_package_update" "test" {
  channel = "%v"
}
`, providerConfig, channel)
}
