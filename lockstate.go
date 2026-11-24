package raypass

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/apv022/sol/sdk/go/sol"
)

// Live lock state for the web UI, without polling (design/DESIGN.md "Lock state"): GET
// /lockstate on the app's own UI origin is a text/event-stream that sends
// {"locked","locks_in"} at once and again whenever the vault locks or unlocks or the auto-lock
// deadline moves, so the UI wipes itself the moment the app auto-locks and needs no timer of
// its own. Sol's daemon proxies the app origin with httputil.ReverseProxy, which flushes
// text/event-stream responses as they are written.
//
// The stream carries no secret and no vault content: two fields any status call returns.
// It still answers only Sol-Self callers, like every action.

// lockHeartbeat is how often an idle stream gets a comment line, so proxies and the browser
// keep the connection open. A variable so tests can shorten it.
var lockHeartbeat = 25 * time.Second

// notifyLock pokes every open /lockstate stream. Callers may hold a.mu; it never blocks.
func (a *App) notifyLock() {
	a.subMu.Lock()
	defer a.subMu.Unlock()
	for ch := range a.lockSubs {
		select {
		case ch <- struct{}{}:
		default: // already poked: the stream reads the latest state anyway
		}
	}
}

func (a *App) subscribeLock() (chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	a.subMu.Lock()
	if a.lockSubs == nil {
		a.lockSubs = map[chan struct{}]struct{}{}
	}
	a.lockSubs[ch] = struct{}{}
	a.subMu.Unlock()
	return ch, func() {
		a.subMu.Lock()
		delete(a.lockSubs, ch)
		a.subMu.Unlock()
	}
}

// lockSnapshot is what a stream compares to decide whether to send: lock state and deadline.
type lockSnapshot struct {
	locked bool
	at     time.Time
}

func (a *App) lockSnap() lockSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	return lockSnapshot{locked: a.vk == nil, at: a.lockAt}
}

// serveLockState is GET /lockstate.
func (a *App) serveLockState(w http.ResponseWriter, r *http.Request) {
	if !sol.CallerOf(r).Self {
		sol.WriteError(w, sol.Errorf("forbidden", "Raypass answers only my own devices and agents"))
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		sol.WriteError(w, sol.Errorf("unavailable", "streaming is not supported here"))
		return
	}
	ch, unsubscribe := a.subscribeLock()
	defer unsubscribe()
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	var last lockSnapshot
	send := func(first bool) bool {
		snap := a.lockSnap()
		if !first && snap == last {
			return true
		}
		last = snap
		b, _ := json.Marshal(map[string]any{"locked": snap.locked, "locks_in": a.locksIn()})
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		fl.Flush()
		return true
	}
	if !send(true) {
		return
	}
	done := a.runDone
	if done == nil {
		done = make(chan struct{}) // not started: only the request ends the stream
	}
	hb := time.NewTicker(lockHeartbeat)
	defer hb.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-done:
			return
		case <-ch:
			if !send(false) {
				return
			}
		case <-hb.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}
