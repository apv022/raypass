// Command raypass is the Raypass app as Sol runs it: it reads SOL_API and SOL_APP_TOKEN (the
// daemon's API and this app's token) and serves the UI and actions on SOL_APP_LISTEN. Install it
// with `sol app install <dir>` (see README.md); it is not meant to be started by hand.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/apv022/raypass"
	"github.com/apv022/sol/sdk/go/sol"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("raypass: ")
	api, err := sol.FromEnv()
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Logs carry no secrets: the app never passes vault content or request bodies to Logf.
	app := raypass.New(api, raypass.Config{Logf: log.Printf})
	if err := app.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
