// Command raypass-dev runs Raypass against an in-memory Sol so the UI can be tried in a browser
// without a daemon: one person, Alice, with two devices, each running its own Raypass on its own
// address, so sync between devices can be watched (add an item on one, it appears on the other).
// The vault is set up and seeded with sample items; the master password is printed. The memsol
// fake is served at /_sol/ on each origin, where the real daemon's bridge is.
//
//	go run ./cmd/raypass-dev    # http://127.0.0.1:8791/ and http://127.0.0.1:8792/
//
// It uses 100 000 PBKDF2 rounds (the minimum) instead of 600 000 so unlocking is quick.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"strings"

	"github.com/apv022/raypass"
	"github.com/apv022/raypass/vault"
	"github.com/apv022/sol/sdk/go/sol"
	"github.com/apv022/sol/sdk/go/sol/memsol"
)

// Password is the dev vault's master password.
const Password = "correct horse battery staple"

func main() {
	addr1 := flag.String("addr", "127.0.0.1:8791", "address of the first device's UI")
	addr2 := flag.String("addr2", "127.0.0.1:8792", "address of the second device's UI (empty to skip)")
	empty := flag.Bool("empty", false, "start with no vault (to try setup)")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, *addr1, *addr2, *empty); err != nil {
		log.Fatal(err)
	}
}

type device struct {
	d   *memsol.Device
	app *raypass.App
	c   *memsol.Client
}

func run(ctx context.Context, addr1, addr2 string, empty bool) error {
	n := memsol.New()
	alice := n.AddPerson("Alice")
	laptop := alice.AddDevice("laptop", false)
	m, err := sol.ParseManifest(raypass.ManifestJSON)
	if err != nil {
		return err
	}
	var devs []*device
	for _, d := range []*memsol.Device{alice.Devices[0], laptop} {
		c := d.Client(raypass.AppID)
		app := raypass.New(c, raypass.Config{Logf: log.Printf, KDFIter: vault.MinIter})
		if err := app.Start(ctx); err != nil {
			return err
		}
		defer app.Stop()
		devs = append(devs, &device{d, app, c})
	}
	alice.Install(m, devs[0].app.Handler())
	if !empty {
		if err := seed(devs[0]); err != nil {
			return err
		}
	}
	var servers []*http.Server
	for i, addr := range []string{addr1, addr2} {
		if addr == "" {
			continue
		}
		dv := devs[i]
		bridge := http.StripPrefix("/_sol", memsol.NewServer(n))
		tok := dv.c.Token()
		front := sameOrigin(addr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/_sol/v1/") {
				r.Header.Set("Authorization", "Bearer "+tok)
				bridge.ServeHTTP(w, r)
				return
			}
			r.Header.Set("Sol-Person", alice.ID)
			r.Header.Set("Sol-Device", dv.d.ID)
			r.Header.Set("Sol-Self", "1")
			dv.app.Handler().ServeHTTP(w, r)
		}))
		srv := &http.Server{Addr: addr, Handler: front}
		servers = append(servers, srv)
		go func() {
			if err := srv.ListenAndServe(); err != nil && ctx.Err() == nil {
				log.Print(err)
			}
		}()
	}
	fmt.Printf("Raypass dev UI is running (in-memory Sol, nothing is saved).\n  device 1  http://%s/\n", addr1)
	if addr2 != "" {
		fmt.Printf("  device 2  http://%s/   (same person, another device: watch them sync)\n", addr2)
	}
	if empty {
		fmt.Println("No vault yet: set one up in the UI.")
	} else {
		fmt.Printf("Master password: %s\n", Password)
	}
	fmt.Println("Add ?theme=dark to force dark mode. Ctrl-C to stop.")
	<-ctx.Done()
	for _, s := range servers {
		_ = s.Close()
	}
	return nil
}

