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
// the source router, so it has to reach the device. It is also the field most likely to churn a plan.
// RouterOS hides stored secrets from an account that does not carry the
// `sensitive` policy, so a provider account without it reads the entry back with a mask in place of the
// password, and the mask lands in the state. The diff that follows is left in place on purpose: a
// suppression wide enough to swallow the mask would swallow a genuine password rotation as well, and a
// rotation that silently never reaches the device is the worse of the two failures. Give the provider
// account the `sensitive` policy for a quiet plan, or accept a credential rewritten on every apply.
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
			// Only for the entry nobody declared a password for: an imported or device-made source keeps
			// whatever the router reports without asking for a rewrite. A declared password still diffs.
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
