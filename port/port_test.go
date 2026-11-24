package port

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/apv022/raypass/vault"
)

func load(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func imp(t *testing.T, name, wantFormat string) Result {
	t.Helper()
	data := load(t, name)
	if got := Detect(data); got != wantFormat {
		t.Fatalf("%s: detected %q, want %q", name, got, wantFormat)
	}
	r, err := Import(Auto, data, "", vault.MinIter)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if r.Format != wantFormat {
		t.Fatal(r.Format)
	}
	// Explicit format gives the same.
	r2, err := Import(wantFormat, data, "", vault.MinIter)
	if err != nil || len(r2.Items) != len(r.Items) {
		t.Fatal("explicit format differs")
	}
	return r
}

func find(t *testing.T, r Result, name string) vault.Item {
	t.Helper()
	for _, it := range r.Items {
		if it.Name == name {
			return it
		}
	}
	t.Fatalf("no item named %q", name)
	return vault.Item{}
}

func TestBitwardenJSON(t *testing.T) {
	r := imp(t, "bitwarden.json", BitwardenJSON)
	if len(r.Items) != 4 || r.Skipped != 1 {
		t.Fatalf("%d items, %d skipped", len(r.Items), r.Skipped)
	}
	gh := find(t, r, "GitHub")
	if gh.Type != vault.TypeLogin || gh.User != "octo" || gh.Pass != "gh-secret" || len(gh.URLs) != 2 || gh.Folder != "Work" || !gh.Fav ||
		!strings.HasPrefix(gh.TOTP, "otpauth://") || len(gh.Fields) != 2 || !gh.Fields[0].Hidden || gh.Fields[1].Hidden ||
		len(gh.History) != 1 || gh.History[0].Pass != "old-gh" || gh.History[0].Until == 0 {
		t.Fatalf("%+v", gh)
	}
	if n := find(t, r, "Wifi"); n.Type != vault.TypeNote || !strings.Contains(n.Notes, "home") {
		t.Fatal("note")
	}
	if c := find(t, r, "Visa"); c.Type != vault.TypeCard || c.Exp != "07/29" || c.CVV != "123" || c.Holder != "Ann Smith" {
		t.Fatalf("%+v", c)
	}
	if i := find(t, r, "Me"); i.Type != vault.TypeIdentity || i.Address != "1 Main St, Springfield, 12345, US" || i.Company != "ACME" {
		t.Fatalf("%+v", i)
	}
	if _, err := Import(BitwardenJSON, []byte(`{"encrypted":true,"items":[]}`), "", vault.MinIter); err == nil {
		t.Fatal("encrypted bitwarden accepted")
	}
}

func TestBitwardenCSV(t *testing.T) {
	r := imp(t, "bitwarden.csv", BitwardenCSV)
	if len(r.Items) != 3 {
		t.Fatal(len(r.Items))
	}
	gh := find(t, r, "GitHub")
	if gh.User != "octo" || gh.Pass != "gh-secret" || gh.Folder != "Work" || !gh.Fav || gh.TOTP != "JBSWY3DPEHPK3PXP" || len(gh.Fields) != 2 || gh.Fields[1].Value != "pro" {
		t.Fatalf("%+v", gh)
	}
	if find(t, r, "Wifi").Type != vault.TypeNote {
		t.Fatal("note")
	}
	find(t, r, "example.org") // unnamed login named after its host
}

func TestBrowsersCSV(t *testing.T) {
	r := imp(t, "chrome.csv", ChromeCSV)
	if len(r.Items) != 2 {
		t.Fatal(len(r.Items))
	}
	if ex := find(t, r, "example.com"); ex.Pass != `pa,ss"word` || ex.Notes != "with a note" {
		t.Fatalf("%+v", ex)
	}
	r = imp(t, "firefox.csv", FirefoxCSV)
	if len(r.Items) != 2 || find(t, r, "accounts.example.com").User != "alice" {
		t.Fatal("firefox")
	}
}

func TestOthersCSV(t *testing.T) {
	r := imp(t, "1password.csv", OnePasswordCSV)
	if gh := find(t, r, "GitHub"); !gh.Fav || !reflect.DeepEqual(gh.Tags, []string{"dev", "work"}) || gh.TOTP == "" || gh.Notes != "dev account" {
		t.Fatalf("%+v", gh)
	}
	if ob := find(t, r, "Old bank"); len(ob.Tags) != 1 || ob.Tags[0] != "archived" {
		t.Fatal("archived")
	}
	r = imp(t, "keepass.csv", KeePassCSV)
	if gh := find(t, r, "GitHub"); gh.Folder != "Work" || gh.Pass != "gh-secret" || gh.TOTP == "" {
		t.Fatalf("%+v", gh)
	}
	if rt := find(t, r, "Router"); rt.Folder != "" || rt.URLs[0] != "http://192.168.1.1" {
		t.Fatalf("%+v", rt)
	}
	r = imp(t, "lastpass.csv", LastPassCSV)
	if gh := find(t, r, "GitHub"); gh.Folder != "Work/Code" || !gh.Fav || gh.TOTP == "" || gh.Notes != "dev account" {
		t.Fatalf("%+v", gh)
	}
	if db := find(t, r, "DB server"); db.Type != vault.TypeNote || len(db.URLs) != 0 || !strings.Contains(db.Notes, "db1") {
		t.Fatalf("%+v", db)
	}
}

func TestDetectUnknown(t *testing.T) {
	for _, s := range []string{"", "hello", "a,b,c\n1,2,3", `{"x":1}`, "{bad json"} {
		if Detect([]byte(s)) != "" {
			t.Errorf("detected %q", s)
		}
		if _, err := Import(Auto, []byte(s), "", vault.MinIter); err == nil {
			t.Errorf("imported %q", s)
		}
	}
	if _, err := Import("nope", nil, "", 1); err == nil {
		t.Fatal("unknown format")
	}
}

func sample() []vault.Item {
	return []vault.Item{
		{ID: "0000000000000001", Type: vault.TypeLogin, Name: "GitHub", Folder: "Work", Fav: true, Tags: []string{"dev"}, User: "octo", Pass: "p,\"q\"",
			URLs: []string{"https://github.com", "https://gist.github.com"}, TOTP: "JBSWY3DPEHPK3PXP", Notes: "line1\nline2",
			Fields: []vault.Field{{Name: "PIN", Value: "1", Hidden: true}}, History: []vault.History{{Pass: "old", Until: 5}}},
		{ID: "0000000000000002", Type: vault.TypeCard, Name: "Visa", Holder: "Ann", Number: "4111", Exp: "07/29", CVV: "123", Brand: "Visa"},
		{ID: "0000000000000003", Type: vault.TypeWallet, Name: "BTC", Seed: "abandon abandon about", Network: "bitcoin", Address: "bc1q"},
		{ID: "0000000000000004", Type: vault.TypeIdentity, Name: "Me", First: "Ann", Email: "a@x"},
		{ID: "0000000000000005", Type: vault.TypeNote, Name: "Note", Notes: "hello ü"},
	}
}

func TestRaypassJSONRoundTrip(t *testing.T) {
	now := time.Unix(1700000000, 0)
	data, err := ExportJSON(sample(), now)
	if err != nil {
		t.Fatal(err)
	}
	if Detect(data) != RaypassJSON {
		t.Fatal("detect")
	}
	r, err := Import(Auto, data, "", vault.MinIter)
	if err != nil || len(r.Items) != 5 {
		t.Fatal(err)
	}
	for i, want := range sample() {
		got := r.Items[i]
		w, g := want.Output(), got.Output()
		wj, _ := json.Marshal(w)
		gj, _ := json.Marshal(g)
		if string(wj) != string(gj) {
			t.Errorf("item %d differs after round trip", i)
		}
	}
	enc, err := ExportEncrypted(sample(), "export pw", vault.MinIter, now)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(enc), "GitHub") || strings.Contains(string(enc), "octo") {
		t.Fatal("plaintext in encrypted export")
	}
	if Detect(enc) != RaypassEncrypted {
		t.Fatal("detect encrypted")
	}
	if _, err := Import(Auto, enc, "", vault.MinIter); err == nil {
		t.Fatal("no password accepted")
	}
	if _, err := Import(Auto, enc, "wrong", vault.MinIter); err == nil || err.Error() != "wrong export password" {
		t.Fatal(err)
	}
	r, err = Import(Auto, enc, "export pw", vault.MinIter)
	if err != nil || len(r.Items) != 5 || r.Items[0].Pass != `p,"q"` {
		t.Fatal(err)
	}
}

