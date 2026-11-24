# AGENTS.md

Rules for anyone (human or agent) building Raypass. Read [design/DESIGN.md](design/DESIGN.md)
first: it is the contract (crypto format, item model, actions, extension and Android protocol).

Raypass is a Sol app. Sol's rules apply (github.com/apv022/sol AGENTS.md); the ones that matter
most here:

1. **Sol's public API only.** Import the stdlib, `github.com/apv022/sol/sdk/go/...` and
   `github.com/apv022/sol/ui/kit`. Never Sol's `internal/`. No other Go dependency, no
   third-party JS, no Node/npm, no build step for UIs. Android: Android framework + `javax.crypto`
   only, no Gradle, no AndroidX.
2. **Agent first.** Every user-visible operation is an action in `app.json`, reachable from
   `sol do raypass.* --json`. A button that isn't an action is a bug.
3. **No polling.** Sync comes from Sol's events stream.
4. **Never log secrets.** No passwords, vault contents, keys, TOTP seeds, request bodies, in logs,
   errors, panics or test names. Secret files `0600`, dirs `0700`, writes atomic. Wipe key bytes
   on lock.
5. **Sol only sees ciphertext.** Nothing in a feed body is plaintext except the fields
   design/DESIGN.md says (version, keyring id, KDF params, nonces).
6. **Honest stubs.** Unbuilt behaviour returns `not_implemented`, never fake success.
7. **Tests with every change.** The app tests against `memsol`. Crypto has shared vectors in
   `testdata/vectors.json` that Go and Java both check.
8. **Godoc on every exported identifier**, a package comment saying the package's job and
   invariants. Comment the why.
9. **Escape everything** from data before it reaches the DOM (textContent).
10. Linux, macOS, Windows must build (`GOOS=windows go vet ./...`).

## Commands

```sh
make check         # gofmt + go vet + go test -race ./... ; must pass before you say done
make build         # bin/raypass, bin/raypass-native
make browser-test  # every */test/index.html in headless Chromium
make dev           # the UI against an in-memory Sol at http://127.0.0.1:8791/
make extension     # build/raypass-extension-{chrome,firefox}.zip
android/build.sh   # build/raypass.apk (needs the Android toolchain, see android/README.md)
```
