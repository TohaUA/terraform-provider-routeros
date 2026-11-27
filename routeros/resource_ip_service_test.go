package routeros

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

type ipServiceMockClient struct {
	transport TransportType
	rows      []MikrotikItem
	readErr   error
	queries   [][]string
	paths     []string
	filter    bool
}

func (c *ipServiceMockClient) GetExtraParams() *ExtraParams { return &ExtraParams{} }
func (c *ipServiceMockClient) GetTransport() TransportType  { return c.transport }

func (c *ipServiceMockClient) SendRequest(method crudMethod, url *URL, item MikrotikItem, result interface{}) error {
	if method != crudRead || item != nil {
		return fmt.Errorf("unexpected ip service request: method=%v path=%s item=%v", method, url.Path, item)
	}
	c.paths = append(c.paths, url.Path)
	q := append([]string(nil), url.Query...)
	c.queries = append(c.queries, q)
	if c.readErr != nil {
		return c.readErr
	}

	out := result.(*[]MikrotikItem)
	if !c.filter {
		*out = append(*out, c.rows...)
		return nil
	}

	for _, row := range c.rows {
		if ipServiceRowMatches(row, q) {
			*out = append(*out, row)
		}
	}
	return nil
}

func ipServiceRowMatches(row MikrotikItem, query []string) bool {
	for _, cond := range query {
		cond = strings.TrimPrefix(cond, "?=")
		parts := strings.SplitN(cond, "=", 2)
		if len(parts) != 2 {
			return false
		}
		if row[parts[0]] != parts[1] {
			return false
		}
	}
	return true
}

func TestIpServiceNumbersValidation(t *testing.T) {
	res := ResourceIpService()
	v := res.Schema["numbers"].ValidateDiagFunc
	if v == nil {
		t.Fatal("numbers ValidateDiagFunc is nil")
	}

	for _, valid := range []string{"api", "api-ssl", "ftp", "reverse-proxy", "ssh", "telnet", "winbox", "www", "www-ssl"} {
		if diags := v(valid, cty.GetAttrPath("numbers")); diags.HasError() {
			t.Errorf("numbers=%q unexpectedly failed validation: %v", valid, diags)
		}
	}

	if diags := v("not-a-service", cty.GetAttrPath("numbers")); !diags.HasError() {
		t.Error("numbers=\"not-a-service\" unexpectedly passed validation")
	}
}

func TestResolveIpServiceId(t *testing.T) {
	staticSSH := MikrotikItem{".id": "*1", "name": "ssh", "dynamic": "false", "port": "22"}
	dynamicSSH := MikrotikItem{".id": "*10", "name": "ssh", "dynamic": "true", "port": "22"}

	for _, transport := range []TransportType{TransportREST, TransportAPI} {
		t.Run(fmt.Sprint(transport), func(t *testing.T) {
			wantQuery := []string{"name=ssh", "dynamic=false"}
			if transport == TransportAPI {
				wantQuery = []string{"?=name=ssh", "?=dynamic=false"}
			}

			t.Run("not found", func(t *testing.T) {
				client := &ipServiceMockClient{transport: transport, rows: []MikrotikItem{dynamicSSH}, filter: true}
				_, err := resolveIpServiceId("ssh", "/ip/service", client)
				if err == nil || !strings.Contains(err.Error(), "ip service not found") {
					t.Fatalf("got err=%v, want ip service not found", err)
				}
				if len(client.queries) != 1 || !reflect.DeepEqual(client.queries[0], wantQuery) {
					t.Fatalf("queries=%v, want [%v]", client.queries, wantQuery)
				}
			})

			t.Run("single match filters out dynamic row", func(t *testing.T) {
				client := &ipServiceMockClient{
					transport: transport,
					rows:      []MikrotikItem{staticSSH, dynamicSSH},
					filter:    true,
				}
				id, err := resolveIpServiceId("ssh", "/ip/service", client)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if id != "*1" {
					t.Fatalf("id = %q, want *1", id)
				}
				if len(client.queries) != 1 || !reflect.DeepEqual(client.queries[0], wantQuery) {
					t.Fatalf("queries=%v, want [%v]", client.queries, wantQuery)
				}
			})

			t.Run("ambiguous multiple non-dynamic rows", func(t *testing.T) {
				client := &ipServiceMockClient{
					transport: transport,
					rows: []MikrotikItem{
						{".id": "*1", "name": "ssh", "dynamic": "false"},
						{".id": "*2", "name": "ssh", "dynamic": "false"},
					},
					filter: true,
				}
				_, err := resolveIpServiceId("ssh", "/ip/service", client)
				if err == nil || !strings.Contains(err.Error(), "ip service ambiguous") {
					t.Fatalf("got err=%v, want ip service ambiguous", err)
				}
			})

			t.Run("client error", func(t *testing.T) {
				client := &ipServiceMockClient{transport: transport, readErr: errors.New("boom")}
				_, err := resolveIpServiceId("ssh", "/ip/service", client)
				if err == nil || !strings.Contains(err.Error(), "boom") {
					t.Fatalf("got err=%v, want boom", err)
				}
			})
		})
	}
}

