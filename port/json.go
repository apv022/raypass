package port

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/apv022/raypass/vault"
)

// jsonItem is one item of a Raypass JSON export: exactly vault.Item.Output's keys.
type jsonItem struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Name       string          `json:"name"`
	Folder     string          `json:"folder"`
	Fav        bool            `json:"fav"`
	Tags       []string        `json:"tags"`
	Notes      string          `json:"notes"`
	Fields     []vault.Field   `json:"fields"`
	User       string          `json:"user"`
	Pass       string          `json:"pass"`
	URLs       []string        `json:"urls"`
	TOTP       string          `json:"totp"`
	History    []vault.History `json:"history"`
	Holder     string          `json:"holder"`
	Number     string          `json:"number"`
	Exp        string          `json:"exp"`
	CVV        string          `json:"cvv"`
	Brand      string          `json:"brand"`
	First      string          `json:"first"`
	Last       string          `json:"last"`
	Email      string          `json:"email"`
	Phone      string          `json:"phone"`
	Address    string          `json:"address"`
	Company    string          `json:"company"`
	Seed       string          `json:"seed"`
	Privkey    string          `json:"privkey"`
	Network    string          `json:"network"`
	Passphrase string          `json:"passphrase"`
	Derivation string          `json:"derivation"`
}

// ExportDoc is the Raypass JSON export document.
type ExportDoc struct {
	Raypass  int              `json:"raypass"`
	Exported int64            `json:"exported"`
	Items    []map[string]any `json:"items"`
}

func raypassJSON(data []byte) (Result, error) {
	var doc struct {
		Raypass int               `json:"raypass"`
		Items   []json.RawMessage `json:"items"`
	}
	if json.Unmarshal(data, &doc) != nil || doc.Raypass != 1 {
		return Result{}, Error("not a Raypass JSON export")
	}
	var r Result
	for _, raw := range doc.Items {
		var j jsonItem
		if json.Unmarshal(raw, &j) != nil || !vault.ValidType(j.Type) || strings.TrimSpace(j.Name) == "" {
			r.Skipped++
			continue
		}
		it := vault.Item{ID: j.ID, Type: j.Type, Name: j.Name, Folder: j.Folder, Fav: j.Fav, Tags: j.Tags, Notes: j.Notes,
			Fields: j.Fields, User: j.User, Pass: j.Pass, URLs: j.URLs, TOTP: j.TOTP, History: j.History,
			Holder: j.Holder, Number: j.Number, Exp: j.Exp, CVV: j.CVV, Brand: j.Brand,
			First: j.First, Last: j.Last, Email: j.Email, Phone: j.Phone, Address: j.Address, Company: j.Company,
			Seed: j.Seed, Privkey: j.Privkey, Network: j.Network, Passphrase: j.Passphrase, Derivation: j.Derivation}
		if !vault.ValidID(it.ID) {
			it.ID = ""
		}
		r.Items = append(r.Items, it)
	}
	return r, nil
}

// bitwardenJSON reads Bitwarden's unencrypted JSON export.
func bitwardenJSON(data []byte) (Result, error) {
	var doc struct {
		Encrypted bool `json:"encrypted"`
		Folders   []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"folders"`
		Items []struct {
			Type     int    `json:"type"`
			Name     string `json:"name"`
			Notes    string `json:"notes"`
			Favorite bool   `json:"favorite"`
			FolderID string `json:"folderId"`
			Fields   []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
				Type  int    `json:"type"`
			} `json:"fields"`
			Login *struct {
				URIs []struct {
					URI string `json:"uri"`
				} `json:"uris"`
				Username string `json:"username"`
				Password string `json:"password"`
				TOTP     string `json:"totp"`
			} `json:"login"`
			Card *struct {
				CardholderName string `json:"cardholderName"`
				Brand          string `json:"brand"`
				Number         string `json:"number"`
				ExpMonth       string `json:"expMonth"`
				ExpYear        string `json:"expYear"`
				Code           string `json:"code"`
			} `json:"card"`
			Identity *struct {
				FirstName  string `json:"firstName"`
				MiddleName string `json:"middleName"`
				LastName   string `json:"lastName"`
				Address1   string `json:"address1"`
				Address2   string `json:"address2"`
				Address3   string `json:"address3"`
				City       string `json:"city"`
				State      string `json:"state"`
				PostalCode string `json:"postalCode"`
				Country    string `json:"country"`
				Company    string `json:"company"`
				Email      string `json:"email"`
				Phone      string `json:"phone"`
			} `json:"identity"`
			PasswordHistory []struct {
				LastUsedDate string `json:"lastUsedDate"`
				Password     string `json:"password"`
			} `json:"passwordHistory"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return Result{}, Error("not a Bitwarden JSON export")
	}
	if doc.Encrypted {
		return Result{}, Error("this Bitwarden export is encrypted: export it unencrypted (or as CSV) and import that")
	}
	folders := map[string]string{}
	for _, f := range doc.Folders {
		folders[f.ID] = f.Name
	}
	var r Result
	for _, b := range doc.Items {
		it := vault.Item{Name: b.Name, Notes: b.Notes, Fav: b.Favorite, Folder: folders[b.FolderID]}
		for _, f := range b.Fields {
			if f.Type == 3 { // linked field: points at another field, has no value of its own
				continue
			}
			it.Fields = append(it.Fields, vault.Field{Name: f.Name, Value: f.Value, Hidden: f.Type == 1})
		}
		switch b.Type {
		case 1:
			it.Type = vault.TypeLogin
			if l := b.Login; l != nil {
				it.User, it.Pass, it.TOTP = l.Username, l.Password, l.TOTP
				for _, u := range l.URIs {
					if strings.TrimSpace(u.URI) != "" {
						it.URLs = append(it.URLs, u.URI)
					}
				}
			}
			for _, h := range b.PasswordHistory {
				var until int64
				if t, err := time.Parse(time.RFC3339, h.LastUsedDate); err == nil {
					until = t.UnixMilli()
				}
				it.History = append(it.History, vault.History{Pass: h.Password, Until: until})
			}
		case 2:
			it.Type = vault.TypeNote
		case 3:
			it.Type = vault.TypeCard
			if c := b.Card; c != nil {
				it.Holder, it.Brand, it.Number, it.CVV = c.CardholderName, c.Brand, c.Number, c.Code
				it.Exp = expiry(c.ExpMonth, c.ExpYear)
			}
		case 4:
			it.Type = vault.TypeIdentity
			if id := b.Identity; id != nil {
				it.First = strings.TrimSpace(id.FirstName + " " + id.MiddleName)
				it.Last, it.Email, it.Phone, it.Company = id.LastName, id.Email, id.Phone, id.Company
				it.Address = joinNonEmpty(", ", id.Address1, id.Address2, id.Address3, id.City, id.State, id.PostalCode, id.Country)
			}
		default:
			r.Skipped++ // SSH keys and future types
			continue
		}
		if strings.TrimSpace(it.Name) == "" {
			it.Name = "Imported"
		}
		r.Items = append(r.Items, it)
	}
	return r, nil
}

