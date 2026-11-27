package routeros

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	testingterraform "github.com/hashicorp/terraform-plugin-testing/terraform"
)

type mockMoveOp struct {
	path        string
	numbers     string
	destination string
}

type mockMoveTableClient struct {
	transport TransportType
	items     map[string][]MikrotikItem
	reads     []string
	moves     []mockMoveOp
	moveErr   error
	partialFn func(items []MikrotikItem) []MikrotikItem
}

func (c *mockMoveTableClient) GetExtraParams() *ExtraParams { return &ExtraParams{} }
func (c *mockMoveTableClient) GetTransport() TransportType  { return c.transport }

func (c *mockMoveTableClient) SendRequest(method crudMethod, u *URL, item MikrotikItem, result interface{}) error {
	switch method {
	case crudRead:
		c.reads = append(c.reads, u.Path)
		var out []MikrotikItem
		for _, r := range c.items[u.Path] {
			out = append(out, maps.Clone(r))
		}
		*result.(*[]MikrotikItem) = out
		return nil

	case crudMove:
		c.moves = append(c.moves, mockMoveOp{
			path:        u.Path,
			numbers:     item["numbers"],
			destination: item["destination"],
		})
		if c.moveErr != nil {
			return c.moveErr
		}
		tablePath := u.Path
		if c.transport == TransportREST {
			tablePath = strings.TrimSuffix(tablePath, "/move")
		}
		rows := c.items[tablePath]
		if c.partialFn != nil {
			c.items[tablePath] = c.partialFn(rows)
			return nil
		}
		if err := applyMockMove(&rows, item["numbers"], item["destination"]); err != nil {
			return err
		}
		c.items[tablePath] = rows
		return nil

	default:
		return fmt.Errorf("unexpected method %v on %s", method, u.Path)
	}
}

