package routeros

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
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

// AlwaysPresentNotUserProvided keeps the password out of the diff when no configuration declares one, and
// it is easy to read that as the value never being written. It is not: the serializer skips an Optional
// field only when the state value is empty too, so an apply that touches this entry for any other reason
// sends whatever is in state, and for an account without the `sensitive` policy that is the mask rather
// than the credential. Import a source, change its user, and the password on the source router becomes
// three asterisks. Only interface_lte_apn and interface_w60g suppress a password's own diff the same way,
// and neither says so, so there is not much precedent to lean on. This test pins the behaviour instead,
// so that the documentation and the code cannot drift apart: if the serializer is ever taught to leave a
// suppressed field out of the request, this fails and the prose gets rewritten, rather than quietly
// becoming true by accident.
func TestSystemPackageLocalUpdateSourceSendsTheMaskFromState(t *testing.T) {
	old := RouterOSVersion
	RouterOSVersion = "7.24"
	defer func() { RouterOSVersion = old }()

	res := ResourceSystemPackageLocalUpdateSource()

	// The configuration names the address and the user and says nothing about the password. The state
	// carries what the last read returned, which on such an account is a mask.
	vals := map[string]cty.Value{}
	for name, aType := range res.CoreConfigSchema().ImpliedType().AttributeTypes() {
		vals[name] = cty.NullVal(aType)
	}
	vals["address"] = cty.StringVal("192.168.88.1")
	vals["user"] = cty.StringVal("package-source-2")

	d := res.Data(&terraform.InstanceState{
		ID: "*1",
		Attributes: map[string]string{
			"id":       "*1",
			"address":  "192.168.88.1",
			"user":     "package-source",
			"password": "***",
		},
		RawConfig: cty.ObjectVal(vals),
	})

	actual, _ := TerraformResourceDataToMikrotik(res.Schema, d)
	if got, ok := actual["password"]; !ok || got != "***" {
		t.Fatalf("the request carried password %#v (present: %v); the resource note and the docs page "+
			"both say the masked value is what reaches the device, so one of the two is now wrong", got, ok)
	}
}

// MikroTik documents this menu as appearing in 7.17beta3, so the guard below keeps the test off a
// container that does not have it. Be clear about what that costs today: module_testing.yml runs its
// matrix on 7.12, 7.15 and 7.16, every one of them below the guard, so this test skips on every job and
// the resource ships with no acceptance coverage at all. Adding a 7.17 or newer container to that matrix
// is what turns this back into a test. Even then it would not exercise the masked-password path, because
// the workflow logs in as `admin`, which carries the `sensitive` policy and so reads the real password
// back rather than a mask.
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
