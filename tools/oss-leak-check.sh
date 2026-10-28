#!/usr/bin/env bash
# Checks a tree about to be published for what must stay internal; exits non-zero on any finding.
#
#   OSS_FORBIDDEN_FILE=tools/oss-forbidden.txt tools/oss-leak-check.sh <dir>
#
# 1. Forbidden words (OSS_FORBIDDEN_FILE: one extended regex per line, `warn:` lines only reported, `strip:` lines
#    skipped; or OSS_FORBIDDEN, a single regex), case-insensitive, in file contents (binary files too) and file paths. The words belong to the
#    deployment, so they are passed in and not written here.
# 2. Secrets: gitleaks over the directory with the tree's own .gitleaks.toml (GITLEAKS names the binary; required).
# 3. Private network addresses (10/8, 172.16/12, 192.168/16, 100.64/10) other than the example ones tests use (and the hub compose network 172.29.40.0/24), outside
#    minified vendor files. OSS_IP_ALLOW extends the allowed ones (an extended regex of whole addresses).
# 4. Big files: a binary file over OSS_MAX_BINARY_KB (default 1024) or any file over OSS_MAX_FILE_KB (default 4096).
set -euo pipefail

die() {
  echo "oss-leak-check: $*" >&2
  exit 2
}

[ $# -eq 1 ] || die "usage: $0 <dir>"
dir=$1
[ -d "$dir" ] || die "$dir is not a directory"
if [ -n "${OSS_FORBIDDEN_FILE:-}" ]; then
  [ -f "$OSS_FORBIDDEN_FILE" ] || die "OSS_FORBIDDEN_FILE $OSS_FORBIDDEN_FILE does not exist"
  OSS_FORBIDDEN_FILE=$(cd "$(dirname "$OSS_FORBIDDEN_FILE")" && pwd)/$(basename "$OSS_FORBIDDEN_FILE")
fi
cd "$dir"

failed=0
report() {
  # report <title> <findings...>: prints them and marks the check failed.
  local title=$1
  shift
  [ $# -gt 0 ] || return 0
  echo "== $title" >&2
  printf '  %s\n' "$@" >&2
  failed=1
}

# 1. Forbidden words.
deny=() warn=()
if [ -n "${OSS_FORBIDDEN_FILE:-}" ]; then
  while IFS= read -r line || [ -n "$line" ]; do
    line=${line%$'\r'}
    case $line in
    '' | '#'*) ;;
    warn:*) warn+=("${line#warn:}") ;;
    strip:*) ;; # tools/oss-export.sh's tracker keys, not a pattern
    *) deny+=("$line") ;;
    esac
  done <"$OSS_FORBIDDEN_FILE"
fi
[ -n "${OSS_FORBIDDEN:-}" ] && deny+=("$OSS_FORBIDDEN")
[ ${#deny[@]} -gt 0 ] || die "no forbidden words: set OSS_FORBIDDEN_FILE or OSS_FORBIDDEN"

join() {
  local IFS='|'
  echo "$*"
}
deny_re=$(join "${deny[@]}")
mapfile -t hits < <({
  find . -path ./.git -prune -o -type f -print | sed 's|^\./||' | grep -iE -- "$deny_re" | sed 's/$/ (path)/' || true
  grep -rnaiEo --exclude-dir=.git -- "$deny_re" . | sed 's|^\./||' | cut -c1-200 || true
} | sort -u)
report "forbidden words" "${hits[@]}"
if [ ${#warn[@]} -gt 0 ]; then
  mapfile -t notes < <(grep -rIliE --exclude-dir=.git -- "$(join "${warn[@]}")" . | sed 's|^\./||' | sort || true)
  if [ ${#notes[@]} -gt 0 ]; then
    echo "== warnings (not failing): files with $(join "${warn[@]}")" >&2
    printf '  %s\n' "${notes[@]}" >&2
  fi
fi

# 2. Secrets.
gitleaks=${GITLEAKS:-gitleaks}
command -v "$gitleaks" >/dev/null || die "gitleaks not found (set GITLEAKS)"
config=()
[ -f .gitleaks.toml ] && config=(--config .gitleaks.toml)
if ! "$gitleaks" dir . "${config[@]}" --no-banner --redact --log-level warn >&2; then
  report "secrets" "gitleaks found the secrets above"
fi

# 3. Private addresses. The allowed ones are the examples of tests and docs: the first hosts of a range, its bounds.
ip_allow='10\.0\.0\.[0-9]{1,3}|10\.1\.2\.3|10\.[0-9]{1,3}\.0\.[01]|192\.168\.[01]\.[0-9]{1,3}|172\.16\.0\.[0-9]{1,3}|172\.31\.255\.255|172\.29\.40\.[0-9]{1,3}|100\.64\.0\.[0-9]{1,3}|100\.100\.[0-9]{1,3}\.[0-9]{1,3}|100\.127\.255\.255'
[ -n "${OSS_IP_ALLOW:-}" ] && ip_allow="$ip_allow|$OSS_IP_ALLOW"
private='(10\.[0-9]{1,3}|172\.(1[6-9]|2[0-9]|3[01])|192\.168|100\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7]))\.[0-9]{1,3}\.[0-9]{1,3}'
mapfile -t ips < <(grep -rIoEn --exclude-dir=.git --exclude='*.min.js' -- "(^|[^0-9.])$private([^0-9.]|$)" . |
  sed -E 's|^\./||; s/^([^:]+:[0-9]+):[^0-9]*([0-9.]*[0-9]).*$/\1: \2/' | grep -vE ": ($ip_allow)\$" | sort -u || true)
report "private network addresses" "${ips[@]}"

# 4. Big files.
max_bin=$((${OSS_MAX_BINARY_KB:-1024} * 1024))
max_any=$((${OSS_MAX_FILE_KB:-4096} * 1024))
big=()
while IFS= read -r -d '' f; do
  size=$(stat -c %s "$f")
  if [ "$size" -gt "$max_any" ]; then
    big+=("${f#./} ($((size / 1024)) KB)")
  elif [ "$size" -gt "$max_bin" ] && ! grep -qI . "$f"; then
    big+=("${f#./} (binary, $((size / 1024)) KB)")
  fi
done < <(find . -path ./.git -prune -o -type f -size +"$((max_bin < max_any ? max_bin : max_any))c" -print0)
report "big files" "${big[@]}"

if [ "$failed" -ne 0 ]; then
  echo "oss-leak-check: the tree is not fit to publish" >&2
  exit 1
fi
echo "oss-leak-check: clean"
