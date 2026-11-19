#!/bin/sh
# Puts the mocked blender on PATH; run from the workspace by the scenario's setup; leaves nothing of itself there.
set -e
mkdir -p "$HOME/.local/bin"
cp mock/blender "$HOME/.local/bin/blender"
chmod 755 "$HOME/.local/bin/blender"
rm -rf mock
