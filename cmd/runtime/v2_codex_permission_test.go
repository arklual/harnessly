package main

import (
	"reflect"
	"testing"
)

// Codex asks about a patch with its whole body; the card names the files, as Claude's Edit does.
func TestPermissionInputOfACodexPatch(t *testing.T) {
	tool := "apply_patch"
	in := permissionInput(&tool, map[string]any{"command": "*** Begin Patch\n*** Add File: /h/.codex/skills/a/SKILL.md\n+probe\n*** End Patch"})
	if tool != "Edit" || !reflect.DeepEqual(in, map[string]any{"file_path": "/h/.codex/skills/a/SKILL.md"}) {
		t.Fatalf("%s %v", tool, in)
	}
	tool = "Bash"
	cmd := map[string]any{"command": "ls"}
	if in := permissionInput(&tool, cmd); tool != "Bash" || !reflect.DeepEqual(in, cmd) {
		t.Fatalf("a command changed: %s %v", tool, in)
	}
}
