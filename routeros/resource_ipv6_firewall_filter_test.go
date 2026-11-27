package routeros

import (
	"context"
	"fmt"
	"maps"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const testIPv6FirewallFilterAddress = "routeros_ipv6_firewall_filter.rule"

func TestAccIPv6FirewallFilterTest_basic(t *testing.T) {
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/ipv6/firewall/filter", "routeros_ipv6_firewall_filter"),
				Steps: []resource.TestStep{
					{
						Config: testAccIPv6FirewallFilterConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testIPv6FirewallFilterAddress),
							resource.TestCheckResourceAttr(testIPv6FirewallFilterAddress, "action", "drop"),
						),
					},
				},
			})

		})
	}
}

func TestAccIPv6FirewallFilter_placeBeforeUpdate(t *testing.T) {
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/ipv6/firewall/filter", "routeros_ipv6_firewall_filter"),
				Steps: []resource.TestStep{
					{
						Config: testAccIPv6FirewallFilterPlaceBeforeConfig("before-update"),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("routeros_ipv6_firewall_filter.positioned", "comment", "before-update"),
							testCheckIPv6FilterPrecedesAnchor,
						),
					},
					{
						Config: testAccIPv6FirewallFilterPlaceBeforeConfig("after-update"),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("routeros_ipv6_firewall_filter.positioned", "comment", "after-update"),
							testCheckIPv6FilterPrecedesAnchor,
						),
					},
				},
			})
		})
	}
}

func testCheckIPv6FilterPrecedesAnchor(s *terraform.State) error {
	positioned := s.RootModule().Resources["routeros_ipv6_firewall_filter.positioned"].Primary.ID
	anchor := s.RootModule().Resources["routeros_ipv6_firewall_filter.anchor"].Primary.ID
	items, err := ReadItems(nil, "/ipv6/firewall/filter", testAccProvider.Meta().(Client))
	if err != nil {
		return err
	}
	positionedIndex, anchorIndex := -1, -1
	for i, item := range *items {
		switch item.GetID(Id) {
		case positioned:
			positionedIndex = i
		case anchor:
			anchorIndex = i
		}
	}
	if positionedIndex < 0 || anchorIndex < 0 || positionedIndex >= anchorIndex {
		return fmt.Errorf("positioned rule index=%d must precede anchor index=%d", positionedIndex, anchorIndex)
	}
	return nil
}

func testAccIPv6FirewallFilterPlaceBeforeConfig(comment string) string {
	return providerConfig + fmt.Sprintf(`
resource "routeros_ipv6_firewall_filter" "anchor" {
  action  = "accept"
  chain   = "routeros-test-place-before"
  comment = "place-before-anchor"
}

resource "routeros_ipv6_firewall_filter" "positioned" {
  action       = "drop"
  chain        = "routeros-test-place-before"
  comment      = %q
  place_before = routeros_ipv6_firewall_filter.anchor.id
}
`, comment)
}

func testAccIPv6FirewallFilterConfig() string {
	return providerConfig + `

resource "routeros_ipv6_firewall_filter" "rule" {
	// add action=drop chain=forward comment="" hop-limit= protocol=icmpv6
	action 		= "drop"
	chain   	= "forward"
	comment 	= "Check drop hop-limit=1 + ipv6 multicast"
	src_address = "ff00::/8"
	hop_limit	= "equal:1"
	protocol 	= "icmpv6"
  }

`
}

func TestAccIPv6FirewallFilter_addressList(t *testing.T) {
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/ipv6/firewall/filter", "routeros_ipv6_firewall_filter"),
				Steps: []resource.TestStep{
					{
						Config: testAccIPv6FirewallFilterAddressListConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId("routeros_ipv6_firewall_filter.address_list"),
							resource.TestCheckResourceAttr("routeros_ipv6_firewall_filter.address_list", "action", "add-src-to-address-list"),
							resource.TestCheckResourceAttr("routeros_ipv6_firewall_filter.address_list", "address_list", "test-v6-list"),
							resource.TestCheckResourceAttr("routeros_ipv6_firewall_filter.address_list", "address_list_timeout", "10s"),
							resource.TestCheckResourceAttr("routeros_ipv6_firewall_filter.address_list", "chain", "forward"),
						),
					},
				},
			})
		})
	}
}

