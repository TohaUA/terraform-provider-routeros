package routeros

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

type lockoutMockClient struct {
	transport TransportType
	extra     *ExtraParams
	readItem  MikrotikItem
	readList  []MikrotikItem
	readErr   error
	postErr   error
	calls     []crudMethod
	postItems []MikrotikItem
}

func (c *lockoutMockClient) GetExtraParams() *ExtraParams { return c.extra }
func (c *lockoutMockClient) GetTransport() TransportType  { return c.transport }

func (c *lockoutMockClient) SendRequest(method crudMethod, u *URL, item MikrotikItem, result interface{}) error {
	c.calls = append(c.calls, method)
	switch method {
	case crudRead:
		if c.readErr != nil {
			return c.readErr
		}
		switch out := result.(type) {
		case *MikrotikItem:
			for k, v := range c.readItem {
				(*out)[k] = v
			}
		case *[]MikrotikItem:
			*out = append(*out, c.readList...)
		}
		return nil
	case crudPost:
		copied := MikrotikItem{}
		for k, v := range item {
			copied[k] = v
		}
		c.postItems = append(c.postItems, copied)
		return c.postErr
	default:
		return nil
	}
}

func TestResolveConnectionEndpoint(t *testing.T) {
	cases := []struct {
		rawURL      string
		wantService string
		wantPort    int
		wantHost    string
		wantErr     bool
	}{
		{rawURL: "http://192.168.88.1", wantService: "www", wantPort: 80, wantHost: "192.168.88.1"},
		{rawURL: "http://192.168.88.1:8080", wantService: "www", wantPort: 8080, wantHost: "192.168.88.1"},
		{rawURL: "https://192.168.88.1", wantService: "www-ssl", wantPort: 443, wantHost: "192.168.88.1"},
		{rawURL: "https://[2001:db8::1]:8443", wantService: "www-ssl", wantPort: 8443, wantHost: "2001:db8::1"},
		{rawURL: "api://192.168.88.1", wantService: "api", wantPort: 8728, wantHost: "192.168.88.1"},
		{rawURL: "api://192.168.88.1:18728", wantService: "api", wantPort: 18728, wantHost: "192.168.88.1"},
		{rawURL: "apis://192.168.88.1", wantService: "api-ssl", wantPort: 8729, wantHost: "192.168.88.1"},
		{rawURL: "apis://[::1]:18729", wantService: "api-ssl", wantPort: 18729, wantHost: "::1"},
		{rawURL: "ftp://192.168.88.1", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.rawURL, func(t *testing.T) {
			parsed, err := url.Parse(tc.rawURL)
			if err != nil {
				t.Fatalf("url.Parse failed: %v", err)
			}
			svc, port, host, err := resolveConnectionEndpoint(parsed)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got svc=%q port=%d host=%q", svc, port, host)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if svc != tc.wantService || port != tc.wantPort || host != tc.wantHost {
				t.Fatalf("got (%q, %d, %q), want (%q, %d, %q)", svc, port, host, tc.wantService, tc.wantPort, tc.wantHost)
			}
		})
	}
}

