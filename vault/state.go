package vault

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"github.com/apv022/sol/sdk/go/crdt"
)

// Settings are the synced settings (s.* keys).
type Settings struct {
	AutoLock  int `json:"auto_lock"`  // minutes, 0 = never
	ClipClear int `json:"clip_clear"` // seconds
}

// Setting defaults and bounds.
const (
	DefaultAutoLock  = 15
	DefaultClipClear = 30
	MaxAutoLock      = 7 * 24 * 60
	MaxClipClear     = 3600
)

// State is the decrypted vault: the LWW map folded from every op of every entry, and the item
// view derived from it. Writes return ops without applying them: the caller encrypts and
// appends them, then applies them (Apply is idempotent, so seeing them again from the feed is
// harmless). Safe for concurrent use.
type State struct {
	mu    sync.Mutex
	m     *crdt.Map
	clk   *crdt.Clock
	items map[string]*Item
	dirty bool
}

// NewState makes an empty state whose writes are stamped by clk.
func NewState(clk *crdt.Clock) *State {
	return &State{m: crdt.NewMap(), clk: clk, dirty: true}
}

// validKey accepts s.<name> and i.<16 hex>.<field> keys; anything else in a decrypted batch is
// ignored (it can only come from a newer or broken Raypass of the same person).
func validKey(k string) bool {
	if name, ok := strings.CutPrefix(k, "s."); ok {
		return name != "" && len(name) < 64
	}
	rest, ok := strings.CutPrefix(k, "i.")
	if !ok || len(rest) < 18 || rest[16] != '.' || !ValidID(rest[:16]) {
		return false
	}
	f := rest[17:]
	if f == "" || len(f) > 32 {
		return false
	}
	for i := 0; i < len(f); i++ {
		if c := f[i]; !(c >= 'a' && c <= 'z' || c == '_' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// Apply folds ops into the map (any order, any number of times) and observes their stamps so
// the next local write wins over everything seen.
func (s *State) Apply(ops []crdt.MapOp) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, op := range ops {
		if !validKey(op.Key) || op.Stamp.Device == "" {
			continue
		}
		s.clk.ObserveOp(op)
		if s.m.Apply(op) {
			s.dirty = true
		}
	}
}

// Wipe drops the decrypted state. The map's strings cannot be zeroed in Go (they are
// immutable); dropping every reference lets the collector reclaim them.
func (s *State) Wipe() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m = crdt.NewMap()
	s.items = nil
	s.dirty = true
}

func (s *State) getLocked(k string, v any) bool {
	raw, ok := s.m.Get(k)
	if !ok {
		return false
	}
	return json.Unmarshal(raw, v) == nil
}

// rebuild re-derives the items from the map. s.mu held.
func (s *State) rebuild() {
	if !s.dirty {
		return
	}
	items := map[string]*Item{}
	for _, k := range s.m.Keys() {
		if strings.HasPrefix(k, "i.") && strings.HasSuffix(k, ".type") && len(k) == 2+16+5 {
			id := k[2:18]
			var t string
			if !s.getLocked(k, &t) || t == "" {
				continue
			}
			items[id] = s.buildLocked(id, t)
		}
	}
	s.items = items
	s.dirty = false
}

func (s *State) buildLocked(id, t string) *Item {
	p := "i." + id + "."
	it := &Item{ID: id, Type: t}
	str := func(f string) string {
		var v string
		s.getLocked(p+f, &v)
		return v
	}
	s.getLocked(p+"fav", &it.Fav)
	s.getLocked(p+"tags", &it.Tags)
	s.getLocked(p+"fields", &it.Fields)
	s.getLocked(p+"created", &it.Created)
	s.getLocked(p+"updated", &it.Updated)
	s.getLocked(p+"trashed", &it.Trashed)
	s.getLocked(p+"urls", &it.URLs)
	s.getLocked(p+"history", &it.History)
	it.Name, it.Folder, it.Notes = str("name"), str("folder"), str("notes")
	it.User, it.Pass, it.TOTP = str("user"), str("pass"), str("totp")
	it.Holder, it.Number, it.Exp, it.CVV, it.Brand = str("holder"), str("number"), str("exp"), str("cvv"), str("brand")
	it.First, it.Last, it.Email, it.Phone, it.Address, it.Company = str("first"), str("last"), str("email"), str("phone"), str("address"), str("company")
	it.Seed, it.Privkey, it.Network, it.Passphrase, it.Derivation = str("seed"), str("privkey"), str("network"), str("passphrase"), str("derivation")
	if it.Name == "" {
		it.Name = "(no name)"
	}
	return it
}

// Items returns every item, trashed ones included, sorted by id.
func (s *State) Items() []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rebuild()
	out := make([]Item, 0, len(s.items))
	for _, it := range s.items {
		out = append(out, *it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Get returns one item.
func (s *State) Get(id string) (Item, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rebuild()
	it := s.items[id]
	if it == nil {
		return Item{}, false
	}
	return *it, true
}

// Settings returns the synced settings with defaults.
func (s *State) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Settings{AutoLock: DefaultAutoLock, ClipClear: DefaultClipClear}
	var n int
	if s.getLocked("s.auto_lock", &n) && n >= 0 && n <= MaxAutoLock {
		st.AutoLock = n
	}
	if s.getLocked("s.clip_clear", &n) && n >= 0 && n <= MaxClipClear {
		st.ClipClear = n
	}
	return st
}

// Keys returns the live keys of the map (tests and diagnostics; values are secret).
func (s *State) Keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m.Keys()
}

// ---------- writes ----------

type opb struct {
	clk *crdt.Clock
	ops []crdt.MapOp
}

func (b *opb) setRaw(k string, raw json.RawMessage) {
	b.ops = append(b.ops, crdt.MapOp{Key: k, Value: append(json.RawMessage(nil), raw...), Stamp: b.clk.Now()})
}

func (b *opb) set(k string, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		panic("raypass: unmarshalable value") // only plain values reach here
	}
	b.setRaw(k, raw)
}

