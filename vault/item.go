package vault

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// Item types.
const (
	TypeLogin    = "login"
	TypeNote     = "note"
	TypeCard     = "card"
	TypeIdentity = "identity"
	TypeWallet   = "wallet"
)

// Types lists the item types in display order.
var Types = []string{TypeLogin, TypeNote, TypeCard, TypeIdentity, TypeWallet}

// Field is a custom field of an item.
type Field struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Hidden bool   `json:"hidden"`
}

// History is one previous password of a login: the password and when it stopped being current.
type History struct {
	Pass  string `json:"pass"`
	Until int64  `json:"until"`
}

// MaxHistory is how many previous passwords a login keeps.
const MaxHistory = 20

// MaxValue is the largest JSON encoding of one field value. Far below what an entry carries so
// a whole item update always fits a few entries.
const MaxValue = 32 << 10

// Item is the typed view of one item's keys in the map.
type Item struct {
	ID      string
	Type    string
	Name    string
	Folder  string
	Fav     bool
	Tags    []string
	Notes   string
	Fields  []Field
	Created int64
	Updated int64
	Trashed int64

	// login
	User    string
	Pass    string
	URLs    []string
	TOTP    string
	History []History
	// card
	Holder, Number, Exp, CVV, Brand string
	// identity (Address is shared with wallet: an item has one type)
	First, Last, Email, Phone, Address, Company string
	// wallet
	Seed, Privkey, Network, Passphrase, Derivation string
}

// kinds of field values.
const (
	kString  = "string"
	kBool    = "bool"
	kStrings = "strings"
	kFields  = "fields"
	kHistory = "history"
	kInt     = "int"
)

// commonFields are settable on every type.
var commonFields = map[string]string{"name": kString, "folder": kString, "fav": kBool, "tags": kStrings, "notes": kString, "fields": kFields}

// typeFields are the settable fields of each type, in display order.
var typeFields = map[string][]string{
	TypeLogin:    {"user", "pass", "urls", "totp", "history"},
	TypeNote:     {},
	TypeCard:     {"holder", "number", "exp", "cvv", "brand"},
	TypeIdentity: {"first", "last", "email", "phone", "address", "company"},
	TypeWallet:   {"seed", "privkey", "address", "network", "passphrase", "derivation"},
}

var fieldKind = map[string]string{"urls": kStrings, "history": kHistory}

// systemFields are written by Raypass itself.
var systemFields = map[string]string{"type": kString, "created": kInt, "updated": kInt, "trashed": kInt}

// ValidType reports whether t is a known item type.
func ValidType(t string) bool { _, ok := typeFields[t]; return ok }

// TypeFields returns the type-specific field names of t, in display order.
func TypeFields(t string) []string { return append([]string(nil), typeFields[t]...) }

// kindOf is the value kind of a settable field of type t, "" when the field is not settable.
func kindOf(t, field string) string {
	if k, ok := commonFields[field]; ok {
		return k
	}
	for _, f := range typeFields[t] {
		if f == field {
			if k, ok := fieldKind[f]; ok {
				return k
			}
			return kString
		}
	}
	return ""
}

// BadInput is a validation error about action input; its text is safe to show (it never holds
// a value, only field names).
type BadInput string

func (e BadInput) Error() string { return string(e) }

func badf(format string, a ...any) error { return BadInput(fmt.Sprintf(format, a...)) }

