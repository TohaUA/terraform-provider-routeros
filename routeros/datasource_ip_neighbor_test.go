package routeros

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/go-routeros/routeros/v3/proto"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testDatasourceIpNeighborEmpty = "data.routeros_ip_neighbor.empty"
const testDatasourceIpNeighborAll = "data.routeros_ip_neighbor.all"

func readAPICommandWords(r io.Reader) ([]string, error) {
	br := bufio.NewReader(r)
	var words []string
	for {
		b0, err := br.ReadByte()
		if err != nil {
			return nil, err
		}
		if b0 == 0 {
			return words, nil
		}
		var length int
		switch {
		case b0&0x80 == 0x00:
			length = int(b0)
		case b0&0xC0 == 0x80:
			b1, err := br.ReadByte()
			if err != nil {
				return nil, err
			}
			length = (int(b0&^0xC0) << 8) | int(b1)
		default:
			return nil, fmt.Errorf("unsupported length prefix 0x%02x", b0)
		}
		buf := make([]byte, length)
		if _, err := io.ReadFull(br, buf); err != nil {
			return nil, err
		}
		words = append(words, string(buf))
	}
}

func newMockAPICommandClient(t *testing.T, onCommand func(words []string, w proto.Writer)) *ApiClient {
	t.Helper()
	f := &fakeDialer{}
	pool := newAPIPool(f.dial, 2*time.Second, 1)
	session, err := pool.acquire()
	if err != nil {
		t.Fatalf("acquire mock api session: %v", err)
	}
	pool.release(session, true)

	peer := f.peer(0)
	t.Cleanup(func() { _ = peer.Close() })

	go func() {
		words, err := readAPICommandWords(peer)
		if err != nil || len(words) == 0 {
			return
		}
		w := proto.NewWriter(peer)
		onCommand(words, w)
	}()

	return &ApiClient{
		ctx:       context.Background(),
		Transport: TransportAPI,
		pool:      pool,
	}
}

func newMockRESTClient(t *testing.T, handler http.HandlerFunc) *RestClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return &RestClient{
		ctx:       context.Background(),
		HostURL:   srv.URL,
		Username:  "admin",
		Password:  "password",
		Transport: TransportREST,
		Client:    srv.Client(),
	}
}

func assertPopulatedNeighborRow(t *testing.T, n map[string]interface{}) {
	t.Helper()
	expected := map[string]string{
		"id":                  "*1",
		"interface":           "ether2",
		"address":             "192.0.2.20",
		"address4":            "192.0.2.20",
		"address6":            "fe80::1",
		"mac_address":         "4C:5E:0C:12:34:56",
		"identity":            "sw1",
		"platform":            "MikroTik",
		"version":             "7.25.2 (stable)",
		"board":               "CRS326-24G-2S+",
		"interface_name":      "ether1",
		"uptime":              "3d4h5m",
		"age":                 "12s",
		"software_id":         "ABCD-1234",
		"discovered_by":       "mndp,lldp",
		"system_description":  "MikroTik RouterOS 7.25.2",
		"system_caps":         "bridge,router",
		"system_caps_enabled": "bridge,router",
	}
	for k, want := range expected {
		if got := n[k]; got != want {
			t.Fatalf("neighbor[%q] = %#v, want %q", k, got, want)
		}
	}
}

