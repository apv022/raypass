package raypass

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/apv022/sol/sdk/go/sol"
	"github.com/apv022/sol/sdk/go/sol/memsol"
)

// partitioned is one person's two devices that cannot reach each other: each runs in its own
// memsol network (person and device ids are derived from names, so they are the same person
// and devices in both). heal copies the laptop's feed entries into the phone's network, the way
// replication delivers them once the devices meet again.
type partitioned struct {
	clk          *fakeClock
	phoneNet     *memsol.Network
	laptopNet    *memsol.Network
	phone        *App // in phoneNet
	laptop       *App // in laptopNet
	laptopInMain *memsol.Device
}

func newPartitioned(t *testing.T) *partitioned {
	t.Helper()
	p := &partitioned{clk: &fakeClock{now: time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)}}
	mk := func() (*memsol.Network, *memsol.Person, *memsol.Device) {
		n := memsol.New()
		n.Now = p.clk.Now
		alice := n.AddPerson("Alice")
		laptop := alice.AddDevice("laptop", false)
		return n, alice, laptop
	}
	var alice1, alice2 *memsol.Person
	var laptop2 *memsol.Device
	p.phoneNet, alice1, p.laptopInMain = mk()
	p.laptopNet, alice2, laptop2 = mk()
	if alice1.ID != alice2.ID || p.laptopInMain.ID != laptop2.ID {
		t.Fatal("memsol ids are not stable across networks")
	}
	e := &env{clk: p.clk}
	p.phone = start(t, e, alice1.Devices[0])
	p.laptop = start(t, e, laptop2)
	return p
}

