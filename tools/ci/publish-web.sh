#!/bin/sh
# Publishes the page of a release tag vX.Y.Z apart from the binaries: the generic package web/X.Y.Z holds
# web.tar.gz (web/dist with its contract, web-release.json) and SHA256SUMS. Only a pipeline of a protected tag writes it:
# the package name is protected (Maintainer to push, package protection rule of the project), and whoever takes it (the
# hub, internal/webpkg) checks that every file came from the pipeline of the tag vX.Y.Z and that the tarball's hash is the
# one SHA256SUMS lists.
#
# usage: publish-web.sh   (tag pipeline of vX.Y.Z, web/dist built)
set -eu

test -f web/dist/index.html || { echo 'no web/dist: build the page first' >&2; exit 1; }
test -f web/dist/web-release.json || { echo 'web/dist has no web-release.json: tools/web-compat.mjs manifest' >&2; exit 1; }
version="${CI_COMMIT_TAG#v}"
out="$(mktemp -d)"
# Fixed order, owner and time: the same page gives the same tarball.
tar --sort=name --owner=0 --group=0 --numeric-owner --mtime='2000-01-01 00:00Z' -C web/dist -czf "$out/web.tar.gz" .
(cd "$out" && sha256sum web.tar.gz > SHA256SUMS && cat SHA256SUMS)
for f in web.tar.gz SHA256SUMS; do
  curl --fail --silent --show-error --retry 2 --header "JOB-TOKEN: $CI_JOB_TOKEN" --upload-file "$out/$f" \
    "$CI_API_V4_URL/projects/$CI_PROJECT_ID/packages/generic/web/$version/$f" >/dev/null
done
echo "published web/$version"
