package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// hostName is the native messaging host name the extension connects to.
const hostName = "app.raypass.native"

// devChromeID is the ID of the extension when loaded unpacked or from the zip: it follows from
// the public key in extension/manifest.json (cmd/extpack checks that they agree).
const devChromeID = "nophamhaahdfdhocbagichcklnlnngdi"

// firefoxID is browser_specific_settings.gecko.id of the Firefox build.
const firefoxID = "raypass@raypass.app"

var chromeIDRe = regexp.MustCompile(`^[a-p]{32}$`)

// target is one browser's place for host manifests.
type target struct {
	Browser string
	Dir     string // the NativeMessagingHosts folder
	Probe   string // the browser's profile folder: present means the browser is installed
	Firefox bool   // Firefox's manifest format (allowed_extensions)
}

// targets lists where each browser looks for user-level host manifests on goos. Windows uses
// the registry instead (windowsRegistry).
func targets(goos, home string) []target {
	var t []target
	switch goos {
	case "darwin":
		as := filepath.Join(home, "Library", "Application Support")
		for _, b := range []struct{ name, dir string }{
			{"Chrome", "Google/Chrome"},
			{"Chrome Beta", "Google/Chrome Beta"},
			{"Chromium", "Chromium"},
			{"Brave", "BraveSoftware/Brave-Browser"},
			{"Edge", "Microsoft Edge"},
			{"Vivaldi", "Vivaldi"},
		} {
			p := filepath.Join(as, filepath.FromSlash(b.dir))
			t = append(t, target{Browser: b.name, Dir: filepath.Join(p, "NativeMessagingHosts"), Probe: p})
		}
		t = append(t, target{Browser: "Firefox", Dir: filepath.Join(as, "Mozilla", "NativeMessagingHosts"),
			Probe: filepath.Join(as, "Firefox"), Firefox: true})
	case "windows":
		return nil
	default: // Linux and the BSDs
		cfg := filepath.Join(home, ".config")
		for _, b := range []struct{ name, dir string }{
			{"Chrome", "google-chrome"},
			{"Chrome Beta", "google-chrome-beta"},
			{"Chromium", "chromium"},
			{"Brave", "BraveSoftware/Brave-Browser"},
			{"Edge", "microsoft-edge"},
			{"Vivaldi", "vivaldi"},
		} {
			p := filepath.Join(cfg, filepath.FromSlash(b.dir))
			t = append(t, target{Browser: b.name, Dir: filepath.Join(p, "NativeMessagingHosts"), Probe: p})
		}
		t = append(t, target{Browser: "Firefox", Dir: filepath.Join(home, ".mozilla", "native-messaging-hosts"),
			Probe: filepath.Join(home, ".mozilla"), Firefox: true})
	}
	return t
}

// chromeManifest and firefoxManifest are the two host manifest formats.
type chromeManifest struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Path           string   `json:"path"`
	Type           string   `json:"type"`
	AllowedOrigins []string `json:"allowed_origins"`
}

type firefoxManifest struct {
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Path              string   `json:"path"`
	Type              string   `json:"type"`
	AllowedExtensions []string `json:"allowed_extensions"`
}

const description = "Raypass browser extension host (runs sol do raypass.*)"

func manifestJSON(firefox bool, path string, chromeIDs []string, ffID string) []byte {
	var v any
	if firefox {
		v = firefoxManifest{hostName, description, path, "stdio", []string{ffID}}
	} else {
		origins := make([]string, len(chromeIDs))
		for i, id := range chromeIDs {
			origins[i] = "chrome-extension://" + id + "/"
		}
		v = chromeManifest{hostName, description, path, "stdio", origins}
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	return append(b, '\n')
}

// writeAtomic writes b to p through a temporary file and a rename.
func writeAtomic(p string, b []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil && runtime.GOOS != "windows" {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), p)
}

// listFlag is a repeatable, comma-separated string flag.
type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }
func (l *listFlag) Set(s string) error {
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			*l = append(*l, v)
		}
	}
	return nil
}

// installOptions are the parsed flags of install.
type installOptions struct {
	chromeIDs []string
	firefoxID string
	path      string
	all       bool
}

func parseInstall(args []string, stderr io.Writer) (installOptions, error) {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var ids listFlag
	o := installOptions{}
	fs.Var(&ids, "extension-id", "Chrome-family extension ID to allow (repeatable; default the Raypass ID "+devChromeID+")")
	fs.StringVar(&o.firefoxID, "firefox-id", firefoxID, "Firefox add-on ID to allow")
	fs.StringVar(&o.path, "path", "", "the host binary the manifests point to (default: this binary)")
	fs.BoolVar(&o.all, "all", false, "write manifests for every browser, not only the installed ones")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if fs.NArg() > 0 {
		return o, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if len(ids) == 0 {
		ids = listFlag{devChromeID}
	}
	for _, id := range ids {
		if !chromeIDRe.MatchString(id) {
			return o, fmt.Errorf("%q is not a Chrome extension ID (32 letters a-p)", id)
		}
	}
	o.chromeIDs = ids
	if o.firefoxID == "" || strings.ContainsAny(o.firefoxID, "\"\\ \t\n") {
		return o, errors.New("bad --firefox-id")
	}
	return o, nil
}

func hostBinary(o installOptions) (string, error) {
	p := o.path
	if p == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		p = exe
	}
	p, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return p, nil
}

