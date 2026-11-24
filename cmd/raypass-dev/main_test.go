package main

import (
	"context"
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
