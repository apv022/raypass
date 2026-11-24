package raypass

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/apv022/raypass/gen"
	"github.com/apv022/raypass/port"
	"github.com/apv022/raypass/totp"
	"github.com/apv022/raypass/urlmatch"
	"github.com/apv022/raypass/vault"
	"github.com/apv022/sol/sdk/go/crdt"
	"github.com/apv022/sol/sdk/go/sol"
)

// Every action: Sol-Self only, then drain the feeds (fresh), then (vault actions, setup, unlock,
// change_password) record activity for auto-lock. Every write honours Sol-Dry-Run: it validates exactly as the real call would, then
// describes the change without making it, and never echoes a secret in the description.

func bad(format string, a ...any) error { return sol.Errorf("bad_request", fmt.Sprintf(format, a...)) }

// inputErr maps vault/port validation errors to bad_request; anything else passes through.
func inputErr(err error) error {
	var bi vault.BadInput
	var pe port.Error
	switch {
	case err == nil:
		return nil
	case errors.Is(err, vault.ErrNoItem):
		return sol.Errorf("not_found", "no such item")
	case errors.As(err, &bi), errors.As(err, &pe):
		return sol.Errorf("bad_request", err.Error())
	}
	return err
}

func dry(would any) map[string]any { return map[string]any{"dry_run": true, "would": would} }

func ok() map[string]any { return map[string]any{"ok": true} }

func (a *App) registerActions(act *sol.Actions) {
	// handle wraps every action: Sol-Self only, drain the feeds, then (when active) record
	// activity for auto-lock. Only actions that use or open the vault are activity; status,
	// generate and lock are not, or a client that merely shows the lock state would keep the
	// vault unlocked forever.
	handle := func(name string, active bool, f func(ctx context.Context, c sol.Call) (any, error)) {
		act.Handle(name, func(ctx context.Context, c sol.Call) (any, error) {
			if !c.Caller.Self {
				return nil, sol.Errorf("forbidden", "Raypass answers only my own devices and agents")
			}
			if err := a.fresh(ctx); err != nil {
				return nil, err
			}
			if active {
				a.touch()
			}
			return f(ctx, c)
		})
	}
	// vh is for actions that need the unlocked vault (always activity).
	vh := func(name string, f func(ctx context.Context, c sol.Call, st *vault.State) (any, error)) {
		handle(name, true, func(ctx context.Context, c sol.Call) (any, error) {
			st, err := a.state()
			if err != nil {
				return nil, err
			}
			return f(ctx, c, st)
		})
	}
	handle("raypass.status", false, a.actStatus)
	handle("raypass.setup", true, a.actSetup)
	handle("raypass.unlock", true, a.actUnlock)
	handle("raypass.lock", false, a.actLock)
	handle("raypass.change_password", true, a.actChangePassword)
	handle("raypass.generate", false, a.actGenerate)
	vh("raypass.list", a.actList)
	vh("raypass.get", a.actGet)
	vh("raypass.add", a.actAdd)
	vh("raypass.update", a.actUpdate)
	vh("raypass.trash", a.actTrash)
	vh("raypass.restore", a.actRestore)
	vh("raypass.purge", a.actPurge)
	vh("raypass.totp", a.actTOTP)
	vh("raypass.match", a.actMatch)
	vh("raypass.fill", a.actFill)
	vh("raypass.save_login", a.actSaveLogin)
	vh("raypass.folders", a.actFolders)
	vh("raypass.health", a.actHealth)
	vh("raypass.export", a.actExport)
	vh("raypass.import", a.actImport)
	vh("raypass.settings", a.actSettings)
}

func decode(c sol.Call, v any) error {
	if err := c.Decode(v); err != nil {
		return bad("bad arguments: %v", jsonErr(err))
	}
	return nil
}

// jsonErr describes a decode error without quoting the input (it could be a password).
func jsonErr(err error) string {
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		return fmt.Sprintf("%s must be a %s", te.Field, te.Type.String())
	}
	return "not valid JSON"
}

// ---------- lock state ----------