func TestMoveItemsSchemaAndResourcePath(t *testing.T) {
	res := ResourceMoveItems()

	seq := res.Schema["sequence"]
	if seq == nil || seq.MinItems != 1 {
		t.Fatalf("expected sequence.MinItems = 1, got %+v", seq)
	}
	if elem, ok := seq.Elem.(*schema.Schema); !ok || elem.MinItems != 0 {
		t.Errorf("expected sequence Elem.MinItems = 0, got %+v", seq.Elem)
	}

	anchor := res.Schema["anchor_rule"]
	if anchor == nil || anchor.Type != schema.TypeString || !anchor.Optional {
		t.Errorf("unexpected anchor_rule schema: %+v", anchor)
	}

	for _, tc := range []struct {
		name string
		cfg  map[string]interface{}
		want string
	}{
		{
			name: "resource_path",
			cfg:  map[string]interface{}{"resource_path": "/ip/firewall/filter", "sequence": []interface{}{"*1", "*2"}},
			want: "/ip/firewall/filter",
		},
		{
			name: "resource_name_leading_slash_fix",
			cfg:  map[string]interface{}{"resource_name": "routeros_ip_firewall_filter", "sequence": []interface{}{"*1", "*2"}},
			want: "/ip/firewall/filter",
		},
		{
			name: "resource_name_ipv6_mangle",
			cfg:  map[string]interface{}{"resource_name": "routeros_ipv6_firewall_mangle", "sequence": []interface{}{"*1", "*2"}},
			want: "/ipv6/firewall/mangle",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := schema.TestResourceDataRaw(t, res.Schema, tc.cfg)
			if got := moveItemsResourcePath(d); got != tc.want {
				t.Errorf("moveItemsResourcePath() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMoveItemsUnit(t *testing.T) {
	ctx := context.Background()
	res := ResourceMoveItems()

	for _, transport := range []TransportType{TransportAPI, TransportREST} {
		tName := "REST"
		wantMovePath := "/ip/firewall/filter/move"
		if transport == TransportAPI {
			tName = "API"
			wantMovePath = "/ip/firewall/filter"
		}

		t.Run(tName+"/classic_requires_two_items_without_anchor", func(t *testing.T) {
			client := &mockMoveTableClient{
				transport: transport,
				items: map[string][]MikrotikItem{
					"/ip/firewall/filter": {{".id": "*1"}},
				},
			}
			d := schema.TestResourceDataRaw(t, res.Schema, map[string]interface{}{
				"resource_name": "routeros_ip_firewall_filter",
				"sequence":      []interface{}{"*1"},
			})
			diags := res.CreateContext(ctx, d, client)
			assertDiagHasError(t, diags, "sequence must contain at least 2 items when anchor_rule is not set")
			if len(client.moves) != 0 {
				t.Errorf("expected 0 move calls, got %d", len(client.moves))
			}
		})

		t.Run(tName+"/classic_resource_name_create_read_inorder_with_unrelated_and_dynamic", func(t *testing.T) {
			client := &mockMoveTableClient{
				transport: transport,
				items: map[string][]MikrotikItem{
					"/ip/firewall/filter": {
						{".id": "*3"},
						{".id": "*9", "dynamic": "true"},
						{".id": "*2"},
						{".id": "*8"},
						{".id": "*1"},
					},
				},
			}
			cfg := map[string]interface{}{
				"resource_name": "routeros_ip_firewall_filter",
				"sequence":      []interface{}{"*1", "*2", "*3"},
			}
			diff, err := res.Diff(ctx, nil, terraform.NewResourceConfigRaw(cfg), client)
			if err != nil {
				t.Fatalf("diff: %v", err)
			}
			state, diags := res.Apply(ctx, nil, diff, client)
			if diags.HasError() {
				t.Fatalf("apply error: %v", diags)
			}
			if state.ID != "ip.firewall.filter" {
				t.Errorf("id = %q, want ip.firewall.filter", state.ID)
			}
			if len(client.moves) != 1 {
				t.Fatalf("expected 1 move, got %d", len(client.moves))
			}
			if client.moves[0].path != wantMovePath || client.moves[0].numbers != "*1,*2" || client.moves[0].destination != "*3" {
				t.Errorf("unexpected move: %+v", client.moves[0])
			}
			if !slices.Equal(client.reads, []string{"/ip/firewall/filter", "/ip/firewall/filter", "/ip/firewall/filter"}) {
				t.Errorf("unexpected read paths (expected leading /): %v", client.reads)
			}

			// Re-read should report no drift even with unrelated & dynamic items present.
			refreshed, diags := res.RefreshWithoutUpgrade(ctx, state, client)
			if diags.HasError() {
				t.Fatalf("refresh error: %v", diags)
			}
			afterDiff, err := res.Diff(ctx, refreshed, terraform.NewResourceConfigRaw(cfg), client)
			if err != nil {
				t.Fatalf("diff after refresh: %v", err)
			}
			if afterDiff != nil && !afterDiff.Empty() {
				t.Errorf("expected empty diff, got %+v", afterDiff.Attributes)
			}
		})

		t.Run(tName+"/classic_drift_and_missing_id", func(t *testing.T) {
			client := &mockMoveTableClient{
				transport: transport,
				items: map[string][]MikrotikItem{
					"/ip/firewall/filter": {
						{".id": "*2"},
						{".id": "*1"},
						{".id": "*3"},
					},
				},
			}
			cfg := map[string]interface{}{
				"resource_path": "/ip/firewall/filter",
				"sequence":      []interface{}{"*1", "*2", "*3"},
			}
			state := &terraform.InstanceState{
				ID: "ip.firewall.filter",
				Attributes: map[string]string{
					"id":            "ip.firewall.filter",
					"resource_path": "/ip/firewall/filter",
					"sequence.#":    "3",
					"sequence.0":    "*1",
					"sequence.1":    "*2",
					"sequence.2":    "*3",
				},
			}
			refreshed, diags := res.RefreshWithoutUpgrade(ctx, state, client)
			if diags.HasError() {
				t.Fatalf("refresh error: %v", diags)
			}
			if refreshed.Attributes["sequence.0"] != "*2" || refreshed.Attributes["sequence.1"] != "*1" {
				t.Fatalf("expected live order [*2 *1 *3], got %v", refreshed.Attributes)
			}
			diff, err := res.Diff(ctx, refreshed, terraform.NewResourceConfigRaw(cfg), client)
			if err != nil {
				t.Fatalf("diff: %v", err)
			}
			if diff == nil || diff.Empty() {
				t.Fatal("expected drift diff on sequence")
			}
			applied, diags := res.Apply(ctx, refreshed, diff, client)
			if diags.HasError() {
				t.Fatalf("apply error: %v", diags)
			}
			if applied.Attributes["sequence.0"] != "*1" || applied.Attributes["sequence.1"] != "*2" || applied.Attributes["sequence.2"] != "*3" {
				t.Errorf("expected reconciled sequence [*1 *2 *3], got %v", applied.Attributes)
			}

			// Missing ID on Read omits it from sequence so Terraform detects drift.
			client.items["/ip/firewall/filter"] = []MikrotikItem{{".id": "*1"}, {".id": "*3"}}
			refreshedMissing, diags := res.RefreshWithoutUpgrade(ctx, applied, client)
			if diags.HasError() {
				t.Fatalf("refresh error: %v", diags)
			}
			if refreshedMissing.Attributes["sequence.#"] != "2" {
				t.Errorf("expected sequence.# = 2 when *2 missing, got %q", refreshedMissing.Attributes["sequence.#"])
			}

			// Update targeting missing *2 fails clearly and refreshes state.
			diffMissing, err := res.Diff(ctx, refreshedMissing, terraform.NewResourceConfigRaw(cfg), client)
			if err != nil {
				t.Fatalf("diff: %v", err)
			}
			_, diags = res.Apply(ctx, refreshedMissing, diffMissing, client)
			assertDiagHasError(t, diags, `sequence item "*2" not found`)
		})

		t.Run(tName+"/classic_partial_move_failure", func(t *testing.T) {
			for _, tc := range []struct {
				name      string
				moveErr   error
				partialFn func([]MikrotikItem) []MikrotikItem
			}{
				{
					name:    "move_error",
					moveErr: errors.New("move failed"),
				},
				{
					name: "move_partial_noop",
					partialFn: func(items []MikrotikItem) []MikrotikItem {
						// *1 moved, *2 still after *3.
						return []MikrotikItem{{".id": "*1"}, {".id": "*3"}, {".id": "*2"}}
					},
				},
			} {
				t.Run(tc.name, func(t *testing.T) {
					client := &mockMoveTableClient{
						transport: transport,
						items: map[string][]MikrotikItem{
							"/ip/firewall/filter": {{".id": "*3"}, {".id": "*2"}, {".id": "*1"}},
						},
						moveErr:   tc.moveErr,
						partialFn: tc.partialFn,
					}
					d := schema.TestResourceDataRaw(t, res.Schema, map[string]interface{}{
						"resource_path": "/ip/firewall/filter",
						"sequence":      []interface{}{"*1", "*2", "*3"},
					})
					diags := res.CreateContext(ctx, d, client)
					if !diags.HasError() {
						t.Fatal("expected error diagnostic on partial move failure")
					}
					if d.Id() != "" {
						t.Errorf("expected empty ID on failed Create, got %q", d.Id())
					}
				})
			}
		})

		t.Run(tName+"/anchor_rule_single_item_create_drift_reconcile", func(t *testing.T) {
			client := &mockMoveTableClient{
				transport: transport,
				items: map[string][]MikrotikItem{
					"/ip/firewall/filter": {
						{".id": "*99", "dynamic": "true"},
						{".id": "*3"},
						{".id": "*1"},
					},
				},
			}
			cfg := map[string]interface{}{
				"resource_name": "routeros_ip_firewall_filter",
				"sequence":      []interface{}{"*1"},
				"anchor_rule":   "*3",
			}
			diff, err := res.Diff(ctx, nil, terraform.NewResourceConfigRaw(cfg), client)
			if err != nil {
				t.Fatalf("diff: %v", err)
			}
			state, diags := res.Apply(ctx, nil, diff, client)
			if diags.HasError() {
				t.Fatalf("apply error: %v", diags)
			}
			if state.ID != "ip.firewall.filter" {
				t.Errorf("id = %q, want ip.firewall.filter", state.ID)
			}
			if len(client.moves) != 1 || client.moves[0].path != wantMovePath ||
				client.moves[0].numbers != "*1" || client.moves[0].destination != "*3" {
				t.Errorf("unexpected move: %+v", client.moves)
			}
			if state.Attributes["anchor_rule"] != "*3" {
				t.Errorf("anchor_rule = %q, want *3", state.Attributes["anchor_rule"])
			}

			// Out-of-band move puts *1 after *3; Refresh clears anchor_rule so TF detects drift.
			client.items["/ip/firewall/filter"] = []MikrotikItem{
				{".id": "*3"},
				{".id": "*99", "dynamic": "true"},
				{".id": "*1"},
			}
			refreshed, diags := res.RefreshWithoutUpgrade(ctx, state, client)
			if diags.HasError() {
				t.Fatalf("refresh error: %v", diags)
			}
			if refreshed.Attributes["anchor_rule"] != "" {
				t.Fatalf("expected anchor_rule cleared when single-item sequence moved after anchor, got %q", refreshed.Attributes["anchor_rule"])
			}
			driftDiff, err := res.Diff(ctx, refreshed, terraform.NewResourceConfigRaw(cfg), client)
			if err != nil {
				t.Fatalf("diff: %v", err)
			}
			if driftDiff == nil || driftDiff.Attributes["anchor_rule"] == nil {
				t.Fatalf("expected diff on anchor_rule, got %+v", driftDiff)
			}
			reconciled, diags := res.Apply(ctx, refreshed, driftDiff, client)
			if diags.HasError() {
				t.Fatalf("apply error: %v", diags)
			}
			if reconciled.Attributes["anchor_rule"] != "*3" {
				t.Errorf("anchor_rule after reconcile = %q, want *3", reconciled.Attributes["anchor_rule"])
			}
			if !moveItemsInOrder(client.items["/ip/firewall/filter"], []string{"*1"}, "*3") {
				t.Errorf("expected *1 before *3, got %v", client.items["/ip/firewall/filter"])
			}
		})

		t.Run(tName+"/anchor_rule_multi_item_and_errors", func(t *testing.T) {
			client := &mockMoveTableClient{
				transport: transport,
				items: map[string][]MikrotikItem{
					"/ip/firewall/filter": {
						{".id": "*4"},
						{".id": "*2"},
						{".id": "*10", "dynamic": "true"},
						{".id": "*1"},
					},
				},
			}
			cfg := map[string]interface{}{
				"resource_path": "/ip/firewall/filter",
				"sequence":      []interface{}{"*1", "*2"},
				"anchor_rule":   "*4",
			}
			diff, err := res.Diff(ctx, nil, terraform.NewResourceConfigRaw(cfg), client)
			if err != nil {
				t.Fatalf("diff: %v", err)
			}
			state, diags := res.Apply(ctx, nil, diff, client)
			if diags.HasError() {
				t.Fatalf("apply error: %v", diags)
			}
			if client.moves[0].numbers != "*1,*2" || client.moves[0].destination != "*4" {
				t.Errorf("unexpected move: %+v", client.moves[0])
			}
			if !moveItemsInOrder(client.items["/ip/firewall/filter"], []string{"*1", "*2"}, "*4") {
				t.Errorf("unexpected table order: %v", client.items["/ip/firewall/filter"])
			}

			// Disappeared anchor_rule clears anchor_rule on Read and errors on Update.
			client.items["/ip/firewall/filter"] = []MikrotikItem{{".id": "*1"}, {".id": "*2"}}
			refreshed, diags := res.RefreshWithoutUpgrade(ctx, state, client)
			if diags.HasError() {
				t.Fatalf("refresh error: %v", diags)
			}
			if refreshed.Attributes["anchor_rule"] != "" {
				t.Errorf("expected anchor_rule cleared when anchor missing, got %q", refreshed.Attributes["anchor_rule"])
			}
			diffMissing, err := res.Diff(ctx, refreshed, terraform.NewResourceConfigRaw(cfg), client)
			if err != nil {
				t.Fatalf("diff: %v", err)
			}
			_, diags = res.Apply(ctx, refreshed, diffMissing, client)
			assertDiagHasError(t, diags, `anchor_rule "*4" not found`)

			// Partial move failure in anchor_rule mode.
			partialClient := &mockMoveTableClient{
				transport: transport,
				items: map[string][]MikrotikItem{
					"/ip/firewall/filter": {{".id": "*4"}, {".id": "*2"}, {".id": "*1"}},
				},
				partialFn: func(items []MikrotikItem) []MikrotikItem {
					// *1 before *4, but *2 stayed after *4.
					return []MikrotikItem{{".id": "*1"}, {".id": "*4"}, {".id": "*2"}}
				},
			}
			d := schema.TestResourceDataRaw(t, res.Schema, cfg)
			diags = res.CreateContext(ctx, d, partialClient)
			assertDiagHasError(t, diags, "was not ordered before anchor_rule")
		})
	}
}

func TestAccMoveItems_anchorRule(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance test: set TF_ACC, ROS_HOSTURL and ROS_USERNAME to run it")
	}

	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			prefix := "tf-acc-move-anchor-" + strings.ToLower(name) + "-"
			var r1ID, r2ID, anchorID string

			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
					configureTestAccClient(t)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/ip/firewall/filter", "routeros_ip_firewall_filter"),
				Steps: []resource.TestStep{
					{
						Config: testAccMoveItemsAnchorConfig(prefix),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId("routeros_move_items.ordered"),
							resource.TestCheckResourceAttrPair("routeros_move_items.ordered", "anchor_rule", "routeros_ip_firewall_filter.anchor", "id"),
							func(s *testingterraform.State) error {
								r1ID = s.RootModule().Resources["routeros_ip_firewall_filter.r1"].Primary.ID
								r2ID = s.RootModule().Resources["routeros_ip_firewall_filter.r2"].Primary.ID
								anchorID = s.RootModule().Resources["routeros_ip_firewall_filter.anchor"].Primary.ID
								client := testAccProvider.Meta().(Client)
								if err := checkFirewallRulesOrdered(client, r1ID, r2ID); err != nil {
									return err
								}
								return checkFirewallRulesOrdered(client, r2ID, anchorID)
							},
						),
					},
					{
						PreConfig: func() {
							client := testAccProvider.Meta().(Client)
							movePath := "/ip/firewall/filter"
							if client.GetTransport() == TransportREST {
								movePath += "/move"
							}
							// Move anchor before r1 out-of-band so sequence drifts after anchor.
							if err := client.SendRequest(crudMove, &URL{Path: movePath}, MikrotikItem{
								"numbers":     anchorID,
								"destination": r1ID,
							}, nil); err != nil {
								t.Fatalf("out-of-band move failed: %v", err)
							}
						},
						Config: testAccMoveItemsAnchorConfig(prefix),
						Check: resource.ComposeTestCheckFunc(
							func(s *testingterraform.State) error {
								client := testAccProvider.Meta().(Client)
								if err := checkFirewallRulesOrdered(client, r1ID, r2ID); err != nil {
									return err
								}
								return checkFirewallRulesOrdered(client, r2ID, anchorID)
							},
						),
					},
				},
			})
		})
	}
}

func testAccMoveItemsAnchorConfig(prefix string) string {
	return providerConfig + fmt.Sprintf(`
resource "routeros_ip_firewall_filter" "r1" {
  chain    = "forward"
  action   = "accept"
  disabled = true
  comment  = %q
}

resource "routeros_ip_firewall_filter" "r2" {
  chain    = "forward"
  action   = "accept"
  disabled = true
  comment  = %q
}

resource "routeros_ip_firewall_filter" "anchor" {
  chain    = "forward"
  action   = "drop"
  disabled = true
  comment  = %q
}

resource "routeros_move_items" "ordered" {
  resource_name = "routeros_ip_firewall_filter"
  sequence = [
    routeros_ip_firewall_filter.r1.id,
    routeros_ip_firewall_filter.r2.id,
  ]
  anchor_rule = routeros_ip_firewall_filter.anchor.id
}
`, prefix+"r1", prefix+"r2", prefix+"anchor")
}
