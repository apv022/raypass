# Raypass browser extension

Fills and saves logins from Raypass in Chrome, Chromium, Brave, Edge, Vivaldi and Firefox
(Manifest V3, one source tree). The extension holds no secrets and no keys: it asks the
**native messaging host** `raypass-native` (`cmd/raypass-native`), which runs
`sol do raypass.<action> --args - --json` as you. Sol's own sign-in is the gate, exactly as for
an agent or you on a terminal. The contract is `design/DESIGN.md`, "Browser extension".

```
extension/
  manifest.json        Chrome manifest (cmd/extpack derives the Firefox one)
  background.js        native port, frame policy, fill/save flows, menu, Ctrl+Shift+L
  content.js           form detection, inline icon + dropdown (closed shadow root), save bar
  lib/                 pure logic, classic scripts shared by background, content and tests
    urlmatch.js        URL / registrable-domain matching (mirrors urlmatch/)
    forms.js           login form detection and filling
    messages.js        validation of every message the background accepts
    native.js          native port client: ids, timeouts, disconnects
  popup/               unlock, this site's logins, search, generator, lock
  options/             native host install steps and a connection test
  offscreen/           Chrome only: clears the clipboard after a copy
  ui/tokens.css        a copy of the Sol ui/kit tokens the pages use
  test/                browser test suite (test/index.html) and the form fixture page
```

## Extension IDs

| Browser | ID |
|---|---|
| Chrome family (unpacked folder or the zip) | `nophamhaahdfdhocbagichcklnlnngdi` |
| Firefox | `raypass@raypass.app` |

The Chrome ID follows from the public key in `manifest.json` (`"key"`), so every unpacked
install gets the same ID and `raypass-native install` can allow it without asking. The private
key is not in the repository and is not needed: Chrome only needs the public key for an unpacked
extension. A build published through the Chrome Web Store gets the store's ID instead; pass it
with `raypass-native install --extension-id <id>` (repeatable).

## 1. Install the native host (once per computer)

```sh
make build                       # or: go build -o bin/raypass-native ./cmd/raypass-native
bin/raypass-native install       # writes the host manifests, remembers where sol is
```

`install` writes `app.raypass.native.json` for every browser whose profile folder exists
(`--all` for all of them):

| | Linux | macOS |
|---|---|---|
| Chrome | `~/.config/google-chrome/NativeMessagingHosts` | `~/Library/Application Support/Google/Chrome/NativeMessagingHosts` |
| Chromium | `~/.config/chromium/NativeMessagingHosts` | `~/Library/Application Support/Chromium/NativeMessagingHosts` |
| Brave | `~/.config/BraveSoftware/Brave-Browser/NativeMessagingHosts` | `~/Library/Application Support/BraveSoftware/Brave-Browser/NativeMessagingHosts` |
| Edge | `~/.config/microsoft-edge/NativeMessagingHosts` | `~/Library/Application Support/Microsoft Edge/NativeMessagingHosts` |
| Vivaldi | `~/.config/vivaldi/NativeMessagingHosts` | `~/Library/Application Support/Vivaldi/NativeMessagingHosts` |
| Firefox | `~/.mozilla/native-messaging-hosts` | `~/Library/Application Support/Mozilla/NativeMessagingHosts` |

On Windows it writes the two manifests to `%LOCALAPPDATA%\Raypass\NativeMessagingHosts` and
prints the `reg add` commands that register them under `HKCU\Software\…\NativeMessagingHosts`.

The manifests point at the binary you ran, so run `install` from where it will stay (not a
`go run` build). The host looks for `sol` in `$SOL_BIN`, then where `install` found it, then
`PATH`, then the usual places (`~/.local/bin`, `~/go/bin`, `/usr/local/bin`, Homebrew). It passes
`SOL_DIR` through, and remembers the `SOL_DIR` that was set when you ran `install` (in
`~/.config/raypass/native.json`), because a browser started from the desktop often has neither
your shell's `PATH` nor its environment. `raypass-native uninstall` removes it all again.

Browsers installed as Flatpak or Snap packages run in a sandbox that cannot start host
programs without extra permissions; use the distribution package or the vendor's build.

## 2. Load the extension

Build the zips (no build step besides zipping): `make extension`, or
`go run ./cmd/extpack` → `build/raypass-extension-chrome.zip` and
`build/raypass-extension-firefox.zip`.

**Chrome, Chromium, Brave, Edge, Vivaldi**: open `chrome://extensions` (`brave://extensions`,
`edge://extensions`), turn on *Developer mode*, then *Load unpacked* and pick the `extension/`
folder (or unzip `raypass-extension-chrome.zip` somewhere and pick that). The ID shown must be
`nophamhaahdfdhocbagichcklnlnngdi`.

**Firefox** (128 or later): for a try-out, open `about:debugging#/runtime/this-firefox`,
*Load Temporary Add-on…*, and pick `build/raypass-extension-firefox.zip` (it stays until
Firefox restarts). To keep it, sign the zip as an unlisted add-on on addons.mozilla.org
(`web-ext sign` or the developer hub) and install the signed `.xpi`; Firefox Developer Edition
and Nightly also accept unsigned add-ons with `xpinstall.signatures.required` set to false.
Firefox asks for the site access on install; allow it on all sites so the form icons appear.

Then open the extension's options page and press **Test connection**: it walks the whole path
(extension → host → sol → Raypass).

## Using it

- A small Raypass icon appears in login fields; click it for this site's logins.
- **Ctrl+Shift+L** fills the best login (Cmd+Shift+L on macOS); **Ctrl+Shift+Y** opens the popup.
- Right-click a field: *Fill with Raypass*.
- The popup unlocks the vault, lists this site's logins (fill, copy username, password, live
  one-time code), searches the whole vault and generates passwords. Copied secrets are cleared
  from the clipboard after Raypass's `clip_clear` setting (30 s by default).
- After you sign in with a login Raypass does not know, a bar offers to save it. Nothing is
  saved until you click *Save* (in the bar or in the popup).

## Security rules the code keeps

- Credentials go only to the frame that asked, and only to the top frame or a frame of the
  same registrable domain. The URL given to `raypass.match`, `raypass.fill` and
  `raypass.save_login` is the one the browser reports for the frame (`sender.url`), never one
  the page could supply; the vault itself refuses to fill an item whose URLs do not match.
- Content scripts can ask only for their own frame's matches and fills; every other action is
  for the extension's own pages, and the host forwards only the ten actions DESIGN.md lists.
- Everything drawn into a page lives in a closed shadow root and reacts only to real clicks
  (`isTrusted`). No secret is written into the page except the value of the field being filled.
  The master password is typed only into the popup, never into a page.
- The extension never calls the vault on its own (no badge counts on navigation), so Raypass's
  auto-lock still locks an idle vault.

## Tests

```sh
make browser-test                 # runs extension/test/index.html with the other suites
go test ./cmd/raypass-native ./cmd/extpack
```

`test/index.html` covers URL matching, message validation, the native client and the form
detector against `test/forms.html` (hidden and off-page honeypots, search boxes, sign-up and
change-password forms, multi-step logins, one-time-code fields, split code boxes, form-less
pages, fields added later, a React-style value tracker). Serve the repository root with any
static server and open the page to run it by hand.

## Known gaps

- The clipboard is cleared unconditionally when the timer runs out (reading it first to check it
  still holds our copy would need the "read clipboard" permission).
- Firefox's sandboxed builds (Flatpak, Snap) cannot start native hosts; see above.
- Forms inside closed shadow roots of the page, and cross-site login iframes (by design), are
  not filled.
