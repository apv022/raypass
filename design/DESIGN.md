# Raypass: design

Status: the contract every part of Raypass is built against (Go app, web UI, browser extension,
Android). If code and this file disagree, the code is wrong; change this file first.

Raypass is a password manager that is a **Sol app** (github.com/apv022/sol, design/API.md there).
It uses only Sol's public app API (`sdk/go/...`, `ui/kit`), never Sol's `internal/`. Sol carries
the bytes between the person's devices; Raypass makes sure Sol only ever carries ciphertext.

## What it does

- Logins (username, password, URLs, TOTP, notes, custom fields, password history)
- Secure notes (free text)
- Crypto wallets (seed phrase, private key, address, network, notes)
- Cards (holder, number, expiry, CVV, notes) and identities (name, email, phone, address)
- TOTP codes (RFC 6238, SHA1/SHA256/SHA512, 6–8 digits, any period; `otpauth://` URIs and bare
  base32 secrets; Steam Guard codes as `steam://SECRET`)
- Password and passphrase generator (crypto/rand, no modulo bias; EFF large wordlist)
- Folders, favourites, tags, trash (restore / purge), search
- Health report: weak, reused, and old passwords; items with a TOTP-able site but no TOTP is out
  of scope (no network)
- Import: Bitwarden JSON (unencrypted), Bitwarden CSV, Chrome/Edge/Brave CSV, Firefox CSV,
  1Password CSV, KeePass(XC) CSV, LastPass CSV, Raypass JSON. Export: Raypass JSON, CSV, and an
  encrypted Raypass export (password-protected, same crypto as the vault)
- Autofill: the browser extension on desktop (Chrome, Chromium, Brave, Edge, Firefox) and an
  Android AutofillService
- Every operation is a Sol action reachable from `sol do raypass.* --json`

## Threat model, in one paragraph

Sol's daemon, its feed store, its replication and every device's disk only see ciphertext and the
KDF salt. The vault key exists in memory only in an unlocked Raypass process (desktop app or
Android app), and is wiped on lock, auto-lock and exit. Someone who copies a device's disk needs
to brute-force the master password through PBKDF2 (600 000 rounds). While unlocked, anything
that can call `raypass.*` actions as the person (the person's own Sol UI, `sol do` by the same OS
user, the browser extension through `sol do`) can read secrets: that is the same as any
unlocked password manager. Agents (Sol agent keys) must get the person's approval for every
action that reveals a secret or changes the vault (`"approval": "agents"`). Peers (other
people) can never call any Raypass action: every handler requires `Sol-Self: 1`.

## Crypto (the interop contract; Go, Java and tests must agree byte for byte)

All base64 is standard base64 **with padding** (RFC 4648 §4). All JSON is UTF-8.

### Master key

```
salt      = 16 random bytes (stored in the keyring)
mk        = PBKDF2-HMAC-SHA256(password UTF-8 bytes, salt, iter, 32 bytes)
```
`iter` is stored in the keyring (default 600 000, minimum accepted 100 000). The password is
used as given (no normalization other than UTF-8 encoding; the UIs must not trim it).

Why PBKDF2 and not Argon2id: it is in Go's standard library (`crypto/pbkdf2`), in Android
(`SecretKeyFactory "PBKDF2WithHmacSHA256"`) and in WebCrypto, so every part of Raypass derives
keys with a platform implementation and no third-party code. Bitwarden made the same choice.

### Vault key and the keyring

The vault key `vk` is 32 random bytes made once at setup. It never changes (changing the master
password re-wraps it), so every entry ever written stays readable.

```
wrap   = AES-256-GCM(key = mk, nonce = 12 random bytes, plaintext = vk, aad = "raypass/keyring/v1")
check  = HMAC-SHA256(key = vk, "raypass/check/v1")   (lets a device tell a wrong password from a corrupt keyring;
                                                      GCM failing already tells it, check is for vk sanity)
```