func testAccIPv6FirewallFilterAddressListConfig() string {
	return providerConfig + `
resource "routeros_ipv6_firewall_filter" "address_list" {
  action               = "add-src-to-address-list"
  address_list         = "test-v6-list"
  address_list_timeout = "10s"
  chain                = "forward"
  protocol             = "tcp"
}
`
}

func TestIPv6FirewallFilterAddressList_SchemaAndValidation(t *testing.T) {
	if err := Provider().InternalValidate(); err != nil {
		t.Fatalf("Provider().InternalValidate() failed: %v", err)
	}

	res := ResourceIPv6FirewallFilter()
	resAttr, ok := res.Schema["address_list"]
	if !ok {
		t.Fatal("ResourceIPv6FirewallFilter().Schema missing address_list")
	}
	if resAttr.Type != schema.TypeString {
		t.Errorf("resource address_list Type = %v, want %v", resAttr.Type, schema.TypeString)
	}
	if !resAttr.Optional {
		t.Error("resource address_list Optional = false, want true")
	}
	if resAttr.Computed {
		t.Error("resource address_list Computed = true, want false")
	}

	ds := DatasourceIPv6Firewall()
	rulesAttr, ok := ds.Schema["rules"]
	if !ok {
		t.Fatal("DatasourceIPv6Firewall().Schema missing rules")
	}
	rulesRes, ok := rulesAttr.Elem.(*schema.Resource)
	if !ok {
		t.Fatalf("DatasourceIPv6Firewall().Schema[rules].Elem = %T, want *schema.Resource", rulesAttr.Elem)
	}
	dsAttr, ok := rulesRes.Schema["address_list"]
	if !ok {
		t.Fatal("DatasourceIPv6Firewall().Schema[rules] missing address_list")
	}
	if dsAttr.Type != schema.TypeString {
		t.Errorf("datasource rules.address_list Type = %v, want %v", dsAttr.Type, schema.TypeString)
	}
	if !dsAttr.Computed {
		t.Error("datasource rules.address_list Computed = false, want true")
	}
}

func TestIPv6FirewallFilterAddressList_SerializationAndUpdate(t *testing.T) {
	old := RouterOSVersion
	RouterOSVersion = "7.25"
	defer func() { RouterOSVersion = old }()

	res := ResourceIPv6FirewallFilter()

	for _, action := range []string{"add-src-to-address-list", "add-dst-to-address-list"} {
		t.Run(action, func(t *testing.T) {
			d := resourceDataWithRawConfig(res, "", map[string]cty.Value{
				"action":               cty.StringVal(action),
				"chain":                cty.StringVal("forward"),
				"address_list":         cty.StringVal("test-v6-list"),
				"address_list_timeout": cty.StringVal("00:00:10"),
			})
			for k, v := range map[string]string{
				"action":               action,
				"chain":                "forward",
				"address_list":         "test-v6-list",
				"address_list_timeout": "00:00:10",
			} {
				if err := d.Set(k, v); err != nil {
					t.Fatalf("d.Set(%q) failed: %v", k, err)
				}
			}

			item, _ := TerraformResourceDataToMikrotik(res.Schema, d)
			if got := item["action"]; got != action {
				t.Errorf("item[action] = %q, want %q", got, action)
			}
			if got := item["chain"]; got != "forward" {
				t.Errorf("item[chain] = %q, want %q", got, "forward")
			}
			if got := item["address-list"]; got != "test-v6-list" {
				t.Errorf("item[address-list] = %q, want %q", got, "test-v6-list")
			}
			if got := item["address-list-timeout"]; got != "00:00:10" {
				t.Errorf("item[address-list-timeout] = %q, want %q", got, "00:00:10")
			}

			readRD := res.TestResourceData()
			diags := MikrotikResourceDataToTerraform(MikrotikItem{
				".id":                  "*1",
				"action":               action,
				"chain":                "forward",
				"address-list":         "test-v6-list",
				"address-list-timeout": "00:00:10",
			}, res.Schema, readRD)
			if len(diags) != 0 {
				t.Fatalf("MikrotikResourceDataToTerraform returned unexpected diagnostics: %v", diags)
			}
			if got := readRD.Get("action"); got != action {
				t.Errorf("readRD.Get(action) = %v, want %q", got, action)
			}
			if got := readRD.Get("address_list"); got != "test-v6-list" {
				t.Errorf("readRD.Get(address_list) = %v, want %q", got, "test-v6-list")
			}
			if got := readRD.Get("address_list_timeout"); got != "00:00:10" {
				t.Errorf("readRD.Get(address_list_timeout) = %v, want %q", got, "00:00:10")
			}
		})
	}

	t.Run("update sends address-list and omits place-before", func(t *testing.T) {
		c := &recordingClient{}
		d := resourceDataWithRawConfig(res, "*1", map[string]cty.Value{
			"action":       cty.StringVal("add-src-to-address-list"),
			"chain":        cty.StringVal("forward"),
			"address_list": cty.StringVal("updated-v6-list"),
			"place_before": cty.StringVal("*5"),
		})
		res.UpdateContext(context.Background(), d, c)

		updates := c.sent[crudUpdate]
		if len(updates) != 1 {
			t.Fatalf("expected 1 update request, got %v", c.sent)
		}
		if got := updates[0]["address-list"]; got != "updated-v6-list" {
			t.Errorf("update sent address-list=%q, want %q (full payload: %v)", got, "updated-v6-list", updates[0])
		}
		if _, ok := updates[0]["place-before"]; ok {
			t.Errorf("update unexpectedly sent place-before: %v", updates[0])
		}
	})
}

