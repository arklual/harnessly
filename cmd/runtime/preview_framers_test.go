package main

import (
	"encoding/json"
	"testing"
)

// A slot entered from the product and from a stand beside it (one site, one partitioned cookie) may be framed by
// both: the cookie remembers the pages the same person came from, and an old cookie without them still reads.
func TestPreviewSlotRemembersItsPages(t *testing.T) {
	base := map[string]any{"sub": "u1", "origin": "https://u-1.preview.example", "app": "https://ai.example", "slot": 1.0,
		"target": map[string]any{"kind": "port", "port": 5173.0}}
	old, ok := previewSessionOf(base)
	if !ok || len(old.framers()) != 1 {
		t.Fatalf("an old cookie: %+v %v", old, ok)
	}
	stand := *old
	stand.app, stand.apps = "https://ai-v2.stand.example", []string{old.app}
	raw, _ := json.Marshal(stand.fields())
	var back map[string]any
	_ = json.Unmarshal(raw, &back)
	got, ok := previewSessionOf(back)
	if !ok || !got.drives("https://ai.example") || !got.drives("https://ai-v2.stand.example") || got.drives("https://evil.example") {
		t.Fatalf("framers: %v", got.framers())
	}
	if csp := previewFrameAncestors(got.framers()...); csp != "frame-ancestors https://ai-v2.stand.example https://ai.example 'self'" {
		t.Fatalf("csp: %q", csp)
	}
	back["apps"] = []any{"javascript:alert(1)", "https://a.example", "https://b.example", "https://c.example", "https://d.example"}
	if got, _ := previewSessionOf(back); len(got.apps) != previewAppsKept || got.drives("javascript:alert(1)") {
		t.Fatalf("a forged list: %v", got.apps)
	}
}
