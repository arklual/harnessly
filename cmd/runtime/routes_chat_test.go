package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// `harnessly chat test` marks the chat of the calling tab (current) or one by its id, through the page's command.
func TestChatTestRoute(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cli-token"), []byte("tok"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := deps{DataDir: dir, CLI: &gate.CLIToken{DataDir: dir, Headers: []string{"x-cli-token"}}}
	conv := &wire.Conversation{ID: "tab-1"}
	chats := appChats{
		ofSession: func(s string) string {
			if s == "hs-agent" {
				return "tab-1"
			}
			return ""
		},
		record: func(id string) (wire.Conversation, bool) { return *conv, id == conv.ID },
		do: func(_ context.Context, cmd wire.Command) wire.CmdResult {
			m := cmd.(wire.ConversationTestCmd)
			conv.Test = m.Test
			return wire.Accepted(wire.ChannelConvs, 1)
		},
	}
	route := chatRoutes(d, chats)[0]
	call := func(method, body string, token bool) (int, string) {
		req := httptest.NewRequest(method, "/api/cli/chat/test", bytes.NewBufferString(body))
		if token {
			req.Header.Set("x-cli-token", "tok")
		}
		w := httptest.NewRecorder()
		route.Handler.ServeHTTP(w, req)
		return w.Code, w.Body.String()
	}

	if code, body := call(http.MethodPost, `{"session":"hs-agent","test":true}`, true); code != http.StatusOK || !conv.Test || !strings.Contains(body, `"test":true`) {
		t.Fatalf("current: %d %s %v", code, body, conv.Test)
	}
	if code, _ := call(http.MethodPost, `{"id":"tab-1","test":false}`, true); code != http.StatusOK || conv.Test {
		t.Fatalf("by id, off: %d %v", code, conv.Test)
	}
	if code, body := call(http.MethodPost, `{"session":"other","test":true}`, true); code != http.StatusNotFound || !strings.Contains(body, "no-chat") {
		t.Fatalf("outside a chat: %d %s", code, body)
	}
	if code, _ := call(http.MethodPost, `{"id":"tab-nope","test":true}`, true); code != http.StatusNotFound {
		t.Fatalf("a chat that is not there: %d", code)
	}
	if code, _ := call(http.MethodPost, `{"test":true}`, true); code != http.StatusBadRequest {
		t.Fatalf("no chat named: %d", code)
	}
	if code, _ := call(http.MethodPost, `{"id":"tab-1","test":true}`, false); code != http.StatusForbidden {
		t.Fatalf("without a token: %d", code)
	}
	if code, _ := call(http.MethodGet, ``, true); code != http.StatusMethodNotAllowed {
		t.Fatalf("a GET: %d", code)
	}
}

// `harnessly chat delete` removes only a chat the calling one delegated.
func TestChatDeleteRoute(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cli-token"), []byte("tok"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := deps{DataDir: dir, CLI: &gate.CLIToken{DataDir: dir, Headers: []string{"x-cli-token"}}}
	parent := "tab-1"
	other := "tab-9"
	convs := map[string]wire.Conversation{"tab-1": {ID: "tab-1"}, "tab-2": {ID: "tab-2", ParentID: &parent}, "tab-3": {ID: "tab-3", ParentID: &other}}
	var deleted []string
	chats := appChats{
		ofSession: func(s string) string {
			if s == "hs-agent" {
				return "tab-1"
			}
			return ""
		},
		record: func(id string) (wire.Conversation, bool) { c, ok := convs[id]; return c, ok },
		do: func(_ context.Context, cmd wire.Command) wire.CmdResult {
			m := cmd.(wire.ConversationDeleteCmd)
			if m.Force != nil {
				t.Fatal("never forced")
			}
			deleted = append(deleted, m.ID)
			return wire.Accepted(wire.ChannelConvs, 1)
		},
	}
	route := chatRoutes(d, chats)[1]
	call := func(body string) (int, string) {
		req := httptest.NewRequest(http.MethodPost, "/api/cli/chat/delete", bytes.NewBufferString(body))
		req.Header.Set("x-cli-token", "tok")
		w := httptest.NewRecorder()
		route.Handler.ServeHTTP(w, req)
		return w.Code, w.Body.String()
	}
	if code, body := call(`{"session":"hs-agent","id":"tab-2"}`); code != http.StatusOK || len(deleted) != 1 || deleted[0] != "tab-2" {
		t.Fatalf("own child: %d %s %v", code, body, deleted)
	}
	if code, body := call(`{"session":"hs-agent","id":"tab-3"}`); code != http.StatusForbidden || !strings.Contains(body, "not-yours") {
		t.Fatalf("another chat's child: %d %s", code, body)
	}
	if code, _ := call(`{"session":"hs-agent","id":"tab-1"}`); code != http.StatusForbidden {
		t.Fatalf("itself: %d", code)
	}
	if code, _ := call(`{"session":"other","id":"tab-2"}`); code != http.StatusNotFound {
		t.Fatalf("outside a chat: %d", code)
	}
	if code, _ := call(`{"session":"hs-agent","id":"tab-nope"}`); code != http.StatusNotFound {
		t.Fatalf("not there: %d", code)
	}
	if len(deleted) != 1 {
		t.Fatalf("deleted %v", deleted)
	}
}
