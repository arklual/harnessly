package transcript

import (
	"encoding/json"
	"strings"
	"testing"
)

// An app's card reaches the feed whole (HRN-644): a tool result of 20 KB that names a view is kept as compact JSON, as a
// Claude Code transcript writes an MCP tool's result (a text item with the JSON), not cut to a first line of 100.
func TestCardResultKeepsAWholeCard(t *testing.T) {
	rows := make([]map[string]any, 0, 200)
	for i := 0; i < 200; i++ {
		rows = append(rows, map[string]any{"n": i, "title": strings.Repeat("задача ", 8)})
	}
	card, _ := json.MarshalIndent(map[string]any{
		"_meta":             map[string]any{"ui": map[string]any{"resourceUri": "ui://acme.pulse/card"}},
		"structuredContent": map[string]any{"rows": rows},
	}, "", "  ")
	if len(card) < 20<<10 {
		t.Fatalf("card of %d bytes, want 20 KB at least", len(card))
	}
	item, _ := json.Marshal([]map[string]string{{"type": "text", "text": string(card)}})
	content, err := claudeParseJSON(string(item))
	if err != nil {
		t.Fatal(err)
	}
	got := summarizeToolResult(content, false)
	var back struct {
		Structured struct {
			Rows []any `json:"rows"`
		} `json:"structuredContent"`
	}
	if json.Unmarshal([]byte(got), &back) != nil || len(back.Structured.Rows) != 200 || strings.Contains(got, "\n") {
		t.Fatalf("card cut: %d bytes, %.120q", len(got), got)
	}
	// A result that names no view stays a first line.
	plain, _ := claudeParseJSON(`[{"type":"text","text":"` + strings.Repeat("x", 300) + `"}]`)
	if s := summarizeToolResult(plain, false); len(s) > 100 {
		t.Fatalf("plain result kept whole: %d", len(s))
	}
}