func TestResourceIpServiceRead(t *testing.T) {
	staticSSH := MikrotikItem{
		".id":      "*1",
		"name":     "ssh",
		"port":     "22",
		"disabled": "false",
		"dynamic":  "false",
		"invalid":  "false",
		"vrf":      "main",
	}
	dynamicSSH := MikrotikItem{
		".id":      "*1",
		"name":     "ssh",
		"port":     "22",
		"disabled": "false",
		"dynamic":  "true",
		"invalid":  "false",
		"vrf":      "main",
	}

	for _, transport := range []TransportType{TransportREST, TransportAPI} {
		t.Run(fmt.Sprint(transport), func(t *testing.T) {
			wantQuery := []string{".id=*1", "dynamic=false"}
			if transport == TransportAPI {
				wantQuery = []string{"?=.id=*1", "?=dynamic=false"}
			}

			t.Run("not found clears id", func(t *testing.T) {
				res := ResourceIpService()
				d := resourceDataWithRawConfig(res, "*1", nil)
				client := &ipServiceMockClient{
					transport: transport,
					rows:      []MikrotikItem{dynamicSSH},
					filter:    true,
				}
				diags := res.ReadContext(context.Background(), d, client)
				if diags.HasError() {
					t.Fatalf("unexpected diagnostics: %v", diags)
				}
				if d.Id() != "" {
					t.Fatalf("id = %q, want empty string", d.Id())
				}
				if len(client.queries) != 1 || !reflect.DeepEqual(client.queries[0], wantQuery) {
					t.Fatalf("queries=%v, want [%v]", client.queries, wantQuery)
				}
			})

			t.Run("single match", func(t *testing.T) {
				res := ResourceIpService()
				d := resourceDataWithRawConfig(res, "*1", nil)
				client := &ipServiceMockClient{
					transport: transport,
					rows:      []MikrotikItem{staticSSH, dynamicSSH},
					filter:    true,
				}
				diags := res.ReadContext(context.Background(), d, client)
				if diags.HasError() {
					t.Fatalf("unexpected diagnostics: %v", diags)
				}
				if d.Id() != "*1" {
					t.Fatalf("id = %q, want *1", d.Id())
				}
				if got := d.Get("name"); got != "ssh" {
					t.Fatalf("name = %v, want ssh", got)
				}
				if got := d.Get("port"); got != 22 {
					t.Fatalf("port = %v, want 22", got)
				}
				if len(client.queries) != 1 || !reflect.DeepEqual(client.queries[0], wantQuery) {
					t.Fatalf("queries=%v, want [%v]", client.queries, wantQuery)
				}
			})

			t.Run("ambiguous multiple rows", func(t *testing.T) {
				res := ResourceIpService()
				d := resourceDataWithRawConfig(res, "*1", nil)
				client := &ipServiceMockClient{
					transport: transport,
					rows:      []MikrotikItem{staticSSH, staticSSH},
					filter:    true,
				}
				diags := res.ReadContext(context.Background(), d, client)
				if !diags.HasError() {
					t.Fatal("expected error diagnostic for ambiguous rows, got none")
				}
				if !strings.Contains(diags[0].Summary, "ip service ambiguous") {
					t.Fatalf("unexpected diagnostic summary: %q", diags[0].Summary)
				}
			})
		})
	}
}

