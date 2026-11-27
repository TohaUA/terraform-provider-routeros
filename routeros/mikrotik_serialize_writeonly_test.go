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

func TestProviderInternalValidate(t *testing.T) {
	if err := Provider().InternalValidate(); err != nil {
		t.Fatalf("Provider().InternalValidate() failed: %v", err)
	}
}

func testWriteOnlyResourceConfig(res *schema.Resource, attrs map[string]cty.Value) *terraform.ResourceConfig {
	ty := res.CoreConfigSchema().ImpliedType()
	vals := make(map[string]cty.Value, len(ty.AttributeTypes()))
	legacy := make(map[string]interface{}, len(attrs))
	for name, attrType := range ty.AttributeTypes() {
		if v, ok := attrs[name]; ok {
			vals[name] = v
			if !v.IsNull() && v.IsKnown() {
				switch v.Type() {
				case cty.String:
					legacy[name] = v.AsString()
				case cty.Number:
					i, _ := v.AsBigFloat().Int64()
					legacy[name] = int(i)
				case cty.Bool:
					legacy[name] = v.True()
				}
			}
		} else {
			vals[name] = cty.NullVal(attrType)
		}
	}

	rc := terraform.NewResourceConfigRaw(legacy)
	rc.CtyValue = cty.ObjectVal(vals)
	return rc
}

func TestWriteOnlySerializer(t *testing.T) {
	res := ResourcePPPSecret()

	t.Run("create sends write-only secret from raw config and omits version attr", func(t *testing.T) {
		d := resourceDataWithRawConfig(res, "", map[string]cty.Value{
			KeyName:               cty.StringVal("alice"),
			"password":            cty.StringVal("s3cr3t"),
			"password_wo_version": cty.NumberIntVal(1),
		})

		item, _ := TerraformResourceDataToMikrotik(res.Schema, d)
		if got := item["password"]; got != "s3cr3t" {
			t.Fatalf("expected password=%q, got %#v (item=%v)", "s3cr3t", got, item)
		}
		if _, ok := item["password-wo-version"]; ok {
			t.Fatalf("version attribute should never be serialized: item=%v", item)
		}
	})

	t.Run("create skips omitted optional write-only secret", func(t *testing.T) {
		d := resourceDataWithRawConfig(res, "", map[string]cty.Value{
			KeyName: cty.StringVal("alice"),
		})

		item, _ := TerraformResourceDataToMikrotik(res.Schema, d)
		if _, ok := item["password"]; ok {
			t.Fatalf("expected omitted write-only field to stay out of item, got %v", item)
		}
	})

	t.Run("update does not resend secret unless wo_version changes", func(t *testing.T) {
		state := &terraform.InstanceState{
			ID: "*1",
			Attributes: map[string]string{
				"id":                  "*1",
				"name":                "alice",
				"password_wo_version": "1",
				"limit_bytes_in":      "0",
				"limit_bytes_out":     "0",
				"profile":             "default",
			},
		}

		// Same password_wo_version, comment updated -> password must NOT be sent.
		rcSameVersion := testWriteOnlyResourceConfig(res, map[string]cty.Value{
			KeyName:               cty.StringVal("alice"),
			KeyComment:            cty.StringVal("updated comment"),
			"password":            cty.StringVal("s3cr3t"),
			"password_wo_version": cty.NumberIntVal(1),
		})
		diffSame, err := res.Diff(context.Background(), state, rcSameVersion, nil)
		if err != nil {
			t.Fatalf("diff (same version): %v", err)
		}
		diffSame.RawConfig = rcSameVersion.CtyValue
		dSame, err := schema.InternalMap(res.Schema).Data(state, diffSame)
		if err != nil {
			t.Fatalf("data (same version): %v", err)
		}
		itemSame, _ := TerraformResourceDataToMikrotik(res.Schema, dSame)
		if _, ok := itemSame["password"]; ok {
			t.Fatalf("expected password not to be sent when password_wo_version is unchanged, got item=%v", itemSame)
		}
		if itemSame["comment"] != "updated comment" {
			t.Fatalf("expected comment to be serialized, got item=%v", itemSame)
		}

		// Bumped password_wo_version -> new password MUST be sent.
		rcNewVersion := testWriteOnlyResourceConfig(res, map[string]cty.Value{
			KeyName:               cty.StringVal("alice"),
			"password":            cty.StringVal("rotated-secret"),
			"password_wo_version": cty.NumberIntVal(2),
		})
		diffRotated, err := res.Diff(context.Background(), state, rcNewVersion, nil)
		if err != nil {
			t.Fatalf("diff (rotated version): %v", err)
		}
		diffRotated.RawConfig = rcNewVersion.CtyValue
		dRotated, err := schema.InternalMap(res.Schema).Data(state, diffRotated)
		if err != nil {
			t.Fatalf("data (rotated version): %v", err)
		}
		itemRotated, _ := TerraformResourceDataToMikrotik(res.Schema, dRotated)
		if got := itemRotated["password"]; got != "rotated-secret" {
			t.Fatalf("expected rotated password=%q, got %#v (item=%v)", "rotated-secret", got, itemRotated)
		}
		if _, ok := itemRotated["password-wo-version"]; ok {
			t.Fatalf("version attribute should never be serialized: item=%v", itemRotated)
		}

		// Bumped password_wo_version with null password -> clears password.
		rcCleared := testWriteOnlyResourceConfig(res, map[string]cty.Value{
			KeyName:               cty.StringVal("alice"),
			"password_wo_version": cty.NumberIntVal(3),
		})
		diffCleared, err := res.Diff(context.Background(), state, rcCleared, nil)
		if err != nil {
			t.Fatalf("diff (cleared version): %v", err)
		}
		diffCleared.RawConfig = rcCleared.CtyValue
		dCleared, err := schema.InternalMap(res.Schema).Data(state, diffCleared)
		if err != nil {
			t.Fatalf("data (cleared version): %v", err)
		}
		itemCleared, _ := TerraformResourceDataToMikrotik(res.Schema, dCleared)
		if got, ok := itemCleared["password"]; !ok || got != "" {
			t.Fatalf("expected cleared password=\"\", got ok=%t val=%#v (item=%v)", ok, got, itemCleared)
		}
	})

	t.Run("read never writes write-only attributes to state", func(t *testing.T) {
		d := res.TestResourceData()
		item := MikrotikItem{
			".id":      "*1",
			"name":     "alice",
			"password": "router-returned-secret",
		}
		if diags := MikrotikResourceDataToTerraform(item, res.Schema, d); diags.HasError() {
			t.Fatalf("MikrotikResourceDataToTerraform error: %v", diags)
		}
		if got := d.Get("password"); got != "" {
			t.Fatalf("expected write-only password not to be written to state, got %q", got)
		}
		if got := d.Get("name"); got != "alice" {
			t.Fatalf("expected name=alice, got %q", got)
		}
	})

	t.Run("read does not preserve legacy plain password in state", func(t *testing.T) {
		legacyState := &terraform.InstanceState{
			ID: "*1",
			Attributes: map[string]string{
				"id":       "*1",
				"name":     "alice",
				"password": "legacy-plain-secret",
			},
		}
		d := res.Data(legacyState)
		if diags := MikrotikResourceDataToTerraform(MikrotikItem{
			".id":      "*1",
			"name":     "alice",
			"password": "legacy-plain-secret",
		}, res.Schema, d); diags.HasError() {
			t.Fatalf("MikrotikResourceDataToTerraform error: %v", diags)
		}

		// Simulate SDKv2 Apply/Read state normalization clearing write-only attrs.
		res.ReadContext = func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			return MikrotikResourceDataToTerraform(MikrotikItem{
				".id":      "*1",
				"name":     "alice",
				"password": "legacy-plain-secret",
			}, res.Schema, d)
		}
		newState, diags := res.RefreshWithoutUpgrade(context.Background(), legacyState, nil)
		if diags.HasError() {
			t.Fatalf("RefreshWithoutUpgrade error: %v", diags)
		}
		if val, ok := newState.Attributes["password"]; ok && val != "" {
			t.Fatalf("expected legacy plain password to be cleared from refreshed state, got %q", val)
		}
	})
}

