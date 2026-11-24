// Package raypass is the Raypass app: a password manager whose vault lives, encrypted, in two
// Sol feeds of the person (design/DESIGN.md).
//
// Feed "keys" holds keyrings (the vault key wrapped under the master password), feed "vault"
// holds AES-GCM encrypted batches of CRDT map ops. Every device of the person writes both; Sol
// only ever carries ciphertext. This package follows both feeds from the events stream (no
// polling; every action drains what the stream has not delivered yet), keeps the ciphertext in
// memory, and while unlocked holds the vault key and the folded, decrypted state.
//
// Invariants:
//   - every action requires a Sol-Self caller (the person's own devices and agents);
//   - while locked no plaintext and no key is held; lock (explicit, auto-lock, Stop) wipes the
//     vault key and drops the decrypted state; actions that need the vault answer
//     forbidden/"locked";
//   - nothing is logged that came from the vault or from a request body;
//   - state is rebuilt from the feeds alone; folding is idempotent and order-independent.
package raypass

import (
	"context"
	_ "embed"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/apv022/raypass/vault"
	"github.com/apv022/sol/sdk/go/crdt"
	"github.com/apv022/sol/sdk/go/sol"
)

// ManifestJSON is the app's app.json.
//
//go:embed app.json
var ManifestJSON []byte

// AppID is the Sol app id.
const AppID = "raypass"

// Feed names.
const (
	FeedKeys  = "keys"
	FeedVault = "vault"
)

// Clock is the app's time source, injectable so tests can drive auto-lock.
type Clock interface {
	Now() time.Time
	// AfterFunc calls f after d and returns a function that stops it.
	AfterFunc(d time.Duration, f func()) (stop func() bool)
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) AfterFunc(d time.Duration, f func()) func() bool {
	return time.AfterFunc(d, f).Stop
}

// Config is how a Raypass instance is set up.
type Config struct {
	// Listener is where the UI and actions are served; nil means sol.Serve (SOL_APP_LISTEN).
	Listener net.Listener
	// Clock overrides the time source (tests).
	Clock Clock
	// Logf receives diagnostics; nil discards them. Never given secrets or vault content.
	Logf func(format string, args ...any)
	// KDFIter is the PBKDF2 round count for new keyrings and encrypted exports (default
	// vault.DefaultIter).
	KDFIter int
	// MinIter is the smallest round count accepted from a keyring, an export or an input
	// (default vault.MinIter). Only tests lower it, to keep them fast.
	MinIter int
}

// App is one running Raypass instance.
type App struct {
	api      sol.API
	cfg      Config
	manifest *sol.Manifest
	clock    Clock

	me, dev string
	clk     *crdt.Clock

	pullMu sync.Mutex // serialises feed reads so each entry is ingested once
	mu     sync.Mutex // guards everything below
	keyPos uint64
	valPos uint64
	rings  []vault.Keyring
	ents   []vault.Entry // every vault entry, ciphertext, in pos order

	// unlocked state; vk == nil means locked
	vk       []byte
	kid      string
	st       *vault.State
	applied  int // ents[:applied] are folded into st
	bad      int // entries of our vault that did not decrypt
	settings vault.Settings
	lockGen  uint64
	stopLock func() bool
	lockAt   time.Time // when auto-lock fires; zero when locked or auto_lock is 0

	unlockMu sync.Mutex // one password check at a time, so the backoff cannot be raced
	failures int
	sleep    func(ctx context.Context, d time.Duration) error

	setupMu sync.Mutex

	// Live lock state for the web UI (lockstate.go): each open /lockstate stream has a channel
	// here that is poked whenever the lock state or the auto-lock deadline changes. subMu is
	// taken after a.mu, never before it.
	subMu    sync.Mutex
	lockSubs map[chan struct{}]struct{}
	runDone  <-chan struct{} // closed when Start's context ends; ends open streams

	noFollow bool
	started  bool
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	handler  http.Handler
}