func TestImportIpServiceState(t *testing.T) {
	res := ResourceIpService()
	rows := []MikrotikItem{
		{".id": "*2", "name": "ssh", "port": "22", "dynamic": "false"},
		{".id": "*20", "name": "ssh", "port": "22", "dynamic": "true"},
		{".id": "*6", "name": "www-ssl", "port": "443", "tls-version": "only-1.2", "dynamic": "false"},
	}

	t.Run("*N passthrough", func(t *testing.T) {
		d := res.TestResourceData()
		d.SetId("*6")
		client := &ipServiceMockClient{transport: TransportREST, rows: rows, filter: true}
		out, err := res.Importer.StateContext(context.Background(), d, client)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out) != 1 || out[0].Id() != "*6" {
			t.Fatalf("imported state = %v, want id *6", out)
		}
		if len(client.queries) != 0 {
			t.Fatalf("expected no RouterOS queries for *N passthrough, got %v", client.queries)
		}
	})

	t.Run("bare service name", func(t *testing.T) {
		d := res.TestResourceData()
		d.SetId("ssh")
		client := &ipServiceMockClient{transport: TransportREST, rows: rows, filter: true}
		out, err := res.Importer.StateContext(context.Background(), d, client)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out) != 1 || out[0].Id() != "*2" {
			t.Fatalf("imported id = %q, want *2", out[0].Id())
		}
		wantQuery := []string{"name=ssh", "dynamic=false"}
		if len(client.queries) != 1 || !reflect.DeepEqual(client.queries[0], wantQuery) {
			t.Fatalf("queries=%v, want [%v]", client.queries, wantQuery)
		}
	})

	t.Run("field=value with snake_case field", func(t *testing.T) {
		d := res.TestResourceData()
		d.SetId("tls_version=only-1.2")
		client := &ipServiceMockClient{transport: TransportREST, rows: rows, filter: true}
		out, err := res.Importer.StateContext(context.Background(), d, client)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out) != 1 || out[0].Id() != "*6" {
			t.Fatalf("imported id = %q, want *6", out[0].Id())
		}
		wantQuery := []string{"tls-version=only-1.2", "dynamic=false"}
		if len(client.queries) != 1 || !reflect.DeepEqual(client.queries[0], wantQuery) {
			t.Fatalf("queries=%v, want [%v]", client.queries, wantQuery)
		}
	})

	for _, invalid := range []string{"", "=ssh", "name=", "="} {
		t.Run("invalid_"+invalid, func(t *testing.T) {
			d := res.TestResourceData()
			d.SetId(invalid)
			client := &ipServiceMockClient{transport: TransportREST, rows: rows, filter: true}
			_, err := res.Importer.StateContext(context.Background(), d, client)
			if err == nil {
				t.Fatalf("expected error for import id %q, got nil", invalid)
			}
			if len(client.queries) != 0 {
				t.Fatalf("expected no queries for invalid import id %q, got %v", invalid, client.queries)
			}
		})
	}
}

const testIpServiceAddress = "routeros_ip_service.telnet"

func TestAccIpServiceTest_basic(t *testing.T) {
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
						Config: testAccIpServiceConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testIpServiceAddress),
							resource.TestCheckResourceAttr(testIpServiceAddress, "name", "telnet"),
						),
					},
				},
			})
		})
	}
}

func testAccIpServiceConfig() string {
	return providerConfig + `

resource "routeros_ip_service" "telnet" {
	numbers  = "telnet"
	disabled = true
	port     = 23
}
`
}
