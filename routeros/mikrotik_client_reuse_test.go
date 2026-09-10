package routeros

import (
	"crypto/tls"
	"net/http"
	"testing"
	"time"
)

// A shared connection is only correct if the key separates everything that
// makes two connections different. A collision here hands one router's
// connection to another, or one account's to another, which is worse than the
// leak the cache replaces.
func TestConnectionKeySeparatesCredentials(t *testing.T) {
	base := []string{"10.0.0.1:8728", "admin", "secret", "", "api", "0"}

	key := func(parts ...string) string { return connectionKey(parts...) }

	if key(base...) != key(base...) {
		t.Fatal("connectionKey is not deterministic")
	}

	for _, tt := range []struct {
		name  string
		parts []string
	}{
		{"different host", []string{"10.0.0.2:8728", "admin", "secret", "", "api", "0"}},
		{"different user", []string{"10.0.0.1:8728", "other", "secret", "", "api", "0"}},
		{"different password", []string{"10.0.0.1:8728", "admin", "other", "", "api", "0"}},
		{"different ca", []string{"10.0.0.1:8728", "admin", "secret", "/ca.pem", "api", "0"}},
		{"different transport", []string{"10.0.0.1:8728", "admin", "secret", "", "rest", "0"}},
		{"different tls", []string{"10.0.0.1:8728", "admin", "secret", "", "api", "1"}},
	} {
		if key(tt.parts...) == key(base...) {
			t.Errorf("%s produced the same key as the base credentials", tt.name)
		}
	}
}

// The parts are joined with a separator so that adjacent fields cannot be
// shifted between each other to produce the same key.
func TestConnectionKeyIsNotAmbiguous(t *testing.T) {
	if connectionKey("ab", "c") == connectionKey("a", "bc") {
		t.Error(`connectionKey("ab", "c") collides with connectionKey("a", "bc")`)
	}
}

func TestRestClientIsReusedPerCredentialSet(t *testing.T) {
	connectionMu.Lock()
	restConnections = map[string]*http.Client{}
	connectionMu.Unlock()

	conf := &tls.Config{InsecureSkipVerify: true}

	first := restClient("key-a", 59*time.Second, conf)
	second := restClient("key-a", 59*time.Second, conf)
	if first != second {
		t.Error("the same credentials produced two HTTP clients; every configuration would open its own connection pool")
	}

	other := restClient("key-b", 59*time.Second, conf)
	if other == first {
		t.Error("different credentials shared one HTTP client")
	}

	if first.Timeout != 59*time.Second {
		t.Errorf("timeout = %v, want 59s: an unbounded client can hang a run indefinitely", first.Timeout)
	}
}
