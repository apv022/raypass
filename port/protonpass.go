package port

// Proton Pass import (design/DESIGN.md "Import").
//
// Format source: Proton's own exporter and importer in github.com/ProtonMail/WebClients
// (packages/pass/lib/export/{archive,csv,types}.ts, packages/pass/lib/import/providers/protonpass/
// and its mocks, packages/pass/types/protobuf/item-v1.static.ts for the enums), cross-checked
// with Bitwarden's open-source importer (github.com/bitwarden/clients,
// libs/importer/src/importers/protonpass/). Read in November 2026.
//
//   - The JSON export is a zip holding "Proton Pass/data.json" (plus "Proton Pass/files/…"
//     attachments). An encrypted export holds "Proton Pass/data.pgp" instead, an armored OpenPGP
//     message; older exports also carry "encrypted": true in data.json.
//   - data.json: {"version", "userId", "encrypted", "vaults": {shareId: {"name", "folders"?,
//     "items": [...]}}}; an item has "data" {"metadata" {name, note}, "extraFields", "type",
//     "content"}, "state" (1 active, 2 trashed), "aliasEmail", "pinned", "folderId"?,
//     "createTime"/"modifyTime" (Unix seconds), "files"?.
//   - The CSV export's header is type,name,url,autofillUrls,email,username,password,note,totp,
//     createTime,modifyTime,vault (older exports lack autofillUrls, email or vault); a card's or
//     an identity's "note" cell is the JSON of its content plus "note".

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/apv022/raypass/vault"
)

// Zip limits. Only data.json is ever decompressed (attachments are not imported), so the bytes
// decompressed are capped by maxProtonJSON whatever sizes the zip's headers claim.
const (
	maxZipEntries = 100000
	maxProtonJSON = 64 << 20
)

const (
	protonDataJSON = "Proton Pass/data.json"
	protonDataPGP  = "Proton Pass/data.pgp"
	pgpArmor       = "-----BEGIN PGP MESSAGE-----"
	zipMagic       = "PK\x03\x04"
)

// errProtonEncrypted is returned for a passphrase-protected Proton Pass export: Raypass has no
// OpenPGP implementation (stdlib only), so it says so instead of guessing.
const errProtonEncrypted = Error("Proton Pass encrypted exports aren't supported: export again with encryption turned off")

// isProtonZipOrPGP reports whether data is a zip or an armored PGP message (both can only be a
// Proton Pass export among the formats Raypass reads).
func isProtonZipOrPGP(data []byte) bool {
	return bytes.HasPrefix(data, []byte(zipMagic)) || bytes.HasPrefix(bytes.TrimSpace(data), []byte(pgpArmor))
}

// protonDataFromZip returns data.json from a Proton Pass zip export.
func protonDataFromZip(data []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, Error("the zip file could not be read")
	}
	if len(zr.File) > maxZipEntries {
		return nil, Error("the zip file has too many entries")
	}
	var entry *zip.File
	for _, f := range zr.File {
		switch f.Name {
		case protonDataPGP:
			return nil, errProtonEncrypted
		case protonDataJSON:
			entry = f
		}
	}
	if entry == nil {
		return nil, Error("this zip is not a Proton Pass export (no Proton Pass/data.json)")
	}
	if entry.UncompressedSize64 > maxProtonJSON {
		return nil, Error("the Proton Pass export is too large")
	}
	rc, err := entry.Open()
	if err != nil {
		return nil, Error("the zip file could not be read")
	}
	defer rc.Close()
	// The header's size may lie: read at most one byte past the cap and refuse if it is reached.
	out, err := io.ReadAll(io.LimitReader(rc, maxProtonJSON+1))
	if err != nil {
		return nil, Error("the zip file could not be read")
	}
	if len(out) > maxProtonJSON {
		return nil, Error("the Proton Pass export is too large")
	}
	return out, nil
}

