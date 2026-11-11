package agent

import (
	"encoding/json"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func TestWorkCacheKeepsNewestAndFrames(t *testing.T) {
	var c workCache
	frame := func(es ...hubproto.ChatWorkEntry) []byte {
		b, _ := json.Marshal(map[string]any{"k": "work", "entries": es})
		return b
	}
	if got := c.take(frame(hubproto.ChatWorkEntry{Conv: "a", Worktree: "/w", At: 2, Branch: "x"})); len(got) != 1 {
		t.Fatalf("first take = %v", got)
	}
	if got := c.take(frame(hubproto.ChatWorkEntry{Conv: "a", Worktree: "/w", At: 1, Branch: "old"})); len(got) != 0 {
		t.Fatalf("an older record was taken: %v", got)
	}
	c.take(frame(hubproto.ChatWorkEntry{Conv: "b", Worktree: "/v", At: 3}, hubproto.ChatWorkEntry{Conv: "", Worktree: "/z", At: 9}))
	list := c.list()
	if len(list) != 2 || list[0].Conv != "b" || list[1].Branch != "x" {
		t.Fatalf("list = %+v", list)
	}
	frames := workFrames(list)
	if len(frames) != 1 {
		t.Fatalf("frames = %d", len(frames))
	}
	var f struct {
		Event   string                   `json:"event"`
		Entries []hubproto.ChatWorkEntry `json:"entries"`
	}
	if err := json.Unmarshal(frames[0], &f); err != nil || f.Event != hubproto.EventChatWork || len(f.Entries) != 2 {
		t.Fatalf("frame = %s", frames[0])
	}
}