Keyring entry body (feed `keys`):
```json
{"v": 1, "type": "keyring", "id": "<16 hex: keyring id, random per vault>",
 "kdf": {"alg": "pbkdf2-sha256", "iter": 600000, "salt": "<b64 16 bytes>"},
 "nonce": "<b64 12 bytes>", "wrapped": "<b64 ciphertext+tag of vk (48 bytes)>",
 "check": "<b64 32 bytes>",
 "stamp": {"w": 1795121162000, "c": 0, "d": "<device id>"}}
```
The current keyring is the one with the greatest `stamp` (crdt.Stamp order: w, then c, then d).
A password change appends a new keyring entry with the same `id`, a new salt, nonce and wrap.
Since vk never changes, every keyring of a vault has the same `check`: a keyring with the vault's
`id` whose `check` differs from that of the vault's first (smallest-stamp) keyring is ignored, so
a broken or stray write can neither lock the person out nor make a device write entries under
another key.
If entries with **different** `id`s exist (two devices set up a vault while offline), the one
with the smallest stamp wins and Raypass refuses to set up again; the other vault's items are
reported by `raypass.status` as `orphaned` (not readable; `orphaned_keyrings` lists the other
vaults' keyring ids) – we never silently lose them: its entries stay in the feed, and
`raypass.recover` with the other vault's master password copies its items into this vault.

### Vault entries

Feed `vault`. Each entry body:
```json
{"v": 1, "k": "<keyring id>", "n": "<b64 12-byte nonce>", "c": "<b64 AES-256-GCM ciphertext+tag>"}
```
`c = AES-256-GCM(key = vk, nonce = n, plaintext = P, aad = "raypass/vault/v1")` where `P` is the
UTF-8 JSON of a batch of CRDT map ops, exactly the SDK's `crdt.MapOp` shape:
```json
{"ops": [{"key": "i.3fa91c20d4e5f601.name", "value": "GitHub", "stamp": {"w": 1795121162000, "c": 0, "d": "<device>"}},
         {"key": "i.3fa91c20d4e5f601.pass", "delete": true, "stamp": {...}}]}
```
Entries larger than Sol's 64 KiB body limit are split into several entries (a batch is only a
grouping; ops are independent). Nonces are random: AES-GCM with random 96-bit nonces is safe far
beyond any vault's number of entries.

