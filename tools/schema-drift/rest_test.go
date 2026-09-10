package main

import (
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/terraform-routeros/terraform-provider-routeros/routeros"
)

func TestDecodeMenuResponse(t *testing.T) {
	items, err := DecodeMenuResponse(200, []byte(`[{".id":"*1","name":"a","port":22},{".id":"*2","name":"b"}]`))
	if err != nil || len(items) != 2 || items[0]["port"] != "22" {
		t.Errorf("list: %v %v", items, err)
	}
	items, err = DecodeMenuResponse(200, []byte(`{"ip-forward":"true","rp-filter":"no"}`))
	if err != nil || len(items) != 1 || items[0]["rp-filter"] != "no" {
		t.Errorf("singleton: %v %v", items, err)
	}
	items, err = DecodeMenuResponse(200, []byte(`[]`))
	if err != nil || len(items) != 0 {
		t.Errorf("empty list: %v %v", items, err)
	}
	_, err = DecodeMenuResponse(400, []byte(`{"detail":"no such command or directory (zerotier)","error":400,"message":"Bad Request"}`))
	if !errors.Is(err, ErrMenuAbsent) || !strings.Contains(err.Error(), "zerotier") {
		t.Errorf("missing package: %v", err)
	}
	_, err = DecodeMenuResponse(404, nil)
	if !errors.Is(err, ErrMenuAbsent) {
		t.Errorf("404: %v", err)
	}
	_, err = DecodeMenuResponse(401, []byte(`{"message":"Unauthorized"}`))
	if err == nil || errors.Is(err, ErrMenuAbsent) || !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("401: %v", err)
	}
	_, err = DecodeMenuResponse(400, []byte(`{"detail":"invalid value"}`))
	if err == nil || errors.Is(err, ErrMenuAbsent) {
		t.Errorf("other 400 must not be treated as absent: %v", err)
	}
}

func TestClientGetIsGetOnly(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		u, p, _ := r.BasicAuth()
		if u != "admin" || p != "secret" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/rest/system/resource":
			w.Write([]byte(`{"version":"7.24 (stable)"}`))
		case "/rest/ip/settings":
			w.Write([]byte(`{"ip-forward":"true"}`))
		default:
			w.WriteHeader(400)
			w.Write([]byte(`{"detail":"no such command or directory (x)"}`))
		}
	}))
	defer srv.Close()

	c := &Client{Base: normalizeBase(srv.URL + "/rest/"), Username: "admin", Password: "secret", HTTP: &http.Client{Timeout: time.Second}}
	v, err := c.Version()
	if err != nil || v != "7.24" {
		t.Errorf("version %q %v", v, err)
	}
	items, err := c.Get("/ip/settings")
	if err != nil || items[0]["ip-forward"] != "true" {
		t.Errorf("settings %v %v", items, err)
	}
	if _, err := c.Get("/zerotier"); !errors.Is(err, ErrMenuAbsent) {
		t.Errorf("zerotier %v", err)
	}
	for _, m := range methods {
		if !strings.HasPrefix(m, "GET ") {
			t.Errorf("non-GET request issued: %s", m)
		}
	}
	if len(methods) != 3 {
		t.Errorf("requests: %v", methods)
	}
}

// clearConnEnv blanks every connection variable the tool reads, so that a developer's own ROS_* or
// MIKROTIK_* environment cannot leak into a test. An empty value counts as unset.
func clearConnEnv(t *testing.T) {
	t.Helper()
	for _, names := range [][]string{envHostURL, envUsername, envPassword, envCACert, envInsecure} {
		for _, n := range names {
			t.Setenv(n, "")
		}
	}
}