// normalize checks a raw input value against its kind and returns the JSON to store.
func normalize(field, kind string, raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) > MaxValue {
		return nil, badf("%s is too long", field)
	}
	switch kind {
	case kString:
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return nil, badf("%s must be a string", field)
		}
		if !utf8.ValidString(s) {
			return nil, badf("%s is not valid UTF-8", field)
		}
		if field == "name" || field == "folder" {
			s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", ""))
		}
		if field == "folder" {
			s = cleanFolder(s)
		}
		return json.Marshal(s)
	case kBool:
		var b bool
		if json.Unmarshal(raw, &b) != nil {
			return nil, badf("%s must be true or false", field)
		}
		return json.Marshal(b)
	case kStrings:
		var ss []string
		if json.Unmarshal(raw, &ss) != nil {
			return nil, badf("%s must be a list of strings", field)
		}
		out := []string{}
		seen := map[string]bool{}
		for _, s := range ss {
			s = strings.TrimSpace(s)
			if s == "" || seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
		return json.Marshal(out)
	case kFields:
		var fs []Field
		if json.Unmarshal(raw, &fs) != nil {
			return nil, badf("%s must be a list of {name, value, hidden}", field)
		}
		if fs == nil {
			fs = []Field{}
		}
		return json.Marshal(fs)
	case kHistory:
		var hs []History
		if json.Unmarshal(raw, &hs) != nil {
			return nil, badf("%s must be a list of {pass, until}", field)
		}
		if hs == nil {
			hs = []History{}
		}
		sort.SliceStable(hs, func(i, j int) bool { return hs[i].Until > hs[j].Until })
		if len(hs) > MaxHistory {
			hs = hs[:MaxHistory]
		}
		return json.Marshal(hs)
	}
	return nil, badf("%s cannot be set", field)
}

// cleanFolder trims each part of a "A/B" folder path and drops empty parts.
func cleanFolder(s string) string {
	var parts []string
	for _, p := range strings.Split(s, "/") {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "/")
}

// isNull reports whether raw is JSON null (a field to clear).
func isNull(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

// Output is the item as the "get" action returns it: every common field and every field of its
// type, with "" / [] / false for the ones not set, so clients see a fixed shape per type.
func (it Item) Output() map[string]any {
	o := map[string]any{"id": it.ID, "type": it.Type, "name": it.Name, "folder": it.Folder, "fav": it.Fav,
		"tags": nonNil(it.Tags), "notes": it.Notes, "fields": nonNilF(it.Fields),
		"created": it.Created, "updated": it.Updated, "trashed": it.Trashed}
	for _, f := range typeFields[it.Type] {
		o[f] = it.value(f)
	}
	return o
}

// value returns a type field by name.
func (it Item) value(f string) any {
	switch f {
	case "user":
		return it.User
	case "pass":
		return it.Pass
	case "urls":
		return nonNil(it.URLs)
	case "totp":
		return it.TOTP
	case "history":
		if it.History == nil {
			return []History{}
		}
		return it.History
	case "holder":
		return it.Holder
	case "number":
		return it.Number
	case "exp":
		return it.Exp
	case "cvv":
		return it.CVV
	case "brand":
		return it.Brand
	case "first":
		return it.First
	case "last":
		return it.Last
	case "email":
		return it.Email
	case "phone":
		return it.Phone
	case "address":
		return it.Address
	case "company":
		return it.Company
	case "seed":
		return it.Seed
	case "privkey":
		return it.Privkey
	case "network":
		return it.Network
	case "passphrase":
		return it.Passphrase
	case "derivation":
		return it.Derivation
	}
	return nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilF(s []Field) []Field {
	if s == nil {
		return []Field{}
	}
	return s
}

// Input builds an action-style field map from the item (for re-adding imported items): every
// non-empty settable field.
func (it Item) Input() map[string]json.RawMessage {
	in := map[string]json.RawMessage{}
	put := func(k string, v any) {
		b, _ := json.Marshal(v)
		in[k] = b
	}
	put("name", it.Name)
	if it.Folder != "" {
		put("folder", it.Folder)
	}
	if it.Fav {
		put("fav", true)
	}
	if len(it.Tags) > 0 {
		put("tags", it.Tags)
	}
	if it.Notes != "" {
		put("notes", it.Notes)
	}
	if len(it.Fields) > 0 {
		put("fields", it.Fields)
	}
	for _, f := range typeFields[it.Type] {
		v := it.value(f)
		switch x := v.(type) {
		case string:
			if x != "" {
				put(f, x)
			}
		case []string:
			if len(x) > 0 {
				put(f, x)
			}
		case []History:
			if len(x) > 0 {
				put(f, x)
			}
		}
	}
	return in
}
