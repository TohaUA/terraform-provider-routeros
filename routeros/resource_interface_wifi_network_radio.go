package routeros

import (
	"context"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

func ResourceWifiNetworkRadio() *schema.Resource {
	r := newAPI725Resource("/interface/wifi/network/radio", "list")
	r.Description += " Labels select radios; they are not entry identifiers. Import using the RouterOS .id. Requires compatible hardware for radio operation."
	r.Importer.StateContext = schema.ImportStatePassthroughContext
	r.Schema["configuration_distance"].DiffSuppressFunc = wifiDistanceEqual
	r.Schema["channel_reselect_interval"].DiffSuppressFunc = TimeEqual
	r.Schema["channel_reselect_time"].DiffSuppressFunc = TimeEqual
	r.UpdateContext = func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
		if ds := requireAPI725(); ds.HasError() {
			return ds
		}
		if ds := ResourceUpdate(ctx, r.Schema, d, m); ds.HasError() {
			return ds
		}
		return ResourceRead(ctx, r.Schema, d, m)
	}
	var names []string
	var unset []string
	for name, f := range r.Schema {
		if !strings.HasPrefix(name, "___") && !f.Computed {
			names = append(names, name)
			if f.Type == schema.TypeString {
				unset = append(unset, name)
				if original := f.ValidateFunc; original != nil {
					f.ValidateFunc = validation.Any(validation.StringInSlice([]string{""}, false), original)
				}
			}
		}
	}
	sort.Strings(unset)
	r.Schema[MetaSetUnsetFields] = PropSetUnsetFields(unset...)
	return protectWifi7Inputs(r, names)
}
