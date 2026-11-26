package port

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"reflect"
	"strings"
	"testing"

	"github.com/apv022/raypass/vault"
)

// protonZip builds a Proton Pass export zip the way Proton Pass lays it out.
func protonZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	if _, err := w.Create("Proton Pass/"); err != nil {
		t.Fatal(err)
	}
	for name, b := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func fieldsByName(it vault.Item) map[string]vault.Field {
	m := map[string]vault.Field{}
	for _, f := range it.Fields {
		m[f.Name] = f
	}
	return m
}

func checkProtonJSON(t *testing.T, r Result) {
	t.Helper()
	// 9 items in the file: 1 trashed (noted), 1 unknown type (skipped), 7 imported.
	if len(r.Items) != 7 || r.Skipped != 1 {
		t.Fatalf("%d items, %d skipped", len(r.Items), r.Skipped)
	}
	wantNotes := []string{
		"2 passkeys were not imported (passkeys can't be moved between managers)",
		"1 item in Proton Pass's trash was not imported",
		"1 file attachment was not imported",
	}
	if !reflect.DeepEqual(r.Notes, wantNotes) {
		t.Fatalf("notes %q", r.Notes)
	}
	l := find(t, r, "Example login")
	if l.Type != vault.TypeLogin || l.User != "john" || l.Pass != "correct horse battery" || !l.Fav || l.Folder != "Personal" ||
		!strings.HasPrefix(l.TOTP, "otpauth://totp/Example") || l.Notes != "My login note." {
		t.Fatalf("login %+v", l)
	}
	if !reflect.DeepEqual(l.URLs, []string{"https://example.com/", "https://login.example.com/"}) {
		t.Fatalf("urls %q", l.URLs)
	}
	f := fieldsByName(l)
	if f["Email"].Value != "john@example.com" || f["Recovery code"] != (vault.Field{Name: "Recovery code", Value: "RC-1234"}) ||
		!f["Security answer"].Hidden || f["Backup 2FA"] != (vault.Field{Name: "Backup 2FA", Value: "JBSWY3DPEHPK3PXQ", Hidden: true}) ||
		f["Renewal"].Value != "2027-01-31" || f["URL (not autofilled)"].Value != "https://never.example.com/" {
		t.Fatalf("login fields %+v", l.Fields)
	}
	e := find(t, r, "Email only login")
	if e.User != "jane@example.org" || e.Folder != "Personal/Clients/Nested" || e.TOTP != "otpauth://totp/X?secret=MFRGGZDFMZTWQ2LK" || len(e.Fields) != 0 {
		t.Fatalf("email-only login %+v", e)
	}
	n := find(t, r, "Wi-Fi at the office")
	if n.Type != vault.TypeNote || n.Folder != "Personal/Clients" || n.Notes == "" || !fieldsByName(n)["Door code"].Hidden {
		t.Fatalf("note %+v", n)
	}
	a := find(t, r, "Newsletter alias")
	if a.Type != vault.TypeLogin || a.User != "news.lilac123@passinbox.com" || !strings.HasPrefix(a.Notes, "for newsletters\n\nProton Pass alias") {
		t.Fatalf("alias %+v", a)
	}
	c := find(t, r, "Visa card")
	if c.Type != vault.TypeCard || c.Holder != "John Doe" || c.Number != "4111111111111111" || c.CVV != "123" || c.Exp != "07/29" ||
		c.Brand != "Visa" || !c.Fav || c.Folder != "Finance" || fieldsByName(c)["PIN"] != (vault.Field{Name: "PIN", Value: "9876", Hidden: true}) ||
		fieldsByName(c)["Bank phone"].Value != "+1 555 0100" {
		t.Fatalf("card %+v", c)
	}
	id := find(t, r, "Me")
	if id.Type != vault.TypeIdentity || id.First != "John Middle" || id.Last != "Doe" || id.Email != "john@example.com" ||
		id.Phone != "+1 555 0101" || id.Company != "Example Corp" || id.Address != "23 Street, 12th Floor, New York, NY, 10001, US" {
		t.Fatalf("identity %+v", id)
	}
	idf := fieldsByName(id)
	for name, want := range map[string]vault.Field{
		"Birthdate": {Value: "1990-01-02"}, "Social security number": {Value: "078-05-1120", Hidden: true},
		"X handle": {Value: "@johnd"}, "Job title": {Value: "Engineer"}, "Nickname": {Value: "JD"},
		"Signal": {Value: "+1 555 0199", Hidden: true}, "Blood type": {Value: "O+"}, "Loyalty number": {Value: "L-77"},
	} {
		want.Name = name
		if idf[name] != want {
			t.Fatalf("identity field %s: %+v", name, idf[name])
		}
	}
	if _, ok := idf["Gender"]; ok {
		t.Fatal("empty identity values become fields")
	}
	k := find(t, r, "Server key")
	if k.Type != vault.TypeNote || !fieldsByName(k)["Private key"].Hidden || fieldsByName(k)["Host"].Value != "example.com" {
		t.Fatalf("ssh key %+v", k)
	}
}

func TestProtonPassJSON(t *testing.T) {
	checkProtonJSON(t, imp(t, "protonpass/data.json", ProtonPassJSON))
}

func TestProtonPassZip(t *testing.T) {
	z := protonZip(t, map[string][]byte{"Proton Pass/data.json": load(t, "protonpass/data.json"), "Proton Pass/files/a.pdf": []byte("%PDF")})
	if got := Detect(z); got != ProtonPassJSON {
		t.Fatalf("detected %q", got)
	}
	r, err := Import(Auto, z, "", vault.MinIter)
	if err != nil {
		t.Fatal(err)
	}
	checkProtonJSON(t, r)
	if r.Format != ProtonPassJSON {
		t.Fatal(r.Format)
	}
}

