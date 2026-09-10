package routeros

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

/* {
  "date": "2024-05-17",
  "dst-active": "false",
  "gmt-offset": "+01:00",
  "time": "17:58:11",
  "time-zone-autodetect": "false",
  "time-zone-name": "Etc/GMT-1"
} */

func ResourceSystemClock() *schema.Resource {
	resSchema := map[string]*schema.Schema{
		MetaResourcePath: PropResourcePath("/system/clock"),
		MetaId:           PropId(Id),
		"date": {
			Type:             schema.TypeString,
			Optional:         true,
			Description:      `Date.`,
			DiffSuppressFunc: AlwaysPresentNotUserProvided,
		},
		"dst_active": {
			Type:        schema.TypeBool,
			Computed:    true,
			Description: `This property has the value yes while daylight saving time of the current time zone is active.`,
		},
		"gmt_offset": {
			Type:     schema.TypeString,
			Computed: true,
			Description: `This is the current value of GMT offset used by the system, after applying base time zone ` +
				`offset and active daylight saving time offset.`,
		},
		"time": {
			Type:             schema.TypeString,
			Optional:         true,
			Description:      `Time.`,
			DiffSuppressFunc: AlwaysPresentNotUserProvided,
		},
		"time_zone_autodetect": {
			Type:             schema.TypeBool,
			Optional:         true,
			Description:      `Feature available from v6.27. If enabled, the time zone will be set automatically.`,
			DiffSuppressFunc: AlwaysPresentNotUserProvided,
		},
		"time_zone_name": {
			Type:     schema.TypeString,
			Optional: true,
			Description: `Name of the time zone. As most of the text values in RouterOS, this value is case ` +
				`sensitive. Special value manual applies ` +
				`[manually configured GMT offset](https://wiki.mikrotik.com/wiki/Manual:System/Time#Manual_time_zone_configuration), ` +
				`which by default is 00:00 with no daylight saving time.`,
			DiffSuppressFunc: AlwaysPresentNotUserProvided,
		},
	}

	return &schema.Resource{
		CreateContext: systemClockApply(resSchema),
		ReadContext:   systemClockRead(resSchema),
		UpdateContext: systemClockApply(resSchema),
		DeleteContext: DefaultSystemDelete(resSchema),

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema:        resSchema,
		SchemaVersion: 1,
		StateUpgraders: []schema.StateUpgrader{
			{
				Version: 0,
				Type:    (&schema.Resource{Schema: resSchema}).CoreConfigSchema().ImpliedType(),
				Upgrade: systemClockForgetLegacyReading,
			},
		},
	}
}

// A running clock never reads back as what was written to it. Straight after the
// write it is already a second or more ahead, and a day later the date has moved
// too, so with the router's reading in state every plan compared configuration
// against a moving target. Left alone that was a permanent diff, and the apply
// that resolved it set the clock back to the old value; suppressed, it also
// swallowed a deliberate change to a new time.
//
// State therefore keeps the date and time last applied rather than the router's
// current reading. An unchanged configuration compares equal, and a changed one
// shows as the change it is.
//
// An empty value in state means nothing has been applied: the resource was
// imported, or its state predates this and was cleared by the upgrade below. The
// configured value is then recorded without being written, because comparing it
// with a reading the router took some time ago says nothing about whether the
// configuration changed, and writing it could move the clock by hours or days.
var systemClockAppliedFields = []string{"date", "time"}

// systemClockForgetLegacyReading upgrades state from before the applied date and
// time were kept. That state holds the router's reading from its last refresh,
// which was not applied by anyone: kept, it differs from an unchanged
// configuration, and the apply that follows would set the clock back to the
// configured time of day. Clearing it marks the applied values as unknown.
func systemClockForgetLegacyReading(_ context.Context, rawState map[string]interface{}, _ interface{}) (map[string]interface{}, error) {
	if rawState == nil {
		return rawState, nil
	}

	for _, field := range systemClockAppliedFields {
		rawState[field] = nil
	}

	return rawState, nil
}

func systemClockApply(s map[string]*schema.Schema) func(context.Context, *schema.ResourceData, interface{}) diag.Diagnostics {
	return func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
		item, metadata := TerraformResourceDataToMikrotik(s, d)

		// Only a date or time that changed is written. The request otherwise
		// carries every value as it stands, and for these two that is the value
		// last applied: an edit to time_zone_name a week later would rewind the
		// router by a week. A value with nothing applied before it is recorded
		// rather than written; see systemClockAppliedFields.
		var diags diag.Diagnostics
		for _, field := range systemClockAppliedFields {
			switch {
			case !d.HasChange(field):
				delete(item, SnakeToKebab(field))
			case systemClockNothingApplied(d, field):
				delete(item, SnakeToKebab(field))
				diags = append(diags, diag.Diagnostic{
					Severity: diag.Warning,
					Summary:  fmt.Sprintf("routeros_system_clock: %s recorded as applied, not written to the router", field),
					Detail: "State held no applied value, because the resource was imported or its state was written by a " +
						"provider version that stored the router's running clock. Writing the configured value could move " +
						"the clock by hours or days, so it was recorded instead. Change it in the configuration to set the clock.",
				})
			}
		}

		var resUrl string
		if m.(Client).GetTransport() == TransportREST {
			resUrl = "/set"
		}

		if err := m.(Client).SendRequest(crudPost, &URL{Path: metadata.Path + resUrl}, item, nil); err != nil {
			return diag.FromErr(err)
		}

		return append(diags, systemClockReadKeepingApplied(ctx, s, d, m, false)...)
	}
}

// systemClockNothingApplied reports a changed date or time on an existing resource
// whose state holds no applied value. A new resource has nothing applied either,
// but creating it with a date or time is asking for the clock to be set.
func systemClockNothingApplied(d *schema.ResourceData, field string) bool {
	old, _ := d.GetChange(field)
	return !d.IsNewResource() && old.(string) == ""
}

func systemClockRead(s map[string]*schema.Schema) func(context.Context, *schema.ResourceData, interface{}) diag.Diagnostics {
	return func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
		return systemClockReadKeepingApplied(ctx, s, d, m, true)
	}
}

func systemClockReadKeepingApplied(ctx context.Context, s map[string]*schema.Schema, d *schema.ResourceData, m interface{},
	refresh bool) diag.Diagnostics {
	applied := systemClockCapture(d, refresh)

	diags := SystemResourceRead(ctx, s, d, m)
	if diags.HasError() {
		return diags
	}

	return append(diags, systemClockRestore(d, applied)...)
}

// systemClockCapture records the date and time the resource data holds before a
// read replaces them with the router's: the planned values during an apply, the
// stored ones during a refresh. An empty planned value is a field the
// configuration leaves out, and the router's reading may stand in for it. An
// empty stored value means nothing has been applied, and it has to stay empty:
// the router's reading would pass for an applied value on the next plan.
func systemClockCapture(d *schema.ResourceData, refresh bool) map[string]string {
	applied := map[string]string{}
	for _, field := range systemClockAppliedFields {
		if v, _ := d.Get(field).(string); v != "" || refresh {
			applied[field] = v
		}
	}

	return applied
}

func systemClockRestore(d *schema.ResourceData, applied map[string]string) diag.Diagnostics {
	var diags diag.Diagnostics
	for field, v := range applied {
		if err := d.Set(field, v); err != nil {
			diags = append(diags, diag.FromErr(err)...)
		}
	}

	return diags
}
