package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/journal"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
)

func TestJournalToolCallWritesTheCall(t *testing.T) {
	j := journal.Open(t.TempDir(), journal.Options{})
	defer j.Close()
	journal.SetDefault(j)
	defer journal.SetDefault(nil)
	ctx, _ := journal.WithScope(context.Background(), "a-1", "")
	journal.SetChat(ctx, "c-1")
	hook := journalToolCall("harnessly")
	hook(ctx, "preview_console", 42*time.Millisecond, mcpserver.TextResult("ok", false))
	hook(ctx, "app_call", 7*time.Millisecond, mcpserver.TextResult("the app did not answer", true))
	lines := j.Query(journal.Filter{})
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	ok, failed := lines[0], lines[1]
	if ok.Event != "mcp.call" || ok.Tool != "harnessly/preview_console" || ok.Dur != 42 || ok.Level != journal.Info || ok.Chat != "c-1" || ok.Trace != "a-1" {
		t.Errorf("a call: %+v", ok)
	}
	if failed.Level != journal.Error || !strings.Contains(failed.Text, "did not answer") {
		t.Errorf("a failed call: %+v", failed)
	}
}
