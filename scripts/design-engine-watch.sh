#!/usr/bin/env bash
# Once a day on a machine logged in to claude.ai: fetch the vendor's newest release of the board engine and, when it is
# new, compare it with the built-in engine. See docs/design-engine-upstream.md.
#   DESIGN_ENGINE_WATCH_CORPUS  folder of control boards (required)
#   DESIGN_ENGINE_WATCH_OUT     where reports go (default ~/.harnessly/design-engine-watch)
#   DESIGN_ENGINE_WATCH_NOTIFY  command run with the report's path when a release is new
set -euo pipefail

corpus="${DESIGN_ENGINE_WATCH_CORPUS:?name the control boards folder}"
out="${DESIGN_ENGINE_WATCH_OUT:-$HOME/.harnessly/design-engine-watch}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
mkdir -p "$out"

field() { node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>console.log(JSON.parse(s)[process.argv[1]]))' "$1"; }

fetched="$(harnessly --json design engine fetch)"
if [ "$(printf '%s' "$fetched" | field new)" != "true" ]; then
  exit 0
fi
release="$(printf '%s' "$fetched" | field release)"
report="$out/$release.txt"
status=0
nice -n 15 node "$here/design-engine-parity.mjs" "$corpus" >"$report" 2>&1 || status=$?
printf 'release %s: parity exit %s, report %s\n' "$release" "$status" "$report"
if [ -n "${DESIGN_ENGINE_WATCH_NOTIFY:-}" ]; then
  "$DESIGN_ENGINE_WATCH_NOTIFY" "$report"
fi
