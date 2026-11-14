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
# the fingerprint ignores the writing pipeline's status and time, and sees a new file or another hash.
a='[{"id":11,"package_id":2,"file_sha256":"aa","pipelines":[{"id":5,"iid":1,"status":"running","updated_at":"t1"}]}]'
b='[{"id":11,"package_id":2,"file_sha256":"aa","pipelines":[{"id":5,"iid":1,"status":"success","updated_at":"t2"}]}]'
c='[{"id":11,"package_id":2,"file_sha256":"aa","pipelines":[{"id":5,"iid":1}]},{"id":12,"package_id":2,"file_sha256":"bb","pipelines":[{"id":9,"iid":3}]}]'
d='[{"id":11,"package_id":2,"file_sha256":"ab","pipelines":[{"id":5,"iid":1}]}]'
[ "$(files_fingerprint "$a")" = "$(files_fingerprint "$b")" ] || { echo "FAIL: a status change counted as a change"; exit 1; }
[ "$(files_fingerprint "$a")" != "$(files_fingerprint "$c")" ] || { echo "FAIL: a new file went unseen"; exit 1; }
[ "$(files_fingerprint "$a")" != "$(files_fingerprint "$d")" ] || { echo "FAIL: another hash went unseen"; exit 1; }
[ -n "$(files_fingerprint "$fixture" | cut -d'|' -f1)" ] || { echo "FAIL: no file ids in the real answer"; exit 1; }
echo "ok"
