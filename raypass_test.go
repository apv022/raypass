package raypass

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apv022/raypass/vault"
	"github.com/apv022/sol/sdk/go/sol"
	"github.com/apv022/sol/sdk/go/sol/memsol"
)

const testIter = 1000 // tests lower the KDF cost; production refuses fewer than 100 000 rounds

// fakeClock is a manual clock: time moves only when a test says so, and timers fire then.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	at      time.Time
	f       func()
	stopped bool
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }

func (c *fakeClock) AfterFunc(d time.Duration, f func()) func() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{at: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	return func() bool { c.mu.Lock(); defer c.mu.Unlock(); was := !t.stopped; t.stopped = true; return was }
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []*fakeTimer
	for _, x := range c.timers {
		if !x.stopped && !x.at.After(c.now) {
			x.stopped = true
			due = append(due, x)
		}
	}
	c.mu.Unlock()
	sort.Slice(due, func(i, j int) bool { return due[i].at.Before(due[j].at) })
	for _, x := range due {
		x.f()
	}
}

type env struct {
	net   *memsol.Network
	clk   *fakeClock
	alice *memsol.Person
	bob   *memsol.Person
	a1    *App // Alice's first device
	a2    *App // Alice's second device
}

func newEnv(t *testing.T) *env {
	t.Helper()
	n := memsol.New()
	e := &env{net: n, clk: &fakeClock{now: time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)}}
	n.Now = e.clk.Now
	e.alice, e.bob = n.AddPerson("Alice"), n.AddPerson("Bob")
	n.Pair(e.alice, e.bob)
	laptop := e.alice.AddDevice("laptop", false)
	m, err := sol.ParseManifest(ManifestJSON)
	if err != nil {
		t.Fatal(err)
	}
	e.a1 = start(t, e, e.alice.Devices[0])
	e.a2 = start(t, e, laptop)
	e.alice.Install(m, e.a1.Handler())
	return e
}

func start(t *testing.T, e *env, d *memsol.Device) *App {
	t.Helper()
	a := New(d.Client(AppID), Config{Clock: e.clk, KDFIter: testIter, MinIter: testIter, Logf: t.Logf})
	ctx, cancel := context.WithCancel(context.Background())
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Stop(); cancel() })
	return a
}

type result struct {
	code int
	body []byte
}

func (r result) obj(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.body, &m); err != nil {
		t.Fatalf("not an object: %s", r.body)
	}
	return m
}

func (r result) arr(t *testing.T) []map[string]any {
	t.Helper()
	var m []map[string]any
	if err := json.Unmarshal(r.body, &m); err != nil {
		t.Fatalf("not an array: %s", r.body)
	}
	return m
}

func (r result) errCode() (string, string) {
	var e struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	_ = json.Unmarshal(r.body, &e)
	return e.Error.Code, e.Error.Message
}

type opts struct {
	dry  bool
	peer bool
}

