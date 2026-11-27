package routeros

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

var (
	testResource = schema.Resource{
		Schema: map[string]*schema.Schema{
			MetaResourcePath: PropResourcePath("/test/resource"),
			MetaId:           PropId(Id),
			"string": {
				Type: schema.TypeString,
			},
			"float": {
				Type: schema.TypeFloat,
			},
			"int": {
				Type: schema.TypeInt,
			},
			"bool": {
				Type: schema.TypeBool,
			},
			"computed": {
				Type:     schema.TypeBool,
				Computed: true,
			},
		},
	}

	testDatasource = schema.Resource{
		Schema: map[string]*schema.Schema{
			MetaResourcePath: PropResourcePath("/test/resource"),
			MetaId:           PropId(Id),
			"test_name": {
				Type:     schema.TypeList,
				Computed: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"string": {
							Type: schema.TypeString,
						},
						"float": {
							Type: schema.TypeFloat,
						},
						"int": {
							Type: schema.TypeInt,
						},
						"bool": {
							Type: schema.TypeBool,
						},
					},
				},
			},
		},
	}
)

func Test_mikrotikResourceDataToTerraform(t *testing.T) {

	testItem := MikrotikItem{".id": "*39", "string": "string12345", "float": "0.01", "int": "10", "bool": "true"}

	testResourceData := testResource.TestResourceData()
	expectedRes := map[string]interface{}{"string": "string12345", "float": 0.01, "int": 10, "bool": true}

	err := MikrotikResourceDataToTerraform(testItem, testResource.Schema, testResourceData)
	if err != nil {
		t.Errorf("decoding err: %v", err)
	}

	for key, expected := range expectedRes {
		actual := testResourceData.Get(key)

		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("bad: expected:%#v\nactual:%#v", expected, actual)
		}
	}

}

func Test_terraformResourceDataToMikrotik(t *testing.T) {

	expected := MikrotikItem{"string": "string12345", "float": "0.01", "int": "10", "bool": "yes"}

	testResourceData := testResource.TestResourceData()
	testResourceData.SetId("*39")
	testResourceData.Set("string", "string12345")
	testResourceData.Set("float", 0.01)
	testResourceData.Set("int", 10)
	testResourceData.Set("bool", true)

	actual, _ := TerraformResourceDataToMikrotik(testResource.Schema, testResourceData)

	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("bad: expected:%#v\nactual:%#v", expected, actual)
	}
}

func Test_mikrotikResourceDataToTerraformDatasource(t *testing.T) {
	testItems := []MikrotikItem{
		{"string": "string12345", "float": "0.01", "int": "10", "bool": "yes"},
		{"string": "12345string", "float": "0.02", "int": "20", "bool": "no"},
	}

	testResourceData := testDatasource.TestResourceData()
	expectedRes := []map[string]interface{}{
		{MetaResourcePath: "", MetaId: 0, "string": "string12345", "float": 0.01, "int": 10, "bool": true},
		{MetaResourcePath: "", MetaId: 0, "string": "12345string", "float": 0.02, "int": 20, "bool": false},
	}

	err := MikrotikResourceDataToTerraformDatasource(&testItems, "test_name", testDatasource.Schema, testResourceData)
	if err != nil {
		t.Errorf("decoding err: %v", err)
	}

	for i, rec := range testResourceData.Get("test_name").([]interface{}) {
		for key, actual := range rec.(map[string]interface{}) {
			if !reflect.DeepEqual(actual, expectedRes[i][key]) {
				t.Fatalf("bad: (key: %v) expected:%#v\tactual:%#v", key, expectedRes[i][key], actual)
			}
		}
	}
}

func Test_loadTransformSet(t *testing.T) {
	testData := []struct {
		s       string
		reverse bool
	}{
		{toQuotedCommaSeparatedString("channel: channel.config", "datapath: datapath.config"), false},
		{toQuotedCommaSeparatedString("mikrotik-field-name : schema-field-name"), false},
		{toQuotedCommaSeparatedString("channel: channel.config", "datapath: datapath.config"), true},
		{toQuotedCommaSeparatedString("mikrotik-field-name:schema-field-name"), true},
	}

	expected := []map[string]string{
		{"channel": "channel.config", "datapath": "datapath.config"},
		{"mikrotik-field-name": "schema-field-name"},
		{"channel.config": "channel", "datapath.config": "datapath"},
		{"schema-field-name": "mikrotik-field-name"},
	}

	for i, actual := range testData {
		if !reflect.DeepEqual(loadTransformSet(actual.s, actual.reverse), expected[i]) {
			t.Fatalf("bad: (item: %v) expected:%#v\tactual:%#v", i, expected[i], loadTransformSet(actual.s, actual.reverse))
		}
	}
}

