#!/bin/sh
# Prints how many commits of main wait for a release at SHA: the commits since the newest runtime tag (vX.Y.Z)
# behind it. Not the hub's tags (X.Y.Z): the hub's semantic-release cuts one in almost every main pipeline, also in the
# ones whose runtime-tag leaves the release to a newer pipeline, so counting from them kept the count near zero and no
# pipeline became the anchor: the runtime went unreleased for hours under a stream of pushes. A main pipeline with HL_RELEASE_EVERY or more of them
# is the release anchor: a newer push does not cancel it (main-supersede) and its runtime-tag cuts the tag although main
# has moved on, so a steady stream of pushes still releases every HL_RELEASE_EVERY commits.
#
# usage: unreleased-commits.sh [SHA]   (in a clone with the full history and the release tags)
set -eu

sha="${1:-HEAD}"
last="$(git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' "$sha" 2>/dev/null || true)"
if [ -z "$last" ]; then
  git rev-list --count "$sha"
else
  git rev-list --count "$last..$sha"
fi
