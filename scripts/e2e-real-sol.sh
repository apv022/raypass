#!/usr/bin/env bash
# End-to-end test of Raypass on a real Sol daemon (not memsol): two devices of one person on
# loopback (clearnet transport only, no Tor), Raypass installed on both, every action driven
# through `sol do`, the native messaging host driven with real frames, the feeds checked for
# plaintext, and an edit on one device seen on the other.
#
#   SOL_BIN=/path/to/sol scripts/e2e-real-sol.sh      (or: scripts/e2e-real-sol.sh /path/to/sol)
#
# Needs bash, go, python3 (frame client only) and curl. Everything lives in a temporary
# directory (HOME included) that is removed at the end; KEEP=1 keeps it. Exits non-zero on the
# first failed check. Prints no secrets other than the throwaway test values it made up.
set -euo pipefail

SOL_BIN=${1:-${SOL_BIN:-}}
[ -n "$SOL_BIN" ] && [ -x "$SOL_BIN" ] || { echo "usage: SOL_BIN=/path/to/sol $0" >&2; exit 2; }
SOL_BIN=$(cd "$(dirname "$SOL_BIN")" && pwd)/$(basename "$SOL_BIN")
ROOT=$(cd "$(dirname "$0")/.." && pwd)
T=$(mktemp -d "${TMPDIR:-/tmp}/raypass-e2e.XXXXXX")
PA=$((20000 + RANDOM % 5000)); PB=$((PA + 1))
export SOL_NO_BROWSER=1 SOL_NOTIFY=off

cleanup() {
	use A; "$SOL_BIN" stop >/dev/null 2>&1 || true
	use B; "$SOL_BIN" stop >/dev/null 2>&1 || true
	if [ "${KEEP:-}" = 1 ]; then echo "kept $T"; else rm -rf "$T"; fi
}
trap cleanup EXIT

use() { export HOME=$T/home$1 SOL_DIR=$T/node$1; }
sol() { "$SOL_BIN" "$@"; }
pass=0
ok() { pass=$((pass + 1)); echo "ok   $*"; }
die() { echo "FAIL $*" >&2; exit 1; }
# want CODE CMD...: runs CMD, checks its exit code, leaves its output in $out.
want() {
	local code=$1; shift
	set +e; out=$("$@" 2>&1); local rc=$?; set -e
	[ "$rc" = "$code" ] || die "$* (exit $rc, want $code): $out"
}
has() { case "$out" in *"$1"*) ;; *) die "output lacks $1: $out" ;; esac; }
lacks() { case "$out" in *"$1"*) die "output has $1: $out" ;; esac; }
waitfor() { # waitfor SECONDS CMD...: until CMD succeeds (state is pushed; this only bounds the wait)
	local n=$1; shift
	for _ in $(seq "$n"); do "$@" >/dev/null 2>&1 && return 0; sleep 1; done
	return 1
}
id_of() { sed -n 's/.*"id":"\([0-9a-f]*\)".*/\1/p' <<<"$out" | head -1; }

echo "== build"
(cd "$ROOT" && make -s package >/dev/null && go build -o "$T/raypass-native" ./cmd/raypass-native)

echo "== two devices of one person (A on :$PA, B on :$PB)"
for d in A B; do mkdir -p "$T/home$d"; done
use A; sol init --name Alice --device devA --transport clearnet --listen 127.0.0.1:0 --ui 127.0.0.1:$PA --files "$T/driveA" >/dev/null
sol run >"$T/A.log" 2>&1 &
waitfor 20 sol status || die "daemon A did not start"
use B; sol init --join --device devB --transport clearnet --listen 127.0.0.1:0 --ui 127.0.0.1:$PB --files "$T/driveB" >/dev/null
sol run >"$T/B.log" 2>&1 &
waitfor 20 sol status || die "daemon B did not start"
use A; link=$(sol invite --join)
use B; sol pair "$link" >/dev/null
ok "B joined A"

for d in A B; do
	use $d
	want 0 sol app install "$ROOT/build/raypass-app"
	waitfor 20 sh -c "'$SOL_BIN' apps | grep -q '^raypass .*true'" || die "raypass not running on $d"
done
ok "raypass installed and running on both"

