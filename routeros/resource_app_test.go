package routeros

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-routeros/routeros/v3/proto"
	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-log/tflogtest"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const appTestYAML = "name: tf-test\nservices:\n  web:\n    image: docker.io/library/alpine:3.22\n    privileged: true\n    environment:\n      ROUTER: '[networkRouterIP]'\n      TOKEN: arbitrary-secret\n"

func TestAppYAML(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{appTestYAML, true},
		{"name: example\nservices: {web: {image: 'example/image:1', future_option: true}}", true},
		{"name: missing-services", false},
		{"services: {web: {image: example}}", false},
		{"name: empty\nservices: {}", false},
		{"name: example\nservices: [web]", false},
		{"name: example\nservices: {web: {image: example}}\n---\nname: second", false},
		{"name: example\nname: duplicate\nservices: {}", false},
		{"[invalid arbitrary-secret", false},
	} {
		_, errs := validateAppYAML(tc.value, "yaml")
		if (len(errs) == 0) != tc.valid {
			t.Errorf("valid=%v, errors=%v", tc.valid, errs)
		}
		for _, err := range errs {
			if strings.Contains(err.Error(), "arbitrary-secret") {
				t.Fatal("YAML parser leaked source")
			}
		}
	}
	canonical := "services: {web: {environment: {TOKEN: arbitrary-secret, ROUTER: '[networkRouterIP]'}, privileged: true, image: 'docker.io/library/alpine:3.22'}}\nname: tf-test\n"
	if !appYAMLEqual("yaml", appTestYAML, canonical, nil) {
		t.Fatal("format-only YAML change causes drift")
	}
	if appYAMLEqual("yaml", appTestYAML, strings.ReplaceAll(canonical, "privileged: true", "privileged: false"), nil) {
		t.Fatal("service change was suppressed")
	}
}

type appTestClient struct {
	row    MikrotikItem
	writes []MikrotikItem
	paths  []string
}

func (c *appTestClient) GetTransport() TransportType  { return TransportREST }
func (c *appTestClient) GetExtraParams() *ExtraParams { return &ExtraParams{} }
func (c *appTestClient) SendRequest(method crudMethod, url *URL, body MikrotikItem, out interface{}) error {
	c.paths = append(c.paths, url.Path)
	if method == crudCreate || method == crudUpdate {
		copy := MikrotikItem{}
		for k, v := range body {
			copy[k] = v
		}
		c.writes = append(c.writes, copy)
		if c.row == nil {
			c.row = MikrotikItem{".id": "*A", "name": "tf-test", "custom": "true"}
		}
		for k, v := range body {
			c.row[k] = v
		}
	}
	if method == crudDelete {
		c.row = nil
	}
	switch result := out.(type) {
	case *MikrotikItem:
		for k, v := range c.row {
			(*result)[k] = v
		}
	case *[]MikrotikItem:
		if c.row != nil {
			*result = append(*result, c.row)
		}
	}
	return nil
}

func TestAppLifecycle(t *testing.T) {
	r := ResourceApp()
	d := resourceDataWithRawConfig(r, "", map[string]cty.Value{"yaml": cty.StringVal(appTestYAML)})
	if err := d.Set("disabled", true); err != nil {
		t.Fatal(err)
	}
	c := &appTestClient{}
	if ds := r.CreateContext(context.Background(), d, c); ds.HasError() {
		t.Fatal(ds)
	}
	if d.Id() != "*A" || len(c.writes) != 1 || c.writes[0]["yaml"] != appTestYAML || c.writes[0]["disabled"] != "yes" {
		t.Fatalf("create = %v, id=%s", c.writes, d.Id())
	}
	next := strings.ReplaceAll(appTestYAML, "privileged: true", "privileged: false")
	update := resourceDataWithRawConfig(r, "*A", map[string]cty.Value{"yaml": cty.StringVal(next)})
	if ds := r.UpdateContext(context.Background(), update, c); ds.HasError() {
		t.Fatal(ds)
	}
	if len(c.writes) != 2 || c.writes[1]["yaml"] != next {
		t.Fatal("update lost planned YAML while checking existing App")
	}
	if ds := r.ReadContext(context.Background(), update, c); ds.HasError() {
		t.Fatal(ds)
	}
	if len(c.writes) != 2 {
		t.Fatal("refresh mutated App")
	}
	if ds := r.DeleteContext(context.Background(), update, c); ds.HasError() {
		t.Fatal(ds)
	}
	if update.Id() != "" || c.row != nil {
		t.Fatal("delete did not remove custom definition")
	}
	for _, path := range c.paths {
		if strings.Contains(path, "cleanup") || strings.Contains(path, "restart") || strings.Contains(path, "update") {
			t.Fatalf("implicit lifecycle action: %s", path)
		}
	}
}

