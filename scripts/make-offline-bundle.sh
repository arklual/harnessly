#!/bin/sh
# Builds the offline bundle for closed networks: the release archives, their checksums and install.sh in one folder
# (and one .tar next to it). Copy the .tar to the closed machine and run
#
#   tar -xf harnessly-offline-X.Y.Z.tar && sh harnessly-offline-X.Y.Z/install.sh --offline harnessly-offline-X.Y.Z
#
# tmux and git are not in the bundle: they come from the machine's own package source.
#
#   scripts/make-offline-bundle.sh X.Y.Z [OUT_DIR] [PLATFORM ...]     PLATFORM is linux_amd64 (default), linux_arm64, darwin_arm64 ...
#
#   scripts/make-offline-bundle.sh --hub HUB_URL [OUT_DIR]             a bundle of a hub's installer and its /dl files
#
# The hub bundle holds install.sh (the hub's own /install, with its address in it) and a copy of /dl; on the closed
# machine run  sh install.sh --token T --offline . . The hub must still be reachable there to register the machine.
# HARNESSLY_MIRROR=URL takes the files from a mirror instead of github.com; HARNESSLY_CA_FILE is honored by curl.
# The bundle is a build output: do not commit it.
set -eu

[ $# -ge 1 ] || {
	sed -n '2,15p' "$0" >&2
	exit 2
}
if [ "$1" = --hub ]; then
	[ $# -ge 2 ] || {
		sed -n '2,15p' "$0" >&2
		exit 2
	}
	HUB=${2%/}
	OUT=${3:-.}
	DIR=$OUT/harnessly-hub-offline
	if [ -n "${HARNESSLY_CA_FILE:-}" ]; then export CURL_CA_BUNDLE="$HARNESSLY_CA_FILE"; fi
	mkdir -p "$DIR"
	curl -fsSL --retry 3 -o "$DIR/install.sh" "$HUB/install" || {
		echo "could not download $HUB/install" >&2
		exit 1
	}
	curl -fsSL --retry 3 -o "$DIR/manifest.json" "$HUB/dl/manifest.json" || {
		echo "could not download $HUB/dl/manifest.json" >&2
		exit 1
	}
	got=0
	for k in agent runtime harnessly; do
		for o in linux darwin; do
			for a in amd64 arm64; do
				# A platform the hub does not serve is left out; the installer says so if that one is asked for.
				if curl -fsSL --retry 3 -o "$DIR/$k-$o-$a" "$HUB/dl/$k-$o-$a" 2>/dev/null; then got=$((got + 1)); else rm -f "$DIR/$k-$o-$a"; fi
			done
		done
	done
	[ "$got" -gt 0 ] || {
		echo "the hub served no program files" >&2
		exit 1
	}
	tar -C "$OUT" -cf "$OUT/harnessly-hub-offline.tar" harnessly-hub-offline
	echo "$OUT/harnessly-hub-offline.tar"
	exit 0
fi
VERSION=${1#v}
OUT=${2:-.}
if [ $# -ge 2 ]; then shift 2; else shift; fi
PLATFORMS=${*:-linux_amd64}
REPO=ly-solutions/harnessly
BASE=${HARNESSLY_MIRROR:-https://github.com/$REPO/releases/download}/v$VERSION
HERE=$(cd "$(dirname "$0")" && pwd)
DIR=$OUT/harnessly-offline-$VERSION

if [ -n "${HARNESSLY_CA_FILE:-}" ]; then export CURL_CA_BUNDLE="$HARNESSLY_CA_FILE"; fi
mkdir -p "$DIR"
for f in checksums.txt checksums.txt.sigstore.json; do
	curl -fsSL --retry 3 -o "$DIR/$f" "$BASE/$f" || [ "$f" != checksums.txt ] || {
		echo "could not download $BASE/$f" >&2
		exit 1
	}
done
for p in $PLATFORMS; do
	a=harnessly_${VERSION}_$p.tar.gz
	curl -fsSL --retry 3 -o "$DIR/$a" "$BASE/$a" || {
		echo "could not download $BASE/$a" >&2
		exit 1
	}
	want=$(awk -v f="$a" '$2 == f { print $1 }' "$DIR/checksums.txt")
	got=$(sha256sum "$DIR/$a" | cut -d' ' -f1)
	[ -n "$want" ] && [ "$got" = "$want" ] || {
		echo "$a does not match checksums.txt" >&2
		exit 1
	}
done
cp "$HERE/../install.sh" "$DIR/install.sh"
cat >"$DIR/README.txt" <<TXT
harnessly $VERSION, offline bundle ($PLATFORMS).
  sh install.sh --offline . [--ca-file FILE] [--proxy URL] [--check]
tmux and git must be installed on the machine. Nothing is downloaded.
TXT
tar -C "$OUT" -cf "$OUT/harnessly-offline-$VERSION.tar" "harnessly-offline-$VERSION"
echo "$OUT/harnessly-offline-$VERSION.tar"