func (a *App) actStatus(_ context.Context, _ sol.Call) (any, error) {
	kr := a.keyring()
	a.mu.Lock()
	locked := a.vk == nil
	st, settings := a.st, a.settings
	a.mu.Unlock()
	out := map[string]any{"initialized": kr.OK, "locked": locked, "items": 0, "trash": 0, "orphaned": 0,
		"auto_lock": settings.AutoLock, "clip_clear": settings.ClipClear, "kdf_iter": 0, "locks_in": a.locksIn()}
	if kr.OK {
		out["kdf_iter"] = kr.Current.KDF.Iter
		out["orphaned"] = a.orphaned(kr.Current.ID)
	}
	if !locked {
		n, t := 0, 0
		for _, it := range st.Items() {
			if it.Trashed != 0 {
				t++
			} else {
				n++
			}
		}
		out["items"], out["trash"] = n, t
	}
	return out, nil
}

// minPassword is the shortest master password accepted at setup and change.
const minPassword = 8

func checkNewPassword(p string) error {
	if len([]rune(p)) < minPassword {
		return bad("the master password must be at least %d characters", minPassword)
	}
	return nil
}

func (a *App) actSetup(ctx context.Context, c sol.Call) (any, error) {
	var in struct {
		Password string `json:"password"`
	}
	if err := decode(c, &in); err != nil {
		return nil, err
	}
	if err := checkNewPassword(in.Password); err != nil {
		return nil, err
	}
	a.setupMu.Lock()
	defer a.setupMu.Unlock()
	if a.keyring().OK {
		return nil, sol.Errorf("conflict", "a vault already exists: unlock it")
	}
	if c.DryRun {
		return dry(map[string]any{"setup": true, "kdf_iter": a.cfg.KDFIter}), nil
	}
	vk := vault.NewVaultKey()
	kr, err := a.wrap(in.Password, vk, vault.RandomHex(8), a.cfg.KDFIter)
	if err != nil {
		vault.Wipe(vk)
		return nil, err
	}
	if _, err := a.api.Append(ctx, FeedKeys, kr); err != nil {
		vault.Wipe(vk)
		return nil, err
	}
	if err := a.pull(ctx, a.addr(FeedKeys)); err != nil {
		vault.Wipe(vk)
		return nil, err
	}
	if cur := a.keyring(); cur.Current.ID != kr.ID {
		vault.Wipe(vk)
		return nil, sol.Errorf("conflict", "another device set up a vault at the same time: unlock with its password")
	}
	a.open(vk, kr.ID)
	return ok(), nil
}

// checkPassword unwraps the current keyring with password, with the backoff of
// design/DESIGN.md: after 5 failures in a row each try waits 2^(n-5) s (at most 60 s).
func (a *App) checkPassword(ctx context.Context, password string) ([]byte, vault.Keyring, error) {
	a.unlockMu.Lock()
	defer a.unlockMu.Unlock()
	if n := a.failures; n >= 5 {
		d := time.Second << min(n-5, 6)
		if err := a.sleep(ctx, min(d, 60*time.Second)); err != nil {
			return nil, vault.Keyring{}, sol.Errorf("unavailable", "cancelled")
		}
	}
	kr := a.keyring()
	if !kr.OK {
		return nil, vault.Keyring{}, sol.Errorf("not_found", "there is no vault yet: set one up with raypass.setup")
	}
	vk, err := kr.Current.Unwrap(password)
	switch {
	case errors.Is(err, vault.ErrWrongPassword):
		a.failures++
		return nil, vault.Keyring{}, sol.Errorf("unauthorized", "wrong password")
	case err != nil:
		return nil, vault.Keyring{}, sol.Errorf("conflict", "the keyring is damaged")
	}
	a.failures = 0
	return vk, kr.Current, nil
}

func (a *App) actUnlock(ctx context.Context, c sol.Call) (any, error) {
	var in struct {
		Password string `json:"password"`
	}
	if err := decode(c, &in); err != nil {
		return nil, err
	}
	// A dry run checks the password too (with the same backoff), as DESIGN.md asks dry runs to
	// validate exactly as the real call would: "would unlock" for a wrong password is a lie.
	vk, kr, err := a.checkPassword(ctx, in.Password)
	if err != nil {
		return nil, err
	}
	if c.DryRun {
		vault.Wipe(vk)
		return dry(map[string]any{"unlock": true}), nil
	}
	a.open(vk, kr.ID)
	return ok(), nil
}

