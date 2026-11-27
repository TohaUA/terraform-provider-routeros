package routeros

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/go-routeros/routeros/v3/proto"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testDatasourceInterfaceBridgeHostEmpty = "data.routeros_interface_bridge_host.empty"
const testDatasourceInterfaceBridgeHostAll = "data.routeros_interface_bridge_host.all"

func assertPopulatedBridgeHostRows(t *testing.T, hosts []interface{}) {
	t.Helper()
	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hosts))
	}

	h0 := hosts[0].(map[string]interface{})
	if h0["id"] != "*1" || h0["bridge"] != "bridge1" || h0["interface"] != "ether2" ||
		h0["mac_address"] != "12:34:56:78:9A:BC" || h0["vid"] != 10 || h0["age"] != "3s" ||
		h0["dynamic"] != true || h0["local"] != false || h0["external"] != false ||
		h0["disabled"] != false || h0["invalid"] != false || h0["comment"] != "access-port" {
		t.Fatalf("unexpected tagged dynamic host row: %+v", h0)
	}

	h1 := hosts[1].(map[string]interface{})
	if h1["id"] != "*2" || h1["bridge"] != "bridge1" || h1["interface"] != "bridge1" ||
		h1["mac_address"] != "AA:BB:CC:DD:EE:FF" || h1["vid"] != 0 ||
		h1["local"] != true || h1["dynamic"] != false {
		t.Fatalf("unexpected untagged local host row (with on-interface fallback): %+v", h1)
	}
}