func TestResolveEnv(t *testing.T) {
	env := func(kv ...string) func(string) string {
		m := map[string]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return func(k string) string { return m[k] }
	}
	for _, tc := range []struct {
		name    string
		getenv  func(string) string
		want    connEnv
		wantErr string
	}{
		{name: "nothing set", getenv: env(), wantErr: "ROS_HOSTURL or MIKROTIK_HOST is not set"},
		{name: "blank host", getenv: env("ROS_HOSTURL", "  ", "ROS_USERNAME", "u"), wantErr: "ROS_HOSTURL or MIKROTIK_HOST is not set"},
		{name: "no username", getenv: env("ROS_HOSTURL", "https://r"), wantErr: "ROS_USERNAME or MIKROTIK_USER is not set"},
		{
			name: "ROS names",
			getenv: env("ROS_HOSTURL", " https://r ", "ROS_USERNAME", "u", "ROS_PASSWORD", "p",
				"ROS_CA_CERTIFICATE", "/ca.pem", "ROS_INSECURE", "false"),
			want: connEnv{HostURL: "https://r", Username: "u", Password: "p", CACert: "/ca.pem", CACertVar: "ROS_CA_CERTIFICATE"},
		},
		{
			name: "MIKROTIK names",
			getenv: env("MIKROTIK_HOST", "https://m", "MIKROTIK_USER", "mu", "MIKROTIK_PASSWORD", "mp",
				"MIKROTIK_CA_CERTIFICATE", "/m.pem", "MIKROTIK_INSECURE", "1"),
			want: connEnv{HostURL: "https://m", Username: "mu", Password: "mp", CACert: "/m.pem", CACertVar: "MIKROTIK_CA_CERTIFICATE", Insecure: true},
		},
		{
			// Provider order: the ROS_* name wins, even ROS_INSECURE=false over MIKROTIK_INSECURE=true.
			name: "ROS names take precedence",
			getenv: env("ROS_HOSTURL", "https://r", "MIKROTIK_HOST", "https://m", "ROS_USERNAME", "u", "MIKROTIK_USER", "mu",
				"ROS_PASSWORD", "p", "MIKROTIK_PASSWORD", "mp", "ROS_CA_CERTIFICATE", "/ca.pem", "MIKROTIK_CA_CERTIFICATE", "/m.pem",
				"ROS_CACERT", "/old.pem", "ROS_INSECURE", "false", "MIKROTIK_INSECURE", "true"),
			want: connEnv{HostURL: "https://r", Username: "u", Password: "p", CACert: "/ca.pem", CACertVar: "ROS_CA_CERTIFICATE"},
		},
		{
			name:   "empty ROS value falls through to MIKROTIK",
			getenv: env("ROS_HOSTURL", "", "MIKROTIK_HOST", "https://m", "ROS_USERNAME", "u"),
			want:   connEnv{HostURL: "https://m", Username: "u"},
		},
		{
			name:   "legacy ROS_CACERT still works",
			getenv: env("ROS_HOSTURL", "https://r", "ROS_USERNAME", "u", "ROS_CACERT", "/old.pem"),
			want:   connEnv{HostURL: "https://r", Username: "u", CACert: "/old.pem", CACertVar: "ROS_CACERT"},
		},
		{
			name:   "provider CA variable beats ROS_CACERT",
			getenv: env("ROS_HOSTURL", "https://r", "ROS_USERNAME", "u", "ROS_CACERT", "/old.pem", "MIKROTIK_CA_CERTIFICATE", "/m.pem"),
			want:   connEnv{HostURL: "https://r", Username: "u", CACert: "/m.pem", CACertVar: "MIKROTIK_CA_CERTIFICATE"},
		},
		{
			name:   "insecure accepts what strconv.ParseBool accepts",
			getenv: env("ROS_HOSTURL", "https://r", "ROS_USERNAME", "u", "ROS_INSECURE", "TRUE"),
			want:   connEnv{HostURL: "https://r", Username: "u", Insecure: true},
		},
		{
			name:    "insecure not a boolean",
			getenv:  env("ROS_HOSTURL", "https://r", "ROS_USERNAME", "u", "MIKROTIK_INSECURE", "yes"),
			wantErr: `MIKROTIK_INSECURE: "yes" is not a boolean`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveEnv(tc.getenv)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// The tool must read the same variables as the provider, in the same order. The provider's
// DefaultFuncs are closures, so probe them: every name alone must be picked up, and with every name
// set the first one must win. ROS_CACERT is the tool's own legacy name and must stay last.
func TestEnvNamesMatchProvider(t *testing.T) {
	p := routeros.Provider()
	if envCACert[len(envCACert)-1] != "ROS_CACERT" {
		t.Fatalf("ROS_CACERT must be the last CA variable so that the provider's names win: %v", envCACert)
	}
	for key, names := range map[string][]string{
		"hosturl":        envHostURL,
		"username":       envUsername,
		"password":       envPassword,
		"ca_certificate": envCACert[:len(envCACert)-1],
		"insecure":       envInsecure,
	} {
		def := p.Schema[key].DefaultFunc
		if def == nil {
			t.Errorf("provider attribute %s has no DefaultFunc", key)
			continue
		}
		clearConnEnv(t)
		for i, n := range names {
			want := fmt.Sprintf("value-%d", i)
			t.Setenv(n, want)
			if got, err := def(); err != nil || got != want {
				t.Errorf("%s: with only %s set the provider reads %v (%v), want %q", key, n, got, err, want)
			}
			t.Setenv(n, "")
		}
		for i, n := range names {
			t.Setenv(n, fmt.Sprintf("value-%d", i))
		}
		if got, err := def(); err != nil || got != "value-0" {
			t.Errorf("%s: with %v all set the provider reads %v (%v), want the value of %s", key, names, got, err, names[0])
		}
	}

	clearConnEnv(t)
	t.Setenv("ROS_CACERT", "/old.pem")
	if got, _ := p.Schema["ca_certificate"].DefaultFunc(); got != nil {
		t.Errorf("the provider now reads ROS_CACERT (%v); revisit its position in envCACert", got)
	}
}

// Only the MIKROTIK_* names are set, and the device's certificate is trusted through
// MIKROTIK_CA_CERTIFICATE: the client must reach the TLS device with the right credentials.
func TestNewClientFromEnvProviderAliases(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, _ := r.BasicAuth(); u != "reader" || p != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"version":"7.24 (stable)"}`))
	}))
	defer srv.Close()
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}

	clearConnEnv(t)
	t.Setenv("MIKROTIK_HOST", srv.URL)
	t.Setenv("MIKROTIK_USER", "reader")
	t.Setenv("MIKROTIK_PASSWORD", "secret")
	t.Setenv("MIKROTIK_CA_CERTIFICATE", ca)
	c, err := NewClientFromEnv(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := c.Version(); err != nil || v != "7.24" {
		t.Errorf("version %q %v", v, err)
	}

	t.Setenv("MIKROTIK_CA_CERTIFICATE", filepath.Join(t.TempDir(), "missing.pem"))
	if _, err := NewClientFromEnv(time.Second); err == nil || !strings.HasPrefix(err.Error(), "MIKROTIK_CA_CERTIFICATE: ") {
		t.Errorf("unreadable CA file must name the variable it came from: %v", err)
	}
}

func TestNormalizeBaseAndVersion(t *testing.T) {
	for in, want := range map[string]string{
		"https://r":       "https://r",
		"https://r/":      "https://r",
		"https://r/rest":  "https://r",
		"https://r/rest/": "https://r",
	} {
		if got := normalizeBase(in); got != want {
			t.Errorf("normalizeBase(%q) = %q", in, got)
		}
	}
	if v, err := ParseVersion("7.24.1 (long-term)"); err != nil || v != "7.24.1" {
		t.Errorf("ParseVersion: %q %v", v, err)
	}
	if _, err := ParseVersion("stable"); err == nil {
		t.Errorf("ParseVersion must fail on garbage")
	}
}

func TestInspectMenu(t *testing.T) {
	in, err := ParseInspect(strings.NewReader(`{"ip":{"_type":"dir","service":{"_type":"dir",
	  "set":{"_type":"cmd","numbers":{"_type":"arg"},"port":{"_type":"arg"},"available-from":{"_type":"arg"},"print":{"_type":"cmd"}},
	  "print":{"_type":"cmd"}},
	  "settings":{"_type":"dir","print":{"_type":"cmd"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	args, ok := in.Menu("/ip/service")
	if !ok || len(args) != 2 {
		t.Errorf("/ip/service: %v %v", args, ok)
	}
	if _, ok := args["numbers"]; ok {
		t.Errorf("numbers is not a property")
	}
	if _, ok := in.Menu("/ip/settings"); ok {
		t.Errorf("menu without add/set must report ok=false")
	}
	if _, ok := in.Menu("/zerotier"); ok {
		t.Errorf("unknown menu must report ok=false")
	}
	var nilInspect *Inspect
	if _, ok := nilInspect.Menu("/ip/service"); ok {
		t.Errorf("nil inspect must report ok=false")
	}
}
