package routeros

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-routeros/routeros/v3"
	"github.com/go-routeros/routeros/v3/proto"
)

// A shared session is only correct if the key separates everything that makes
// two sessions different. A collision hands one router's session to another,
// one account's to another, or a session that skipped certificate verification
// to a configuration that asked for it.
func TestConnectionKeySeparatesCredentials(t *testing.T) {
	// host, user, password, CA fingerprint, transport, TLS, insecure, timeout
	base := []string{"10.0.0.1:8729", "admin", "secret", "", "api", "1", "0", "59s"}

	with := func(i int, v string) []string {
		parts := append([]string(nil), base...)
		parts[i] = v
		return parts
	}

	if connectionKey(base...) != connectionKey(base...) {
		t.Fatal("connectionKey is not deterministic")
	}

	for _, tt := range []struct {
		name  string
		parts []string
	}{
		{"different host", with(0, "10.0.0.2:8729")},
		{"different user", with(1, "other")},
		{"different password", with(2, "other")},
		{"different CA contents", with(3, "3f2a")},
		{"different transport", with(4, "rest")},
		{"TLS off", with(5, "0")},
		{"certificate verification skipped", with(6, "1")},
		{"different timeout", with(7, "30s")},
	} {
		if connectionKey(tt.parts...) == connectionKey(base...) {
			t.Errorf("%s produced the same key as the base credentials", tt.name)
		}
	}
}

// The parts are terminated so that adjacent fields cannot be shifted between
// each other to produce the same key.
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
	if second := restClient("key-a", 59*time.Second, conf); second != first {
		t.Error("the same credentials produced two HTTP clients; every configuration would open its own connection pool")
	}
	if other := restClient("key-b", 59*time.Second, conf); other == first {
		t.Error("different credentials shared one HTTP client")
	}
	if first.Timeout != 59*time.Second {
		t.Errorf("timeout = %v, want 59s: an unbounded client can hang a run indefinitely", first.Timeout)
	}
}

// fakeDialer hands out sessions over in-memory pipes. It keeps the router's end
// of each pipe so a test can answer on it, stay silent on it, or cut it.
type fakeDialer struct {
	mu    sync.Mutex
	dials int
	fail  int
	peers []net.Conn
}

func (f *fakeDialer) dial(time.Duration) (*apiSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.dials++
	if f.fail > 0 {
		f.fail--
		return nil, errors.New("connection refused")
	}

	local, peer := net.Pipe()
	f.peers = append(f.peers, peer)

	client, err := routeros.NewClient(local)
	if err != nil {
		return nil, err
	}

	return &apiSession{conn: local, client: client, lastUsed: time.Now()}, nil
}

func (f *fakeDialer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dials
}

func (f *fakeDialer) peer(i int) net.Conn {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.peers[i]
}

func TestAPIPoolBoundsSessionsAndReusesReleasedOnes(t *testing.T) {
	f := &fakeDialer{}
	p := newAPIPool(f.dial, time.Second, 2)

	a, err := p.acquire()
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.acquire()
	if err != nil {
		t.Fatal(err)
	}

	got := make(chan *apiSession, 1)
	go func() {
		s, err := p.acquire()
		if err != nil {
			t.Error(err)
		}
		got <- s
	}()

	select {
	case <-got:
		t.Fatal("a third session was lent while two were in use in a pool of two")
	case <-time.After(50 * time.Millisecond):
	}

	p.release(a, true)

	select {
	case s := <-got:
		if s != a {
			t.Error("the waiting caller was given a new session instead of the one just released")
		}
	case <-time.After(time.Second):
		t.Fatal("the waiting caller was not woken when a session came back")
	}

	if n := f.count(); n != 2 {
		t.Errorf("dials = %d, want 2", n)
	}

	p.release(b, true)
}

func TestAPIPoolClosesSessionsARequestLeftUnusable(t *testing.T) {
	f := &fakeDialer{}
	p := newAPIPool(f.dial, time.Second, 1)

	a, err := p.acquire()
	if err != nil {
		t.Fatal(err)
	}
	p.release(a, false)

	// Closed, the router's end reads EOF. Still open, it would only time out.
	peer := f.peer(0)
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := peer.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Errorf("an unusable session was not closed (router end read %v)", err)
	}

	b, err := p.acquire()
	if err != nil {
		t.Fatal(err)
	}
	if b == a {
		t.Error("a session left unusable was lent out again")
	}
	if n := f.count(); n != 2 {
		t.Errorf("dials = %d, want 2", n)
	}
}

