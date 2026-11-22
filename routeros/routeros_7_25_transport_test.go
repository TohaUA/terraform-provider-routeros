package routeros

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-routeros/routeros/v3/proto"
	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-log/tflogtest"
)

func TestAPI725CommandFramingAndDoneResult(t *testing.T) {
	f := &fakeDialer{}
	pool := newAPIPool(f.dial, 2*time.Second, 1)
	session, err := pool.acquire()
	if err != nil {
		t.Fatal(err)
	}
	pool.release(session, true)
	peer := f.peer(0)
	t.Cleanup(func() { _ = peer.Close() })
	words := make(chan []string, 1)
	go func() {
		r := proto.NewReader(peer)
		s, err := r.ReadSentence()
		if err != nil {
			words <- nil
			return
		}
		var command []string
		command = append(command, s.Word)
		for _, pair := range s.List {
			command = append(command, "="+pair.Key+"="+pair.Value)
		}
		words <- command
		w := proto.NewWriter(peer)
		w.BeginSentence()
		w.WriteWord("!done")
		w.WriteWord("=output=arbitrary-secret")
		_ = w.EndSentence()
	}()
	var logs bytes.Buffer
	client := &ApiClient{ctx: tflogtest.RootLogger(context.Background(), &logs), Transport: TransportAPI, pool: pool}
	var rows []MikrotikItem
	if err := client.SendRequest(crudExecute, &URL{Path: "/system/ssh-exec"}, MikrotikItem{"custom": "arbitrary-secret"}, &rows); err != nil {
		t.Fatal(err)
	}
	got := <-words
	if !reflect.DeepEqual(got, []string{"/system/ssh-exec", "=custom=arbitrary-secret"}) {
		t.Fatalf("command = %v", got)
	}
	if len(rows) != 1 || rows[0]["output"] != "arbitrary-secret" {
		t.Fatalf("!done result lost: %v", rows)
	}
	if strings.Contains(logs.String(), "arbitrary-secret") {
		t.Fatalf("command body/result leaked: %s", logs.String())
	}
}

func TestREST725CommandFramingAndErrors(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "error"}[fail], func(t *testing.T) {
			var received MikrotikItem
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/rest/tool/fetch" {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
					t.Error(err)
				}
				w.Header().Set("Content-Type", "application/json")
				if fail {
					w.WriteHeader(400)
					_, _ = w.Write([]byte(`{"error":400,"message":"arbitrary-secret","detail":"arbitrary-secret"}`))
					return
				}
				_, _ = w.Write([]byte(`{"data":"arbitrary-secret","status":"finished"}`))
			}))
			defer srv.Close()
			var logs bytes.Buffer
			c := &RestClient{ctx: tflogtest.RootLogger(context.Background(), &logs), HostURL: srv.URL, Transport: TransportREST, Client: srv.Client()}
			var rows []MikrotikItem
			err := c.SendRequest(crudExecute, &URL{Path: "/tool/fetch"}, MikrotikItem{"custom": "arbitrary-secret"}, &rows)
			if received["custom"] != "arbitrary-secret" {
				t.Fatalf("argument changed: %v", received)
			}
			if fail && err == nil {
				t.Fatal("device error ignored")
			}
			if !fail && (err != nil || len(rows) != 1 || rows[0]["data"] != "arbitrary-secret") {
				t.Fatalf("result=%v err=%v", rows, err)
			}
			if strings.Contains(logs.String(), "arbitrary-secret") || (err != nil && strings.Contains(err.Error(), "arbitrary-secret")) {
				t.Fatalf("command data leaked: log=%s err=%v", logs.String(), err)
			}
		})
	}
}

type api725Recorder struct {
	calls  int
	method crudMethod
	path   string
	body   MikrotikItem
	rows   []MikrotikItem
	err    error
}

func (c *api725Recorder) GetTransport() TransportType  { return TransportREST }
func (c *api725Recorder) GetExtraParams() *ExtraParams { return &ExtraParams{} }
func (c *api725Recorder) SendRequest(method crudMethod, url *URL, item MikrotikItem, result interface{}) error {
	c.calls++
	c.method = method
	c.path = url.Path
	c.body = item
	if c.err != nil {
		return c.err
	}
	if rows, ok := result.(*[]MikrotikItem); ok {
		*rows = append(*rows, c.rows...)
	}
	return nil
}

func TestAPI725ActionLifecycle(t *testing.T) {
	old := RouterOSVersion
	RouterOSVersion = "7.25.1"
	t.Cleanup(func() { RouterOSVersion = old })
	r := ResourceToolFetch()
	d := resourceDataWithRawConfig(r, "", map[string]cty.Value{"url": cty.StringVal("https://example.test/")})
	c := &api725Recorder{rows: []MikrotikItem{{"status": "connecting"}, {"status": "finished", "data": "payload"}}}
	if diags := r.CreateContext(context.Background(), d, c); diags.HasError() {
		t.Fatal(diags)
	}
	if c.method != crudExecute || c.path != "/tool/fetch" || c.body["url"] != "https://example.test/" || d.Id() == "" {
		t.Fatalf("wrong execution: %#v id=%s", c, d.Id())
	}
	if d.Get("status") != "finished" {
		t.Fatal("final command result was lost among progress replies")
	}
	if c.body["arguments"] != "" || c.body["triggers"] != "" {
		t.Fatal("Terraform control fields reached RouterOS")
	}
	if diags := r.ReadContext(context.Background(), d, c); diags.HasError() {
		t.Fatal(diags)
	}
	if c.calls != 1 {
		t.Fatal("refresh executed command")
	}
	if diags := r.UpdateContext(context.Background(), d, c); diags.HasError() {
		t.Fatal(diags)
	}
	if c.calls != 2 {
		t.Fatal("explicit update did not execute command")
	}
	c.rows = nil
	if diags := r.UpdateContext(context.Background(), d, c); diags.HasError() {
		t.Fatal(diags)
	}
	if d.Get("result_json") != "[]" || d.Get("status") != "" || d.Get("data") != "" {
		t.Fatal("a command without output retained stale results")
	}
	if diags := r.DeleteContext(context.Background(), d, c); diags.HasError() {
		t.Fatal(diags)
	}
	if c.calls != 3 || d.Id() != "" {
		t.Fatal("deletion executed a command or retained state")
	}
	c.err = errors.New("router rejected command")
	if diags := r.CreateContext(context.Background(), d, c); !diags.HasError() || d.Id() != "" {
		t.Fatal("failed command recorded as successful")
	}
	RouterOSVersion = "7.25"
	before := c.calls
	if diags := r.CreateContext(context.Background(), d, c); !diags.HasError() || c.calls != before {
		t.Fatal("unsupported router was called")
	}
}

func TestAPI725DatasourceDottedState(t *testing.T) {
	old := RouterOSVersion
	RouterOSVersion = "7.25.1"
	t.Cleanup(func() { RouterOSVersion = old })
	r := DatasourceRoutingPimsmInterfaces()
	d := r.TestResourceData()
	c := &api725Recorder{rows: []MikrotikItem{{"interface": "ether1", "gmp.state.robustness": "3", "gmp.querier": "true"}}}
	if diags := r.ReadContext(context.Background(), d, c); diags.HasError() {
		t.Fatal(diags)
	}
	rows := d.Get("data").([]interface{})
	row := rows[0].(map[string]interface{})
	if row["gmp_state_robustness"] != 3 || row["gmp_querier"] != true {
		t.Fatalf("dotted status lost: %v", row)
	}
	if c.method != crudRead {
		t.Fatal("read-only table executed a mutation")
	}
}
