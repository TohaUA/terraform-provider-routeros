package routeros

import (
	"testing"

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

	applied := systemClockCapture(d)

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

func TestSystemClockKeepsTheRoutersReadingWhenNothingWasApplied(t *testing.T) {
	d := ResourceSystemClock().Data(&terraform.InstanceState{
		ID:         "system.clock",
		Attributes: map[string]string{"id": "system.clock"},
	})

	applied := systemClockCapture(d)

	if err := d.Set("time", "10:15:37"); err != nil {
		t.Fatal(err)
	}
	if diags := systemClockRestore(d, applied); diags.HasError() {
		t.Fatalf("restore: %v", diags)
	}

	if got := d.Get("time"); got != "10:15:37" {
		t.Errorf("time = %q; an import with nothing applied should keep the router's reading", got)
	}
}