func TestDatasourceInterfaceBridgeHostUnit(t *testing.T) {
	res := DatasourceInterfaceBridgeHost()

	t.Run("REST/empty", func(t *testing.T) {
		client := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/rest/interface/bridge/host" {
				t.Errorf("unexpected REST request %s %s", r.Method, r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		})
		d := res.TestResourceData()
		diags := res.ReadContext(context.Background(), d, client)
		if len(diags) != 0 {
			t.Fatalf("unexpected diagnostics: %+v", diags)
		}
		if d.Id() == "" {
			t.Fatal("expected non-empty datasource id")
		}
		if hosts := d.Get("hosts").([]interface{}); len(hosts) != 0 {
			t.Fatalf("expected 0 hosts, got %d", len(hosts))
		}
	})

	t.Run("API/empty", func(t *testing.T) {
		var sentWords []string
		client := newMockAPICommandClient(t, func(words []string, w proto.Writer) {
			sentWords = words
			w.BeginSentence()
			w.WriteWord("!done")
			_ = w.EndSentence()
		})
		d := res.TestResourceData()
		diags := res.ReadContext(context.Background(), d, client)
		if len(diags) != 0 {
			t.Fatalf("unexpected diagnostics: %+v", diags)
		}
		if len(sentWords) == 0 || sentWords[0] != "/interface/bridge/host/print" {
			t.Fatalf("unexpected command words: %v", sentWords)
		}
		if d.Id() == "" {
			t.Fatal("expected non-empty datasource id")
		}
		if hosts := d.Get("hosts").([]interface{}); len(hosts) != 0 {
			t.Fatalf("expected 0 hosts, got %d", len(hosts))
		}
	})

	t.Run("REST/populated_filtered", func(t *testing.T) {
		var capturedQuery url.Values
		client := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
			capturedQuery = r.URL.Query()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{
					".id":"*1",
					"bridge":"bridge1",
					"interface":"ether2",
					"mac-address":"12:34:56:78:9A:BC",
					"vid":"10",
					"age":"3s",
					"local":"false",
					"dynamic":"true",
					"external":"false",
					"disabled":"false",
					"invalid":"false",
					"comment":"access-port"
				},
				{
					".id":"*2",
					"bridge":"bridge1",
					"on-interface":"bridge1",
					"mac-address":"AA:BB:CC:DD:EE:FF",
					"vid":"",
					"local":"true",
					"dynamic":"false"
				}
			]`))
		})
		d := res.TestResourceData()
		if err := d.Set("filter", map[string]interface{}{"bridge": "bridge1"}); err != nil {
			t.Fatalf("set filter: %v", err)
		}
		diags := res.ReadContext(context.Background(), d, client)
		if len(diags) != 0 {
			t.Fatalf("unexpected diagnostics: %+v", diags)
		}
		if capturedQuery.Get("bridge") != "bridge1" {
			t.Fatalf("unexpected REST filter query: %v", capturedQuery)
		}
		assertPopulatedBridgeHostRows(t, d.Get("hosts").([]interface{}))
	})

	t.Run("API/populated_filtered", func(t *testing.T) {
		var sentWords []string
		client := newMockAPICommandClient(t, func(words []string, w proto.Writer) {
			sentWords = words
			for _, row := range []map[string]string{
				{
					".id":         "*1",
					"bridge":      "bridge1",
					"interface":   "ether2",
					"mac-address": "12:34:56:78:9A:BC",
					"vid":         "10",
					"age":         "3s",
					"local":       "false",
					"dynamic":     "true",
					"external":    "false",
					"disabled":    "false",
					"invalid":     "false",
					"comment":     "access-port",
				},
				{
					".id":          "*2",
					"bridge":       "bridge1",
					"on-interface": "bridge1",
					"mac-address":  "AA:BB:CC:DD:EE:FF",
					"vid":          "",
					"local":        "true",
					"dynamic":      "false",
				},
			} {
				w.BeginSentence()
				w.WriteWord("!re")
				for k, v := range row {
					w.WriteWord("=" + k + "=" + v)
				}
				_ = w.EndSentence()
			}
			w.BeginSentence()
			w.WriteWord("!done")
			_ = w.EndSentence()
		})
		d := res.TestResourceData()
		if err := d.Set("filter", map[string]interface{}{"bridge": "bridge1"}); err != nil {
			t.Fatalf("set filter: %v", err)
		}
		diags := res.ReadContext(context.Background(), d, client)
		if len(diags) != 0 {
			t.Fatalf("unexpected diagnostics: %+v", diags)
		}
		if len(sentWords) == 0 || sentWords[0] != "/interface/bridge/host/print" {
			t.Fatalf("unexpected command: %v", sentWords)
		}
		if !slices.Contains(sentWords, "?=bridge=bridge1") {
			t.Fatalf("API command missing filter word: %v", sentWords)
		}
		assertPopulatedBridgeHostRows(t, d.Get("hosts").([]interface{}))
	})

	t.Run("REST/error", func(t *testing.T) {
		client := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":500,"message":"Internal Server Error","detail":"bridge host read failed"}`))
		})
		d := res.TestResourceData()
		diags := res.ReadContext(context.Background(), d, client)
		if !diags.HasError() {
			t.Fatal("expected error diagnostic on REST HTTP 500")
		}
	})

	t.Run("API/error", func(t *testing.T) {
		client := newMockAPICommandClient(t, func(words []string, w proto.Writer) {
			w.BeginSentence()
			w.WriteWord("!trap")
			w.WriteWord("=message=bridge host error")
			_ = w.EndSentence()
			w.BeginSentence()
			w.WriteWord("!done")
			_ = w.EndSentence()
		})
		d := res.TestResourceData()
		diags := res.ReadContext(context.Background(), d, client)
		if !diags.HasError() {
			t.Fatal("expected error diagnostic on API !trap")
		}
	})

	t.Run("REST/malformed_vid", func(t *testing.T) {
		client := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{".id":"*1","bridge":"bridge1","vid":"not-a-number"}]`))
		})
		d := res.TestResourceData()
		diags := res.ReadContext(context.Background(), d, client)
		if !diags.HasError() {
			t.Fatal("expected error diagnostic on malformed vid")
		}
	})
}

func TestAccDatasourceInterfaceBridgeHost_basic(t *testing.T) {
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
						Config: testAccDatasourceInterfaceBridgeHostEmptyConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testDatasourceInterfaceBridgeHostEmpty),
							resource.TestCheckResourceAttr(testDatasourceInterfaceBridgeHostEmpty, "hosts.#", "0"),
						),
					},
					{
						Config: testAccDatasourceInterfaceBridgeHostAllConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testDatasourceInterfaceBridgeHostAll),
						),
					},
				},
			})
		})
	}
}

func testAccDatasourceInterfaceBridgeHostEmptyConfig() string {
	return providerConfig + `
data "routeros_interface_bridge_host" "empty" {
  filter = {
    bridge = "nonexistent-bridge-host-ds"
  }
}
`
}

func testAccDatasourceInterfaceBridgeHostAllConfig() string {
	return providerConfig + `
data "routeros_interface_bridge_host" "all" {}
`
}
