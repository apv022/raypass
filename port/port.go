// Package port reads other password managers' exports and writes Raypass's own (design/DESIGN.md
// "Import" and "Export"): Bitwarden JSON (unencrypted) and CSV, Chrome/Edge/Brave CSV, Firefox
// CSV, 1Password CSV, KeePass(XC) CSV, LastPass CSV, Proton Pass JSON (data.json or the zip)
// and CSV, Raypass JSON and the encrypted Raypass export; exports are Raypass JSON, CSV (Bitwarden's CSV layout, so other managers can read it)
// and the encrypted Raypass export.
//
// Invariants: parsing is pure (no I/O, no network); errors name the format or the row, never a
// value from the file; an entry that cannot become an item is counted as skipped, never
// silently dropped.
package port

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"

	"github.com/apv022/raypass/vault"
)

// Formats accepted by Import.
const (
	Auto              = "auto"
	BitwardenJSON     = "bitwarden_json"
	BitwardenCSV      = "bitwarden_csv"
	ChromeCSV         = "chrome_csv"
	FirefoxCSV        = "firefox_csv"
	OnePasswordCSV    = "1password_csv"
	KeePassCSV        = "keepass_csv"
	LastPassCSV       = "lastpass_csv"
	ProtonPassJSON    = "protonpass_json" // data.json, or the whole export zip
	ProtonPassCSV     = "protonpass_csv"
	RaypassJSON       = "raypass_json"
	RaypassEncrypted  = "raypass_encrypted"
	exportFormatJSON  = "json"
	exportFormatCSV   = "csv"
	exportFormatCrypt = "encrypted"
)

// Result is what an import found.
type Result struct {
	Format  string
	Items   []vault.Item // ID is set only for Raypass JSON (kept so a re-import is idempotent)
	Skipped int
	// Notes say what the file held that could not become an item and is not in Skipped
	// (passkeys, trashed items, attachments), e.g. "3 passkeys were not imported".
	Notes []string
}

// Error is an import error; its text never contains data from the file.
type Error string

func (e Error) Error() string { return string(e) }

// Import parses data in format (Auto detects it). password is needed for RaypassEncrypted;
// minIter is the smallest KDF round count an encrypted export may declare.
func Import(format string, data []byte, password string, minIter int) (Result, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if format == "" || format == Auto {
		format = Detect(data)
		if format == "" {
			return Result{}, Error("could not tell the format of this file: pass format")
		}
	}
	var r Result
	var err error
	switch format {
	case BitwardenJSON:
		r, err = bitwardenJSON(data)
	case RaypassJSON:
		r, err = raypassJSON(data)
	case ProtonPassJSON:
		r, err = protonPassJSON(data)
	case ProtonPassCSV:
		r, err = protonPassCSV(data)
	case RaypassEncrypted:
		if password == "" {
			return Result{}, Error("this export is encrypted: pass its password")
		}
		plain, err2 := vault.OpenExport(data, password, minIter)
		if err2 != nil {
			if err2 == vault.ErrWrongPassword {
				return Result{}, Error("wrong export password")
			}
			return Result{}, Error(err2.Error())
		}
		r, err = raypassJSON(plain)
		vault.Wipe(plain)
	case BitwardenCSV, ChromeCSV, FirefoxCSV, OnePasswordCSV, KeePassCSV, LastPassCSV:
		var rows []map[string]string
		rows, err = readCSV(data)
		if err == nil {
			r = fromCSV(format, rows)
		}
	default:
		return Result{}, Error("unknown import format " + format)
	}
	r.Format = format
	return r, err
}

// Detect guesses the format of an export, "" when it cannot tell. A zip or an armored PGP
// message is taken for a Proton Pass export (the only such format Raypass reads; the import
// then says what is wrong with it).
func Detect(data []byte) string {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if isProtonZipOrPGP(data) {
		return ProtonPassJSON
	}
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '{' {
		if vault.IsExportFile(data) {
			return RaypassEncrypted
		}
		var probe map[string]json.RawMessage
		if json.Unmarshal(data, &probe) != nil {
			return ""
		}
		if _, ok := probe["raypass"]; ok {
			return RaypassJSON
		}
		if _, ok := probe["items"]; ok {
			return BitwardenJSON
		}
		if _, ok := probe["vaults"]; ok {
			return ProtonPassJSON
		}
		return ""
	}
	line, _, _ := bytes.Cut(data, []byte("\n"))
	rec, err := csv.NewReader(bytes.NewReader(line)).Read()
	if err != nil {
		return ""
	}
	h := map[string]bool{}
	for _, f := range rec {
		h[strings.ToLower(strings.TrimSpace(f))] = true
	}
	switch {
	case h["login_uri"] || h["login_username"]:
		return BitwardenCSV
	case h["httprealm"] || h["formactionorigin"]:
		return FirefoxCSV
	case h["grouping"] && h["extra"]:
		return LastPassCSV
	case h["otpauth"] || h["archived"] || (h["title"] && h["website"]):
		return OnePasswordCSV
	case (h["group"] && h["title"]) || (h["account"] && h["login name"]):
		return KeePassCSV
	case h["type"] && h["name"] && h["note"] && h["createtime"] && h["modifytime"]:
		return ProtonPassCSV
	case h["url"] && h["username"] && h["password"]:
		return ChromeCSV
	}
	return ""
}

