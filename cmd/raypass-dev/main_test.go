package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apv022/raypass"
	"github.com/apv022/raypass/vault"
	"github.com/apv022/sol/sdk/go/sol/memsol"
)

// TestSeed: the dev vault sets up and every seed item is accepted.
func TestSeed(t *testing.T) {
	n := memsol.New()
	p := n.AddPerson("Alice")
	c := p.Devices[0].Client(raypass.AppID)
	app := raypass.New(c, raypass.Config{KDFIter: vault.MinIter})
	if err := app.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer app.Stop()
	dv := &device{p.Devices[0], app, c}
	if err := seed(dv); err != nil {
		t.Fatal(err)
	}
	st, err := dv.do("raypass.status", map[string]any{})
	if err != nil || st["items"] != float64(9) || st["trash"] != float64(1) {
		t.Fatalf("%v %v", st, err)
	}
}

// TestSameOrigin: the dev server stamps requests Sol-Self, so only its own pages may reach it.
func TestSameOrigin(t *testing.T) {
	h := sameOrigin("127.0.0.1:8791", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	cases := []struct {
		method, host, origin, site string
		want                       int
	}{
		{"GET", "127.0.0.1:8791", "", "", 200},
		{"GET", "localhost:8791", "", "none", 200},
		{"POST", "127.0.0.1:8791", "http://127.0.0.1:8791", "same-origin", 200},
		{"POST", "127.0.0.1:8791", "https://evil.example", "cross-site", 403}, // CSRF
		{"POST", "127.0.0.1:8791", "", "", 403},                               // no Origin on a write
		{"GET", "evil.example:8791", "", "", 403},                             // DNS rebinding
		{"GET", "127.0.0.1:8791", "", "cross-site", 403},                      // a script or frame of another site
		{"GET", "127.0.0.1:8791", "", "cross-site/navigate", 200},             // following a link
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, "http://"+c.host+"/_sol/actions/raypass.status", nil)
		r.Host = c.host
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if site, mode, ok := strings.Cut(c.site, "/"); ok {
			r.Header.Set("Sec-Fetch-Site", site)
			r.Header.Set("Sec-Fetch-Mode", mode)
		} else if c.site != "" {
			r.Header.Set("Sec-Fetch-Site", c.site)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%s host %s origin %q site %q: %d, want %d", c.method, c.host, c.origin, c.site, w.Code, c.want)
		}
	}
}
