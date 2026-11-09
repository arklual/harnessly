#!/bin/sh
# Publishes the linux-amd64 binaries of a release tag vX.Y.Z (agent, harnessly, runtime and their SHA256SUMS) to
# the generic package agent/X.Y.Z, which the machines take (their pin checks exactly these four files, each uploaded by a
# pipeline of this tag). Not rebuilt: the main pipeline of this very commit built them already (release-build, on the
# release runner with the protected caches, as this job would), and runtime-tag named that job in the tag's message
# (Build-Job). They are taken only when
#   - the tag says so: it is protected, and only runtime-tag of the main pipeline writes it; Source-SHA is this commit,
#   - the build job's own record (release.env) names this commit and this version,
#   - every binary says so itself, read and never run: `go version -m` shows vcs.revision=$CI_COMMIT_SHA and
#     linux/amd64, and the version string is linked in.
# Anything else, or any step that fails, builds them here from the tag (the page first), as every release did before.
#
# usage: publish-linux.sh   (tag pipeline of vX.Y.Z)
set -eu

version="${CI_COMMIT_TAG#v}"
api="$CI_API_V4_URL/projects/$CI_PROJECT_ID"
out=dist
rm -rf "$out" && mkdir -p "$out"

reuse() {
  # The annotated tag itself, not only its commit.
  git fetch --quiet --no-tags origin "+refs/tags/$CI_COMMIT_TAG:refs/tags/$CI_COMMIT_TAG" || true
  message="$(git for-each-ref --format='%(contents)' "refs/tags/$CI_COMMIT_TAG")"
  job="$(printf '%s\n' "$message" | sed -n 's/^[[:space:]]*Build-Job: *\([0-9][0-9]*\)[[:space:]]*$/\1/p' | head -n1)"
  source_sha="$(printf '%s\n' "$message" | sed -n 's/^[[:space:]]*Source-SHA: *\([0-9a-f]\{40\}\)[[:space:]]*$/\1/p' | head -n1)"
  [ -n "$job" ] || { echo "the tag names no release build"; return 1; }
  [ "$source_sha" = "$CI_COMMIT_SHA" ] || { echo "the tag was cut for ${source_sha:-no commit}, not $CI_COMMIT_SHA"; return 1; }
  for f in release.env agent-linux-amd64 harnessly-linux-amd64 runtime-linux-amd64; do
    curl -fsS --retry 2 --header "JOB-TOKEN: $CI_JOB_TOKEN" -o "$out/$f" "$api/jobs/$job/artifacts/dist-linux/$f" \
      || { echo "cannot fetch $f of release-build job $job"; return 1; }
  done
  built_sha="$(sed -n 's/^RELEASE_SHA=//p' "$out/release.env")"
  built_version="$(sed -n 's/^RELEASE_VERSION=//p' "$out/release.env")"
  [ "$built_sha" = "$CI_COMMIT_SHA" ] || { echo "release-build job $job built $built_sha, not $CI_COMMIT_SHA"; return 1; }
  [ "$built_version" = "$version" ] || { echo "release-build job $job built version $built_version, not $version"; return 1; }
  for bin in agent harnessly runtime; do
    info="$(go version -m "$out/$bin-linux-amd64")" || { echo "$bin-linux-amd64 is not a Go binary"; return 1; }
    printf '%s\n' "$info" | grep -Eq "^[[:space:]]+build[[:space:]]+vcs.revision=$CI_COMMIT_SHA\$" \
      || { echo "$bin-linux-amd64 was not built from $CI_COMMIT_SHA"; return 1; }
    if ! printf '%s\n' "$info" | grep -Eq '^[[:space:]]+build[[:space:]]+GOOS=linux$' \
      || ! printf '%s\n' "$info" | grep -Eq '^[[:space:]]+build[[:space:]]+GOARCH=amd64$'; then
      echo "$bin-linux-amd64 is not linux/amd64"; return 1
    fi
    # -trimpath keeps -ldflags out of the build info; the version is linked in as a string.
    grep -aqF "$version" "$out/$bin-linux-amd64" || { echo "$bin-linux-amd64 does not carry $version"; return 1; }
  done
  chmod 0755 "$out"/*-linux-amd64
  echo "reusing the binaries release-build job $job built for $CI_COMMIT_SHA, version $version"
}

if ! reuse; then
  echo "building the linux-amd64 binaries of $CI_COMMIT_TAG here"
  rm -rf "$out" && mkdir -p "$out"
  if [ ! -f web/dist/index.html ]; then
    node_version=22.12.0
    # .tar.gz, not .tar.xz: the Go image has no xz.
    curl -sSfL "https://nodejs.org/dist/v${node_version}/node-v${node_version}-linux-x64.tar.gz" | tar -xz -C /tmp
    PATH="/tmp/node-v${node_version}-linux-x64/bin:$PATH"
    export PATH
    # Not corepack: the node tarball ships a corepack whose registry keys are out of date (hub-journey).
    npm install -g "$(sed -n 's/.*"packageManager": *"\(pnpm@[^"]*\)".*/\1/p' web/package.json)" >/dev/null
    (cd web && pnpm install --frozen-lockfile && pnpm build)
  fi
  go test -tags webui -count=1 -run TestWebUIBuild ./cmd/runtime
  tools/ci/release-linux.sh "$out" "$version"
fi

(cd "$out" && sha256sum agent-linux-amd64 harnessly-linux-amd64 runtime-linux-amd64 > SHA256SUMS && cat SHA256SUMS)
for f in agent-linux-amd64 harnessly-linux-amd64 runtime-linux-amd64 SHA256SUMS; do
  curl --fail --silent --show-error --header "JOB-TOKEN: $CI_JOB_TOKEN" --upload-file "$out/$f" \
    "$api/packages/generic/agent/$version/$f" >/dev/null
done
echo "published agent/$version: agent, harnessly and runtime for linux-amd64"
