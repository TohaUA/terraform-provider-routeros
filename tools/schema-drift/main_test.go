package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runTool starts a fake device that answers the version probe and the given menus, and 404 for any
// other menu, then runs the tool with -fail-on-missing against the -resources selection.
func runTool(t *testing.T, resources string, menus map[string]func(w http.ResponseWriter)) (int, error, *Report) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/system/resource" {
			_, _ = w.Write([]byte(`{"version":"7.24 (stable)"}`))
			return
		}
		if serve, ok := menus[r.URL.Path]; ok {
			serve(w)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	clearConnEnv(t)
	t.Setenv("ROS_HOSTURL", srv.URL)
	t.Setenv("ROS_USERNAME", "reader")
	t.Setenv("ROS_PASSWORD", "secret")

	jsonOut := filepath.Join(t.TempDir(), "report.json")
	code, err := run("", "", resources, "", "", jsonOut, false, true, 5*time.Second, 2)

	var report *Report
	if raw, rErr := os.ReadFile(jsonOut); rErr == nil {
		report = &Report{}
		if jErr := json.Unmarshal(raw, report); jErr != nil {
			t.Fatalf("report json: %v", jErr)
		}
	}
	return code, err, report
}

// runAgainst runs the tool against /ip/settings, which answers normally, and /ip/service, which
// answers the way the caller decides.
func runAgainst(t *testing.T, service func(w http.ResponseWriter)) (int, error, *Report) {
	t.Helper()
	return runTool(t, "/ip/settings,/ip/service", map[string]func(w http.ResponseWriter){
		"/rest/ip/settings": func(w http.ResponseWriter) { _, _ = w.Write([]byte(`[]`)) },
		"/rest/ip/service":  service,
	})
}

// A menu that cannot be read must fail the run even though -fail-on-missing found no drift,
// and the report must still say which menu was unreadable.
func TestRunFailsWhenAMenuCannotBeRead(t *testing.T) {
	for _, tc := range []struct {
		name  string
		serve func(w http.ResponseWriter)
		want  string
	}{
		{"5xx", func(w http.ResponseWriter) { w.WriteHeader(http.StatusInternalServerError) }, "HTTP 500"},
		{"401", func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Unauthorized"}`))
		}, "Unauthorized"},
		{"403", func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"detail":"not enough permissions"}`))
		}, "not enough permissions"},
		{"malformed body", func(w http.ResponseWriter) { _, _ = w.Write([]byte(`[{"name":`)) }, "decode list"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, err, report := runAgainst(t, tc.serve)
			if code != 1 || err == nil {
				t.Fatalf("code = %d, err = %v; want 1 and an error", code, err)
			}
			if !strings.Contains(err.Error(), "/ip/service") {
				t.Errorf("error must name the menu: %v", err)
			}
			if report == nil {
				t.Fatal("report was not written")
			}
			if report.Summary.Failed != 1 || report.Summary.Skipped != 1 || report.Summary.Compared != 1 {
				t.Errorf("summary = %+v; want compared 1, skipped 1, failed 1", report.Summary)
			}
			var failed *Comparison
			for _, m := range report.Menus {
				if m.Failed {
					failed = m
				}
			}
			if failed == nil {
				t.Fatal("no menu marked failed in the report")
			}
			if failed.Path != "/ip/service" || !strings.HasPrefix(failed.Skipped, "GET failed: ") ||
				!strings.Contains(failed.Skipped, tc.want) {
				t.Errorf("failed menu = %+v; want /ip/service with %q", failed, tc.want)
			}
		})
	}
}

// A menu the device does not have stays a plain skip: exit 0, not counted as failed.
func TestRunSkipsAbsentMenu(t *testing.T) {
	for _, tc := range []struct {
		name  string
		serve func(w http.ResponseWriter)
	}{
		{"404", func(w http.ResponseWriter) { w.WriteHeader(http.StatusNotFound) }},
		{"no such command", func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"detail":"no such command or directory (service)"}`))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, err, report := runAgainst(t, tc.serve)
			if code != 0 || err != nil {
				t.Fatalf("code = %d, err = %v; want 0 and no error", code, err)
			}
			if report.Summary.Failed != 0 || report.Summary.Skipped != 1 || report.Summary.Compared != 1 {
				t.Errorf("summary = %+v; want compared 1, skipped 1, failed 0", report.Summary)
			}
		})
	}
}

// On a menu the provider maps with two schemas, -resources with one resource's name compares that
// schema only: the other is named in shared_with but gets no entry, so its fields cannot fail
// -fail-on-missing. The menu path compares both. A menu the device lacks keeps the same entries.
func TestRunResourceFilterOnASharedMenu(t *testing.T) {
	const (
		path  = "/interface/ethernet/switch"
		plain = "routeros_interface_ethernet_switch"
		crs   = "routeros_interface_ethernet_switch_crs"
	)
	present := map[string]func(w http.ResponseWriter){
		"/rest" + path: func(w http.ResponseWriter) { _, _ = w.Write([]byte(`[]`)) },
	}
	absent := map[string]func(w http.ResponseWriter){}
	both := []string{plain + " shared with " + crs, crs + " shared with " + plain}

	for _, tc := range []struct {
		name, resources string
		menus           map[string]func(w http.ResponseWriter)
		want            []string
		skipped         int
	}{
		{"resource name", crs, present, []string{crs + " shared with " + plain}, 0},
		{"menu path", path, present, both, 0},
		{"resource name, menu absent", plain, absent, []string{plain + " shared with " + crs}, 1},
		{"menu path, menu absent", path, absent, both, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, err, report := runTool(t, tc.resources, tc.menus)
			if code != 0 || err != nil {
				t.Fatalf("code = %d, err = %v; want 0 and no error", code, err)
			}
			var got []string
			for _, m := range report.Menus {
				got = append(got, strings.Join(m.Resources, ",")+" shared with "+strings.Join(m.SharedWith, ","))
			}
			if strings.Join(got, "; ") != strings.Join(tc.want, "; ") {
				t.Errorf("entries = %q, want %q", got, tc.want)
			}
			if s := report.Summary; s.Menus != 1 || s.SharedMenus != 1 || s.Skipped != tc.skipped {
				t.Errorf("summary = %+v; want 1 menu, 1 shared menu, %d skipped", s, tc.skipped)
			}
		})
	}
}
