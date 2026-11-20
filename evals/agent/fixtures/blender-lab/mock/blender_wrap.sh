#!/bin/sh
# `blender` of this machine (~/.local/bin/blender -> this file; HRN-1708). Runs the real Blender with the same arguments,
# then shows every .blend the run just saved in <blender-lab>/out as a Sinopia 3D-scene document, so the person sees the
# scene without the agent having to remember the step. Same .blend = same document, so a rerun makes no duplicates.
# BLENDER_REAL overrides the binary; BLENDER_LAB_NO_SINOPIA=1 turns the step off.
SELF=$(readlink -f "$0")
LAB=$(dirname "$(dirname "$SELF")")
REAL=${BLENDER_REAL:-$HOME/opt/blender/blender}
MARK=$(mktemp "${TMPDIR:-/tmp}/blender-wrap.XXXXXX") || exit 1
trap 'rm -f "$MARK"' EXIT

"$REAL" "$@"
code=$?
[ -n "$BLENDER_LAB_NO_SINOPIA" ] && exit $code

# the scene script of this run: the argument after -P / --python, when it lies in scenes/
script=
prev=
for a in "$@"; do
  case $prev in -P|--python) script=$a ;; esac
  prev=$a
done
case $script in
  "$LAB"/scenes/*.py) script=scenes/${script##*/} ;;
  scenes/*.py) ;;
  ./scenes/*.py) script=${script#./} ;;
  *) script= ;;
esac

# Blends of this run only (newer than the start), no GPU packs or backups.
for f in "$LAB"/out/*.blend; do
  [ -f "$f" ] && [ "$f" -nt "$MARK" ] || continue
  case ${f##*/} in *pack*|*.blend1) continue ;; esac
  rel=out/${f##*/}
  set -- design scene3d add --root "$LAB" --blend "$rel"
  [ -n "$script" ] && set -- "$@" --script "$script"
  if out=$(harnessly "$@" 2>&1); then
    printf '[blender-lab] %s -> Sinopia: %s\n' "$rel" "$out"
  else
    printf '[blender-lab] %s was saved but NOT shown in Sinopia (%s). Run it yourself: harnessly %s\n' "$rel" "$out" "$*"
  fi
done
exit $code
