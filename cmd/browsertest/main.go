// Command browsertest runs Raypass's browser JavaScript test suites in a real headless browser,
// with no Node.js. It serves the repository root (plus /kit/ from Sol's ui/kit and /sol.js from
// Sol's sdk/js, the same paths the app serves), opens each suite's test/index.html in headless
// Chromium (one at a time, fresh profile each), and waits for the page to POST its result to
// /__result (Sol's kit/testing.js does this). It exits non-zero if any suite fails or doesn't
// report in time. Same result protocol as Sol's own cmd/browsertest.
//
//	go run ./cmd/browsertest            # every */test/index.html under the repo
//	go run ./cmd/browsertest ui/test    # selected suites
//
// The browser is $RAYPASS_BROWSER, else Playwright's Chromium under $PLAYWRIGHT_BROWSERS_PATH
// or /opt/pw-browsers, else chromium, chromium-browser, google-chrome or chrome on PATH.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/apv022/sol/sdk/js"
	"github.com/apv022/sol/ui/kit"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "browsertest:", err)
		os.Exit(1)
	}
}

// browser finds a Chromium-family binary.
func browser() (string, error) {
	if b := os.Getenv("RAYPASS_BROWSER"); b != "" {
		return b, nil
	}
	roots := []string{os.Getenv("PLAYWRIGHT_BROWSERS_PATH"), "/opt/pw-browsers"}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".cache/ms-playwright"))
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		if p := filepath.Join(root, "chromium"); isFile(p) {
			return p, nil
		}
		// Newest version first: chromium-1243/chrome-linux64/chrome, chrome-linux/chrome.
		matches, _ := filepath.Glob(filepath.Join(root, "chromium-*", "chrome-*", "chrome"))
		sort.Sort(sort.Reverse(sort.StringSlice(matches)))
		for _, m := range matches {
			if isFile(m) {
				return m, nil
			}
		}
	}
	names := []string{"chromium", "chromium-browser", "google-chrome", "chrome"}
	if runtime.GOOS == "darwin" {
		names = append(names, "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Chromium.app/Contents/MacOS/Chromium")
	}
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return p, nil
		}
	}
	return "", errors.New("no Chromium found")
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// suites lists the directories named test that hold an index.html.
func suites(args []string) ([]string, error) {
	if len(args) > 0 {
		return args, nil
	}
	var out []string
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "bin" || d.Name() == "build" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == "index.html" && filepath.Base(filepath.Dir(p)) == "test" {
			out = append(out, filepath.Dir(p))
		}
		return nil
	})
	return out, err
}

// handler serves the repository with the app origin's style policy, the kit and sol.js.
func handler(results chan<- string) http.Handler {
	mux := http.NewServeMux()
	files := http.FileServer(http.Dir("."))
	// The style policy of every Sol UI origin, so a suite that renders an inline style
	// attribute fails (testing.js reports securitypolicyviolation).
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "style-src 'self'")
		w.Header().Set("Cache-Control", "no-store")
		files.ServeHTTP(w, r)
	}))
	mux.Handle("GET /kit/", http.StripPrefix("/kit/", http.FileServerFS(kit.FS)))
	mux.HandleFunc("GET /sol.js", func(w http.ResponseWriter, r *http.Request) {
		b, err := js.FS.ReadFile("sol.js")
		if err != nil {
			http.Error(w, "sol.js missing", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("POST /__result", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		select {
		case results <- string(b):
		default:
		}
	})
	return mux
}

func run(args []string) error {
	bin, err := browser()
	if err != nil {
		return fmt.Errorf("%w (set RAYPASS_BROWSER)", err)
	}
	list, err := suites(args)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return errors.New("no */test/index.html found")
	}
	results := make(chan string, 1)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: handler(results), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	failed := 0
	for _, s := range list {
		res, err := runSuite(bin, ln.Addr().String(), filepath.ToSlash(s), results)
		switch {
		case err != nil:
			failed++
			fmt.Printf("FAIL %s: %v\n", s, err)
		case strings.HasPrefix(res, "PASS"):
			fmt.Printf("%s  %s\n", firstLine(res), s)
		default:
			failed++
			fmt.Printf("%s  %s\n%s\n", firstLine(res), s, indent(res))
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d suites failed", failed, len(list))
	}
	return nil
}

func runSuite(bin, addr, suite string, results chan string) (string, error) {
	profile, err := os.MkdirTemp("", "browsertest-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(profile)
	// Drain a late result from a previous suite that timed out.
	select {
	case <-results:
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	url := "http://" + addr + "/" + strings.TrimPrefix(suite, "./") + "/index.html"
	cmd := exec.CommandContext(ctx, bin, "--headless", "--no-sandbox", "--disable-gpu",
		"--no-first-run", "--no-default-browser-check", "--disable-extensions",
		"--user-data-dir="+profile, url)
	if err := cmd.Start(); err != nil {
		return "", err
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	select {
	case r := <-results:
		return r, nil
	case <-ctx.Done():
		return "", fmt.Errorf("no result within 60s")
	}
}

func firstLine(s string) string { l, _, _ := strings.Cut(s, "\n"); return l }

func indent(s string) string {
	_, rest, _ := strings.Cut(s, "\n")
	return "    " + strings.ReplaceAll(rest, "\n", "\n    ")
}
