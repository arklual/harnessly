#!/bin/sh
# Run with a reviewed Chrome for Testing version, then update Version and the three hashes in provision/download.go.
# The public archives do not publish SHA-256 manifests; hashes must come from full HTTPS downloads.
set -eu
version=${1:?usage: tools/update-browser-checksums.sh VERSION}
cache=$(mktemp -d)
trap 'rm -rf "$cache"' EXIT
for platform in linux64 mac-arm64 mac-x64; do
 archive="$cache/chrome-$platform.zip"
 curl -fL --retry 3 "https://storage.googleapis.com/chrome-for-testing-public/$version/$platform/chrome-$platform.zip" \
  -o "$archive"
 if command -v sha256sum >/dev/null 2>&1; then sha256sum "$archive"; else shasum -a 256 "$archive"; fi
done
