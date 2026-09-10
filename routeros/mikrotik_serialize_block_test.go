package routeros

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

// These tests drive the real SDK Diff -> Apply path with a Create/UpdateContext that only
// serializes the resource data, so no RouterOS connection is needed. They guard the
// `case *schema.Resource` branch of TerraformResourceDataToMikrotik against an
// Optional+Computed block (`output` on the BGP resources) that Read materialized in the
// state while the configuration does not declare it: the raw config is then an empty
// list or null and must not be indexed.

func blockTestNullRawConfig(res *schema.Resource) (cty.Type, map[string]cty.Value) {
	ty := res.CoreConfigSchema().ImpliedType()
	attrs := map[string]cty.Value{}
	for name, aty := range ty.AttributeTypes() {
		attrs[name] = cty.NullVal(aty)
	}
	return ty, attrs
}

func blockTestApplyUpdate(t *testing.T, res *schema.Resource, state *terraform.InstanceState,
	cfg map[string]interface{}) (item MikrotikItem, recovered any) {
	t.Helper()

	res.UpdateContext = func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
		func() {
			defer func() { recovered = recover() }()
			item, _ = TerraformResourceDataToMikrotik(res.Schema, d)
		}()
		return nil
	}
	diff, err := res.Diff(context.Background(), state, terraform.NewResourceConfigRaw(cfg), nil)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if diff == nil {
		t.Fatalf("expected a diff")
	}
	if _, diags := res.Apply(context.Background(), state, diff, nil); diags.HasError() {
		t.Fatalf("apply: %v", diags)
	}
	return
}

func blockTestSetVersion(t *testing.T, version string) {
	t.Helper()
	previous := RouterOSVersion
	RouterOSVersion = version
	t.Cleanup(func() { RouterOSVersion = previous })
}

// The configuration declares no `output` block; the state carries the block a previous Read
// populated (RouterOS 7.24 always reports `output.add-path`); the user changes `comment`.
func Test_terraformResourceDataToMikrotik_ComputedBlockAbsentFromConfig(t *testing.T) {
	blockTestSetVersion(t, "7.24")

	for _, tc := range []struct {
		name string
		mk   func() *schema.Resource
	}{
		{"connection", ResourceRoutingBgpConnection},
		{"template", ResourceRoutingBgpTemplate},
	} {
		for _, nullOutput := range []bool{false, true} {
			res := tc.mk()
			ty, attrs := blockTestNullRawConfig(res)
			attrs["name"] = cty.StringVal("peer1")
			attrs["as"] = cty.StringVal("65000")
			attrs["comment"] = cty.StringVal("changed")
			if !nullOutput {
				// Terraform core sends an absent nested block as an empty list.
				attrs["output"] = cty.ListValEmpty(ty.AttributeType("output").ElementType())
			}
			state := &terraform.InstanceState{
				ID: "*1",
				Attributes: map[string]string{
					"id": "*1", "name": "peer1", "as": "65000", "comment": "old",
					"output.#": "1", "output.0.add_path": "ip",
				},
				RawConfig: cty.ObjectVal(attrs),
			}

			item, recovered := blockTestApplyUpdate(t, res, state, map[string]interface{}{
				"name": "peer1", "as": "65000", "comment": "changed",
			})
			if recovered != nil {
				t.Errorf("%s nullOutput=%v: TerraformResourceDataToMikrotik panicked: %v", tc.name, nullOutput, recovered)
				continue
			}
			if got := item["comment"]; got != "changed" {
				t.Errorf("%s nullOutput=%v: comment = %q, want %q", tc.name, nullOutput, got, "changed")
			}
			if _, ok := item["output.add-path"]; ok {
				t.Errorf("%s nullOutput=%v: router-owned output.add-path must not be sent back: %v", tc.name, nullOutput, item)
			}
		}
	}
}

// The configuration declares `output { filter_chain = "x" }` while the state also carries
// the router-reported `add_path`; the declared block must still be serialized.
func Test_terraformResourceDataToMikrotik_ComputedBlockDeclaredInConfig(t *testing.T) {
	blockTestSetVersion(t, "7.24")

	res := ResourceRoutingBgpConnection()
	ty, attrs := blockTestNullRawConfig(res)
	attrs["name"] = cty.StringVal("peer1")
	attrs["as"] = cty.StringVal("65000")
	attrs["comment"] = cty.StringVal("changed")
	outTy := ty.AttributeType("output").ElementType()
	outAttrs := map[string]cty.Value{}
	for name, aty := range outTy.AttributeTypes() {
		outAttrs[name] = cty.NullVal(aty)
	}
	outAttrs["filter_chain"] = cty.StringVal("x")
	attrs["output"] = cty.ListVal([]cty.Value{cty.ObjectVal(outAttrs)})
	state := &terraform.InstanceState{
		ID: "*1",
		Attributes: map[string]string{
			"id": "*1", "name": "peer1", "as": "65000", "comment": "old",
			"output.#": "1", "output.0.add_path": "ip", "output.0.filter_chain": "x",
		},
		RawConfig: cty.ObjectVal(attrs),
	}

	item, recovered := blockTestApplyUpdate(t, res, state, map[string]interface{}{
		"name": "peer1", "as": "65000", "comment": "changed",
		"output": []interface{}{map[string]interface{}{"filter_chain": "x"}},
	})
	if recovered != nil {
		t.Fatalf("TerraformResourceDataToMikrotik panicked: %v", recovered)
	}
	if got := item["output.filter-chain"]; got != "x" {
		t.Errorf("output.filter-chain = %q, want %q (item: %v)", got, "x", item)
	}
}

