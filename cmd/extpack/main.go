// Command extpack packs the browser extension (extension/) into the two zips the browsers take:
//
//	build/raypass-extension-chrome.zip    Chrome, Chromium, Brave, Edge, Vivaldi
//	build/raypass-extension-firefox.zip   Firefox (128+)
//
// There is no build step: the zips hold the source files as they are. Only the manifest
// differs. extension/manifest.json is the Chrome one (so the folder loads unpacked as is), and
// the Firefox manifest is derived from it: a background script instead of a service worker,
// the gecko add-on id, and no Chrome-only keys or permissions.
//
// Invariants: the zips are reproducible (sorted entries, fixed times); tests, docs and dot files
// stay out; every file the manifest names exists; the manifest's public key gives the
// extension ID the native host allows by default (chromeID).
//
//	go run ./cmd/extpack [-src extension] [-out build]
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// chromeID is the extension ID that the manifest's "key" yields; cmd/raypass-native allows it
// by default.
const chromeID = "nophamhaahdfdhocbagichcklnlnngdi"

// geckoID is the Firefox add-on id (design/DESIGN.md: raypass@raypass.app).
const geckoID = "raypass@raypass.app"

// firefoxMin is the oldest Firefox the extension supports: the current ESR line, which has MV3
// background module scripts, storage.session and the scripting of host permissions we use.
const firefoxMin = "128.0"

// zipTime is the modification time of every entry, so two packs of the same tree are equal.
var zipTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func main() {
	src := flag.String("src", "extension", "the extension source folder")
	out := flag.String("out", "build", "where to write the zips")
	flag.Parse()
	if err := pack(*src, *out); err != nil {
		fmt.Fprintln(os.Stderr, "extpack:", err)
		os.Exit(1)
	}
}

func pack(src, out string) error {
	raw, err := os.ReadFile(filepath.Join(src, "manifest.json"))
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("manifest.json: %w", err)
	}
	if id, err := idFromKey(str(m["key"])); err != nil || id != chromeID {
		return fmt.Errorf("manifest key gives extension ID %q, want %q (%v)", id, chromeID, err)
	}
	files, err := sourceFiles(src)
	if err != nil {
		return err
	}
	if err := checkReferences(m, files); err != nil {
		return err
	}
	ff, err := firefoxManifest(m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	for _, z := range []struct {
		name     string
		manifest []byte
	}{
		{"raypass-extension-chrome.zip", raw},
		{"raypass-extension-firefox.zip", ff},
	} {
		b, err := zipTree(src, files, z.manifest)
		if err != nil {
			return err
		}
		p := filepath.Join(out, z.name)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			return err
		}
		fmt.Println("wrote", p)
	}
	return nil
}

func str(v any) string { s, _ := v.(string); return s }

// idFromKey computes a Chrome extension ID: the first 16 bytes of SHA-256 of the DER public key,
// written in hex with the digits 0-f mapped to the letters a-p.
func idFromKey(key string) (string, error) {
	der, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(der) == 0 {
		return "", errors.New("manifest has no valid base64 \"key\"")
	}
	sum := sha256.Sum256(der)
	h := hex.EncodeToString(sum[:16])
	var b strings.Builder
	for _, c := range h {
		if c >= '0' && c <= '9' {
			b.WriteRune('a' + (c - '0'))
		} else {
			b.WriteRune('k' + (c - 'a'))
		}
	}
	return b.String(), nil
}

// sourceFiles lists the files that go into the zips (slash paths, sorted).
func sourceFiles(src string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		rel = filepath.ToSlash(rel)
		name := d.Name()
		if rel != "." && strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if rel == "test" {
				return filepath.SkipDir
			}
			return nil
		}
		if rel == "manifest.json" || strings.HasSuffix(name, ".md") {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	sort.Strings(files)
	return files, err
}

// checkReferences makes sure every file the manifest names is packed.
func checkReferences(m map[string]any, files []string) error {
	have := map[string]bool{}
	for _, f := range files {
		have[f] = true
	}
	var refs []string
	add := func(v any) {
		if s := str(v); s != "" {
			refs = append(refs, s)
		}
	}
	for _, v := range asMap(m["icons"]) {
		add(v)
	}
	bg := asMap(m["background"])
	add(bg["service_worker"])
	for _, cs := range asList(m["content_scripts"]) {
		for _, js := range asList(asMap(cs)["js"]) {
			add(js)
		}
	}
	act := asMap(m["action"])
	add(act["default_popup"])
	for _, v := range asMap(act["default_icon"]) {
		add(v)
	}
	add(asMap(m["options_ui"])["page"])
	var missing []string
	for _, r := range refs {
		if !have[path.Clean(r)] {
			missing = append(missing, r)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("manifest names missing files: %s", strings.Join(missing, ", "))
	}
	return nil
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func asList(v any) []any         { l, _ := v.([]any); return l }

// firefoxManifest derives the Firefox manifest from the Chrome one.
func firefoxManifest(chrome map[string]any) ([]byte, error) {
	m := map[string]any{}
	for k, v := range chrome {
		m[k] = v
	}
	delete(m, "key")
	delete(m, "minimum_chrome_version")
	bg := asMap(chrome["background"])
	sw := str(bg["service_worker"])
	if sw == "" {
		return nil, errors.New("manifest has no background.service_worker")
	}
	// Firefox runs MV3 backgrounds as event pages: same file, as a module script.
	m["background"] = map[string]any{"scripts": []any{sw}, "type": str(bg["type"])}
	var perms []any
	for _, p := range asList(chrome["permissions"]) {
		if p != "offscreen" { // Chrome only; Firefox's background page has a document
			perms = append(perms, p)
		}
	}
	m["permissions"] = perms
	m["browser_specific_settings"] = map[string]any{
		"gecko": map[string]any{
			"id": geckoID, "strict_min_version": firefoxMin,
			// addons.mozilla.org requires the declaration; Raypass collects nothing.
			"data_collection_permissions": map[string]any{"required": []any{"none"}},
		},
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// zipTree zips files from src plus the given manifest, reproducibly.
func zipTree(src string, files []string, manifest []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name string, data []byte) error {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: zipTime}
		h.SetMode(0o644)
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	if err := write("manifest.json", manifest); err != nil {
		return nil, err
	}
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(src, filepath.FromSlash(f)))
		if err != nil {
			return nil, err
		}
		if err := write(f, b); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