func expiry(month, year string) string {
	month, year = strings.TrimSpace(month), strings.TrimSpace(year)
	if month == "" && year == "" {
		return ""
	}
	if len(month) == 1 {
		month = "0" + month
	}
	if len(year) == 4 {
		year = year[2:]
	}
	return month + "/" + year
}

func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

// ExportJSON writes the Raypass JSON export of items.
func ExportJSON(items []vault.Item, now time.Time) ([]byte, error) {
	doc := ExportDoc{Raypass: 1, Exported: now.UnixMilli(), Items: []map[string]any{}}
	for _, it := range items {
		doc.Items = append(doc.Items, it.Output())
	}
	return json.MarshalIndent(doc, "", "  ")
}

// ExportEncrypted writes the encrypted Raypass export of items.
func ExportEncrypted(items []vault.Item, password string, iter int, now time.Time) ([]byte, error) {
	plain, err := ExportJSON(items, now)
	if err != nil {
		return nil, err
	}
	defer vault.Wipe(plain)
	// Fresh salt and nonce; the caller picks iter (vault.SealExport would refuse the low counts
	// tests use).
	f, err := vault.SealExportWith(password, plain, iter, vault.RandomBytes(vault.SaltSize), vault.RandomBytes(vault.NonceSize))
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(f, "", "  ")
}

// csvHeader is Bitwarden's CSV layout, which most managers import.
var csvHeader = []string{"folder", "favorite", "type", "name", "notes", "fields", "reprompt", "login_uri", "login_username", "login_password", "login_totp"}

// ExportCSV writes items as Bitwarden-layout CSV. That layout has only logins and notes, so
// cards, identities and wallets are written as notes with their fields in the "fields" column
// ("name: value" lines): nothing is lost, and a Raypass re-import keeps them as notes. Extra
// login URLs go in fields as "url: …".
func ExportCSV(items []vault.Item) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(csvHeader); err != nil {
		return nil, err
	}
	for _, it := range items {
		var fields []string
		for _, f := range it.Fields {
			fields = append(fields, f.Name+": "+f.Value)
		}
		typ, uri := "note", ""
		if it.Type == vault.TypeLogin {
			typ = "login"
			if len(it.URLs) > 0 {
				uri = it.URLs[0]
				for _, u := range it.URLs[1:] {
					fields = append(fields, "url: "+u)
				}
			}
		} else {
			out := it.Output()
			for _, f := range vault.TypeFields(it.Type) {
				if s, ok := out[f].(string); ok && s != "" {
					fields = append(fields, f+": "+s)
				}
			}
		}
		fav := ""
		if it.Fav {
			fav = "1"
		}
		rec := []string{it.Folder, fav, typ, it.Name, it.Notes, strings.Join(fields, "\n"), "0", uri}
		if it.Type == vault.TypeLogin {
			rec = append(rec, it.User, it.Pass, it.TOTP)
		} else {
			rec = append(rec, "", "", "")
		}
		if err := w.Write(rec); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// Filename is the suggested file name of an export.
func Filename(format string, now time.Time) string {
	d := now.Format("2006-01-02")
	switch format {
	case exportFormatCSV:
		return fmt.Sprintf("raypass-export-%s.csv", d)
	case exportFormatCrypt:
		return fmt.Sprintf("raypass-export-%s.encrypted.json", d)
	}
	return fmt.Sprintf("raypass-export-%s.json", d)
}