// The configuration drops a previously declared `input` block (Optional, not Computed). RouterOS
// keeps every property a `set` leaves out, so the properties the state carried must be unset,
// while the Computed `output` block present in the same state stays untouched. On the connection
// the `local` block is dropped too: its Optional properties are unset, the Required `role` is not.
func Test_terraformResourceDataToMikrotik_RemovedBlockIsUnset(t *testing.T) {
	blockTestSetVersion(t, "7.24")

	for _, tc := range []struct {
		name     string
		mk       func() *schema.Resource
		hasLocal bool
	}{
		{"connection", ResourceRoutingBgpConnection, true},
		{"template", ResourceRoutingBgpTemplate, false},
	} {
		for _, nullBlocks := range []bool{false, true} {
			res := tc.mk()
			ty, attrs := blockTestNullRawConfig(res)
			attrs["name"] = cty.StringVal("peer1")
			attrs["as"] = cty.StringVal("65000")
			stateAttrs := map[string]string{
				"id": "*1", "name": "peer1", "as": "65000",
				"input.#": "1", "input.0.filter": "in", "input.0.affinity": "alone",
				"input.0.allow_as": "2", "input.0.ignore_as_path_len": "true", "input.0.accept_nlri": "",
				"output.#": "1", "output.0.filter_chain": "out",
			}
			blocks := []string{"input", "output"}
			if tc.hasLocal {
				stateAttrs["local.#"] = "1"
				stateAttrs["local.0.address"] = "127.0.0.1"
				stateAttrs["local.0.role"] = "ebgp"
				blocks = append(blocks, "local")
			}
			if !nullBlocks {
				// Terraform core sends an absent nested block as an empty list.
				for _, block := range blocks {
					attrs[block] = cty.ListValEmpty(ty.AttributeType(block).ElementType())
				}
			}
			state := &terraform.InstanceState{ID: "*1", Attributes: stateAttrs, RawConfig: cty.ObjectVal(attrs)}

			item, recovered := blockTestApplyUpdate(t, res, state, map[string]interface{}{
				"name": "peer1", "as": "65000",
			})
			if recovered != nil {
				t.Errorf("%s nullBlocks=%v: TerraformResourceDataToMikrotik panicked: %v", tc.name, nullBlocks, recovered)
				continue
			}

			want := []string{"!input.filter", "!input.affinity", "!input.allow-as", "!input.ignore-as-path-len"}
			if tc.hasLocal {
				want = append(want, "!local.address")
			}
			for _, key := range want {
				if got, ok := item[key]; !ok || got != "" {
					t.Errorf("%s nullBlocks=%v: %s = %q (present: %v), want an unset (item: %v)", tc.name, nullBlocks, key, got, ok, item)
				}
			}
			for key := range item {
				switch {
				case key == "!input.accept-nlri":
					t.Errorf("%s nullBlocks=%v: accept_nlri was empty in the state, nothing to unset (item: %v)", tc.name, nullBlocks, item)
				case key == "!local.role":
					t.Errorf("%s nullBlocks=%v: the Required local.role must not be unset (item: %v)", tc.name, nullBlocks, item)
				case strings.HasPrefix(key, "input.") || strings.HasPrefix(key, "local."):
					t.Errorf("%s nullBlocks=%v: a removed block must not send values, got %s (item: %v)", tc.name, nullBlocks, key, item)
				case strings.HasPrefix(key, "output.") || strings.HasPrefix(key, "!output."):
					t.Errorf("%s nullBlocks=%v: the Computed output block must stay untouched, got %s (item: %v)", tc.name, nullBlocks, key, item)
				}
			}
		}
	}
}

// Create path: no state and no `output` block in the configuration.
func Test_terraformResourceDataToMikrotik_ComputedBlockAbsentOnCreate(t *testing.T) {
	blockTestSetVersion(t, "7.24")

	res := ResourceRoutingBgpConnection()
	ty, attrs := blockTestNullRawConfig(res)
	attrs["name"] = cty.StringVal("peer1")
	attrs["as"] = cty.StringVal("65000")
	attrs["output"] = cty.ListValEmpty(ty.AttributeType("output").ElementType())

	var item MikrotikItem
	var recovered any
	res.CreateContext = func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
		func() {
			defer func() { recovered = recover() }()
			item, _ = TerraformResourceDataToMikrotik(res.Schema, d)
		}()
		d.SetId("*1")
		return nil
	}
	diff, err := res.Diff(context.Background(), nil, terraform.NewResourceConfigRaw(map[string]interface{}{
		"name": "peer1", "as": "65000",
	}), nil)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	diff.RawConfig = cty.ObjectVal(attrs)
	if _, diags := res.Apply(context.Background(), nil, diff, nil); diags.HasError() {
		t.Fatalf("apply: %v", diags)
	}
	if recovered != nil {
		t.Fatalf("TerraformResourceDataToMikrotik panicked: %v", recovered)
	}
	if got := item["name"]; got != "peer1" {
		t.Errorf("name = %q, want %q (item: %v)", got, "peer1", item)
	}
}
