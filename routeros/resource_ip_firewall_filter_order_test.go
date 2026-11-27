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

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	testingterraform "github.com/hashicorp/terraform-plugin-testing/terraform"
)

type mockFirewallReadCall struct {
	path  string
	query []string
}

type mockFirewallMoveCall struct {
	path        string
	numbers     string
	destination string
}

type mockFirewallTableClient struct {
	transport TransportType
	rules     []MikrotikItem
	nextID    int

	reads   []mockFirewallReadCall
	creates []MikrotikItem
	updates []MikrotikItem
	moves   []mockFirewallMoveCall

	moveErr          error
	partialMoveNoOp  bool
	failAfterMoveGet bool
}

func (c *mockFirewallTableClient) GetExtraParams() *ExtraParams { return &ExtraParams{} }
func (c *mockFirewallTableClient) GetTransport() TransportType  { return c.transport }

func (c *mockFirewallTableClient) SendRequest(method crudMethod, u *URL, item MikrotikItem, result interface{}) error {
	switch method {
	case crudRead:
		c.reads = append(c.reads, mockFirewallReadCall{
			path:  u.Path,
			query: slices.Clone(u.Query),
		})
		if c.failAfterMoveGet && len(c.moves) > 0 && len(u.Query) == 0 {
			return errors.New("read failed after move")
		}
		var out []MikrotikItem
		if len(u.Query) == 0 {
			for _, r := range c.rules {
				out = append(out, maps.Clone(r))
			}
		} else {
			for _, r := range c.rules {
				if mockMatchesIDQuery(r, u.Query) {
					out = append(out, maps.Clone(r))
				}
			}
		}
		*result.(*[]MikrotikItem) = out
		return nil

	case crudCreate:
		c.creates = append(c.creates, maps.Clone(item))
		c.nextID++
		id := fmt.Sprintf("*%d", c.nextID)
		created := maps.Clone(item)
		delete(created, "place-before")
		created[".id"] = id
		if anchor := item["place-before"]; anchor != "" {
			idx := slices.IndexFunc(c.rules, func(r MikrotikItem) bool { return r[".id"] == anchor })
			if idx >= 0 {
				c.rules = slices.Insert(c.rules, idx, created)
			} else {
				c.rules = append(c.rules, created)
			}
		} else {
			c.rules = append(c.rules, created)
		}
		*result.(*MikrotikItem) = maps.Clone(created)
		return nil

	case crudUpdate:
		c.updates = append(c.updates, maps.Clone(item))
		var targetID string
		if c.transport == TransportREST {
			const prefix = "/ip/firewall/filter/"
			targetID = strings.TrimPrefix(u.Path, prefix)
		} else {
			targetID = item[".id"]
		}
		for i, r := range c.rules {
			if r[".id"] == targetID {
				updated := maps.Clone(r)
				for k, v := range item {
					if k == ".id" {
						continue
					}
					if strings.HasPrefix(k, "!") {
						delete(updated, strings.TrimPrefix(k, "!"))
						continue
					}
					updated[k] = v
				}
				c.rules[i] = updated
				*result.(*MikrotikItem) = maps.Clone(updated)
				return nil
			}
		}
		return fmt.Errorf("rule %q not found", targetID)

	case crudMove:
		c.moves = append(c.moves, mockFirewallMoveCall{
			path:        u.Path,
			numbers:     item["numbers"],
			destination: item["destination"],
		})
		if c.moveErr != nil {
			return c.moveErr
		}
		if c.partialMoveNoOp {
			return nil
		}
		return applyMockMove(&c.rules, item["numbers"], item["destination"])

	default:
		return fmt.Errorf("unexpected method %v", method)
	}
}

func mockMatchesIDQuery(r MikrotikItem, query []string) bool {
	for _, q := range query {
		if strings.HasPrefix(q, "?.id=") {
			return r[".id"] == strings.TrimPrefix(q, "?.id=")
		}
		if strings.HasPrefix(q, ".id=") {
			return r[".id"] == strings.TrimPrefix(q, ".id=")
		}
	}
	return true
}