// New creates a Raypass instance. Call Start (or Run) to begin.
func New(api sol.API, cfg Config) *App {
	m, err := sol.ParseManifest(ManifestJSON)
	if err != nil {
		panic("raypass: bad embedded app.json: " + err.Error())
	}
	if cfg.Clock == nil {
		cfg.Clock = realClock{}
	}
	if cfg.KDFIter == 0 {
		cfg.KDFIter = vault.DefaultIter
	}
	if cfg.MinIter == 0 {
		cfg.MinIter = vault.MinIter
	}
	a := &App{api: api, cfg: cfg, manifest: m, clock: cfg.Clock,
		settings: vault.Settings{AutoLock: vault.DefaultAutoLock, ClipClear: vault.DefaultClipClear}}
	a.sleep = func(ctx context.Context, d time.Duration) error {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-t.C:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	a.handler = a.buildHandler()
	return a
}

// Handler serves the UI and the action endpoints.
func (a *App) Handler() http.Handler { return a.handler }

func (a *App) logf(format string, args ...any) {
	if a.cfg.Logf != nil {
		a.cfg.Logf(format, args...)
	}
}

func (a *App) now() time.Time { return a.clock.Now() }

// Run starts the app and serves until ctx ends.
func (a *App) Run(ctx context.Context) error {
	if err := a.Start(ctx); err != nil {
		return err
	}
	defer a.Stop()
	var err error
	if a.cfg.Listener != nil {
		err = sol.ServeListener(ctx, a.cfg.Listener, a.api, a.handler)
	} else {
		err = sol.Serve(ctx, a.api, a.handler)
	}
	if ctx.Err() != nil {
		return nil
	}
	return err
}

func (a *App) addr(feed string) string { return sol.Address(a.me, AppID, feed) }

// Start reads both feeds and follows the events stream.
func (a *App) Start(ctx context.Context) error {
	if a.started {
		return errors.New("raypass: already started")
	}
	who, err := a.api.WhoAmI(ctx)
	if err != nil {
		return err
	}
	a.me, a.dev = who.Person.ID, who.Device.ID
	a.clk = crdt.NewClock(a.dev)
	a.clk.Wall = func() int64 { return a.clock.Now().UnixMilli() }
	ctx, a.cancel = context.WithCancel(ctx)
	a.runDone = ctx.Done()
	// Subscribe before the first read so nothing slips between them.
	var ev <-chan sol.Event
	if !a.noFollow {
		ev, err = a.api.Events(ctx, sol.EventOptions{Feeds: []string{a.addr(FeedKeys), a.addr(FeedVault)}})
		if err != nil {
			a.cancel()
			return err
		}
	}
	if err := a.sync(ctx); err != nil {
		a.cancel()
		return err
	}
	a.started = true
	if ev != nil {
		a.wg.Add(1)
		go a.follow(ctx, ev)
	}
	return nil
}

// Stop ends the event loop and locks the vault.
func (a *App) Stop() {
	if !a.started {
		return
	}
	a.cancel()
	a.wg.Wait()
	a.lock()
	a.started = false
}

func (a *App) follow(ctx context.Context, ev <-chan sol.Event) {
	defer a.wg.Done()
	for e := range ev {
		switch {
		case e.Type == sol.EventEntry && e.Entry != nil:
			if err := a.pull(ctx, e.Entry.Feed); err != nil && ctx.Err() == nil {
				a.logf("raypass: reading %s: %v", e.Entry.Feed, err)
			}
		case e.Type == sol.EventError:
			a.logf("raypass: events stream ended: %s", e.Err)
		}
	}
}

// sync reads whatever the two feeds hold beyond our cursors.
func (a *App) sync(ctx context.Context) error {
	infos, err := a.api.Feeds(ctx, a.me)
	if err != nil {
		return err
	}
	for _, fi := range infos {
		if fi.Feed != a.addr(FeedKeys) && fi.Feed != a.addr(FeedVault) {
			continue
		}
		a.mu.Lock()
		pos := a.keyPos
		if fi.Feed == a.addr(FeedVault) {
			pos = a.valPos
		}
		a.mu.Unlock()
		if pos >= fi.Pos {
			continue
		}
		if err := a.pull(ctx, fi.Feed); err != nil {
			return err
		}
	}
	return nil
}

// pull reads and ingests the new entries of one of our feeds.
func (a *App) pull(ctx context.Context, address string) error {
	isKeys := address == a.addr(FeedKeys)
	if !isKeys && address != a.addr(FeedVault) {
		return nil
	}
	a.pullMu.Lock()
	defer a.pullMu.Unlock()
	for {
		a.mu.Lock()
		from := a.valPos + 1
		if isKeys {
			from = a.keyPos + 1
		}
		a.mu.Unlock()
		es, err := a.api.Read(ctx, address, from, 500)
		if err != nil {
			return err
		}
		if len(es) == 0 {
			return nil
		}
		a.mu.Lock()
		for _, e := range es {
			if isKeys {
				if kr, ok := vault.ParseKeyring(e.Body); ok {
					a.clk.Observe(kr.Stamp)
					a.rings = append(a.rings, kr)
				}
				a.keyPos = max(a.keyPos, e.Pos)
			} else {
				if ve, ok := vault.ParseEntry(e.Body); ok {
					a.ents = append(a.ents, ve)
				}
				a.valPos = max(a.valPos, e.Pos)
			}
		}
		a.foldLocked()
		a.mu.Unlock()
		if len(es) < 500 {
			return nil
		}
	}
}

// foldLocked decrypts and applies the vault entries not folded yet, when unlocked. a.mu held.
func (a *App) foldLocked() {
	if a.vk == nil {
		return
	}
	for ; a.applied < len(a.ents); a.applied++ {
		e := a.ents[a.applied]
		if e.K != a.kid {
			continue // another vault's entry: counted as orphaned
		}
		ops, err := e.Decrypt(a.vk)
		if err != nil {
			a.bad++
			a.logf("raypass: a vault entry did not decrypt; it is kept and counted as orphaned")
			continue
		}
		a.st.Apply(ops)
	}
	a.settings = a.st.Settings()
}

// fresh folds whatever the events stream has not delivered yet.
func (a *App) fresh(ctx context.Context) error {
	if a.clk == nil {
		return sol.Errorf("unavailable", "Raypass has not started")
	}
	return a.sync(ctx)
}

// keyring returns the current keyring choice.
func (a *App) keyring() vault.Keyrings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return vault.ChooseKeyring(a.rings, a.cfg.MinIter)
}

