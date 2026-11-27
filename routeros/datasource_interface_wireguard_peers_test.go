package routeros

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testDatasourceWgPeersEmpty = "data.routeros_interface_wireguard_peers.empty"
const testDatasourceWgPeersPopulated = "data.routeros_interface_wireguard_peers.populated"

type mockWgPeersClient struct {
	transport TransportType
	rows      []MikrotikItem
	queries   [][]string
}

func (c *mockWgPeersClient) GetExtraParams() *ExtraParams { return &ExtraParams{} }
func (c *mockWgPeersClient) GetTransport() TransportType  { return c.transport }
func (c *mockWgPeersClient) SendRequest(method crudMethod, u *URL, item MikrotikItem, result interface{}) error {
	if method != crudRead {
		return fmt.Errorf("unexpected %v call", method)
	}
	c.queries = append(c.queries, slices.Clone(u.Query))
	*result.(*[]MikrotikItem) = slices.Clone(c.rows)
	return nil
}

func TestDatasourceInterfaceWireguardPeersUnit(t *testing.T) {
	ds := DatasourceInterfaceWireguardPeers()
	peerSchema := ds.Schema["peers"].Elem.(*schema.Resource).Schema

	for _, forbidden := range []string{"private_key", "preshared_key"} {
		if _, ok := peerSchema[forbidden]; ok {
			t.Fatalf("schema must not expose %q", forbidden)
		}
	}
	for _, p := range wireguardPeerProplist {
		if p == "private-key" || p == "preshared-key" {
			t.Fatalf("wire projection must not request %q", p)
		}
	}

	u := &URL{
		Path:  "/interface/wireguard/peers",
		Query: []string{"interface=wg1", ".proplist=" + strings.Join(wireguardPeerProplist, ",")},
	}
	restURL := u.GetRestURL()
	if strings.Contains(restURL, "%2C") {
		t.Fatalf("expected unescaped commas in .proplist REST URL, got %q", restURL)
	}

	for _, transport := range []TransportType{TransportREST, TransportAPI} {
		name := "REST"
		if transport == TransportAPI {
			name = "API"
		}
		t.Run(name+"/empty", func(t *testing.T) {
			client := &mockWgPeersClient{transport: transport}
			d := ds.TestResourceData()
			diags := ds.ReadContext(context.Background(), d, client)
			if diags.HasError() {
				t.Fatalf("unexpected error: %v", diags)
			}
			peers := d.Get("peers").([]interface{})
			if len(peers) != 0 {
				t.Fatalf("expected 0 peers, got %d", len(peers))
			}
			if len(client.queries) != 1 || !slices.ContainsFunc(client.queries[0], func(q string) bool {
				return strings.Contains(q, ".proplist=")
			}) {
				t.Fatalf("expected .proplist query parameter, got %+v", client.queries)
			}
		})

		t.Run(name+"/populated", func(t *testing.T) {
			client := &mockWgPeersClient{
				transport: transport,
				rows: []MikrotikItem{
					{
						".id":                      "*1",
						"interface":                "wg-ds",
						"name":                     "peer1",
						"public-key":               "QxC+CTcrDdU5+ny0+2ChUH3NegTrwoVCv53TllI5T0I=",
						"allowed-address":          "10.0.0.2/32,10.0.1.0/24",
						"endpoint-address":         "198.51.100.2",
						"endpoint-port":            "51820",
						"current-endpoint-address": "198.51.100.2",
						"current-endpoint-port":    "51820",
						"last-handshake":           "12s",
						"persistent-keepalive":     "25s",
						"rx":                       "1024",
						"tx":                       "2048",
						"disabled":                 "false",
						"dynamic":                  "false",
						"is-responder":             "true",
						"comment":                  "remote site",
						"private-key":              "MUST_BE_DROPPED",
						"preshared-key":            "MUST_BE_DROPPED",
					},
					{
						".id":            "*2",
						"interface":      "wg-ds",
						"name":           "peer-never-handshaked",
						"public-key":     "gLP306E2BCZBeyZ0ILrS5Ubdg4VjkFYiWkg7HpKYM10=",
						"last-handshake": "",
					},
				},
			}
			d := ds.TestResourceData()
			if err := d.Set("filter", map[string]interface{}{"interface": "wg-ds"}); err != nil {
				t.Fatalf("set filter: %v", err)
			}
			diags := ds.ReadContext(context.Background(), d, client)
			if len(diags) != 0 {
				t.Fatalf("expected zero diagnostics, got %+v", diags)
			}
			peers := d.Get("peers").([]interface{})
			if len(peers) != 2 {
				t.Fatalf("expected 2 peers, got %d", len(peers))
			}
			p0 := peers[0].(map[string]interface{})
			if p0["public_key"] != "QxC+CTcrDdU5+ny0+2ChUH3NegTrwoVCv53TllI5T0I=" ||
				p0["last_handshake"] != "12s" ||
				p0["current_endpoint_port"] != 51820 ||
				p0["is_responder"] != true {
				t.Fatalf("unexpected decoded peer0: %+v", p0)
			}
			if _, hasPriv := p0["private_key"]; hasPriv {
				t.Fatal("private_key leaked into decoded map")
			}
			if _, hasPsk := p0["preshared_key"]; hasPsk {
				t.Fatal("preshared_key leaked into decoded map")
			}
			p1 := peers[1].(map[string]interface{})
			if p1["last_handshake"] != "" {
				t.Fatalf("expected empty last_handshake for unhandshaked peer, got %q", p1["last_handshake"])
			}
		})
	}
}