State is a single LWW `crdt.Map` folded from every op of every entry (any order, idempotent).
Comparing stamps: `w`, then `c`, then `d` (string compare). A device must `Observe` every stamp
it applies into its HLC clock so its next write wins over what it has seen. So a far-future
stamp (a device whose clock ran years ahead) does not freeze a field: every device's clock
follows it and later writes still win. Stamps are not clamped to local time, because devices
must converge on the same state whatever their clocks say. (Only a stamp whose counter is near
2^32 could make an observing clock's counter wrap; ops are inside the AES-GCM ciphertext, so
only a holder of the vault key, one of the person's own devices, could write one.)

### Encrypted export file

```json
{"raypass_export": 1, "kdf": {"alg": "pbkdf2-sha256", "iter": 600000, "salt": "<b64>"},
 "nonce": "<b64>", "data": "<b64 AES-256-GCM(key = PBKDF2(export password), plaintext = Raypass JSON export, aad = \"raypass/export/v1\")>"}
```

## Item model (keys of the map)

`<id>` is 16 lowercase hex chars (8 random bytes). An item exists once `i.<id>.type` is set.

| Key | Value |
|---|---|
| `i.<id>.type` | `login`, `note`, `card`, `identity`, `wallet` |
| `i.<id>.name` | display name |
| `i.<id>.folder` | folder name ("" = none). Folders are just names (`Work/Servers` allowed) |
| `i.<id>.fav` | bool |
| `i.<id>.tags` | array of strings |
| `i.<id>.notes` | free text (the whole body of a secure note) |
| `i.<id>.fields` | array of `{"name","value","hidden":bool}` |
| `i.<id>.created`, `i.<id>.updated` | ms since epoch |
| `i.<id>.trashed` | ms since epoch, `0` = not in trash |
| login: `user`, `pass`, `urls` (array of strings), `totp` (otpauth URI, `steam://…` or base32), `history` (array of `{"pass","until"}` newest first, at most 20) |
| card: `holder`, `number`, `exp` (`MM/YY`), `cvv`, `brand` |
| identity: `first`, `last`, `email`, `phone`, `address`, `company` |
| wallet: `seed` (seed phrase), `privkey`, `address`, `network` (e.g. `bitcoin`, `ethereum`), `passphrase` (BIP39 25th word), `derivation` |
| `s.auto_lock` | settings, synced: minutes (default 15, 0 = never) |
| `s.clip_clear` | seconds before the UI clears a copied secret (default 30) |

A **purge** deletes every key of the item (map tombstones; the ciphertext of old values stays in
the feed history, which is why changing the master password does not make old ciphertext safe
from someone who already knows the old password – documented, same as every synced manager).

Changing `pass` of a login appends the previous one to `history`.

## Lock state (per Raypass process)

- `raypass.setup` creates the keyring (refused if one exists), then the vault is unlocked.
- `raypass.unlock` derives mk, unwraps vk, folds the vault. Wrong password: `unauthorized`.
  After 5 failures in a row each try waits 2^(n-5) s (cap 60 s) before answering.
- Auto-lock after `s.auto_lock` minutes without use; `raypass.lock` at once. Use is any action
  that needs the vault, plus `setup`, `unlock`, `change_password` and `settings`; `status`,
  `generate` and `lock` are not use, so a client that only shows the lock state never keeps the
  vault open. `raypass.status` reports `locks_in`, the seconds left (0 when locked or never). No
  client locks the vault because it was idle itself (another client may be using it). Lock
  zeroes vk and drops all decrypted state (the folded map is rebuilt on unlock).
- Live lock state: `GET /lockstate` on the app's UI origin is a `text/event-stream` (Sol-Self
  only, like every action) that sends `data: {"locked","locks_in"}` at once and again whenever
  the vault locks or unlocks or the auto-lock deadline moves, with a `: keepalive` comment every
  25 s. The web UI follows it (EventSource, reopened with backoff if refused): locked wipes and
  shows the unlock screen; unlocked by another client while it shows the unlock screen opens
  the vault. It keeps no timer and never polls. Sol's daemon proxies app origins with
  `httputil.ReverseProxy`, which flushes `text/event-stream` responses as written, so the
  stream works through it (the tests run it behind such a proxy).
- While locked, actions that need the vault answer `{"error":{"code":"forbidden","message":"locked"}}`
  (HTTP 403, message exactly `locked`), so clients can show the unlock screen.
- Ciphertext entries keep arriving while locked (events stream); they are stored as ciphertext
  and decrypted on unlock.

## Feeds and sync

Feeds `keys` and `vault`, owned by the person, written by every device of the person, never
shared with anyone. Start: read both from pos 1; live: the events stream (`entry` events for both
feeds), never polling. Every action first drains what the stream has not delivered yet.

## Actions (app id `raypass`)

Every action requires `Sol-Self: 1` (else `forbidden`). "Vault" means it answers `locked` while
locked. Secrets are only in outputs of actions marked **reveals**; those are `approval: agents`.

