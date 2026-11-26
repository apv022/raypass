# Raypass

Raypass is a password manager built as a [Sol](https://github.com/apv022/sol) app. It keeps your
logins, secure notes, cards, identities and crypto wallets in a vault that is encrypted on your
own devices and synced between them by Sol. Sol stores and carries only ciphertext. Autofill
comes from a browser extension (Chrome family and Firefox) and an Android app, and every
operation is also a command that you or an agent can run from a terminal.

The contract (crypto format, item model, actions, extension and Android protocol) is
[design/DESIGN.md](design/DESIGN.md). Rules for contributors are in [AGENTS.md](AGENTS.md).

## Features

- **Item types**: logins (username, password, several URLs, TOTP, notes, custom fields,
  password history), secure notes, payment cards, identities, and crypto wallets (seed
  phrase, private key, address, network, BIP39 passphrase, derivation path).
- **One-time codes**: TOTP per RFC 6238 (SHA1, SHA256 and SHA512, 6 to 8 digits, any
  period) from `otpauth://` URIs or bare base32 secrets, plus Steam Guard codes.
- **Generator**: random characters or EFF-wordlist passphrases from `crypto/rand` with no
  modulo bias, with the entropy shown.
- **Organisation**: folders (nested, like `Work/Servers`), favourites, tags, search, and a
  trash with restore and purge.
- **Health report**: weak, reused and old passwords, with a score.
- **Import and export**: imports from Bitwarden (JSON and CSV), Chrome, Edge and Brave,
  Firefox, 1Password, KeePass and KeePassXC, LastPass, Proton Pass (the export zip or CSV),
  and Raypass's own formats. Exports to
  Raypass JSON, CSV (Bitwarden layout) and a password-protected encrypted file. Exact
  duplicates are skipped on import.
- **Sync**: two Sol feeds (`keys` and `vault`) hold CRDT operations, so edits made on several
  devices merge, including edits made offline. Changes are pushed over Sol's events stream;
  nothing polls.
- **Auto-lock**: after a configurable number of minutes without use, on every client at once.
  The web UI hears about it live from a server-sent event stream, and the clipboard is cleared
  after a copy.
- **Autofill**: a browser extension with inline field icons, a popup, a save-login bar and
  Ctrl+Shift+L, and an Android `AutofillService` that works in apps and browsers.
- **Agent first**: every operation is an action in [app.json](app.json), reachable through
  `sol do raypass.* --json` with `--dry-run` and generated `--help`.

## Security model in brief

- **Sol only sees ciphertext.** The vault key (32 random bytes) is wrapped with a key derived
  from your master password by PBKDF2-HMAC-SHA256 (600 000 rounds). Items are encrypted with
  AES-256-GCM. Sol's daemon, its replication and every device's disk hold only ciphertext,
  salts and nonces.
- **The key lives only in an unlocked Raypass process** (the desktop app or the Android app).
  Locking, auto-lock and exit wipe the key and drop every decrypted value.
- **A copied disk is not enough.** To read it, an attacker has to guess your master password
  through PBKDF2. After 5 wrong guesses in a row, each further try waits longer.
- **Only you can call Raypass.** Every action requires a call from your own devices
  (`Sol-Self`); your Sol contacts can never call any of them. An agent (a Sol agent key) must
  get your approval for each action that reveals a secret or changes the vault.
- **Autofill is careful.** It fills only pages whose registrable domain matches a saved URL,
  only for `http` and `https`, and only into the top frame or a frame of the same site. A login
  saved for `https` is not filled into a plain `http` page until you confirm a warning.
- **Platform crypto only.** Go's standard library, `javax.crypto` on Android, and no
  third-party crypto code. Go and Java check the same test vectors
  ([testdata/vectors.json](testdata/vectors.json)).

The threat model is spelled out in [design/DESIGN.md](design/DESIGN.md).

## Download

Prebuilt binaries are on the [Releases page](https://github.com/apv022/raypass/releases): the
desktop app and native host for Linux, macOS and Windows (amd64 and arm64), the browser extension
for Chrome-family browsers and Firefox, the Android APK, and `SHA256SUMS`. Unpack the archive for
your platform and follow its `INSTALL.txt` (`sol app install raypass-app`, then
`raypass-native install` for the extension).

Releases are built by `.github/workflows/release.yml` when a `vX.Y.Z` tag is pushed, or when it is run by hand from the Actions tab with a tag name (a tag with a
`-suffix` is published as a pre-release); `scripts/release.sh 0.1.0` builds the same files into
`dist/` locally. To sign the APK with a stable key, set the repository secrets
`RAYPASS_KEYSTORE_B64` (base64 of a PKCS12 keystore), `RAYPASS_KEY_ALIAS` and
`RAYPASS_KEYSTORE_PASS`. Sol binds Raypass to the APK's signing certificate, so keep that key.

## Build and install on a desktop

You need Go 1.26 or later and Sol running on the machine.

```sh
make package                      # build/raypass-app/: app.json, the raypass binary, icon.svg
sol app install build/raypass-app
```

Then open `http://raypass.localhost:7777/` (or Raypass on Sol's home screen) and choose a master
password. Install Raypass the same way on each of your computers; each one unlocks with the same
master password once Sol has synced the `keys` feed. To build for another platform, set
`GOOS`/`GOARCH`: `GOOS=windows make package` names the binary `raypass.exe` and points
`app.json`'s `exec` at it.

## Browser extension

The extension holds no keys and no secrets. A native messaging host, `raypass-native`, runs
`sol do raypass.<action>` as you, so Sol's own sign-in is the gate.

```sh
make build                        # bin/raypass and bin/raypass-native
bin/raypass-native install        # registers the host with Chrome, Chromium, Brave, Edge, Vivaldi, Firefox
make extension                    # build/raypass-extension-{chrome,firefox}.zip
```

To load it in a Chrome-family browser, open `chrome://extensions`, turn on Developer mode,
choose *Load unpacked* and pick `extension/`. Firefox can load the zip as a temporary add-on.
For keeping it in Firefox, Windows registry steps, the extension IDs and the security rules
the code follows, see [extension/README.md](extension/README.md).

## Android

Raypass for Android ([android/README.md](android/README.md)) is a separate APK that connects
to Sol on the phone and does the same crypto in Java. It is built without Gradle:

```sh
android/toolchain.sh              # once: android.jar (API 34) and the dexer into ~/raypass-mobile/.toolchain
android/build.sh                  # android/build/raypass.apk, signed with a debug key
adb install -r android/build/raypass.apk
```

1. **Connect to Sol.** Install and set up Sol on the phone, open Raypass, tap **Connect to
   Sol** and allow it in Sol.
2. **Unlock or create a vault.** Raypass reads your `keys` feed first. If you already have a
   vault, unlock it with your master password. Biometric unlock can be turned on in Settings.
3. **Turn on autofill.** In Raypass's Settings, tap **Use Raypass for autofill**, or go to the
   system's *Passwords & accounts* → *Autofill service* and choose **Raypass**.

## Importing from other managers

Export from your old manager, then in Raypass open **Settings → Import**, leave **From** on
*Detect automatically* (or pick the manager) and choose the file.

**Proton Pass**: in Proton Pass open **Settings → Export**, choose **JSON** (a `.zip`, keeps
extra fields, folders and favourites) or **CSV**, and turn **encryption off** (Raypass can't
read PGP-encrypted exports). Enter your Proton password when asked and save the file. In Raypass,
**Settings → Import → Choose file…** and pick the `.zip` or `.csv` as it is (no need to unzip).
Aliases come in as logins with the alias address; passkeys and items in Proton's trash are not
imported, and Raypass tells you how many.

- **Bitwarden**: Tools → Export vault → `.json` or `.csv` (not the encrypted JSON).
- **Chrome, Brave, Edge**: Settings → Passwords (Password Manager) → Export passwords (`.csv`).
- **Firefox**: about:logins → ⋯ menu → Export Passwords (`.csv`).
- **1Password**: File → Export → your account → CSV.
- **KeePassXC**: Database → Export → CSV File.
- **LastPass**: Advanced options (or Account → Fix a problem yourself) → Export → `.csv`.

An export holds every password in plain text: **delete the export file** (and empty the
system trash) once the import is done.

## Use it from a terminal or an agent

Every operation is a Sol action:

```sh
sol do raypass.status
sol do raypass.unlock --args -                        # then type {"password": "…"} on stdin
sol do raypass.add --type login --name GitHub --user octo --pass 'S3cret!' --urls https://github.com
sol do raypass.add --args '{"type":"note","name":"Wi-Fi","notes":"…"}'
sol do raypass.list --args '{"query": "github"}'
sol do raypass.get --id 3fa91c20d4e5f601 --json
sol do raypass.totp --id 3fa91c20d4e5f601
sol do raypass.match --url https://github.com/login
sol do raypass.generate --mode words --words 6
sol do raypass.import --args '{"data": "…file contents…"}'
base64 -w0 export.zip | jq -Rs '{data: ., encoding: "base64"}' | sol do raypass.import --args -
sol do raypass.export --format encrypted --password '…'
sol do raypass.purge --all_trash --dry-run            # what would go, changing nothing
sol do --help raypass                                 # every action and its arguments
```

Pass secrets with `--args -` on stdin rather than in argv, where other local users can see
them in the process list.

## Develop

```sh
make dev           # the web UI on an in-memory Sol: http://127.0.0.1:8791/ and :8792/ (two
                   # devices of one person, to watch sync); the master password is printed
```

Layout: `vault/` (crypto, keyring, item model, CRDT fold), `totp/`, `gen/` (generator and
`gen/wordlist.txt`), `urlmatch/` (URL matching and `suffixes.txt`), `port/` (import and
export), the app itself at the root (`raypass.go`, `actions.go`, `lockstate.go`, `serve.go`,
`app.json`), `ui/` (the web UI: plain JS modules, no build step), `cmd/` (binaries),
`extension/`, `android/`.

`gen/wordlist.txt` should be the [EFF large word list](https://www.eff.org/dice). If it starts
with a `# Raypass FALLBACK` comment, it is a generated stand-in of the same size and entropy
per word; `make wordlist` downloads and checks the real one.

## Testing

```sh
make check                    # gofmt, go vet (Linux, Windows, macOS), go test -race ./...
make browser-test             # ui/test and extension/test in headless Chromium (go run ./cmd/browsertest)
android/test.sh               # the Android core's pure-Java tests on the JDK, against the shared vectors
android/build.sh              # the APK builds (javac with -Xlint, no invokedynamic for dx)
SOL_BIN=/path/to/sol scripts/e2e-real-sol.sh   # two real Sol daemons on loopback, every action
                              # through sol do, the native host, the live lock stream, sync both ways
```

The Go tests run against `memsol`, Sol's in-memory fake. They cover two devices of one person,
devices that set up separate vaults while partitioned, auto-lock and the lock event stream
behind a proxy like Sol's.

## Known limits

- **Firefox extension untested here.** The extension's test suite runs in headless Chromium.
  The Firefox build is generated from the same source (`cmd/extpack`) but has not been run
  in Firefox in this repository's tests.
- **Android not yet run on a device.** The Android app compiles and its core passes the
  shared tests on the JDK, but it has not been run on a phone yet.
- **Windows uses a TCP socket.** On Windows, Sol serves apps on a loopback TCP port instead
  of a Unix socket. Another local process that finds the port can talk to Raypass directly,
  without going through Sol's sign-in. This is Sol's design on Windows, not Raypass's.
- **Agent approvals need Sol v0.2.0-beta.3 or later to keep secrets off disk.** `app.json` marks
  every password, seed, key, card number, note and custom field argument `"secret": true`, and
  every action that returns a secret `output_secret`. Sol v0.2.0-beta.3 and later (apv022/sol#4)
  then keeps those only in memory while an agent's call waits for your approval, and never
  writes an approved action's output to disk. Older Sol versions ignore the marks and store the
  call's arguments and output in `inbox.json` in plain text.
- **Only http and https pages get domain matching.** Matching by registrable domain uses a
  short embedded list of multi-part and hosting suffixes (`urlmatch/suffixes.txt`), not the
  full Public Suffix List. Any other scheme needs the same scheme, host and port. The
  insecure-page warning (an https login on an http page) exists only in the extension,
  because Android browsers do not reliably report the scheme to autofill.
- **Two vaults set up offline need recovering.** If two devices each set up a vault before
  they ever synced, the earlier vault wins. The other vault's items are kept but cannot be
  read with the winner's key. `raypass.status` lists them in `orphaned_keyrings`;
  `raypass.recover` (or Settings → *Recover items from another vault*) copies them in with the
  other vault's master password.
- **Old ciphertext stays readable with the old password.** Changing the master password
  re-wraps the vault key but does not re-encrypt history. Someone who knew the old password
  and copied your data before the change can still read what it held then. Every synced
  password manager has this limit.
