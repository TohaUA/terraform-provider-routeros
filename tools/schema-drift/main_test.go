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

// runAgainst starts a fake device that answers the version probe and /ip/settings normally and
// lets the caller decide what /ip/service does, then runs the tool against those two menus.
func runAgainst(t *testing.T, service func(w http.ResponseWriter)) (int, error, *Report) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/system/resource":
			_, _ = w.Write([]byte(`{"version":"7.24 (stable)"}`))
		case "/rest/ip/settings":
			_, _ = w.Write([]byte(`[]`))
		case "/rest/ip/service":
			service(w)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	t.Setenv("ROS_HOSTURL", srv.URL)
	t.Setenv("ROS_USERNAME", "reader")
	t.Setenv("ROS_PASSWORD", "secret")

	jsonOut := filepath.Join(t.TempDir(), "report.json")
	code, err := run("", "", "/ip/settings,/ip/service", "", "", jsonOut, false, true, 5*time.Second, 2)

	var report *Report
	if raw, rErr := os.ReadFile(jsonOut); rErr == nil {
		report = &Report{}
		if jErr := json.Unmarshal(raw, report); jErr != nil {
			t.Fatalf("report json: %v", jErr)
		}
	}
	return code, err, report
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