// unlocked reports whether the vault key is held.
func (a *App) unlocked() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.vk != nil
}

// errLocked is the exact error of design/DESIGN.md: clients key the unlock screen on it.
func errLocked() error { return sol.Errorf("forbidden", "locked") }

// state returns the decrypted state, or the locked error.
func (a *App) state() (*vault.State, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.vk == nil {
		return nil, errLocked()
	}
	return a.st, nil
}

// open installs vk as the unlocked key of vault kid and folds everything. Takes ownership of vk.
func (a *App) open(vk []byte, kid string) {
	a.mu.Lock()
	if a.vk != nil {
		a.mu.Unlock()
		vault.Wipe(vk) // already unlocked: keep the key we hold
		a.touch()
		return
	}
	a.vk, a.kid = vk, kid
	a.st = vault.NewState(a.clk)
	a.applied, a.bad = 0, 0
	a.foldLocked()
	a.mu.Unlock()
	a.touch()
}

// lock wipes the vault key and drops the decrypted state.
func (a *App) lock() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lockLocked()
}

// lockLocked is lock with a.mu held.
func (a *App) lockLocked() {
	if a.stopLock != nil {
		a.stopLock()
		a.stopLock = nil
	}
	a.lockGen++
	a.lockAt = time.Time{}
	a.notifyLock()
	if a.vk == nil {
		return
	}
	vault.Wipe(a.vk)
	a.vk, a.kid = nil, ""
	if a.st != nil {
		a.st.Wipe()
	}
	a.st = nil
	a.applied, a.bad = 0, 0
}

// touch records activity: it re-arms the auto-lock timer for s.auto_lock minutes. Only calls
// that use the vault (or unlock it, or change its settings) count as activity: status,
// generate and lock do not, so a client that merely looks never keeps the vault open.
func (a *App) touch() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopLock != nil {
		a.stopLock()
		a.stopLock = nil
	}
	a.lockGen++
	a.lockAt = time.Time{}
	defer a.notifyLock() // the deadline moved (or went away)
	if a.vk == nil || a.settings.AutoLock <= 0 {
		return
	}
	gen, minutes := a.lockGen, a.settings.AutoLock
	d := time.Duration(minutes) * time.Minute
	a.lockAt = a.clock.Now().Add(d)
	a.stopLock = a.clock.AfterFunc(d, func() {
		// Check and lock in one critical section, so an action that re-arms the timer at the
		// same moment either wins (stale) or finds the vault locked: never locked after it.
		a.mu.Lock()
		fired := a.lockGen == gen
		if fired {
			a.lockLocked()
		}
		a.mu.Unlock()
		if fired {
			a.logf("raypass: auto-locked after %d minutes without use", minutes)
		}
	})
}

// locksIn is how many seconds remain before auto-lock (rounded up), 0 when locked or never.
func (a *App) locksIn() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.vk == nil || a.lockAt.IsZero() {
		return 0
	}
	d := a.lockAt.Sub(a.clock.Now())
	if d <= 0 {
		return 0
	}
	return int((d + time.Second - 1) / time.Second)
}

// orphaned counts vault entries that cannot be read with this vault's key: other vaults'
// entries, and (when unlocked) entries of ours that failed to decrypt.
func (a *App) orphaned(kid string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := a.bad
	for _, e := range a.ents {
		if e.K != kid {
			n++
		}
	}
	return n
}

// appendOps encrypts ops under the vault key, appends them to the vault feed and folds them.
func (a *App) appendOps(ctx context.Context, ops []crdt.MapOp) error {
	if len(ops) == 0 {
		return nil
	}
	a.mu.Lock()
	if a.vk == nil {
		a.mu.Unlock()
		return errLocked()
	}
	ents, err := vault.EncryptOps(a.vk, a.kid, ops)
	a.mu.Unlock()
	if err != nil {
		var bi vault.BadInput
		if errors.As(err, &bi) || err.Error() == "a value is too large to store" {
			return sol.Errorf("bad_request", err.Error())
		}
		return err
	}
	for _, e := range ents {
		if _, err := a.api.Append(ctx, FeedVault, e); err != nil {
			return err
		}
	}
	return a.pull(ctx, a.addr(FeedVault))
}
