package routeros

import (
	"errors"
	"testing"
	"time"

	"github.com/go-routeros/routeros/v3"
	"github.com/go-routeros/routeros/v3/proto"
)

// RouterOS answers a command it rejects with a !trap and then, like every other
// reply, a !done. A pooled session is only safe to hand to the next request if
// the synchronous client reads both. Were the !done left in the stream, the next
// request on the session would take it as its own empty success, and every later
// request would read the reply meant for the one before it.
//
// This drives two requests over a single session. The router side rejects the
// first with !trap followed by !done, then answers the second with its own !re.
// The second request has to see its own answer.
func TestAPISessionStaysInStepAfterATrap(t *testing.T) {
	f := &fakeDialer{}
	p := newAPIPool(f.dial, time.Second, 1)

	s, err := p.acquire()
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		peer := f.peer(0)
		r := proto.NewReader(peer)
		w := proto.NewWriter(peer)

		if _, err := r.ReadSentence(); err != nil {
			return
		}
		w.BeginSentence()
		w.WriteWord("!trap")
		w.WriteWord("=message=input does not match any value of interface")
		_ = w.EndSentence()
		w.BeginSentence()
		w.WriteWord("!done")
		_ = w.EndSentence()

		if _, err := r.ReadSentence(); err != nil {
			return
		}
		w.BeginSentence()
		w.WriteWord("!re")
		w.WriteWord("=name=second-request")
		_ = w.EndSentence()
		w.BeginSentence()
		w.WriteWord("!done")
		_ = w.EndSentence()
	}()

	_, err = s.run([]string{"/interface/bridge/port/add", "=interface=nonexistent"}, time.Second)

	var deviceErr *routeros.DeviceError
	if !errors.As(err, &deviceErr) || deviceErr.Sentence.Word != "!trap" {
		t.Fatalf("first request: err = %v, want the !trap as a DeviceError", err)
	}
	if !sessionUsable(err) {
		t.Fatal("the trap left the session judged unusable, so the rest of this test would not apply")
	}
	p.release(s, sessionUsable(err))

	// A pool of one hands back the same session.
	s, err = p.acquire()
	if err != nil {
		t.Fatal(err)
	}

	reply, err := s.run([]string{"/system/identity/print"}, time.Second)
	if err != nil {
		t.Fatalf("the second request on the same session failed: %v", err)
	}
	if len(reply.Re) != 1 || reply.Re[0].Map["name"] != "second-request" {
		t.Fatalf("the second request read %v instead of its own reply; the first request's !done was left in the stream", reply)
	}
}

// If a router ever sent a !trap without the !done that should follow it, the
// synchronous client would keep waiting for that !done. The deadline has to end
// the wait, and the session must not go back into the pool, since the !done
// could still arrive and be read by the next request.
func TestAPISessionTrapWithoutDoneIsNotReused(t *testing.T) {
	f := &fakeDialer{}
	p := newAPIPool(f.dial, 100*time.Millisecond, 1)

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
		w.WriteWord("!trap")
		w.WriteWord("=message=rejected")
		_ = w.EndSentence()
	}()

	start := time.Now()
	_, err = s.run([]string{"/interface/bridge/port/add"}, 100*time.Millisecond)
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Fatalf("returned after %v; the deadline did not end the wait for a !done that never came", elapsed)
	}
	if sessionUsable(err) {
		t.Errorf("a session still waiting for its !done was judged reusable (err: %v)", err)
	}
}
