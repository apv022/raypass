package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTargets(t *testing.T) {
	want := map[string][]string{
		"linux": {
			".config/google-chrome/NativeMessagingHosts",
			".config/chromium/NativeMessagingHosts",
			".config/BraveSoftware/Brave-Browser/NativeMessagingHosts",
			".config/microsoft-edge/NativeMessagingHosts",
			".config/vivaldi/NativeMessagingHosts",
			".mozilla/native-messaging-hosts",
		},
		"darwin": {
			"Library/Application Support/Google/Chrome/NativeMessagingHosts",
			"Library/Application Support/Chromium/NativeMessagingHosts",
			"Library/Application Support/BraveSoftware/Brave-Browser/NativeMessagingHosts",
			"Library/Application Support/Microsoft Edge/NativeMessagingHosts",
			"Library/Application Support/Vivaldi/NativeMessagingHosts",
			"Library/Application Support/Mozilla/NativeMessagingHosts",
		},
	}
	for goos, dirs := range want {
		have := map[string]bool{}
		for _, tg := range targets(goos, "/h") {
			rel, _ := filepath.Rel("/h", tg.Dir)
			have[filepath.ToSlash(rel)] = true
			if tg.Firefox != strings.Contains(tg.Dir, "ozilla") {
				t.Errorf("%s: %s firefox=%v", goos, tg.Dir, tg.Firefox)
			}
		}
		for _, d := range dirs {
			if !have[d] {
				t.Errorf("%s: missing %s", goos, d)
			}
		}
	}
	if len(targets("windows", "/h")) != 0 {
		t.Error("windows uses the registry")
	}
	if cmds := windowsRegistry(`C:\x`, false); len(cmds) != len(windowsKeys) || !strings.Contains(cmds[0], `HKCU\Software\Google\Chrome\NativeMessagingHosts\app.raypass.native`) {
		t.Errorf("%v", cmds)
	}
}

func TestInstallUninstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("manifest folders are a Linux/macOS thing")
	}
	home := t.TempDir()
	for _, tg := range targets(runtime.GOOS, home) {
		if tg.Browser == "Chromium" || tg.Browser == "Firefox" {
			os.MkdirAll(tg.Probe, 0o755)
		}
	}
	o, err := parseInstall([]string{"--extension-id", "abcdefghijklmnopabcdefghijklmnop,nophamhaahdfdhocbagichcklnlnngdi"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	n, err := install(runtime.GOOS, home, o, "/opt/raypass-native", &out)
	if err != nil || n != 2 {
		t.Fatalf("%d %v\n%s", n, err, out.String())
	}
	for _, tg := range targets(runtime.GOOS, home) {
		b, err := os.ReadFile(filepath.Join(tg.Dir, hostName+".json"))
		if tg.Browser != "Chromium" && tg.Browser != "Firefox" {
			if err == nil {
				t.Errorf("%s written though not installed", tg.Browser)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		if m["name"] != hostName || m["path"] != "/opt/raypass-native" || m["type"] != "stdio" {
			t.Errorf("%s: %v", tg.Browser, m)
		}
		if tg.Firefox {
			if ids, _ := m["allowed_extensions"].([]any); len(ids) != 1 || ids[0] != firefoxID {
				t.Errorf("firefox: %v", m)
			}
			if m["allowed_origins"] != nil {
				t.Error("firefox manifest has allowed_origins")
			}
		} else if o, _ := m["allowed_origins"].([]any); len(o) != 2 || o[1] != "chrome-extension://"+devChromeID+"/" {
			t.Errorf("chromium: %v", m)
		}
	}
	// --all writes every browser.
	o.all = true
	if n, _ := install(runtime.GOOS, home, o, "/opt/raypass-native", io.Discard); n != len(targets(runtime.GOOS, home)) {
		t.Fatalf("--all wrote %d", n)
	}
	// uninstall removes only our file.
	other := filepath.Join(targets(runtime.GOOS, home)[0].Dir, "other.host.json")
	os.WriteFile(other, []byte("{}"), 0o644)
	if n := uninstall(runtime.GOOS, home, io.Discard); n != len(targets(runtime.GOOS, home)) {
		t.Fatalf("removed %d", n)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("uninstall removed another host's manifest")
	}
}

func TestParseInstall(t *testing.T) {
	o, err := parseInstall(nil, io.Discard)
	if err != nil || len(o.chromeIDs) != 1 || o.chromeIDs[0] != devChromeID || o.firefoxID != firefoxID {
		t.Fatalf("%+v %v", o, err)
	}
	for _, bad := range [][]string{{"--extension-id", "xyz"}, {"--extension-id", strings.Repeat("q", 32)}, {"extra"}, {"--firefox-id", `a"b`}} {
		if _, err := parseInstall(bad, io.Discard); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestInstallCmdTempHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("registry")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("SOL_DIR", "/srv/sol")
	t.Setenv("PATH", "")
	var out, errb bytes.Buffer
	if code := installCmd([]string{"--all", "--path", "/opt/rn"}, &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	c := loadConfig()
	if c.SolDir != "/srv/sol" {
		t.Fatalf("config %+v", c)
	}
	p, _ := configPath()
	if st, err := os.Stat(p); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("config file %v %v", st, err)
	}
	if code := uninstallCmd(nil, &out, &errb); code != 0 {
		t.Fatal(code)
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("config left behind")
	}
}