func applyMockMove(rules *[]MikrotikItem, numbers, destination string) error {
	ids := strings.Split(numbers, ",")
	moveSet := make(map[string]struct{}, len(ids))
	byID := make(map[string]MikrotikItem, len(*rules))
	for _, r := range *rules {
		byID[r[".id"]] = r
	}
	var moved []MikrotikItem
	for _, id := range ids {
		r, ok := byID[id]
		if !ok {
			return fmt.Errorf("item %q not found", id)
		}
		moveSet[id] = struct{}{}
		moved = append(moved, r)
	}
	if _, ok := byID[destination]; !ok {
		return fmt.Errorf("destination %q not found", destination)
	}
	var remaining []MikrotikItem
	for _, r := range *rules {
		if _, ok := moveSet[r[".id"]]; ok {
			continue
		}
		if r[".id"] == destination {
			remaining = append(remaining, moved...)
		}
		remaining = append(remaining, r)
	}
	*rules = remaining
	return nil
}

func TestIPFirewallFilterOrderSchema(t *testing.T) {
	res := ResourceIPFirewallFilter()

	placeBefore, ok := res.Schema[KeyPlaceBefore]
	if !ok {
		t.Fatalf("missing %q in schema", KeyPlaceBefore)
	}
	if placeBefore.ForceNew {
		t.Errorf("ResourceIPFirewallFilter().Schema[%q].ForceNew = true, want false", KeyPlaceBefore)
	}
	if !PropPlaceBefore.ForceNew {
		t.Errorf("shared PropPlaceBefore.ForceNew was mutated to false, want true")
	}

	enforce, ok := res.Schema["enforce_order"]
	if !ok {
		t.Fatalf("missing enforce_order in schema")
	}
	if enforce.Type != schema.TypeBool || !enforce.Optional || enforce.Default != false {
		t.Errorf("unexpected enforce_order schema: %+v", enforce)
	}
}

