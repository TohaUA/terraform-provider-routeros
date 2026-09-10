package routeros

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

// A read writes the router's running clock into the resource data. What goes
// into state afterwards has to be what was applied, or the next plan compares
// the configuration against a clock that has moved on.
func TestSystemClockKeepsTheAppliedDateAndTime(t *testing.T) {
	d := ResourceSystemClock().Data(&terraform.InstanceState{
		ID: "system.clock",
		Attributes: map[string]string{
			"id":             "system.clock",
			"date":           "2026-09-10",
			"time":           "10:15:30",
			"time_zone_name": "UTC",
		},
	})

	applied := systemClockCapture(d, true)

	// What a read leaves behind: the router's clock a week and seven seconds on,
	// and a time zone the router changed on its own.
	for field, v := range map[string]string{"date": "2026-09-17", "time": "10:15:37", "time_zone_name": "Europe/Riga"} {
		if err := d.Set(field, v); err != nil {
			t.Fatal(err)
		}
	}

	if diags := systemClockRestore(d, applied); diags.HasError() {
		t.Fatalf("restore: %v", diags)
	}

	for field, want := range map[string]string{"date": "2026-09-10", "time": "10:15:30"} {
		if got := d.Get(field); got != want {
			t.Errorf("%s = %q after a read, want the applied %q", field, got, want)
		}
	}

	// Only the moving clock is held back; everything else still reflects the router.
	if got := d.Get("time_zone_name"); got != "Europe/Riga" {
		t.Errorf("time_zone_name = %q, want the router's reading to stand", got)
	}
}

// An empty value means different things on the two paths. Planned for an apply,
// it is a field the configuration leaves out, and the router's reading may stand
// in. Stored and refreshed, it means nothing was applied, and the router's
// reading must not take its place or it passes for an applied value.
func TestSystemClockEmptyDateAndTime(t *testing.T) {
	for _, tc := range []struct {
		name    string
		refresh bool
		want    string
	}{
		{"apply", false, "10:15:37"},
		{"refresh", true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := ResourceSystemClock().Data(&terraform.InstanceState{
				ID:         "system.clock",
				Attributes: map[string]string{"id": "system.clock"},
			})

			applied := systemClockCapture(d, tc.refresh)
			if err := d.Set("time", "10:15:37"); err != nil {
				t.Fatal(err)
			}
			if diags := systemClockRestore(d, applied); diags.HasError() {
				t.Fatalf("restore: %v", diags)
			}

			if got := d.Get("time"); got != tc.want {
				t.Errorf("time = %q, want %q", got, tc.want)
			}
		})
	}
}

// fakeClockRouter answers /system/clock like a router whose clock has run on
// since it was last read, and records every set it receives.
type fakeClockRouter struct {
	clock MikrotikItem
	sets  []MikrotikItem
}

func (f *fakeClockRouter) GetExtraParams() *ExtraParams { return &ExtraParams{} }
func (f *fakeClockRouter) GetTransport() TransportType  { return TransportAPI }

func (f *fakeClockRouter) SendRequest(method crudMethod, _ *URL, item MikrotikItem, result interface{}) error {
	switch method {
	case crudPost:
		f.sets = append(f.sets, item)
	case crudRead:
		for k, v := range f.clock {
			(*result.(*MikrotikItem))[k] = v
		}
	}
	return nil
}

func (f *fakeClockRouter) lastSet(t *testing.T) MikrotikItem {
	t.Helper()
	if len(f.sets) == 0 {
		t.Fatal("the apply sent nothing to the router")
	}
	return f.sets[len(f.sets)-1]
}

// planAndApply plans config against state the way Terraform does, after a
// refresh, and applies the plan. It returns the state unchanged when nothing
// was planned.
func planAndApply(t *testing.T, state *terraform.InstanceState, config map[string]string,
	router *fakeClockRouter) (*terraform.InstanceState, diag.Diagnostics, bool) {
	t.Helper()
	ctx := context.Background()
	r := ResourceSystemClock()

	if state.ID != "" {
		refreshed, diags := r.RefreshWithoutUpgrade(ctx, state, router)
		if diags.HasError() {
			t.Fatalf("refresh: %v", diags)
		}
		state = refreshed
	}

	block := r.CoreConfigSchema()
	attrs := map[string]cty.Value{}
	for name, ty := range block.ImpliedType().AttributeTypes() {
		attrs[name] = cty.NullVal(ty)
	}
	for name, v := range config {
		attrs[name] = cty.StringVal(v)
	}
	cfg := cty.ObjectVal(attrs)

	// Terraform always sends the configuration as a value alongside its legacy
	// form; the diff suppression functions read it from there.
	rc := terraform.NewResourceConfigShimmed(cfg, block)
	rc.CtyValue = cfg
	diff, err := r.SimpleDiff(ctx, state, rc, router)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if diff == nil || diff.Empty() {
		return state, nil, false
	}
	diff.RawConfig = cfg

	applied, diags := r.Apply(ctx, state, diff, router)
	if diags.HasError() {
		t.Fatalf("apply: %v", diags)
	}
	return applied, diags, true
}

