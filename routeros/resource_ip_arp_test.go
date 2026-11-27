package routeros

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testIpArpEther2 = "routeros_ip_arp.arp_ether2"
const testIpArpEther3 = "routeros_ip_arp.arp_ether3"

type mockIpArpClient struct {
	transport TransportType
	rows      []MikrotikItem
	calls     []crudMethod
}

func (c *mockIpArpClient) GetExtraParams() *ExtraParams { return &ExtraParams{} }
func (c *mockIpArpClient) GetTransport() TransportType  { return c.transport }

func (c *mockIpArpClient) SendRequest(method crudMethod, u *URL, item MikrotikItem, result interface{}) error {
	c.calls = append(c.calls, method)
	if method != crudRead {
		return fmt.Errorf("unexpected %v call in test", method)
	}

	out := []MikrotikItem{}
	for _, row := range c.rows {
		matched := true
		for _, q := range u.Query {
			q = strings.TrimPrefix(q, "?=")
			q = strings.TrimPrefix(q, "?")
			k, v, ok := strings.Cut(q, "=")
			if !ok {
				continue
			}
			if row[k] != v {
				matched = false
				break
			}
		}
		if matched {
			out = append(out, row)
		}
	}
	*result.(*[]MikrotikItem) = out
	return nil
}

func TestIpArpValidateStaticRow(t *testing.T) {
	static := MikrotikItem{".id": "*1", "address": "192.0.2.50", "interface": "ether2", "dynamic": "false", "DHCP": "false"}
	if err := validateStaticArpRow(static); err != nil {
		t.Fatalf("expected static non-DHCP row to pass, got %v", err)
	}

	for _, tc := range []struct {
		name string
		item MikrotikItem
	}{
		{name: "dynamic true", item: MikrotikItem{".id": "*2", "address": "192.0.2.51", "interface": "ether2", "dynamic": "true", "DHCP": "false"}},
		{name: "uppercase DHCP true", item: MikrotikItem{".id": "*3", "address": "192.0.2.52", "interface": "ether2", "dynamic": "false", "DHCP": "true"}},
		{name: "lowercase dhcp yes", item: MikrotikItem{".id": "*4", "address": "192.0.2.53", "interface": "ether2", "dynamic": "no", "dhcp": "yes"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateStaticArpRow(tc.item); err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestIpArpReadHandlesUppercaseDHCP(t *testing.T) {
	res := ResourceIpArp()
	d := res.TestResourceData()
	item := MikrotikItem{
		".id":         "*1",
		"DHCP":        "false",
		"address":     "192.0.2.50",
		"complete":    "true",
		"disabled":    "false",
		"dynamic":     "false",
		"interface":   "ether2",
		"invalid":     "false",
		"mac-address": "00:11:22:33:44:55",
		"published":   "false",
		"status":      "permanent",
	}

	diags := MikrotikResourceDataToTerraform(item, res.Schema, d)
	if len(diags) != 0 {
		t.Fatalf("expected zero diagnostics, got %+v", diags)
	}
	if got := d.Get("dhcp").(bool); got {
		t.Fatalf("expected dhcp=false, got true")
	}
	if got := d.Get("status").(string); got != "permanent" {
		t.Fatalf("expected status=permanent, got %q", got)
	}
}

func TestIpArpImportResolution(t *testing.T) {
	rows := []MikrotikItem{
		{".id": "*1", "address": "192.0.2.50", "interface": "ether2", "mac-address": "00:11:22:33:44:55", "dynamic": "false", "DHCP": "false"},
		{".id": "*2", "address": "192.0.2.50", "interface": "ether3", "mac-address": "00:11:22:33:44:66", "dynamic": "false", "DHCP": "false"},
		{".id": "*3", "address": "192.0.2.60", "interface": "ether2", "dynamic": "true", "DHCP": "false"},
		{".id": "*4", "address": "192.0.2.70", "interface": "ether2", "dynamic": "false", "DHCP": "true"},
	}

	for _, transport := range []TransportType{TransportREST, TransportAPI} {
		name := "REST"
		if transport == TransportAPI {
			name = "API"
		}
		t.Run(name, func(t *testing.T) {
			client := &mockIpArpClient{transport: transport, rows: rows}
			res := ResourceIpArp()

			for _, tc := range []struct {
				id      string
				wantID  string
				wantErr string
			}{
				{id: "*1", wantID: "*1"},
				{id: "192.0.2.50,ether2", wantID: "*1"},
				{id: "address=192.0.2.50,interface=ether3", wantID: "*2"},
				{id: "192.0.2.50", wantErr: "ambiguous import ID"},
				{id: "address=192.0.2.50", wantErr: "ambiguous import ID"},
				{id: "192.0.2.99,ether2", wantErr: "no /ip/arp entry found"},
				{id: "*3", wantErr: "dynamic"},
				{id: "192.0.2.60,ether2", wantErr: "dynamic"},
				{id: "*4", wantErr: "DHCP"},
				{id: "192.0.2.70,ether2", wantErr: "DHCP"},
				{id: "   ", wantErr: "empty import ID"},
			} {
				d := res.TestResourceData()
				d.SetId(tc.id)
				imported, err := res.Importer.StateContext(context.Background(), d, client)
				if tc.wantErr != "" {
					if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
						t.Fatalf("import %q: expected error containing %q, got imported=%v err=%v", tc.id, tc.wantErr, imported, err)
					}
					continue
				}
				if err != nil {
					t.Fatalf("import %q: unexpected error: %v", tc.id, err)
				}
				if len(imported) != 1 || imported[0].Id() != tc.wantID {
					t.Fatalf("import %q: got %v, want ID %q", tc.id, imported, tc.wantID)
				}
			}
		})
	}
}

func TestIpArpReadUpdateDeleteRejectDynamicOrDHCP(t *testing.T) {
	res := ResourceIpArp()
	for _, tc := range []struct {
		name string
		row  MikrotikItem
	}{
		{name: "dynamic", row: MikrotikItem{".id": "*3", "address": "192.0.2.60", "interface": "ether2", "dynamic": "true", "DHCP": "false"}},
		{name: "dhcp", row: MikrotikItem{".id": "*4", "address": "192.0.2.70", "interface": "ether2", "dynamic": "false", "DHCP": "true"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &mockIpArpClient{transport: TransportREST, rows: []MikrotikItem{tc.row}}
			d := res.TestResourceData()
			d.SetId(tc.row[".id"])

			if diags := res.ReadContext(context.Background(), d, client); !diags.HasError() {
				t.Fatal("ReadContext: expected error for non-static row")
			}
			if diags := res.UpdateContext(context.Background(), d, client); !diags.HasError() {
				t.Fatal("UpdateContext: expected error for non-static row")
			}
			if diags := res.DeleteContext(context.Background(), d, client); !diags.HasError() {
				t.Fatal("DeleteContext: expected error for non-static row")
			}
			for _, m := range client.calls {
				if m != crudRead {
					t.Fatalf("expected only crudRead calls when row rejected, saw %v", client.calls)
				}
			}
		})
	}
}

func TestAccIpArpTest_basic(t *testing.T) {
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/ip/arp", "routeros_ip_arp"),
				Steps: []resource.TestStep{
					{
						Config: testAccIpArpConfigInitial(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testIpArpEther2),
							resource.TestCheckResourceAttr(testIpArpEther2, "address", "192.0.2.50"),
							resource.TestCheckResourceAttr(testIpArpEther2, "interface", "ether2"),
							resource.TestCheckResourceAttr(testIpArpEther2, "mac_address", "00:11:22:33:44:55"),
							resource.TestCheckResourceAttr(testIpArpEther2, "comment", "static arp ether2"),
							resource.TestCheckResourceAttr(testIpArpEther2, "dynamic", "false"),
							resource.TestCheckResourceAttr(testIpArpEther2, "dhcp", "false"),
							testResourcePrimaryInstanceId(testIpArpEther3),
							resource.TestCheckResourceAttr(testIpArpEther3, "address", "192.0.2.50"),
							resource.TestCheckResourceAttr(testIpArpEther3, "interface", "ether3"),
							resource.TestCheckResourceAttr(testIpArpEther3, "mac_address", "00:11:22:33:44:66"),
							resource.TestCheckResourceAttr(testIpArpEther3, "dynamic", "false"),
							resource.TestCheckResourceAttr(testIpArpEther3, "dhcp", "false"),
						),
					},
					{
						Config: testAccIpArpConfigUpdated(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testIpArpEther2),
							resource.TestCheckResourceAttr(testIpArpEther2, "comment", "static arp ether2 updated"),
							resource.TestCheckResourceAttr(testIpArpEther2, "disabled", "true"),
							resource.TestCheckResourceAttr(testIpArpEther2, "published", "true"),
							resource.TestCheckResourceAttr(testIpArpEther3, "address", "192.0.2.50"),
							resource.TestCheckResourceAttr(testIpArpEther3, "interface", "ether3"),
						),
					},
					{
						ResourceName:      testIpArpEther2,
						ImportState:       true,
						ImportStateId:     "192.0.2.50,ether2",
						ImportStateVerify: true,
					},
					{
						ResourceName:  testIpArpEther2,
						ImportState:   true,
						ImportStateId: "192.0.2.50",
						ExpectError:   regexp.MustCompile("ambiguous import ID"),
					},
					{
						ResourceName:      testIpArpEther3,
						ImportState:       true,
						ImportStateVerify: true,
					},
				},
			})
		})
	}
}

func testAccIpArpConfigInitial() string {
	return providerConfig + `
resource "routeros_ip_arp" "arp_ether2" {
  address     = "192.0.2.50"
  interface   = "ether2"
  mac_address = "00:11:22:33:44:55"
  comment     = "static arp ether2"
}

resource "routeros_ip_arp" "arp_ether3" {
  address     = "192.0.2.50"
  interface   = "ether3"
  mac_address = "00:11:22:33:44:66"
  comment     = "static arp ether3"
}
`
}

func testAccIpArpConfigUpdated() string {
	return providerConfig + `
resource "routeros_ip_arp" "arp_ether2" {
  address     = "192.0.2.50"
  interface   = "ether2"
  mac_address = "00:11:22:33:44:aa"
  comment     = "static arp ether2 updated"
  disabled    = true
  published   = true
}

resource "routeros_ip_arp" "arp_ether3" {
  address     = "192.0.2.50"
  interface   = "ether3"
  mac_address = "00:11:22:33:44:66"
  comment     = "static arp ether3"
}
`
}
