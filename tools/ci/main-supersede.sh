#!/bin/sh
# A push to main supersedes the older main pipelines still running, but not all of them: GitLab's own auto-cancel
# is off on main (workflow), this job does it instead.
#   - the oldest running pipeline whose commit has HL_RELEASE_EVERY or more unreleased commits (unreleased-commits.sh) is the
#     release anchor and runs to the end: its runtime-tag and the hub's semantic-release cut the release although main
#     moved on. One anchor at a time: the ones after it are cancelled as usual.
#   - a pipeline that has started releasing (a job of the release, promote or deploy stage started) is not touched.
#   - every other older pipeline of a push to main is cancelled: this one checks and releases its commits.
# Without HL_SUPERSEDE_TOKEN (a project access token, Maintainer: a pipeline of the protected main is cancelled only by
# who may push there) nothing is cancelled and the older pipelines simply run on.
#
# usage: main-supersede.sh   (in CI, in a clone with the full history and the release tags)
set -eu

every="${HL_RELEASE_EVERY:-10}"
if [ -z "${HL_SUPERSEDE_TOKEN:-}" ]; then
  echo "HL_SUPERSEDE_TOKEN is not set: the older main pipelines run on"; exit 0
fi
api="$CI_API_V4_URL/projects/$CI_PROJECT_ID"
auth="PRIVATE-TOKEN: $HL_SUPERSEDE_TOKEN"
here="$(dirname "$0")"
git fetch --tags --force --quiet origin

curl --fail --silent --show-error --header "$auth" \
  "$api/pipelines?ref=$CI_DEFAULT_BRANCH&source=push&order_by=id&sort=desc&per_page=50" \
  | jq -r --argjson self "$CI_PIPELINE_ID" \
      'reverse | .[] | select(.id < $self and (.status | IN("created","waiting_for_resource","preparing","pending","running"))) | "\(.id) \(.sha)"' \
  > older.txt

anchor=""
while read -r id sha; do
  [ -n "$id" ] || continue
  if ! git merge-base --is-ancestor "$sha" "$CI_COMMIT_SHA" 2>/dev/null; then
    echo "pipeline $id ($sha) is not behind $CI_COMMIT_SHA: left alone"; continue
  fi
  unreleased="$("$here/unreleased-commits.sh" "$sha")"
  if [ -z "$anchor" ] && [ "$unreleased" -ge "$every" ]; then
    anchor="$id"
    echo "pipeline $id ($sha): $unreleased unreleased commits (>= $every), the release anchor: runs to the end"; continue
  fi
  releasing="$(curl --fail --silent --show-error --header "$auth" "$api/pipelines/$id/jobs?per_page=100" \
    | jq '[.[] | select((.stage | IN("release","promote","deploy")) and (.status | IN("running","success","failed")))] | length')"
  if [ "$releasing" -gt 0 ]; then
    echo "pipeline $id ($sha) is already releasing: left alone"; continue
  fi
  echo "pipeline $id ($sha): $unreleased unreleased commits, superseded by $CI_PIPELINE_ID: cancelling"
  curl --fail --silent --show-error --output /dev/null --request POST --header "$auth" "$api/pipelines/$id/cancel" \
    || echo "could not cancel pipeline $id"
done < older.txt