func TestIPFirewallFilterOrderUnit(t *testing.T) {
	ctx := context.Background()
	res := ResourceIPFirewallFilter()

	for _, transport := range []TransportType{TransportAPI, TransportREST} {
		tName := "REST"
		wantMovePath := "/ip/firewall/filter/move"
		if transport == TransportAPI {
			tName = "API"
			wantMovePath = "/ip/firewall/filter"
		}

		t.Run(tName+"/create_enforce_requires_place_before", func(t *testing.T) {
			client := &mockFirewallTableClient{transport: transport}
			cfg := map[string]interface{}{
				"chain":         "forward",
				"action":        "accept",
				"enforce_order": true,
			}
			diff, err := res.Diff(ctx, nil, terraform.NewResourceConfigRaw(cfg), client)
			if err != nil {
				t.Fatalf("diff: %v", err)
			}
			_, diags := res.Apply(ctx, nil, diff, client)
			assertDiagHasError(t, diags, "enforce_order requires place_before")
			if len(client.creates) != 0 {
				t.Errorf("expected no create requests, got %d", len(client.creates))
			}
		})

		t.Run(tName+"/create_enforced_skips_enforce_order_and_confirms_order", func(t *testing.T) {
			client := &mockFirewallTableClient{
				transport: transport,
				nextID:    10,
				rules: []MikrotikItem{
					{".id": "*99", "chain": "forward", "action": "drop"},
				},
			}
			cfg := map[string]interface{}{
				"chain":         "forward",
				"action":        "accept",
				"place_before":  "*99",
				"enforce_order": true,
			}
			diff, err := res.Diff(ctx, nil, terraform.NewResourceConfigRaw(cfg), client)
			if err != nil {
				t.Fatalf("diff: %v", err)
			}
			state, diags := res.Apply(ctx, nil, diff, client)
			if diags.HasError() {
				t.Fatalf("unexpected apply error: %v", diags)
			}
			if len(client.creates) != 1 {
				t.Fatalf("expected 1 create, got %d", len(client.creates))
			}
			if _, ok := client.creates[0]["enforce-order"]; ok {
				t.Errorf("enforce-order leaked into create payload: %v", client.creates[0])
			}
			if got := client.creates[0]["place-before"]; got != "*99" {
				t.Errorf("create place-before = %q, want *99", got)
			}
			if state.Attributes["place_before"] != "*99" {
				t.Errorf("state place_before = %q, want *99", state.Attributes["place_before"])
			}
		})

		t.Run(tName+"/read_enforce_false_zero_extra_calls", func(t *testing.T) {
			// Even though *1 is after *99 on router, enforce_order=false skips table read.
			client := &mockFirewallTableClient{
				transport: transport,
				rules: []MikrotikItem{
					{".id": "*99", "chain": "forward", "action": "drop"},
					{".id": "*1", "chain": "forward", "action": "accept"},
				},
			}
			state := &terraform.InstanceState{
				ID: "*1",
				Attributes: map[string]string{
					"id":            "*1",
					"chain":         "forward",
					"action":        "accept",
					"place_before":  "*99",
					"enforce_order": "false",
					"log":           "false",
				},
			}
			refreshed, diags := res.RefreshWithoutUpgrade(ctx, state, client)
			if diags.HasError() {
				t.Fatalf("refresh error: %v", diags)
			}
			if len(client.reads) != 1 || len(client.reads[0].query) == 0 {
				t.Errorf("expected exactly 1 filtered ID read, got %+v", client.reads)
			}
			if len(client.moves) != 0 {
				t.Errorf("expected 0 moves, got %d", len(client.moves))
			}
			if refreshed.Attributes["place_before"] != "*99" {
				t.Errorf("place_before = %q, want *99 unchanged", refreshed.Attributes["place_before"])
			}
		})

		t.Run(tName+"/read_in_order_with_unrelated_and_dynamic_rules", func(t *testing.T) {
			client := &mockFirewallTableClient{
				transport: transport,
				rules: []MikrotikItem{
					{".id": "*0", "chain": "input", "action": "accept", "dynamic": "true"},
					{".id": "*1", "chain": "forward", "action": "accept"},
					{".id": "*2", "chain": "forward", "action": "passthrough", "dynamic": "true"},
					{".id": "*3", "chain": "forward", "action": "log"},
					{".id": "*99", "chain": "forward", "action": "drop"},
				},
			}
			cfg := map[string]interface{}{
				"chain":         "forward",
				"action":        "accept",
				"place_before":  "*99",
				"enforce_order": true,
			}
			state := &terraform.InstanceState{
				ID: "*1",
				Attributes: map[string]string{
					"id":            "*1",
					"chain":         "forward",
					"action":        "accept",
					"place_before":  "*99",
					"enforce_order": "true",
					"log":           "false",
				},
			}
			refreshed, diags := res.RefreshWithoutUpgrade(ctx, state, client)
			if diags.HasError() {
				t.Fatalf("refresh error: %v", diags)
			}
			if refreshed.Attributes["place_before"] != "*99" {
				t.Errorf("place_before = %q, want *99", refreshed.Attributes["place_before"])
			}
			diff, err := res.Diff(ctx, refreshed, terraform.NewResourceConfigRaw(cfg), client)
			if err != nil {
				t.Fatalf("diff error: %v", err)
			}
			if diff != nil && !diff.Empty() {
				t.Errorf("expected empty diff when in order, got %+v", diff.Attributes)
			}
			if len(client.moves) != 0 {
				t.Errorf("expected 0 moves, got %d", len(client.moves))
			}
		})

		t.Run(tName+"/drift_out_of_band_move_and_inplace_reconcile", func(t *testing.T) {
			client := &mockFirewallTableClient{
				transport: transport,
				rules: []MikrotikItem{
					{".id": "*99", "chain": "forward", "action": "drop"},
					{".id": "*2", "chain": "forward", "action": "passthrough", "dynamic": "true"},
					{".id": "*1", "chain": "forward", "action": "accept", "comment": "keep-me"},
				},
			}
			cfg := map[string]interface{}{
				"chain":         "forward",
				"action":        "accept",
				"comment":       "keep-me",
				"place_before":  "*99",
				"enforce_order": true,
			}
			state := &terraform.InstanceState{
				ID: "*1",
				Attributes: map[string]string{
					"id":            "*1",
					"chain":         "forward",
					"action":        "accept",
					"comment":       "keep-me",
					"place_before":  "*99",
					"enforce_order": "true",
					"log":           "false",
				},
			}

			refreshed, diags := res.RefreshWithoutUpgrade(ctx, state, client)
			if diags.HasError() {
				t.Fatalf("refresh error: %v", diags)
			}
			if refreshed.Attributes["place_before"] != "" {
				t.Fatalf("expected place_before cleared on drift, got %q", refreshed.Attributes["place_before"])
			}

			diff, err := res.Diff(ctx, refreshed, terraform.NewResourceConfigRaw(cfg), client)
			if err != nil {
				t.Fatalf("diff error: %v", err)
			}
			if diff == nil || diff.Attributes["place_before"] == nil {
				t.Fatalf("expected diff on place_before, got %+v", diff)
			}
			if diff.RequiresNew() {
				t.Fatalf("expected in-place update, got RequiresNew on diff: %+v", diff.Attributes)
			}

			applied, diags := res.Apply(ctx, refreshed, diff, client)
			if diags.HasError() {
				t.Fatalf("apply error: %v", diags)
			}
			if applied.ID != "*1" {
				t.Errorf("expected rule ID *1 preserved, got %q", applied.ID)
			}
			if len(client.updates) != 0 {
				t.Errorf("expected 0 crudUpdate calls on order-only reconciliation, got %v", client.updates)
			}
			if len(client.moves) != 1 {
				t.Fatalf("expected 1 move call, got %d", len(client.moves))
			}
			if client.moves[0].path != wantMovePath || client.moves[0].numbers != "*1" || client.moves[0].destination != "*99" {
				t.Errorf("unexpected move call: %+v", client.moves[0])
			}
			if applied.Attributes["place_before"] != "*99" {
				t.Errorf("place_before after apply = %q, want *99", applied.Attributes["place_before"])
			}
			rIdx, aIdx := findFirewallRuleAndAnchorIndex(client.rules, "*1", "*99")
			if rIdx < 0 || aIdx < 0 || rIdx >= aIdx {
				t.Errorf("expected *1 before *99 after apply, got ruleIdx=%d anchorIdx=%d", rIdx, aIdx)
			}
		})

		t.Run(tName+"/mixed_attribute_and_order_update_skips_order_fields_in_patch", func(t *testing.T) {
			client := &mockFirewallTableClient{
				transport: transport,
				rules: []MikrotikItem{
					{".id": "*99", "chain": "forward", "action": "drop"},
					{".id": "*1", "chain": "forward", "action": "accept", "comment": "old"},
				},
			}
			cfg := map[string]interface{}{
				"chain":         "forward",
				"action":        "accept",
				"comment":       "new",
				"place_before":  "*99",
				"enforce_order": true,
			}
			state := &terraform.InstanceState{
				ID: "*1",
				Attributes: map[string]string{
					"id":            "*1",
					"chain":         "forward",
					"action":        "accept",
					"comment":       "old",
					"place_before":  "",
					"enforce_order": "true",
					"log":           "false",
				},
			}
			diff, err := res.Diff(ctx, state, terraform.NewResourceConfigRaw(cfg), client)
			if err != nil {
				t.Fatalf("diff: %v", err)
			}
			applied, diags := res.Apply(ctx, state, diff, client)
			if diags.HasError() {
				t.Fatalf("apply error: %v", diags)
			}
			if len(client.updates) != 1 {
				t.Fatalf("expected 1 crudUpdate, got %d", len(client.updates))
			}
			if _, ok := client.updates[0]["place-before"]; ok {
				t.Errorf("place-before leaked into crudUpdate: %v", client.updates[0])
			}
			if _, ok := client.updates[0]["enforce-order"]; ok {
				t.Errorf("enforce-order leaked into crudUpdate: %v", client.updates[0])
			}
			if client.updates[0]["comment"] != "new" {
				t.Errorf("expected comment=new in update, got %v", client.updates[0])
			}
			if len(client.moves) != 1 {
				t.Fatalf("expected 1 move, got %d", len(client.moves))
			}
			if applied.Attributes["comment"] != "new" || applied.Attributes["place_before"] != "*99" {
				t.Errorf("unexpected applied state: %v", applied.Attributes)
			}
		})

		t.Run(tName+"/anchor_disappeared_and_replaced", func(t *testing.T) {
			client := &mockFirewallTableClient{
				transport: transport,
				rules: []MikrotikItem{
					{".id": "*100", "chain": "forward", "action": "drop"},
					{".id": "*1", "chain": "forward", "action": "accept"},
				},
			}
			state := &terraform.InstanceState{
				ID: "*1",
				Attributes: map[string]string{
					"id":            "*1",
					"chain":         "forward",
					"action":        "accept",
					"place_before":  "*99",
					"enforce_order": "true",
					"log":           "false",
				},
			}

			// Read detects missing anchor *99.
			refreshed, diags := res.RefreshWithoutUpgrade(ctx, state, client)
			if diags.HasError() {
				t.Fatalf("refresh error: %v", diags)
			}
			if refreshed.Attributes["place_before"] != "" {
				t.Fatalf("expected place_before cleared when anchor missing, got %q", refreshed.Attributes["place_before"])
			}

			// Applying old config targeting missing *99 fails clearly and keeps state accurate.
			cfgMissing := map[string]interface{}{
				"chain":         "forward",
				"action":        "accept",
				"place_before":  "*99",
				"enforce_order": true,
			}
			diffMissing, err := res.Diff(ctx, refreshed, terraform.NewResourceConfigRaw(cfgMissing), client)
			if err != nil {
				t.Fatalf("diff: %v", err)
			}
			failedState, diags := res.Apply(ctx, refreshed, diffMissing, client)
			assertDiagHasError(t, diags, `anchor rule "*99" not found`)
			if failedState != nil && failedState.Attributes["place_before"] != "" {
				t.Errorf("expected place_before empty after failed update, got %q", failedState.Attributes["place_before"])
			}

			// Updating config to replacement anchor *100 moves rule in-place.
			cfgReplaced := map[string]interface{}{
				"chain":         "forward",
				"action":        "accept",
				"place_before":  "*100",
				"enforce_order": true,
			}
			diffReplaced, err := res.Diff(ctx, refreshed, terraform.NewResourceConfigRaw(cfgReplaced), client)
			if err != nil {
				t.Fatalf("diff: %v", err)
			}
			if diffReplaced.RequiresNew() {
				t.Fatalf("expected in-place update when changing place_before, got RequiresNew")
			}
			applied, diags := res.Apply(ctx, refreshed, diffReplaced, client)
			if diags.HasError() {
				t.Fatalf("apply error: %v", diags)
			}
			if applied.ID != "*1" {
				t.Errorf("expected ID *1 preserved, got %q", applied.ID)
			}
			if applied.Attributes["place_before"] != "*100" {
				t.Errorf("place_before = %q, want *100", applied.Attributes["place_before"])
			}
		})

		t.Run(tName+"/partial_move_failure_returns_error_and_syncs_state", func(t *testing.T) {
			for _, tc := range []struct {
				name   string
				noOp   bool
				err    error
				readFn bool
			}{
				{name: "move_returns_nil_without_reordering", noOp: true},
				{name: "move_returns_error", err: errors.New("router rejected move")},
				{name: "post_move_read_fails", readFn: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					client := &mockFirewallTableClient{
						transport: transport,
						rules: []MikrotikItem{
							{".id": "*99", "chain": "forward", "action": "drop"},
							{".id": "*1", "chain": "forward", "action": "accept"},
						},
						partialMoveNoOp:  tc.noOp,
						moveErr:          tc.err,
						failAfterMoveGet: tc.readFn,
					}
					state := &terraform.InstanceState{
						ID: "*1",
						Attributes: map[string]string{
							"id":            "*1",
							"chain":         "forward",
							"action":        "accept",
							"place_before":  "",
							"enforce_order": "true",
							"log":           "false",
						},
					}
					cfg := map[string]interface{}{
						"chain":         "forward",
						"action":        "accept",
						"place_before":  "*99",
						"enforce_order": true,
					}
					diff, err := res.Diff(ctx, state, terraform.NewResourceConfigRaw(cfg), client)
					if err != nil {
						t.Fatalf("diff: %v", err)
					}
					after, diags := res.Apply(ctx, state, diff, client)
					if !diags.HasError() {
						t.Fatal("expected error diagnostic on partial move failure")
					}
					if !tc.readFn && after != nil && after.Attributes["place_before"] != "" {
						t.Errorf("expected place_before cleared in state after failed move, got %q", after.Attributes["place_before"])
					}
				})
			}
		})

		t.Run(tName+"/managed_rule_missing_on_update", func(t *testing.T) {
			client := &mockFirewallTableClient{
				transport: transport,
				rules: []MikrotikItem{
					{".id": "*99", "chain": "forward", "action": "drop"},
				},
			}
			state := &terraform.InstanceState{
				ID: "*1",
				Attributes: map[string]string{
					"id":            "*1",
					"chain":         "forward",
					"action":        "accept",
					"place_before":  "",
					"enforce_order": "true",
					"log":           "false",
				},
			}
			cfg := map[string]interface{}{
				"chain":         "forward",
				"action":        "accept",
				"place_before":  "*99",
				"enforce_order": true,
			}
			diff, err := res.Diff(ctx, state, terraform.NewResourceConfigRaw(cfg), client)
			if err != nil {
				t.Fatalf("diff: %v", err)
			}
			_, diags := res.Apply(ctx, state, diff, client)
			assertDiagHasError(t, diags, `managed rule "*1" not found`)
		})

		t.Run(tName+"/enforce_false_place_before_change_moves_inplace", func(t *testing.T) {
			client := &mockFirewallTableClient{
				transport: transport,
				rules: []MikrotikItem{
					{".id": "*88", "chain": "forward", "action": "drop"},
					{".id": "*99", "chain": "forward", "action": "drop"},
					{".id": "*1", "chain": "forward", "action": "accept"},
				},
			}
			state := &terraform.InstanceState{
				ID: "*1",
				Attributes: map[string]string{
					"id":            "*1",
					"chain":         "forward",
					"action":        "accept",
					"place_before":  "*88",
					"enforce_order": "false",
					"log":           "false",
				},
			}
			cfg := map[string]interface{}{
				"chain":         "forward",
				"action":        "accept",
				"place_before":  "*99",
				"enforce_order": false,
			}
			diff, err := res.Diff(ctx, state, terraform.NewResourceConfigRaw(cfg), client)
			if err != nil {
				t.Fatalf("diff: %v", err)
			}
			if diff == nil || diff.RequiresNew() {
				t.Fatalf("expected in-place diff when changing place_before, got %+v", diff)
			}
			applied, diags := res.Apply(ctx, state, diff, client)
			if diags.HasError() {
				t.Fatalf("apply error: %v", diags)
			}
			if applied.ID != "*1" {
				t.Errorf("expected ID *1 preserved, got %q", applied.ID)
			}
			if len(client.updates) != 0 {
				t.Errorf("expected 0 crudUpdate calls, got %d", len(client.updates))
			}
			if len(client.moves) != 1 || client.moves[0].destination != "*99" {
				t.Errorf("expected 1 move to *99, got %+v", client.moves)
			}
		})
	}
}

