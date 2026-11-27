package routeros

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

/*
{
  "allowed-interface-list": "LAN"
}
*/

// https://help.mikrotik.com/docs/display/ROS/MAC+server
func ResourceToolMacServer() *schema.Resource {
	resSchema := map[string]*schema.Schema{
		MetaResourcePath: PropResourcePath("/tool/mac-server"),
		MetaId:           PropId(Id),

		"allowed_interface_list": {
			Type:        schema.TypeString,
			Required:    true,
			Description: "List of interfaces for MAC Telnet access.",
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

// https://help.mikrotik.com/docs/display/ROS/MAC+server
func ResourceToolMacServerWinBox() *schema.Resource {
	resSchema := map[string]*schema.Schema{
		MetaResourcePath: PropResourcePath("/tool/mac-server/mac-winbox"),
		MetaId:           PropId(Id),
		MetaSkipFields:   PropSkipFields(KeyLockoutAck),

		"allowed_interface_list": {
			Type:        schema.TypeString,
			Required:    true,
			Description: "List of interfaces for MAC WinBox access.",
		},
		KeyLockoutAck: PropLockoutAck,
	}

	return &schema.Resource{
		CreateContext: func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			var c Client
			if m != nil {
				c = m.(Client)
			}
			if err := checkMacWinboxLockout(
				ctx,
				false,
				"",
				d.Get("allowed_interface_list").(string),
				d.Get(KeyLockoutAck).(string),
				c,
			); err != nil {
				return diag.FromErr(err)
			}
			return SystemResourceCreateUpdate(ctx, resSchema, d, m)
		},
		ReadContext: DefaultSystemRead(resSchema),
		UpdateContext: func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			var c Client
			if m != nil {
				c = m.(Client)
			}
			oldVal, _ := d.GetChange("allowed_interface_list")
			if err := checkMacWinboxLockout(
				ctx,
				true,
				fmt.Sprint(oldVal),
				d.Get("allowed_interface_list").(string),
				d.Get(KeyLockoutAck).(string),
				c,
			); err != nil {
				return diag.FromErr(err)
			}
			return SystemResourceCreateUpdate(ctx, resSchema, d, m)
		},
		DeleteContext: DefaultSystemDelete(resSchema),

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		CustomizeDiff: func(ctx context.Context, d *schema.ResourceDiff, m interface{}) error {
			if m == nil {
				return nil
			}
			if d.Id() != "" && !d.HasChange("allowed_interface_list") {
				return nil
			}
			oldVal, _ := d.GetChange("allowed_interface_list")
			return checkMacWinboxLockout(
				ctx,
				d.Id() != "",
				fmt.Sprint(oldVal),
				d.Get("allowed_interface_list").(string),
				d.Get(KeyLockoutAck).(string),
				m.(Client),
			)
		},

		Schema: resSchema,
	}
}

// https://help.mikrotik.com/docs/spaces/ROS/pages/98795539/MAC+server#MACserver-MACPingServer
func ResourceToolMacServerPing() *schema.Resource {
	resSchema := map[string]*schema.Schema{
		MetaResourcePath: PropResourcePath("/tool/mac-server/ping"),
		MetaId:           PropId(Id),

		KeyEnabled: PropEnabled("Whether to enable the MAC Ping server."),
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
