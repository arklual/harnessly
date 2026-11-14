package main

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestWebVitalGoesToTheJournalWithoutAHub(t *testing.T) {
	j := journal.Open(t.TempDir(), journal.Options{})
	defer j.Close()
	journal.SetDefault(j)
	defer journal.SetDefault(nil)
	routes := journalRoutes(deps{SessionOK: func(*http.Request) bool { return true }})
	serve := func(body string) int {
		r := httptest.NewRequest(http.MethodPost, "/api/v2/web-vitals", strings.NewReader(body))
		for _, rt := range routes {
			if rt.Match(r) {
				w := httptest.NewRecorder()
				rt.Handler.ServeHTTP(w, r)
				return w.Code
			}
		}
		return 0
	}
	if code := serve(`{"name":"sinopia_home_full","seconds":1.25}`); code != http.StatusNoContent {
		t.Fatalf("status %d", code)
	}
	if code := serve(`{"name":"made_up","seconds":1}`); code != http.StatusNoContent {
		t.Fatalf("status %d", code)
	}
	var found []journal.Entry
	for i := 0; i < 100 && len(found) == 0; i++ {
		found = j.Query(journal.Filter{Level: journal.Info, Text: "sinopia_home_full"})
		time.Sleep(20 * time.Millisecond)
	}
	if len(found) != 1 || found[0].Event != "web-vital" || !strings.Contains(found[0].Text, "1.250 s") {
		t.Fatalf("journal: %+v", found)
	}
	if other := j.Query(journal.Filter{Level: journal.Info, Text: "made_up"}); len(other) != 0 {
		t.Fatalf("an unknown name was kept: %+v", other)
	}
}