func TestCSVExport(t *testing.T) {
	data, err := ExportCSV(sample())
	if err != nil {
		t.Fatal(err)
	}
	if Detect(data) != BitwardenCSV {
		t.Fatal("our CSV is not read as Bitwarden CSV")
	}
	r, err := Import(Auto, data, "", vault.MinIter)
	if err != nil || len(r.Items) != 5 {
		t.Fatal(err, len(r.Items))
	}
	gh := r.Items[0]
	if gh.Pass != `p,"q"` || gh.Notes != "line1\nline2" || gh.URLs[0] != "https://github.com" || gh.Folder != "Work" || !gh.Fav {
		t.Fatalf("%+v", gh)
	}
	btc := r.Items[2]
	if btc.Type != vault.TypeNote || !strings.Contains(fieldsText(btc), "seed: abandon abandon about") {
		t.Fatal("wallet fields lost in CSV")
	}
}

func fieldsText(it vault.Item) string {
	var s []string
	for _, f := range it.Fields {
		s = append(s, f.Name+": "+f.Value)
	}
	return strings.Join(s, "\n")
}

func TestFilename(t *testing.T) {
	now := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	if Filename("csv", now) != "raypass-export-2026-11-24.csv" || Filename("encrypted", now) != "raypass-export-2026-11-24.encrypted.json" || Filename("json", now) != "raypass-export-2026-11-24.json" {
		t.Fatal("filenames")
	}
}