func TestAppRejectsCatalogMutation(t *testing.T) {
	for _, operation := range []string{"read", "update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			r := ResourceApp()
			d := resourceDataWithRawConfig(r, "*1", map[string]cty.Value{"yaml": cty.StringVal(appTestYAML)})
			c := &appTestClient{row: MikrotikItem{".id": "*1", "name": "catalog", "custom": "false", "yaml": appTestYAML}}
			f := r.ReadContext
			if operation == "update" {
				f = schema.ReadContextFunc(r.UpdateContext)
			}
			if operation == "delete" {
				f = schema.ReadContextFunc(r.DeleteContext)
			}
			if ds := f(context.Background(), d, c); !ds.HasError() {
				t.Fatal("catalog entry accepted")
			}
			if len(c.writes) != 0 || c.row == nil {
				t.Fatal("catalog mutated")
			}
		})
	}
}

func TestAppActionLifecycle(t *testing.T) {
	for _, action := range []string{"restart", "update", "cleanup"} {
		t.Run(action, func(t *testing.T) {
			r := newAPI725Action("/app/" + action)
			for _, empty := range []string{"", "  "} {
				if _, errs := r.Schema["numbers"].ValidateFunc(empty, "numbers"); len(errs) == 0 {
					t.Fatal("empty App selector accepted")
				}
			}
			d := resourceDataWithRawConfig(r, "", map[string]cty.Value{"numbers": cty.StringVal("*A")})
			c := &api725Recorder{}
			if ds := r.CreateContext(context.Background(), d, c); ds.HasError() {
				t.Fatal(ds)
			}
			if c.method != crudExecute || c.path != "/app/"+action || c.body["numbers"] != "*A" {
				t.Fatalf("action=%#v", c)
			}
			if ds := r.ReadContext(context.Background(), d, c); ds.HasError() {
				t.Fatal(ds)
			}
			if ds := r.DeleteContext(context.Background(), d, c); ds.HasError() {
				t.Fatal(ds)
			}
			if c.calls != 1 {
				t.Fatal("refresh or delete reran action")
			}
		})
	}
}

func TestRESTAppBodyPrivacy(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "error"}[fail], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body MikrotikItem
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["yaml"] != appTestYAML {
					t.Error("transport changed YAML")
				}
				w.Header().Set("Content-Type", "application/json")
				if fail {
					w.WriteHeader(400)
					_, _ = w.Write([]byte(`{"error":400,"message":"arbitrary-secret","detail":"arbitrary-secret"}`))
					return
				}
				_ = json.NewEncoder(w).Encode(MikrotikItem{".id": "*A", "yaml": appTestYAML, "unknown-server-field": "arbitrary-secret"})
			}))
			defer server.Close()
			var logs bytes.Buffer
			c := &RestClient{ctx: tflogtest.RootLogger(context.Background(), &logs), HostURL: server.URL, Transport: TransportREST, Client: server.Client()}
			row := MikrotikItem{}
			err := c.SendRequest(crudCreate, &URL{Path: "/app"}, MikrotikItem{"yaml": appTestYAML}, &row)
			if fail != (err != nil) {
				t.Fatalf("error = %v", err)
			}
			if strings.Contains(logs.String(), "arbitrary-secret") || (err != nil && strings.Contains(err.Error(), "arbitrary-secret")) {
				t.Fatal("App body leaked into log or diagnostic")
			}
			if !fail && row["yaml"] != appTestYAML {
				t.Fatal("response was redacted instead of log")
			}
		})
	}
}

