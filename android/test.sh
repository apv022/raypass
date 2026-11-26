#!/usr/bin/env bash
# Runs the pure-Java tests on the JDK (no device, no Android SDK): compiles src/app/raypass/core
# (which must not import android.*) and test/, then runs app.raypass.test.Main against the shared
# vectors in ../testdata/vectors.json and the word list in ../gen/wordlist.txt.
set -euo pipefail
cd "$(dirname "$0")"
OUT=build/test-classes
rm -rf "$OUT" && mkdir -p "$OUT"
if grep -rln '^import android\.' src/app/raypass/core; then
  echo "src/app/raypass/core must not depend on android.*" >&2; exit 1
fi
# The APK must ship the Go app's suffix list (UrlMatch.BUILTIN is checked against it by the tests).
grep -q 'cp ../urlmatch/suffixes.txt "$OUT/res-extra/raw/suffixes.txt"' build.sh || {
  echo "build.sh must pack ../urlmatch/suffixes.txt as res/raw/suffixes.txt" >&2; exit 1; }
grep -q 'cp ../urlmatch/equivalent.txt "$OUT/res-extra/raw/equivalent.txt"' build.sh || {
  echo "build.sh must pack ../urlmatch/equivalent.txt as res/raw/equivalent.txt" >&2; exit 1; }
unset _JAVA_OPTIONS JAVA_TOOL_OPTIONS 2>/dev/null || true
javac -Xlint:all -Xlint:-options -Xlint:-serial -Xlint:-rawtypes -Xlint:-unchecked --release 8 -d "$OUT" \
  $(find src/app/raypass/core test -name '*.java')
java -cp "$OUT" app.raypass.test.Main ..
