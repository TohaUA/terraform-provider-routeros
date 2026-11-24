package routeros

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/go-routeros/routeros/v3/proto"
)

func TestGetRestURL(t *testing.T) {
	tests := []struct {
		name string
		url  URL
		want string
	}{
		{
			name: "empty query",
			url:  URL{Path: "/ip/pool"},
			want: "/ip/pool",
		},
		{
			name: "empty slice query",
			url:  URL{Path: "/ip/pool", Query: []string{}},
			want: "/ip/pool",
		},
		{
			name: "empty entry query",
			url:  URL{Path: "/ip/pool", Query: []string{""}},
			want: "/ip/pool",
		},
		{
			name: "hash in value",
			url:  URL{Path: "/ip/pool", Query: []string{"name=web#2"}},
			want: "/ip/pool?name=web%232",
		},
		{
			name: "ampersand and equals in value",
			url:  URL{Path: "/ip/pool", Query: []string{"name=a&disabled=yes"}},
			want: "/ip/pool?name=a%26disabled%3Dyes",
		},
		{
			name: "plus in value",
			url:  URL{Path: "/ip/pool", Query: []string{"name=a+b"}},
			want: "/ip/pool?name=a%2Bb",
		},
		{
			name: "percent in value",
			url:  URL{Path: "/ip/pool", Query: []string{"name=100%"}},
			want: "/ip/pool?name=100%25",
		},
		{
			name: "space in value",
			url:  URL{Path: "/ip/pool", Query: []string{"name=a b"}},
			want: "/ip/pool?name=a%20b",
		},
		{
			name: "equals in value",
			url:  URL{Path: "/ip/pool", Query: []string{"comment=a=b"}},
			want: "/ip/pool?comment=a%3Db",
		},
		{
			name: "preserves parameter order",
			url:  URL{Path: "/ip/pool", Query: []string{"name=web#2", "comment=a&b"}},
			want: "/ip/pool?name=web%232&comment=a%26b",
		},
		{
			name: "preserves reverse parameter order",
			url:  URL{Path: "/ip/pool", Query: []string{"comment=a&b", "name=web#2"}},
			want: "/ip/pool?comment=a%26b&name=web%232",
		},
		{
			name: "escapes key and value",
			url:  URL{Path: "/ip/pool", Query: []string{"my key=val 1"}},
			want: "/ip/pool?my%20key=val%201",
		},
		{
			name: "ReadItems leading question mark with id",
			url:  URL{Path: "/interface/vlan", Query: []string{"?.id=*39"}},
			want: "/interface/vlan?.id=%2A39",
		},
		{
			name: "ReadItems leading question mark with name",
			url:  URL{Path: "/ip/pool", Query: []string{"?name=web#2"}},
			want: "/ip/pool?name=web%232",
		},
		{
			name: "bare entry without equals",
			url:  URL{Path: "/ip/pool", Query: []string{"bare#flag"}},
			want: "/ip/pool?bare%23flag",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.url.GetRestURL(); got != tc.want {
				t.Errorf("URL(%+v).GetRestURL() = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}

func TestGetApiCmd(t *testing.T) {
	u := &URL{
		Path: "/ip/pool",
		Query: []string{
			"?=name=web#2",
			"?=name=a&disabled=yes",
			"?=name=a+b",
			"?=name=100%",
			"?=name=a b",
			"?.id=*39",
		},
	}
	want := []string{
		"/ip/pool",
		"?=name=web#2",
		"?=name=a&disabled=yes",
		"?=name=a+b",
		"?=name=100%",
		"?=name=a b",
		"?.id=*39",
	}

	if got := u.GetApiCmd(); !reflect.DeepEqual(got, want) {
		t.Errorf("GetApiCmd() = %#v, want %#v", got, want)
	}
}

func TestRESTClientQueryEscaping(t *testing.T) {
	values := []string{
		"web#2",
		"a&disabled=yes",
		"a+b",
		"100%",
		"a b",
		"a=b",
	}

	for _, val := range values {
		t.Run("SendRequest/"+val, func(t *testing.T) {
			var gotQuery url.Values
			var gotFragment string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.Query()
				gotFragment = r.URL.Fragment
				_, _ = w.Write([]byte(`[]`))
			}))
			defer srv.Close()

			c := &RestClient{
				ctx:       context.Background(),
				HostURL:   srv.URL,
				Transport: TransportREST,
				Client:    srv.Client(),
			}

			var res []MikrotikItem
			if err := c.SendRequest(crudRead, &URL{Path: "/ip/pool", Query: []string{"name=" + val}}, nil, &res); err != nil {
				t.Fatalf("SendRequest error = %v", err)
			}
			if gotFragment != "" {
				t.Errorf("r.URL.Fragment = %q, want empty", gotFragment)
			}
			if len(gotQuery) != 1 || gotQuery.Get("name") != val {
				t.Errorf("r.URL.Query() = %v, want single param name=%q", gotQuery, val)
			}
		})

		t.Run("ReadItemsFiltered/"+val, func(t *testing.T) {
			var gotQuery url.Values
			var gotFragment string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.Query()
				gotFragment = r.URL.Fragment
				_, _ = w.Write([]byte(`[]`))
			}))
			defer srv.Close()

			c := &RestClient{
				ctx:       context.Background(),
				HostURL:   srv.URL,
				Transport: TransportREST,
				Client:    srv.Client(),
			}

			if _, err := ReadItemsFiltered([]string{"name=" + val}, "/ip/pool", c); err != nil {
				t.Fatalf("ReadItemsFiltered error = %v", err)
			}
			if gotFragment != "" {
				t.Errorf("r.URL.Fragment = %q, want empty", gotFragment)
			}
			if len(gotQuery) != 1 || gotQuery.Get("name") != val {
				t.Errorf("r.URL.Query() = %v, want single param name=%q", gotQuery, val)
			}
		})
	}

	t.Run("ReadItems/NameAndId", func(t *testing.T) {
		for _, tc := range []struct {
			id      ItemId
			wantKey string
		}{
			{id: ItemId{Type: Name, Value: "web#2"}, wantKey: "name"},
			{id: ItemId{Type: Name, Value: "a&disabled=yes"}, wantKey: "name"},
			{id: ItemId{Type: Id, Value: "*39"}, wantKey: ".id"},
		} {
			var gotQuery url.Values
			var gotFragment string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.Query()
				gotFragment = r.URL.Fragment
				_, _ = w.Write([]byte(`[]`))
			}))

			c := &RestClient{
				ctx:       context.Background(),
				HostURL:   srv.URL,
				Transport: TransportREST,
				Client:    srv.Client(),
			}

			_, err := ReadItems(&tc.id, "/ip/pool", c)
			srv.Close()
			if err != nil {
				t.Fatalf("ReadItems(%+v) error = %v", tc.id, err)
			}
			if gotFragment != "" {
				t.Errorf("r.URL.Fragment = %q, want empty", gotFragment)
			}
			if len(gotQuery) != 1 || gotQuery.Get(tc.wantKey) != tc.id.Value {
				t.Errorf("r.URL.Query() = %v, want single param %s=%q", gotQuery, tc.wantKey, tc.id.Value)
			}
		}
	})
}

