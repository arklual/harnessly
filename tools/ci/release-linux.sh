#!/bin/sh
# HRN-912. Builds the binaries the machines run, agent, harnessly and runtime (with the page, the webui tag) for
# linux/amd64, exactly as a release does: -trimpath, stripped, the version linked in. The main pipeline (release-build)
# builds them with the version its tag will have (next-runtime-version.sh), so the tag pipeline only checks and publishes
# them (publish-linux.sh); it builds them itself only when that fails. A merge request builds them too, for its smoke test
# only (release-linux), never for the machines. Other systems are the tag's release-cross job.
#
# usage: release-linux.sh <out-dir> <version X.Y.Z>   (web/dist built)
# Writes <out-dir>/{agent,harnessly,runtime}-linux-amd64 and <out-dir>/release.env (RELEASE_SHA, RELEASE_VERSION).
set -eu

out="${1:?usage: release-linux.sh <out-dir> <version>}"
version="${2:?usage: release-linux.sh <out-dir> <version>}"
test -f web/dist/index.html || { echo 'no web/dist: build the page first' >&2; exit 1; }
mkdir -p "$out"
for bin in agent harnessly runtime; do
  tags=""
  [ "$bin" = runtime ] && tags="-tags webui"
  # shellcheck disable=SC2086 # tags is empty or one flag with its value
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath $tags \
    -ldflags "-s -w -X github.com/ly-solutions/harnessly/internal/agent.BuildVersion=$version -X github.com/ly-solutions/harnessly/internal/hlcli.Version=$version" \
    -o "$out/$bin-linux-amd64" "./cmd/$bin"
done
printf 'RELEASE_SHA=%s\nRELEASE_VERSION=%s\n' "$(git rev-parse HEAD)" "$version" > "$out/release.env"
(cd "$out" && sha256sum agent-linux-amd64 harnessly-linux-amd64 runtime-linux-amd64)