func configureTestAccClient(t *testing.T) Client {
	t.Helper()
	diags := testAccProvider.Configure(context.Background(), terraform.NewResourceConfigRaw(map[string]interface{}{
		"insecure": true,
	}))
	if diags.HasError() {
		t.Fatalf("failed to configure testAccProvider: %v", diags)
	}
	client, ok := testAccProvider.Meta().(Client)
	if !ok || client == nil {
		t.Fatal("testAccProvider.Meta() did not return a Client")
	}
	return client
}

func checkFirewallRulesOrdered(client Client, beforeID, afterID string) error {
	rules, err := ReadItems(nil, "/ip/firewall/filter", client)
	if err != nil {
		return fmt.Errorf("ReadItems(/ip/firewall/filter): %w", err)
	}
	beforeIdx, afterIdx := findFirewallRuleAndAnchorIndex(*rules, beforeID, afterID)
	if beforeIdx < 0 || afterIdx < 0 || beforeIdx >= afterIdx {
		return fmt.Errorf("expected rule %q before %q in /ip/firewall/filter, got beforeIdx=%d afterIdx=%d", beforeID, afterID, beforeIdx, afterIdx)
	}
	return nil
}

func cleanupAccFirewallRulesByCommentPrefix(client Client, prefix string) {
	rules, err := ReadItems(nil, "/ip/firewall/filter", client)
	if err != nil {
		return
	}
	for _, r := range *rules {
		if strings.HasPrefix(r["comment"], prefix) {
			_ = DeleteItem(&ItemId{Type: Id, Value: r[".id"]}, "/ip/firewall/filter", client)
		}
	}
}