func (a *App) actLock(_ context.Context, c sol.Call) (any, error) {
	if c.DryRun {
		return dry(map[string]any{"lock": true}), nil
	}
	a.lock()
	return ok(), nil
}

func (a *App) actChangePassword(ctx context.Context, c sol.Call) (any, error) {
	var in struct {
		Old  string `json:"old"`
		New  string `json:"new"`
		Iter int    `json:"iter"`
	}
	if err := decode(c, &in); err != nil {
		return nil, err
	}
	if err := checkNewPassword(in.New); err != nil {
		return nil, err
	}
	iter := a.cfg.KDFIter
	if in.Iter != 0 {
		if in.Iter < a.cfg.MinIter || in.Iter > vault.MaxIter {
			return nil, bad("iter must be %d to %d", a.cfg.MinIter, vault.MaxIter)
		}
		iter = in.Iter
	}
	vk, cur, err := a.checkPassword(ctx, in.Old)
	if err != nil {
		return nil, err
	}
	defer vault.Wipe(vk)
	if c.DryRun {
		return dry(map[string]any{"change_password": true, "kdf_iter": iter}), nil
	}
	kr, err := a.wrap(in.New, vk, cur.ID, iter)
	if err != nil {
		return nil, err
	}
	return a.appendKeyring(ctx, kr)
}

// wrap makes a keyring of vault id wrapping vk under password, stamped now. Fresh salt and
// nonce every time (vault.NewKeyring and vault.Rewrap do the same but refuse iter below
// vault.MinIter, which only tests use).
func (a *App) wrap(password string, vk []byte, id string, iter int) (vault.Keyring, error) {
	return vault.WrapKeyring(password, vk, vault.KeyringParams{ID: id, Salt: vault.RandomBytes(vault.SaltSize),
		Nonce: vault.RandomBytes(vault.NonceSize), Iter: iter, Stamp: a.clk.Now()})
}

func (a *App) appendKeyring(ctx context.Context, kr vault.Keyring) (any, error) {
	if _, err := a.api.Append(ctx, FeedKeys, kr); err != nil {
		return nil, err
	}
	if err := a.pull(ctx, a.addr(FeedKeys)); err != nil {
		return nil, err
	}
	return ok(), nil
}

// ---------- reads ----------

// Row is one line of raypass.list and raypass.match: no secrets.
type Row struct {
	ID      string   `json:"id"`
	Type    string   `json:"type"`
	Name    string   `json:"name"`
	User    string   `json:"user"`
	URLs    []string `json:"urls"`
	Folder  string   `json:"folder"`
	Fav     bool     `json:"fav"`
	Tags    []string `json:"tags"`
	HasTOTP bool     `json:"has_totp"`
	Updated int64    `json:"updated"`
	Trashed int64    `json:"trashed"`
}

func rowOf(it vault.Item) Row {
	r := Row{ID: it.ID, Type: it.Type, Name: it.Name, User: it.User, URLs: it.URLs, Folder: it.Folder, Fav: it.Fav,
		Tags: it.Tags, HasTOTP: it.TOTP != "", Updated: it.Updated, Trashed: it.Trashed}
	if it.Type == vault.TypeCard {
		r.User = it.Holder
	}
	if it.Type == vault.TypeIdentity {
		r.User = strings.TrimSpace(it.First + " " + it.Last)
	}
	if r.URLs == nil {
		r.URLs = []string{}
	}
	if r.Tags == nil {
		r.Tags = []string{}
	}
	return r
}

func sortRows(rows []Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		x, y := rows[i], rows[j]
		if x.Fav != y.Fav {
			return x.Fav
		}
		if xl, yl := strings.ToLower(x.Name), strings.ToLower(y.Name); xl != yl {
			return xl < yl
		}
		return x.ID < y.ID
	})
}

func inFolder(folder, want string) bool {
	return folder == want || strings.HasPrefix(folder, want+"/")
}

