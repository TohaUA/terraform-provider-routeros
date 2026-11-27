package routeros

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-routeros/routeros/v3/proto"
	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testDatasourceSystemDeviceModeCurrent = "data.routeros_system_device_mode.current"

var populatedDeviceModeFields = map[string]string{
	"mode":             "advanced",
	"allowed-versions": "7.13+",
	"flagged":          "no",
	"flagging-enabled": "yes",
	"attempt-count":    "0",
	"scheduler":        "true",
	"fetch":            "false",
	"container":        "no",
	"app":              "yes",
	"update-time":      "never",
}

func decodeDeviceModeState(t *testing.T, res *schema.Resource, d *schema.ResourceData) cty.Value {
	t.Helper()
	stateVal, err := schema.StateValueFromInstanceState(d.State(), res.CoreConfigSchema().ImpliedType())
	if err != nil {
		t.Fatalf("StateValueFromInstanceState failed: %v", err)
	}
	return stateVal
}

func assertPopulatedDeviceModeState(t *testing.T, res *schema.Resource, d *schema.ResourceData) {
	t.Helper()
	if d.Id() != "/system/device-mode" {
		t.Fatalf("unexpected id %q", d.Id())
	}

	val := decodeDeviceModeState(t, res, d)
	attrs := val.AsValueMap()

	assertStringAttr := func(name, want string) {
		t.Helper()
		v, ok := attrs[name]
		if !ok || v.IsNull() {
			t.Fatalf("%s: expected %q, got null/missing", name, want)
		}
		if v.AsString() != want {
			t.Fatalf("%s: expected %q, got %q", name, want, v.AsString())
		}
	}

	assertBoolAttr := func(name string, want bool) {
		t.Helper()
		v, ok := attrs[name]
		if !ok || v.IsNull() {
			t.Fatalf("%s: expected explicit %t, got null/missing", name, want)
		}
		if v.True() != want {
			t.Fatalf("%s: expected %t, got %t", name, want, v.True())
		}
	}

	assertNullAttr := func(name string) {
		t.Helper()
		v, ok := attrs[name]
		if !ok {
			t.Fatalf("%s: attribute missing from schema object", name)
		}
		if !v.IsNull() {
			t.Fatalf("%s: expected null when omitted by RouterOS, got %#v", name, v)
		}
	}

	assertStringAttr("mode", "advanced")
	assertStringAttr("allowed_versions", "7.13+")

	attemptCount := attrs["attempt_count"]
	if attemptCount.IsNull() {
		t.Fatal("attempt_count: expected non-null 0, got null")
	}
	if bf := attemptCount.AsBigFloat(); bf.String() != "0" {
		t.Fatalf("attempt_count: expected 0, got %s", bf.String())
	}

	assertBoolAttr("flagged", false)
	assertBoolAttr("flagging_enabled", true)
	assertBoolAttr("scheduler", true)
	assertBoolAttr("fetch", false)
	assertBoolAttr("container", false)

	for _, omitted := range []string{"routerboard", "partitions", "zerotier"} {
		assertNullAttr(omitted)
	}

	featuresVal := attrs["features"]
	if featuresVal.IsNull() {
		t.Fatal("features map: expected non-null map")
	}
	features := featuresVal.AsValueMap()
	for k, want := range map[string]bool{
		"scheduler": true,
		"fetch":     false,
		"container": false,
		"app":       true,
	} {
		got, exists := features[k]
		if !exists || got.IsNull() {
			t.Fatalf("features[%q]: expected %t, got missing/null", k, want)
		}
		if got.True() != want {
			t.Fatalf("features[%q]: expected %t, got %t", k, want, got.True())
		}
	}
	for _, omitted := range []string{"routerboard", "partitions", "zerotier", "update_time", "mode", "flagged"} {
		if _, exists := features[omitted]; exists {
			t.Fatalf("features[%q]: unexpected entry for %q", omitted, omitted)
		}
	}
}

func newMockAPIDeviceModeClient(t *testing.T, respond func(w proto.Writer)) *ApiClient {
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
		r := proto.NewReader(peer)
		s, err := r.ReadSentence()
		if err != nil {
			return
		}
		if s.Word != "/system/device-mode/print" {
			w := proto.NewWriter(peer)
			w.BeginSentence()
			w.WriteWord("!trap")
			w.WriteWord("=message=unexpected command " + s.Word)
			_ = w.EndSentence()
			w.BeginSentence()
			w.WriteWord("!done")
			_ = w.EndSentence()
			return
		}
		w := proto.NewWriter(peer)
		respond(w)
	}()

	return &ApiClient{
		ctx:       context.Background(),
		Transport: TransportAPI,
		pool:      pool,
	}
}

