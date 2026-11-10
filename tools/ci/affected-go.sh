#!/bin/sh
# Prints the Go packages a merge request has to test (one per line), or `./...` for all of them.
# Only when the request changes nothing but .go files under cmd/ and internal/: then a test can see the change only
# through what its test binary imports, so the packages whose test binary (`go list -test`, every dependency, the test
# imports' own ones too) contains a changed package are the ones to test. Anything else — a test's data, a page, a
# document, a skill, go.mod, the CI — and the tests read it by path from other packages too (cmd/runtime reads
# internal/*/testdata, internal/apps reads docs/ and web/), so all of them run. The nightly schedule runs all of them
# anyway; main runs none (fast-forward: the merge request checked that very commit).
#
# usage: affected-go.sh   (merge request pipeline: CI_MERGE_REQUEST_DIFF_BASE_SHA)
set -eu

all() { echo ./...; exit 0; }
[ "${CI_PIPELINE_SOURCE:-}" = merge_request_event ] || all
base="${CI_MERGE_REQUEST_DIFF_BASE_SHA:-}"
[ -n "$base" ] || all
git cat-file -e "$base^{commit}" 2>/dev/null || git fetch --quiet --no-tags --depth=1 origin "$base" 2>/dev/null || all
changed="$(git diff --name-only --no-renames "$base" HEAD 2>/dev/null)" || all
[ -n "$changed" ] || all

module="$(go list -m)"
pkgs=""
while IFS= read -r f; do
  case "$f" in
    cmd/*.go|internal/*.go) ;;
    *) echo "$f is not Go code: every package is tested" >&2; all ;;
  esac
  case "$f" in *testdata/*|*' '*) all ;; esac
  pkgs="$pkgs $module/$(dirname "$f")"
done <<EOF
$changed
EOF

# "P", "P [P.test]", "P_test [P.test]" and "P.test", each with every package its build holds.
go list -test -f '{{.ImportPath}}{{range .Deps}} {{.}}{{end}}' ./... | awk -v changed="$pkgs" '
  BEGIN { n = split(changed, c, " "); for (i = 1; i <= n; i++) hit[c[i]] = 1 }
  {
    owner = $1
    if ($2 ~ /^\[/) { owner = substr($2, 2); sub(/\.test\]$/, "", owner) }
    else sub(/\.test$/, "", owner)
    self = $1; sub(/_test$/, "", self)
    found = (self in hit) || (owner in hit)
    for (i = 2; i <= NF && !found; i++) if ($i in hit) found = 1
    if (found && !(owner in out)) { out[owner] = 1; print owner }
  }' | sort