func (a *App) actList(_ context.Context, c sol.Call, st *vault.State) (any, error) {
	var in struct {
		Query  string `json:"query"`
		Type   string `json:"type"`
		Folder string `json:"folder"`
		Fav    *bool  `json:"fav"`
		Trash  bool   `json:"trash"`
		URL    string `json:"url"`
	}
	if err := decode(c, &in); err != nil {
		return nil, err
	}
	if in.Type != "" && !vault.ValidType(in.Type) {
		return nil, bad("type must be one of %s", strings.Join(vault.Types, ", "))
	}
	words := strings.Fields(strings.ToLower(in.Query))
	rows := []Row{}
	for _, it := range st.Items() {
		if (it.Trashed != 0) != in.Trash || (in.Type != "" && it.Type != in.Type) ||
			(in.Folder != "" && !inFolder(it.Folder, in.Folder)) || (in.Fav != nil && it.Fav != *in.Fav) ||
			(in.URL != "" && urlmatch.Best(it.URLs, in.URL) == 0) {
			continue
		}
		if len(words) > 0 {
			hay := strings.ToLower(strings.Join(append(append([]string{it.Name, it.User, it.Folder, it.Holder, it.Email, it.Network}, it.URLs...), it.Tags...), "\n"))
			match := true
			for _, w := range words {
				match = match && strings.Contains(hay, w)
			}
			if !match {
				continue
			}
		}
		rows = append(rows, rowOf(it))
	}
	sortRows(rows)
	return rows, nil
}

type idArg struct {
	ID string `json:"id"`
}

func (a *App) item(st *vault.State, id string) (vault.Item, error) {
	it, found := st.Get(id)
	if !found {
		return vault.Item{}, sol.Errorf("not_found", "no such item")
	}
	return it, nil
}

func (a *App) actGet(_ context.Context, c sol.Call, st *vault.State) (any, error) {
	var in idArg
	if err := decode(c, &in); err != nil {
		return nil, err
	}
	it, err := a.item(st, in.ID)
	if err != nil {
		return nil, err
	}
	return it.Output(), nil
}

func (a *App) totpOf(it vault.Item) (map[string]any, error) {
	if it.TOTP == "" {
		return nil, bad("this item has no TOTP")
	}
	p, err := totp.Parse(it.TOTP)
	if err != nil {
		return nil, bad("the TOTP secret of this item is not valid")
	}
	code, rem := p.Code(a.now())
	return map[string]any{"code": code, "period": p.Period, "remaining": rem, "digits": p.Digits}, nil
}

func (a *App) actTOTP(_ context.Context, c sol.Call, st *vault.State) (any, error) {
	var in idArg
	if err := decode(c, &in); err != nil {
		return nil, err
	}
	it, err := a.item(st, in.ID)
	if err != nil {
		return nil, err
	}
	return a.totpOf(it)
}

// matches returns the live logins matching url, best first.
func matches(st *vault.State, url string) []vault.Item {
	type hit struct {
		it    vault.Item
		score int
	}
	var hits []hit
	for _, it := range st.Items() {
		if it.Type != vault.TypeLogin || it.Trashed != 0 {
			continue
		}
		if s := urlmatch.Best(it.URLs, url); s > 0 {
			hits = append(hits, hit{it, s})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		if x, y := strings.ToLower(hits[i].it.Name), strings.ToLower(hits[j].it.Name); x != y {
			return x < y
		}
		return hits[i].it.ID < hits[j].it.ID
	})
	out := make([]vault.Item, len(hits))
	for i, h := range hits {
		out[i] = h.it
	}
	return out
}

func (a *App) actMatch(_ context.Context, c sol.Call, st *vault.State) (any, error) {
	var in struct {
		URL string `json:"url"`
	}
	if err := decode(c, &in); err != nil {
		return nil, err
	}
	if _, valid := urlmatch.Parse(in.URL); !valid {
		return nil, bad("url is not a URL")
	}
	rows := []Row{}
	for _, it := range matches(st, in.URL) {
		rows = append(rows, rowOf(it))
	}
	return rows, nil
}

func (a *App) actFill(_ context.Context, c sol.Call, st *vault.State) (any, error) {
	var in struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := decode(c, &in); err != nil {
		return nil, err
	}
	it, err := a.item(st, in.ID)
	if err != nil {
		return nil, err
	}
	if it.Type != vault.TypeLogin || it.Trashed != 0 || urlmatch.Best(it.URLs, in.URL) == 0 {
		return nil, sol.Errorf("forbidden", "url does not match")
	}
	out := map[string]any{"user": it.User, "pass": it.Pass}
	if it.TOTP != "" {
		if t, err := a.totpOf(it); err == nil {
			out["totp"] = t["code"]
		}
	}
	return out, nil
}

