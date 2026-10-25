package routeros

import (
	"sort"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

// No resource may write back an unchanged sensitive value that configuration does not declare. State cannot
// be trusted to hold the secret: RouterOS reads it back as a mask to an account without the `sensitive`
// policy, so a value taken from state and sent can replace a working credential with the mask. This walks
// every top-level optional sensitive string in the provider, puts a mask in state while configuration says
// nothing about it, and fails if the mask appears anywhere in the request, under any name.
func TestSensitiveValuesAreNotSentUnlessDeclared(t *testing.T) {
	old := RouterOSVersion
	RouterOSVersion = "7.24"
	defer func() { RouterOSVersion = old }()

	const mask = "***"

	resources := Provider().ResourcesMap
	names := make([]string, 0, len(resources))
	for name := range resources {
		names = append(names, name)
	}
	sort.Strings(names)

	checked := 0
	for _, name := range names {
		res := resources[name]

		for field, fieldSchema := range res.Schema {
			if !fieldSchema.Optional || !fieldSchema.Sensitive || fieldSchema.Type != schema.TypeString {
				continue
			}
			checked++

			t.Run(name+"."+field, func(t *testing.T) {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("serializing panicked: %v", r)
					}
				}()

				vals := map[string]cty.Value{}
				for attr, attrType := range res.CoreConfigSchema().ImpliedType().AttributeTypes() {
					vals[attr] = cty.NullVal(attrType)
				}

				d := res.Data(&terraform.InstanceState{
					ID:         "*1",
					Attributes: map[string]string{"id": "*1", field: mask},
					RawConfig:  cty.ObjectVal(vals),
				})

				item, _ := TerraformResourceDataToMikrotik(res.Schema, d)
				for key, value := range item {
					if value == mask {
						t.Errorf("the request carried %s=%q from state although configuration does not declare %s",
							key, value, field)
					}
				}
			})
		}
	}

	if checked == 0 {
		t.Fatal("found no optional sensitive string fields, so the walk is not looking where it should")
	}
	t.Logf("checked %d optional sensitive string fields", checked)
}

// The same rule holds for an Optional+Sensitive field inside a nested TypeList block: a masked value taken from
// state must not be sent when configuration declares the block but leaves the secret out.
func TestNestedListSensitiveValueNotSentUnlessDeclared(t *testing.T) {
	old := RouterOSVersion
	RouterOSVersion = "7.24"
	defer func() { RouterOSVersion = old }()

	const mask = "***"

	res := &schema.Resource{
		Schema: map[string]*schema.Schema{
			MetaResourcePath: PropResourcePath("/test/nested"),
			"block": {
				Type:     schema.TypeList,
				Optional: true,
				MaxItems: 1,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"secret": {
							Type:      schema.TypeString,
							Optional:  true,
							Sensitive: true,
						},
					},
				},
			},
		},
	}

	serialize := func(configSecret cty.Value, stateSecret string) MikrotikItem {
		vals := map[string]cty.Value{}
		for attr, attrType := range res.CoreConfigSchema().ImpliedType().AttributeTypes() {
			vals[attr] = cty.NullVal(attrType)
		}
		vals["block"] = cty.ListVal([]cty.Value{cty.ObjectVal(map[string]cty.Value{
			"name":   cty.StringVal("x"),
			"secret": configSecret,
		})})

		d := res.Data(&terraform.InstanceState{
			ID: "*1",
			Attributes: map[string]string{
				"id":             "*1",
				"block.#":        "1",
				"block.0.name":   "x",
				"block.0.secret": stateSecret,
			},
			RawConfig: cty.ObjectVal(vals),
		})

		item, _ := TerraformResourceDataToMikrotik(res.Schema, d)
		if got := item["block.name"]; got != "x" {
			t.Fatalf("the declared nested field was not sent: block.name=%q, item=%v", got, item)
		}
		return item
	}

	t.Run("undeclared", func(t *testing.T) {
		item := serialize(cty.NullVal(cty.String), mask)
		for key, value := range item {
			if value == mask {
				t.Errorf("the request carried %s=%q from state although configuration does not declare block.secret",
					key, value)
			}
		}
	})

	// The guard must not be broader than the rule: a secret that configuration declares is still sent.
	t.Run("declared", func(t *testing.T) {
		item := serialize(cty.StringVal("real"), "real")
		if got := item["block.secret"]; got != "real" {
			t.Errorf("the declared nested secret was not sent: block.secret=%q, item=%v", got, item)
		}
	})
}