func newMockRESTDeviceModeClient(t *testing.T, status int, body string) *RestClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/rest/system/device-mode" {
			t.Errorf("unexpected REST request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
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

func assertDiagHasErrorSubstring(t *testing.T, diags diag.Diagnostics, substr string) {
	t.Helper()
	if !diags.HasError() {
		t.Fatalf("expected error diagnostics containing %q, got %+v", substr, diags)
	}
	var combined strings.Builder
	for _, d := range diags {
		combined.WriteString(d.Summary)
		combined.WriteByte(' ')
		combined.WriteString(d.Detail)
		combined.WriteByte('\n')
	}
	if !strings.Contains(combined.String(), substr) {
		t.Fatalf("diagnostics %q did not contain %q", combined.String(), substr)
	}
}

func TestDatasourceSystemDeviceModeUnit(t *testing.T) {
	res := DatasourceSystemDeviceMode()

	restPayloads := map[string]string{
		"object": `{"mode":"advanced","allowed-versions":"7.13+","flagged":"no","flagging-enabled":"yes","attempt-count":"0","scheduler":"true","fetch":"false","container":"no","app":"yes","update-time":"never"}`,
		"array":  `[{"mode":"advanced","allowed-versions":"7.13+","flagged":"no","flagging-enabled":"yes","attempt-count":"0","scheduler":"true","fetch":"false","container":"no","app":"yes","update-time":"never"}]`,
	}

	for shape, payload := range restPayloads {
		t.Run("REST/"+shape, func(t *testing.T) {
			client := newMockRESTDeviceModeClient(t, http.StatusOK, payload)
			d := res.TestResourceData()
			diags := res.ReadContext(context.Background(), d, client)
			if len(diags) != 0 {
				t.Fatalf("expected zero diagnostics, got %+v", diags)
			}
			assertPopulatedDeviceModeState(t, res, d)
		})
	}

	t.Run("API/populated", func(t *testing.T) {
		client := newMockAPIDeviceModeClient(t, func(w proto.Writer) {
			w.BeginSentence()
			w.WriteWord("!re")
			for k, v := range populatedDeviceModeFields {
				w.WriteWord("=" + k + "=" + v)
			}
			_ = w.EndSentence()
			w.BeginSentence()
			w.WriteWord("!done")
			_ = w.EndSentence()
		})
		d := res.TestResourceData()
		diags := res.ReadContext(context.Background(), d, client)
		if len(diags) != 0 {
			t.Fatalf("expected zero diagnostics, got %+v", diags)
		}
		assertPopulatedDeviceModeState(t, res, d)
	})

	t.Run("REST/http_error", func(t *testing.T) {
		client := newMockRESTDeviceModeClient(t, http.StatusInternalServerError, `{"error":500,"message":"Internal Server Error","detail":"device-mode unavailable"}`)
		d := res.TestResourceData()
		diags := res.ReadContext(context.Background(), d, client)
		assertDiagHasErrorSubstring(t, diags, "/system/device-mode")
	})

	t.Run("API/trap_error", func(t *testing.T) {
		client := newMockAPIDeviceModeClient(t, func(w proto.Writer) {
			w.BeginSentence()
			w.WriteWord("!trap")
			w.WriteWord("=message=not allowed")
			_ = w.EndSentence()
			w.BeginSentence()
			w.WriteWord("!done")
			_ = w.EndSentence()
		})
		d := res.TestResourceData()
		diags := res.ReadContext(context.Background(), d, client)
		assertDiagHasErrorSubstring(t, diags, "not allowed")
	})

	for _, emptyCase := range []struct {
		name string
		body string
	}{
		{name: "empty_object", body: `{}`},
		{name: "empty_array", body: `[]`},
	} {
		t.Run("REST/"+emptyCase.name, func(t *testing.T) {
			client := newMockRESTDeviceModeClient(t, http.StatusOK, emptyCase.body)
			d := res.TestResourceData()
			diags := res.ReadContext(context.Background(), d, client)
			assertDiagHasErrorSubstring(t, diags, "no device-mode data")
		})
	}

	t.Run("API/empty", func(t *testing.T) {
		client := newMockAPIDeviceModeClient(t, func(w proto.Writer) {
			w.BeginSentence()
			w.WriteWord("!done")
			_ = w.EndSentence()
		})
		d := res.TestResourceData()
		diags := res.ReadContext(context.Background(), d, client)
		assertDiagHasErrorSubstring(t, diags, "no device-mode data")
	})

	t.Run("REST/malformed_bool", func(t *testing.T) {
		client := newMockRESTDeviceModeClient(t, http.StatusOK, `{"mode":"advanced","scheduler":"maybe"}`)
		d := res.TestResourceData()
		diags := res.ReadContext(context.Background(), d, client)
		assertDiagHasErrorSubstring(t, diags, "expected boolean")
	})

	t.Run("REST/malformed_attempt_count", func(t *testing.T) {
		client := newMockRESTDeviceModeClient(t, http.StatusOK, `{"mode":"advanced","attempt-count":"abc"}`)
		d := res.TestResourceData()
		diags := res.ReadContext(context.Background(), d, client)
		assertDiagHasErrorSubstring(t, diags, "expected integer")
	})
}

func TestAccDatasourceSystemDeviceMode_basic(t *testing.T) {
	boolRe := regexp.MustCompile(`^(true|false)$`)

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
						Config: testAccDatasourceSystemDeviceModeConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testDatasourceSystemDeviceModeCurrent),
							resource.TestCheckResourceAttrSet(testDatasourceSystemDeviceModeCurrent, "mode"),
							resource.TestMatchResourceAttr(testDatasourceSystemDeviceModeCurrent, "scheduler", boolRe),
							resource.TestMatchResourceAttr(testDatasourceSystemDeviceModeCurrent, "fetch", boolRe),
							resource.TestMatchResourceAttr(testDatasourceSystemDeviceModeCurrent, "features.scheduler", boolRe),
							resource.TestMatchResourceAttr(testDatasourceSystemDeviceModeCurrent, "features.fetch", boolRe),
						),
					},
				},
			})
		})
	}
}

func testAccDatasourceSystemDeviceModeConfig() string {
	return providerConfig + `
data "routeros_system_device_mode" "current" {}
`
}
