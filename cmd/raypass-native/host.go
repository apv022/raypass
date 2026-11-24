package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

const (
	// maxOut is the browsers' limit for a message from the host (Chrome and Firefox: 1 MB).
	maxOut = 1 << 20
	// maxIn bounds a request. The browsers allow far more (64 MiB / 4 GiB), but no request of
	// the forwarded actions comes near this; a bigger frame means a confused or hostile peer.
	maxIn = 1 << 20
	// maxParallel bounds the sol processes one session runs at once.
	maxParallel = 4
)

// allowedActions are the only actions the host forwards (design/DESIGN.md "Browser
// extension"), with how long each may take. unlock is slow on purpose: PBKDF2 at 600 000 rounds
// and, after repeated failures, the vault's own back-off of up to 60 s.
var allowedActions = map[string]time.Duration{
	"raypass.status":     15 * time.Second,
	"raypass.unlock":     90 * time.Second,
	"raypass.lock":       15 * time.Second,
	"raypass.match":      20 * time.Second,
	"raypass.fill":       20 * time.Second,
	"raypass.save_login": 20 * time.Second,
	"raypass.generate":   15 * time.Second,
	"raypass.totp":       15 * time.Second,
	"raypass.list":       20 * time.Second,
	"raypass.get":        20 * time.Second,
}

// pingAction is answered by the host itself (no sol involved): the options page uses it to tell
// "host not installed" from "Sol not running".
const pingAction = "ping"

// request is one message from the extension.
type request struct {
	ID     json.RawMessage `json:"id"`
	Action string          `json:"action"`
	Args   json.RawMessage `json:"args"`
}

// response is one message to the extension: Result when OK, else Error.
type response struct {
	ID     json.RawMessage `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *hostError      `json:"error,omitempty"`
}

// hostError is the {"code","message"} of a failed request, the same shape as Sol's errors.
type hostError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *hostError) Error() string { return e.Code + ": " + e.Message }

func errf(code, format string, a ...any) *hostError {
	return &hostError{Code: code, Message: fmt.Sprintf(format, a...)}
}

// runner runs one action through sol and returns its JSON result. The real one is solRunner;
// tests swap in fakes.
type runner interface {
	run(ctx context.Context, action string, args []byte) (json.RawMessage, *hostError)
}

// host is one browser session: it reads requests until stdin closes.
type host struct {
	cfg config
	run runner

	wmu sync.Mutex // one frame at a time on stdout
}

func newHost(cfg config) *host {
	return &host{cfg: cfg, run: &solRunner{cfg: cfg}}
}

// readFrame reads one native messaging frame. io.EOF means the browser closed the port.
func readFrame(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, fmt.Errorf("truncated length")
		}
		return nil, err
	}
	n := binary.NativeEndian.Uint32(hdr[:])
	if n > maxIn {
		return nil, fmt.Errorf("frame of %d bytes is over the %d byte limit", n, maxIn)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, fmt.Errorf("truncated frame: %w", err)
	}
	return b, nil
}

// writeFrame writes one frame. Messages over maxOut are refused: the browser would drop the
// whole connection.
func writeFrame(w io.Writer, b []byte) error {
	if len(b) > maxOut {
		return fmt.Errorf("message of %d bytes is over the %d byte limit", len(b), maxOut)
	}
	buf := make([]byte, 4+len(b))
	binary.NativeEndian.PutUint32(buf, uint32(len(b)))
	copy(buf[4:], b)
	_, err := w.Write(buf)
	return err
}

// serve answers requests until r ends. Requests run concurrently (the popup asks for the
// status and the matches at once); answers carry the request's id so order does not matter.
func (h *host) serve(r io.Reader, w io.Writer) error {
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		b, err := readFrame(r)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			h.send(w, h.handle(b))
		}()
	}
}

// send writes resp, replacing it with an error answer when it is too big for the browser.
func (h *host) send(w io.Writer, resp response) {
	b, err := json.Marshal(resp)
	if err == nil && len(b) > maxOut {
		b, err = json.Marshal(response{ID: resp.ID, Error: errf("too_large", "the answer is over the browser's 1 MB limit")})
	}
	if err != nil {
		logf("encode: %v", err)
		return
	}
	h.wmu.Lock()
	defer h.wmu.Unlock()
	if err := writeFrame(w, b); err != nil {
		logf("write: %v", err)
	}
}

// handle turns one frame into its answer.
func (h *host) handle(frame []byte) response {
	var req request
	dec := json.NewDecoder(bytes.NewReader(frame))
	if err := dec.Decode(&req); err != nil {
		return response{ID: json.RawMessage("null"), Error: errf("bad_request", "request is not a JSON object")}
	}
	if len(req.ID) == 0 {
		req.ID = json.RawMessage("null")
	}
	fail := func(e *hostError) response { return response{ID: req.ID, Error: e} }
	if req.Action == pingAction {
		res, _ := json.Marshal(h.ping())
		return response{ID: req.ID, OK: true, Result: res}
	}
	timeout, ok := allowedActions[req.Action]
	if !ok {
		logf("refused action %q", truncate(req.Action, 64))
		return fail(errf("forbidden", "action not allowed through the browser extension"))
	}
	args, herr := normalizeArgs(req.Args)
	if herr != nil {
		return fail(herr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	res, herr := h.run.run(ctx, req.Action, args)
	if herr != nil {
		logf("%s: %s", req.Action, herr.Code)
		return fail(herr)
	}
	return response{ID: req.ID, OK: true, Result: res}
}

// normalizeArgs checks that args is a JSON object (absent or null become {}).
func normalizeArgs(raw json.RawMessage) ([]byte, *hostError) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || bytes.Equal(t, []byte("null")) {
		return []byte("{}"), nil
	}
	var obj map[string]json.RawMessage
	if t[0] != '{' || json.Unmarshal(t, &obj) != nil {
		return nil, errf("bad_request", "args must be a JSON object")
	}
	return t, nil
}

// pingResult is what the "ping" action answers.
type pingResult struct {
	Host    string `json:"host"`
	Version string `json:"version"`
	Sol     string `json:"sol"`               // the sol binary the host would run, "" if none found
	SolDir  string `json:"sol_dir,omitempty"` // SOL_DIR passed to sol, if any
}

func (h *host) ping() pingResult {
	bin, _ := findSol(h.cfg)
	return pingResult{Host: "raypass-native", Version: version, Sol: bin, SolDir: h.cfg.solDir()}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
