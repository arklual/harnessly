package history

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// the hub's copy keeps a tool's whole output, pages carry its first 4 KB as the machine's do, and
// `…/entries/<id>/output` gives it whole — with the machine off.
func TestTheCopyKeepsAToolsWholeOutputAndPagesCarryItsHead(t *testing.T) {
	ctx, db := context.Background(), openDB(t)
	_ = ApplyBackfill(ctx, db, mine, hubproto.HistoryBackfill{Ch: "convs", Events: []json.RawMessage{upsert("tab-a", 1, 1)}}, now)
	whole := strings.Repeat("я", 3000) // 6000 bytes
	tool := func(text string, truncated any) map[string]any {
		return map[string]any{"id": "c:t1#3", "turnId": "c:t1#0", "at": 1001, "rev": 2, "t": "tool",
			"output": map[string]any{"text": text, "truncated": truncated}}
	}
	cut := tool(whole[:4096], map[string]any{"shown": 4096, "total": len(whole)})
	t1 := turn("c:t1#0", 1000)
	reset := func(e map[string]any) json.RawMessage {
		return j(map[string]any{"type": "feed_reset", "turns": []any{t1}, "entries": []any{entry("c:t1#0", "c:t1#0", 1000), e}, "beforeTurnId": nil})
	}
	for _, ev := range []json.RawMessage{reset(cut), reset(tool(whole, nil)), reset(cut)} {
		if err := ApplyBackfill(ctx, db, mine, hubproto.HistoryBackfill{Ch: "conv:tab-a", Events: []json.RawMessage{ev}}, now); err != nil {
			t.Fatal(err)
		}
	}
	type out struct {
		Text      string `json:"text"`
		Truncated *struct {
			Shown, Total int
		} `json:"truncated"`
	}
	raw, err := EntryOutput(ctx, db, "u-sergey", "tab-a", "c:t1#3", []string{"m-1"})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		EntryID string `json:"entryId"`
		Output  out    `json:"output"`
	}
	if json.Unmarshal(raw, &got) != nil || got.EntryID != "c:t1#3" || got.Output.Text != whole || got.Output.Truncated != nil {
		t.Fatalf("the whole output: %.200s", raw)
	}
	_, steps, err := TurnEntries(ctx, db, "u-sergey", "tab-a", "c:t1#0", []string{"m-1"})
	if err != nil || len(steps) != 2 {
		t.Fatalf("steps %d %v", len(steps), err)
	}
	var step struct {
		Output out `json:"output"`
	}
	if json.Unmarshal(steps[1], &step) != nil || len(step.Output.Text) > 4096 || step.Output.Truncated == nil ||
		step.Output.Truncated.Shown != len(step.Output.Text) || step.Output.Truncated.Total != len(whole) {
		t.Fatalf("the page's step: %d bytes %+v", len(step.Output.Text), step.Output.Truncated)
	}
	if _, err := EntryOutput(ctx, db, "u-roma", "tab-a", "c:t1#3", []string{"m-1"}); err != ErrNotFound {
		t.Errorf("another person reads the output: %v", err)
	}
	if _, err := EntryOutput(ctx, db, "u-sergey", "tab-a", "c:t1#0", []string{"m-1"}); err != ErrNotFound {
		t.Errorf("an entry without output: %v", err)
	}
}

func TestTurnsForAPageWithoutLinks(t *testing.T) {
	both := json.RawMessage(`{"id":"t1","outputs":[{"kind":"stand","op":"created","title":"s","ref":"https://s.example.com"},{"kind":"task","op":"created","title":"HRN-1","ref":"https://t.example.com/issue/HRN-1"}]}`)
	only := json.RawMessage(`{"id":"t2","outputs":[{"kind":"commit","op":"created","title":"c","ref":"https://g.example.com/p/-/commit/abc1234"}]}`)
	none := json.RawMessage(`{"id":"t3"}`)
	got := TurnsFor([]json.RawMessage{both, only, none}, false)
	if strings.Contains(string(got[0]), "task") || !strings.Contains(string(got[0]), "stand") {
		t.Fatalf("both: %s", got[0])
	}
	if strings.Contains(string(got[1]), "outputs") {
		t.Fatalf("only: %s", got[1])
	}
	if string(got[2]) != string(none) {
		t.Fatalf("none: %s", got[2])
	}
	if kept := TurnsFor([]json.RawMessage{both}, true); string(kept[0]) != string(both) {
		t.Fatal("a page with links gets the turn as it is")
	}
}
