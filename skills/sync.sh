#!/usr/bin/env bash
# Keeps skills/upstream equal to the commits pinned in skills/SOURCES.
#
#   skills/sync.sh           fetch every pinned commit and rewrite skills/upstream and skills/MANIFEST.sha256
#   skills/sync.sh --check   fetch every pinned commit and fail when skills/upstream differs from it
#
# To test an unpublished commit, point a source at a local checkout of it:
#   SKILLS_SRC_agent_skills=/path/to/agent-skills skills/sync.sh
# (the name with "-" written as "_"). The commit in SOURCES must exist in that checkout.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
mode=sync
[ "${1:-}" = "--check" ] && mode=check
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

fetch() { # name repo commit -> $work/name holds the tree of that commit
	local name=$1 repo=$2 commit=$3 var local_src
	var="SKILLS_SRC_${name//-/_}"
	local_src=${!var:-}
	mkdir -p "$work/$name"
	if [ -n "$local_src" ]; then
		git -C "$local_src" archive "$commit" | tar -x -C "$work/$name"
	else
		git -C "$work" init -q "$work/$name.git"
		git -C "$work/$name.git" fetch -q --depth 1 "$repo" "$commit"
		git -C "$work/$name.git" archive FETCH_HEAD | tar -x -C "$work/$name"
	fi
}

want="$work/want"
while read -r name repo commit paths; do
	case "$name" in ''|'#'*) continue ;; esac
	[[ $commit =~ ^[0-9a-f]{40}$ ]] || { echo "sync: $name: the commit must be a full 40-digit hash" >&2; exit 1; }
	fetch "$name" "$repo" "$commit"
	for p in $paths; do
		[ -e "$work/$name/$p" ] || { echo "sync: $name: $p is not in $commit" >&2; exit 1; }
		mkdir -p "$want/$name/$(dirname "$p")"
		cp -a "$work/$name/$p" "$want/$name/$p"
	done
done <"$here/SOURCES"

manifest() { (cd "$1" && find . -type f ! -name MANIFEST.sha256 | LC_ALL=C sort | xargs sha256sum); }

if [ "$mode" = check ]; then
	if ! diff -ru "$want" "$here/upstream" >&2; then
		echo "sync: skills/upstream differs from the commits pinned in skills/SOURCES; run skills/sync.sh" >&2
		exit 1
	fi
	manifest "$here/upstream" | diff -u - "$here/MANIFEST.sha256" >&2 || { echo "sync: MANIFEST.sha256 is stale; run skills/sync.sh" >&2; exit 1; }
	echo "skills/upstream matches the pinned commits"
	exit 0
fi

rm -rf "$here/upstream"
mkdir -p "$here/upstream"
cp -a "$want/." "$here/upstream/"
manifest "$here/upstream" >"$here/MANIFEST.sha256"
echo "skills/upstream updated from the pinned commits"