func TestNewClientExtraParams(t *testing.T) {
	t.Run("http", func(t *testing.T) {
		isolateClientGlobals(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"version":"7.25"}]`))
		}))
		defer srv.Close()

		parsed, _ := url.Parse(srv.URL)
		wantPort, _ := strconv.Atoi(parsed.Port())
		d := newClientTestResourceData(t, map[string]interface{}{
			"hosturl": srv.URL,
		})
		client, diags := NewClient(context.Background(), d)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		extra := client.(Client).GetExtraParams()
		if extra.ActiveService != "www" || extra.ActivePort != wantPort || extra.ActiveHost != parsed.Hostname() {
			t.Fatalf("extra = %+v, want service=www port=%d host=%s", extra, wantPort, parsed.Hostname())
		}
	})

	t.Run("https", func(t *testing.T) {
		isolateClientGlobals(t)
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"version":"7.25"}]`))
		}))
		defer srv.Close()

		parsed, _ := url.Parse(srv.URL)
		wantPort, _ := strconv.Atoi(parsed.Port())
		d := newClientTestResourceData(t, map[string]interface{}{
			"hosturl":  srv.URL,
			"insecure": true,
		})
		client, diags := NewClient(context.Background(), d)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		extra := client.(Client).GetExtraParams()
		if extra.ActiveService != "www-ssl" || extra.ActivePort != wantPort || extra.ActiveHost != parsed.Hostname() {
			t.Fatalf("extra = %+v, want service=www-ssl port=%d host=%s", extra, wantPort, parsed.Hostname())
		}
	})

	t.Run("api", func(t *testing.T) {
		isolateClientGlobals(t)
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen failed: %v", err)
		}
		done := startMockAPIServer(ln)
		defer closeMockAPIServer(ln, done)

		host, portStr, _ := net.SplitHostPort(ln.Addr().String())
		wantPort, _ := strconv.Atoi(portStr)
		d := newClientTestResourceData(t, map[string]interface{}{
			"hosturl": "api://" + ln.Addr().String(),
		})
		client, diags := NewClient(context.Background(), d)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		extra := client.(Client).GetExtraParams()
		if extra.ActiveService != "api" || extra.ActivePort != wantPort || extra.ActiveHost != host {
			t.Fatalf("extra = %+v, want service=api port=%d host=%s", extra, wantPort, host)
		}
	})

	t.Run("apis", func(t *testing.T) {
		isolateClientGlobals(t)
		certSource := httptest.NewTLSServer(nil)
		cert := certSource.TLS.Certificates[0]
		certSource.Close()

		ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
		if err != nil {
			t.Fatalf("tls listen failed: %v", err)
		}
		done := startMockAPIServer(ln)
		defer closeMockAPIServer(ln, done)

		host, portStr, _ := net.SplitHostPort(ln.Addr().String())
		wantPort, _ := strconv.Atoi(portStr)
		d := newClientTestResourceData(t, map[string]interface{}{
			"hosturl":  "apis://" + ln.Addr().String(),
			"insecure": true,
		})
		client, diags := NewClient(context.Background(), d)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		extra := client.(Client).GetExtraParams()
		if extra.ActiveService != "api-ssl" || extra.ActivePort != wantPort || extra.ActiveHost != host {
			t.Fatalf("extra = %+v, want service=api-ssl port=%d host=%s", extra, wantPort, host)
		}
	})
}

