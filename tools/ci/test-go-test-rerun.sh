#!/bin/sh
# usage: tools/ci/test-go-test-rerun.sh
# go-test-rerun.sh against a module made here: a test that fails once, one that fails always, a panic, a broken build.
set -eu
script="$(cd "$(dirname "$0")" && pwd)/go-test-rerun.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$work"
cat > go.mod <<'EOF'
module example.com/rerun

go 1.21
EOF
mkdir once always boom broken green
# Each test reads the folder MARK names: a file there with the test's name means "this is the second run".
helper='
func second(t *testing.T) bool {
	mark := filepath.Join(os.Getenv("MARK"), t.Name())
	if _, err := os.Stat(mark); err == nil {
		return true
	}
	if err := os.WriteFile(mark, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return false
}
'
head='
import (
	"os"
	"path/filepath"
	"testing"
)
'
printf 'package once\n%s%s\nfunc TestOnce(t *testing.T) {\n\tif !second(t) {\n\t\tt.Fatal("first run")\n\t}\n}\n\nfunc TestFine(t *testing.T) {}\n' "$head" "$helper" > once/once_test.go
printf 'package always\n\nimport "testing"\n\nfunc TestAlways(t *testing.T) { t.Fatal("always") }\n' > always/always_test.go
printf 'package boom\n%s%s\nfunc TestBoom(t *testing.T) {\n\tif !second(t) {\n\t\tpanic("first run")\n\t}\n}\n\nfunc TestAfterBoom(t *testing.T) {\n\tif os.Getenv("AFTER_FAILS") != "" {\n\t\tt.Fatal("the test after the panic")\n\t}\n}\n' "$head" "$helper" > boom/boom_test.go
printf 'package broken\n\nimport "testing"\n\nfunc TestBroken(t *testing.T) { undefined() }\n' > broken/broken_test.go
printf 'package green\n\nimport "testing"\n\nfunc TestGreen(t *testing.T) {}\n' > green/green_test.go

export GO_TEST_FLAGS=-count=1 GO_TEST_FLAKY_FILE="$work/flaky.txt" GOFLAGS=-mod=mod
# check NAME WANT_EXIT PACKAGES...: runs the script on a fresh MARK folder and compares its answer.
check() {
  name=$1 want=$2; shift 2
  MARK=$(mktemp -d "$work/mark.XXXXXX"); export MARK
  rc=0; "$script" "$@" > "$work/out" 2>&1 || rc=$?
  [ "$rc" = "$want" ] || { cat "$work/out"; echo "FAIL: $name: exit $rc, want $want"; exit 1; }
}
has() { grep -q -- "$1" "$work/out" || { cat "$work/out"; echo "FAIL: $2: no '$1' in the output"; exit 1; }; }
flaky() { [ "$(cat "$work/flaky.txt")" = "$1" ] || { cat "$work/out"; echo "FAIL: $2: flaky file '$(cat "$work/flaky.txt")', want '$1'"; exit 1; }; }

check "all green" 0 ./green ./once/... -run TestFine
flaky "" "all green"

check "fails once" 0 ./green ./once
has "FLAKY example.com/rerun/once TestOnce" "fails once"
flaky "example.com/rerun/once TestOnce" "fails once"

check "fails always" 1 ./once ./always
has "failed twice" "fails always"
flaky "example.com/rerun/once TestOnce" "fails always"

check "a panic runs the package again whole" 0 ./boom
has "example.com/rerun/boom whole" "panic"
flaky "example.com/rerun/boom (whole package)" "panic"

AFTER_FAILS=1 check "the test a panic hid is not lost" 1 ./boom
has "failed twice" "the test a panic hid"

check "a broken build" 1 ./green ./broken
has "does not build" "a broken build"

GO_TEST_RERUN_MAX=0 check "too many failures" 1 ./once
has "not a flaky test" "too many failures"
echo "ok"