func TestAccDatasourceInterfaceWireguardPeers_basic(t *testing.T) {
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
						Config: testAccDatasourceWgPeersEmptyConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testDatasourceWgPeersEmpty),
							resource.TestCheckResourceAttr(testDatasourceWgPeersEmpty, "peers.#", "0"),
						),
					},
					{
						Config: testAccDatasourceWgPeersPopulatedConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testDatasourceWgPeersPopulated),
							resource.TestCheckResourceAttr(testDatasourceWgPeersPopulated, "peers.#", "1"),
							resource.TestCheckResourceAttr(testDatasourceWgPeersPopulated, "peers.0.interface", "wg-ds-test"),
							resource.TestCheckResourceAttr(testDatasourceWgPeersPopulated, "peers.0.public_key", "QxC+CTcrDdU5+ny0+2ChUH3NegTrwoVCv53TllI5T0I="),
							resource.TestCheckResourceAttr(testDatasourceWgPeersPopulated, "peers.0.last_handshake", ""),
							resource.TestCheckNoResourceAttr(testDatasourceWgPeersPopulated, "peers.0.private_key"),
							resource.TestCheckNoResourceAttr(testDatasourceWgPeersPopulated, "peers.0.preshared_key"),
						),
					},
				},
			})
		})
	}
}

func testAccDatasourceWgPeersEmptyConfig() string {
	return providerConfig + `
data "routeros_interface_wireguard_peers" "empty" {
  filter = {
    interface = "nonexistent-wg-if"
  }
}
`
}

func testAccDatasourceWgPeersPopulatedConfig() string {
	return providerConfig + `
resource "routeros_interface_wireguard" "wg_ds" {
  name        = "wg-ds-test"
  listen_port = 13239
}

resource "routeros_interface_wireguard_peer" "wg_ds_peer" {
  interface                = routeros_interface_wireguard.wg_ds.name
  public_key               = "QxC+CTcrDdU5+ny0+2ChUH3NegTrwoVCv53TllI5T0I="
  preshared_key            = "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA="
  preshared_key_wo_version = 1
  allowed_address          = ["10.200.0.2/32"]
}

data "routeros_interface_wireguard_peers" "populated" {
  filter = {
    interface = routeros_interface_wireguard.wg_ds.name
  }
  depends_on = [routeros_interface_wireguard_peer.wg_ds_peer]
}
`
}