func raw(a *App, name string, args any, o opts) result {
	if args == nil {
		args = map[string]any{}
	}
	b, _ := json.Marshal(args)
	r := httptest.NewRequest("POST", "/_sol/actions/"+name, bytes.NewReader(b))
	r.Header.Set("Sol-Person", a.me)
	r.Header.Set("Sol-Device", a.dev)
	if !o.peer {
		r.Header.Set("Sol-Self", "1")
	}
	if o.dry {
		r.Header.Set("Sol-Dry-Run", "1")
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return result{w.Code, w.Body.Bytes()}
}

// call runs an action and fails the test unless it answers 200.
func call(t *testing.T, a *App, name string, args any) result {
	t.Helper()
	r := raw(a, name, args, opts{})
	if r.code != 200 {
		// The action name only: arguments may hold secrets, and test output is a log.
		t.Fatalf("%s: %d %s", name, r.code, r.body)
	}
	return r
}

func expectErr(t *testing.T, r result, status int, code string) string {
	t.Helper()
	c, msg := r.errCode()
	if r.code != status || c != code {
		t.Fatalf("want %d %s, got %d %s", status, code, r.code, r.body)
	}
	return msg
}

const pw = "correct horse battery staple"

func (e *env) feedBodies(t *testing.T, feed string) []string {
	t.Helper()
	c := e.alice.Devices[0].Client(AppID)
	es, err := c.Read(context.Background(), sol.Address(e.alice.ID, AppID, feed), 1, 5000)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, x := range es {
		out = append(out, string(x.Body))
	}
	return out
}

func TestSetupUnlockOnTwoDevices(t *testing.T) {
	e := newEnv(t)
	st := call(t, e.a2, "raypass.status", nil).obj(t)
	if st["initialized"] != false || st["locked"] != true {
		t.Fatalf("%v", st)
	}
	expectErr(t, raw(e.a1, "raypass.setup", map[string]any{"password": "short"}, opts{}), 400, "bad_request")
	call(t, e.a1, "raypass.setup", map[string]any{"password": pw})
	expectErr(t, raw(e.a2, "raypass.setup", map[string]any{"password": pw + "!"}, opts{}), 409, "conflict")

	st = call(t, e.a1, "raypass.status", nil).obj(t)
	if st["initialized"] != true || st["locked"] != false || st["kdf_iter"] != float64(testIter) || st["auto_lock"] != float64(15) {
		t.Fatalf("%v", st)
	}
	st = call(t, e.a2, "raypass.status", nil).obj(t)
	if st["initialized"] != true || st["locked"] != true {
		t.Fatalf("second device: %v", st)
	}
	expectErr(t, raw(e.a2, "raypass.unlock", map[string]any{"password": "wrong password"}, opts{}), 401, "unauthorized")
	call(t, e.a2, "raypass.unlock", map[string]any{"password": pw})

	// Edits on both devices merge.
	id := call(t, e.a1, "raypass.add", map[string]any{"type": "login", "name": "GitHub", "user": "octo", "pass": "gh-1", "urls": []string{"https://github.com"}}).obj(t)["id"].(string)
	got := call(t, e.a2, "raypass.get", map[string]any{"id": id}).obj(t)
	if got["pass"] != "gh-1" || got["user"] != "octo" {
		t.Fatalf("second device sees %v", got["name"])
	}
	call(t, e.a1, "raypass.update", map[string]any{"id": id, "notes": "from one"})
	call(t, e.a2, "raypass.update", map[string]any{"id": id, "pass": "gh-2"})
	for _, a := range []*App{e.a1, e.a2} {
		got := call(t, a, "raypass.get", map[string]any{"id": id}).obj(t)
		h := got["history"].([]any)
		if got["notes"] != "from one" || got["pass"] != "gh-2" || len(h) != 1 || h[0].(map[string]any)["pass"] != "gh-1" {
			t.Fatalf("not merged: notes=%v history=%d", got["notes"], len(h))
		}
	}
	// The events stream alone (no action) brings new entries to the other device.
	call(t, e.a1, "raypass.add", map[string]any{"type": "note", "name": "Pushed", "notes": "x"})
	waitFor(t, func() bool {
		e.a2.mu.Lock()
		defer e.a2.mu.Unlock()
		return e.a2.valPos == uint64(len(e.feedBodies(t, FeedVault)))
	})
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestChangePassword(t *testing.T) {
	e := newEnv(t)
	call(t, e.a1, "raypass.setup", map[string]any{"password": pw})
	id := call(t, e.a1, "raypass.add", map[string]any{"type": "note", "name": "Secret note", "notes": "n"}).obj(t)["id"].(string)
	expectErr(t, raw(e.a1, "raypass.change_password", map[string]any{"old": "nope nope", "new": "new password 1"}, opts{}), 401, "unauthorized")
	expectErr(t, raw(e.a1, "raypass.change_password", map[string]any{"old": pw, "new": "x"}, opts{}), 400, "bad_request")
	r := raw(e.a1, "raypass.change_password", map[string]any{"old": pw, "new": "new password 1"}, opts{dry: true})
	if r.code != 200 || len(e.feedBodies(t, FeedKeys)) != 1 {
		t.Fatal("dry-run change_password wrote")
	}
	call(t, e.a1, "raypass.change_password", map[string]any{"old": pw, "new": "new password 1"})
	if n := len(e.feedBodies(t, FeedKeys)); n != 2 {
		t.Fatalf("%d keyrings", n)
	}
	// The other device unlocks with the new password only.
	expectErr(t, raw(e.a2, "raypass.unlock", map[string]any{"password": pw}, opts{}), 401, "unauthorized")
	call(t, e.a2, "raypass.unlock", map[string]any{"password": "new password 1"})
	if call(t, e.a2, "raypass.get", map[string]any{"id": id}).obj(t)["notes"] != "n" {
		t.Fatal("old entries unreadable after a password change")
	}
	// The first device stays unlocked, and after a lock needs the new password too.
	call(t, e.a1, "raypass.list", nil)
	call(t, e.a1, "raypass.lock", nil)
	expectErr(t, raw(e.a1, "raypass.unlock", map[string]any{"password": pw}, opts{}), 401, "unauthorized")
	call(t, e.a1, "raypass.unlock", map[string]any{"password": "new password 1"})
}

func TestPeerForbidden(t *testing.T) {
	e := newEnv(t)
	call(t, e.a1, "raypass.setup", map[string]any{"password": pw})
	for _, s := range e.a1.manifest.Actions {
		args := map[string]any{"id": "0000000000000000", "ids": []string{"x"}, "password": pw, "url": "https://x.com",
			"data": "", "format": "json", "old": pw, "new": pw, "user": "u", "pass": "p", "type": "note", "name": "n"}
		if s.Name == "raypass.import" {
			args["format"] = "auto"
		}
		r := raw(e.a1, s.Name, args, opts{peer: true})
		expectErr(t, r, 403, "forbidden")
	}
	// Through memsol's cross-person call path too: Bob calling Alice's app.
	_, err := e.bob.Client(AppID).CallAction(context.Background(), sol.ActionCall{Person: e.alice.ID, Action: "raypass.status", Args: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("a peer called raypass.status")
	}
}

func TestLocked(t *testing.T) {
	e := newEnv(t)
	// Before setup: vault actions are locked, unlock says there is no vault.
	if msg := expectErr(t, raw(e.a1, "raypass.list", nil, opts{}), 403, "forbidden"); msg != "locked" {
		t.Fatal(msg)
	}
	expectErr(t, raw(e.a1, "raypass.unlock", map[string]any{"password": pw}, opts{}), 404, "not_found")
	call(t, e.a1, "raypass.setup", map[string]any{"password": pw})
	call(t, e.a1, "raypass.lock", nil)
	vaultActions := map[string]any{
		"raypass.list": nil, "raypass.get": map[string]any{"id": "x"}, "raypass.add": map[string]any{"type": "note", "name": "n"},
		"raypass.update": map[string]any{"id": "x"}, "raypass.trash": map[string]any{"ids": []string{"x"}},
		"raypass.restore": map[string]any{"ids": []string{"x"}}, "raypass.purge": map[string]any{"all_trash": true},
		"raypass.totp": map[string]any{"id": "x"}, "raypass.match": map[string]any{"url": "https://x.com"},
		"raypass.fill": map[string]any{"id": "x", "url": "https://x.com"}, "raypass.save_login": map[string]any{"url": "https://x.com", "user": "u", "pass": "p"},
		"raypass.folders": nil, "raypass.health": nil, "raypass.export": map[string]any{"format": "json"},
		"raypass.import": map[string]any{"data": "x"}, "raypass.settings": map[string]any{"auto_lock": 5},
	}
	for name, args := range vaultActions {
		r := raw(e.a1, name, args, opts{})
		if r.code != 403 || string(bytes.TrimSpace(r.body)) != `{"error":{"code":"forbidden","message":"locked"}}` {
			t.Fatalf("%s while locked: %d %s", name, r.code, r.body)
		}
	}
	// Not vault-gated: status, generate, lock.
	call(t, e.a1, "raypass.status", nil)
	call(t, e.a1, "raypass.generate", nil)
	call(t, e.a1, "raypass.lock", nil)
	if e.a1.vk != nil || e.a1.st != nil {
		t.Fatal("key held while locked")
	}
}

func TestAutoLock(t *testing.T) {
	e := newEnv(t)
	call(t, e.a1, "raypass.setup", map[string]any{"password": pw})
	e.clk.advance(14 * time.Minute)
	call(t, e.a1, "raypass.list", nil) // activity re-arms the timer
	e.clk.advance(14 * time.Minute)
	if !e.a1.unlocked() {
		t.Fatal("locked before 15 idle minutes")
	}
	e.clk.advance(2 * time.Minute)
	if e.a1.unlocked() {
		t.Fatal("not auto-locked")
	}
	call(t, e.a1, "raypass.unlock", map[string]any{"password": pw})
	call(t, e.a1, "raypass.settings", map[string]any{"auto_lock": 0})
	e.clk.advance(24 * time.Hour)
	if !e.a1.unlocked() {
		t.Fatal("auto_lock 0 must never lock")
	}
	got := call(t, e.a1, "raypass.settings", map[string]any{"auto_lock": 1, "clip_clear": 10}).obj(t)
	if got["auto_lock"] != float64(1) || got["clip_clear"] != float64(10) {
		t.Fatal(got)
	}
	// Settings sync: the other device sees them after unlocking.
	call(t, e.a2, "raypass.unlock", map[string]any{"password": pw})
	if st := call(t, e.a2, "raypass.status", nil).obj(t); st["clip_clear"] != float64(10) {
		t.Fatal(st)
	}
	e.clk.advance(61 * time.Second)
	if e.a1.unlocked() || e.a2.unlocked() {
		t.Fatal("1-minute auto-lock did not lock")
	}
}

func TestUnlockBackoff(t *testing.T) {
	e := newEnv(t)
	var waits []time.Duration
	e.a2.sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	call(t, e.a1, "raypass.setup", map[string]any{"password": pw})
	for i := 0; i < 13; i++ {
		raw(e.a2, "raypass.unlock", map[string]any{"password": "wrong password"}, opts{})
	}
	want := []time.Duration{1, 2, 4, 8, 16, 32, 60, 60}
	if len(waits) != len(want) {
		t.Fatalf("waits %v", waits)
	}
	for i, w := range want {
		if waits[i] != w*time.Second {
			t.Fatalf("waits %v", waits)
		}
	}
	call(t, e.a2, "raypass.unlock", map[string]any{"password": pw}) // waits too, then resets
	waits = nil
	raw(e.a2, "raypass.unlock", map[string]any{"password": "wrong password"}, opts{})
	if len(waits) != 0 {
		t.Fatal("backoff not reset by a success")
	}
}

func TestDryRunChangesNothing(t *testing.T) {
	e := newEnv(t)
	r := raw(e.a1, "raypass.setup", map[string]any{"password": pw}, opts{dry: true})
	if r.code != 200 || r.obj(t)["dry_run"] != true || len(e.feedBodies(t, FeedKeys)) != 0 || e.a1.unlocked() {
		t.Fatalf("dry-run setup: %s", r.body)
	}
	call(t, e.a1, "raypass.setup", map[string]any{"password": pw})
	id := call(t, e.a1, "raypass.add", map[string]any{"type": "login", "name": "A", "user": "u", "pass": "p", "urls": []string{"https://a.com"}}).obj(t)["id"].(string)
	call(t, e.a1, "raypass.add", map[string]any{"type": "note", "name": "T", "notes": "x"})
	tid := call(t, e.a1, "raypass.list", map[string]any{"type": "note"}).arr(t)[0]["id"].(string)
	call(t, e.a1, "raypass.trash", map[string]any{"ids": []string{tid}})
	before := len(e.feedBodies(t, FeedVault))
	csv := "name,url,username,password,note\nx,https://x.com,u,secret-x,\n"
	for name, args := range map[string]any{
		"raypass.add":        map[string]any{"type": "note", "name": "n", "notes": "dry secret"},
		"raypass.update":     map[string]any{"id": id, "pass": "dry secret"},
		"raypass.trash":      map[string]any{"ids": []string{id}},
		"raypass.restore":    map[string]any{"ids": []string{tid}},
		"raypass.purge":      map[string]any{"all_trash": true},
		"raypass.save_login": map[string]any{"url": "https://new.com", "user": "u", "pass": "dry secret"},
		"raypass.import":     map[string]any{"data": csv},
		"raypass.settings":   map[string]any{"auto_lock": 3},
		"raypass.lock":       nil,
	} {
		r := raw(e.a1, name, args, opts{dry: true})
		if r.code != 200 || r.obj(t)["dry_run"] != true {
			t.Fatalf("%s: %d %s", name, r.code, r.body)
		}
		if strings.Contains(string(r.body), "dry secret") || strings.Contains(string(r.body), "secret-x") {
			t.Fatalf("%s: dry-run output echoes a secret", name)
		}
	}
	if after := len(e.feedBodies(t, FeedVault)); after != before || !e.a1.unlocked() {
		t.Fatalf("dry runs wrote %d entries", after-before)
	}
	if n := call(t, e.a1, "raypass.import", map[string]any{"data": csv}).obj(t)["imported"]; n != float64(1) {
		t.Fatal(n)
	}
}

func TestFeedsHoldNoPlaintext(t *testing.T) {
	e := newEnv(t)
	call(t, e.a1, "raypass.setup", map[string]any{"password": pw})
	secrets := []string{pw, "Bank of Examples", "alice-user", "hunter2-very-secret", "bank.example", "a private note", "JBSWY3DPEHPK3PXP",
		"Wallet name", "abandon ability able", "Card holder", "4111111111111111", "WorkFolder", "tag-one"}
	call(t, e.a1, "raypass.add", map[string]any{"type": "login", "name": "Bank of Examples", "user": "alice-user", "pass": "hunter2-very-secret",
		"urls": []string{"https://bank.example"}, "notes": "a private note", "totp": "JBSWY3DPEHPK3PXP", "folder": "WorkFolder", "tags": []string{"tag-one"}})
	call(t, e.a1, "raypass.add", map[string]any{"type": "wallet", "name": "Wallet name", "seed": "abandon ability able"})
	call(t, e.a1, "raypass.add", map[string]any{"type": "card", "name": "Visa", "holder": "Card holder", "number": "4111111111111111"})
	call(t, e.a1, "raypass.change_password", map[string]any{"old": pw, "new": "new password 1"})
	secrets = append(secrets, "new password 1")
	all := append(e.feedBodies(t, FeedVault), e.feedBodies(t, FeedKeys)...)
	for _, body := range all {
		for _, s := range secrets {
			if strings.Contains(strings.ToLower(body), strings.ToLower(s)) {
				t.Fatalf("a feed body contains secret #%d", sortIndex(secrets, s))
			}
		}
		// Only the documented fields.
		var m map[string]any
		_ = json.Unmarshal([]byte(body), &m)
		for k := range m {
			switch k {
			case "v", "k", "n", "c", "type", "id", "kdf", "nonce", "wrapped", "check", "stamp":
			default:
				t.Fatalf("undocumented field %q in a feed body", k)
			}
		}
	}
}

func sortIndex(ss []string, s string) int {
	for i, x := range ss {
		if x == s {
			return i
		}
	}
	return -1
}

func TestTrashRestorePurge(t *testing.T) {
	e := newEnv(t)
	call(t, e.a1, "raypass.setup", map[string]any{"password": pw})
	call(t, e.a2, "raypass.unlock", map[string]any{"password": pw})
	id := call(t, e.a1, "raypass.add", map[string]any{"type": "note", "name": "Gone", "notes": "bye"}).obj(t)["id"].(string)
	id2 := call(t, e.a1, "raypass.add", map[string]any{"type": "note", "name": "Kept"}).obj(t)["id"].(string)
	expectErr(t, raw(e.a1, "raypass.purge", map[string]any{"ids": []string{id}}, opts{}), 400, "bad_request")
	expectErr(t, raw(e.a1, "raypass.trash", map[string]any{"ids": []string{"ffffffffffffffff"}}, opts{}), 404, "not_found")
	if n := call(t, e.a1, "raypass.trash", map[string]any{"ids": []string{id, id2}}).obj(t)["trashed"]; n != float64(2) {
		t.Fatal(n)
	}
	if rows := call(t, e.a2, "raypass.list", nil).arr(t); len(rows) != 0 {
		t.Fatal("trashed items listed")
	}
	if rows := call(t, e.a2, "raypass.list", map[string]any{"trash": true}).arr(t); len(rows) != 2 {
		t.Fatal("trash not listed")
	}
	if n := call(t, e.a2, "raypass.restore", map[string]any{"ids": []string{id2}}).obj(t)["restored"]; n != float64(1) {
		t.Fatal(n)
	}
	st := call(t, e.a1, "raypass.status", nil).obj(t)
	if st["items"] != float64(1) || st["trash"] != float64(1) {
		t.Fatal(st)
	}
	if n := call(t, e.a1, "raypass.purge", map[string]any{"all_trash": true}).obj(t)["purged"]; n != float64(1) {
		t.Fatal(n)
	}
	expectErr(t, raw(e.a2, "raypass.get", map[string]any{"id": id}, opts{}), 404, "not_found")
	for _, k := range e.a2.st.Keys() {
		if strings.Contains(k, id) {
			t.Fatal("purged key left on the other device")
		}
	}
}

func TestLoginsMatchFillSaveTOTP(t *testing.T) {
	e := newEnv(t)
	call(t, e.a1, "raypass.setup", map[string]any{"password": pw})
	gh := call(t, e.a1, "raypass.add", map[string]any{"type": "login", "name": "GitHub", "user": "octo", "pass": "gh-pass",
		"urls": []string{"https://github.com"}, "totp": "otpauth://totp/GitHub:octo?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", "fav": true}).obj(t)["id"].(string)
	gist := call(t, e.a1, "raypass.add", map[string]any{"type": "login", "name": "Gist", "user": "octo2", "pass": "p", "urls": []string{"https://gist.github.com"}}).obj(t)["id"].(string)
	call(t, e.a1, "raypass.add", map[string]any{"type": "login", "name": "Other", "user": "x", "pass": "p", "urls": []string{"https://other.com"}})

	rows := call(t, e.a1, "raypass.match", map[string]any{"url": "https://gist.github.com/me"}).arr(t)
	if len(rows) != 2 || rows[0]["id"] != gist || rows[1]["id"] != gh {
		t.Fatalf("match order %v", rows)
	}
	for _, r := range rows {
		if _, leaked := r["pass"]; leaked {
			t.Fatal("match rows carry a password")
		}
	}
	fill := call(t, e.a1, "raypass.fill", map[string]any{"id": gh, "url": "https://github.com/login"}).obj(t)
	if fill["user"] != "octo" || fill["pass"] != "gh-pass" || len(fill["totp"].(string)) != 6 {
		t.Fatal("fill output")
	}
	if msg := expectErr(t, raw(e.a1, "raypass.fill", map[string]any{"id": gh, "url": "https://github.com.evil.com"}, opts{}), 403, "forbidden"); msg != "url does not match" {
		t.Fatal(msg)
	}
	e.clk.mu.Lock()
	e.clk.now = time.Unix(1234567890, 0)
	e.clk.mu.Unlock()
	code := call(t, e.a1, "raypass.totp", map[string]any{"id": gh}).obj(t)
	// The same code as the vector in testdata/vectors.json for this secret and time.
	if code["code"] != "005924" || code["period"] != float64(30) || code["digits"] != float64(6) || code["remaining"] != float64(30-1234567890%30) {
		t.Fatalf("totp %v", code)
	}
	expectErr(t, raw(e.a1, "raypass.totp", map[string]any{"id": gist}, opts{}), 400, "bad_request")

	// save_login: same user + matching URL updates (history kept); same password changes nothing.
	s := call(t, e.a1, "raypass.save_login", map[string]any{"url": "https://github.com/session?token=abc", "user": "octo", "pass": "gh-new"}).obj(t)
	if s["id"] != gh || s["created"] != false {
		t.Fatal(s)
	}
	item := call(t, e.a1, "raypass.get", map[string]any{"id": gh}).obj(t)
	if item["pass"] != "gh-new" || len(item["history"].([]any)) != 1 {
		t.Fatal("save_login did not update")
	}
	before := len(e.feedBodies(t, FeedVault))
	call(t, e.a1, "raypass.save_login", map[string]any{"url": "https://github.com", "user": "octo", "pass": "gh-new"})
	if len(e.feedBodies(t, FeedVault)) != before {
		t.Fatal("unchanged login rewritten")
	}
	s = call(t, e.a1, "raypass.save_login", map[string]any{"url": "https://www.newsite.org/login?next=/x", "user": "me", "pass": "pw"}).obj(t)
	if s["created"] != true {
		t.Fatal(s)
	}
	item = call(t, e.a1, "raypass.get", map[string]any{"id": s["id"]}).obj(t)
	if item["name"] != "newsite.org" || item["urls"].([]any)[0] != "https://www.newsite.org" {
		t.Fatalf("saved %v %v", item["name"], item["urls"])
	}
	// list: favourites first, then by name; filters.
	all := call(t, e.a1, "raypass.list", nil).arr(t)
	if all[0]["id"] != gh || all[1]["name"] != "Gist" {
		t.Fatalf("order %v", all)
	}
	if q := call(t, e.a1, "raypass.list", map[string]any{"query": "OCTO2"}).arr(t); len(q) != 1 || q[0]["id"] != gist {
		t.Fatal("query")
	}
	if q := call(t, e.a1, "raypass.list", map[string]any{"url": "https://github.com"}).arr(t); len(q) != 2 {
		t.Fatal("url filter")
	}
	if q := call(t, e.a1, "raypass.list", map[string]any{"fav": true}).arr(t); len(q) != 1 || q[0]["has_totp"] != true {
		t.Fatal("fav filter")
	}
}

func TestFoldersHealthGenerate(t *testing.T) {
	e := newEnv(t)
	call(t, e.a1, "raypass.setup", map[string]any{"password": pw})
	a := call(t, e.a1, "raypass.add", map[string]any{"type": "login", "name": "A", "pass": "password", "folder": "Work/Servers"}).obj(t)["id"].(string)
	b := call(t, e.a1, "raypass.add", map[string]any{"type": "login", "name": "B", "pass": "kq8#Lm2!vZ-9xR&w4TpQ", "folder": "Work"}).obj(t)["id"].(string)
	c := call(t, e.a1, "raypass.add", map[string]any{"type": "login", "name": "C", "pass": "kq8#Lm2!vZ-9xR&w4TpQ"}).obj(t)["id"].(string)
	call(t, e.a1, "raypass.add", map[string]any{"type": "login", "name": "D", "pass": "another-Strong-pass-19!"})
	f := call(t, e.a1, "raypass.folders", nil).arr(t)
	if len(f) != 2 || f[0]["name"] != "Work" || f[1]["name"] != "Work/Servers" || f[1]["count"] != float64(1) {
		t.Fatal(f)
	}
	if rows := call(t, e.a1, "raypass.list", map[string]any{"folder": "Work"}).arr(t); len(rows) != 2 {
		t.Fatal("folder filter includes subfolders")
	}
	e.clk.advance(400 * 24 * time.Hour)
	call(t, e.a1, "raypass.unlock", map[string]any{"password": pw}) // auto-locked by the jump
	h := call(t, e.a1, "raypass.health", nil).obj(t)
	weak := h["weak"].([]any)
	reused := h["reused"].([]any)
	if len(weak) != 1 || weak[0] != a || len(reused) != 1 || len(h["old"].([]any)) != 4 {
		t.Fatalf("health %v", h)
	}
	pair := reused[0].([]any)
	if !(pair[0] == b && pair[1] == c || pair[0] == c && pair[1] == b) || h["score"] != float64(0) {
		t.Fatalf("health %v", h)
	}
	if strings.Contains(fmtJSON(h), "kq8#") {
		t.Fatal("health leaks a password")
	}
	g := call(t, e.a1, "raypass.generate", map[string]any{"length": 32, "symbols": false}).obj(t)
	if p := g["password"].(string); len(p) != 32 || strings.ContainsAny(p, "!#$%&*") || g["entropy_bits"].(float64) < 180 {
		t.Fatal("generate")
	}
	g = call(t, e.a1, "raypass.generate", map[string]any{"mode": "words", "words": 4, "separator": " "}).obj(t)
	if len(strings.Fields(g["password"].(string))) != 4 {
		t.Fatal("words")
	}
	expectErr(t, raw(e.a1, "raypass.generate", map[string]any{"length": 2}, opts{}), 400, "bad_request")
}

func fmtJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestImportExportRoundTrip(t *testing.T) {
	e := newEnv(t)
	call(t, e.a1, "raypass.setup", map[string]any{"password": pw})
	call(t, e.a1, "raypass.add", map[string]any{"type": "login", "name": "GitHub", "user": "octo", "pass": "p", "urls": []string{"https://github.com"},
		"fields": []map[string]any{{"name": "PIN", "value": "1", "hidden": true}}, "tags": []string{"dev"}, "folder": "Work"})
	call(t, e.a1, "raypass.add", map[string]any{"type": "wallet", "name": "BTC", "seed": "abandon about", "network": "bitcoin"})
	call(t, e.a1, "raypass.add", map[string]any{"type": "card", "name": "Visa", "number": "4111", "cvv": "123", "exp": "07/29"})

	for _, format := range []string{"json", "encrypted"} {
		args := map[string]any{"format": format}
		if format == "encrypted" {
			args["password"] = "export password"
		}
		out := call(t, e.a1, "raypass.export", args).obj(t)
		data := out["data"].(string)
		if !strings.HasPrefix(out["filename"].(string), "raypass-export-") {
			t.Fatal(out["filename"])
		}
		// Into a fresh vault (Bob's) and back out: same items.
		n := memsol.New()
		bob := n.AddPerson("Bob")
		b := New(bob.Client(AppID), Config{Clock: e.clk, KDFIter: testIter, MinIter: testIter})
		if err := b.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		defer b.Stop()
		call(t, b, "raypass.setup", map[string]any{"password": pw})
		in := map[string]any{"data": data}
		if format == "encrypted" {
			in["password"] = "export password"
		}
		res := call(t, b, "raypass.import", in).obj(t)
		if res["imported"] != float64(3) || res["skipped"] != float64(0) {
			t.Fatalf("%s: %v", format, res)
		}
		// Importing again adds nothing.
		if res := call(t, b, "raypass.import", in).obj(t); res["imported"] != float64(0) || res["skipped"] != float64(3) {
			t.Fatalf("re-import: %v", res)
		}
		x := strip(call(t, e.a1, "raypass.export", map[string]any{"format": "json"}).obj(t)["data"].(string))
		y := strip(call(t, b, "raypass.export", map[string]any{"format": "json"}).obj(t)["data"].(string))
		if x != y {
			t.Fatalf("%s round trip differs:\n%s\n%s", format, x, y)
		}
	}
	csv := call(t, e.a1, "raypass.export", map[string]any{"format": "csv"}).obj(t)["data"].(string)
	if !strings.HasPrefix(csv, "folder,favorite,type,name") {
		t.Fatal("csv header")
	}
	expectErr(t, raw(e.a1, "raypass.export", map[string]any{"format": "encrypted"}, opts{}), 400, "bad_request")
	expectErr(t, raw(e.a1, "raypass.import", map[string]any{"data": "garbage"}, opts{}), 400, "bad_request")
}

// strip drops the export time and per-vault timestamps so two vaults' exports compare.
func strip(data string) string {
	var doc struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal([]byte(data), &doc)
	for _, it := range doc.Items {
		delete(it, "created")
		delete(it, "updated")
	}
	sort.Slice(doc.Items, func(i, j int) bool { return doc.Items[i]["id"].(string) < doc.Items[j]["id"].(string) })
	return fmtJSON(doc.Items)
}

func TestOrphanedVault(t *testing.T) {
	e := newEnv(t)
	call(t, e.a1, "raypass.setup", map[string]any{"password": pw})
	call(t, e.a1, "raypass.add", map[string]any{"type": "note", "name": "mine"})
	// Another device set up a second vault while offline: a later keyring with another id, and
	// an entry under it.
	c := e.alice.Devices[1].Client(AppID)
	vk := vault.NewVaultKey()
	kr, err := vault.WrapKeyring("other password", vk, vault.KeyringParams{ID: "ffffffffffffffff", Salt: vault.RandomBytes(16), Nonce: vault.RandomBytes(12),
		Iter: testIter, Stamp: e.a1.clk.Now()})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := c.Append(ctx, FeedKeys, kr); err != nil {
		t.Fatal(err)
	}
	ents, _ := vault.EncryptOps(vk, kr.ID, nil)
	p, _ := vault.MarshalBatch(nil)
	ent, _ := vault.EncryptBatch(vk, kr.ID, p)
	ents = append(ents, ent)
	for _, x := range ents {
		if _, err := c.Append(ctx, FeedVault, x); err != nil {
			t.Fatal(err)
		}
	}
	st := call(t, e.a1, "raypass.status", nil).obj(t)
	if st["orphaned"] != float64(1) || st["items"] != float64(1) {
		t.Fatal(st)
	}
	// The first vault still wins: its password unlocks, the other does not.
	call(t, e.a1, "raypass.lock", nil)
	expectErr(t, raw(e.a1, "raypass.unlock", map[string]any{"password": "other password"}, opts{}), 401, "unauthorized")
	call(t, e.a1, "raypass.unlock", map[string]any{"password": pw})
	expectErr(t, raw(e.a1, "raypass.setup", map[string]any{"password": "other password"}, opts{}), 409, "conflict")
}

func TestServesUI(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/", "/kit/kit.css", "/sol.js"} {
		w := httptest.NewRecorder()
		e.a1.Handler().ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d", p, w.Code)
		}
	}
	r := raw(e.a1, "raypass.status", nil, opts{})
	_ = r
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/_sol/actions/raypass.status", strings.NewReader("{}"))
	req.Header.Set("Sol-Self", "1")
	e.a1.Handler().ServeHTTP(w, req)
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("action responses must not be cached")
	}
}

func TestManifestMatchesDesign(t *testing.T) {
	m, err := sol.ParseManifest(ManifestJSON)
	if err != nil {
		t.Fatal(err)
	}
	reveals := map[string]bool{"raypass.get": true, "raypass.totp": true, "raypass.fill": true, "raypass.export": true}
	for _, s := range m.Actions {
		if reveals[s.Name] && s.Approval != sol.ApprovalAgents {
			t.Errorf("%s reveals secrets but agents need no approval", s.Name)
		}
		if s.Effect != sol.EffectRead && s.Name != "raypass.lock" && s.Approval != sol.ApprovalAgents {
			t.Errorf("%s changes the vault but agents need no approval", s.Name)
		}
	}
	if len(m.Actions) != 22 {
		t.Fatalf("%d actions", len(m.Actions))
	}
}