func TestRESTCRUDPathEscaping(t *testing.T) {
	tests := []struct {
		name            string
		id              string
		wantEscapedPath string
	}{
		{
			name:            "asterisk id",
			id:              "*39",
			wantEscapedPath: "/rest/ip/pool/%2A39",
		},
		{
			name:            "id with path and query metacharacters",
			id:              "a/b#c?d=1%20",
			wantEscapedPath: "/rest/ip/pool/a%2Fb%23c%3Fd=1%2520",
		},
	}

	for _, tc := range tests {
		t.Run("UpdateItem/"+tc.name, func(t *testing.T) {
			var gotEscapedPath, gotRawQuery, gotFragment string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotEscapedPath = r.URL.EscapedPath()
				gotRawQuery = r.URL.RawQuery
				gotFragment = r.URL.Fragment
				_, _ = w.Write([]byte(`{".id":"*39"}`))
			}))
			defer srv.Close()

			c := &RestClient{
				ctx:       context.Background(),
				HostURL:   srv.URL,
				Transport: TransportREST,
				Client:    srv.Client(),
			}

			if _, err := UpdateItem(&ItemId{Type: Id, Value: tc.id}, "/ip/pool", MikrotikItem{"name": "p1"}, c); err != nil {
				t.Fatalf("UpdateItem error = %v", err)
			}
			if gotEscapedPath != tc.wantEscapedPath {
				t.Errorf("EscapedPath = %q, want %q", gotEscapedPath, tc.wantEscapedPath)
			}
			if gotRawQuery != "" || gotFragment != "" {
				t.Errorf("RawQuery = %q, Fragment = %q; want both empty", gotRawQuery, gotFragment)
			}
		})

		t.Run("DeleteItem/"+tc.name, func(t *testing.T) {
			var gotEscapedPath, gotRawQuery, gotFragment string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotEscapedPath = r.URL.EscapedPath()
				gotRawQuery = r.URL.RawQuery
				gotFragment = r.URL.Fragment
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()

			c := &RestClient{
				ctx:       context.Background(),
				HostURL:   srv.URL,
				Transport: TransportREST,
				Client:    srv.Client(),
			}

			if err := DeleteItem(&ItemId{Type: Id, Value: tc.id}, "/ip/pool", c); err != nil {
				t.Fatalf("DeleteItem error = %v", err)
			}
			if gotEscapedPath != tc.wantEscapedPath {
				t.Errorf("EscapedPath = %q, want %q", gotEscapedPath, tc.wantEscapedPath)
			}
			if gotRawQuery != "" || gotFragment != "" {
				t.Errorf("RawQuery = %q, Fragment = %q; want both empty", gotRawQuery, gotFragment)
			}
		})
	}
}

func TestAPIFilteredReadLeavesWordsRaw(t *testing.T) {
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

	c := &ApiClient{ctx: context.Background(), Transport: TransportAPI, pool: pool}
	if _, err := ReadItemsFiltered([]string{"name=web#2", "comment=a&disabled=yes"}, "/ip/pool", c); err != nil {
		t.Fatal(err)
	}

	raw := <-sent
	for _, want := range []string{"/ip/pool/print", "?=name=web#2", "?=comment=a&disabled=yes"} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Errorf("API wire command missing raw word %q: %q", want, raw)
		}
	}
}
