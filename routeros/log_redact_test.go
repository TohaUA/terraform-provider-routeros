package routeros

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-routeros/routeros/v3"
	"github.com/go-routeros/routeros/v3/proto"
	"github.com/hashicorp/terraform-plugin-log/tflogtest"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// Every Sensitive field in the provider has to be in the redaction set, nested
// blocks included, or its value is logged in clear.
func TestSensitiveNamesCoverEverySensitiveField(t *testing.T) {
	loadSensitiveNames()
	if len(sensitiveNames) == 0 {
		t.Fatal("no sensitive field names were collected")
	}

	var walk func(owner string, s map[string]*schema.Schema)
	walk = func(owner string, s map[string]*schema.Schema) {
		for name, field := range s {
			if field.Sensitive {
				if _, ok := sensitiveNames[SnakeToKebab(name)]; !ok {
					t.Errorf("%s.%s is Sensitive but would be logged in clear", owner, name)
				}
			}
			if elem, ok := field.Elem.(*schema.Resource); ok {
				walk(owner+"."+name, elem.Schema)
			}
		}
	}

	p := Provider()
	for name, r := range p.ResourcesMap {
		walk(name, r.Schema)
	}
	for name, r := range p.DataSourcesMap {
		walk(name, r.Schema)
	}
}

func TestRedactAPIWords(t *testing.T) {
	words := []string{
		"/ppp/secret/add",
		"=name=alice",
		"=password=hunter2",
		"=private-key=abc=def=",   // a value containing '=' is redacted whole
		"=!password=",             // an unset has no value to hide
		"=input.auth-key=k3y",     // a sensitive name inside a nested block
		"=comment=password=nope",  // a sensitive word in a value is not a key
		"?.id=*1",                 // an id query is kept
		"?=password=f1lter",       // ReadItemsFiltered, for a filtered read or an import
		"?secret=qu3ry",           // a query on a property
		"?>preshared-key=gr3ater", // a comparison query
		"?#|",                     // a query operator has no value
	}
	sent := append([]string(nil), words...)

	logged := strings.Join(redactAPIWords(words), " ")

	for _, secret := range []string{"hunter2", "abc=def=", "k3y", "f1lter", "qu3ry", "gr3ater"} {
		if strings.Contains(logged, secret) {
			t.Errorf("the log still carries %q: %s", secret, logged)
		}
	}
	for _, kept := range []string{"/ppp/secret/add", "=name=alice", "=password=***", "=private-key=***",
		"=!password= ", "=input.auth-key=***", "=comment=password=nope", "?.id=*1",
		"?=password=***", "?secret=***", "?>preshared-key=***", "?#|"} {
		if !strings.Contains(logged+" ", kept) {
			t.Errorf("the log lost %q: %s", kept, logged)
		}
	}

	for i := range words {
		if words[i] != sent[i] {
			t.Fatalf("redacting the log changed word %d of the command sent to the router: %q -> %q", i, sent[i], words[i])
		}
	}
}

func TestRedactURL(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"https://r/rest/ppp/secret", "https://r/rest/ppp/secret"},
		{"https://r/rest/ppp/secret?name=alice&password=hunter2", "https://r/rest/ppp/secret?name=alice&password=***"},
		{"https://r/rest/ppp/secret?password=hun%20ter2&name=alice", "https://r/rest/ppp/secret?password=***&name=alice"},
		{"https://r/rest/ppp/secret?pass%77ord=hunter2", "https://r/rest/ppp/secret?pass%77ord=***"},
		// The query is not escaped, so a value can contain '&'.
		{"https://r/rest/ppp/secret?password=hun&ter2&name=alice", "https://r/rest/ppp/secret?password=***&name=alice"},
		{"https://r/rest/routing/bgp/connection?input.auth-key=k3y", "https://r/rest/routing/bgp/connection?input.auth-key=***"},
		{"https://r/rest/ppp/secret?password=&comment=password", "https://r/rest/ppp/secret?password=&comment=password"},
		{"https://r/rest/interface/vlan?.id=*39", "https://r/rest/interface/vlan?.id=*39"},
	} {
		if got := redactURL(tc.raw); got != tc.want {
			t.Errorf("redactURL(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

// A filtered read or an import puts its filter in the REST query string. The
// router has to receive it as it is, but neither the debug log nor the error
// returned for a failed request may carry a sensitive value from it.
func TestRESTRequestKeepsASensitiveQueryOutOfLogsAndErrors(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":400,"message":"Bad Request","detail":"no such item"}`))
	}))
	defer srv.Close()

	var logs bytes.Buffer
	c := &RestClient{
		ctx:       tflogtest.RootLogger(context.Background(), &logs),
		HostURL:   srv.URL,
		Transport: TransportREST,
		Client:    srv.Client(),
	}

	var res []MikrotikItem
	err := c.SendRequest(crudRead, &URL{Path: "/ppp/secret", Query: []string{"name=alice", "password=hunter2"}}, nil, &res)

	if gotQuery != "name=alice&password=hunter2" {
		t.Fatalf("the router got the query %q; redacting the log must not change the request", gotQuery)
	}
	if err == nil {
		t.Fatal("want the error for the 400 response")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("the returned error carries the password: %v", err)
	}
	if !strings.Contains(err.Error(), "password=***") {
		t.Errorf("the returned error lost the redacted query: %v", err)
	}
	if strings.Contains(logs.String(), "hunter2") {
		t.Errorf("the debug log carries the password:\n%s", logs.String())
	}
}

