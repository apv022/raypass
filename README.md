# Raypass

A password manager that is a [Sol](https://github.com/apv022/sol) app: logins (with TOTP and
password history), secure notes, cards, identities and crypto wallets, end-to-end encrypted and
synced between your devices by Sol. Sol only ever carries ciphertext; the vault key exists only
in an unlocked Raypass process. Autofill comes from a browser extension (Chrome family and
Firefox) and an Android app.

The contract — crypto format, item model, actions, extension and Android protocol — is
[design/DESIGN.md](design/DESIGN.md). Rules for contributors: [AGENTS.md](AGENTS.md).

## Install

You need Go (1.26+) and a running Sol daemon.

```sh
make package                      # build/raypass-app/: app.json + raypass binary + icon.svg
sol app install build/raypass-app
```

Then open `http://raypass.localhost:7777/` (or Raypass in Sol's home screen), choose a master
password, and you are set. Install it the same way on every computer; each one unlocks with the
same master password. To build for another platform, set `GOOS`/`GOARCH`
(`GOOS=windows make package` names the binary `raypass.exe` and points `exec` at it).

`make build` builds `bin/raypass` and `bin/raypass-native` (the extension's native messaging
host; `bin/raypass-native install` registers it with your browsers). `make extension` packs the
extension into `build/`.

## Use it from a terminal or an agent

Every operation is a Sol action:

```sh
sol do raypass.status
sol do raypass.unlock --args '{"password": "…"}'       # or: --args - and type it on stdin
sol do raypass.list --args '{"query": "github"}'
sol do raypass.get --args '{"id": "3fa91c20d4e5f601"}' --json
sol do raypass.generate --args '{"mode": "words", "words": 6}'
sol do raypass.import --args '{"data": "…file contents…"}'
sol do raypass --help                                  # every action and its arguments
```

Actions that reveal a secret or change the vault need your approval when an agent calls them.
Other people (your Sol contacts) can never call any Raypass action.

## Develop

```sh
make dev           # the UI on an in-memory Sol: http://127.0.0.1:8791/ and :8792/ (two devices
                   # of one person, so you can watch sync); master password printed at start
make check         # gofmt, go vet (Linux, Windows, macOS), go test -race
make browser-test  # the UI's JS tests in headless Chromium
```

Layout: `vault/` crypto and item model, `totp/`, `gen/` (generator; `gen/wordlist.txt`),
`urlmatch/`, `port/` (import and export), the app itself at the root (`raypass.go`,
`actions.go`, `serve.go`, `app.json`), `ui/` the web UI, `cmd/` binaries, `extension/`,
`android/`. `testdata/vectors.json` holds crypto test vectors that the Go and Java tests share
(regenerate with `go test ./vault -run TestVectors -update`).

### The word list

`gen/wordlist.txt` should be the [EFF large word list](https://www.eff.org/dice). If it starts
with a `# Raypass FALLBACK` comment, it is a generated stand-in of the same size (7776
consonant-vowel words, so passphrases have the same 12.9 bits per word) made where eff.org could
not be reached; `make wordlist` downloads and checks the real one.
