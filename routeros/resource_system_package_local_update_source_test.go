package routeros

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testSystemPackageLocalUpdateSourceMinVersion = "7.17"
const testSystemPackageLocalUpdateSource = "routeros_system_package_local_update_source.test"

// The password has to reach the device. Listing it in the skip fields is the obvious way to keep a masked
// value out of the state, and it is the wrong one: a skipped field is left out of the request as well, so
// the entry would be written without the credential it exists to carry. This fails if anyone tries it.
func TestSystemPackageLocalUpdateSourceSendsThePassword(t *testing.T) {
	old := RouterOSVersion
	RouterOSVersion = "7.24"
	defer func() { RouterOSVersion = old }()

	res := ResourceSystemPackageLocalUpdateSource()
	d := resourceDataWithRawConfig(res, "*1", map[string]cty.Value{
		"address":  cty.StringVal("192.168.88.1"),
		"user":     cty.StringVal("package-source"),
		"password": cty.StringVal("package-source-secret"),
	})

	expected := MikrotikItem{
		"address":  "192.168.88.1",
		"user":     "package-source",
		"password": "package-source-secret",
	}

	actual, meta := TerraformResourceDataToMikrotik(res.Schema, d)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("bad: expected:%#v\nactual:%#v", expected, actual)
	}
	if meta.Path != "/system/package/local-update/update-package-source" {
		t.Fatalf("the request would go to %q", meta.Path)
	}
}

// A router that answers an account without the `sensitive` policy returns a mask where the password is.
// Reading such an entry must still succeed and must leave the address and the user alone, because those
// are what an import and the next plan recognise the entry by.
func TestSystemPackageLocalUpdateSourceReadsAMaskedPassword(t *testing.T) {
	old := RouterOSVersion
	RouterOSVersion = "7.24"
	defer func() { RouterOSVersion = old }()

	res := ResourceSystemPackageLocalUpdateSource()
	item := MikrotikItem{
		".id":      "*1",
		"address":  "192.168.88.1",
		"user":     "package-source",
		"password": "***",
	}
	expected := map[string]interface{}{
		"address":  "192.168.88.1",
		"user":     "package-source",
		"password": "***",
	}

	d := res.TestResourceData()
	if diags := MikrotikResourceDataToTerraform(item, res.Schema, d); diags.HasError() {
		t.Fatalf("decoding err: %v", diags)
	}

	for key, want := range expected {
		if got := d.Get(key); !reflect.DeepEqual(got, want) {
			t.Fatalf("bad: (key: %v) expected:%#v\tactual:%#v", key, want, got)
		}
	}
}

func TestAccSystemPackageLocalUpdateSourceTest_basic(t *testing.T) {
	if !testCheckMinVersion(t, testSystemPackageLocalUpdateSourceMinVersion) {
		t.Logf("Test skipped, the minimum required version is %v", testSystemPackageLocalUpdateSourceMinVersion)
		return
	}

	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy: testCheckResourceDestroy("/system/package/local-update/update-package-source",
					"routeros_system_package_local_update_source"),
				Steps: []resource.TestStep{
					{
						Config: testAccSystemPackageLocalUpdateSourceConfig("package-source"),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testSystemPackageLocalUpdateSource),
							resource.TestCheckResourceAttr(testSystemPackageLocalUpdateSource, "address", "192.168.88.1"),
							resource.TestCheckResourceAttr(testSystemPackageLocalUpdateSource, "user", "package-source"),
						),
					},
					{
						Config: testAccSystemPackageLocalUpdateSourceConfig("package-source-2"),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr(testSystemPackageLocalUpdateSource, "user", "package-source-2"),
						),
					},
				},
			})

		})
	}
}

func testAccSystemPackageLocalUpdateSourceConfig(user string) string {
	return fmt.Sprintf(`%v

resource "routeros_system_package_local_update_source" "test" {
  address  = "192.168.88.1"
  user     = "%v"
  password = "package-source-secret"
}
`, providerConfig, user)
}
