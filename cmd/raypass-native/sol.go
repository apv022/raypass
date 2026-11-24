package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// config is what `raypass-native install` remembered about the person's Sol, because a
// browser started from the desktop often has neither the shell's PATH nor its SOL_DIR.
// The environment always wins over it.
type config struct {
	SolBin string `json:"sol_bin,omitempty"`
	SolDir string `json:"sol_dir,omitempty"`

	getenv func(string) string // os.Getenv; tests swap it
}

func (c config) env(k string) string {
	if c.getenv != nil {
		return c.getenv(k)
	}
	return os.Getenv(k)
}

// solDir is the SOL_DIR the host passes to sol ("" lets sol use its default: ~/.local/share/sol
// on Linux, ~/Library/Application Support/Sol on macOS, %LOCALAPPDATA%\Sol on Windows).
func (c config) solDir() string {
	if d := c.env("SOL_DIR"); d != "" {
		return d
	}
	return c.SolDir
}

// configPath is where install keeps the config (not a secret: two paths).
func configPath() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "raypass", "native.json"), nil
}

// loadConfig reads the config; a missing or broken one is an empty config.
func loadConfig() config {
	var c config
	if p, err := configPath(); err == nil {
		if b, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(b, &c)
		}
	}
	return c
}

// findSol returns the sol binary to run: $SOL_BIN, the installed config, PATH, then the usual
// install locations (go install, ~/.local/bin, Homebrew, /usr/local/bin, Windows' per-user
// Programs folder).
func findSol(c config) (string, error) {
	if b := c.env("SOL_BIN"); b != "" {
		return b, nil
	}
	if c.SolBin != "" && isExec(c.SolBin) {
		return c.SolBin, nil
	}
	if p, err := exec.LookPath("sol"); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			return abs, nil
		}
		return p, nil
	}
	for _, p := range solCandidates(c) {
		if isExec(p) {
			return p, nil
		}
	}
	return "", errors.New("sol not found")
}

func solCandidates(c config) []string {
	exe := "sol"
	if runtime.GOOS == "windows" {
		exe = "sol.exe"
	}
	home, _ := os.UserHomeDir()
	var out []string
	add := func(parts ...string) {
		if parts[0] != "" {
			out = append(out, filepath.Join(append(parts, exe)...))
		}
	}
	add(home, ".local", "bin")
	if gp := c.env("GOPATH"); gp != "" {
		add(strings.Split(gp, string(os.PathListSeparator))[0], "bin")
	}
	add(home, "go", "bin")
	add(home, "bin")
	switch runtime.GOOS {
	case "windows":
		add(c.env("LOCALAPPDATA"), "Programs", "Sol")
		add(c.env("LOCALAPPDATA"), "Sol")
		add(c.env("ProgramFiles"), "Sol")
	case "darwin":
		add("/opt/homebrew/bin")
		add("/usr/local/bin")
		add("/Applications/Sol.app/Contents/MacOS")
	default:
		add("/usr/local/bin")
		add("/usr/bin")
	}
	return out
}

func isExec(p string) bool {
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return false
	}
	return runtime.GOOS == "windows" || st.Mode()&0o111 != 0
}

// solRunner runs actions with `sol do`.
type solRunner struct{ cfg config }

// limitedBuffer keeps at most max bytes and remembers that it overflowed.
type limitedBuffer struct {
	bytes.Buffer
	max  int
	over bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.Len(); len(p) > room {
		b.over = true
		if room > 0 {
			b.Buffer.Write(p[:room])
		}
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

// run runs `sol do <action> --args - --json` with args on stdin. Arguments never go on the
// command line: other users can read a process's argv, and passwords are arguments here.
func (s *solRunner) run(ctx context.Context, action string, args []byte) (json.RawMessage, *hostError) {
	bin, err := findSol(s.cfg)
	if err != nil {
		return nil, errf("sol_not_found", "the sol command was not found (set SOL_BIN, or run raypass-native install after installing Sol)")
	}
	cmd := exec.CommandContext(ctx, bin, "do", action, "--args", "-", "--json")
	cmd.Env = os.Environ()
	if d := s.cfg.solDir(); d != "" {
		cmd.Env = append(cmd.Env, "SOL_DIR="+d)
	}
	cmd.Stdin = bytes.NewReader(args)
	stdout := &limitedBuffer{max: maxOut + 1}
	stderr := &limitedBuffer{max: 16 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = 2 * time.Second
	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, errf("timeout", "sol did not answer in time")
	}
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			return nil, errf("sol_not_found", "could not start sol: %s", truncate(firstLine(err.Error()), 200))
		}
		code = ee.ExitCode()
	}
	return solResult(code, stdout, stderr)
}

// solResult turns sol's exit code and output into a result or an error. The exit codes are
// sol do's: 0 done, 1 failed, 2 bad arguments, 3 waiting for approval.
func solResult(code int, stdout, stderr *limitedBuffer) (json.RawMessage, *hostError) {
	switch code {
	case 0:
		if stdout.over {
			return nil, errf("too_large", "the answer is over the browser's 1 MB limit")
		}
		out := bytes.TrimSpace(stdout.Bytes())
		if len(out) == 0 || !json.Valid(out) {
			return nil, errf("internal", "sol did not answer with JSON")
		}
		return json.RawMessage(out), nil
	case 3:
		var ap struct {
			Approval string `json:"approval"`
		}
		_ = json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &ap)
		msg := "this needs your approval in Sol"
		if ap.Approval != "" && idRe.MatchString(ap.Approval) {
			msg += ": sol inbox approve " + ap.Approval
		}
		return nil, &hostError{Code: "approval_required", Message: msg}
	}
	e := parseSolError(stderr.String())
	if code == 2 && e.Code == "internal" {
		e.Code = "bad_request"
		if strings.HasPrefix(e.Message, "no action ") {
			e.Code, e.Message = "not_installed", "Raypass is not installed in Sol (sol app install)"
		}
	}
	return nil, e
}

var (
	codeRe = regexp.MustCompile(`^[a-z][a-z_]{0,40}$`)
	idRe   = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)
)

// parseSolError reads the error sol do printed: "sol do: sol: <code>: <message>" for an error
// from the daemon (forbidden: locked while the vault is locked, passed through unchanged so the
// extension shows its unlock screen), "sol do: <text>" for one of its own.
func parseSolError(stderr string) *hostError {
	line := ""
	for _, l := range strings.Split(stderr, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			line = l
		}
	}
	line = strings.TrimPrefix(line, "sol do: ")
	if strings.Contains(line, "is not running") {
		return &hostError{Code: "unavailable", Message: "Sol is not running (start it with: sol)"}
	}
	if rest, ok := strings.CutPrefix(line, "sol: "); ok {
		code, msg, _ := strings.Cut(rest, ": ")
		if codeRe.MatchString(code) {
			return &hostError{Code: code, Message: truncate(msg, 300)}
		}
	}
	if line == "" {
		line = "sol failed"
	}
	return &hostError{Code: "internal", Message: truncate(line, 300)}
}

func firstLine(s string) string { l, _, _ := strings.Cut(s, "\n"); return l }
