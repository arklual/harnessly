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

// HRN-881: of a project's hidden media folder only the published files and the PDF mark sync.
func TestPublishedMediaSyncs(t *testing.T) {
	for rel, want := range map[string]bool{
		"deck/.media/published/deck.pdf":     true,
		"deck/.media/published/deck.pdf.rev": true,
		"deck/.media/pdf-auto":               true,
		"deck/.media/published/.hidden":      false,
		"deck/.media/published/CLAUDE.md":    false,
		"deck/.media/published/x.html":       false,
		"deck/.media/published/video.mp4":    true,
		"deck/.media/published/a/b.pdf":      false,
		"deck/.media/rec/x.webm":             false,
		"deck/.media/exports/x.pdf":          false,
		".media/published/deck.pdf":          false,
		"deck/project/.media/published/x":    false,
	} {
		if got := Synced(rel); got != want {
			t.Errorf("Synced(%q) = %v, want %v", rel, got, want)
		}
	}
	for rel, want := range map[string]bool{"deck/.media": false, "deck/.media/published": false, "deck/.media/rec": true, "deck/.claims": true} {
		if got := SkippedDir(rel); got != want {
			t.Errorf("SkippedDir(%q) = %v, want %v", rel, got, want)
		}
	}
}
