#!/usr/bin/env bash
# Builds the public tree from one internal commit and makes it ONE commit on top of the public line, in a local branch.
# Nothing is fetched or pushed: the public line is whatever OSS_BASE points at in this clone.
#
#   OSS_FORBIDDEN_FILE=tools/oss-forbidden.txt tools/oss-export.sh [commit]
#
# commit               the internal commit to publish (default HEAD)
# OSS_BASE             the public line the commit goes on (default origin/main)
# OSS_BRANCH           the local branch it lands in (default oss/sync-<short sha of commit>); an existing one is refused
# OSS_EXCLUDE_FILE     paths left out, git pathspecs as globs (default tools/oss-exclude.txt of the exported commit)
# OSS_FORBIDDEN_FILE   the leak check's forbidden words (see tools/oss-leak-check.sh; default tools/oss-forbidden.txt of
#                      this checkout when it has one)
# OSS_AUTHOR_NAME/EMAIL  author and committer of the sync commit (default: git config user.name/user.email)
# GITLEAKS             gitleaks binary for the leak check
#
# The commit's message is "sync: internal main <sha>". The tree is the internal one minus the excluded paths, with the
# internal tracker's provenance tags out of comments and Markdown (see `strip:` below), so a file
# only the public line has is deleted by the sync: the script lists such files before it commits, for a person to read.
set -euo pipefail

die() {
  echo "oss-export: $*" >&2
  exit 1
}

[ $# -le 1 ] || die "usage: $0 [commit]"
src=$(git rev-parse --show-toplevel)
here=$(cd "$(dirname "$0")" && pwd)
commit=$(git -C "$src" rev-parse --verify --quiet "${1:-HEAD}^{commit}") || die "no commit ${1:-HEAD}"
short=$(git -C "$src" rev-parse --short=8 "$commit")
base_ref=${OSS_BASE:-origin/main}
base=$(git -C "$src" rev-parse --verify --quiet "$base_ref^{commit}") || die "no public line $base_ref"
branch=${OSS_BRANCH:-oss/sync-$short}
git -C "$src" check-ref-format --branch "$branch" >/dev/null || die "bad branch name $branch"
if git -C "$src" rev-parse --verify --quiet "refs/heads/$branch" >/dev/null; then
  die "branch $branch exists; delete it or set OSS_BRANCH"
fi
name=${OSS_AUTHOR_NAME:-$(git -C "$src" config user.name || true)}
email=${OSS_AUTHOR_EMAIL:-$(git -C "$src" config user.email || true)}
[ -n "$name" ] && [ -n "$email" ] || die "set OSS_AUTHOR_NAME and OSS_AUTHOR_EMAIL"

if [ -z "${OSS_FORBIDDEN_FILE:-}" ] && [ -z "${OSS_FORBIDDEN:-}" ] && [ -f "$src/tools/oss-forbidden.txt" ]; then
  export OSS_FORBIDDEN_FILE=$src/tools/oss-forbidden.txt
fi

work=$(mktemp -d "${TMPDIR:-/tmp}/oss-export.XXXXXX")
trap 'rm -rf "$work"' EXIT
export GIT_INDEX_FILE=$work/index

# The tree: the commit's, minus the excluded paths, staged in a private index (the clone's own index is untouched).
git -C "$src" read-tree "$commit"
excludes=$work/exclude
if [ -n "${OSS_EXCLUDE_FILE:-}" ]; then
  cp "$OSS_EXCLUDE_FILE" "$excludes"
else
  git -C "$src" show "$commit:tools/oss-exclude.txt" >"$excludes" 2>/dev/null || die "$short has no tools/oss-exclude.txt"
fi
while IFS= read -r line || [ -n "$line" ]; do
  line=${line%$'\r'}
  case $line in '' | '#'*) continue ;; esac
  git -C "$src" rm -r -q --cached --ignore-unmatch -- ":(glob)$line" ":(glob)$line/**"
done <"$excludes"
tree=$(git -C "$src" write-tree)

# The leak check reads the files as they will be published.
mkdir "$work/tree"
git -C "$src" archive --format=tar "$tree" | tar -x -C "$work/tree"

# Provenance tags of the internal tracker leave Markdown and comment lines: `(KEY-12)`, `(KEY-12, KEY-13)`,
# `(KEY-12: why)` → `(why)`, a comment led by `KEY-12: `. The key prefixes are the `strip:` lines of OSS_FORBIDDEN_FILE
# (or OSS_STRIP_KEYS, comma-separated); a tag anywhere else, a string of the code, is left for the leak check to refuse.
keys=${OSS_STRIP_KEYS:-}
if [ -n "${OSS_FORBIDDEN_FILE:-}" ]; then
  keys=$keys,$(sed -n 's/^strip://p' "$OSS_FORBIDDEN_FILE" | tr -d '\r' | paste -sd, -)
fi
keys=$(tr ',' '\n' <<<"$keys" | grep -E '^[A-Z][A-Z0-9]{1,9}$' | paste -sd'|' - || true)
if [ -n "$keys" ]; then
  # shellcheck disable=SC2016 # the perl program is perl's, not the shell's
  find "$work/tree" -type f \( -name '*.md' -o -name '*.go' -o -name '*.ts' -o -name '*.tsx' -o -name '*.js' \
    -o -name '*.mjs' -o -name '*.css' -o -name '*.sh' -o -name '*.yml' -o -name '*.yaml' \) ! -name '*.min.js' -print0 |
    OSS_KEYS=$keys xargs -0 perl -i -pe '
      BEGIN { $k = qr/(?:$ENV{OSS_KEYS})-\d+/ }
      if ($ARGV =~ /\.md$/ || /^\s*(?:\/\/|\/\*|\*|#)/) {
        s/ ?\((?:$k)(?:, (?:$k))*\)//g;
        s/\((?:$k)(?:, (?:$k))*[:;,] /(/g;
        s/, (?:$k)(?:, (?:$k))*\)/)/g;
        s/^(\s*(?:\/\/|\/\*\*?|\*|#)\s*)(?:$k)[:.] /$1/;
      }'
  # The index takes the files as they are now; only the stripped ones change.
  git -C "$src" --work-tree="$work/tree" add -A -f -- .
  tree=$(git -C "$src" write-tree)
fi
"$here/oss-leak-check.sh" "$work/tree" || die "the leak check refused the tree of $short; no branch made"

if [ "$tree" = "$(git -C "$src" rev-parse "$base^{tree}")" ]; then
  echo "oss-export: $base_ref already has the tree of $short; nothing to commit"
  exit 0
fi
gone=$(git -C "$src" diff --name-only --diff-filter=D "$base" "$tree")
if [ -n "$gone" ]; then
  echo "oss-export: files only $base_ref has, which this sync deletes (port them to the internal line to keep them):" >&2
  while IFS= read -r f; do echo "  $f" >&2; done <<<"$gone"
fi

sync=$(GIT_AUTHOR_NAME=$name GIT_AUTHOR_EMAIL=$email GIT_COMMITTER_NAME=$name GIT_COMMITTER_EMAIL=$email \
  git -C "$src" commit-tree "$tree" -p "$base" -m "sync: internal main $(git -C "$src" rev-parse "$commit")")
git -C "$src" update-ref "refs/heads/$branch" "$sync" ""
echo "oss-export: $branch at $(git -C "$src" rev-parse --short "$sync") on $base_ref, the tree of $short"
git -C "$src" diff --shortstat "$base" "$sync"