// sameOrigin lets through only requests made to addr by a page of addr. The dev server stamps
// every request Sol-Self, the way the daemon does for the person's own UI, so without this any
// website open in the same browser could drive the vault: by a cross-site POST (CSRF) or, with
// DNS rebinding (a Host that is not ours), even read the answers. The real daemon makes the
// same checks (Sol's design/API.md section 10).
func sameOrigin(addr string, h http.Handler) http.Handler {
	_, port, _ := net.SplitHostPort(addr)
	hosts := map[string]bool{addr: true, "localhost:" + port: true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hosts[r.Host] {
			http.Error(w, "wrong host", http.StatusForbidden)
			return
		}
		origin := r.Header.Get("Origin")
		if r.Method != http.MethodGet && r.Method != http.MethodHead && origin != "http://"+r.Host {
			http.Error(w, "cross-origin request refused", http.StatusForbidden)
			return
		}
		navigate := r.Method == http.MethodGet && r.Header.Get("Sec-Fetch-Mode") == "navigate"
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" && !navigate {
			http.Error(w, "cross-site request refused", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// do runs an action on a device's app through its handler, as the person's own device.
func (dv *device) do(name string, args map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(args)
	r := httptest.NewRequest("POST", "/_sol/actions/"+name, bytes.NewReader(body))
	r.Header.Set("Sol-Self", "1")
	r.Header.Set("Sol-Device", dv.d.ID)
	w := httptest.NewRecorder()
	dv.app.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		return nil, fmt.Errorf("%s: %s", name, w.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out, nil
}

func seed(dv *device) error {
	var first error
	do := func(name string, args map[string]any) map[string]any {
		out, err := dv.do(name, args)
		if err != nil && first == nil {
			first = err
		}
		return out
	}
	do("raypass.setup", map[string]any{"password": Password})
	gh := do("raypass.add", map[string]any{"type": "login", "name": "GitHub", "user": "alice@example.com", "pass": "gh-old-Passw0rd",
		"urls": []string{"https://github.com"}, "totp": "otpauth://totp/GitHub:alice?secret=JBSWY3DPEHPK3PXP&issuer=GitHub",
		"folder": "Work", "fav": true, "tags": []string{"dev"}})
	if gh != nil {
		do("raypass.update", map[string]any{"id": gh["id"], "pass": "x7#Qm9!vLr2&Tz4p"})
	}
	do("raypass.add", map[string]any{"type": "login", "name": "Gmail", "user": "alice@gmail.com", "pass": "password123",
		"urls": []string{"https://accounts.google.com", "androidapp://com.google.android.gm"}})
	do("raypass.add", map[string]any{"type": "login", "name": "Bank", "user": "alice", "pass": "password123",
		"urls": []string{"https://bank.example.co.uk"}, "folder": "Finance",
		"fields": []map[string]any{{"name": "Customer number", "value": "88412093", "hidden": false}, {"name": "PIN", "value": "4096", "hidden": true}}})
	do("raypass.add", map[string]any{"type": "login", "name": "Home router", "user": "admin", "pass": "r0uter-Admin-2024", "urls": []string{"http://192.168.1.1"}, "folder": "Work/Servers"})
	do("raypass.add", map[string]any{"type": "login", "name": "Steam", "user": "alice_games", "pass": "S7eam-Games-Pass!", "urls": []string{"https://store.steampowered.com"}, "totp": "steam://JBSWY3DPEHPK3PXP"})
	do("raypass.add", map[string]any{"type": "note", "name": "Wi-Fi at home", "notes": "Network: Lighthouse\nPassword: sea-breeze-lantern-42"})
	do("raypass.add", map[string]any{"type": "card", "name": "Visa", "holder": "Alice Example", "number": "4111 1111 1111 1111", "exp": "07/29", "cvv": "123", "brand": "Visa", "folder": "Finance"})
	do("raypass.add", map[string]any{"type": "identity", "name": "Alice", "first": "Alice", "last": "Example", "email": "alice@example.com", "phone": "+44 20 7946 0000", "address": "1 Harbour St, Brighton", "company": "Example Ltd"})
	do("raypass.add", map[string]any{"type": "wallet", "name": "Bitcoin savings", "network": "bitcoin", "seed": "abandon ability able about above absent absorb abstract absurd abuse access accident",
		"address": "bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq", "derivation": "m/84'/0'/0'"})
	old := do("raypass.add", map[string]any{"type": "login", "name": "Old forum", "user": "alice", "pass": "hunter2", "urls": []string{"https://forum.example.org"}})
	if old != nil {
		do("raypass.trash", map[string]any{"ids": []string{old["id"].(string)}})
	}
	return first
}