func installCmd(args []string, stdout, stderr io.Writer) int {
	o, err := parseInstall(args, stderr)
	if err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stderr, "raypass-native install:", err)
		}
		return 2
	}
	bin, err := hostBinary(o)
	if err != nil {
		fmt.Fprintln(stderr, "raypass-native install:", err)
		return 1
	}
	if strings.Contains(bin, "go-build") {
		fmt.Fprintln(stderr, "warning: this binary is a temporary `go run` build; install with `go install` or `make build` and run that copy")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, "raypass-native install:", err)
		return 1
	}
	if err := saveConfig(stderr); err != nil {
		fmt.Fprintln(stderr, "raypass-native install: config:", err)
		return 1
	}
	if runtime.GOOS == "windows" {
		return installWindows(o, bin, stdout, stderr)
	}
	n, err := install(runtime.GOOS, home, o, bin, stdout)
	if err != nil {
		fmt.Fprintln(stderr, "raypass-native install:", err)
		return 1
	}
	if n == 0 {
		fmt.Fprintln(stderr, "no supported browser profile found; run again with --all to write the manifests anyway")
		return 1
	}
	return 0
}

// install writes the manifests for goos under home and reports each one on out. It returns how
// many it wrote.
func install(goos, home string, o installOptions, bin string, out io.Writer) (int, error) {
	n := 0
	for _, t := range targets(goos, home) {
		if !o.all {
			if _, err := os.Stat(t.Probe); err != nil {
				fmt.Fprintf(out, "skip   %-11s (not installed)\n", t.Browser)
				continue
			}
		}
		p := filepath.Join(t.Dir, hostName+".json")
		if err := writeAtomic(p, manifestJSON(t.Firefox, bin, o.chromeIDs, o.firefoxID), 0o644); err != nil {
			return n, fmt.Errorf("%s: %w", t.Browser, err)
		}
		fmt.Fprintf(out, "wrote  %-11s %s\n", t.Browser, p)
		n++
	}
	return n, nil
}

// saveConfig remembers the sol binary and SOL_DIR seen now, for browsers started without them.
func saveConfig(stderr io.Writer) error {
	c := config{SolDir: os.Getenv("SOL_DIR")}
	if bin, err := findSol(config{}); err == nil {
		c.SolBin = bin
	} else {
		fmt.Fprintln(stderr, "warning: sol not found yet; the host will look for it again each time (set SOL_BIN)")
	}
	p, err := configPath()
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return writeAtomic(p, append(b, '\n'), 0o600)
}

// windowsKeys are the registry keys (under HKCU) where Chrome-family browsers and Firefox look
// up host manifests on Windows.
var windowsKeys = []struct {
	browser, key string
	firefox      bool
}{
	{"Chrome", `Software\Google\Chrome\NativeMessagingHosts`, false},
	{"Chromium", `Software\Chromium\NativeMessagingHosts`, false},
	{"Brave", `Software\BraveSoftware\Brave-Browser\NativeMessagingHosts`, false},
	{"Edge", `Software\Microsoft\Edge\NativeMessagingHosts`, false},
	{"Vivaldi", `Software\Vivaldi\NativeMessagingHosts`, false},
	{"Firefox", `Software\Mozilla\NativeMessagingHosts`, true},
}

// windowsRegistry returns the reg.exe commands that register (or, with del, unregister) the
// manifests in dir.
func windowsRegistry(dir string, del bool) []string {
	var cmds []string
	for _, k := range windowsKeys {
		key := `HKCU\` + k.key + `\` + hostName
		if del {
			cmds = append(cmds, fmt.Sprintf(`reg delete "%s" /f`, key))
			continue
		}
		file := "chrome.json"
		if k.firefox {
			file = "firefox.json"
		}
		cmds = append(cmds, fmt.Sprintf(`reg add "%s" /ve /t REG_SZ /d "%s" /f`, key, filepath.Join(dir, file)))
	}
	return cmds
}

func windowsDir() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		d, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		base = d
	}
	return filepath.Join(base, "Raypass", "NativeMessagingHosts"), nil
}

func installWindows(o installOptions, bin string, stdout, stderr io.Writer) int {
	dir, err := windowsDir()
	if err != nil {
		fmt.Fprintln(stderr, "raypass-native install:", err)
		return 1
	}
	for _, f := range []struct {
		name    string
		firefox bool
	}{{"chrome.json", false}, {"firefox.json", true}} {
		p := filepath.Join(dir, f.name)
		if err := writeAtomic(p, manifestJSON(f.firefox, bin, o.chromeIDs, o.firefoxID), 0o644); err != nil {
			fmt.Fprintln(stderr, "raypass-native install:", err)
			return 1
		}
		fmt.Fprintln(stdout, "wrote", p)
	}
	fmt.Fprintln(stdout, "\nNow register them (run in cmd.exe or PowerShell):")
	for _, c := range windowsRegistry(dir, false) {
		fmt.Fprintln(stdout, "  "+c)
	}
	return 0
}

func uninstallCmd(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintln(stderr, "raypass-native uninstall: takes no arguments")
		return 2
	}
	if p, err := configPath(); err == nil {
		_ = os.Remove(p)
	}
	if runtime.GOOS == "windows" {
		dir, err := windowsDir()
		if err == nil {
			_ = os.RemoveAll(dir)
		}
		fmt.Fprintln(stdout, "Removed the manifests. Unregister them with:")
		for _, c := range windowsRegistry(dir, true) {
			fmt.Fprintln(stdout, "  "+c)
		}
		return 0
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, "raypass-native uninstall:", err)
		return 1
	}
	uninstall(runtime.GOOS, home, stdout)
	return 0
}

// uninstall removes our manifest from every target; it touches nothing else.
func uninstall(goos, home string, out io.Writer) int {
	n := 0
	for _, t := range targets(goos, home) {
		p := filepath.Join(t.Dir, hostName+".json")
		if err := os.Remove(p); err == nil {
			fmt.Fprintf(out, "removed %-11s %s\n", t.Browser, p)
			n++
		}
	}
	if n == 0 {
		fmt.Fprintln(out, "nothing to remove")
	}
	return n
}
