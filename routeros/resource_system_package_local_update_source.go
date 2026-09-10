package routeros

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

/*
  {
    ".id": "*1",
    "address": "192.168.88.1",
    "password": "package-source-secret",
    "user": "package-source"
  }
*/

// A local-update client takes its .npk files from the sources listed in this menu instead of from
// MikroTik's download servers. MikroTik documents the menu as available from 7.17beta3. The transfer runs
// over WinBox rather than HTTP or FTP, so the address has to be one the source router answers WinBox on,
// and the account named here lives on the source router, not on this one.
//
// The password is the field the resource lives or dies by: an entry without one cannot authenticate to
// the source router, so it has to reach the device. RouterOS hides stored secrets from an account that
// does not carry the `sensitive` policy, so a provider account without it reads the entry back with a mask
// in place of the password, and the mask lands in the state. A declared password is then compared against
// the mask on every plan, and each apply writes the real password again. That churn is left in place on
// purpose: a suppression wide enough to swallow the mask would swallow a genuine rotation as well, and a
// rotation that silently never reaches the device is the worse failure. Give the provider account the
// `sensitive` policy for a quiet plan.
//
// An entry whose configuration declares no password, such as an imported one, is handled differently.
// Its diff is suppressed so the plan does not offer to write a password nobody set, and the serializer
// never writes back a sensitive value that configuration does not declare, so changing the entry's user
// or address leaves the credential on the source router as it was.
//
// https://help.mikrotik.com/docs/display/ROS/Upgrading+and+installation
func ResourceSystemPackageLocalUpdateSource() *schema.Resource {
	resSchema := map[string]*schema.Schema{
		MetaResourcePath: PropResourcePath("/system/package/local-update/update-package-source"),
		MetaId:           PropId(Id),

		"address": {
			Type:     schema.TypeString,
			Required: true,
			Description: "Address of the router that serves the packages. The client opens a WinBox connection to " +
				"it, so the source router has to answer WinBox on this address and let this client reach it.",
		},
		"password": {
			Type:        schema.TypeString,
			Optional:    true,
			Sensitive:   true,
			Description: "Password of the account the client authenticates to the package source with.",
			// Keeps an entry with no declared password out of the plan. Nothing is written for it either:
			// the serializer never sends an unchanged sensitive value that configuration does not declare,
			// so a mask read back into state cannot replace the credential. A declared password still diffs
			// and is still sent, which is what lets a rotation reach the device.
			DiffSuppressFunc: AlwaysPresentNotUserProvided,
		},
		"user": {
			Type:     schema.TypeString,
			Optional: true,
			Description: "Name of the account the client authenticates to the package source with. It is an " +
				"account on the source router, and reading that router's file list is all it has to be able to do.",
		},
	}

	return &schema.Resource{
		Description:   `*<span style="color:red">This resource requires a minimum version of RouterOS 7.17.</span>*`,
		CreateContext: DefaultCreate(resSchema),
		ReadContext:   DefaultRead(resSchema),
		UpdateContext: DefaultUpdate(resSchema),
		DeleteContext: DefaultDelete(resSchema),

		Importer: &schema.ResourceImporter{
			StateContext: ImportStateCustomContext(resSchema),
		},

		Schema: resSchema,
	}
}
