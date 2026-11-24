package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// The test binary doubles as a fake `sol` (RAYPASS_FAKE_SOL=1): cross-platform, nothing to
// build, and it checks the host's calling convention from the other side.
func TestMain(m *testing.M) {
	if os.Getenv("RAYPASS_FAKE_SOL") == "1" {
		os.Exit(fakeSol(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fakeSol(argv []string) int {
	if len(argv) != 5 || argv[0] != "do" || argv[2] != "--args" || argv[3] != "-" || argv[4] != "--json" {
		fmt.Fprintln(os.Stderr, "sol do: fake: bad argv", argv)
		return 2
	}
	in, _ := io.ReadAll(os.Stdin)
	var args map[string]any
	if err := json.Unmarshal(in, &args); err != nil {
		fmt.Fprintln(os.Stderr, "sol do: --args is not a JSON object")
		return 2
	}
	switch argv[1] {
	case "raypass.status":
		fmt.Printf(`{"initialized":true,"locked":false,"sol_dir":%q}`+"\n", os.Getenv("SOL_DIR"))
	case "raypass.unlock":
		if args["password"] != "correct horse" {
			fmt.Fprintln(os.Stderr, "sol do: sol: unauthorized: wrong password")
			return 1
		}
		fmt.Println(`{"ok":true}`)
	case "raypass.match":
		fmt.Fprintln(os.Stderr, "sol do: sol: forbidden: locked")
		return 1
	case "raypass.get":
		os.Stdout.Write(in) // echo: proves the args arrived on stdin
	case "raypass.totp":
		fmt.Println(`{"approval":"ap_42"}`)
		return 3
	case "raypass.list":
		fmt.Print(`["` + strings.Repeat("x", 2<<20) + `"]`)
	case "raypass.fill":
		time.Sleep(10 * time.Second)
	case "raypass.lock":
		fmt.Fprintln(os.Stderr, "sol do: daemon: sol is not running here (start it with sol run)")
		return 1
	case "raypass.generate":
		fmt.Fprintln(os.Stderr, `sol do: no action "raypass.generate" here (sol do --help raypass)`)
		return 2
	default:
		fmt.Println("not json")
	}
	return 0
}

func fakeConfig(t *testing.T, extra map[string]string) config {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("RAYPASS_FAKE_SOL", "1") // inherited by the child through os.Environ
	env := map[string]string{"SOL_BIN": exe}
	for k, v := range extra {
		env[k] = v
	}
	return config{getenv: func(k string) string { return env[k] }}
}

func frame(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := writeFrame(&buf, b); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// session runs the host over the given requests and returns the answers by id.
func session(t *testing.T, h *host, reqs ...any) map[string]response {
	t.Helper()
	var in bytes.Buffer
	for _, r := range reqs {
		in.Write(frame(t, r))
	}
	var out bytes.Buffer
	if err := h.serve(&in, &out); err != nil {
		t.Fatal(err)
	}
	got := map[string]response{}
	for {
		b, err := readFrame(&out)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		var r response
		if err := json.Unmarshal(b, &r); err != nil {
			t.Fatal(err)
		}
		got[string(r.ID)] = r
	}
	return got
}

func TestFraming(t *testing.T) {
	var buf bytes.Buffer
	if err := writeFrame(&buf, []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if n := binary.NativeEndian.Uint32(buf.Bytes()[:4]); n != 7 {
		t.Fatalf("length %d", n)
	}
	b, err := readFrame(&buf)
	if err != nil || string(b) != `{"a":1}` {
		t.Fatalf("%q %v", b, err)
	}
	if _, err := readFrame(&buf); err != io.EOF {
		t.Fatalf("want EOF, got %v", err)
	}
	if err := writeFrame(io.Discard, make([]byte, maxOut+1)); err == nil {
		t.Fatal("over-limit frame written")
	}
	var big [4]byte
	binary.NativeEndian.PutUint32(big[:], maxIn+1)
	if _, err := readFrame(bytes.NewReader(big[:])); err == nil {
		t.Fatal("over-limit frame read")
	}
	if _, err := readFrame(bytes.NewReader([]byte{5, 0})); err == nil || err == io.EOF {
		t.Fatalf("truncated length: %v", err)
	}
	var short bytes.Buffer
	binary.Write(&short, binary.NativeEndian, uint32(10))
	short.WriteString("abc")
	if _, err := readFrame(&short); err == nil {
		t.Fatal("truncated body read")
	}
}

type recordRunner struct {
	mu    sync.Mutex
	calls []string
}

func (r *recordRunner) run(_ context.Context, action string, args []byte) (json.RawMessage, *hostError) {
	r.mu.Lock()
	r.calls = append(r.calls, action)
	r.mu.Unlock()
	return json.RawMessage(args), nil
}

func TestAllowlist(t *testing.T) {
	rr := &recordRunner{}
	h := &host{run: rr}
	var reqs []any
	for a := range allowedActions {
		reqs = append(reqs, map[string]any{"id": a, "action": a, "args": map[string]any{"x": 1}})
	}
	for _, a := range []string{"raypass.export", "raypass.purge", "raypass.setup", "raypass.change_password", "files.list", "", "raypass.status;rm"} {
		reqs = append(reqs, map[string]any{"id": "bad:" + a, "action": a})
	}
	got := session(t, h, reqs...)
	for a := range allowedActions {
		r := got[`"`+a+`"`]
		if !r.OK || string(r.Result) != `{"x":1}` {
			t.Errorf("%s: %+v", a, r)
		}
	}
	for k, r := range got {
		if strings.HasPrefix(k, `"bad:`) && (r.OK || r.Error.Code != "forbidden") {
			t.Errorf("%s forwarded: %+v", k, r)
		}
	}
	if len(rr.calls) != len(allowedActions) {
		t.Fatalf("ran %v", rr.calls)
	}
}

func TestBadRequests(t *testing.T) {
	h := &host{run: &recordRunner{}}
	var in bytes.Buffer
	writeFrame(&in, []byte(`not json`))
	writeFrame(&in, []byte(`{"id":7,"action":"raypass.get","args":[1]}`))
	writeFrame(&in, []byte(`{"id":8,"action":"raypass.status"}`))
	var out bytes.Buffer
	if err := h.serve(&in, &out); err != nil {
		t.Fatal(err)
	}
	codes := map[string]string{}
	for {
		b, err := readFrame(&out)
		if err != nil {
			break
		}
		var r response
		json.Unmarshal(b, &r)
		if r.Error != nil {
			codes[string(r.ID)] = r.Error.Code
		} else {
			codes[string(r.ID)] = "ok:" + string(r.Result)
		}
	}
	want := map[string]string{"null": "bad_request", "7": "bad_request", "8": "ok:{}"}
	for k, v := range want {
		if codes[k] != v {
			t.Errorf("id %s: %q, want %q", k, codes[k], v)
		}
	}
}

func TestPing(t *testing.T) {
	h := newHost(fakeConfig(t, map[string]string{"SOL_DIR": "/x/sol"}))
	r := session(t, h, map[string]any{"id": 1, "action": "ping"})["1"]
	var p pingResult
	json.Unmarshal(r.Result, &p)
	if !r.OK || p.Host != "raypass-native" || p.Sol == "" || p.SolDir != "/x/sol" {
		t.Fatalf("%+v %+v", r, p)
	}
}

func TestExecSol(t *testing.T) {
	cfg := fakeConfig(t, map[string]string{"SOL_DIR": "/tmp/fake-sol-dir"})
	h := newHost(cfg)
	old := allowedActions["raypass.fill"]
	allowedActions["raypass.fill"] = 300 * time.Millisecond
	defer func() { allowedActions["raypass.fill"] = old }()

	got := session(t, h,
		map[string]any{"id": "status", "action": "raypass.status"},
		map[string]any{"id": "unlock-ok", "action": "raypass.unlock", "args": map[string]any{"password": "correct horse"}},
		map[string]any{"id": "unlock-bad", "action": "raypass.unlock", "args": map[string]any{"password": "nope"}},
		map[string]any{"id": "locked", "action": "raypass.match", "args": map[string]any{"url": "https://a.example"}},
		map[string]any{"id": "echo", "action": "raypass.get", "args": map[string]any{"id": "0123456789abcdef"}},
		map[string]any{"id": "approval", "action": "raypass.totp", "args": map[string]any{"id": "x"}},
		map[string]any{"id": "big", "action": "raypass.list"},
		map[string]any{"id": "slow", "action": "raypass.fill"},
		map[string]any{"id": "down", "action": "raypass.lock"},
		map[string]any{"id": "missing", "action": "raypass.generate"},
		map[string]any{"id": "junk", "action": "raypass.save_login"},
	)
	ok := func(id, want string) {
		t.Helper()
		r := got[`"`+id+`"`]
		if !r.OK || string(r.Result) != want {
			t.Errorf("%s: %+v (result %s)", id, r, r.Result)
		}
	}
	fail := func(id, code, msg string) {
		t.Helper()
		r := got[`"`+id+`"`]
		if r.OK || r.Error == nil || r.Error.Code != code || (msg != "" && r.Error.Message != msg) {
			t.Errorf("%s: %+v %+v", id, r, r.Error)
		}
	}
	ok("status", `{"initialized":true,"locked":false,"sol_dir":"/tmp/fake-sol-dir"}`)
	ok("unlock-ok", `{"ok":true}`)
	fail("unlock-bad", "unauthorized", "wrong password")
	fail("locked", "forbidden", "locked")
	ok("echo", `{"id":"0123456789abcdef"}`)
	fail("approval", "approval_required", "this needs your approval in Sol: sol inbox approve ap_42")
	fail("big", "too_large", "")
	fail("slow", "timeout", "")
	fail("down", "unavailable", "")
	fail("missing", "not_installed", "")
	fail("junk", "internal", "sol did not answer with JSON")
}

func TestSolNotFound(t *testing.T) {
	h := newHost(config{getenv: func(k string) string {
		if k == "SOL_BIN" {
			return "/nonexistent/sol"
		}
		return ""
	}})
	r := session(t, h, map[string]any{"id": 1, "action": "raypass.status"})["1"]
	if r.OK || r.Error.Code != "sol_not_found" {
		t.Fatalf("%+v", r.Error)
	}
}

func TestParseSolError(t *testing.T) {
	for in, want := range map[string]hostError{
		"sol do: sol: forbidden: locked\n":     {"forbidden", "locked"},
		"sol do: sol: not_found\n":             {"not_found", ""},
		"warning\nsol do: sol: conflict: x: y": {"conflict", "x: y"},
		"sol do: something odd":                {"internal", "something odd"},
		"":                                     {"internal", "sol failed"},
		"sol do: sol: Bad Code: x":             {"internal", "sol: Bad Code: x"},
	} {
		if got := parseSolError(in); *got != want {
			t.Errorf("%q: %+v, want %+v", in, *got, want)
		}
	}
}
