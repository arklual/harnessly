#!/bin/sh
# Reads the JSON of GET /projects/:id/packages/:id/package_files without jq (the Go image has none); sourced by
# publish-linux.sh, tested by test-package-files.sh against a real answer (testdata/package-files-agent-build.json).

# Prints the ids of the pipelines that wrote the files, once each, space separated. Only pipelines[].id: a pipeline object
# starts with {"id":N,"iid":, whereas the user and other objects inside it carry "id" keys of their own (user id 3 once
# read as a pipeline).
written_by_pipelines() {
  printf '%s' "$1" | grep -o '"pipelines":\[[^]]*\]' | grep -o '{"id":[0-9][0-9]*,"iid"' | grep -o '[0-9][0-9]*' | sort -u | tr '\n' ' ' | sed 's/ $//'
}

# What must not change while the files are downloaded: each file (its id and SHA-256) and the pipelines that
# wrote them. Not the whole answer: it carries the writing pipeline's status and update time, which move on while the
# main pipeline that built the files still runs (a tag's publish no longer waits for the release runner).
files_fingerprint() {
  printf '%s|%s|%s' \
    "$(printf '%s' "$1" | grep -o '{"id":[0-9][0-9]*,"package_id"' | grep -o '[0-9][0-9]*,' | sort | tr -d '\n')" \
    "$(printf '%s' "$1" | grep -o '"file_sha256":"[0-9a-f]*"' | sort | tr -d '\n')" \
    "$(written_by_pipelines "$1")"
}