func (a *App) actFolders(_ context.Context, _ sol.Call, st *vault.State) (any, error) {
	counts := map[string]int{}
	for _, it := range st.Items() {
		if it.Trashed == 0 && it.Folder != "" {
			counts[it.Folder]++
		}
	}
	type folder struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	out := []folder{}
	for name, n := range counts {
		out = append(out, folder{name, n})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// Health thresholds.
const (
	weakBits = 50.0
	oldAfter = 365 * 24 * time.Hour
)

func (a *App) actHealth(_ context.Context, _ sol.Call, st *vault.State) (any, error) {
	weak, old := []string{}, []string{}
	byPass := map[string][]string{}
	var logins []vault.Item
	for _, it := range st.Items() {
		if it.Type != vault.TypeLogin || it.Trashed != 0 || it.Pass == "" {
			continue
		}
		logins = append(logins, it)
		if gen.Estimate(it.Pass) < weakBits {
			weak = append(weak, it.ID)
		}
		byPass[it.Pass] = append(byPass[it.Pass], it.ID)
		changed := it.Created
		if len(it.History) > 0 {
			changed = it.History[0].Until
		}
		if changed > 0 && a.now().Sub(time.UnixMilli(changed)) > oldAfter {
			old = append(old, it.ID)
		}
	}
	reused := [][]string{}
	issue := map[string]bool{}
	for _, ids := range byPass {
		if len(ids) > 1 {
			sort.Strings(ids)
			reused = append(reused, ids)
			for _, id := range ids {
				issue[id] = true
			}
		}
	}
	sort.Slice(reused, func(i, j int) bool { return reused[i][0] < reused[j][0] })
	for _, id := range append(append([]string{}, weak...), old...) {
		issue[id] = true
	}
	score := 100
	if len(logins) > 0 {
		score = 100 * (len(logins) - len(issue)) / len(logins)
	}
	return map[string]any{"weak": weak, "reused": reused, "old": old, "score": score}, nil
}

func (a *App) actExport(_ context.Context, c sol.Call, st *vault.State) (any, error) {
	var in struct {
		Format   string `json:"format"`
		Password string `json:"password"`
	}
	if err := decode(c, &in); err != nil {
		return nil, err
	}
	var items []vault.Item
	for _, it := range st.Items() {
		if it.Trashed == 0 {
			items = append(items, it)
		}
	}
	now := a.now()
	var data []byte
	var err error
	switch in.Format {
	case "json":
		data, err = port.ExportJSON(items, now)
	case "csv":
		data, err = port.ExportCSV(items)
	case "encrypted":
		if in.Password == "" {
			return nil, bad("an encrypted export needs a password")
		}
		data, err = port.ExportEncrypted(items, in.Password, a.cfg.KDFIter, now)
	default:
		return nil, bad("format must be json, csv or encrypted")
	}
	if err != nil {
		return nil, inputErr(err)
	}
	return map[string]any{"data": string(data), "filename": port.Filename(in.Format, now)}, nil
}

func (a *App) actGenerate(_ context.Context, c sol.Call) (any, error) {
	var in struct {
		Mode           string  `json:"mode"`
		Length         *int    `json:"length"`
		Upper          *bool   `json:"upper"`
		Lower          *bool   `json:"lower"`
		Digits         *bool   `json:"digits"`
		Symbols        *bool   `json:"symbols"`
		AvoidAmbiguous bool    `json:"avoid_ambiguous"`
		Words          *int    `json:"words"`
		Separator      *string `json:"separator"`
		Capitalize     bool    `json:"capitalize"`
		Number         bool    `json:"number"`
	}
	if err := decode(c, &in); err != nil {
		return nil, err
	}
	o := gen.Defaults()
	if in.Mode != "" {
		o.Mode = in.Mode
	}
	setInt := func(dst *int, v *int) {
		if v != nil {
			*dst = *v
		}
	}
	setBool := func(dst *bool, v *bool) {
		if v != nil {
			*dst = *v
		}
	}
	setInt(&o.Length, in.Length)
	setInt(&o.Words, in.Words)
	setBool(&o.Upper, in.Upper)
	setBool(&o.Lower, in.Lower)
	setBool(&o.Digits, in.Digits)
	setBool(&o.Symbols, in.Symbols)
	if in.Separator != nil {
		o.Separator = *in.Separator
	}
	o.AvoidAmbiguous, o.Capitalize, o.Number = in.AvoidAmbiguous, in.Capitalize, in.Number
	p, bits, err := gen.Generate(o)
	if err != nil {
		return nil, bad("bad options: mode chars needs length 4 to 128 and at least one character class; words needs 3 to 20 words")
	}
	return map[string]any{"password": p, "entropy_bits": float64(int(bits*10)) / 10}, nil
}

// ---------- writes ----------

// fieldNames lists the input's field names for a dry-run description (never the values).
func fieldNames(in map[string]json.RawMessage) []string {
	var out []string
	for k := range in {
		if k != "id" && k != "type" {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func (a *App) nowMs() int64 { return a.now().UnixMilli() }

func (a *App) actAdd(ctx context.Context, c sol.Call, st *vault.State) (any, error) {
	var in map[string]json.RawMessage
	if err := decode(c, &in); err != nil {
		return nil, err
	}
	var typ string
	if raw, has := in["type"]; has {
		_ = json.Unmarshal(raw, &typ)
	}
	id, ops, err := st.Add(typ, in, a.nowMs())
	if err != nil {
		return nil, inputErr(err)
	}
	if c.DryRun {
		return dry(map[string]any{"add": typ, "fields": fieldNames(in)}), nil
	}
	if err := a.appendOps(ctx, ops); err != nil {
		return nil, err
	}
	return map[string]any{"id": id}, nil
}

func (a *App) actUpdate(ctx context.Context, c sol.Call, st *vault.State) (any, error) {
	var in map[string]json.RawMessage
	if err := decode(c, &in); err != nil {
		return nil, err
	}
	var id string
	if raw, has := in["id"]; has {
		_ = json.Unmarshal(raw, &id)
	}
	ops, err := st.Update(id, in, a.nowMs())
	if err != nil {
		return nil, inputErr(err)
	}
	if c.DryRun {
		return dry(map[string]any{"update": id, "fields": fieldNames(in)}), nil
	}
	if err := a.appendOps(ctx, ops); err != nil {
		return nil, err
	}
	return map[string]any{"id": id}, nil
}

type idsArg struct {
	IDs      []string `json:"ids"`
	AllTrash bool     `json:"all_trash"`
}

func (a *App) actTrash(ctx context.Context, c sol.Call, st *vault.State) (any, error) {
	return a.bulk(ctx, c, st, "trashed", func(ids []string) ([]crdt.MapOp, int, error) { return st.Trash(ids, a.nowMs()) })
}

func (a *App) actRestore(ctx context.Context, c sol.Call, st *vault.State) (any, error) {
	return a.bulk(ctx, c, st, "restored", func(ids []string) ([]crdt.MapOp, int, error) { return st.Restore(ids, a.nowMs()) })
}

func (a *App) actPurge(ctx context.Context, c sol.Call, st *vault.State) (any, error) {
	return a.bulk(ctx, c, st, "purged", st.Purge)
}

func (a *App) bulk(ctx context.Context, c sol.Call, st *vault.State, key string, f func([]string) ([]crdt.MapOp, int, error)) (any, error) {
	var in idsArg
	if err := decode(c, &in); err != nil {
		return nil, err
	}
	ids := in.IDs
	if in.AllTrash {
		if key != "purged" {
			return nil, bad("all_trash is only for raypass.purge")
		}
		ids = append(ids, st.TrashedIDs()...)
	}
	if len(ids) == 0 && !in.AllTrash {
		return nil, bad("ids is empty")
	}
	ops, n, err := f(ids)
	if err != nil {
		return nil, inputErr(err)
	}
	if c.DryRun {
		return dry(map[string]any{key: n, "ids": ids}), nil
	}
	if err := a.appendOps(ctx, ops); err != nil {
		return nil, err
	}
	return map[string]any{key: n}, nil
}

func (a *App) actSaveLogin(ctx context.Context, c sol.Call, st *vault.State) (any, error) {
	var in struct {
		URL  string `json:"url"`
		User string `json:"user"`
		Pass string `json:"pass"`
		Name string `json:"name"`
	}
	if err := decode(c, &in); err != nil {
		return nil, err
	}
	u, valid := urlmatch.Parse(in.URL)
	if !valid {
		return nil, bad("url is not a URL")
	}
	if in.Pass == "" {
		return nil, bad("pass is empty")
	}
	for _, it := range matches(st, in.URL) {
		if it.User != in.User {
			continue
		}
		if it.Pass == in.Pass {
			return map[string]any{"id": it.ID, "created": false}, nil
		}
		ops, err := st.Update(it.ID, map[string]json.RawMessage{"pass": mustJSON(in.Pass)}, a.nowMs())
		if err != nil {
			return nil, inputErr(err)
		}
		if c.DryRun {
			return dry(map[string]any{"update": it.ID, "fields": []string{"pass"}}), nil
		}
		if err := a.appendOps(ctx, ops); err != nil {
			return nil, err
		}
		return map[string]any{"id": it.ID, "created": false}, nil
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = strings.TrimPrefix(urlmatch.Host(in.URL), "www.")
	}
	// Only the origin is stored: paths and queries can carry tokens.
	fields := map[string]json.RawMessage{"name": mustJSON(name), "user": mustJSON(in.User), "pass": mustJSON(in.Pass), "urls": mustJSON([]string{u.String()})}
	id, ops, err := st.Add(vault.TypeLogin, fields, a.nowMs())
	if err != nil {
		return nil, inputErr(err)
	}
	if c.DryRun {
		return dry(map[string]any{"add": vault.TypeLogin, "name": name, "url": u.String()}), nil
	}
	if err := a.appendOps(ctx, ops); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "created": true}, nil
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic("raypass: unmarshalable value")
	}
	return b
}

// dupKey identifies an item for import de-duplication: an import never adds an exact copy of
// an item that is already in the vault (or earlier in the same file).
func dupKey(it vault.Item) string {
	b, _ := json.Marshal([]any{it.Type, it.Name, it.User, it.Pass, it.URLs, it.Notes, it.Number, it.Seed, it.Privkey})
	return string(b)
}

func (a *App) actImport(ctx context.Context, c sol.Call, st *vault.State) (any, error) {
	var in struct {
		Format   string `json:"format"`
		Data     string `json:"data"`
		Password string `json:"password"`
	}
	if err := decode(c, &in); err != nil {
		return nil, err
	}
	if in.Format == "" {
		in.Format = port.Auto
	}
	res, err := port.Import(in.Format, []byte(in.Data), in.Password, a.cfg.MinIter)
	if err != nil {
		return nil, inputErr(err)
	}
	seen := map[string]bool{}
	for _, it := range st.Items() {
		if it.Trashed == 0 {
			seen[dupKey(it)] = true
		}
	}
	var ops []crdt.MapOp
	imported, skipped := 0, res.Skipped
	now := a.nowMs()
	for _, it := range res.Items {
		k := dupKey(it)
		if seen[k] {
			skipped++
			continue
		}
		if it.ID != "" {
			if _, exists := st.Get(it.ID); exists {
				skipped++
				continue
			}
		} else {
			it.ID = vault.NewID()
		}
		_, iops, err := st.AddWithID(it.ID, it.Type, it.Input(), now)
		if err != nil {
			skipped++
			continue
		}
		seen[k] = true
		ops = append(ops, iops...)
		imported++
	}
	out := map[string]any{"imported": imported, "skipped": skipped, "format": res.Format}
	if c.DryRun {
		return dry(out), nil
	}
	if err := a.appendOps(ctx, ops); err != nil {
		return nil, err
	}
	return out, nil
}

func (a *App) actSettings(ctx context.Context, c sol.Call, st *vault.State) (any, error) {
	var in struct {
		AutoLock  *int `json:"auto_lock"`
		ClipClear *int `json:"clip_clear"`
	}
	if err := decode(c, &in); err != nil {
		return nil, err
	}
	ops, err := st.SetSettings(in.AutoLock, in.ClipClear)
	if err != nil {
		return nil, inputErr(err)
	}
	if c.DryRun {
		s := st.Settings()
		if in.AutoLock != nil {
			s.AutoLock = *in.AutoLock
		}
		if in.ClipClear != nil {
			s.ClipClear = *in.ClipClear
		}
		return dry(s), nil
	}
	if err := a.appendOps(ctx, ops); err != nil {
		return nil, err
	}
	a.touch() // re-arm auto-lock with the new value
	return st.Settings(), nil
}
