package fileplace

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

func text(t *testing.T, res *jsvalue.Object) (string, bool) {
	t.Helper()
	content, _ := res.Get("content")
	list, _ := content.([]any)
	first, _ := list[0].(*jsvalue.Object)
	got, _ := first.Get("text")
	failed, _ := res.Get("isError")
	isErr, _ := failed.(bool)
	s, _ := got.(string)
	return s, isErr
}

func TestTheAgentReadsWhatThePageReported(t *testing.T) {
	store := NewStore()
	post := func(body string) int {
		w := httptest.NewRecorder()
		store.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v2/pane-selection", strings.NewReader(body)))
		return w.Code
	}
	tools := Tools{Store: store, Chat: "c1"}
	if got, _ := text(t, tools.Call(context.Background(), ToolSelection, jsvalue.NewObject())); !strings.Contains(got, "Nothing") {
		t.Fatalf("with nothing reported: %q", got)
	}
	if code := post(`{"conversation":"c1","selection":{"path":"/w/a.xlsx","kind":"cells","label":"D2","place":"D2","what":"cell D2","content":"5"}}`); code != http.StatusNoContent {
		t.Fatalf("report: %d", code)
	}
	got, _ := text(t, tools.Call(context.Background(), ToolSelection, jsvalue.NewObject()))
	for _, want := range []string{"/w/a.xlsx", "cell D2", "place: D2", "content:\n5"} {
		if !strings.Contains(got, want) {
			t.Fatalf("no %q in %q", want, got)
		}
	}
	other, _ := text(t, Tools{Store: store, Chat: "c2"}.Call(context.Background(), ToolSelection, jsvalue.NewObject()))
	if !strings.Contains(other, "Nothing") {
		t.Fatalf("another chat read it: %q", other)
	}
	if code := post(`{"conversation":"c1","selection":null}`); code != http.StatusNoContent {
		t.Fatalf("clearing: %d", code)
	}
	if _, ok := store.Of("c1"); ok {
		t.Fatal("a cleared selection stayed")
	}
	if code := post(`{"selection":null}`); code != http.StatusBadRequest {
		t.Fatalf("a report of no chat: %d", code)
	}
}

func TestShowOpensOnlyWhatThePaneMayOpen(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "a.xlsx")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	var shown []string
	tools := Tools{
		Store:  NewStore(),
		Access: files.NewAccess([]string{dir}, nil),
		Chat:   "c1",
		Cwd:    dir,
		Show: func(chat, path, place string) error {
			shown = append(shown, chat+"|"+path+"|"+place)
			return nil
		},
	}
	call := func(pairs ...any) (string, bool) {
		return text(t, tools.Call(context.Background(), ToolShow, jsvalue.NewObject(pairs...)))
	}
	if got, failed := call("path", "a.xlsx", "place", " D2:D6 "); failed {
		t.Fatalf("refused: %s", got)
	}
	if len(shown) != 1 || shown[0] != "c1|"+file+"|D2:D6" {
		t.Fatalf("shown %v", shown)
	}
	if _, failed := call("path", "/etc/hostname"); !failed {
		t.Fatal("a file outside the workspace was shown")
	}
	if _, failed := call("path", "a.xlsx", "place", strings.Repeat("x", 300)); !failed {
		t.Fatal("an endless place was taken")
	}
	tools.Show = func(string, string, string) error { return errors.New("the chat has no pane") }
	if got, failed := call("path", "a.xlsx"); !failed || !strings.Contains(got, "no pane") {
		t.Fatalf("a chat with no pane: %q", got)
	}
	tools.Chat, tools.Why = "", "no chat"
	if got, failed := call("path", "a.xlsx"); !failed || !strings.Contains(got, "no chat") {
		t.Fatalf("an agent in no chat: %q", got)
	}
}