// State from before the applied date and time were kept holds the router's
// reading from its last refresh. After the upgrade, an unchanged configuration
// must not set the clock back to the configured time of day; the configured
// values are recorded, the router is left alone, and the next plan is clean.
// A deliberate change after that is written as usual.
func TestSystemClockLeavesAClockItNeverAppliedAlone(t *testing.T) {
	defer func(v string) { RouterOSVersion = v }(RouterOSVersion)
	RouterOSVersion = "7.24"
	ctx := context.Background()

	r := ResourceSystemClock()
	if len(r.StateUpgraders) == 0 {
		t.Fatal("no state upgrader: legacy state keeps the router's old reading as if it had been applied")
	}
	legacy := map[string]interface{}{
		"id": "system.clock", "date": "2026-09-03", "time": "03:12:40", "time_zone_name": "UTC",
	}
	upgraded, err := r.StateUpgraders[0].Upgrade(ctx, legacy, nil)
	if err != nil {
		t.Fatal(err)
	}
	attrs := map[string]string{}
	for k, v := range upgraded {
		if s, ok := v.(string); ok {
			attrs[k] = s
		}
	}
	if attrs["time_zone_name"] != "UTC" {
		t.Errorf("the upgrade lost time_zone_name: %v", upgraded)
	}
	state := &terraform.InstanceState{ID: "system.clock", Attributes: attrs}

	router := &fakeClockRouter{clock: MikrotikItem{
		"date": "2026-09-10", "time": "03:12:44", "time-zone-name": "UTC",
		"time-zone-autodetect": "false", "dst-active": "false", "gmt-offset": "+00:00",
	}}
	config := map[string]string{"date": "2026-09-03", "time": "17:58:11", "time_zone_name": "UTC"}

	state, diags, planned := planAndApply(t, state, config, router)
	if planned {
		sent := router.lastSet(t)
		for _, key := range []string{"date", "time"} {
			if v, ok := sent[key]; ok {
				t.Errorf("the apply wrote %s=%s to a clock nothing had applied", key, v)
			}
		}
		if !diags.HasError() && len(diags) == 0 {
			t.Error("recording the configured values without writing them gave no warning")
		}
		for _, dg := range diags {
			if dg.Severity == diag.Warning && !strings.Contains(dg.Summary, "not written") {
				t.Errorf("unexpected warning: %s", dg.Summary)
			}
		}
	}
	for field, want := range map[string]string{"date": "2026-09-03", "time": "17:58:11"} {
		if got := state.Attributes[field]; got != want {
			t.Errorf("state %s = %q, want the configured %q recorded", field, got, want)
		}
	}

	sets := len(router.sets)
	if _, _, planned := planAndApply(t, state, config, router); planned || len(router.sets) != sets {
		t.Error("the plan after recording the configured values is not clean")
	}

	changed := map[string]string{"date": "2026-09-03", "time": "18:30:00", "time_zone_name": "UTC"}
	if _, _, planned := planAndApply(t, state, changed, router); !planned {
		t.Fatal("a deliberate change to the time planned nothing")
	}
	if got := router.lastSet(t)["time"]; got != "18:30:00" {
		t.Errorf("a deliberate change sent time=%q, want 18:30:00", got)
	}
	if _, ok := router.lastSet(t)["date"]; ok {
		t.Error("a change to the time alone also wrote the unchanged date")
	}
}

// Creating the resource with a date and time is asking for the clock to be set.
func TestSystemClockCreateWritesTheConfiguredTime(t *testing.T) {
	defer func(v string) { RouterOSVersion = v }(RouterOSVersion)
	RouterOSVersion = "7.24"

	router := &fakeClockRouter{clock: MikrotikItem{"date": "2026-09-10", "time": "03:12:44", "time-zone-name": "UTC"}}
	state, _, planned := planAndApply(t, &terraform.InstanceState{}, map[string]string{"time": "17:58:11"}, router)
	if !planned {
		t.Fatal("creating the resource planned nothing")
	}
	if got := router.lastSet(t)["time"]; got != "17:58:11" {
		t.Errorf("create sent time=%q, want 17:58:11", got)
	}
	if got := state.Attributes["time"]; got != "17:58:11" {
		t.Errorf("state time = %q after create, want 17:58:11", got)
	}
}
