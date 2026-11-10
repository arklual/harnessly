#!/bin/sh
# HRN-912. Publishes the linux-amd64 binaries of a release tag vX.Y.Z (agent, harnessly, runtime and their SHA256SUMS) to
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

# shellcheck source=tools/ci/package-files.sh
. "$(dirname "$0")/package-files.sh"

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
  # Any developer's pipeline may write to the package registry with its job token, so the staged files are trusted only
  # when every one of them came from the main pipeline of this commit that ran the release-build job the protected tag
  # names, and from no other pipeline. All read with this job's own token: it cannot read a job (401), but it reads the
  # package, its files with the pipelines that wrote them, and a pipeline with its jobs.
  get() { curl -fsS --header "JOB-TOKEN: $CI_JOB_TOKEN" "$api/$1"; }
  package="$(get "packages?package_name=agent-build&package_version=$CI_COMMIT_SHA")" \
    || { echo "cannot list the package agent-build/$CI_COMMIT_SHA"; return 1; }
  package_id="$(printf '%s' "$package" | sed -n 's/^\[{"id":\([0-9][0-9]*\).*/\1/p')"
  [ -n "$package_id" ] || { echo "no package agent-build/$CI_COMMIT_SHA"; return 1; }
  files="$(get "packages/$package_id/package_files?per_page=100")" || { echo "cannot list the files of package $package_id"; return 1; }
  for f in release.env agent-linux-amd64 harnessly-linux-amd64 runtime-linux-amd64; do
    printf '%s' "$files" | grep -q "\"file_name\":\"$f\"" || { echo "package $package_id has no $f"; return 1; }
  done
  # Every file (and every re-upload of one) carries the pipeline that wrote it.
  pipelines="$(written_by_pipelines "$files")"
  build_pipeline="$pipelines"
  case "$build_pipeline" in ''|*[!0-9]*) echo "package $package_id was written by other pipelines or none ($pipelines)"; return 1 ;; esac
  [ "$(printf '%s' "$files" | grep -o '"file_name"' | wc -l)" -eq 4 ] || { echo "package $package_id holds other files than the four of release-build"; return 1; }
  info="$(get "pipelines/$build_pipeline")" || { echo "cannot read pipeline $build_pipeline"; return 1; }
  # Not "success": the main pipeline is still running (runtime-tag cut the tag in it); its release-build job is checked below.
  if ! printf '%s' "$info" | grep -q "\"sha\":\"$CI_COMMIT_SHA\"" \
    || ! printf '%s' "$info" | grep -q "\"ref\":\"$CI_DEFAULT_BRANCH\",\"status\":\"[a-z_]*\",\"source\":\"push\""; then
    echo "pipeline $build_pipeline is not the $CI_DEFAULT_BRANCH push pipeline of $CI_COMMIT_SHA"; return 1
  fi
  get "pipelines/$build_pipeline/jobs?per_page=100" | grep -q "\"id\":$job,\"status\":\"success\",\"stage\":\"build\",\"name\":\"release-build\"" \
    || { echo "release-build job $job is not a successful job of pipeline $build_pipeline"; return 1; }
  for f in release.env agent-linux-amd64 harnessly-linux-amd64 runtime-linux-amd64; do
    curl -fsS --retry 2 --header "JOB-TOKEN: $CI_JOB_TOKEN" -o "$out/$f" "$api/packages/generic/agent-build/$CI_COMMIT_SHA/$f" \
      || { echo "cannot fetch $f of release-build job $job from the package agent-build/$CI_COMMIT_SHA"; return 1; }
  done
  # Nobody wrote to the package while it was downloaded: the listing is the same as the one that was checked.
  files_after="$(get "packages/$package_id/package_files?per_page=100")" || files_after=""
  [ "$files_after" = "$files" ] || { echo "package $package_id changed while it was downloaded"; return 1; }
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
    node_version=22.20.0
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

# The staging copy of release-build is not needed any more; best effort, only with a token that may delete packages
# (a job token cannot; without one the copy stays for the registry's cleanup policy).
if [ -n "${RELEASE_GITLAB_TOKEN:-}" ]; then
  staged="$(curl -fsS --header "PRIVATE-TOKEN: $RELEASE_GITLAB_TOKEN" "$api/packages?package_name=agent-build&package_version=$CI_COMMIT_SHA" \
    | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p' | head -n1)" || staged=""
  if [ -n "$staged" ]; then
    curl -fsS --request DELETE --header "PRIVATE-TOKEN: $RELEASE_GITLAB_TOKEN" "$api/packages/$staged" >/dev/null || echo "cannot delete agent-build/$CI_COMMIT_SHA"
  fi
fi
