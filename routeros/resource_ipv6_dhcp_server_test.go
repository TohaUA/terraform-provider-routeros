package routeros

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testIpv6DhcpServerMinVersion = "7.20"
const testIpv6DhcpServer = "routeros_ipv6_dhcp_server.test"

func TestAccIpv6DhcpServerTest_basic(t *testing.T) {
	if !testCheckMinVersion(t, testIpv6DhcpServerMinVersion) {
		t.Logf("Test skipped, the minimum required version is %v", testIpv6DhcpServerMinVersion)
		return
	}

	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/ipv6/dhcp-server", "routeros_ipv6_dhcp_server"),
				Steps: []resource.TestStep{
					{
						Config: testAccIpv6DhcpServerConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testIpv6DhcpServer),
							resource.TestCheckResourceAttr(testIpv6DhcpServer, "address_pool", "test-pool-0"),
							resource.TestCheckResourceAttr(testIpv6DhcpServer, "prefix_pool", "test-pool-1"),
							resource.TestCheckResourceAttr(testIpv6DhcpServer, "interface", "bridge"),
							resource.TestCheckResourceAttr(testIpv6DhcpServer, "lease_time", "1m"),
							resource.TestCheckResourceAttr(testIpv6DhcpServer, "name", "test-dhcpv6"),
							resource.TestCheckResourceAttr(testIpv6DhcpServer, "preference", "128"),
						),
					},
				},
			})

		})
	}
}

func testAccIpv6DhcpServerConfig() string {
	return fmt.Sprintf(`%v

resource "routeros_ipv6_pool" "pool-0" {
  name          = "test-pool-0"
  prefix        = "2001:db8:40::/65"
  prefix_length = 128
}

resource "routeros_ipv6_pool" "pool-1" {
  name          = "test-pool-1"
  prefix        = "2001:db8:12::/65"
  prefix_length = 128
}

resource "routeros_ipv6_dhcp_server" "test" {
  address_pool  = routeros_ipv6_pool.pool-0.name
  prefix_pool   = routeros_ipv6_pool.pool-1.name
  address_lists = ["test-list-0", "test-list-1"]
  interface     = "bridge"
  lease_time    = "1m"
  name          = "test-dhcpv6"
  preference    = 128
}
`, providerConfig)
}

func TestResourceIpv6DhcpServerPoolDiffSuppress(t *testing.T) {
	res := ResourceIpv6DhcpServer()

	for _, field := range []string{"address_pool", "prefix_pool"} {
		s := res.Schema[field]
		if s == nil {
			t.Fatalf("missing schema field %s", field)
		}
		if !s.Optional || !s.Computed {
			t.Errorf("%s must be Optional and Computed (Optional=%v Computed=%v)", field, s.Optional, s.Computed)
		}
		if s.DiffSuppressFunc == nil {
			t.Errorf("%s must have DiffSuppressFunc set", field)
		}
	}

	t.Run("omitted_prefix_pool_in_config_preserved", func(t *testing.T) {
		state := &terraform.InstanceState{
			ID: "*1",
			Attributes: map[string]string{
				"id":           "*1",
				"name":         "dhcpv6-1",
				"interface":    "bridge",
				"address_pool": "pool-a",
				"prefix_pool":  "static-only",
			},
		}
		cfg := terraform.NewResourceConfigRaw(map[string]interface{}{
			"name":         "dhcpv6-1",
			"interface":    "bridge",
			"address_pool": "pool-a",
		})
		cfg.CtyValue = resourceDataWithRawConfig(res, "*1", map[string]cty.Value{
			"name":         cty.StringVal("dhcpv6-1"),
			"interface":    cty.StringVal("bridge"),
			"address_pool": cty.StringVal("pool-a"),
		}).GetRawConfig()

		diff, err := res.Diff(context.Background(), state, cfg, nil)
		if err != nil {
			t.Fatalf("diff failed: %v", err)
		}
		if diff != nil && !diff.Empty() {
			t.Fatalf("expected empty diff when prefix_pool is omitted from config, got %#v", diff.Attributes)
		}
	})

	t.Run("omitted_address_pool_in_config_preserved", func(t *testing.T) {
		state := &terraform.InstanceState{
			ID: "*1",
			Attributes: map[string]string{
				"id":           "*1",
				"name":         "dhcpv6-1",
				"interface":    "bridge",
				"address_pool": "static-only",
				"prefix_pool":  "pool-p",
			},
		}
		cfg := terraform.NewResourceConfigRaw(map[string]interface{}{
			"name":        "dhcpv6-1",
			"interface":   "bridge",
			"prefix_pool": "pool-p",
		})
		cfg.CtyValue = resourceDataWithRawConfig(res, "*1", map[string]cty.Value{
			"name":        cty.StringVal("dhcpv6-1"),
			"interface":   cty.StringVal("bridge"),
			"prefix_pool": cty.StringVal("pool-p"),
		}).GetRawConfig()

		diff, err := res.Diff(context.Background(), state, cfg, nil)
		if err != nil {
			t.Fatalf("diff failed: %v", err)
		}
		if diff != nil && !diff.Empty() {
			t.Fatalf("expected empty diff when address_pool is omitted from config, got %#v", diff.Attributes)
		}
	})

	t.Run("explicit_pool_change_plans_update", func(t *testing.T) {
		state := &terraform.InstanceState{
			ID: "*1",
			Attributes: map[string]string{
				"id":           "*1",
				"name":         "dhcpv6-1",
				"interface":    "bridge",
				"address_pool": "pool-a",
				"prefix_pool":  "pool-p1",
			},
		}
		cfg := terraform.NewResourceConfigRaw(map[string]interface{}{
			"name":         "dhcpv6-1",
			"interface":    "bridge",
			"address_pool": "pool-a2",
			"prefix_pool":  "pool-p2",
		})
		cfg.CtyValue = resourceDataWithRawConfig(res, "*1", map[string]cty.Value{
			"name":         cty.StringVal("dhcpv6-1"),
			"interface":    cty.StringVal("bridge"),
			"address_pool": cty.StringVal("pool-a2"),
			"prefix_pool":  cty.StringVal("pool-p2"),
		}).GetRawConfig()

		diff, err := res.Diff(context.Background(), state, cfg, nil)
		if err != nil {
			t.Fatalf("diff failed: %v", err)
		}
		if diff == nil || diff.Attributes["address_pool"] == nil || diff.Attributes["prefix_pool"] == nil {
			t.Fatalf("expected diffs on both address_pool and prefix_pool, got %#v", diff)
		}
	})
}
