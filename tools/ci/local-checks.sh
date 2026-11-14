#!/bin/sh
# The checks the main pipeline no longer runs while HL_CI_TESTS is off (14.11), run before a push on
# the build machine (or any machine with Go and pnpm), and only for what changed since the last release
# (tools/ci/changed-areas.sh):
#   Go     gofmt, go vet, golangci-lint, the race tests of the packages the change reaches (tools/ci/affected-go.sh)
#   web    the routes check, eslint, prettier, the build (tsc -b), vitest; plus the Go tests that read web/ (internal/apps)
#   docs   only the Go tests that read docs/ (internal/apps)
#   e2e    the browser smoke with the linux-amd64 binaries, with --e2e (or HL_LOCAL_E2E=1)
# Every step runs, the failed ones are listed at the end; the exit code is 1 when any failed.
#
# usage: tools/ci/local-checks.sh [--base <ref>] [--all] [--e2e] [--no-lint]
#   --base <ref>  compare with <ref> instead of the last release (origin/main: only what is not pushed yet)
#   --all         every area, every Go package
set -u

cd "$(git rev-parse --show-toplevel)" || exit 2
all=0 e2e="${HL_LOCAL_E2E:-0}" lint=1
while [ $# -gt 0 ]; do
  case "$1" in
    --base) HL_AREA_BASE="$2"; export HL_AREA_BASE; shift ;;
    --all) all=1 ;;
    --e2e) e2e=1 ;;
    --no-lint) lint=0 ;;
    -h|--help) sed -n '2,15p' "$0"; exit 0 ;;
    *) echo "unknown flag $1" >&2; exit 2 ;;
  esac
  shift
done

git fetch --quiet --tags origin 2>/dev/null || echo "no fetch: the tags may be old" >&2
if [ "$all" = 1 ]; then
  HL_AREA_WEB=1 HL_AREA_GO=1 HL_AREA_DOCS=1 HL_AREA_ALL=1 HL_AREA_BASE=
else
  areas="$(tools/ci/changed-areas.sh)" || exit 2
  eval "$areas"
fi
export HL_AREA_BASE
mkdir -p .local-checks
GO_TEST_FLAKY_FILE="$PWD/.local-checks/flaky-tests.txt"; export GO_TEST_FLAKY_FILE

failed=""
step() {
  name="$1"; shift
  printf '\n=== %s\n' "$name"
  start=$(date +%s)
  if "$@"; then printf -- '--- ok %s (%ss)\n' "$name" $(($(date +%s) - start))
  else printf -- '--- FAILED %s (%ss)\n' "$name" $(($(date +%s) - start)); failed="$failed $name"; fi
}

tools_dir="${XDG_CACHE_HOME:-$HOME/.cache}/harnessly-ci-tools"
golangci() {
  v=2.15.0 # the version of the golangci job and of the public GitHub CI
  bin="$tools_dir/golangci-lint-$v"
  if [ ! -x "$bin" ]; then
    mkdir -p "$tools_dir" && curl -sSfL "https://github.com/golangci/golangci-lint/releases/download/v$v/golangci-lint-$v-linux-amd64.tar.gz" \
      | tar -xzO "golangci-lint-$v-linux-amd64/golangci-lint" > "$bin.tmp" && chmod +x "$bin.tmp" && mv "$bin.tmp" "$bin" || return 1
  fi
  "$bin" run ./...
}
gofmt_clean() { out="$(gofmt -l cmd internal)"; [ -z "$out" ] || { echo "$out"; return 1; }; }
go_tests() {
  pkgs="$(tools/ci/affected-go.sh)" || return 1
  echo "packages: $(printf '%s\n' "$pkgs" | grep -c .)"
  # shellcheck disable=SC2086 # one package per word
  tools/ci/go-test-rerun.sh $pkgs
}
web_install() { (cd web && pnpm install --frozen-lockfile --prefer-offline >/dev/null); }
web_lint() { node tools/web-routes-check.mjs && (cd web && pnpm exec eslint . && pnpm exec prettier --check . >/dev/null); }
web_build() { (cd web && pnpm build >/dev/null); }
web_test() { (cd web && pnpm exec vitest run --exclude '**/bench.test.tsx'); }
e2e_smoke() {
  go test -tags webui -count=1 -run TestWebUIBuild ./cmd/runtime &&
    tools/ci/release-linux.sh .local-checks/dist-linux "0.0.0-local" >/dev/null &&
    (cd web && RUNTIME_BIN="$PWD/../.local-checks/dist-linux/runtime-linux-amd64" \
      HARNESSLY_BIN="$PWD/../.local-checks/dist-linux/harnessly-linux-amd64" PW_CHANNEL=chromium pnpm e2e:smoke)
}

if [ "$HL_AREA_GO" = 1 ]; then
  step gofmt gofmt_clean
  step go-vet go vet ./...
  [ "$lint" = 1 ] && step golangci golangci
  if [ "$all" = 1 ]; then step go-test tools/ci/go-test-rerun.sh ./...; else step go-test go_tests; fi
elif [ "$HL_AREA_WEB" = 1 ] || [ "$HL_AREA_DOCS" = 1 ]; then
  step go-test-apps go test -count=1 ./internal/apps/...
fi
if [ "$HL_AREA_WEB" = 1 ] || [ "$e2e" = 1 ]; then
  step web-install web_install
fi
if [ "$HL_AREA_WEB" = 1 ]; then
  [ "$lint" = 1 ] && step web-lint web_lint
  step web-build web_build
  step web-test web_test
fi
if [ "$e2e" = 1 ]; then
  [ -f web/dist/index.html ] || step web-build web_build
  step e2e-smoke e2e_smoke
fi

printf '\nareas since %s: web=%s go=%s docs=%s all=%s\n' "${HL_AREA_BASE:-everything}" "$HL_AREA_WEB" "$HL_AREA_GO" "$HL_AREA_DOCS" "$HL_AREA_ALL"
if [ -n "$failed" ]; then echo "FAILED:$failed"; exit 1; fi
echo "all checks passed"
