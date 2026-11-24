package raypass

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/apv022/sol/sdk/go/sol"
	"github.com/apv022/sol/sdk/js"
	"github.com/apv022/sol/ui/kit"
)

// The web UI (ui/), the Sol UI kit at /kit/ and the SDK's sol.js bridge at /sol.js, the same
// layout as Sol's own apps. Nothing here is cached by a service worker or holds secrets: the
// UI asks for everything through actions.

//go:embed ui
var uiFS embed.FS

func (a *App) buildHandler() http.Handler {
	mux := http.NewServeMux()
	act := sol.NewActions(a.manifest)
	a.registerActions(act)
	if miss := act.Missing(); len(miss) > 0 {
		panic("raypass: actions without a handler: " + strings.Join(miss, ", "))
	}
	mux.Handle("POST /_sol/actions/", act)
	mux.HandleFunc("GET /lockstate", a.serveLockState)
	mux.Handle("GET /kit/", http.StripPrefix("/kit/", http.FileServerFS(kit.FS)))
	mux.HandleFunc("GET /sol.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		b, _ := js.FS.ReadFile("sol.js")
		_, _ = w.Write(b)
	})
	ui, _ := fs.Sub(uiFS, "ui")
	mux.Handle("GET /", http.FileServerFS(ui))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		// Secrets pass through action responses: never let a proxy or the browser cache them.
		if strings.HasPrefix(r.URL.Path, "/_sol/actions/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		mux.ServeHTTP(w, r)
	})
}
