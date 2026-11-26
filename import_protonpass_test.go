package raypass

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// TestImportProtonPassZip imports Proton Pass's export zip end to end through raypass.import
// with encoding base64, the way the web UI sends a binary file.
func TestImportProtonPassZip(t *testing.T) {
	e := newEnv(t)
	call(t, e.a1, "raypass.setup", map[string]any{"password": pw})
	js, err := os.ReadFile("port/testdata/protonpass/data.json")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("Proton Pass/data.json")
	_, _ = f.Write(js)
	_ = zw.Close()
	args := map[string]any{"data": base64.StdEncoding.EncodeToString(buf.Bytes()), "encoding": "base64"}

	wantNotes := []any{
		"2 passkeys were not imported (passkeys can't be moved between managers)",
		"1 item in Proton Pass's trash was not imported",
		"1 file attachment was not imported",
	}
	before := len(e.feedBodies(t, FeedVault))
	d := raw(e.a1, "raypass.import", args, opts{dry: true}).obj(t)
	would, _ := d["would"].(map[string]any)
	if d["dry_run"] != true || would["imported"] != float64(7) || would["format"] != "protonpass_json" || !reflect.DeepEqual(would["notes"], wantNotes) {
		t.Fatalf("dry run: %v", d)
	}
	if after := len(e.feedBodies(t, FeedVault)); after != before {
		t.Fatal("a dry-run import wrote entries")
	}
	res := call(t, e.a1, "raypass.import", args).obj(t)
	if res["imported"] != float64(7) || res["skipped"] != float64(1) || !reflect.DeepEqual(res["notes"], wantNotes) {
		t.Fatalf("import: %v", res)
	}
	var items []any
	if err := json.Unmarshal(call(t, e.a1, "raypass.list", nil).body, &items); err != nil || len(items) != 7 {
		t.Fatalf("%d items listed", len(items))
	}
	// Importing again adds nothing.
	if res := call(t, e.a1, "raypass.import", args).obj(t); res["imported"] != float64(0) {
		t.Fatalf("re-import: %v", res)
	}

	// Plain-text data.json works too, notes is [] for other formats, and bad input is refused.
	if res := call(t, e.a1, "raypass.import", map[string]any{"data": "url,username,password\nhttps://a.example,u,p\n"}).obj(t); !reflect.DeepEqual(res["notes"], []any{}) {
		t.Fatalf("csv notes: %v", res["notes"])
	}
	expectErr(t, raw(e.a1, "raypass.import", map[string]any{"data": "%%%", "encoding": "base64"}, opts{}), 400, "bad_request")
	expectErr(t, raw(e.a1, "raypass.import", map[string]any{"data": "x", "encoding": "hex"}, opts{}), 400, "bad_request")
	msg := expectErr(t, raw(e.a1, "raypass.import", map[string]any{"data": `{"encrypted": true, "vaults": {}}`}, opts{}), 400, "bad_request")
	if msg != "Proton Pass encrypted exports aren't supported: export again with encryption turned off" {
		t.Fatal(msg)
	}
}
