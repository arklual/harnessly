package antigravity

import (
	"reflect"
	"testing"
)

func TestFamilies(t *testing.T) {
	got := Families([]Model{
		{ID: "gemini-4-neon-high", Label: "Gemini 4 Neon (High)"}, {ID: "gemini-4-neon-xhigh", Label: "Gemini 4 Neon (XHigh)"},
		{ID: "gemini-4-neon-low", Label: "Gemini 4 Neon (Low)"}, {ID: "gemini-pro-orchestrator-neon", Label: "Orchestrator (Neon)"},
		{ID: "gemini-pro-orchestrator", Label: "Orchestrator (Carbon)"}, {ID: "claude-opus-5-5-medium", Label: "Claude Opus 5.5 (Medium)"},
		{ID: "claude-opus-5-5-low", Label: "Claude Opus 5.5 (Low)"}, {ID: "gpt-oss-120b-medium", Label: "GPT-OSS 120B (Medium)"},
	})
	want := []Family{
		{ID: "gemini-4-neon", Label: "Gemini 4 Neon", Efforts: []string{"low", "high", "xhigh"}, DefaultEffort: "high"},
		{ID: "gemini-pro-orchestrator-neon", Label: "Orchestrator (Neon)"},
		{ID: "gemini-pro-orchestrator", Label: "Orchestrator (Carbon)"},
		{ID: "claude-opus-5-5", Label: "Claude Opus 5.5", Efforts: []string{"low", "medium"}, DefaultEffort: "medium"},
		{ID: "gpt-oss-120b-medium", Label: "GPT-OSS 120B"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}