// protonField is one Proton Pass extra field. data is {"content"} for text and hidden,
// {"totpUri"} for totp, {"timestamp"} for timestamp (a string, or a protobuf Timestamp object).
type protonField struct {
	FieldName string `json:"fieldName"`
	Type      string `json:"type"`
	Data      struct {
		Content   string          `json:"content"`
		TotpURI   string          `json:"totpUri"`
		Timestamp json.RawMessage `json:"timestamp"`
	} `json:"data"`
}

type protonSection struct {
	SectionName   string        `json:"sectionName"`
	SectionFields []protonField `json:"sectionFields"`
}

type protonItem struct {
	Data struct {
		Metadata struct {
			Name string `json:"name"`
			Note string `json:"note"`
		} `json:"metadata"`
		ExtraFields []protonField   `json:"extraFields"`
		Type        string          `json:"type"`
		Content     json.RawMessage `json:"content"`
	} `json:"data"`
	State      int             `json:"state"`
	AliasEmail string          `json:"aliasEmail"`
	Pinned     bool            `json:"pinned"`
	FolderID   string          `json:"folderId"`
	Files      json.RawMessage `json:"files"`
}

type protonDoc struct {
	Encrypted bool `json:"encrypted"`
	Vaults    map[string]struct {
		Name    string `json:"name"`
		Folders []struct {
			FolderID       string `json:"folderId"`
			ParentFolderID string `json:"parentFolderId"`
			Name           string `json:"name"`
		} `json:"folders"`
		Items []protonItem `json:"items"`
	} `json:"vaults"`
}

// protonStateTrashed is Proton Pass's ItemState.Trashed.
const protonStateTrashed = 2

// protonCounts tallies what could not be imported, for Result.Notes.
type protonCounts struct{ trashed, passkeys, files int }

func (c protonCounts) notes() []string {
	var out []string
	add := func(n int, one, many string) {
		if n == 1 {
			out = append(out, one)
		} else if n > 1 {
			out = append(out, fmt.Sprintf(many, n))
		}
	}
	add(c.passkeys, "1 passkey was not imported (passkeys can't be moved between managers)", "%d passkeys were not imported (passkeys can't be moved between managers)")
	add(c.trashed, "1 item in Proton Pass's trash was not imported", "%d items in Proton Pass's trash were not imported")
	add(c.files, "1 file attachment was not imported", "%d file attachments were not imported")
	return out
}

// protonPassJSON reads Proton Pass's JSON export: data.json, or the whole zip.
func protonPassJSON(data []byte) (Result, error) {
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte(pgpArmor)) {
		return Result{}, errProtonEncrypted
	}
	if bytes.HasPrefix(data, []byte(zipMagic)) {
		var err error
		if data, err = protonDataFromZip(data); err != nil {
			return Result{}, err
		}
		data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	}
	var doc protonDoc
	if err := json.Unmarshal(data, &doc); err != nil || doc.Vaults == nil {
		return Result{}, Error("not a Proton Pass JSON export")
	}
	if doc.Encrypted {
		return Result{}, errProtonEncrypted
	}
	var r Result
	var c protonCounts
	for _, v := range sortedVaults(doc) {
		// Folder paths inside the vault ("Vault/Parent/Child"), parents resolved with a depth cap
		// so a cyclic parent chain in a hostile file ends.
		names, parents := map[string]string{}, map[string]string{}
		for _, f := range v.Folders {
			names[f.FolderID], parents[f.FolderID] = f.Name, f.ParentFolderID
		}
		path := func(id string) string {
			var parts []string
			for depth := 0; id != "" && depth < 32; depth++ {
				n, ok := names[id]
				if !ok {
					break
				}
				parts = append([]string{n}, parts...)
				id = parents[id]
			}
			return strings.Join(append([]string{v.Name}, parts...), "/")
		}
		for _, p := range v.Items {
			if p.State == protonStateTrashed {
				c.trashed++
				continue
			}
			var files []json.RawMessage
			if json.Unmarshal(p.Files, &files) == nil {
				c.files += len(files)
			}
			it, ok := protonItemToVault(p, &c)
			if !ok {
				r.Skipped++
				continue
			}
			it.Folder = path(p.FolderID)
			r.Items = append(r.Items, it)
		}
	}
	r.Notes = c.notes()
	return r, nil
}