// A filtered read or an import sends its filter as "?=name=value" query words.
// The router has to receive them as they are, but the debug log must not carry a
// sensitive value from them.
func TestAPIRequestKeepsSensitiveQueryWordsOutOfLogs(t *testing.T) {
	f := &fakeDialer{}
	pool := newAPIPool(f.dial, time.Second, 1)
	s, err := pool.acquire()
	if err != nil {
		t.Fatal(err)
	}
	pool.release(s, true)

	sent := make(chan []byte, 1)
	go func() {
		peer := f.peer(0)
		buf := make([]byte, 4096)
		n, err := peer.Read(buf)
		if err != nil {
			sent <- nil
			return
		}
		sent <- buf[:n]
		w := proto.NewWriter(peer)
		w.BeginSentence()
		w.WriteWord("!done")
		_ = w.EndSentence()
	}()

	var logs bytes.Buffer
	c := &ApiClient{ctx: tflogtest.RootLogger(context.Background(), &logs), Transport: TransportAPI, pool: pool}

	var res []MikrotikItem
	if err := c.SendRequest(crudRead, &URL{Path: "/ppp/secret", Query: []string{"?=name=alice", "?=password=hunter2"}}, nil, &res); err != nil {
		t.Fatal(err)
	}

	if raw := <-sent; !bytes.Contains(raw, []byte("?=password=hunter2")) {
		t.Fatalf("the router did not get the query word as it was built; got %q", raw)
	}
	if strings.Contains(logs.String(), "hunter2") {
		t.Errorf("the debug log carries the password:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "?=password=***") {
		t.Errorf("the debug log lost the redacted query word:\n%s", logs.String())
	}
}

func TestRedactReply(t *testing.T) {
	reply := &routeros.Reply{
		Re: []*proto.Sentence{{
			Word: "!re",
			List: []proto.Pair{{Key: ".id", Value: "*1"}, {Key: "name", Value: "alice"}, {Key: "password", Value: "hunter2"}},
			Map:  map[string]string{".id": "*1", "name": "alice", "password": "hunter2"},
		}},
		Done: &proto.Sentence{Word: "!done"},
	}

	logged := redactReply(reply)

	if strings.Contains(logged, "hunter2") {
		t.Errorf("the logged reply still carries the password: %s", logged)
	}
	if !strings.Contains(logged, "alice") || !strings.Contains(logged, "!done") {
		t.Errorf("the logged reply lost non-sensitive content: %s", logged)
	}
	if reply.Re[0].List[2].Value != "hunter2" || reply.Re[0].Map["password"] != "hunter2" {
		t.Fatal("redacting the log changed the reply the provider decodes")
	}
}

func TestRedactJSON(t *testing.T) {
	body := []byte(`[{".id":"*1","name":"alice","password":"hun\"ter2","private-key":"abc",` +
		`"comment":"the password is not here","!password":"","input.auth-key":"k3y"}]`)
	original := string(body)

	logged := redactJSON(body)

	for _, secret := range []string{`hun\"ter2`, `"abc"`, `k3y`} {
		if strings.Contains(logged, secret) {
			t.Errorf("the log still carries %s: %s", secret, logged)
		}
	}
	for _, kept := range []string{`"name":"alice"`, `"password":"***"`, `"private-key":"***"`,
		`"comment":"the password is not here"`, `"!password":""`, `"input.auth-key":"***"`} {
		if !strings.Contains(logged, kept) {
			t.Errorf("the log lost %s: %s", kept, logged)
		}
	}
	if string(body) != original {
		t.Fatal("redacting the log changed the body that is decoded")
	}
}