func (b *opb) del(k string) {
	b.ops = append(b.ops, crdt.MapOp{Key: k, Delete: true, Stamp: b.clk.Now()})
}

// NewID makes a random item id.
func NewID() string { return RandomHex(8) }

// Add returns the ops that create an item of type typ from in (field name → JSON value), with
// a new random id. Unknown fields are an error; null values are ignored. now is ms since epoch.
func (s *State) Add(typ string, in map[string]json.RawMessage, now int64) (string, []crdt.MapOp, error) {
	return s.AddWithID(NewID(), typ, in, now)
}

// AddWithID is Add with a chosen id (import of Raypass JSON keeps ids).
func (s *State) AddWithID(id, typ string, in map[string]json.RawMessage, now int64) (string, []crdt.MapOp, error) {
	if !ValidType(typ) {
		return "", nil, badf("type must be one of %s", strings.Join(Types, ", "))
	}
	if !ValidID(id) {
		return "", nil, badf("bad id")
	}
	vals, err := checkFields(typ, in)
	if err != nil {
		return "", nil, err
	}
	var name string
	if raw, ok := vals["name"]; ok && raw != nil {
		_ = json.Unmarshal(raw, &name)
	}
	if name == "" {
		return "", nil, badf("an item needs a name")
	}
	p := "i." + id + "."
	b := &opb{clk: s.clk}
	b.set(p+"type", typ)
	b.set(p+"created", now)
	b.set(p+"updated", now)
	b.set(p+"trashed", 0)
	for _, f := range sortedKeys(vals) {
		if vals[f] != nil {
			b.setRaw(p+f, vals[f])
		}
	}
	return id, b.ops, nil
}