echo "== actions through sol do (device A)"
use A
PW='e2e master password 1'
want 0 sol do raypass.status --json; has '"initialized":false'
want 0 sol do raypass.setup --args - --json <<<"{\"password\":\"$PW\"}"
want 1 sol do raypass.setup --args - --json <<<'{"password":"another one 1"}'; has conflict
want 0 sol do raypass.add --args '{"type":"login","name":"GitHub","user":"alice","pass":"E2E-pass-1","urls":["https://github.com"],"totp":"JBSWY3DPEHPK3PXP"}' --json
GH=$(id_of)
want 0 sol do raypass.add --type note --name Wifi --notes 'E2E-note-secret' --json; NOTE=$(id_of)
want 0 sol do raypass.add --args '{"type":"wallet","name":"BTC","seed":"E2E-seed-words"}' --json
want 0 sol do raypass.add --args '{"type":"card","name":"Visa","number":"4111111111111111","cvv":"123"}' --json
want 0 sol do raypass.list --json; has GitHub; has Wifi; lacks E2E-pass-1
want 0 sol do raypass.get --id "$GH" --json; has E2E-pass-1
want 0 sol do raypass.totp --id "$GH" --json; has '"digits":6'
want 0 sol do raypass.generate --mode words --words 5 --json; has entropy_bits
want 0 sol do raypass.match --url https://github.com/login --json; has "$GH"
want 0 sol do raypass.fill --id "$GH" --url https://github.com/login --json; has E2E-pass-1
want 1 sol do raypass.fill --id "$GH" --url https://evil.example/ --json; has forbidden
want 0 sol do raypass.save_login --url https://github.com/login --user alice --pass E2E-pass-2 --json; has '"created":false'
want 0 sol do raypass.get --id "$GH" --json; has E2E-pass-2; has history
want 0 sol do raypass.trash --ids "$NOTE" --dry-run --json; has dry_run
want 0 sol do raypass.trash --ids "$NOTE" --json
want 0 sol do raypass.restore --ids "$NOTE" --json
want 0 sol do raypass.trash --ids "$NOTE" --json
want 0 sol do raypass.purge --all_trash --json; has '"purged":1'
want 0 sol do raypass.export --format encrypted --password 'e2e export pw' --json; has raypass_export
want 2 sol do raypass.export --format encrypted --json
want 0 sol do raypass.import --args - --json <<<'{"format":"chrome_csv","data":"name,url,username,password,note\nShop,https://shop.example,carol,E2E-pass-3,\n"}'
has '"imported":1'
want 0 sol do --help raypass; has raypass.fill
want 0 sol do raypass.add --help; has 'Approval: agents'
ok "every action answers on a real daemon"

PW2='e2e master password 2'
want 1 sol do raypass.change_password --args - --json <<<"{\"old\":\"wrong pw 1\",\"new\":\"$PW2\"}"
want 0 sol do raypass.change_password --args - --json <<<"{\"old\":\"$PW\",\"new\":\"$PW2\"}"
want 0 sol do raypass.lock --json
want 1 sol do raypass.list --json; has locked
want 1 sol do raypass.unlock --args - --json --dry-run <<<"{\"password\":\"$PW\"}"
want 0 sol do raypass.unlock --args - --json <<<"{\"password\":\"$PW2\"}"
ok "change_password, lock, unlock"

echo "== native messaging host"
use A
out=$(SOL_BIN=$SOL_BIN python3 - "$T/raypass-native" "$GH" <<'EOF'
import json, struct, subprocess, sys
p = subprocess.Popen([sys.argv[1], "chrome-extension://abcdefghijklmnopabcdefghijklmnop/"], stdin=subprocess.PIPE, stdout=subprocess.PIPE)
reqs = [{"action": "raypass.match", "args": {"url": "https://github.com/x"}},
        {"action": "raypass.fill", "args": {"id": sys.argv[2], "url": "https://github.com/x"}},
        {"action": "raypass.purge", "args": {"all_trash": True}}]
for i, r in enumerate(reqs):
    r["id"] = i; b = json.dumps(r).encode()
    p.stdin.write(struct.pack("=I", len(b)) + b); p.stdin.flush()
    n = struct.unpack("=I", p.stdout.read(4))[0]
    print(p.stdout.read(n).decode())
p.stdin.close(); sys.exit(p.wait())
EOF
)
has '"id":0,"ok":true'; has E2E-pass-2; has 'not allowed through the browser extension'
ok "native host: match, fill, refusal"

echo "== the UI is served through the daemon"
want 0 curl -fsS --noproxy '*' "http://raypass.localhost:$PA/"; has '<title>Raypass</title>'
ok "UI at raypass.localhost:$PA"

echo "== feeds hold ciphertext only"
for s in E2E-pass-1 E2E-pass-2 E2E-pass-3 E2E-note-secret E2E-seed-words 4111111111111111 GitHub "$PW" "$PW2"; do
	if grep -rlaF --exclude-dir=apps -- "$s" "$T/nodeA" "$T/A.log"; then die "plaintext in A's data or log"; fi
done
ok "no plaintext in A's data directory (apps/ holds the binary) or log"

echo "== device B: same vault, same password, edits travel both ways"
use B
want 1 sol do raypass.unlock --args - --json <<<"{\"password\":\"$PW\"}"
waitfor 20 sh -c "printf '%s' '{\"password\":\"$PW2\"}' | '$SOL_BIN' do raypass.unlock --args - --json" || die "B could not unlock"
waitfor 20 sh -c "'$SOL_BIN' do raypass.list --json | grep -q GitHub" || die "B does not see A's items"
want 0 sol do raypass.update --args "{\"id\":\"$GH\",\"name\":\"GitHub edited on B\"}" --json
use A
waitfor 20 sh -c "'$SOL_BIN' do raypass.list --json | grep -q 'GitHub edited on B'" || die "A does not see B's edit"
ok "B unlocked with A's password; B's edit reached A"

echo "PASS ($pass checks)"