// protonVault is one vault with the folder separator removed from its names (a "/" in a
// Proton vault or folder name would otherwise create a Raypass sub-folder).
type protonVault struct {
	Name    string
	Folders []struct{ FolderID, ParentFolderID, Name string }
	Items   []protonItem
}

// sortedVaults returns the vaults in a stable order (JSON objects have none; Go maps neither).
func sortedVaults(doc protonDoc) []protonVault {
	ids := make([]string, 0, len(doc.Vaults))
	for id := range doc.Vaults {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []protonVault
	for _, id := range ids {
		src := doc.Vaults[id]
		x := protonVault{Name: strings.ReplaceAll(src.Name, "/", "-"), Items: src.Items}
		for _, f := range src.Folders {
			x.Folders = append(x.Folders, struct{ FolderID, ParentFolderID, Name string }{f.FolderID, f.ParentFolderID, strings.ReplaceAll(f.Name, "/", "-")})
		}
		out = append(out, x)
	}
	return out
}

// Proton Pass AutofillUrl.Mode values whose URL is a plain address Raypass can match by site.
// Never (2), Pattern (4) and RegularExpression (5) are kept as a text field instead, so
// Raypass doesn't fill where Proton Pass wouldn't.
var protonMatchModes = map[int]bool{0: true, 1: true, 3: true, 6: true}

type protonLogin struct {
	ItemEmail    string   `json:"itemEmail"`
	ItemUsername string   `json:"itemUsername"`
	Username     string   `json:"username"` // exports before 1.18 had only this (an email)
	Password     string   `json:"password"`
	URLs         []string `json:"urls"`
	AutofillURLs []struct {
		URL  string `json:"url"`
		Mode int    `json:"mode"`
	} `json:"autofillUrls"`
	TotpURI  string            `json:"totpUri"`
	Passkeys []json.RawMessage `json:"passkeys"`
}

type protonCard struct {
	CardholderName     string `json:"cardholderName"`
	CardType           int    `json:"cardType"`
	Number             string `json:"number"`
	VerificationNumber string `json:"verificationNumber"`
	ExpirationDate     string `json:"expirationDate"`
	PIN                string `json:"pin"`
	Note               string `json:"note"` // CSV only
}

// protonIdentityLabels names the identity fields that have no Raypass identity field, in
// Proton Pass's display order; the ones marked hidden are ID numbers.
var protonIdentityLabels = []struct {
	key, label string
	hidden     bool
}{
	{"birthdate", "Birthdate", false}, {"gender", "Gender", false},
	{"organization", "Organization", false},
	{"socialSecurityNumber", "Social security number", true}, {"passportNumber", "Passport number", true},
	{"licenseNumber", "License number", true},
	{"website", "Website", false}, {"xHandle", "X handle", false}, {"secondPhoneNumber", "Second phone number", false},
	{"linkedin", "LinkedIn", false}, {"reddit", "Reddit", false}, {"facebook", "Facebook", false},
	{"yahoo", "Yahoo", false}, {"instagram", "Instagram", false},
	{"jobTitle", "Job title", false}, {"personalWebsite", "Personal website", false},
	{"workPhoneNumber", "Work phone number", false}, {"workEmail", "Work email", false},
}

// protonExtraFields converts extra fields. A totp field becomes the item's TOTP when the item
// is a login without one, else a hidden field.
func protonExtraFields(it *vault.Item, fs []protonField) {
	for _, f := range fs {
		switch f.Type {
		case "totp":
			if it.Type == vault.TypeLogin && it.TOTP == "" && strings.TrimSpace(f.Data.TotpURI) != "" {
				it.TOTP = f.Data.TotpURI
				continue
			}
			it.Fields = append(it.Fields, vault.Field{Name: f.FieldName, Value: f.Data.TotpURI, Hidden: true})
		case "hidden":
			it.Fields = append(it.Fields, vault.Field{Name: f.FieldName, Value: f.Data.Content, Hidden: true})
		case "timestamp":
			it.Fields = append(it.Fields, vault.Field{Name: f.FieldName, Value: protonTimestamp(f.Data.Timestamp)})
		default: // "text" and any future kind with content
			it.Fields = append(it.Fields, vault.Field{Name: f.FieldName, Value: f.Data.Content})
		}
	}
}

// protonTimestamp renders a timestamp field: a string as is, a protobuf Timestamp by its seconds.
func protonTimestamp(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var ts struct {
		Seconds json.Number `json:"seconds"`
	}
	if json.Unmarshal(raw, &ts) == nil {
		return ts.Seconds.String()
	}
	return ""
}

func protonSections(it *vault.Item, ss []protonSection) {
	for _, s := range ss {
		protonExtraFields(it, s.SectionFields)
	}
}

// protonLoginUser applies the username rule: username, else email; both and different keeps
// the email as a field "Email".
func protonLoginUser(it *vault.Item, username, email string) {
	username, email = strings.TrimSpace(username), strings.TrimSpace(email)
	switch {
	case username != "":
		it.User = username
		if email != "" && email != username {
			it.Fields = append(it.Fields, vault.Field{Name: "Email", Value: email})
		}
	default:
		it.User = email
	}
}

// protonAliasNote is appended to the notes of a Proton alias, which Raypass keeps as a login.
const protonAliasNote = "Proton Pass alias: an email forwarding address managed by Proton. Raypass keeps it as a login; manage the alias itself in Proton Pass."

func appendNote(notes, add string) string {
	if strings.TrimSpace(notes) == "" {
		return add
	}
	return notes + "\n\n" + add
}

// protonCardInto fills a card. Proton's expiry is "YYYY-MM"; Raypass's is "MM/YY".
func protonCardInto(it *vault.Item, cc protonCard) {
	it.Type = vault.TypeCard
	it.Holder, it.Number, it.CVV = cc.CardholderName, cc.Number, cc.VerificationNumber
	it.Brand = map[int]string{2: "Visa", 3: "Mastercard", 4: "American Express"}[cc.CardType]
	if y, m, ok := strings.Cut(strings.TrimSpace(cc.ExpirationDate), "-"); ok && len(y) == 4 && len(m) >= 1 {
		it.Exp = expiry(m, y)
	}
	if strings.TrimSpace(cc.PIN) != "" {
		it.Fields = append(it.Fields, vault.Field{Name: "PIN", Value: cc.PIN, Hidden: true})
	}
}

// protonIdentityInto fills an identity from Proton's identity content (a JSON object).
func protonIdentityInto(it *vault.Item, raw json.RawMessage) {
	it.Type = vault.TypeIdentity
	var m map[string]json.RawMessage
	_ = json.Unmarshal(raw, &m)
	str := func(k string) string {
		var s string
		_ = json.Unmarshal(m[k], &s)
		return strings.TrimSpace(s)
	}
	first, middle, last := str("firstName"), str("middleName"), str("lastName")
	if first == "" && last == "" {
		// Only the full name: the last word is the last name (Bitwarden's importer does the same).
		if parts := strings.Fields(str("fullName")); len(parts) > 0 {
			last = parts[len(parts)-1]
			first = strings.Join(parts[:len(parts)-1], " ")
			if first == "" {
				first, last = last, ""
			}
		}
	}
	it.First, it.Last = joinNonEmpty(" ", first, middle), last
	it.Email, it.Phone, it.Company = str("email"), str("phoneNumber"), str("company")
	it.Address = joinNonEmpty(", ", str("streetAddress"), str("floor"), str("city"), str("county"), str("stateOrProvince"), str("zipOrPostalCode"), str("countryOrRegion"))
	for _, l := range protonIdentityLabels {
		if v := str(l.key); v != "" {
			it.Fields = append(it.Fields, vault.Field{Name: l.label, Value: v, Hidden: l.hidden})
		}
	}
	for _, k := range []string{"extraPersonalDetails", "extraAddressDetails", "extraContactDetails", "extraWorkDetails"} {
		var fs []protonField
		if json.Unmarshal(m[k], &fs) == nil {
			protonExtraFields(it, fs)
		}
	}
	var ss []protonSection
	if json.Unmarshal(m["extraSections"], &ss) == nil {
		protonSections(it, ss)
	}
}

// protonItemToVault maps one active Proton Pass item. Types Raypass has no counterpart for
// (sshKey, wifi, custom) become notes with their values as fields, so nothing is lost.
func protonItemToVault(p protonItem, c *protonCounts) (vault.Item, bool) {
	d := p.Data
	it := vault.Item{Name: strings.TrimSpace(d.Metadata.Name), Notes: d.Metadata.Note, Fav: p.Pinned}
	switch d.Type {
	case "login":
		var l protonLogin
		if json.Unmarshal(d.Content, &l) != nil {
			return it, false
		}
		it.Type = vault.TypeLogin
		email := l.ItemEmail
		if email == "" && l.ItemUsername == "" {
			email = l.Username
		}
		protonLoginUser(&it, l.ItemUsername, email)
		it.Pass, it.TOTP = l.Password, l.TotpURI
		if len(l.AutofillURLs) > 0 { // authoritative when present; urls repeats its Default entries
			for _, u := range l.AutofillURLs {
				if strings.TrimSpace(u.URL) == "" {
					continue
				}
				if protonMatchModes[u.Mode] {
					it.URLs = append(it.URLs, strings.TrimSpace(u.URL))
				} else {
					it.Fields = append(it.Fields, vault.Field{Name: "URL (not autofilled)", Value: u.URL})
				}
			}
		} else {
			for _, u := range l.URLs {
				if u = strings.TrimSpace(u); u != "" {
					it.URLs = append(it.URLs, u)
				}
			}
		}
		c.passkeys += len(l.Passkeys)
	case "alias":
		it.Type = vault.TypeLogin
		it.User = strings.TrimSpace(p.AliasEmail)
		it.Notes = appendNote(it.Notes, protonAliasNote)
	case "note":
		it.Type = vault.TypeNote
	case "creditCard":
		var cc protonCard
		if json.Unmarshal(d.Content, &cc) != nil {
			return it, false
		}
		protonCardInto(&it, cc)
	case "identity":
		protonIdentityInto(&it, d.Content)
	case "sshKey":
		var k struct {
			PrivateKey  string          `json:"privateKey"`
			PublicKey   string          `json:"publicKey"`
			Fingerprint string          `json:"fingerprint"`
			Sections    []protonSection `json:"sections"`
		}
		if json.Unmarshal(d.Content, &k) != nil {
			return it, false
		}
		it.Type = vault.TypeNote
		for _, f := range []vault.Field{{Name: "Private key", Value: k.PrivateKey, Hidden: true}, {Name: "Public key", Value: k.PublicKey}, {Name: "Fingerprint", Value: k.Fingerprint}} {
			if strings.TrimSpace(f.Value) != "" {
				it.Fields = append(it.Fields, f)
			}
		}
		protonSections(&it, k.Sections)
	case "wifi":
		var w struct {
			SSID     string          `json:"ssid"`
			Password string          `json:"password"`
			Security int             `json:"security"`
			Sections []protonSection `json:"sections"`
		}
		if json.Unmarshal(d.Content, &w) != nil {
			return it, false
		}
		it.Type = vault.TypeNote
		if w.SSID != "" {
			it.Fields = append(it.Fields, vault.Field{Name: "SSID", Value: w.SSID})
		}
		if w.Password != "" {
			it.Fields = append(it.Fields, vault.Field{Name: "Password", Value: w.Password, Hidden: true})
		}
		if s := map[int]string{1: "WPA", 2: "WPA2", 3: "WPA3", 4: "WEP"}[w.Security]; s != "" {
			it.Fields = append(it.Fields, vault.Field{Name: "Security", Value: s})
		}
		protonSections(&it, w.Sections)
	case "custom":
		var cu struct {
			Sections []protonSection `json:"sections"`
		}
		if json.Unmarshal(d.Content, &cu) != nil {
			return it, false
		}
		it.Type = vault.TypeNote
		// Sections go after the extra fields, as Proton Pass shows them.
		protonExtraFields(&it, d.ExtraFields)
		protonSections(&it, cu.Sections)
		return named(it), true
	default:
		return it, false // a type newer than this importer: counted as skipped
	}
	protonExtraFields(&it, d.ExtraFields)
	return named(it), true
}

// named gives an item with no name one, the way the other importers do.
func named(it vault.Item) vault.Item {
	if it.Name == "" {
		var url string
		if len(it.URLs) > 0 {
			url = it.URLs[0]
		}
		if it.Name = nameFor(url, it.User); it.Name == "" {
			it.Name = "Imported"
		}
	}
	return it
}

// protonPassCSV reads Proton Pass's CSV export. CSV carries no extra fields, pins or trash, and
// aliases appear with type "alias" and the address in the email column.
func protonPassCSV(data []byte) (Result, error) {
	rows, err := readCSV(data)
	if err != nil {
		return Result{}, err
	}
	var r Result
	hasEmail := len(rows) > 0
	if hasEmail {
		_, hasEmail = rows[0]["email"]
	}
	for _, row := range rows {
		it := vault.Item{Name: strings.TrimSpace(row["name"]), Notes: row["note"], Folder: strings.ReplaceAll(strings.TrimSpace(row["vault"]), "/", "-")}
		switch strings.TrimSpace(row["type"]) {
		case "", "login":
			it.Type = vault.TypeLogin
			if hasEmail {
				protonLoginUser(&it, row["username"], row["email"])
			} else { // before Proton Pass 1.18 the username column held the email
				it.User = strings.TrimSpace(row["username"])
			}
			it.Pass, it.TOTP = row["password"], strings.TrimSpace(row["totp"])
			it.URLs = protonCSVURLs(&it, row)
		case "alias":
			it.Type = vault.TypeLogin
			it.User = strings.TrimSpace(pick(row, "email", "username"))
			it.Notes = appendNote(it.Notes, protonAliasNote)
		case "note":
			it.Type = vault.TypeNote
		case "creditCard":
			var cc protonCard
			if json.Unmarshal([]byte(row["note"]), &cc) != nil {
				r.Skipped++
				continue
			}
			it.Notes = cc.Note
			protonCardInto(&it, cc)
		case "identity":
			var m map[string]json.RawMessage
			if json.Unmarshal([]byte(row["note"]), &m) != nil {
				r.Skipped++
				continue
			}
			var note string
			_ = json.Unmarshal(m["note"], &note)
			it.Notes = note
			protonIdentityInto(&it, []byte(row["note"]))
		default: // sshKey, wifi, custom: the CSV holds only name and note
			it.Type = vault.TypeNote
		}
		if it.Type == vault.TypeLogin && it.User == "" && it.Pass == "" && len(it.URLs) == 0 && it.Notes == "" {
			r.Skipped++
			continue
		}
		r.Items = append(r.Items, named(it))
	}
	return r, nil
}

// protonCSVURLs reads the autofillUrls column (JSON, every mode) when present, else url (the
// Default-mode URLs joined with ", ").
func protonCSVURLs(it *vault.Item, row map[string]string) []string {
	var au []struct {
		URL  string `json:"url"`
		Mode int    `json:"mode"`
	}
	var out []string
	if s := strings.TrimSpace(row["autofillurls"]); s != "" && json.Unmarshal([]byte(s), &au) == nil && len(au) > 0 {
		for _, u := range au {
			if strings.TrimSpace(u.URL) == "" {
				continue
			}
			if protonMatchModes[u.Mode] {
				out = append(out, strings.TrimSpace(u.URL))
			} else {
				it.Fields = append(it.Fields, vault.Field{Name: "URL (not autofilled)", Value: u.URL})
			}
		}
		return out
	}
	for _, u := range strings.Split(row["url"], ", ") {
		if u = strings.TrimSpace(u); u != "" {
			out = append(out, u)
		}
	}
	return out
}
