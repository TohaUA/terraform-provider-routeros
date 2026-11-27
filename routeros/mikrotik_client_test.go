package routeros

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-routeros/routeros/v3/proto"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func newClientTestResourceData(t *testing.T, overrides map[string]interface{}) *schema.ResourceData {
	t.Helper()

	for _, env := range []string{
		"ROS_HOSTURL",
		"ROS_USERNAME",
		"ROS_PASSWORD",
		"ROS_CA_CERTIFICATE",
		"ROS_INSECURE",
		"ROS_SUPPRESS_SYSO_DEL_WARN",
	} {
		t.Setenv(env, "")
	}

	raw := map[string]interface{}{
		"hosturl":                "https://127.0.0.1",
		"username":               "admin",
		"password":               "secret",
		"ca_certificate":         "",
		"insecure":               false,
		"suppress_syso_del_warn": false,
		"rest_timeout":           59,
	}
	for k, v := range overrides {
		raw[k] = v
	}

	return schema.TestResourceDataRaw(t, Provider().Schema, raw)
}

func isolateClientGlobals(t *testing.T) {
	t.Helper()

	connectionMu.Lock()
	origRest := restConnections
	origPools := apiPools
	restConnections = map[string]*http.Client{}
	apiPools = map[string]*apiPool{}
	connectionMu.Unlock()

	t.Cleanup(func() {
		connectionMu.Lock()
		for _, pool := range apiPools {
			pool.mu.Lock()
			for _, s := range pool.idle {
				_ = s.close()
			}
			pool.idle = nil
			pool.mu.Unlock()
		}
		for _, client := range restConnections {
			client.CloseIdleConnections()
		}
		restConnections = origRest
		apiPools = origPools
		connectionMu.Unlock()
	})
}

func TestNewClientUnknownSchemeReturnsError(t *testing.T) {
	isolateClientGlobals(t)

	for _, scheme := range []string{"htps", "ftp"} {
		t.Run(scheme, func(t *testing.T) {
			d := newClientTestResourceData(t, map[string]interface{}{
				"hosturl": scheme + "://router.local",
			})

			client, diags := NewClient(context.Background(), d)
			if client != nil {
				t.Fatalf("expected nil client for scheme %q, got %T", scheme, client)
			}
			if !diags.HasError() {
				t.Fatalf("expected error diagnostic for scheme %q, got %v", scheme, diags)
			}
			if !strings.Contains(diags[0].Summary, scheme) {
				t.Errorf("error summary %q does not mention scheme %q", diags[0].Summary, scheme)
			}
		})
	}
}

func TestNewClientRejectsInvalidCAFile(t *testing.T) {
	isolateClientGlobals(t)

	dir := t.TempDir()
	tests := []struct {
		name    string
		content []byte
	}{
		{name: "empty file", content: []byte{}},
		{name: "non-PEM file", content: []byte("not a PEM certificate\n")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			caPath := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "_")+".pem")
			if err := os.WriteFile(caPath, tc.content, 0o600); err != nil {
				t.Fatalf("failed to write temp CA file: %v", err)
			}

			d := newClientTestResourceData(t, map[string]interface{}{
				"hosturl":        "https://127.0.0.1:8443",
				"ca_certificate": caPath,
				"insecure":       false,
			})

			client, diags := NewClient(context.Background(), d)
			if client != nil {
				t.Fatalf("expected nil client for %s, got %T", tc.name, client)
			}
			if !diags.HasError() {
				t.Fatalf("expected error diagnostic for %s, got %v", tc.name, diags)
			}
			if !strings.Contains(diags[0].Summary, caPath) {
				t.Errorf("error summary %q does not name CA file %q", diags[0].Summary, caPath)
			}
		})
	}
}

func TestRESTClientRefusesRedirect(t *testing.T) {
	isolateClientGlobals(t)

	var plainHTTPRequests atomic.Int32
	plainServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plainHTTPRequests.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer plainServer.Close()

	tlsRedirectServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plainServer.URL+"/rest/system/resource", http.StatusPermanentRedirect)
	}))
	defer tlsRedirectServer.Close()

	httpClient := restClient("redirect-test-key", 5*time.Second, &tls.Config{InsecureSkipVerify: true})

	req, err := http.NewRequest(http.MethodPatch, tlsRedirectServer.URL+"/rest/system/resource", strings.NewReader(`{"name":"x"}`))
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	req.SetBasicAuth("admin", "secret")

	res, err := httpClient.Do(req)
	if res != nil && res.Body != nil {
		_ = res.Body.Close()
	}
	if err == nil {
		t.Fatal("expected httpClient.Do to fail on 308 redirect, got nil error")
	}
	if got := plainHTTPRequests.Load(); got != 0 {
		t.Fatalf("plain HTTP redirect target received %d request(s) after Do, want 0", got)
	}

	rc := &RestClient{
		ctx:       context.Background(),
		HostURL:   tlsRedirectServer.URL,
		Username:  "admin",
		Password:  "secret",
		Transport: TransportREST,
		extra:     &ExtraParams{},
		Client:    httpClient,
	}
	var out MikrotikItem
	err = rc.SendRequest(crudUpdate, &URL{Path: "/system/resource"}, MikrotikItem{"name": "x"}, &out)
	if err == nil {
		t.Fatal("expected RestClient.SendRequest to fail on 308 redirect, got nil error")
	}
	if got := plainHTTPRequests.Load(); got != 0 {
		t.Fatalf("plain HTTP redirect target received %d request(s) after SendRequest, want 0", got)
	}
}