func TestDatasourceIpNeighborUnit(t *testing.T) {
	res := DatasourceIpNeighbor()

	t.Run("REST/empty", func(t *testing.T) {
		client := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/rest/ip/neighbor" {
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
		if neighbors := d.Get("neighbors").([]interface{}); len(neighbors) != 0 {
			t.Fatalf("expected 0 neighbors, got %d", len(neighbors))
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
		if len(sentWords) == 0 || sentWords[0] != "/ip/neighbor/print" {
			t.Fatalf("unexpected command words: %v", sentWords)
		}
		if d.Id() == "" {
			t.Fatal("expected non-empty datasource id")
		}
		if neighbors := d.Get("neighbors").([]interface{}); len(neighbors) != 0 {
			t.Fatalf("expected 0 neighbors, got %d", len(neighbors))
		}
	})

	t.Run("REST/populated_filtered", func(t *testing.T) {
		var capturedQuery url.Values
		client := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
			capturedQuery = r.URL.Query()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{
				".id":"*1",
				"interface":"ether2",
				"address":"192.0.2.20",
				"address4":"192.0.2.20",
				"address6":"fe80::1",
				"mac-address":"4C:5E:0C:12:34:56",
				"identity":"sw1",
				"platform":"MikroTik",
				"version":"7.25.2 (stable)",
				"board":"CRS326-24G-2S+",
				"interface-name":"ether1",
				"uptime":"3d4h5m",
				"age":"12s",
				"software-id":"ABCD-1234",
				"discovered-by":"mndp,lldp",
				"system-description":"MikroTik RouterOS 7.25.2",
				"system-caps":"bridge,router",
				"system-caps-enabled":"bridge,router"
			}]`))
		})
		d := res.TestResourceData()
		if err := d.Set("filter", map[string]interface{}{"interface": "ether2", "identity": "sw1"}); err != nil {
			t.Fatalf("set filter: %v", err)
		}
		diags := res.ReadContext(context.Background(), d, client)
		if len(diags) != 0 {
			t.Fatalf("unexpected diagnostics: %+v", diags)
		}
		if capturedQuery.Get("interface") != "ether2" || capturedQuery.Get("identity") != "sw1" {
			t.Fatalf("unexpected REST filter query: %v", capturedQuery)
		}
		neighbors := d.Get("neighbors").([]interface{})
		if len(neighbors) != 1 {
			t.Fatalf("expected 1 neighbor, got %d", len(neighbors))
		}
		assertPopulatedNeighborRow(t, neighbors[0].(map[string]interface{}))
	})

	t.Run("API/populated_filtered", func(t *testing.T) {
		var sentWords []string
		client := newMockAPICommandClient(t, func(words []string, w proto.Writer) {
			sentWords = words
			w.BeginSentence()
			w.WriteWord("!re")
			for k, v := range map[string]string{
				".id":                 "*1",
				"interface":           "ether2",
				"address":             "192.0.2.20",
				"address4":            "192.0.2.20",
				"address6":            "fe80::1",
				"mac-address":         "4C:5E:0C:12:34:56",
				"identity":            "sw1",
				"platform":            "MikroTik",
				"version":             "7.25.2 (stable)",
				"board":               "CRS326-24G-2S+",
				"interface-name":      "ether1",
				"uptime":              "3d4h5m",
				"age":                 "12s",
				"software-id":         "ABCD-1234",
				"discovered-by":       "mndp,lldp",
				"system-description":  "MikroTik RouterOS 7.25.2",
				"system-caps":         "bridge,router",
				"system-caps-enabled": "bridge,router",
			} {
				if v != "" {
					w.WriteWord("=" + k + "=" + v)
				}
			}
			_ = w.EndSentence()
			w.BeginSentence()
			w.WriteWord("!done")
			_ = w.EndSentence()
		})
		d := res.TestResourceData()
		if err := d.Set("filter", map[string]interface{}{"interface": "ether2", "identity": "sw1"}); err != nil {
			t.Fatalf("set filter: %v", err)
		}
		diags := res.ReadContext(context.Background(), d, client)
		if len(diags) != 0 {
			t.Fatalf("unexpected diagnostics: %+v", diags)
		}
		if len(sentWords) == 0 || sentWords[0] != "/ip/neighbor/print" {
			t.Fatalf("unexpected command: %v", sentWords)
		}
		if !slices.Contains(sentWords, "?=interface=ether2") || !slices.Contains(sentWords, "?=identity=sw1") {
			t.Fatalf("API command missing filter words: %v", sentWords)
		}
		neighbors := d.Get("neighbors").([]interface{})
		if len(neighbors) != 1 {
			t.Fatalf("expected 1 neighbor, got %d", len(neighbors))
		}
		assertPopulatedNeighborRow(t, neighbors[0].(map[string]interface{}))
	})

	t.Run("REST/error", func(t *testing.T) {
		client := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":500,"message":"Internal Server Error","detail":"neighbor read failed"}`))
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
			w.WriteWord("=message=neighbor table error")
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
}

func TestAccDatasourceIpNeighbor_basic(t *testing.T) {
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
						Config: testAccDatasourceIpNeighborEmptyConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testDatasourceIpNeighborEmpty),
							resource.TestCheckResourceAttr(testDatasourceIpNeighborEmpty, "neighbors.#", "0"),
						),
					},
					{
						Config: testAccDatasourceIpNeighborAllConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testDatasourceIpNeighborAll),
						),
					},
				},
			})
		})
	}
}

func testAccDatasourceIpNeighborEmptyConfig() string {
	return providerConfig + `
data "routeros_ip_neighbor" "empty" {
  filter = {
    identity = "nonexistent-neighbor-identity"
  }
}
`
}

func testAccDatasourceIpNeighborAllConfig() string {
	return providerConfig + `
data "routeros_ip_neighbor" "all" {}
`
}