func TestIPv6FirewallFilterAddressList_Datasource(t *testing.T) {
	old := RouterOSVersion
	RouterOSVersion = "7.25"
	defer func() { RouterOSVersion = old }()

	ds := DatasourceIPv6Firewall()
	s := maps.Clone(ds.Schema)
	s[MetaResourcePath] = PropResourcePath("/ipv6/firewall/filter")

	d := ds.TestResourceData()
	items := []MikrotikItem{
		{
			".id":          "*1",
			"action":       "add-src-to-address-list",
			"chain":        "forward",
			"address-list": "test-v6-list",
		},
	}

	diags := MikrotikResourceDataToTerraformDatasource(&items, "rules", s, d)
	if len(diags) != 0 {
		t.Fatalf("MikrotikResourceDataToTerraformDatasource returned unexpected diagnostics: %v", diags)
	}

	rules, ok := d.Get("rules").([]interface{})
	if !ok || len(rules) != 1 {
		t.Fatalf("rules = %#v, want 1 item", d.Get("rules"))
	}
	ruleMap, ok := rules[0].(map[string]interface{})
	if !ok {
		t.Fatalf("rules[0] = %T, want map[string]interface{}", rules[0])
	}
	if got := ruleMap["address_list"]; got != "test-v6-list" {
		t.Errorf("rules[0][address_list] = %v, want %q", got, "test-v6-list")
	}
	if got := ruleMap["action"]; got != "add-src-to-address-list" {
		t.Errorf("rules[0][action] = %v, want %q", got, "add-src-to-address-list")
	}

	readRD := schema.TestResourceDataRaw(t, ds.Schema, map[string]interface{}{
		"rules": []interface{}{map[string]interface{}{}},
	})
	client := &recordingClient{
		readItem: MikrotikItem{
			".id":          "*1",
			"action":       "add-dst-to-address-list",
			"chain":        "forward",
			"address-list": "ds-v6-list",
		},
	}
	readDiags := ds.ReadContext(context.Background(), readRD, client)
	if len(readDiags) != 0 {
		t.Fatalf("ds.ReadContext returned unexpected diagnostics: %v", readDiags)
	}
	readRules := readRD.Get("rules").([]interface{})
	if len(readRules) != 1 {
		t.Fatalf("ds.ReadContext rules = %#v, want 1 item", readRules)
	}
	if got := readRules[0].(map[string]interface{})["address_list"]; got != "ds-v6-list" {
		t.Errorf("ds.ReadContext rules[0][address_list] = %v, want %q", got, "ds-v6-list")
	}
}