func TestProtonPassCSV(t *testing.T) {
	r := imp(t, "protonpass/protonpass.csv", ProtonPassCSV)
	if len(r.Items) != 6 || r.Skipped != 0 {
		t.Fatalf("%d items, %d skipped", len(r.Items), r.Skipped)
	}
	l := find(t, r, "login title")
	if l.User != "john" || l.Pass != "password123" || l.Folder != "Personal" || l.Notes != "login note" ||
		!reflect.DeepEqual(l.URLs, []string{"https://example.com/", "https://proton.me/"}) ||
		fieldsByName(l)["Email"].Value != "john@example.com" || fieldsByName(l)["URL (not autofilled)"].Value != "https://never.example.com/" {
		t.Fatalf("login %+v", l)
	}
	if e := find(t, r, "email only"); e.User != "jane@example.org" || len(e.Fields) != 0 {
		t.Fatalf("email only %+v", e)
	}
	if a := find(t, r, "alias title"); a.User != "news.lilac123@passinbox.com" || !strings.Contains(a.Notes, "Proton Pass alias") {
		t.Fatalf("alias %+v", a)
	}
	c := find(t, r, "credit card title")
	if c.Type != vault.TypeCard || c.Number != "4242424242424242" || c.Exp != "02/24" || c.Notes != "credit card note" || c.Folder != "Second vault" || !fieldsByName(c)["PIN"].Hidden {
		t.Fatalf("card %+v", c)
	}
	id := find(t, r, "identity title")
	if id.Type != vault.TypeIdentity || id.First != "John Middle" || id.Last != "Doe" || id.Notes != "identity note" || id.Address != "23 Street, New York" || fieldsByName(id)["Nickname"].Value != "JD" {
		t.Fatalf("identity %+v", id)
	}
	if n := find(t, r, "note title"); n.Type != vault.TypeNote || n.Notes != "this is my note" {
		t.Fatalf("note %+v", n)
	}
	// The header of exports before Proton Pass 1.18 (no email column): username is the email.
	old := "type,name,url,username,password,note,totp,createTime,modifyTime,vault\nlogin,old,https://a.example/,me@example.com,pw,,,1,2,Personal\n"
	r, err := Import(Auto, []byte(old), "", vault.MinIter)
	if err != nil || r.Format != ProtonPassCSV || len(r.Items) != 1 || r.Items[0].User != "me@example.com" {
		t.Fatalf("old csv: %v %+v", err, r)
	}
}

func TestProtonPassEncrypted(t *testing.T) {
	cases := map[string][]byte{
		"encrypted flag": load(t, "protonpass/encrypted.json"),
		"armored pgp":    load(t, "protonpass/data.pgp"),
		"pgp in zip":     protonZip(t, map[string][]byte{"Proton Pass/data.pgp": load(t, "protonpass/data.pgp")}),
	}
	for name, data := range cases {
		_, err := Import(Auto, data, "", vault.MinIter)
		if err != errProtonEncrypted {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestProtonPassHostile(t *testing.T) {
	expectErr := func(name string, data []byte, want string) {
		t.Helper()
		_, err := Import(ProtonPassJSON, data, "", vault.MinIter)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	expectErr("malformed json", load(t, "protonpass/malformed.json"), "not a Proton Pass JSON export")
	expectErr("not a zip", []byte(zipMagic+"garbage"), "could not be read")
	expectErr("zip without data.json", protonZip(t, map[string][]byte{"other.txt": []byte("x")}), "no Proton Pass/data.json")

	// Zip bomb: data.json inflates past the cap. Once with an honest header, once with a header
	// claiming 10 bytes (archive/zip refuses to read past the declared size; the read limit
	// would stop it otherwise).
	var bomb bytes.Buffer
	fw, _ := flate.NewWriter(&bomb, flate.BestCompression)
	zeros := make([]byte, 1<<20)
	for i := 0; i < maxProtonJSON>>20+1; i++ {
		_, _ = fw.Write(zeros)
	}
	_ = fw.Close()
	for _, lie := range []bool{false, true} {
		var buf bytes.Buffer
		w := zip.NewWriter(&buf)
		size := uint64(maxProtonJSON + 1<<20)
		if lie {
			size = 10
		}
		f, err := w.CreateRaw(&zip.FileHeader{Name: protonDataJSON, Method: zip.Deflate, CompressedSize64: uint64(bomb.Len()), UncompressedSize64: size})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write(bomb.Bytes())
		_ = w.Close()
		want := "too large"
		if lie {
			want = "could not be read"
		}
		expectErr("zip bomb", buf.Bytes(), want)
	}

	// Too many entries.
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for i := 0; i <= maxZipEntries; i++ {
		if _, err := w.CreateHeader(&zip.FileHeader{Name: "Proton Pass/files/f", Method: zip.Store}); err != nil {
			t.Fatal(err)
		}
	}
	_ = w.Close()
	expectErr("entry count", buf.Bytes(), "too many entries")

	// A cyclic folder chain ends; a "/" in a vault name doesn't make a sub-folder.
	cyc := `{"vaults":{"v":{"name":"A/B","folders":[{"folderId":"x","parentFolderId":"y","name":"X"},{"folderId":"y","parentFolderId":"x","name":"Y"}],
	"items":[{"data":{"metadata":{"name":"n"},"type":"note","content":{}},"state":1,"folderId":"x"}]}}}`
	r, err := Import(ProtonPassJSON, []byte(cyc), "", vault.MinIter)
	if err != nil || len(r.Items) != 1 || !strings.HasPrefix(r.Items[0].Folder, "A-B/") || strings.Count(r.Items[0].Folder, "/") > 33 {
		t.Fatalf("cyclic folders: %v %+v", err, r)
	}
}
