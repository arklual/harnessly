#!/usr/bin/env bash
# Once a day on a machine logged in to claude.ai: read the name of the vendor's current release of the board engine and
# report each release once, compared with the built-in engine when a canvas is named. See docs/design-engine-upstream.md.
#   DESIGN_ENGINE_WATCH_CORPUS       folder of control boards (required)
#   DESIGN_ENGINE_WATCH_OUT          where the engine and the reports go (default ~/.harnessly/design-engine-watch)
#   DESIGN_ENGINE_WATCH_CANVAS       canvas of the design type to download from; without it releases are only reported
#   DESIGN_ENGINE_WATCH_PARITY_ARGS  more arguments of the comparison, split on spaces (--playwright DIR, --sample N)
#   DESIGN_ENGINE_WATCH_NOTIFY       command run once per release with the report's path ("" when not compared), the release
#                                    and the comparison's exit code ("skipped"); when it fails the next run calls it again
set -euo pipefail

corpus="${DESIGN_ENGINE_WATCH_CORPUS:?name the control boards folder}"
out="${DESIGN_ENGINE_WATCH_OUT:-$HOME/.harnessly/design-engine-watch}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
mkdir -p "$out"

field() { node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>console.log(JSON.parse(s)[process.argv[1]]))' "$1"; }

release="$(harnessly --json design engine check | field release)"
if [ -e "$out/$release.notified" ]; then
  exit 0
fi

# Comparing needs the release's engine, and downloading it needs a canvas. Without one the release is only reported.
if [ -z "${DESIGN_ENGINE_WATCH_CANVAS:-}" ]; then
  printf 'release %s: not compared, no canvas\n' "$release"
  if [ -n "${DESIGN_ENGINE_WATCH_NOTIFY:-}" ]; then
    "$DESIGN_ENGINE_WATCH_NOTIFY" "" "$release" skipped
  fi
  : >"$out/$release.notified"
  exit 0
fi

# A folder of its own: a release in the machine's engine folder would move this machine's new projects onto it.
fetched="$(harnessly --json design engine fetch --dir "$out/engine" --canvas "$DESIGN_ENGINE_WATCH_CANVAS")"
release="$(printf '%s' "$fetched" | field release)"
engine="$(printf '%s' "$fetched" | field engine)"
case "$engine" in
  /*) ;;
  *) engine="$out/engine/$engine" ;; # a release already kept is named from the engine folder
esac

# By what is done, not by whether the fetch was new: a run stopped halfway finishes its release next time.
if [ -e "$out/$release.notified" ]; then
  exit 0
fi
report="$out/$release.txt"
if [ ! -e "$out/$release.status" ]; then
  read -r -a more <<<"${DESIGN_ENGINE_WATCH_PARITY_ARGS:-}"
  status=0
  nice -n 15 node "$here/design-engine-parity.mjs" --corpus "$corpus" --work "$out/work" --out "$out/$release.json" \
    --vendor "$engine" ${more[@]+"${more[@]}"} >"$report" 2>&1 || status=$?
  rm -rf "$out/work"
  printf '%s\n' "$status" >"$out/$release.status"
fi
status="$(cat "$out/$release.status")"
printf 'release %s: parity exit %s, report %s\n' "$release" "$status" "$report"
if [ -n "${DESIGN_ENGINE_WATCH_NOTIFY:-}" ]; then
  "$DESIGN_ENGINE_WATCH_NOTIFY" "$report" "$release" "$status"
fi
: >"$out/$release.notified"