func TestAPIPoolFreesTheSlotWhenADialFails(t *testing.T) {
	f := &fakeDialer{fail: 1}
	p := newAPIPool(f.dial, time.Second, 1)

	if _, err := p.acquire(); err == nil {
		t.Fatal("a failed dial was reported as a session")
	}

	// In a pool of one, a slot kept by the failed dial would block this for ever.
	done := make(chan error, 1)
	go func() {
		_, err := p.acquire()
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("the failed dial kept its slot, and the next caller waited instead of dialling")
	}
}

func TestAPIPoolProbesAnIdleSessionAndReplacesADeadOne(t *testing.T) {
	f := &fakeDialer{}
	p := newAPIPool(f.dial, 200*time.Millisecond, 1)

	a, err := p.acquire()
	if err != nil {
		t.Fatal(err)
	}
	p.release(a, true)

	// The router restarted while the session sat idle.
	a.lastUsed = time.Now().Add(-2 * apiIdleProbeAfter)
	_ = f.peer(0).Close()

	b, err := p.acquire()
	if err != nil {
		t.Fatal(err)
	}
	if b == a {
		t.Error("an idle session whose router had gone was lent out without being replaced")
	}
	if n := f.count(); n != 2 {
		t.Errorf("dials = %d, want 2", n)
	}
}

func TestAPISessionRunReadsAReply(t *testing.T) {
	f := &fakeDialer{}
	p := newAPIPool(f.dial, time.Second, 1)

	s, err := p.acquire()
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		peer := f.peer(0)
		if _, err := proto.NewReader(peer).ReadSentence(); err != nil {
			return
		}
		w := proto.NewWriter(peer)
		w.BeginSentence()
		w.WriteWord("!re")
		w.WriteWord("=name=MikroTik")
		_ = w.EndSentence()
		w.BeginSentence()
		w.WriteWord("!done")
		_ = w.EndSentence()
	}()

	reply, err := s.run([]string{"/system/identity/print"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.Re) != 1 || reply.Re[0].Map["name"] != "MikroTik" {
		t.Fatalf("reply = %v, want one !re with name=MikroTik", reply)
	}
}

// The failure this pool exists for, seen from the caller: the request goes out
// and no reply ever comes back. It has to return, and the session must not be
// reused, since a late reply would be read by whichever request came next.
func TestAPISessionRunReturnsWhenTheReplyNeverComes(t *testing.T) {
	f := &fakeDialer{}
	p := newAPIPool(f.dial, 100*time.Millisecond, 1)

	s, err := p.acquire()
	if err != nil {
		t.Fatal(err)
	}

	// Take the request so the write completes, then never answer.
	go func() { _, _ = proto.NewReader(f.peer(0)).ReadSentence() }()

	start := time.Now()
	_, err = s.run([]string{"/system/identity/print"}, 100*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("a request that never got a reply returned success")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("returned after %v; the deadline did not bound the read", elapsed)
	}
	if sessionUsable(err) {
		t.Errorf("a session whose reply never came was judged reusable (err: %v)", err)
	}
}

func TestSessionUsable(t *testing.T) {
	trap := &routeros.DeviceError{Sentence: &proto.Sentence{Word: "!trap"}}
	fatal := &routeros.DeviceError{Sentence: &proto.Sentence{Word: "!fatal"}}

	for _, tt := range []struct {
		name string
		err  error
		want bool
	}{
		{"success", nil, true},
		{"RouterOS declined the command", trap, true},
		{"declined, wrapped by a caller", fmt.Errorf("create: %w", trap), true},
		{"RouterOS closed the session", fatal, false},
		{"deadline passed", os.ErrDeadlineExceeded, false},
		{"socket gone", io.ErrClosedPipe, false},
		{"reply word the client does not know", &routeros.UnknownReplyError{Sentence: &proto.Sentence{Word: "!weird"}}, false},
	} {
		if got := sessionUsable(tt.err); got != tt.want {
			t.Errorf("%s: sessionUsable = %v, want %v", tt.name, got, tt.want)
		}
	}
}
