#!/usr/bin/env bash
# Builds build/raypass.apk with no Gradle (like Sol's android/build.sh): aapt2 compile + link,
# javac, a dexer (D8 if $TC/r8.jar exists, else dx), zipalign, apksigner (a debug key kept in TC).
#
#   android/toolchain.sh   once: fetches android.jar (API 34) and dx into TC, checks aapt2/zipalign/apksigner
#   android/build.sh       build/raypass.apk (debug key)
#   RELEASE=1 android/build.sh   build/raypass-release.apk signed with RAYPASS_KEYSTORE / RAYPASS_KEY_ALIAS /
#                          RAYPASS_KEYSTORE_PASS (environment only, never argv)
#
# Shared with the Go app and packed as raw resources: ../gen/wordlist.txt (passphrases) and
# ../urlmatch/suffixes.txt (registrable domains). The pure-Java tests are android/test.sh.
set -euo pipefail
cd "$(dirname "$0")"
HERE=$PWD
TC=${TC:-$HOME/raypass-mobile/.toolchain}
OUT=$HERE/build
MIN_SDK=26
TARGET_SDK=34
VERSION_NAME=${RAYPASS_VERSION:-0.1.0-dev}
VERSION_CODE=${RAYPASS_VERSION_CODE:-1}
jar=$TC/android.jar
unset _JAVA_OPTIONS JAVA_TOOL_OPTIONS 2>/dev/null || true
for t in aapt2 zipalign apksigner javac keytool; do command -v $t >/dev/null || { echo "missing $t (run android/toolchain.sh)" >&2; exit 1; }; done
[ -f "$jar" ] || { echo "no $jar (run android/toolchain.sh)" >&2; exit 1; }
for f in ../gen/wordlist.txt ../urlmatch/suffixes.txt; do [ -f $f ] || { echo "missing $f" >&2; exit 1; }; done
if [ -n "${RELEASE:-}" ]; then
  for v in RAYPASS_KEYSTORE RAYPASS_KEY_ALIAS RAYPASS_KEYSTORE_PASS; do
    [ -n "${!v:-}" ] || { echo "RELEASE=1 needs $v" >&2; exit 1; }
  done
  APK=$OUT/raypass-release.apk
else
  APK=$OUT/raypass.apk
  if [ ! -f "$TC/debug.keystore" ]; then
    keytool -genkeypair -keystore "$TC/debug.keystore" -storetype PKCS12 -storepass android -keypass android \
      -alias raypass-debug -keyalg RSA -keysize 2048 -validity 10000 -dname "CN=Raypass debug build" >/dev/null 2>&1
    chmod 600 "$TC/debug.keystore"
  fi
fi

rm -rf "$OUT/gen" "$OUT/classes" "$OUT/dex" "$OUT/res-extra" "$APK" && mkdir -p "$OUT/gen" "$OUT/classes" "$OUT/dex" "$OUT/res-extra/raw"
cp ../gen/wordlist.txt "$OUT/res-extra/raw/wordlist.txt"
cp ../urlmatch/suffixes.txt "$OUT/res-extra/raw/suffixes.txt"

aapt2 compile --dir res -o "$OUT/res.zip"
aapt2 compile "$OUT/res-extra/raw/wordlist.txt" -o "$OUT/res-extra/"
aapt2 compile "$OUT/res-extra/raw/suffixes.txt" -o "$OUT/res-extra/"
aapt2 link -o "$OUT/unsigned.apk" -I "$jar" --manifest AndroidManifest.xml --java "$OUT/gen" \
  --min-sdk-version $MIN_SDK --target-sdk-version $TARGET_SDK --version-name "$VERSION_NAME" --version-code "$VERSION_CODE" \
  "$OUT/res.zip" "$OUT"/res-extra/*.flat

# Java 8 bytecode against the API 34 framework classes; the JDK 8 API (--release 8) is the
# subset Android 8 (API 26) also has, so nothing newer than minSdk slips in from java.*.
javac -Xlint:all -Xlint:-options -Xlint:-classfile -Xlint:-serial -Xlint:-deprecation -Xlint:-rawtypes -Xlint:-unchecked \
  --release 8 -classpath "$jar" -d "$OUT/classes" $(find src "$OUT/gen" -name '*.java')

if [ -f "$TC/r8.jar" ]; then
  java -cp "$TC/r8.jar" com.android.tools.r8.D8 --lib "$jar" --min-api $MIN_SDK --release --output "$OUT/dex" $(find "$OUT/classes" -name '*.class')
else
  # dx cannot desugar lambdas: ART has no LambdaMetafactory, so an invokedynamic would crash at runtime.
  if find "$OUT/classes" -name '*.class' -exec javap -c -p {} + 2>/dev/null | grep -q invokedynamic; then
    echo "invokedynamic (a lambda or method reference) found: dx cannot desugar it; use anonymous classes" >&2; exit 1
  fi
  java -cp "$TC/dx.jar" com.android.dx.command.Main --dex --min-sdk-version=$MIN_SDK --output="$OUT/dex/classes.dex" "$OUT/classes"
fi
(cd "$OUT/dex" && zip -q "$OUT/unsigned.apk" classes.dex)
zipalign -f -p 4 "$OUT/unsigned.apk" "$OUT/aligned.apk"
if [ -n "${RELEASE:-}" ]; then
  apksigner sign --ks "$RAYPASS_KEYSTORE" --ks-pass env:RAYPASS_KEYSTORE_PASS --key-pass env:RAYPASS_KEYSTORE_PASS \
    --ks-key-alias "$RAYPASS_KEY_ALIAS" --out "$APK" "$OUT/aligned.apk"
else
  apksigner sign --ks "$TC/debug.keystore" --ks-pass pass:android --key-pass pass:android --out "$APK" "$OUT/aligned.apk"
fi
apksigner verify "$APK"
rm -f "$OUT/unsigned.apk" "$OUT/aligned.apk" "$OUT/res.zip" "$APK.idsig"
echo "built $APK ($(du -h "$APK" | cut -f1))"
