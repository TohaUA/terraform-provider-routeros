package routeros

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testDatasourceRoutingBgpSessionEmpty = "data.routeros_routing_bgp_session.empty"
const testDatasourceRoutingBgpSessionAll = "data.routeros_routing_bgp_session.all"

type mockBgpSessionClient struct {
	transport TransportType
	rows      []MikrotikItem
	queries   [][]string
}

func (c *mockBgpSessionClient) GetExtraParams() *ExtraParams { return &ExtraParams{} }
func (c *mockBgpSessionClient) GetTransport() TransportType  { return c.transport }
func (c *mockBgpSessionClient) SendRequest(method crudMethod, u *URL, item MikrotikItem, result interface{}) error {
	if method != crudRead {
		return fmt.Errorf("unexpected %v call", method)
	}
	c.queries = append(c.queries, slices.Clone(u.Query))
	*result.(*[]MikrotikItem) = slices.Clone(c.rows)
	return nil
}

func TestDatasourceRoutingBgpSessionUnit(t *testing.T) {
	ds := DatasourceRoutingBgpSession()

	for _, transport := range []TransportType{TransportREST, TransportAPI} {
		name := "REST"
		if transport == TransportAPI {
			name = "API"
		}
		t.Run(name+"/empty", func(t *testing.T) {
			client := &mockBgpSessionClient{transport: transport}
			d := ds.TestResourceData()
			diags := ds.ReadContext(context.Background(), d, client)
			if diags.HasError() {
				t.Fatalf("unexpected error: %v", diags)
			}
			if d.Id() == "" {
				t.Fatal("expected non-empty datasource id")
			}
			sessions := d.Get("sessions").([]interface{})
			if len(sessions) != 0 {
				t.Fatalf("expected 0 sessions, got %d", len(sessions))
			}
		})

		t.Run(name+"/populated", func(t *testing.T) {
			client := &mockBgpSessionClient{
				transport: transport,
				rows: []MikrotikItem{
					{
						".id":              "*1",
						"name":             "peer-up-1",
						"established":      "true",
						"disabled":         "false",
						"inactive":         "false",
						"dynamic":          "false",
						"remote.address":   "192.0.2.2",
						"remote.as":        "65552",
						"remote.id":        "192.0.2.2",
						"remote.hold-time": "3m",
						"local.address":    "192.0.2.1",
						"local.as":         "65551",
						"local.id":         "192.0.2.1",
						"local.role":       "ebgp",
						"uptime":           "1h2m3s",
						"prefix-count":     "42",
						"keepalive-time":   "1m",
						"routing-table":    "main",
						"vrf":              "main",
						"comment":          "upstream",
					},
					{
						".id":            "*2",
						"name":           "peer-down-1",
						"established":    "false",
						"remote.address": "192.0.2.9",
						"prefix-count":   "",
					},
				},
			}
			d := ds.TestResourceData()
			if err := d.Set("filter", map[string]interface{}{"established": "true"}); err != nil {
				t.Fatalf("set filter: %v", err)
			}
			diags := ds.ReadContext(context.Background(), d, client)
			if len(diags) != 0 {
				t.Fatalf("expected zero diagnostics, got %+v", diags)
			}
			sessions := d.Get("sessions").([]interface{})
			if len(sessions) != 2 {
				t.Fatalf("expected 2 sessions, got %d", len(sessions))
			}
			s0 := sessions[0].(map[string]interface{})
			if s0["name"] != "peer-up-1" || s0["established"] != true || s0["remote_as"] != 65552 || s0["prefix_count"] != 42 || s0["remote_id"] != "192.0.2.2" {
				t.Fatalf("unexpected decoded first session: %+v", s0)
			}
			s1 := sessions[1].(map[string]interface{})
			if s1["established"] != false || s1["prefix_count"] != 0 {
				t.Fatalf("unexpected decoded down session: %+v", s1)
			}
		})
	}
}

func TestAccDatasourceRoutingBgpSession_basic(t *testing.T) {
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				Steps: []resource.TestStep{
					{
						Config: testAccDatasourceRoutingBgpSessionEmptyConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testDatasourceRoutingBgpSessionEmpty),
							resource.TestCheckResourceAttr(testDatasourceRoutingBgpSessionEmpty, "sessions.#", "0"),
						),
					},
					{
						Config: testAccDatasourceRoutingBgpSessionLiveConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testDatasourceRoutingBgpSessionAll),
						),
					},
				},
			})
		})
	}
}

func testAccDatasourceRoutingBgpSessionEmptyConfig() string {
	return providerConfig + `
data "routeros_routing_bgp_session" "empty" {
  filter = {
    name = "nonexistent-bgp-session"
  }
}
`
}

func testAccDatasourceRoutingBgpSessionLiveConfig() string {
	return providerConfig + `
resource "routeros_interface_bridge" "bgp_br" {
  name = "bgp-ds-test-br"
}

resource "routeros_ip_address" "bgp_a" {
  address   = "192.0.2.1/30"
  interface = routeros_interface_bridge.bgp_br.name
}

resource "routeros_ip_address" "bgp_b" {
  address   = "192.0.2.2/30"
  interface = routeros_interface_bridge.bgp_br.name
}

resource "routeros_routing_bgp_instance" "a" {
  name = "bgp-ds-inst-a"
  as   = "65551"
}

resource "routeros_routing_bgp_instance" "b" {
  name = "bgp-ds-inst-b"
  as   = "65552"
}

resource "routeros_routing_bgp_connection" "a" {
  instance = routeros_routing_bgp_instance.a.name
  name     = "bgp-ds-peer-a"
  as       = "65551"
  connect  = true
  listen   = true
  local {
    address = "192.0.2.1"
    role    = "ebgp"
  }
  remote {
    address = "192.0.2.2"
    as      = "65552"
  }
  depends_on = [routeros_ip_address.bgp_a, routeros_ip_address.bgp_b]
}

resource "routeros_routing_bgp_connection" "b" {
  instance = routeros_routing_bgp_instance.b.name
  name     = "bgp-ds-peer-b"
  as       = "65552"
  connect  = true
  listen   = true
  local {
    address = "192.0.2.2"
    role    = "ebgp"
  }
  remote {
    address = "192.0.2.1"
    as      = "65551"
  }
  depends_on = [routeros_ip_address.bgp_a, routeros_ip_address.bgp_b]
}

data "routeros_routing_bgp_session" "all" {
  depends_on = [routeros_routing_bgp_connection.a, routeros_routing_bgp_connection.b]
}
`
}
