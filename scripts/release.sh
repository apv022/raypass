#!/usr/bin/env bash
# Builds every Raypass release file into dist/:
#
#   raypass-<v>-<os>-<arch>.tar.gz (.zip for Windows), for linux, darwin and windows on amd64 and arm64:
#       raypass-app/        app.json + raypass binary + icon.svg, for `sol app install raypass-app`
#       raypass-native      the browser extension's native messaging host (`raypass-native install`)
#       README.md, LICENSE, INSTALL.txt
#   raypass-extension-<v>-chrome.zip, raypass-extension-<v>-firefox.zip
#   raypass-<v>.apk         the Android app (skipped with SKIP_ANDROID=1)
#   SHA256SUMS
#
#   scripts/release.sh 0.1.0
#
# The version must equal app.json's and extension/manifest.json's, so a tag can't ship files
# that call themselves something else. Android: with RAYPASS_KEYSTORE, RAYPASS_KEY_ALIAS and
# RAYPASS_KEYSTORE_PASS set (env only, never argv) the APK is signed with that key; otherwise with
# the toolchain's debug key, and the file is named raypass-<v>-debugkey.apk so nobody mistakes it
# for a release-signed build (Sol binds the app to its signing certificate, so switching keys
# later means reconnecting Raypass to Sol).
set -euo pipefail
cd "$(dirname "$0")/.."
V=${1:?usage: scripts/release.sh VERSION (e.g. 0.1.0)}
V=${V#v}
[[ $V =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]] || { echo "bad version $V" >&2; exit 2; }
for f in app.json extension/manifest.json; do
  got=$(sed -n 's/^ *"version": *"\([^"]*\)".*/\1/p' "$f" | head -1)
  [ "$got" = "${V%%-*}" ] || { echo "$f says version $got, releasing $V: bump it first" >&2; exit 2; }
done

DIST=dist
rm -rf "$DIST" && mkdir -p "$DIST/stage"
export CGO_ENABLED=0

for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
  os=${target%/*} arch=${target#*/}
  exe=""; [ "$os" = windows ] && exe=.exe
  name=raypass-$V-$os-$arch
  d=$DIST/stage/$name
  mkdir -p "$d/raypass-app"
  GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w" -o "$d/raypass-app/raypass$exe" ./cmd/raypass
  GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w" -o "$d/raypass-native$exe" ./cmd/raypass-native
  sed "s/\"exec\": \"raypass\"/\"exec\": \"raypass$exe\"/" app.json > "$d/raypass-app/app.json"
  cp ui/icon.svg "$d/raypass-app/icon.svg"
  cp README.md LICENSE "$d/"
  cat > "$d/INSTALL.txt" <<EOF
Raypass $V ($os/$arch)

1. Install the app into Sol (Sol must be set up on this computer):

     sol app install raypass-app

   then open http://raypass.localhost:7777/ (or: sol open raypass) and create your vault.

2. Browser extension: install the extension (Chrome/Brave/Edge: raypass-extension-$V-chrome.zip,
   Firefox: raypass-extension-$V-firefox.zip; see extension/README.md in the source), then put
   raypass-native$exe somewhere permanent and run once:

     raypass-native$exe install

3. Command line: sol do --help raypass
EOF
  if [ "$os" = windows ]; then
    (cd "$DIST/stage" && zip -qr "../$name.zip" "$name")
  else
    tar -C "$DIST/stage" -czf "$DIST/$name.tar.gz" "$name"
  fi
done

go run ./cmd/extpack -out "$DIST/stage/ext"
cp "$DIST/stage/ext/raypass-extension-chrome.zip" "$DIST/raypass-extension-$V-chrome.zip"
cp "$DIST/stage/ext/raypass-extension-firefox.zip" "$DIST/raypass-extension-$V-firefox.zip"

if [ -z "${SKIP_ANDROID:-}" ]; then
  # versionCode must grow with every release, pre-releases included: X.Y.Z-anything.N gets N
  # (1-98) in the last two digits, a final X.Y.Z gets 99, so 0.2.0-beta.3 < 0.2.0 < 0.2.1-beta.1.
  IFS=. read -r ma mi pa <<<"${V%%-*}"
  n=99
  if [[ $V == *-* ]]; then
    n=${V##*.}
    [[ $n =~ ^[0-9]+$ && $n -ge 1 && $n -le 98 ]] || { echo "pre-release $V must end in .N (1-98) for the Android versionCode" >&2; exit 2; }
  fi
  export RAYPASS_VERSION=$V RAYPASS_VERSION_CODE=$((ma * 1000000 + mi * 10000 + pa * 100 + n))
  if [ -n "${RAYPASS_KEYSTORE:-}" ]; then
    RELEASE=1 android/build.sh
    cp android/build/raypass-release.apk "$DIST/raypass-$V.apk"
  else
    android/build.sh
    cp android/build/raypass.apk "$DIST/raypass-$V-debugkey.apk"
  fi
fi

rm -rf "$DIST/stage"
(cd "$DIST" && sha256sum -- * > SHA256SUMS)
ls -la "$DIST"