func TestCheckIpServiceLockout(t *testing.T) {
	ctx := context.Background()

	t.Run("nil client or empty active service is allowed", func(t *testing.T) {
		if err := checkIpServiceLockout(ctx, "api", true, 8728, "", "", nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		c := &lockoutMockClient{extra: &ExtraParams{}}
		if err := checkIpServiceLockout(ctx, "api", true, 8728, "", "", c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("non-active service is always allowed", func(t *testing.T) {
		c := &lockoutMockClient{extra: &ExtraParams{
			ActiveService:  "api",
			ActivePort:     8728,
			ActiveHost:     "192.168.88.1",
			ClientSourceIP: "192.168.88.10",
		}}
		if err := checkIpServiceLockout(ctx, "telnet", true, 2323, "10.0.0.0/8", "", c); err != nil {
			t.Fatalf("unexpected error for non-active service: %v", err)
		}
	})

	for _, svc := range []struct {
		name string
		port int
	}{
		{name: "api", port: 8728},
		{name: "api-ssl", port: 8729},
		{name: "www", port: 80},
		{name: "www-ssl", port: 443},
	} {
		t.Run("service_"+svc.name, func(t *testing.T) {
			c := &lockoutMockClient{extra: &ExtraParams{
				ActiveService:  svc.name,
				ActivePort:     svc.port,
				ActiveHost:     "192.168.88.1",
				ClientSourceIP: "192.168.88.10",
			}}

			// Disabled without ack.
			err := checkIpServiceLockout(ctx, svc.name, true, svc.port, "", "", c)
			wantToken := "ip-service:" + svc.name + ":disable"
			if err == nil || !strings.Contains(err.Error(), svc.name) || !strings.Contains(err.Error(), wantToken) {
				t.Fatalf("expected lockout error with %q, got %v", wantToken, err)
			}

			// Wrong ack.
			err = checkIpServiceLockout(ctx, svc.name, true, svc.port, "", "wrong", c)
			if err == nil || !strings.Contains(err.Error(), wantToken) {
				t.Fatalf("expected lockout error with wrong ack, got %v", err)
			}

			// Exact ack succeeds.
			if err := checkIpServiceLockout(ctx, svc.name, true, svc.port, "", wantToken, c); err != nil {
				t.Fatalf("expected exact ack %q to succeed, got %v", wantToken, err)
			}

			// Port mismatch.
			newPort := svc.port + 100
			err = checkIpServiceLockout(ctx, svc.name, false, newPort, "", "", c)
			wantPortToken := "ip-service:" + svc.name + ":port=" + strconv.Itoa(newPort)
			if err == nil || !strings.Contains(err.Error(), wantPortToken) {
				t.Fatalf("expected port mismatch token %q, got %v", wantPortToken, err)
			}

			// Safe config (same port, enabled, matching subnet) succeeds.
			if err := checkIpServiceLockout(ctx, svc.name, false, svc.port, "192.168.88.0/24", "", c); err != nil {
				t.Fatalf("expected matching subnet to succeed, got %v", err)
			}
		})
	}

	t.Run("cidr and family checks", func(t *testing.T) {
		cases := []struct {
			name      string
			extra     *ExtraParams
			address   string
			wantBlock bool
			wantToken string
		}{
			{
				name: "ipv4 excludes client source ip",
				extra: &ExtraParams{
					ActiveService:  "www-ssl",
					ActivePort:     443,
					ActiveHost:     "192.168.88.1",
					ClientSourceIP: "192.168.88.10",
				},
				address:   "10.0.0.0/8, 172.16.0.0/12",
				wantBlock: true,
				wantToken: "ip-service:www-ssl:address=10.0.0.0/8, 172.16.0.0/12",
			},
			{
				name: "ipv4 includes 0.0.0.0/0",
				extra: &ExtraParams{
					ActiveService:  "www-ssl",
					ActivePort:     443,
					ActiveHost:     "192.168.88.1",
					ClientSourceIP: "192.168.88.10",
				},
				address:   "0.0.0.0/0",
				wantBlock: false,
			},
			{
				name: "ipv4 target with ipv6-only address",
				extra: &ExtraParams{
					ActiveService: "api",
					ActivePort:    8728,
					ActiveHost:    "192.168.88.1",
				},
				address:   "2001:db8::/32",
				wantBlock: true,
				wantToken: "ip-service:api:address=2001:db8::/32",
			},
			{
				name: "ipv6 excludes client source ip",
				extra: &ExtraParams{
					ActiveService:  "api-ssl",
					ActivePort:     8729,
					ActiveHost:     "2001:db8:1::1",
					ClientSourceIP: "2001:db8:1::10",
				},
				address:   "2001:db8:2::/48",
				wantBlock: true,
				wantToken: "ip-service:api-ssl:address=2001:db8:2::/48",
			},
			{
				name: "ipv6 includes client source ip",
				extra: &ExtraParams{
					ActiveService:  "api-ssl",
					ActivePort:     8729,
					ActiveHost:     "2001:db8:1::1",
					ClientSourceIP: "2001:db8:1::10",
				},
				address:   "2001:db8:1::/48",
				wantBlock: false,
			},
			{
				name: "loopback local detection blocks non-loopback cidr",
				extra: &ExtraParams{
					ActiveService: "www-ssl",
					ActivePort:    443,
					ActiveHost:    "127.0.0.1",
				},
				address:   "203.0.113.0/24",
				wantBlock: true,
				wantToken: "ip-service:www-ssl:address=203.0.113.0/24",
			},
			{
				name: "public host without override does not false positive under nat",
				extra: &ExtraParams{
					ActiveService: "www-ssl",
					ActivePort:    443,
					ActiveHost:    "198.51.100.1",
				},
				address:   "203.0.113.5/32",
				wantBlock: false,
			},
			{
				name: "unparseable address does not false positive",
				extra: &ExtraParams{
					ActiveService:  "api",
					ActivePort:     8728,
					ActiveHost:     "192.168.88.1",
					ClientSourceIP: "192.168.88.10",
				},
				address:   "not-a-cidr",
				wantBlock: false,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				c := &lockoutMockClient{extra: tc.extra}
				err := checkIpServiceLockout(ctx, tc.extra.ActiveService, false, tc.extra.ActivePort, tc.address, "", c)
				if tc.wantBlock {
					if err == nil || !strings.Contains(err.Error(), tc.wantToken) {
						t.Fatalf("expected error containing %q, got %v", tc.wantToken, err)
					}
					if err := checkIpServiceLockout(ctx, tc.extra.ActiveService, false, tc.extra.ActivePort, tc.address, tc.wantToken, c); err != nil {
						t.Fatalf("expected exact ack %q to succeed, got %v", tc.wantToken, err)
					}
				} else if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
			})
		}
	})
}

func TestCheckMacWinboxLockout(t *testing.T) {
	ctx := context.Background()

	t.Run("non-none list is allowed without live read", func(t *testing.T) {
		c := &lockoutMockClient{readErr: errors.New("should not be called")}
		if err := checkMacWinboxLockout(ctx, false, "", "all", "", c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(c.calls) != 0 {
			t.Fatalf("expected 0 router reads, got %v", c.calls)
		}
	})

	t.Run("prior state already none is allowed", func(t *testing.T) {
		c := &lockoutMockClient{}
		if err := checkMacWinboxLockout(ctx, true, "none", "none", "", c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := checkMacWinboxLockout(ctx, true, "", "NONE", "", c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("prior state transition requires ack", func(t *testing.T) {
		c := &lockoutMockClient{}
		err := checkMacWinboxLockout(ctx, true, "LAN", "none", "", c)
		if err == nil || !strings.Contains(err.Error(), "mac-winbox:LAN->none") {
			t.Fatalf("expected mac-winbox:LAN->none error, got %v", err)
		}

		err = checkMacWinboxLockout(ctx, true, "LAN", "", "wrong", c)
		if err == nil || !strings.Contains(err.Error(), "mac-winbox:LAN->none") {
			t.Fatalf("expected mac-winbox:LAN->none error for wrong ack, got %v", err)
		}

		if err := checkMacWinboxLockout(ctx, true, "LAN", "none", "mac-winbox:LAN->none", c); err != nil {
			t.Fatalf("expected exact ack to succeed, got %v", err)
		}
	})

	t.Run("create reads live router state", func(t *testing.T) {
		c := &lockoutMockClient{
			readItem: MikrotikItem{"allowed-interface-list": "all"},
		}
		err := checkMacWinboxLockout(ctx, false, "", "none", "", c)
		if err == nil || !strings.Contains(err.Error(), "mac-winbox:all->none") {
			t.Fatalf("expected mac-winbox:all->none error, got %v", err)
		}
		if len(c.calls) != 1 || c.calls[0] != crudRead {
			t.Fatalf("expected single crudRead call, got %v", c.calls)
		}

		if err := checkMacWinboxLockout(ctx, false, "", "none", "mac-winbox:all->none", c); err != nil {
			t.Fatalf("expected exact ack to succeed, got %v", err)
		}

		cAlreadyNone := &lockoutMockClient{
			readItem: MikrotikItem{"allowed-interface-list": "none"},
		}
		if err := checkMacWinboxLockout(ctx, false, "", "none", "", cAlreadyNone); err != nil {
			t.Fatalf("expected already-none live router to succeed, got %v", err)
		}

		cErr := &lockoutMockClient{readErr: errors.New("connection refused")}
		if err := checkMacWinboxLockout(ctx, false, "", "none", "", cErr); err == nil || !strings.Contains(err.Error(), "connection refused") {
			t.Fatalf("expected live read error, got %v", err)
		}
	})
}

func ipServiceTestResourceData(res *schema.Resource, id string, set map[string]cty.Value, attrs map[string]string) *schema.ResourceData {
	vals := map[string]cty.Value{}
	for name, aType := range res.CoreConfigSchema().ImpliedType().AttributeTypes() {
		if v, ok := set[name]; ok {
			vals[name] = v
		} else {
			vals[name] = cty.NullVal(aType)
		}
	}
	stateAttrs := map[string]string{}
	for k, v := range attrs {
		stateAttrs[k] = v
	}
	return res.Data(&terraform.InstanceState{
		ID:         id,
		Attributes: stateAttrs,
		RawConfig:  cty.ObjectVal(vals),
	})
}

func TestLockoutGuardResourceHandlers(t *testing.T) {
	ctx := context.Background()

	t.Run("ip_service read is never blocked and lockout_ack is skipped on wire", func(t *testing.T) {
		res := ResourceIpService()
		client := &lockoutMockClient{
			transport: TransportREST,
			extra: &ExtraParams{
				ActiveService:  "www-ssl",
				ActivePort:     443,
				ActiveHost:     "192.168.88.1",
				ClientSourceIP: "192.168.88.10",
			},
			readList: []MikrotikItem{{
				".id":      "*6",
				"name":     "www-ssl",
				"port":     "443",
				"disabled": "true",
				"dynamic":  "false",
				"invalid":  "false",
				"vrf":      "main",
			}},
		}

		dRead := resourceDataWithRawConfig(res, "*6", nil)
		if diags := res.ReadContext(ctx, dRead, client); diags.HasError() {
			t.Fatalf("ReadContext unexpectedly failed: %v", diags)
		}

		// Create without ack fails before sending crudPost.
		dCreateBlocked := ipServiceTestResourceData(res, "", map[string]cty.Value{
			"numbers":  cty.StringVal("www-ssl"),
			"port":     cty.NumberIntVal(443),
			"disabled": cty.BoolVal(true),
		}, map[string]string{
			"numbers":  "www-ssl",
			"port":     "443",
			"disabled": "true",
		})
		if diags := res.CreateContext(ctx, dCreateBlocked, client); !diags.HasError() {
			t.Fatal("expected CreateContext to fail without lockout_ack")
		}
		if len(client.postItems) != 0 {
			t.Fatalf("expected no crudPost when blocked, got %v", client.postItems)
		}

		// Create with ack sends crudPost without lockout-ack field.
		dCreateAllowed := ipServiceTestResourceData(res, "", map[string]cty.Value{
			"numbers":     cty.StringVal("www-ssl"),
			"port":        cty.NumberIntVal(443),
			"disabled":    cty.BoolVal(true),
			KeyLockoutAck: cty.StringVal("ip-service:www-ssl:disable"),
		}, map[string]string{
			"numbers":     "www-ssl",
			"port":        "443",
			"disabled":    "true",
			KeyLockoutAck: "ip-service:www-ssl:disable",
		})
		if diags := res.CreateContext(ctx, dCreateAllowed, client); diags.HasError() {
			t.Fatalf("expected CreateContext with ack to succeed, got %v", diags)
		}
		if len(client.postItems) != 1 {
			t.Fatalf("expected 1 crudPost, got %d", len(client.postItems))
		}
		if _, exists := client.postItems[0]["lockout-ack"]; exists {
			t.Fatalf("lockout-ack unexpectedly serialized to RouterOS payload: %v", client.postItems[0])
		}
		if _, exists := client.postItems[0][KeyLockoutAck]; exists {
			t.Fatalf("lockout_ack unexpectedly serialized to RouterOS payload: %v", client.postItems[0])
		}
	})

	t.Run("ip_service CustomizeDiff checks risky changes and skips unrelated diffs", func(t *testing.T) {
		res := ResourceIpService()
		client := &lockoutMockClient{
			transport: TransportREST,
			extra: &ExtraParams{
				ActiveService:  "www-ssl",
				ActivePort:     443,
				ActiveHost:     "192.168.88.1",
				ClientSourceIP: "192.168.88.10",
			},
		}

		// Unrelated diff (e.g., removing lockout_ack when disabled/port/address/numbers unchanged) is allowed.
		state := &terraform.InstanceState{
			ID: "*6",
			Attributes: map[string]string{
				"numbers":     "www-ssl",
				"port":        "443",
				"disabled":    "true",
				"address":     "",
				KeyLockoutAck: "ip-service:www-ssl:disable",
			},
		}
		cfgUnrelated := terraform.NewResourceConfigRaw(map[string]interface{}{
			"numbers":  "www-ssl",
			"port":     443,
			"disabled": true,
		})
		cfgUnrelated.CtyValue = ipServiceTestResourceData(res, "*6", map[string]cty.Value{
			"numbers":  cty.StringVal("www-ssl"),
			"port":     cty.NumberIntVal(443),
			"disabled": cty.True,
		}, nil).GetRawConfig()
		if _, err := res.Diff(ctx, state, cfgUnrelated, client); err != nil {
			t.Fatalf("expected unrelated diff to pass CustomizeDiff, got %v", err)
		}

		// Risky change in diff is blocked without ack.
		cfgRisky := terraform.NewResourceConfigRaw(map[string]interface{}{
			"numbers":  "www-ssl",
			"port":     8443,
			"disabled": false,
		})
		cfgRisky.CtyValue = ipServiceTestResourceData(res, "*6", map[string]cty.Value{
			"numbers":  cty.StringVal("www-ssl"),
			"port":     cty.NumberIntVal(8443),
			"disabled": cty.False,
		}, nil).GetRawConfig()
		if _, err := res.Diff(ctx, state, cfgRisky, client); err == nil || !strings.Contains(err.Error(), "ip-service:www-ssl:port=8443") {
			t.Fatalf("expected CustomizeDiff to block port change, got %v", err)
		}
	})

	t.Run("mac_winbox read is never blocked and Create/Diff enforce lockout guard", func(t *testing.T) {
		res := ResourceToolMacServerWinBox()
		client := &lockoutMockClient{
			transport: TransportREST,
			extra:     &ExtraParams{},
			readItem:  MikrotikItem{"allowed-interface-list": "all"},
		}

		// Ordinary read when live router is already "none" is never blocked.
		clientNone := &lockoutMockClient{
			transport: TransportREST,
			extra:     &ExtraParams{},
			readItem:  MikrotikItem{"allowed-interface-list": "none"},
		}
		dRead := schema.TestResourceDataRaw(t, res.Schema, map[string]interface{}{
			"allowed_interface_list": "none",
		})
		dRead.SetId("tool.mac-server.mac-winbox")
		if diags := res.ReadContext(ctx, dRead, clientNone); diags.HasError() {
			t.Fatalf("ReadContext unexpectedly failed: %v", diags)
		}

		// Create with "none" without ack is blocked before crudPost.
		dCreateBlocked := schema.TestResourceDataRaw(t, res.Schema, map[string]interface{}{
			"allowed_interface_list": "none",
		})
		if diags := res.CreateContext(ctx, dCreateBlocked, client); !diags.HasError() {
			t.Fatal("expected CreateContext to fail without lockout_ack")
		}
		if len(client.postItems) != 0 {
			t.Fatalf("expected no crudPost when blocked, got %v", client.postItems)
		}

		// Create with "none" and exact ack succeeds without sending lockout-ack.
		dCreateAllowed := schema.TestResourceDataRaw(t, res.Schema, map[string]interface{}{
			"allowed_interface_list": "none",
			KeyLockoutAck:            "mac-winbox:all->none",
		})
		if diags := res.CreateContext(ctx, dCreateAllowed, client); diags.HasError() {
			t.Fatalf("expected CreateContext with ack to succeed, got %v", diags)
		}
		if len(client.postItems) != 1 {
			t.Fatalf("expected 1 crudPost, got %d", len(client.postItems))
		}
		if _, exists := client.postItems[0]["lockout-ack"]; exists {
			t.Fatalf("lockout-ack unexpectedly serialized to RouterOS payload: %v", client.postItems[0])
		}

		// Diff from LAN -> none without ack is blocked.
		stateLAN := &terraform.InstanceState{
			ID: "tool.mac-server.mac-winbox",
			Attributes: map[string]string{
				"allowed_interface_list": "LAN",
			},
		}
		cfgNone := terraform.NewResourceConfigRaw(map[string]interface{}{
			"allowed_interface_list": "none",
		})
		if _, err := res.Diff(ctx, stateLAN, cfgNone, client); err == nil || !strings.Contains(err.Error(), "mac-winbox:LAN->none") {
			t.Fatalf("expected Diff to block LAN->none, got %v", err)
		}

		// Diff when allowed_interface_list is unchanged ("none") is allowed.
		stateNone := &terraform.InstanceState{
			ID: "tool.mac-server.mac-winbox",
			Attributes: map[string]string{
				"allowed_interface_list": "none",
				KeyLockoutAck:            "mac-winbox:LAN->none",
			},
		}
		if _, err := res.Diff(ctx, stateNone, cfgNone, client); err != nil {
			t.Fatalf("expected Diff with unchanged allowed_interface_list to pass, got %v", err)
		}
	})
}
