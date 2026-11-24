package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The real extension packs, both zips are reproducible, and the Firefox manifest is right.
func TestPackExtension(t *testing.T) {
	out := t.TempDir()
	if err := pack("../../extension", out); err != nil {
		t.Fatal(err)
	}
	first := map[string][]byte{}
	for _, n := range []string{"raypass-extension-chrome.zip", "raypass-extension-firefox.zip"} {
		b, err := os.ReadFile(filepath.Join(out, n))
		if err != nil {
			t.Fatal(err)
		}
		first[n] = b
	}
	again := t.TempDir()
	if err := pack("../../extension", again); err != nil {
		t.Fatal(err)
	}
	for n, b := range first {
		b2, _ := os.ReadFile(filepath.Join(again, n))
		if !bytes.Equal(b, b2) {
			t.Errorf("%s is not reproducible", n)
		}
	}

	read := func(b []byte) (map[string]bool, map[string]any) {
		zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			t.Fatal(err)
		}
		names := map[string]bool{}
		var m map[string]any
		for _, f := range zr.File {
			names[f.Name] = true
			if f.Name == "manifest.json" {
				rc, _ := f.Open()
				if err := json.NewDecoder(rc).Decode(&m); err != nil {
					t.Fatal(err)
				}
				rc.Close()
			}
		}
		return names, m
	}
	names, cm := read(first["raypass-extension-chrome.zip"])
	for _, want := range []string{"manifest.json", "background.js", "content.js", "lib/forms.js", "lib/urlmatch.js", "popup/popup.html", "options/options.html", "offscreen/offscreen.html", "icons/icon-128.png"} {
		if !names[want] {
			t.Errorf("chrome zip lacks %s", want)
		}
	}
	for n := range names {
		if strings.HasPrefix(n, "test/") || strings.HasSuffix(n, ".md") {
			t.Errorf("chrome zip has %s", n)
		}
	}
	if asMap(cm["background"])["service_worker"] != "background.js" || cm["key"] == nil {
		t.Errorf("chrome manifest: %v", cm)
	}

	_, fm := read(first["raypass-extension-firefox.zip"])
	gecko := asMap(asMap(fm["browser_specific_settings"])["gecko"])
	if gecko["id"] != geckoID {
		t.Errorf("gecko id %v", gecko)
	}
	bg := asMap(fm["background"])
	if bg["service_worker"] != nil || len(asList(bg["scripts"])) != 1 || bg["type"] != "module" {
		t.Errorf("firefox background %v", bg)
	}
	if fm["key"] != nil {
		t.Error("firefox manifest has the Chrome key")
	}
	for _, p := range asList(fm["permissions"]) {
		if p == "offscreen" {
			t.Error("firefox manifest asks for offscreen")
		}
	}
}

// The ID the key yields is the one the native host allows by default.
func TestChromeIDMatchesHost(t *testing.T) {
	raw, err := os.ReadFile("../../extension/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(raw, &m)
	id, err := idFromKey(str(m["key"]))
	if err != nil || id != chromeID {
		t.Fatalf("%s %v", id, err)
	}
	src, err := os.ReadFile("../raypass-native/install.go")
	if err != nil {
		t.Fatal(err)
	}
	if got := regexp.MustCompile(`devChromeID = "([a-p]{32})"`).FindSubmatch(src); got == nil || string(got[1]) != chromeID {
		t.Fatalf("cmd/raypass-native devChromeID %q, want %s", got, chromeID)
	}
}

// The extension's copy of the public-suffix list is the urlmatch package's, so the extension's
// frame policy and the vault's matching agree.
func TestSuffixesMatchURLMatch(t *testing.T) {
	txt, err := os.ReadFile("../../urlmatch/suffixes.txt")
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, l := range strings.Split(string(txt), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "#") {
			want = append(want, strings.Fields(l)...)
		}
	}
	js, err := os.ReadFile("../../extension/lib/urlmatch.js")
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)const MULTI = new Set\(\[(.*?)\]\);`).FindSubmatch(js)
	if block == nil {
		t.Fatal("no MULTI set in urlmatch.js")
	}
	var got []string
	for _, m := range regexp.MustCompile(`'([^']+)'`).FindAllSubmatch(block[1], -1) {
		got = append(got, string(m[1]))
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("extension/lib/urlmatch.js suffixes differ from urlmatch/suffixes.txt:\n%v\n%v", got, want)
	}
}

func TestIDFromKnownKey(t *testing.T) {
	if _, err := idFromKey("not base64!"); err == nil {
		t.Fatal("bad key accepted")
	}
	id, _ := idFromKey("AAAA")
	if len(id) != 32 || strings.Trim(id, "abcdefghijklmnop") != "" {
		t.Fatalf("%q", id)
	}
}

func TestMissingReference(t *testing.T) {
	m := map[string]any{"background": map[string]any{"service_worker": "nope.js"}}
	if err := checkReferences(m, []string{"background.js"}); err == nil {
		t.Fatal("missing file not reported")
	}
}