func startMockAPIServer(ln net.Listener) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		r := proto.NewReader(conn)
		w := proto.NewWriter(conn)
		for {
			s, err := r.ReadSentence()
			if err != nil {
				return
			}
			switch s.Word {
			case "/login":
				w.BeginSentence()
				w.WriteWord("!done")
				if err := w.EndSentence(); err != nil {
					return
				}
			case "/system/resource/print":
				w.BeginSentence()
				w.WriteWord("!re")
				w.WriteWord("=version=7.25.2")
				if err := w.EndSentence(); err != nil {
					return
				}
				w.BeginSentence()
				w.WriteWord("!done")
				if err := w.EndSentence(); err != nil {
					return
				}
			default:
				w.BeginSentence()
				w.WriteWord("!done")
				if err := w.EndSentence(); err != nil {
					return
				}
			}
		}
	}()
	return done
}

func closeMockAPIServer(ln net.Listener, done <-chan struct{}) {
	_ = ln.Close()
	connectionMu.Lock()
	for _, pool := range apiPools {
		pool.mu.Lock()
		for _, s := range pool.idle {
			_ = s.close()
		}
		pool.idle = nil
		pool.mu.Unlock()
	}
	connectionMu.Unlock()
	<-done
}

func TestNewClientPlaintextWarnings(t *testing.T) {
	t.Run("http emits warning", func(t *testing.T) {
		isolateClientGlobals(t)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"version":"7.25.2"}]`))
		}))
		defer srv.Close()

		d := newClientTestResourceData(t, map[string]interface{}{
			"hosturl": srv.URL,
		})

		client, diags := NewClient(context.Background(), d)
		if client == nil {
			t.Fatalf("expected non-nil client, got nil (diags: %v)", diags)
		}
		if diags.HasError() {
			t.Fatalf("unexpected error diagnostics: %v", diags)
		}
		if len(diags) != 1 || diags[0].Severity != diag.Warning {
			t.Fatalf("expected 1 warning diagnostic, got %+v", diags)
		}
		if !strings.Contains(diags[0].Detail, "http://") {
			t.Errorf("expected warning detail to mention http://, got %q", diags[0].Detail)
		}
	})

	t.Run("api emits warning", func(t *testing.T) {
		isolateClientGlobals(t)

		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to listen: %v", err)
		}
		done := startMockAPIServer(ln)
		defer closeMockAPIServer(ln, done)

		d := newClientTestResourceData(t, map[string]interface{}{
			"hosturl": "api://" + ln.Addr().String(),
		})

		client, diags := NewClient(context.Background(), d)
		if client == nil {
			t.Fatalf("expected non-nil client, got nil (diags: %v)", diags)
		}
		if diags.HasError() {
			t.Fatalf("unexpected error diagnostics: %v", diags)
		}
		if len(diags) != 1 || diags[0].Severity != diag.Warning {
			t.Fatalf("expected 1 warning diagnostic, got %+v", diags)
		}
		if !strings.Contains(diags[0].Detail, "api://") {
			t.Errorf("expected warning detail to mention api://, got %q", diags[0].Detail)
		}
	})

	t.Run("https emits no warnings", func(t *testing.T) {
		isolateClientGlobals(t)

		tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"version":"7.25.2"}]`))
		}))
		defer tlsSrv.Close()

		d := newClientTestResourceData(t, map[string]interface{}{
			"hosturl":  tlsSrv.URL,
			"insecure": true,
		})

		client, diags := NewClient(context.Background(), d)
		if client == nil {
			t.Fatalf("expected non-nil client, got nil (diags: %v)", diags)
		}
		if len(diags) != 0 {
			t.Fatalf("expected no diagnostics for https://, got %+v", diags)
		}
	})

	t.Run("apis emits no warnings", func(t *testing.T) {
		isolateClientGlobals(t)

		certSource := httptest.NewTLSServer(nil)
		cert := certSource.TLS.Certificates[0]
		certSource.Close()

		ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
			Certificates: []tls.Certificate{cert},
		})
		if err != nil {
			t.Fatalf("failed to listen TLS: %v", err)
		}
		done := startMockAPIServer(ln)
		defer closeMockAPIServer(ln, done)

		d := newClientTestResourceData(t, map[string]interface{}{
			"hosturl":  "apis://" + ln.Addr().String(),
			"insecure": true,
		})

		client, diags := NewClient(context.Background(), d)
		if client == nil {
			t.Fatalf("expected non-nil client, got nil (diags: %v)", diags)
		}
		if len(diags) != 0 {
			t.Fatalf("expected no diagnostics for apis://, got %+v", diags)
		}
	})

	t.Run("schemeless hosturl defaults to https with no warnings", func(t *testing.T) {
		isolateClientGlobals(t)

		tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"version":"7.25.2"}]`))
		}))
		defer tlsSrv.Close()

		d := newClientTestResourceData(t, map[string]interface{}{
			"hosturl":  strings.TrimPrefix(tlsSrv.URL, "https://"),
			"insecure": true,
		})

		client, diags := NewClient(context.Background(), d)
		if client == nil {
			t.Fatalf("expected non-nil client, got nil (diags: %v)", diags)
		}
		if len(diags) != 0 {
			t.Fatalf("expected no diagnostics for schemeless hosturl, got %+v", diags)
		}
		rc, ok := client.(*RestClient)
		if !ok {
			t.Fatalf("expected *RestClient, got %T", client)
		}
		if rc.HostURL != tlsSrv.URL {
			t.Errorf("HostURL = %q, want %q", rc.HostURL, tlsSrv.URL)
		}
		if rc.Version != "7.25.2" {
			t.Errorf("Version = %q, want %q", rc.Version, "7.25.2")
		}
	})
}

func TestNewClientMinimumVersionGuard(t *testing.T) {
	for _, tc := range []struct {
		name        string
		rawVersion  string
		wantVersion string
		wantErr     bool
	}{
		{name: "rejects 7.24.5 with channel", rawVersion: "7.24.5 (stable)", wantErr: true},
		{name: "rejects 7.24 without channel", rawVersion: "7.24", wantErr: true},
		{name: "accepts 7.25 without channel", rawVersion: "7.25", wantVersion: "7.25"},
		{name: "accepts 7.25.2 without channel", rawVersion: "7.25.2", wantVersion: "7.25.2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateClientGlobals(t)

			tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`[{"version":"` + tc.rawVersion + `"}]`))
			}))
			defer tlsSrv.Close()

			d := newClientTestResourceData(t, map[string]interface{}{
				"hosturl":  tlsSrv.URL,
				"insecure": true,
			})

			client, diags := NewClient(context.Background(), d)
			if tc.wantErr {
				if client != nil {
					t.Fatalf("expected nil client for version %q, got %T", tc.rawVersion, client)
				}
				if !diags.HasError() {
					t.Fatalf("expected error diagnostic for version %q, got %+v", tc.rawVersion, diags)
				}
				if !strings.Contains(diags[0].Summary, minSupportedRouterOSVersion) {
					t.Errorf("error summary %q must mention minimum version %s", diags[0].Summary, minSupportedRouterOSVersion)
				}
				return
			}
			if diags.HasError() {
				t.Fatalf("unexpected error diagnostics for %q: %v", tc.rawVersion, diags)
			}
			rc, ok := client.(*RestClient)
			if !ok {
				t.Fatalf("expected *RestClient, got %T", client)
			}
			if rc.Version != tc.wantVersion {
				t.Errorf("Version = %q, want %q", rc.Version, tc.wantVersion)
			}
		})
	}
}

func TestGetRouterOSVersionFormats(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{raw: "7.25", want: "7.25"},
		{raw: "7.25.2", want: "7.25.2"},
		{raw: "7.25.2 (stable)", want: "7.25.2"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`[{"version":"` + tc.raw + `"}]`))
			}))
			defer srv.Close()

			rc := &RestClient{
				ctx:       context.Background(),
				HostURL:   srv.URL,
				Transport: TransportREST,
				extra:     &ExtraParams{},
				Client:    srv.Client(),
			}
			got, diags := GetRouterOSVersion(rc)
			if diags.HasError() {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
			if got != tc.want {
				t.Errorf("GetRouterOSVersion(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestRESTClientUsesProxyFromEnvironment(t *testing.T) {
	isolateClientGlobals(t)

	client := restClient("proxy-test-key", 5*time.Second, &tls.Config{InsecureSkipVerify: true})
	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}
	if tr.Proxy == nil {
		t.Fatal("expected http.Transport.Proxy to be set, got nil")
	}
	if reflect.ValueOf(tr.Proxy).Pointer() != reflect.ValueOf(http.ProxyFromEnvironment).Pointer() {
		t.Error("expected http.Transport.Proxy to be http.ProxyFromEnvironment")
	}
}
