package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