// readCSV reads a CSV with a header row into maps keyed by the lowercased header.
func readCSV(data []byte) ([]map[string]string, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	all, err := r.ReadAll()
	if err != nil {
		return nil, Error("the CSV file could not be read")
	}
	if len(all) == 0 {
		return nil, Error("the CSV file is empty")
	}
	head := make([]string, len(all[0]))
	for i, h := range all[0] {
		head[i] = strings.ToLower(strings.TrimSpace(h))
	}
	var rows []map[string]string
	for _, rec := range all[1:] {
		m := map[string]string{}
		empty := true
		for i, v := range rec {
			if i < len(head) {
				m[head[i]] = v
				empty = empty && strings.TrimSpace(v) == ""
			}
		}
		if !empty {
			rows = append(rows, m)
		}
	}
	return rows, nil
}

// pick returns the first non-empty value among the given headers.
func pick(row map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := row[k]; strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "y", "x", "on":
		return true
	}
	return false
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// fromCSV maps rows of one of the CSV formats to items.
func fromCSV(format string, rows []map[string]string) Result {
	var r Result
	for _, row := range rows {
		it, ok := csvItem(format, row)
		if !ok {
			r.Skipped++
			continue
		}
		r.Items = append(r.Items, it)
	}
	return r
}

func csvItem(format string, row map[string]string) (vault.Item, bool) {
	it := vault.Item{Type: vault.TypeLogin}
	var url string
	switch format {
	case BitwardenCSV:
		if t := strings.ToLower(row["type"]); t == "note" {
			it.Type = vault.TypeNote
		} else if t != "" && t != "login" {
			return it, false
		}
		// unCSVSafe: Raypass's CSV export defuses formulas in these columns (csvSafe).
		it.Name, it.Notes, it.Folder, it.Fav = unCSVSafe(row["name"]), row["notes"], unCSVSafe(row["folder"]), truthy(row["favorite"])
		url = unCSVSafe(row["login_uri"])
		it.User, it.Pass, it.TOTP = unCSVSafe(row["login_username"]), row["login_password"], row["login_totp"]
		it.Fields = parseBWFields(row["fields"])
	case ChromeCSV:
		it.Name, url, it.User, it.Pass, it.Notes = row["name"], row["url"], row["username"], row["password"], pick(row, "note", "notes")
	case FirefoxCSV:
		url, it.User, it.Pass = row["url"], row["username"], row["password"]
	case OnePasswordCSV:
		it.Name = pick(row, "title", "name")
		url = pick(row, "url", "website", "urls")
		it.User, it.Pass = pick(row, "username", "login"), row["password"]
		it.TOTP = pick(row, "otpauth", "one-time password", "totp")
		it.Notes = pick(row, "notes", "notesplain")
		it.Fav = truthy(row["favorite"])
		it.Tags = splitList(row["tags"])
		if truthy(row["archived"]) {
			it.Tags = append(it.Tags, "archived")
		}
	case KeePassCSV:
		it.Name = pick(row, "title", "account")
		url = pick(row, "url", "web site")
		it.User = pick(row, "username", "login name")
		it.Pass = row["password"]
		it.Notes = pick(row, "notes", "comments")
		it.TOTP = row["totp"]
		it.Folder = strings.TrimPrefix(strings.TrimPrefix(row["group"], "Root/"), "Root")
	case LastPassCSV:
		url = row["url"]
		it.Name, it.User, it.Pass, it.TOTP, it.Notes = row["name"], row["username"], row["password"], row["totp"], row["extra"]
		it.Folder, it.Fav = strings.ReplaceAll(row["grouping"], "\\", "/"), truthy(row["fav"])
		if strings.TrimSpace(url) == "http://sn" {
			it.Type, url = vault.TypeNote, ""
		}
	}
	if url = strings.TrimSpace(url); url != "" {
		it.URLs = []string{url}
	}
	if strings.TrimSpace(it.Name) == "" {
		it.Name = nameFor(url, it.User)
	}
	if it.Type == vault.TypeLogin && it.User == "" && it.Pass == "" && url == "" && it.Notes == "" {
		return it, false
	}
	if it.Type == vault.TypeNote && strings.TrimSpace(it.Name) == "" && it.Notes == "" {
		return it, false
	}
	if strings.TrimSpace(it.Name) == "" {
		it.Name = "Imported"
	}
	return it, true
}

// nameFor names a login with no name after its host (the way browsers show them).
func nameFor(url, user string) string {
	u := strings.TrimSpace(url)
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	if i := strings.IndexAny(u, "/?#"); i >= 0 {
		u = u[:i]
	}
	u = strings.TrimPrefix(strings.ToLower(u), "www.")
	if u != "" {
		return u
	}
	return user
}

// parseBWFields reads Bitwarden CSV's "fields" column: one "name: value" per line.
func parseBWFields(s string) []vault.Field {
	var out []vault.Field
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, value, ok := strings.Cut(line, ": ")
		if !ok {
			name, value = line, ""
		}
		out = append(out, vault.Field{Name: name, Value: value})
	}
	return out
}