// checkFields validates every input field for typ. A nil value in the result means "clear".
func checkFields(typ string, in map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	for f, raw := range in {
		if f == "id" || f == "type" {
			continue
		}
		k := kindOf(typ, f)
		if k == "" {
			if _, sys := systemFields[f]; sys {
				return nil, badf("%s cannot be set", f)
			}
			return nil, badf("a %s has no field %q", typ, f)
		}
		if isNull(raw) {
			out[f] = nil
			continue
		}
		v, err := normalize(f, k, raw)
		if err != nil {
			return nil, err
		}
		out[f] = v
	}
	return out, nil
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// Update returns the ops that set the given fields of an item (null clears one; the type
// cannot change). Changing a login's password records the previous one in its history.
func (s *State) Update(id string, in map[string]json.RawMessage, now int64) ([]crdt.MapOp, error) {
	it, ok := s.Get(id)
	if !ok {
		return nil, ErrNoItem
	}
	if raw, ok := in["type"]; ok {
		var t string
		if json.Unmarshal(raw, &t) != nil || t != it.Type {
			return nil, badf("the type of an item cannot change")
		}
	}
	vals, err := checkFields(it.Type, in)
	if err != nil {
		return nil, err
	}
	if raw, ok := vals["name"]; ok {
		var name string
		if raw != nil {
			_ = json.Unmarshal(raw, &name)
		}
		if name == "" {
			return nil, badf("an item needs a name")
		}
	}
	p := "i." + id + "."
	b := &opb{clk: s.clk}
	if raw, ok := vals["pass"]; ok && it.Type == TypeLogin {
		var np string
		if raw != nil {
			_ = json.Unmarshal(raw, &np)
		}
		if _, explicit := vals["history"]; !explicit && it.Pass != "" && np != it.Pass {
			h := append([]History{{Pass: it.Pass, Until: now}}, it.History...)
			if len(h) > MaxHistory {
				h = h[:MaxHistory]
			}
			b.set(p+"history", h)
		}
	}
	for _, f := range sortedKeys(vals) {
		if vals[f] == nil {
			b.del(p + f)
		} else {
			b.setRaw(p+f, vals[f])
		}
	}
	b.set(p+"updated", now)
	return b.ops, nil
}

// ErrNoItem is returned for an id that names no item.
var ErrNoItem = BadInput("no such item")

// Trash returns ops moving the items to the trash, and how many were not already there.
// Unknown ids are an error (ErrNoItem).
func (s *State) Trash(ids []string, now int64) ([]crdt.MapOp, int, error) {
	b := &opb{clk: s.clk}
	n := 0
	for _, id := range dedupe(ids) {
		it, ok := s.Get(id)
		if !ok {
			return nil, 0, ErrNoItem
		}
		if it.Trashed == 0 {
			b.set("i."+id+".trashed", now)
			n++
		}
	}
	return b.ops, n, nil
}

// Restore returns ops taking items out of the trash, and how many were in it.
func (s *State) Restore(ids []string, now int64) ([]crdt.MapOp, int, error) {
	b := &opb{clk: s.clk}
	n := 0
	for _, id := range dedupe(ids) {
		it, ok := s.Get(id)
		if !ok {
			return nil, 0, ErrNoItem
		}
		if it.Trashed != 0 {
			b.set("i."+id+".trashed", 0)
			b.set("i."+id+".updated", now)
			n++
		}
	}
	return b.ops, n, nil
}

// Purge returns ops deleting every key of the items, which must all be in the trash.
func (s *State) Purge(ids []string) ([]crdt.MapOp, int, error) {
	ids = dedupe(ids)
	for _, id := range ids {
		it, ok := s.Get(id)
		if !ok {
			return nil, 0, ErrNoItem
		}
		if it.Trashed == 0 {
			return nil, 0, badf("only items in the trash can be purged")
		}
	}
	keys := s.Keys()
	b := &opb{clk: s.clk}
	for _, id := range ids {
		p := "i." + id + "."
		for _, k := range keys {
			if strings.HasPrefix(k, p) {
				b.del(k)
			}
		}
	}
	return b.ops, len(ids), nil
}

// TrashedIDs lists the items in the trash.
func (s *State) TrashedIDs() []string {
	var out []string
	for _, it := range s.Items() {
		if it.Trashed != 0 {
			out = append(out, it.ID)
		}
	}
	return out
}

// SetSettings returns ops changing the given settings (nil leaves one as it is).
func (s *State) SetSettings(autoLock, clipClear *int) ([]crdt.MapOp, error) {
	b := &opb{clk: s.clk}
	if autoLock != nil {
		if *autoLock < 0 || *autoLock > MaxAutoLock {
			return nil, badf("auto_lock must be 0 to %d minutes", MaxAutoLock)
		}
		b.set("s.auto_lock", *autoLock)
	}
	if clipClear != nil {
		if *clipClear < 0 || *clipClear > MaxClipClear {
			return nil, badf("clip_clear must be 0 to %d seconds", MaxClipClear)
		}
		b.set("s.clip_clear", *clipClear)
	}
	return b.ops, nil
}

func dedupe(ids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