func TestAccIPFirewallFilter_orderDrift(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance test: set TF_ACC, ROS_HOSTURL and ROS_USERNAME to run it")
	}

	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			commentPrefix := "tf-acc-order-" + strings.ToLower(name) + "-"
			drop1Comment := commentPrefix + "unmanaged-drop-1"
			drop2Comment := commentPrefix + "unmanaged-drop-2"
			managedComment := commentPrefix + "managed"
			unrelatedComment := commentPrefix + "unrelated"

			var capturedManagedID string
			var drop1ID string
			var drop2ID string

			t.Cleanup(func() {
				if client, ok := testAccProvider.Meta().(Client); ok && client != nil {
					cleanupAccFirewallRulesByCommentPrefix(client, commentPrefix)
				}
			})

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
						PreConfig: func() {
							client := testAccProvider.Meta().(Client)
							cleanupAccFirewallRulesByCommentPrefix(client, commentPrefix)
							res, err := CreateItem(context.Background(), MikrotikItem{
								"chain":   "forward",
								"action":  "drop",
								"comment": drop1Comment,
							}, "/ip/firewall/filter", client)
							if err != nil {
								t.Fatalf("failed creating unmanaged drop1 rule: %v", err)
							}
							drop1ID = res.GetID(Id)
							if drop1ID == "" {
								t.Fatalf("created unmanaged drop1 rule without id: %#v", res)
							}
						},
						Config: testAccIPFirewallFilterOrderConfig(drop1Comment, managedComment, unrelatedComment),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId("routeros_ip_firewall_filter.managed"),
							func(s *testingterraform.State) error {
								rs := s.RootModule().Resources["routeros_ip_firewall_filter.managed"]
								capturedManagedID = rs.Primary.ID
								return checkFirewallRulesOrdered(testAccProvider.Meta().(Client), capturedManagedID, drop1ID)
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
							// Move drop1 before managed so managed sits AFTER drop1 out-of-band.
							if err := client.SendRequest(crudMove, &URL{Path: movePath}, MikrotikItem{
								"numbers":     drop1ID,
								"destination": capturedManagedID,
							}, nil); err != nil {
								t.Fatalf("out-of-band move failed: %v", err)
							}
						},
						Config: testAccIPFirewallFilterOrderConfig(drop1Comment, managedComment, unrelatedComment),
						Check: resource.ComposeTestCheckFunc(
							func(s *testingterraform.State) error {
								rs := s.RootModule().Resources["routeros_ip_firewall_filter.managed"]
								if rs.Primary.ID != capturedManagedID {
									return fmt.Errorf("managed rule recreated: got %q, want %q", rs.Primary.ID, capturedManagedID)
								}
								return checkFirewallRulesOrdered(testAccProvider.Meta().(Client), capturedManagedID, drop1ID)
							},
						),
					},
					{
						PreConfig: func() {
							client := testAccProvider.Meta().(Client)
							res, err := CreateItem(context.Background(), MikrotikItem{
								"chain":   "forward",
								"action":  "drop",
								"comment": drop2Comment,
							}, "/ip/firewall/filter", client)
							if err != nil {
								t.Fatalf("failed creating replacement unmanaged drop2 rule: %v", err)
							}
							drop2ID = res.GetID(Id)
							if drop2ID == "" {
								t.Fatalf("created replacement unmanaged drop2 rule without id: %#v", res)
							}
							if err := DeleteItem(&ItemId{Type: Id, Value: drop1ID}, "/ip/firewall/filter", client); err != nil {
								t.Fatalf("failed deleting old unmanaged drop1 rule: %v", err)
							}
						},
						Config: testAccIPFirewallFilterOrderConfig(drop2Comment, managedComment, unrelatedComment),
						Check: resource.ComposeTestCheckFunc(
							func(s *testingterraform.State) error {
								rs := s.RootModule().Resources["routeros_ip_firewall_filter.managed"]
								if rs.Primary.ID != capturedManagedID {
									return fmt.Errorf("managed rule recreated on anchor change: got %q, want %q", rs.Primary.ID, capturedManagedID)
								}
								return checkFirewallRulesOrdered(testAccProvider.Meta().(Client), capturedManagedID, drop2ID)
							},
						),
					},
				},
			})
		})
	}
}

func testAccIPFirewallFilterOrderConfig(anchorComment, managedComment, unrelatedComment string) string {
	return providerConfig + fmt.Sprintf(`
data "routeros_ip_firewall" "anchor" {
  rules {
    filter = {
      comment = %q
    }
  }
}

resource "routeros_ip_firewall_filter" "unrelated" {
  chain    = "forward"
  action   = "passthrough"
  disabled = true
  comment  = %q
}

resource "routeros_ip_firewall_filter" "managed" {
  chain         = "forward"
  action        = "accept"
  disabled      = true
  comment       = %q
  place_before  = data.routeros_ip_firewall.anchor.rules[0].id
  enforce_order = true
}
`, anchorComment, unrelatedComment, managedComment)
}

func assertDiagHasError(t *testing.T, diags diag.Diagnostics, substr string) {
	t.Helper()
	if !diags.HasError() {
		t.Fatalf("expected error diagnostic containing %q, got none", substr)
	}
	for _, d := range diags {
		if d.Severity == diag.Error && (strings.Contains(d.Summary, substr) || strings.Contains(d.Detail, substr)) {
			return
		}
	}
	t.Fatalf("expected error diagnostic containing %q, got %+v", substr, diags)
}
