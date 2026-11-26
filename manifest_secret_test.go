package raypass

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/apv022/sol/sdk/go/sol"
)

// TestManifestMarksSecrets checks that Sol is told which action arguments and outputs are
// secrets (API.md §8–9), so an agent's call waiting for approval never puts a password, seed,
// key or decrypted item into Sol's inbox on disk.
func TestManifestMarksSecrets(t *testing.T) {
	m, err := sol.ParseManifest(ManifestJSON)
	if err != nil {
		t.Fatal(err)
	}
	secretArgs := map[string][]string{
		"raypass.setup":           {"password"},
		"raypass.unlock":          {"password"},
		"raypass.change_password": {"old", "new"},
		"raypass.recover":         {"password", "keyring"},
		"raypass.export":          {"password"},
		"raypass.import":          {"data", "password"},
		"raypass.save_login":      {"pass"},
		"raypass.add":             {"pass", "totp", "notes", "fields", "seed", "privkey", "passphrase", "number", "cvv"},
		"raypass.update":          {"pass", "totp", "notes", "fields", "seed", "privkey", "passphrase", "number", "cvv"},
	}
	for name, keys := range secretArgs {
		a, ok := m.Action(name)
		if !ok {
			t.Fatalf("%s missing", name)
		}
		args := map[string]any{}
		for _, k := range keys {
			args[k] = "s3cr3t-" + k
		}
		raw, _ := json.Marshal(args)
		red, _ := a.Input.Redact(raw)
		if strings.Contains(string(red), "s3cr3t") {
			t.Errorf("%s: a secret survives redaction: %s", name, red)
		}
	}
	for _, name := range []string{"raypass.get", "raypass.fill", "raypass.totp", "raypass.export", "raypass.generate"} {
		if a, _ := m.Action(name); !a.OutputSecret {
			t.Errorf("%s: output not marked output_secret", name)
		}
	}
	// update must still accept null to clear a secret field.
	a, _ := m.Action("raypass.update")
	if err := a.Input.Validate(json.RawMessage(`{"id":"0123456789abcdef","pass":null,"seed":"x"}`)); err != nil {
		t.Fatal(err)
	}
}