func Test_loadSkipFields(t *testing.T) {
	testData := []struct {
		s string
	}{
		{toQuotedCommaSeparatedString("name")},
		{toQuotedCommaSeparatedString("name", "rx_1024_1518", "rx_128_255", "rx_1519_max", "rx_256_511", "rx_512_1023", "rx_64")},
	}

	expected := []map[string]struct{}{
		{"name": struct{}{}},
		{"name": struct{}{}, "rx_1024_1518": struct{}{}, "rx_128_255": struct{}{}, "rx_1519_max": struct{}{},
			"rx_256_511": struct{}{}, "rx_512_1023": struct{}{}, "rx_64": struct{}{}},
	}

	for i, actual := range testData {
		if !reflect.DeepEqual(loadSkipFields(actual.s), expected[i]) {
			t.Fatalf("bad: (item: %v) expected:%#v\tactual:%#v", i, expected[i], loadSkipFields(actual.s))
		}
	}
}

// RouterOS 7.21 replaced the bool `always-allow-password-login` with the tri-state
// `password-authentication`. The resource models the new key as its own attribute, so no drift
// entry may rename it away before the schema lookup; otherwise the attribute is never read back.
func Test_mikrotikResourceDataToTerraform_ipSSHServerPasswordAuthentication(t *testing.T) {
	originalVersion := RouterOSVersion
	defer func() { RouterOSVersion = originalVersion }()

	res := ResourceIpSSHServer()

	testData := []struct {
		version   string
		item      MikrotikItem
		expected  map[string]interface{}
		driftSize int
	}{
		{
			version:   "7.25",
			item:      MikrotikItem{"password-authentication": "yes-if-no-key"},
			expected:  map[string]interface{}{"password_authentication": "yes-if-no-key"},
			driftSize: 0,
		},
		{
			version:   "7.25",
			item:      MikrotikItem{"password-authentication": "no"},
			expected:  map[string]interface{}{"password_authentication": "no"},
			driftSize: 0,
		},
	}

	for _, tc := range testData {
		RouterOSVersion = tc.version

		if drift := driftAttributeSlice.GetDriftMap(tc.version, "/ip/ssh", true); len(drift) != tc.driftSize {
			t.Fatalf("bad: (version: %v) unexpected /ip/ssh drift map %#v", tc.version, drift)
		}

		d := res.TestResourceData()
		if diags := MikrotikResourceDataToTerraform(tc.item, res.Schema, d); len(diags) != 0 {
			t.Fatalf("bad: (version: %v) unexpected diagnostics: %v", tc.version, diags)
		}

		for key, expected := range tc.expected {
			if actual := d.Get(key); !reflect.DeepEqual(actual, expected) {
				t.Fatalf("bad: (version: %v) %v expected:%#v\tactual:%#v", tc.version, key, expected, actual)
			}
		}
	}
}

