package routeros

import (
	"context"

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

		Schema: resSchema,
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
// shows as the change it is. On import nothing has been applied yet, so the
// router's own values are kept.
var systemClockAppliedFields = []string{"date", "time"}

func systemClockApply(s map[string]*schema.Schema) func(context.Context, *schema.ResourceData, interface{}) diag.Diagnostics {
	return func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
		item, metadata := TerraformResourceDataToMikrotik(s, d)

		// Only a date or time that changed is written. The request otherwise
		// carries every value as it stands, and for these two that is the value
		// last applied: an edit to time_zone_name a week later would rewind the
		// router by a week.
		for _, field := range systemClockAppliedFields {
			if !d.HasChange(field) {
				delete(item, SnakeToKebab(field))
			}
		}

		var resUrl string
		if m.(Client).GetTransport() == TransportREST {
			resUrl = "/set"
		}

		if err := m.(Client).SendRequest(crudPost, &URL{Path: metadata.Path + resUrl}, item, nil); err != nil {
			return diag.FromErr(err)
		}

		return systemClockReadKeepingApplied(ctx, s, d, m)
	}
}

func systemClockRead(s map[string]*schema.Schema) func(context.Context, *schema.ResourceData, interface{}) diag.Diagnostics {
	return func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
		return systemClockReadKeepingApplied(ctx, s, d, m)
	}
}

func systemClockReadKeepingApplied(ctx context.Context, s map[string]*schema.Schema, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	applied := systemClockCapture(d)

	diags := SystemResourceRead(ctx, s, d, m)
	if diags.HasError() {
		return diags
	}

	return append(diags, systemClockRestore(d, applied)...)
}

// systemClockCapture records the date and time the resource data holds before a
// read replaces them with the router's: the planned values during an apply, the
// stored ones during a refresh.
func systemClockCapture(d *schema.ResourceData) map[string]string {
	applied := map[string]string{}
	for _, field := range systemClockAppliedFields {
		if v, ok := d.Get(field).(string); ok && v != "" {
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
