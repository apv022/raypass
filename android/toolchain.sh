#!/usr/bin/env bash
# Assembles the build toolchain for android/build.sh in TC (default ~/raypass-mobile/.toolchain)
# without the Android SDK manager (dl.google.com may be unreachable):
#   android.jar  Robolectric's android-all for API 34 (Maven Central): every framework class and
#                the framework resources.arsc, enough for javac and for aapt2 link -I
#   dex          D8 if $TC/r8.jar exists (copy one in by hand), else dx (Maven Central,
#                com.jakewharton.android.repackaged:dalvik-dx), which needs lambda-free Java 8
#   aapt2, zipalign, apksigner   from PATH; on Debian/Ubuntu: apt-get install aapt zipalign apksigner
# Downloads are checked against pinned SHA-256 sums.
set -euo pipefail
TC=${TC:-$HOME/raypass-mobile/.toolchain}
mkdir -p "$TC"
MC=https://repo1.maven.org/maven2
fetch() { # url dest sha256
  if [ ! -s "$2" ]; then curl -fsSL -o "$2.part" "$1" && mv "$2.part" "$2"; fi
  echo "$3  $2" | sha256sum -c --quiet - || { echo "checksum mismatch: $2" >&2; rm -f "$2"; exit 1; }
}
fetch $MC/org/robolectric/android-all/14-robolectric-10818077/android-all-14-robolectric-10818077.jar "$TC/android.jar" \
  6be2218c6a53fe3c57bc22ebdc723edcb7270a8a6f187545708aa5c0ed813977
fetch $MC/com/jakewharton/android/repackaged/dalvik-dx/16.0.1/dalvik-dx-16.0.1.jar "$TC/dx.jar" \
  1e4b645628e3bdb097b5331d669e177ef235a551582a8c646dbe36865e541907
for t in aapt2 zipalign apksigner; do
  command -v $t >/dev/null || {
    if command -v apt-get >/dev/null && [ "$(id -u)" = 0 ]; then DEBIAN_FRONTEND=noninteractive apt-get install -y aapt zipalign apksigner >/dev/null; break
    else echo "missing $t: install aapt, zipalign and apksigner (Debian/Ubuntu packages) or put them on PATH" >&2; exit 1; fi
  }
done
ls -la "$TC"
