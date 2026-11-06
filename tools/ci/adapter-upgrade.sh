#!/bin/sh
# The scenario of HRN-735: the example machine adapter (examples/machine-adapter), run as its own process, survives the
# update of the hub from release build N to build N+1. N is the base of the merge request (or $HUB_N_REF), N+1 is the
# tree under test. Neither the hub nor the adapter is edited between the two halves; the scenario is
# internal/hub/upgradecheck.
set -eu
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
work=$(mktemp -d)
trap 'git worktree remove --force "$work/n" 2>/dev/null || true; rm -rf "$work"' EXIT

ref=${HUB_N_REF:-}
if [ -z "$ref" ]; then
  base=${CI_MERGE_REQUEST_DIFF_BASE_SHA:-}
  [ -n "$base" ] || base=$(git merge-base HEAD origin/main 2>/dev/null || true)
  # on main itself the base is the previous commit: N is what was there before
  if [ -z "$base" ] || [ "$base" = "$(git rev-parse HEAD)" ]; then base=$(git rev-parse HEAD~1); fi
  ref=$base
fi
echo "build N = $ref, build N+1 = $(git rev-parse --short HEAD)"

git worktree add --detach "$work/n" "$ref" >/dev/null
(cd "$work/n" && go build -o "$work/hub-n" ./cmd/hub)
go build -o "$work/hub-n1" ./cmd/hub
go build -o "$work/adapter" ./examples/machine-adapter

HUB_BIN_N="$work/hub-n" HUB_BIN_N1="$work/hub-n1" ADAPTER_BIN="$work/adapter" \
  go test -count=1 -v -run TestAdapterSurvivesHubUpdate ./internal/hub/upgradecheck/
