#!/bin/sh
# Claude Code PreToolUse hook: refuses a Write/Edit/shell command that writes project/canvas.json of a design project,
# which the runtime builds from meta/. The hook's JSON comes on stdin; the refusal (if any) goes to stdout.
# Set HARNESSLY_BIN when the command is not on PATH. See docs/design-pages.md.
exec "${HARNESSLY_BIN:-harnessly}" hook design-guard
