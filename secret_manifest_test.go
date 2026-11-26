package raypass

import (
	"testing"

	"github.com/apv022/sol/sdk/go/sol"
)

// Every input that can carry a secret is marked secret and every output that reveals one is
// output_secret, so Sol keeps them out of its inbox on disk (apv022/raypass#1).
func TestManifestMarksSecrets(t *testing.T) {
	m, err := sol.ParseManifest(ManifestJSON)
	if err != nil {
		t.Fatal(err)
	}
	in := map[string][]string{
		"raypass.setup": {"password"}, "raypass.unlock": {"password"}, "raypass.change_password": {"old", "new"},
		"raypass.add":        {"pass", "totp", "cvv", "number", "seed", "privkey", "passphrase", "notes", "fields"},
		"raypass.update":     {"pass", "totp", "cvv", "number", "seed", "privkey", "passphrase", "notes", "fields"},
		"raypass.save_login": {"pass"}, "raypass.export": {"password"}, "raypass.import": {"password", "data"},
		"raypass.recover": {"password", "keyring"},
	}
	for action, props := range in {
		spec, ok := m.Action(action)
		if !ok {
			t.Fatalf("no %s", action)
		}
		for _, p := range props {
			if s := spec.Input.Properties[p]; s == nil || !s.Secret {
				t.Errorf("%s: %s is not marked secret", action, p)
			}
		}
	}
	for _, action := range []string{"raypass.get", "raypass.fill", "raypass.totp", "raypass.export", "raypass.generate"} {
		if spec, _ := m.Action(action); !spec.OutputSecret {
			t.Errorf("%s: output not marked secret", action)
		}
	}
	// A cleared field (null) still validates on update.
	spec, _ := m.Action("raypass.update")
	if err := spec.Input.Validate([]byte(`{"id":"x","pass":null,"totp":"otpauth://x"}`)); err != nil {
		t.Errorf("update with a cleared secret field: %v", err)
	}
}
