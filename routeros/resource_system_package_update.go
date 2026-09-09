package routeros

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

// The menu a router uses to decide which RouterOS release it should be comparing itself against. A
// RouterOS 7.24 router answers it with seven properties -- channel, check-certificate,
// installed-version, ip-version, latest-version, mode and status -- and `channel` is the one this
// resource writes. The other six are declared read-only, for two different reasons.
//
// installed-version, latest-version and status are what the router found rather than what anyone asks
// of it, so there is nothing to write. check-certificate, ip-version and mode are a different case:
// they genuinely are writable, and recently so. The console command tree captured off this fleet shows
// `/system/package/update set` accepting all four properties on 7.23 and 7.24 and accepting `channel`
// alone on 7.19 through 7.22. They are still declared read-only here because their accepted values were
// never checked against a router, and a validator built on a guess invites an apply that RouterOS
// rejects; leaving them undeclared is the other failure, since a property the device returns that the
// schema does not know about turns every read into a warning. Whoever confirms what check-certificate,
// ip-version and mode accept can make them writable then. On a router below 7.23 the three do not exist
// in the menu at all, so those attributes stay empty there however this resource is configured.
//
// Checking for updates and installing them are one-shot actions rather than desired state, so this
// resource does neither. It records the channel and reads back what the router last found.
//
// https://help.mikrotik.com/docs/display/ROS/Upgrading+and+installation
func ResourceSystemPackageUpdate() *schema.Resource {
	resSchema := map[string]*schema.Schema{
		MetaResourcePath: PropResourcePath("/system/package/update"),
		MetaId:           PropId(Id),

		"channel": {
			Type:     schema.TypeString,
			Optional: true,
			Description: "Release channel the router compares its installed version against and takes packages " +
				"from.",
			ValidateFunc:     validation.StringInSlice([]string{"development", "long-term", "stable", "testing"}, false),
			DiffSuppressFunc: AlwaysPresentNotUserProvided,
		},
		"check_certificate": {
			Type:        schema.TypeString,
			Computed:    true,
			Description: "Whether the router validates the update server's certificate. Read-only here, see the resource note.",
		},
		"installed_version": {
			Type:        schema.TypeString,
			Computed:    true,
			Description: "RouterOS version the router is running.",
		},
		"ip_version": {
			Type:        schema.TypeString,
			Computed:    true,
			Description: "IP version the router reaches the update server over. Read-only here, see the resource note.",
		},
		"latest_version": {
			Type:     schema.TypeString,
			Computed: true,
			Description: "Version the last check found on the selected channel. It stays empty until the router has " +
				"run a check.",
		},
		"mode": {
			Type:        schema.TypeString,
			Computed:    true,
			Description: "Update mode the router reports. Read-only here, see the resource note.",
		},
		"status": {
			Type:     schema.TypeString,
			Computed: true,
			Description: "How the last check ended, in the router's own words. It stays empty until the router has " +
				"run a check.",
		},
	}

	return &schema.Resource{
		CreateContext: DefaultSystemCreate(resSchema),
		ReadContext:   DefaultSystemRead(resSchema),
		UpdateContext: DefaultSystemUpdate(resSchema),
		DeleteContext: DefaultSystemDelete(resSchema),

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: resSchema,
	}
}
