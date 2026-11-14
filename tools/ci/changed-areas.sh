#!/bin/sh
# What a commit changed since the last release, by area, so a check or a build runs only for what it covers:
#   HL_AREA_WEB=1   the page (web/, but not web/*.go: those are the Go embed files)
#   HL_AREA_GO=1    the Go tree (cmd/, internal/, skills/ (embedded), web/*.go, go.mod, go.sum, tools/oss-public/)
#   HL_AREA_DOCS=1  documents only (docs/, *.md): nothing heavy runs for them
#   HL_AREA_ALL=1   anything else (CI, tools/ci, Dockerfile, deploy/, scripts, desktop, clients, ...): everything runs
# HL_AREA_ALL=1 sets HL_AREA_WEB and HL_AREA_GO too, so a caller only asks for its own area.
#
# The base is the last release, not the previous commit: a newer push to main cancels the older pipeline
# (main-supersede), so a pipeline checks everything not yet released, not only its own push. With two
# release lines (the runtime's vX.Y.Z and the hub's X.Y.Z) the older of the two newest ancestors is taken: what one of
# them has not released yet still counts. No tag reachable, a shallow clone without it, any error: everything runs.
#
# usage: changed-areas.sh [base]   prints the four lines above (KEY=0|1) and HL_AREA_BASE=<sha>; a dotenv file as is.
#        HL_AREA_BASE=<ref> in the environment overrides the base too (local-checks.sh --base).
set -eu

everything() {
  printf 'HL_AREA_WEB=1\nHL_AREA_GO=1\nHL_AREA_DOCS=1\nHL_AREA_ALL=1\nHL_AREA_BASE=%s\n' "${1:-none}"
  echo "changed-areas: everything ($2)" >&2
  exit 0
}

newest() { git describe --tags --abbrev=0 --match "$1" HEAD 2>/dev/null || true; }

base="${1:-${HL_AREA_BASE:-}}"
if [ -z "$base" ]; then
  runtime="$(newest 'v[0-9]*.[0-9]*.[0-9]*')"
  hub="$(newest '[0-9]*.[0-9]*.[0-9]*')"
  if [ -n "$runtime" ] && [ -n "$hub" ]; then
    # the older of the two: the one that is an ancestor of the other
    if git merge-base --is-ancestor "$runtime" "$hub" 2>/dev/null; then base="$runtime"; else base="$hub"; fi
  else
    base="${runtime:-$hub}"
  fi
fi
[ -n "$base" ] || everything "" "no release tag reachable from HEAD"
sha="$(git rev-parse -q --verify "$base^{commit}" 2>/dev/null)" || everything "" "base $base is not in this clone"
changed="$(git diff --name-only --no-renames "$sha" HEAD)" || everything "$sha" "git diff failed"
[ -n "$changed" ] || { printf 'HL_AREA_WEB=0\nHL_AREA_GO=0\nHL_AREA_DOCS=0\nHL_AREA_ALL=0\nHL_AREA_BASE=%s\n' "$sha"; exit 0; }

web=0 go=0 docs=0 all=0 why=""
while IFS= read -r f; do
  case "$f" in
    web/*/*) web=1 ;;
    web/*.go) go=1 ;;
    web/*) web=1 ;; # package.json, the lockfile, the configs
    cmd/*|internal/*|skills/*|go.mod|go.sum|tools/oss-public/*) go=1 ;;
    docs/*|*.md) docs=1 ;;
    *) all=1; why="$f" ;;
  esac
done <<EOF
$changed
EOF
if [ "$all" = 1 ]; then web=1 go=1; fi
printf 'HL_AREA_WEB=%s\nHL_AREA_GO=%s\nHL_AREA_DOCS=%s\nHL_AREA_ALL=%s\nHL_AREA_BASE=%s\n' "$web" "$go" "$docs" "$all" "$sha"
echo "changed-areas: since $base ($(printf '%s\n' "$changed" | grep -c .) files): web=$web go=$go docs=$docs all=$all${why:+ (first: $why)}" >&2