func TestWriteOnlyNestedListSerializer(t *testing.T) {
	res := &schema.Resource{
		Schema: map[string]*schema.Schema{
			MetaResourcePath: PropResourcePath("/test/nested"),
			MetaId:           PropId(Id),
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
							WriteOnly: true,
						},
					},
				},
			},
			"block_wo_version": PropWriteOnlyVersion("block"),
		},
	}

	vals := map[string]cty.Value{}
	for attr, attrType := range res.CoreConfigSchema().ImpliedType().AttributeTypes() {
		vals[attr] = cty.NullVal(attrType)
	}
	vals["block"] = cty.ListVal([]cty.Value{cty.ObjectVal(map[string]cty.Value{
		"name":   cty.StringVal("x"),
		"secret": cty.StringVal("nested-secret"),
	})})

	d := res.Data(&terraform.InstanceState{
		ID: "",
		Attributes: map[string]string{
			"block.#":      "1",
			"block.0.name": "x",
		},
		RawConfig: cty.ObjectVal(vals),
	})

	item, _ := TerraformResourceDataToMikrotik(res.Schema, d)
	expected := MikrotikItem{
		"block.name":   "x",
		"block.secret": "nested-secret",
	}
	if !reflect.DeepEqual(item, expected) {
		t.Fatalf("expected %#v, got %#v", expected, item)
	}

	readData := res.TestResourceData()
	_ = readData.Set("block", []interface{}{map[string]interface{}{"name": "x"}})
	diags := MikrotikResourceDataToTerraform(MikrotikItem{
		".id":          "*1",
		"block.name":   "x",
		"block.secret": "nested-secret",
	}, res.Schema, readData)
	if diags.HasError() {
		t.Fatalf("read error: %v", diags)
	}
	block := readData.Get("block").([]interface{})[0].(map[string]interface{})
	if block["secret"] != "" {
		t.Fatalf("expected nested write-only secret not written to state, got %q", block["secret"])
	}
}
