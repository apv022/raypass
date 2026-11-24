package raypass

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"
)

// solLikeProxy fronts h the way Sol's daemon proxies an app's UI origin (internal/apphost
// proxy): an httputil.ReverseProxy with no FlushInterval that strips the caller's Sol-*
// headers and sets Sol-Self. ReverseProxy flushes text/event-stream responses at once, which is
// what makes /lockstate work through the daemon; this test would hang if it buffered.
func solLikeProxy(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	app := httptest.NewServer(h)
	t.Cleanup(app.Close)
	target, _ := url.Parse(app.URL)
	rp := &httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) {
		pr.SetURL(target)
		for k := range pr.Out.Header {
			if strings.HasPrefix(http.CanonicalHeaderKey(k), "Sol-") {
				pr.Out.Header.Del(k)
			}
		}
		pr.Out.Header.Set("Sol-Self", "1")
	}}
	front := httptest.NewServer(rp)
	t.Cleanup(front.Close)
	return front
}

// lockStream reads /lockstate events (data lines as maps, comments as "comment").
type lockStream struct {
	lines chan string
}

func openLockStream(t *testing.T, base string) *lockStream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", base+"/lockstate", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("%d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	s := &lockStream{lines: make(chan string, 64)}
	go func() {
		defer close(s.lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if l := sc.Text(); l != "" {
				s.lines <- l
			}
		}
	}()
	t.Cleanup(func() { cancel(); resp.Body.Close() })
	return s
}

func (s *lockStream) next(t *testing.T) string {
	t.Helper()
	select {
	case l, ok := <-s.lines:
		if !ok {
			t.Fatal("stream ended")
		}
		return l
	case <-time.After(5 * time.Second):
		t.Fatal("no event within 5 s")
	}
	return ""
}

// event skips heartbeats and returns the next state.
func (s *lockStream) event(t *testing.T) map[string]any {
	t.Helper()
	for {
		l := s.next(t)
		if strings.HasPrefix(l, ":") {
			continue
		}
		data, ok := strings.CutPrefix(l, "data: ")
		if !ok {
			t.Fatalf("unexpected line %q", l)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(data), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
}

func wantState(t *testing.T, m map[string]any, locked bool, locksIn int) {
	t.Helper()
	if m["locked"] != locked || m["locks_in"] != float64(locksIn) || len(m) != 2 {
		t.Fatalf("state %v, want locked=%v locks_in=%d", m, locked, locksIn)
	}
}

func TestLockStateStream(t *testing.T) {
	e := newEnv(t)
	front := solLikeProxy(t, e.a1.Handler())
	s := openLockStream(t, front.URL)
	wantState(t, s.event(t), true, 0) // no vault yet

	call(t, e.a1, "raypass.setup", map[string]any{"password": pw})
	wantState(t, s.event(t), false, 15*60)

	// Use moves the deadline: a new event. Looking (status) does not.
	e.clk.advance(time.Minute)
	call(t, e.a1, "raypass.status", nil)
	call(t, e.a1, "raypass.list", nil)
	wantState(t, s.event(t), false, 15*60)

	// Auto-lock reaches the page at once, with no request from it.
	e.clk.advance(15 * time.Minute)
	wantState(t, s.event(t), true, 0)

	// Unlocked by another client (the extension, sol do): the page hears that too.
	call(t, e.a1, "raypass.unlock", map[string]any{"password": pw})
	wantState(t, s.event(t), false, 15*60)
	call(t, e.a1, "raypass.settings", map[string]any{"auto_lock": 0})
	wantState(t, s.event(t), false, 0)
	call(t, e.a1, "raypass.lock", nil)
	wantState(t, s.event(t), true, 0)

	// A second stream starts with the current state.
	s2 := openLockStream(t, front.URL)
	wantState(t, s2.event(t), true, 0)
}

func TestLockStateHeartbeatAndSelfOnly(t *testing.T) {
	old := lockHeartbeat
	lockHeartbeat = 20 * time.Millisecond
	t.Cleanup(func() { lockHeartbeat = old })
	e := newEnv(t)
	front := solLikeProxy(t, e.a1.Handler())
	s := openLockStream(t, front.URL)
	s.event(t)
	if l := s.next(t); l != ": keepalive" {
		t.Fatalf("heartbeat %q", l)
	}

	// Without Sol-Self (a peer), nothing streams.
	w := httptest.NewRecorder()
	e.a1.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/lockstate", nil))
	if w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), "locked") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

// Stopping the app ends open streams (so the SDK's server shutdown is not held up by them).
func TestLockStateEndsOnStop(t *testing.T) {
	e := newEnv(t)
	front := solLikeProxy(t, e.a1.Handler())
	s := openLockStream(t, front.URL)
	s.event(t)
	e.a1.Stop()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-s.lines:
			if !ok {
				return // ended (after at most the final "locked" event)
			}
		case <-deadline:
			t.Fatal("stream still open after Stop")
		}
	}
}
