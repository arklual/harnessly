#!/usr/bin/env bash
# Exports one commit's tree into a fresh repository with a single commit and no history, for publishing elsewhere.
# It refuses when the exported tree holds a forbidden word or gitleaks finds a secret in it; nothing is pushed.
#
#   PUBLISH_FORBIDDEN='word|other|/some/path' \
#   PUBLISH_AUTHOR_NAME='Jane Doe' PUBLISH_AUTHOR_EMAIL='jane@example.org' \
#   tools/publish-tree.sh <out-dir> <message> [commit]
#
# PUBLISH_FORBIDDEN    extended regex matched case-insensitively against every exported file and file name
#                      (required: the words belong to the deployment, so they are not written here)
# PUBLISH_AUTHOR_*     author of the commit; PUBLISH_COMMITTER_NAME/EMAIL default to the author
# PUBLISH_DATE         commit date (default: the exported commit's committer date)
# GITLEAKS             gitleaks binary (default: gitleaks on PATH)
set -euo pipefail

die() {
  echo "publish-tree: $*" >&2
  exit 1
}

[ $# -ge 2 ] && [ $# -le 3 ] || die "usage: $0 <out-dir> <message> [commit]"
out=$1 message=$2 rev=${3:-HEAD}
[ -n "$message" ] || die "the commit message is empty"
[ -n "${PUBLISH_FORBIDDEN:-}" ] || die "PUBLISH_FORBIDDEN is not set"
[ -n "${PUBLISH_AUTHOR_NAME:-}" ] && [ -n "${PUBLISH_AUTHOR_EMAIL:-}" ] || die "PUBLISH_AUTHOR_NAME and PUBLISH_AUTHOR_EMAIL must be set"
committer_name=${PUBLISH_COMMITTER_NAME:-$PUBLISH_AUTHOR_NAME}
committer_email=${PUBLISH_COMMITTER_EMAIL:-$PUBLISH_AUTHOR_EMAIL}
gitleaks=${GITLEAKS:-gitleaks}
command -v "$gitleaks" >/dev/null || die "gitleaks not found (set GITLEAKS)"

src=$(git rev-parse --show-toplevel)
commit=$(git -C "$src" rev-parse --verify --quiet "$rev^{commit}") || die "no commit $rev"
date=${PUBLISH_DATE:-$(git -C "$src" show -s --format=%cI "$commit")}

if [ -e "$out" ] && [ -n "$(ls -A "$out")" ]; then
  die "$out exists and is not empty"
fi
mkdir -p "$out"
out=$(cd "$out" && pwd)
fail() {
  rm -rf "$out"
  die "$@"
}

git -C "$src" archive --format=tar "$commit" | tar -x -C "$out"

# File names and contents, binary files included (-a).
hits=$(cd "$out" && {
  find . -type f | grep -iE -- "$PUBLISH_FORBIDDEN" || true
  grep -rlaiE -- "$PUBLISH_FORBIDDEN" . || true
} | sort -u)
if [ -n "$hits" ]; then
  echo "$hits" >&2
  fail "forbidden words in the files above; nothing exported"
fi

# The tree's own allowlists when it has them (a commit without them is scanned with the default rules).
config=()
[ -f "$out/.gitleaks.toml" ] && config=(--config "$out/.gitleaks.toml")
if ! "$gitleaks" dir "$out" "${config[@]}" --no-banner --redact --log-level warn; then
  fail "gitleaks found secrets; nothing exported"
fi

git -C "$out" init -q -b main
git -C "$out" add -A
GIT_AUTHOR_NAME=$PUBLISH_AUTHOR_NAME GIT_AUTHOR_EMAIL=$PUBLISH_AUTHOR_EMAIL GIT_AUTHOR_DATE=$date \
  GIT_COMMITTER_NAME=$committer_name GIT_COMMITTER_EMAIL=$committer_email GIT_COMMITTER_DATE=$date \
  git -C "$out" -c commit.gpgsign=false commit -q --no-verify -F - <<<"$message"

[ "$(git -C "$out" rev-parse 'HEAD^{tree}')" = "$(git -C "$src" rev-parse "$commit^{tree}")" ] ||
  fail "the exported tree differs from that of $commit"
echo "publish-tree: $out at $(git -C "$out" rev-parse HEAD), the tree of $commit"
