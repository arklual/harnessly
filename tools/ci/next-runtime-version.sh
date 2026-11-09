#!/bin/sh
# Prints the runtime release `runtime-tag` (.gitlab-ci.yml) would cut for HEAD once it is on main: the next patch
# vX.Y.Z after the newest release tag, or nothing when HEAD would not be released (no runtime path changed since that tag)
# or cannot be told (that tag is not behind HEAD).
# The main pipeline (release-build) builds the linux-amd64 binaries with this version, and the tag pipeline takes them only
# when they carry exactly its version and commit (tools/ci/publish-linux.sh); a wrong guess costs a rebuild there, never a
# wrong binary. Keep the rule in step with runtime-tag.
#
# usage: next-runtime-version.sh   (in a clone with the release tags)
set -eu

runtime_paths='^(cmd/|internal/|skills/|web/|deploy/(install|container)/|go[.]mod|go[.]sum)'
last="$(git tag -l 'v*' --sort=-v:refname | grep -E '^v(0|[1-9][0-9]*)[.](0|[1-9][0-9]*)[.](0|[1-9][0-9]*)$' | head -n1 || true)"
[ -n "$last" ] || exit 0
git merge-base --is-ancestor "$last" HEAD 2>/dev/null || exit 0
if ! git diff --name-only "$last" HEAD | grep -E "$runtime_paths" >/dev/null; then exit 0; fi
ver="${last#v}"; major="${ver%%.*}"; rest="${ver#*.}"; minor="${rest%%.*}"; patch="${rest#*.}"
echo "v$major.$minor.$((patch + 1))"
