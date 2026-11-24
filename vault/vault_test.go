package vault

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/apv022/sol/sdk/go/crdt"
)

func TestKeyringSetupUnwrapRewrap(t *testing.T) {
	vk := NewVaultKey()
	kr, err := NewKeyring("first password", vk, MinIter, crdt.Stamp{Wall: 1, Device: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if !kr.Valid(MinIter) || !ValidID(kr.ID) {
		t.Fatalf("invalid keyring %+v", kr.KDF)
	}
	got, err := kr.Unwrap("first password")
	if err != nil || string(got) != string(vk) {
		t.Fatalf("unwrap: %v", err)
	}
	if _, err := kr.Unwrap("wrong"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("wrong password: %v", err)
	}
	kr2, err := Rewrap(kr, "second password", vk, MinIter, crdt.Stamp{Wall: 2, Device: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if kr2.ID != kr.ID || kr2.KDF.Salt == kr.KDF.Salt || kr2.Nonce == kr.Nonce {
		t.Fatal("rewrap must keep the id and change salt and nonce")
	}
	if got, err := kr2.Unwrap("second password"); err != nil || string(got) != string(vk) {
		t.Fatal("rewrapped key")
	}
	if _, err := kr2.Unwrap("first password"); err == nil {
		t.Fatal("old password still opens the new keyring")
	}
	if _, err := NewKeyring("x", vk, MinIter-1, crdt.Stamp{}); err == nil {
		t.Fatal("iter below minimum accepted")
	}
	// A keyring whose check does not match is corrupt, not a wrong password.
	bad := kr
	bad.Check = B64.EncodeToString(make([]byte, 32))
	if _, err := bad.Unwrap("first password"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("bad check: %v", err)
	}
	body, _ := json.Marshal(kr)
	for _, s := range []string{"first password", string(vk)} {
		if strings.Contains(string(body), s) {
			t.Fatal("keyring body holds plaintext")
		}
	}
}

func TestChooseKeyring(t *testing.T) {
	mk := func(id string, w int64, d string) Keyring {
		kr, err := WrapKeyring("p", seq(1, 32), KeyringParams{ID: id, Salt: seq(0, 16), Nonce: seq(0, 12), Iter: MinIter, Stamp: crdt.Stamp{Wall: w, Device: d}})
		if err != nil {
			t.Fatal(err)
		}
		return kr
	}
	a1, a2, b1 := mk("aaaaaaaaaaaaaaaa", 10, "x"), mk("aaaaaaaaaaaaaaaa", 30, "x"), mk("bbbbbbbbbbbbbbbb", 20, "y")
	weak := mk("cccccccccccccccc", 1, "z")
	weak.KDF.Iter = 10
	got := ChooseKeyring([]Keyring{b1, a2, weak, a1}, MinIter)
	if !got.OK || got.Current.ID != a1.ID || got.Current.Stamp.Wall != 30 || len(got.Others) != 1 || got.Others[0] != b1.ID {
		t.Fatalf("%+v", got)
	}
	if ChooseKeyring(nil, MinIter).OK {
		t.Fatal("empty")
	}
}

func TestEntryRoundTripAndSplit(t *testing.T) {
	vk := NewVaultKey()
	big := strings.Repeat("plaintext-marker ", 1800)
	var ops []crdt.MapOp
	for i := 0; i < 10; i++ {
		raw, _ := json.Marshal(big)
		ops = append(ops, crdt.MapOp{Key: "i.0000000000000001.notes", Value: raw, Stamp: crdt.Stamp{Wall: int64(i), Device: "d"}})
	}
	ents, err := EncryptOps(vk, "0123456789abcdef", ops)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) < 5 {
		t.Fatalf("expected a split, got %d entries", len(ents))
	}
	var back []crdt.MapOp
	for _, e := range ents {
		body, _ := json.Marshal(e)
		if len(body) > MaxBody {
			t.Fatalf("entry body %d bytes", len(body))
		}
		if strings.Contains(string(body), "marker") {
			t.Fatal("plaintext in entry")
		}
		pe, ok := ParseEntry(body)
		if !ok {
			t.Fatal("parse")
		}
		got, err := pe.Decrypt(vk)
		if err != nil {
			t.Fatal(err)
		}
		back = append(back, got...)
	}
	if len(back) != len(ops) || back[9].Stamp.Wall != 9 {
		t.Fatal("ops lost in split")
	}
	if _, err := ents[0].Decrypt(NewVaultKey()); !errors.Is(err, ErrCorrupt) {
		t.Fatal("wrong key decrypted")
	}
	huge, _ := json.Marshal(strings.Repeat("y", MaxBody))
	if _, err := SplitOps([]crdt.MapOp{{Key: "k", Value: huge}}); err == nil {
		t.Fatal("an op over the limit was accepted")
	}
	// Tampering is detected (GCM tag, and the AAD binds the entry to the vault context).
	tam := ents[0]
	c, _ := B64.DecodeString(tam.C)
	c[0] ^= 1
	tam.C = B64.EncodeToString(c)
	if _, err := tam.Decrypt(vk); err == nil {
		t.Fatal("tampered entry decrypted")
	}
	p, _ := MarshalBatch(ops[:1])
	ks, _ := Seal(vk, seq(0, 12), p, AADKeyring)
	wrongAAD := Entry{V: 1, K: "x", N: B64.EncodeToString(seq(0, 12)), C: B64.EncodeToString(ks)}
	if _, err := wrongAAD.Decrypt(vk); err == nil {
		t.Fatal("ciphertext with another AAD decrypted as a vault entry")
	}
}

func TestWipe(t *testing.T) {
	b := []byte{1, 2, 3}
	Wipe(b)
	if b[0]|b[1]|b[2] != 0 {
		t.Fatal("not wiped")
	}
}

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func newState(dev string) *State {
	c := crdt.NewClock(dev)
	return NewState(c)
}

func TestItemOps(t *testing.T) {
	s := newState("a")
	id, ops, err := s.Add(TypeLogin, map[string]json.RawMessage{"name": raw(" GitHub "), "user": raw("octo"), "pass": raw("p1"),
		"urls": raw([]string{" https://github.com ", "", "https://github.com "}), "tags": raw([]string{"dev"}), "folder": raw(" Work / Code "), "fav": raw(true)}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	s.Apply(ops)
	it, ok := s.Get(id)
	if !ok || it.Name != "GitHub" || it.User != "octo" || it.Pass != "p1" || len(it.URLs) != 1 || it.Folder != "Work/Code" || !it.Fav || it.Created != 1000 {
		t.Fatalf("%+v", it)
	}
	out := it.Output()
	for _, k := range []string{"id", "type", "name", "user", "pass", "urls", "totp", "history", "tags", "fields", "notes", "trashed"} {
		if _, ok := out[k]; !ok {
			t.Errorf("output lacks %s", k)
		}
	}
	if _, ok := out["cvv"]; ok {
		t.Error("login output has card fields")
	}

	// Password change records history, newest first, capped.
	for i := 2; i <= 25; i++ {
		ops, err := s.Update(id, map[string]json.RawMessage{"pass": raw("p" + string(rune('a'+i)))}, int64(1000+i))
		if err != nil {
			t.Fatal(err)
		}
		s.Apply(ops)
	}
	it, _ = s.Get(id)
	if len(it.History) != MaxHistory || it.History[0].Pass != "py" || it.History[0].Until != 1025 || it.Updated != 1025 {
		t.Fatalf("history %d %+v", len(it.History), it.History[0])
	}
	// Same password: no history entry.
	ops, _ = s.Update(id, map[string]json.RawMessage{"pass": raw(it.Pass)}, 2000)
	s.Apply(ops)
	if it2, _ := s.Get(id); it2.History[0].Until != 1025 {
		t.Fatal("unchanged password added history")
	}
	// null clears.
	ops, err = s.Update(id, map[string]json.RawMessage{"user": json.RawMessage("null")}, 2001)
	if err != nil {
		t.Fatal(err)
	}
	s.Apply(ops)
	if it, _ = s.Get(id); it.User != "" {
		t.Fatal("not cleared")
	}
	// Validation.
	for _, in := range []map[string]json.RawMessage{
		{"cvv": raw("123")}, {"type": raw("card")}, {"name": raw("")}, {"fav": raw("yes")}, {"urls": raw("x")}, {"created": raw(5)},
	} {
		if _, err := s.Update(id, in, 1); err == nil {
			t.Errorf("accepted %v", in)
		} else if _, ok := err.(BadInput); !ok {
			t.Errorf("not BadInput: %T", err)
		}
	}
	if _, _, err := s.Add("bogus", map[string]json.RawMessage{"name": raw("x")}, 1); err == nil {
		t.Fatal("bad type")
	}
	if _, _, err := s.Add(TypeNote, map[string]json.RawMessage{}, 1); err == nil {
		t.Fatal("no name")
	}
	if _, err := s.Update("ffffffffffffffff", map[string]json.RawMessage{"name": raw("x")}, 1); err != ErrNoItem {
		t.Fatal("missing item")
	}

	// Trash, restore, purge.
	if _, _, err := s.Purge([]string{id}); err == nil {
		t.Fatal("purged an item not in the trash")
	}
	ops, n, err := s.Trash([]string{id, id}, 3000)
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	s.Apply(ops)
	if it, _ = s.Get(id); it.Trashed != 3000 {
		t.Fatal("not trashed")
	}
	ops, n, _ = s.Restore([]string{id}, 3001)
	s.Apply(ops)
	if it, _ = s.Get(id); n != 1 || it.Trashed != 0 {
		t.Fatal("not restored")
	}
	ops, _, _ = s.Trash([]string{id}, 3002)
	s.Apply(ops)
	ops, n, err = s.Purge([]string{id})
	if err != nil || n != 1 {
		t.Fatal(err)
	}
	s.Apply(ops)
	if _, ok := s.Get(id); ok {
		t.Fatal("purged item still there")
	}
	for _, k := range s.Keys() {
		if strings.HasPrefix(k, "i."+id) {
			t.Fatalf("key %s survived purge", k)
		}
	}
}

func TestFoldConvergesAnyOrder(t *testing.T) {
	a, b := newState("a"), newState("b")
	id, ops1, _ := a.Add(TypeNote, map[string]json.RawMessage{"name": raw("n"), "notes": raw("one")}, 1)
	a.Apply(ops1)
	b.Apply(ops1)
	opsA, _ := a.Update(id, map[string]json.RawMessage{"notes": raw("from a")}, 2)
	opsB, _ := b.Update(id, map[string]json.RawMessage{"name": raw("from b")}, 2)
	// Apply in opposite orders, twice.
	a.Apply(opsA)
	a.Apply(opsB)
	a.Apply(opsA)
	b.Apply(opsB)
	b.Apply(opsA)
	ia, _ := a.Get(id)
	ib, _ := b.Get(id)
	if ia.Notes != "from a" || ia.Name != "from b" || ib.Notes != ia.Notes || ib.Name != ia.Name {
		t.Fatalf("%+v vs %+v", ia, ib)
	}
	// After observing b's ops, a's next write wins over them.
	ops, _ := a.Update(id, map[string]json.RawMessage{"name": raw("later")}, 3)
	b.Apply(ops)
	if ib, _ = b.Get(id); ib.Name != "later" {
		t.Fatal("clock not observed")
	}
	// Junk keys are ignored.
	a.Apply([]crdt.MapOp{{Key: "../etc", Value: raw("x"), Stamp: crdt.Stamp{Wall: 9, Device: "z"}}, {Key: "i.short.name", Value: raw("x"), Stamp: crdt.Stamp{Wall: 9, Device: "z"}}})
	for _, k := range a.Keys() {
		if k == "../etc" || k == "i.short.name" {
			t.Fatal("junk key folded")
		}
	}
}

func TestSettings(t *testing.T) {
	s := newState("a")
	if st := s.Settings(); st.AutoLock != DefaultAutoLock || st.ClipClear != DefaultClipClear {
		t.Fatal(st)
	}
	z, c := 0, 45
	ops, err := s.SetSettings(&z, &c)
	if err != nil {
		t.Fatal(err)
	}
	s.Apply(ops)
	if st := s.Settings(); st.AutoLock != 0 || st.ClipClear != 45 {
		t.Fatal(st)
	}
	neg := -1
	if _, err := s.SetSettings(&neg, nil); err == nil {
		t.Fatal("negative auto_lock")
	}
	s.Wipe()
	if len(s.Keys()) != 0 {
		t.Fatal("wipe")
	}
}

func TestExportRoundTrip(t *testing.T) {
	f, err := SealExport("pw", []byte(`{"x":1}`), MinIter)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(f)
	if !IsExportFile(b) {
		t.Fatal("not recognized")
	}
	p, err := OpenExport(b, "pw", MinIter)
	if err != nil || string(p) != `{"x":1}` {
		t.Fatal(err)
	}
	if _, err := OpenExport(b, "nope", MinIter); !errors.Is(err, ErrWrongPassword) {
		t.Fatal(err)
	}
}

// A far-future stamp (a device whose clock ran years ahead) does not freeze a field: applying
// it moves this device's clock past it, so the next local write still wins.
func TestFarFutureStampDoesNotFreeze(t *testing.T) {
	clk := crdt.NewClock("me")
	clk.Wall = func() int64 { return 1795121162000 }
	s := NewState(clk)
	id := "0123456789abcdef"
	future := crdt.Stamp{Wall: 1795121162000 * 100, Counter: 7, Device: "ahead"}
	s.Apply([]crdt.MapOp{
		{Key: "i." + id + ".type", Value: json.RawMessage(`"note"`), Stamp: crdt.Stamp{Wall: 1795121162000, Device: "other"}},
		{Key: "i." + id + ".name", Value: json.RawMessage(`"from the future"`), Stamp: future},
	})
	ops, err := s.Update(id, map[string]json.RawMessage{"name": json.RawMessage(`"mine"`)}, 1)
	if err != nil {
		t.Fatal(err)
	}
	s.Apply(ops)
	if it, _ := s.Get(id); it.Name != "mine" {
		t.Fatalf("a local write lost to a far-future stamp: %q", it.Name)
	}
}
