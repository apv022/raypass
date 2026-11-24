# Raypass for Android

The Android app of Raypass (`app.raypass`): an external Sol app that keeps your vault in your
Sol feeds `keys` and `vault`, encrypted on the phone (design/DESIGN.md "Crypto", byte for byte
the same as the Go app; checked against `testdata/vectors.json`). It is built with the Android
framework and `javax.crypto` only: no Gradle, no AndroidX, no third-party code.

## What it does

- **Connect** to Sol once (`app.sol.action.CONNECT`, app id `raypass`, scope `feeds`); the token
  and API address stay in the app's private storage. Handles Sol missing, denied, not set up, and
  a revoked token (401: connect again).
- **Sync** with Sol's loopback API: `GET /v1/whoami`, read `<person>/raypass/keys` and `…/vault`
  from the cached position, `POST /v1/feeds/{feed}/append`, and the events stream
  (`/v1/events`, resumed with `Last-Event-ID`; reconnects with backoff, never polls). Every
  entry is cached as ciphertext in `files/feeds/`, so the app opens and unlocks offline; writes
  made offline wait in an outbox and are appended in order.
- **Vault**: create (only after the `keys` feed was read, so a phone never starts a second
  vault), unlock (PBKDF2-SHA256, 600 000 rounds; throttled after 5 failures), optional
  biometric unlock (the vault key wrapped by an Android Keystore key that needs a strong
  biometric for each use), change master password, lock, auto-lock after `s.auto_lock` minutes.
  Locking wipes the key and every decrypted value.
- **Items**: list with search and filters (favourites, logins, notes, cards, identities,
  wallets, trash), detail with copy (marked sensitive, cleared after `s.clip_clear` seconds),
  show/hide, live TOTP (RFC 6238, SHA1/256/512, Steam), password history; add/edit every type
  with custom fields; trash, restore, delete forever. `FLAG_SECURE` on every window.
- **Generator**: characters or EFF words (`../gen/wordlist.txt`, packed as `res/raw/wordlist`),
  entropy computed like the Go app.
- **Autofill** (`RaypassAutofillService`): finds username/password fields from autofill hints,
  HTML attributes, input types and id/hint words; offers logins matching
  `androidapp://<package>`, or the web domain reported by a known browser (any other app could
  claim any domain, so only browsers are believed); "Unlock Raypass" when locked; saves new or
  changed logins (same user and domain updates the password and keeps the old one in history).

## Build

```sh
android/toolchain.sh   # once: android.jar (API 34) + dx into ~/raypass-mobile/.toolchain, checks tools
android/build.sh       # build/raypass.apk, signed with a debug key kept in the toolchain directory
android/test.sh        # the pure-Java tests on the JDK (no device)
```

The toolchain needs no Android SDK manager: `android.jar` is Robolectric's `android-all` for
API 34 from Maven Central (all framework classes and the framework `resources.arsc`, which
`aapt2 link` accepts), the dexer is D8 if you put an `r8.jar` in the toolchain directory, else
`dx` (Maven Central, `com.jakewharton.android.repackaged:dalvik-dx`), and `aapt2`, `zipalign`
and `apksigner` come from the Debian/Ubuntu packages `aapt zipalign apksigner`. Because `dx`
cannot desugar lambdas (ART has no LambdaMetafactory), the code uses anonymous classes and
`build.sh` refuses any `invokedynamic`. A release build: `RELEASE=1` with `RAYPASS_KEYSTORE`,
`RAYPASS_KEY_ALIAS` and `RAYPASS_KEYSTORE_PASS` in the environment (never argv).

Code layout: `src/app/raypass/core/` is pure Java (no `android.*`; `test.sh` refuses it): JSON,
crypto, keyring, entries, CRDT map and HLC, item model, TOTP, URL matching, autofill field
heuristics, generator, session. `src/app/raypass/` is the Android side. `test/` is a tiny
`main()` test runner (JUnit would be a third-party library).

## Install and connect

1. Install Sol on the phone and set it up.
2. `adb install -r android/build/raypass.apk`
3. Open Raypass, tap **Connect to Sol**, and allow it in Sol.
4. Raypass reads your keys from Sol. If you already have a vault (on your laptop, say), unlock it
   with your master password; otherwise create one.

## Use it for autofill

Settings → Passwords & accounts (or System → Languages & input → Autofill service, depending on
the Android version) → Autofill service → **Raypass**. Raypass's own Settings screen has a
**Use Raypass for autofill** button that opens that page. Biometric unlock is in the same
Settings screen, once the vault is unlocked.

## Not done yet

- Import and export (the Go app's `raypass.import` / `raypass.export`) are not on the phone;
  use the desktop app.
- The health report is not shown on the phone.
- A one-time-code field is recognised but not filled.
- Not tested on a device or emulator in this environment (none available); it compiles against
  API 34, every API above 26 is guarded by a version check (checked by compiling against the
  API 26 framework), and the logic below the UI is covered by `test.sh`.
