#!/bin/sh
# usage: tools/ci/test-package-files.sh
set -eu
# shellcheck source=tools/ci/package-files.sh
. "$(dirname "$0")/package-files.sh"
fixture="$(cat "$(dirname "$0")/testdata/package-files-agent-build.json")"
got="$(written_by_pipelines "$fixture")"
[ "$got" = "15770" ] || { echo "FAIL: pipelines of the real answer: '$got', want '15770'"; exit 1; }
two='[{"id":1,"pipelines":[{"id":5,"iid":1,"user":{"id":3}}]},{"id":2,"pipelines":[{"id":7,"iid":2,"user":{"id":3}},{"id":5,"iid":1,"user":{"id":3}}]}]'
got="$(written_by_pipelines "$two")"
[ "$got" = "5 7" ] || { echo "FAIL: two pipelines: '$got', want '5 7'"; exit 1; }
[ -z "$(written_by_pipelines '[]')" ] || { echo "FAIL: no files"; exit 1; }
echo "ok"
