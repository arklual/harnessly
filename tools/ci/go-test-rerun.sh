#!/bin/sh
# usage: tools/ci/go-test-rerun.sh PACKAGE...
#
# Runs `go test` on the packages. The tests that failed are run once more, alone, and the job is red only when a test
# failed twice. 37 % of the merge request pipelines were red (05-09.11), most of them on a test that waits on a clock
# and loses under a loaded runner; each one cost a 17 minute pipeline and a new push.
#
# What is not run again, because a second run would hide it or cannot help:
#   - a package that does not build or whose setup failed;
#   - more than $GO_TEST_RERUN_MAX failed tests (10) or more than 5 failed packages: that is a broken change.
# A package that failed without naming a test, or with a panic or a timeout (the tests after it never ran), is run
# again whole. A test counts as passed on the second run only when `go test -v` prints its PASS line: a -run pattern
# that matches nothing is green too.
#
# Every test that passed on the second run is printed as "FLAKY <package> <test>" and written to
# $GO_TEST_FLAKY_FILE (flaky-tests.txt): those are the tests to fix.
set -u
[ $# -gt 0 ] || { echo "usage: $0 PACKAGE..." >&2; exit 2; }
# shellcheck disable=SC2086 # the flags are words
flags=${GO_TEST_FLAGS:--race -count=1}
max=${GO_TEST_RERUN_MAX:-10}
flaky_file=${GO_TEST_FLAKY_FILE:-flaky-tests.txt}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
: > "$flaky_file"

# run LOG ARGS...: go test with its output on the screen and in LOG; the answer is go test's own.
run() {
  log=$1; shift
  # shellcheck disable=SC2086
  { go test $flags "$@" 2>&1; echo $? > "$tmp/rc"; } | tee "$log"
  return "$(cat "$tmp/rc")"
}

run "$tmp/first.log" "$@" && exit 0

# One line per failed package: "hard PKG", "whole PKG" or "tests PKG TestA|TestB". go test prints the output of a
# package in one piece and ends it with "FAIL<tab>PKG<tab>time" (or "ok", or "?"), so the top-level "--- FAIL:" lines
# above that line are this package's.
awk '
  function reset() { n = 0; whole = 0; names = "" }
  BEGIN { reset() }
  /^--- FAIL: / { if (!(($3) in seen)) { names = names (n ? "|" : "") $3; n++ } seen[$3] = 1; next }
  /^panic: / || /^\*\*\* Test killed/ || /^fatal error: / { whole = 1; next }
  /^(ok|\?)[ \t]/ { reset(); delete seen; next }
  /^FAIL[ \t]+[^ \t]/ {
    if ($0 ~ /\[(build|setup) failed\]/) print "hard", $2
    else if (n == 0 || whole) print "whole", $2
    else print "tests", $2, names
    reset(); delete seen
  }
' "$tmp/first.log" > "$tmp/failed"

if [ ! -s "$tmp/failed" ]; then
  echo "go-test-rerun: go test failed and named no package: not run again"
  exit 1
fi
if grep -q '^hard ' "$tmp/failed"; then
  echo "go-test-rerun: a package does not build: not run again"
  exit 1
fi
packages=$(wc -l < "$tmp/failed")
tests=$(awk '$1 == "tests" { c += split($3, a, "|") } END { print c + 0 }' "$tmp/failed")
if [ "$packages" -gt 5 ] || [ "$tests" -gt "$max" ]; then
  echo "go-test-rerun: $tests tests failed in $packages packages: that is not a flaky test, not run again"
  exit 1
fi

echo
echo "go-test-rerun: the second run of what failed"
bad=0
while read -r kind pkg names; do
  if [ "$kind" = whole ]; then
    echo "go-test-rerun: $pkg whole (it named no test, or it stopped on a panic or a timeout)"
    if run "$tmp/again.log" "$pkg"; then echo "$pkg (whole package)" >> "$flaky_file"; else bad=1; fi
    continue
  fi
  echo "go-test-rerun: $pkg $names"
  if ! run "$tmp/again.log" -v -run "^($names)\$" "$pkg"; then bad=1; continue; fi
  for name in $(echo "$names" | tr '|' ' '); do
    if grep -q "^--- PASS: $name " "$tmp/again.log"; then
      echo "$pkg $name" >> "$flaky_file"
    else
      echo "go-test-rerun: $pkg $name did not run the second time"
      bad=1
    fi
  done
done < "$tmp/failed"

if [ -s "$flaky_file" ]; then
  echo
  sed 's/^/FLAKY /' "$flaky_file"
fi
if [ "$bad" -ne 0 ]; then
  echo "go-test-rerun: failed twice"
  exit 1
fi
echo "go-test-rerun: everything that failed passed on the second run; the tests above are flaky and want fixing"
