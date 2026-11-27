// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package routeros

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/terraform-routeros/terraform-provider-routeros/internal/macboot"
)

func TestDatasourceMNDPNeighborsSchemaAndRead(t *testing.T) {
	if err := Provider().InternalValidate(); err != nil {
		t.Fatalf("Provider().InternalValidate() failed: %v", err)
	}

	ds := DatasourceMNDPNeighbors()
	if _, ok := Provider().DataSourcesMap["routeros_mndp_neighbors"]; !ok {
		t.Fatalf("routeros_mndp_neighbors not registered in DataSourcesMap")
	}

	origDiscover := discoverMNDPFunc
	defer func() { discoverMNDPFunc = origDiscover }()

	discoverMNDPFunc = func(_ context.Context, opts macboot.DiscoverOptions) ([]macboot.MNDPNeighbor, error) {
		if opts.Interface != "eth1" {
			t.Fatalf("expected interface eth1, got %q", opts.Interface)
		}
		if opts.Timeout != 3*time.Second {
			t.Fatalf("expected timeout 3s, got %s", opts.Timeout)
		}
		return []macboot.MNDPNeighbor{
			{
				MACAddress:    "54:05:AB:01:02:03",
				Identity:      "MikroTik",
				Version:       "7.25.2 (stable)",
				Platform:      "MikroTik",
				UptimeSeconds: 42,
				SoftwareID:    "SOFT-1234",
				Board:         "CHR",
				Unpack:        1,
				IPv6Address:   "fe80::1",
				InterfaceName: "ether2",
				IPv4Address:   "",
				SourceIP:      "0.0.0.0",
			},
		}, nil
	}

	d := schema.TestResourceDataRaw(t, ds.Schema, map[string]interface{}{
		"interface": "eth1",
		"timeout":   "3s",
	})
	diags := ds.ReadContext(context.Background(), d, &unconfiguredClient{})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %+v", diags)
	}
	if d.Id() != "mndp:eth1" {
		t.Fatalf("Id = %q, want mndp:eth1", d.Id())
	}

	neighbors := d.Get("neighbors").([]interface{})
	if len(neighbors) != 1 {
		t.Fatalf("expected 1 neighbor, got %d", len(neighbors))
	}
	first := neighbors[0].(map[string]interface{})
	if first["mac_address"] != "54:05:AB:01:02:03" || first["board"] != "CHR" || first["uptime_seconds"] != 42 {
		t.Fatalf("unexpected neighbor map: %+v", first)
	}
}

func TestDatasourceMNDPNeighborsValidation(t *testing.T) {
	ds := DatasourceMNDPNeighbors()
	vf := ds.Schema["timeout"].ValidateFunc
	if _, errs := vf("not-a-duration", "timeout"); len(errs) == 0 {
		t.Fatalf("expected validation error for invalid timeout")
	}
	if _, errs := vf("-1s", "timeout"); len(errs) == 0 {
		t.Fatalf("expected validation error for negative timeout")
	}
	if _, errs := vf("5s", "timeout"); len(errs) != 0 {
		t.Fatalf("unexpected validation error for valid timeout: %+v", errs)
	}
}