func TestAPIAppBodyPrivacy(t *testing.T) {
	f := &fakeDialer{}
	pool := newAPIPool(f.dial, 2*time.Second, 1)
	session, err := pool.acquire()
	if err != nil {
		t.Fatal(err)
	}
	pool.release(session, true)
	peer := f.peer(0)
	t.Cleanup(func() { _ = peer.Close() })
	received := make(chan string, 1)
	go func() {
		r := proto.NewReader(peer)
		sentence, err := r.ReadSentence()
		if err != nil {
			received <- ""
			return
		}
		received <- sentence.Map["yaml"]
		w := proto.NewWriter(peer)
		w.BeginSentence()
		w.WriteWord("!re")
		w.WriteWord("=.id=*A")
		w.WriteWord("=yaml=" + appTestYAML)
		w.WriteWord("=unknown-field=arbitrary-secret")
		_ = w.EndSentence()
		w.BeginSentence()
		w.WriteWord("!done")
		_ = w.EndSentence()
	}()
	var logs bytes.Buffer
	c := &ApiClient{ctx: tflogtest.RootLogger(context.Background(), &logs), Transport: TransportAPI, pool: pool}
	row := MikrotikItem{}
	if err := c.SendRequest(crudCreate, &URL{Path: "/app"}, MikrotikItem{"yaml": appTestYAML}, &row); err != nil {
		t.Fatal(err)
	}
	if <-received != appTestYAML || row["yaml"] != appTestYAML {
		t.Fatal("wire YAML was changed")
	}
	if strings.Contains(logs.String(), "arbitrary-secret") {
		t.Fatal("App response/request leaked")
	}
}

func TestAppActionPlanning(t *testing.T) {
	for _, action := range []string{"restart", "update", "cleanup"} {
		r := newAPI725Action("/app/" + action)
		for _, next := range []string{"*A", "*B"} {
			config := api725ActionConfig(r, map[string]cty.Value{"numbers": cty.StringVal(next)})
			state := &terraform.InstanceState{ID: "app-action", Attributes: map[string]string{"id": "app-action", "numbers": "*A"}, RawConfig: config.CtyValue}
			diff, err := r.Diff(context.Background(), state, config, nil)
			if err != nil {
				t.Fatal(err)
			}
			if next == "*A" {
				if diff != nil && !diff.Empty() {
					t.Fatalf("%s: unchanged action reran: %#v", action, diff)
				}
				continue
			}
			if diff == nil || diff.Attributes["numbers"] == nil {
				t.Fatal("selector change suppressed")
			}
			c := &api725Recorder{}
			if _, ds := r.Apply(context.Background(), state, diff, c); ds.HasError() {
				t.Fatal(ds)
			}
			if c.calls != 1 || c.body["numbers"] != "*B" {
				t.Fatal("selector update did not execute once")
			}
		}
	}
}

func TestAppCapturedContract(t *testing.T) {
	var inventory struct {
		Contracts map[string]struct {
			Arguments []string `json:"arguments"`
		} `json:"runtime_contracts"`
	}
	raw, err := os.ReadFile("../tools/schema_changes/routeros-7.25-features.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &inventory); err != nil {
		t.Fatal(err)
	}
	for path, r := range map[string]*schema.Resource{
		"app,add": ResourceApp(), "app,set": ResourceApp(), "app,settings,set": ResourceAppSettings(),
		"app,restart": newAPI725Action("/app/restart"), "app,update": newAPI725Action("/app/update"), "app,cleanup": newAPI725Action("/app/cleanup"),
	} {
		t.Run(path, func(t *testing.T) {
			contract, ok := inventory.Contracts[path]
			if !ok {
				t.Fatal("missing independent live contract")
			}
			args := map[string]bool{}
			for _, wire := range contract.Arguments {
				args[wire] = true
				// copy-from duplicates an existing entry instead of declaring YAML;
				// numbers for set is supplied by the common CRUD ID transport.
				if wire == "copy-from" || (wire == "numbers" && strings.HasSuffix(path, ",set")) {
					continue
				}
				if r.Schema[api725Name(wire)] == nil {
					t.Errorf("missing live argument %s", wire)
				}
			}
			for name, field := range r.Schema {
				if strings.HasPrefix(name, "___") || field.Computed || name == "triggers" || name == "arguments" {
					continue
				}
				if !args[SnakeToKebab(name)] {
					t.Errorf("unsupported writable argument %s", name)
				}
			}
		})
	}
}