func (p *partitioned) heal(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	from := p.laptop.api
	to := p.laptopInMain.Client(AppID)
	for _, feed := range []string{FeedKeys, FeedVault} {
		es, err := from.Read(ctx, sol.Address(p.laptop.me, AppID, feed), 1, 5000)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range es {
			if _, err := to.Append(ctx, feed, json.RawMessage(e.Body)); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func names(t *testing.T, a *App) []string {
	t.Helper()
	var out []string
	for _, r := range call(t, a, "raypass.list", nil).arr(t) {
		out = append(out, r["name"].(string))
	}
	sort.Strings(out)
	return out
}

// Two devices set up a vault each while offline; once they meet, the earlier vault wins and
// the other's items are orphaned until raypass.recover brings them in with its password.
func TestRecoverOrphanedVault(t *testing.T) {
	p := newPartitioned(t)
	const pw2 = "the laptop's own password"
	call(t, p.phone, "raypass.setup", map[string]any{"password": pw})
	call(t, p.phone, "raypass.add", map[string]any{"type": "login", "name": "Shared", "user": "me", "pass": "same", "urls": []string{"https://shared.example"}})
	p.clk.advance(time.Second) // the laptop's vault is the later one
	call(t, p.laptop, "raypass.setup", map[string]any{"password": pw2})
	call(t, p.laptop, "raypass.add", map[string]any{"type": "login", "name": "Shared", "user": "me", "pass": "same", "urls": []string{"https://shared.example"}})
	call(t, p.laptop, "raypass.add", map[string]any{"type": "login", "name": "Laptop login", "user": "lap", "pass": "lap-pass", "urls": []string{"https://laptop.example"},
		"totp": "otpauth://totp/x?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", "folder": "Work", "fav": true, "tags": []string{"t"}})
	call(t, p.laptop, "raypass.add", map[string]any{"type": "note", "name": "Laptop note", "notes": "n"})
	gone := call(t, p.laptop, "raypass.add", map[string]any{"type": "note", "name": "Trashed there"}).obj(t)["id"]
	call(t, p.laptop, "raypass.trash", map[string]any{"ids": []any{gone}})
	call(t, p.laptop, "raypass.update", map[string]any{"id": call(t, p.laptop, "raypass.list", map[string]any{"query": "Laptop login"}).arr(t)[0]["id"], "pass": "lap-pass-2"})
	laptopKid := p.laptop.keyring().Current.ID

	p.heal(t)
	st := call(t, p.phone, "raypass.status", nil).obj(t)
	if ok := st["orphaned_keyrings"].([]any); len(ok) != 1 || ok[0] != laptopKid || st["orphaned"].(float64) == 0 {
		t.Fatalf("status %v", st)
	}
	if got := names(t, p.phone); len(got) != 1 {
		t.Fatalf("before recover: %v", got)
	}

	// Refusals: wrong password, unknown keyring, bad input, locked.
	expectErr(t, raw(p.phone, "raypass.recover", map[string]any{"password": pw}, opts{}), 401, "unauthorized")
	expectErr(t, raw(p.phone, "raypass.recover", map[string]any{"password": pw2, "keyring": "0000000000000000"}, opts{}), 404, "not_found")
	expectErr(t, raw(p.phone, "raypass.recover", map[string]any{"password": pw2, "keyring": "nope"}, opts{}), 400, "bad_request")
	expectErr(t, raw(p.phone, "raypass.recover", map[string]any{}, opts{}), 400, "bad_request")

	// A dry run counts and changes nothing.
	before := len(p.phoneFeed(t))
	d := raw(p.phone, "raypass.recover", map[string]any{"password": pw2}, opts{dry: true})
	if d.code != 200 || d.obj(t)["would"].(map[string]any)["recovered"] != float64(2) {
		t.Fatalf("dry run %s", d.body)
	}
	if len(p.phoneFeed(t)) != before {
		t.Fatal("dry run wrote")
	}

	r := call(t, p.phone, "raypass.recover", map[string]any{"password": pw2, "keyring": laptopKid}).obj(t)
	if r["recovered"] != float64(2) || r["skipped"] != float64(1) {
		t.Fatalf("recover %v", r)
	}
	if got := names(t, p.phone); len(got) != 3 || got[0] != "Laptop login" || got[1] != "Laptop note" || got[2] != "Shared" {
		t.Fatalf("after recover: %v", got)
	}
	row := call(t, p.phone, "raypass.list", map[string]any{"query": "Laptop login"}).arr(t)[0]
	item := call(t, p.phone, "raypass.get", map[string]any{"id": row["id"]}).obj(t)
	if item["pass"] != "lap-pass-2" || item["folder"] != "Work" || item["fav"] != true || len(item["history"].([]any)) != 1 || item["totp"] == "" {
		t.Fatalf("recovered item lost fields: folder=%v fav=%v history=%d", item["folder"], item["fav"], len(item["history"].([]any)))
	}
	if _, same := p.laptop.st.Get(row["id"].(string)); same {
		t.Fatal("recovered item kept its old id")
	}
	// Again: nothing new.
	if r := call(t, p.phone, "raypass.recover", map[string]any{"password": pw2}).obj(t); r["recovered"] != float64(0) || r["skipped"] != float64(3) {
		t.Fatalf("second recover %v", r)
	}
	call(t, p.phone, "raypass.lock", nil)
	if msg := expectErr(t, raw(p.phone, "raypass.recover", map[string]any{"password": pw2}, opts{}), 403, "forbidden"); msg != "locked" {
		t.Fatal(msg)
	}
}

func TestRecoverWithoutOrphans(t *testing.T) {
	e := newEnv(t)
	call(t, e.a1, "raypass.setup", map[string]any{"password": pw})
	if st := call(t, e.a1, "raypass.status", nil).obj(t); len(st["orphaned_keyrings"].([]any)) != 0 {
		t.Fatal(st)
	}
	expectErr(t, raw(e.a1, "raypass.recover", map[string]any{"password": pw}, opts{}), 404, "not_found")
}

func (p *partitioned) phoneFeed(t *testing.T) []sol.Entry {
	t.Helper()
	es, err := p.phone.api.Read(context.Background(), sol.Address(p.phone.me, AppID, FeedVault), 1, 5000)
	if err != nil {
		t.Fatal(err)
	}
	return es
}
