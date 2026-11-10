#!/bin/sh
# The acceptance scenario of a public release (HRN-757, internal/hub/acceptance): a clean install of the hub and of the
# example extension pack, sign-in, organization, machine, agent, update to the next version, and rollback.
# N+1 is the tree under test (the release tag); N is the previous release tag, or $HUB_N_REF.
set -eu
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
work=$(mktemp -d)
trap 'git worktree remove --force "$work/n" 2>/dev/null || true; rm -rf "$work"' EXIT

ref=${HUB_N_REF:-}
if [ -z "$ref" ]; then
  # the newest semver tag before this one: `v1.2.3` or `1.2.3`
  ref=$(git tag --list --sort=-v:refname 'v[0-9]*.[0-9]*.[0-9]*' '[0-9]*.[0-9]*.[0-9]*' --merged HEAD |
    grep -E '^v?[0-9]+\.[0-9]+\.[0-9]+$' | grep -vx "${CI_COMMIT_TAG:-none}" | head -1 || true)
fi
if [ -z "$ref" ]; then
  echo "acceptance-pack: no earlier release tag to update from; set HUB_N_REF to the build N" >&2
  exit 1
fi
echo "build N = $ref, build N+1 = $(git rev-parse --short HEAD)"

git worktree add -q --detach "$work/n" "$ref" >/dev/null
(cd "$work/n" && go build -o "$work/hub-n" ./cmd/hub)
go build -o "$work/hub-n1" ./cmd/hub
go build -o "$work/adapter" ./examples/machine-adapter

# the adapters of the pack are TypeScript run by node itself: its own tests first
node --experimental-strip-types --test examples/ext-pack-example/adapters/adapters.test.ts

HUB_BIN_N="$work/hub-n" HUB_BIN_N1="$work/hub-n1" ADAPTER_BIN="$work/adapter" \
  PACK_SCRIPT="$root/examples/ext-pack-example/ext-pack.sh" \
  go test -count=1 -v -run TestCleanInstallUpdateRollback ./internal/hub/acceptance/