| Action | Effect | Input | Output |
|---|---|---|---|
| `raypass.status` | read | – | `{"initialized","locked","items","trash","orphaned","orphaned_keyrings","auto_lock","clip_clear","kdf_iter","locks_in"}` |
| `raypass.setup` | write, agents | `password` | `{"ok":true}` (unlocked) |
| `raypass.unlock` | write, agents | `password` | `{"ok":true}` |
| `raypass.lock` | write | – | `{"ok":true}` |
| `raypass.change_password` | write, agents | `old`, `new`, `iter?` | `{"ok":true}` |
| `raypass.list` | read, vault | `query?`, `type?`, `folder?`, `fav?`, `trash?` (bool), `url?` | `[{id,type,name,user,urls,folder,fav,tags,has_totp,updated,trashed}]` (no secrets; `user` is shown, it is not a secret here). Sorted: favourites, then name |
| `raypass.get` | read, vault, **reveals** | `id` | the whole item: `id`,`type`,`name`,`folder`,`fav`,`tags`,`notes`,`fields`,`created`,`updated`,`trashed` and every field of its type (`""`/`[]` when unset), so the shape is fixed per type |
| `raypass.add` | write, vault, agents | `type`, `name`, and any fields of the type | `{"id"}` |
| `raypass.update` | write, vault, agents | `id` + fields to set (`null` clears a field) | `{"id"}` |
| `raypass.trash` | write, vault, agents | `ids` | `{"trashed": n}` |
| `raypass.restore` | write, vault, agents | `ids` | `{"restored": n}` |
| `raypass.purge` | destructive, vault, agents | `ids` (must be in trash) or `all_trash: true` | `{"purged": n}` |
| `raypass.generate` | read | `mode` (`chars`/`words`), `length` (chars, 4–128, default 20), `upper`,`lower`,`digits`,`symbols` (bools, default true), `avoid_ambiguous`, `words` (3–20, default 5), `separator` (default `-`), `capitalize`, `number` | `{"password","entropy_bits"}` |
| `raypass.totp` | read, vault, **reveals** | `id` | `{"code","period","remaining","digits"}` |
| `raypass.match` | read, vault | `url` | logins whose URLs match (see below), like `list` rows, best match first; a row also has `"insecure": true` when `url` is plain `http` and the login matches it only through `https` URLs |
| `raypass.fill` | read, vault, **reveals** | `id`, `url`, `allow_insecure?` | `{"user","pass","totp"?}`; refuses (`forbidden`, "url does not match") unless one of the item's URLs matches `url`, and (`forbidden`, "insecure page") an insecure match (as in `match`) unless `allow_insecure: true` |
| `raypass.save_login` | write, vault, agents | `url`, `user`, `pass`, `name?` | `{"id","created":bool}`; updates the pass of an existing login with the same user and matching URL (history kept), else adds one named after the host |
| `raypass.folders` | read, vault | – | `[{"name","count"}]` |
| `raypass.health` | read, vault | – | `{"weak":[ids],"reused":[[ids]],"old":[ids],"score"}` (no secrets) |
| `raypass.export` | read, vault, **reveals** | `format` (`json`,`csv`,`encrypted`), `password` (for encrypted) | `{"data","filename"}` |
| `raypass.import` | write, vault, agents | `format` (`auto`,`bitwarden_json`,`bitwarden_csv`,`chrome_csv`,`firefox_csv`,`1password_csv`,`keepass_csv`,`lastpass_csv`,`raypass_json`,`raypass_encrypted`), `data`, `password?` | `{"imported": n, "skipped": n, "format"}` (`format` is the detected one; exact duplicates of items already in the vault, and Raypass JSON ids already present, are skipped) |
| `raypass.settings` | write, vault, agents | `auto_lock?`, `clip_clear?` | the settings |
| `raypass.recover` | write, vault, agents | `password` (the orphaned vault's master password), `keyring?` (one of `orphaned_keyrings`) | `{"recovered": n, "skipped": n}`: the orphaned vault's entries are decrypted with its own key and its live items re-added here with new ids; exact duplicates of live items are skipped, so running it twice adds nothing. Wrong password: `unauthorized` (counts toward the unlock backoff); no such orphaned vault: `not_found` |

All write actions honour `Sol-Dry-Run: 1` (describe, change nothing): they validate exactly as
the real call would and answer `{"dry_run": true, "would": {...}}`, which names fields but never
echoes a secret value.

Notes fixed by the Go implementation (the table above is authoritative for shapes):

- Approval: every action that reveals a secret **or changes the vault** is `approval: agents`, as
  the threat model says (only `raypass.lock` among the writes is not).
- `raypass.setup` and `raypass.change_password` refuse a new master password shorter than 8
  characters (`bad_request`). `setup` when a keyring exists: `conflict`. `unlock` or
  `change_password` with no vault: `not_found`. A wrong (old) password: `unauthorized`; the
  backoff counts failures of both, per Raypass process, in memory.
- `raypass.status` while locked reports `items` and `trash` as 0, and the settings last seen
  unlocked (defaults before that). `orphaned` counts vault entries under another keyring id, plus
  (while unlocked) entries of this vault that failed to decrypt; it stays non-zero after
  `raypass.recover` (the entries are never removed). `orphaned_keyrings` is `[]` when there are
  none. The web UI's Settings shows "Recover items from another vault" while it is non-empty.
- `list`/`match` rows: `user` is the login's user, a card's holder, or an identity's
  "first last". `fill` works on logins not in the trash only.
- Errors for unknown items are `not_found`; bad input `bad_request`.
- CSV export uses Bitwarden's CSV layout (most managers read it). It has only logins and notes,
  so cards, identities and wallets are written as notes with their fields as `name: value` lines
  in the `fields` column, and a login's extra URLs as `url: …` fields. Nothing is lost.
- Exports contain the items not in the trash.
- CSV export defuses spreadsheet formulas in the columns a web page can influence (through
  `save_login`) or a hostile import can carry: `folder`, `name`, `login_uri` and `login_username`
  get a leading `'` when they start with `=`, `+`, `-`, `@`, tab or CR (after any `'`s).
  Passwords, TOTP, notes and fields are written unchanged (changing a secret would corrupt it in
  the manager the file is for). Bitwarden CSV import removes that one `'`, so a Raypass round trip
  is exact.

### URL matching

Normalize: lowercase scheme and host, default scheme `https`. Two URLs match when their
**registrable domain** is equal (eTLD+1, using an embedded short list of multi-part public
suffixes: `co.uk`, `com.au`, `co.jp`, `github.io`, … ; else last two labels), or for IP
addresses and `localhost`, host and port are equal. Android apps are URLs `androidapp://<package>`
and match exactly. Registrable-domain matching is for `http`/`https` only: a URL with any other
scheme (`javascript://bank.com/`, `ftp://bank.com`) matches only the same scheme, host and port.
Best-match ordering: exact host, then same registrable domain; then by name.

**Insecure pages.** A login whose matching URLs are all `https` offered on a plain `http` page
of the same site (a downgraded or misconfigured page: anyone on the network reads what is
typed there) is *insecure*: `raypass.match` marks the row `"insecure": true` and `raypass.fill`
answers `forbidden` "insecure page" unless called with `allow_insecure: true`. A login that also
has a matching `http` URL was saved for that page on purpose and is not insecure. Clients send
`allow_insecure` only after the person confirmed a warning (the extension's "Fill anyway").
The web UI does not fill pages and is unaffected.

## Web UI (served by the app at `/`, opened at `http://raypass.localhost:7777/`)

Plain JS modules, `ui/kit` tokens and components (`/kit/...` served from `ui/kit`'s embed), the
SDK's `sol.js` bridge for action calls, no build step, no third-party JS. Screens: setup, unlock,
vault (sidebar: All, Favourites, Logins, Notes, Cards, Identities, Wallets, folders, Trash,
Generator, Health, Settings), list with search, item detail with copy buttons, reveal toggles,
live TOTP ring, edit forms per type, generator, import/export. Clipboard is cleared after
`s.clip_clear` seconds (only if it still holds what we copied). Locks itself on the `locked` error
from any call and on a locked state from `/lockstate`. Every value from data is escaped (textContent, never innerHTML with data).

## Browser extension (`extension/`, Manifest V3, Chrome family and Firefox)

The extension holds no secrets and no keys. It talks to a **native messaging host**,
`raypass-native` (Go, stdlib only, `cmd/raypass-native`), which runs
`sol do <action> --args - --json` with the arguments on stdin (never argv) and returns the JSON.
So the extension reaches Raypass exactly the way an agent or the person on a terminal does, and
Sol's own sign-in (the admin token of the OS user) is the gate. Host name:
`app.raypass.native`. Only actions `raypass.status`, `unlock`, `lock`, `match`, `fill`,
`save_login`, `generate`, `totp`, `list`, `get` are forwarded; anything else is refused by the host.

- Content script: finds login forms (password inputs, their username fields, TOTP fields by
  `autocomplete="one-time-code"` and name heuristics), shows a small inline icon in the field,
  fills on request (dispatching `input`/`change` events), and offers to save a submitted login
  (asks the background, which shows a bar; nothing saved without a click).
- Popup: unlock, matches for the current tab (fill / copy user / copy pass / copy TOTP), search
  all, generator.
- Keyboard shortcut `Ctrl+Shift+L` fills the best match.
- Insecure pages: a match marked `insecure` shows "Not secure" in the field's panel and the
  popup; filling it (or copying its password from the popup) needs a second, real click on a
  "Fill anyway" warning, and only then is `allow_insecure` sent. The background's own
  password comparison before offering to save sends `allow_insecure` (the secret never reaches
  the page).
- Fill only into the top frame or same-registrable-domain frames; the URL passed to
  `raypass.fill` is the frame's URL as seen by the background (`sender.url`), never from the page.
- `raypass-native install` writes the host manifests for Chrome, Chromium, Brave, Edge, Firefox
  on Linux, macOS (and prints the registry command on Windows).

## Android (`android/`)

An external Sol app (Sol's android/README.md "For Android developers"): its own APK
`app.raypass`, registered with Sol through `app.sol.action.CONNECT` as app id `raypass` with
scopes `feeds`. It reads and writes the same `keys` and `vault` feeds through Sol's loopback API
with its token, and does the crypto above itself in Java (`javax.crypto`, no third-party code).

- Unlock screen (master password; optional biometric unlock: the vault key wrapped by an Android
  Keystore key that requires user authentication, `BiometricPrompt`), vault list with search,
  item detail with copy and TOTP, add/edit for every type, generator, lock.
- `AutofillService`: fills logins into apps (match `androidapp://<package>` or the web domain
  that the autofill structure reports for browsers), offers to save new logins
  (`SaveInfo`), and an "Open Raypass" dataset when locked (authentication intent).
  No insecure-page warning on Android: browsers report the page's domain to an autofill
  service (`ViewStructure.getWebDomain`) but its scheme only on Android 9+ and not from every
  browser (`getWebScheme`, else taken as `https`), so an http page cannot be told from https
  reliably. Android fills as before; the warning is the extension's.
- Clipboard: copies are marked sensitive (`ClipDescription.EXTRA_IS_SENSITIVE`) and cleared.
- `FLAG_SECURE` on every window that may show a secret.
- Built with no Gradle, like Sol's own APK (`android/build.sh`: javac, d8, aapt2, apksigner).

## Layout

```
app.json                 manifest
raypass.go …             package raypass: the app (actions, sync, lock state, serve)
vault/                   crypto, keyring, item model, CRDT fold (stdlib + sol/sdk crdt)
totp/                    RFC 4226/6238, otpauth URIs, Steam
gen/                     generator + EFF wordlist
port/                    import / export formats
urlmatch/                URL normalization and matching
ui/                      web UI
cmd/raypass/             the app binary (sol.FromEnv)
cmd/raypass-dev/         the UI against memsol, no daemon
cmd/raypass-native/      native messaging host
cmd/browsertest/         runs */test/index.html in headless Chromium
extension/               the browser extension
android/                 the Android app
testdata/vectors.json    crypto test vectors shared by Go and Java tests
```