func Test_terraformResourceDataToMikrotik_TypeIntSetUnsetFields(t *testing.T) {
	makeResource := func() *schema.Resource {
		return &schema.Resource{
			Schema: map[string]*schema.Schema{
				MetaResourcePath:   PropResourcePath("/test/int-unset"),
				MetaId:             PropId(Id),
				MetaSetUnsetFields: PropSetUnsetFields("client_mtu"),
				"comment": {
					Type:     schema.TypeString,
					Optional: true,
				},
				"client_mtu": {
					Type:     schema.TypeInt,
					Optional: true,
				},
			},
		}
	}

	t.Run("removed_int_attr_emits_unset", func(t *testing.T) {
		res := makeResource()
		_, attrs := blockTestNullRawConfig(res)
		state := &terraform.InstanceState{
			ID: "*1",
			Attributes: map[string]string{
				"id":         "*1",
				"client_mtu": "1380",
			},
			RawConfig: cty.ObjectVal(attrs),
		}
		cfg := terraform.NewResourceConfigRaw(map[string]interface{}{})
		cfg.CtyValue = cty.ObjectVal(attrs)

		var item MikrotikItem
		res.UpdateContext = func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			item, _ = TerraformResourceDataToMikrotik(res.Schema, d)
			return nil
		}
		diff, err := res.Diff(context.Background(), state, cfg, nil)
		if err != nil {
			t.Fatalf("diff: %v", err)
		}
		if diff == nil || diff.Attributes["client_mtu"] == nil {
			t.Fatalf("expected diff on client_mtu, got %#v", diff)
		}
		if _, diags := res.Apply(context.Background(), state, diff, nil); diags.HasError() {
			t.Fatalf("apply: %v", diags)
		}
		if got, ok := item["!client-mtu"]; !ok || got != "" {
			t.Fatalf("expected !client-mtu=\"\", got item=%#v", item)
		}
		if _, ok := item["client-mtu"]; ok {
			t.Fatalf("did not expect client-mtu in item=%#v", item)
		}
	})

	t.Run("explicit_zero_int_attr_emits_zero", func(t *testing.T) {
		res := makeResource()
		_, attrs := blockTestNullRawConfig(res)
		attrs["client_mtu"] = cty.NumberIntVal(0)
		state := &terraform.InstanceState{
			ID: "*1",
			Attributes: map[string]string{
				"id":         "*1",
				"client_mtu": "1380",
			},
			RawConfig: cty.ObjectVal(attrs),
		}
		cfg := terraform.NewResourceConfigRaw(map[string]interface{}{
			"client_mtu": 0,
		})
		cfg.CtyValue = cty.ObjectVal(attrs)

		var item MikrotikItem
		res.UpdateContext = func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			item, _ = TerraformResourceDataToMikrotik(res.Schema, d)
			return nil
		}
		diff, err := res.Diff(context.Background(), state, cfg, nil)
		if err != nil {
			t.Fatalf("diff: %v", err)
		}
		if diff == nil || diff.Attributes["client_mtu"] == nil {
			t.Fatalf("expected diff on client_mtu, got %#v", diff)
		}
		if _, diags := res.Apply(context.Background(), state, diff, nil); diags.HasError() {
			t.Fatalf("apply: %v", diags)
		}
		if got, ok := item["client-mtu"]; !ok || got != "0" {
			t.Fatalf("expected client-mtu=\"0\", got item=%#v", item)
		}
		if _, ok := item["!client-mtu"]; ok {
			t.Fatalf("did not expect !client-mtu when explicitly set to 0, got item=%#v", item)
		}
	})

	t.Run("never_set_unrelated_update_omits_both", func(t *testing.T) {
		res := makeResource()
		_, attrs := blockTestNullRawConfig(res)
		attrs["comment"] = cty.StringVal("new")
		state := &terraform.InstanceState{
			ID: "*1",
			Attributes: map[string]string{
				"id":      "*1",
				"comment": "old",
			},
			RawConfig: cty.ObjectVal(attrs),
		}
		cfg := terraform.NewResourceConfigRaw(map[string]interface{}{
			"comment": "new",
		})
		cfg.CtyValue = cty.ObjectVal(attrs)

		var item MikrotikItem
		res.UpdateContext = func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			item, _ = TerraformResourceDataToMikrotik(res.Schema, d)
			return nil
		}
		diff, err := res.Diff(context.Background(), state, cfg, nil)
		if err != nil {
			t.Fatalf("diff: %v", err)
		}
		if _, diags := res.Apply(context.Background(), state, diff, nil); diags.HasError() {
			t.Fatalf("apply: %v", diags)
		}
		if item["comment"] != "new" {
			t.Fatalf("expected comment=new, got item=%#v", item)
		}
		if _, ok := item["client-mtu"]; ok {
			t.Fatalf("did not expect client-mtu, got item=%#v", item)
		}
		if _, ok := item["!client-mtu"]; ok {
			t.Fatalf("did not expect !client-mtu, got item=%#v", item)
		}
	})

	t.Run("null_raw_config_does_not_panic", func(t *testing.T) {
		res := &schema.Resource{
			Schema: map[string]*schema.Schema{
				MetaResourcePath:   PropResourcePath("/test/int-unset-null"),
				MetaId:             PropId(Id),
				MetaSetUnsetFields: PropSetUnsetFields("client_mtu"),
				"comment": {
					Type:     schema.TypeString,
					Optional: true,
				},
				"client_mtu": {
					Type: schema.TypeInt,
				},
			},
		}
		d := res.TestResourceData()
		d.SetId("*1")
		item, _ := TerraformResourceDataToMikrotik(res.Schema, d)
		if got, ok := item["!client-mtu"]; !ok || got != "" {
			t.Fatalf("expected !client-mtu=\"\" with null raw config, got item=%#v", item)
		}
	})
}
