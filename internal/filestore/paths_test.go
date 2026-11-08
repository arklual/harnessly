package filestore

import "testing"

func TestSyncedOwnerFileIsTheOnlyHiddenFile(t *testing.T) {
	for rel, want := range map[string]bool{
		"proj/.owner.json":              true,
		"proj/board.html":               true,
		".owner.json":                   false, // the library root is not a project
		".hidden/.owner.json":           false,
		"proj/sub/.owner.json":          false,
		"proj/.owner.json/x":            false,
		"proj/.owner.json.tmp.1.2":      false,
		"proj/.owner.json~":             false,
		"proj/.OWNER.json":              false,
		"proj/.claude/settings.json":    false,
		"proj/.mcp.json":                false,
		"proj/.envrc":                   false,
		"proj/.git/config":              false,
		"proj/.owner.json/../.envrc":    false,
		"proj//.owner.json":             false,
		"proj/.owner.json\n":            false,
		"proj/node_modules/.owner.json": false,
	} {
		if got := Synced(rel); got != want {
			t.Errorf("Synced(%q) = %v, want %v", rel, got, want)
		}
	}
	if SkippedDir("proj") || !SkippedDir("proj/.claude") {
		t.Error("SkippedDir changed")
	}
}

func TestSyncedFolderLayouts(t *testing.T) {
	for rel, want := range map[string]bool{
		".folders/b4a0f3045b62f7ea5e3212f0d5bb9b19.json":         true,
		".folders/B4A0F3045B62F7EA5E3212F0D5BB9B19.json":         false,
		".folders/b4a0f3045b62f7ea5e3212f0d5bb9b1.json":          false,
		".folders/b4a0f3045b62f7ea5e3212f0d5bb9b19.json.tmp.1.2": false,
		".folders/x/b4a0f3045b62f7ea5e3212f0d5bb9b19.json":       false,
		".folders/.mcp.json":                                     false,
		".folders":                                               false,
		"proj/.folders/b4a0f3045b62f7ea5e3212f0d5bb9b19.json":    false,
		".claims/b4a0f3045b62f7ea5e3212f0d5bb9b19.json":          false,
		"./.folders/b4a0f3045b62f7ea5e3212f0d5bb9b19.json":       false,
		".folders/../.envrc":                                     false,
	} {
		if got := Synced(rel); got != want {
			t.Errorf("Synced(%q) = %v, want %v", rel, got, want)
		}
	}
	if SkippedDir(".folders") || !SkippedDir(".claims") || !SkippedDir(".folders/x") {
		t.Error("SkippedDir: .folders is walked, other hidden folders are not")
	}
}
