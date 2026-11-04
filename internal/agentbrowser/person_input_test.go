package agentbrowser

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/cdp"
)

func TestPanelInputPersistsGuardBeforeDispatch(t *testing.T) {
	for _, in := range []Input{
		{T: "text", Text: "fixture"},
		{T: "key", Key: "a", Code: "KeyA"},
		{T: "key", Key: "v", Code: "KeyV", Ctrl: true},
	} {
		t.Run(in.T+in.Key, func(t *testing.T) {
			m := New(Config{DataDir: t.TempDir()})
			defer m.Close()
			i := newInstance(m, DefaultProfile)
			defer i.cancel()
			p := newPage(Tab{ID: "tab", Conversation: "chat", URL: "about:blank", Control: "agent"})
			i.pages[p.ID] = p
			i.current["chat"] = p.ID
			connectHandoffFixture(t, i, p, "")
			v := &panelView{changes: make(chan struct{}, 1)}
			must(t, v.input(t.Context(), i, "chat", in))
			restored := newInstance(m, DefaultProfile)
			defer restored.cancel()
			if !restored.pages[p.ID].PersonInput || restored.pages[p.ID].PersonDocument != "document" || p.Control != "agent" {
				t.Fatal("panel input did not persist its guard without taking control")
			}
			writes := i.writes
			must(t, v.input(t.Context(), i, "chat", in))
			if i.writes != writes {
				t.Fatal("repeated input synchronously rewrote an already dirty guard")
			}
			p.PersonDocument = "previous-document"
			must(t, v.input(t.Context(), i, "chat", in))
			if i.writes != writes+1 || p.PersonDocument != "document" {
				t.Fatal("input in a different document did not update the durable marker")
			}

		})
	}
}

func TestLazyInputInspectionFailureStaysSealedAndPending(t *testing.T) {
	for _, failure := range []string{"Page.resetNavigationHistory", "DOM.describeNode", ""} {
		t.Run(failure, func(t *testing.T) {
			m := New(Config{DataDir: t.TempDir()})
			defer m.Close()
			i := newInstance(m, DefaultProfile)
			defer i.cancel()
			p := newPage(Tab{ID: "tab", Conversation: "chat", URL: "about:blank", Control: "agent", PersonInput: true})
			i.pages[p.ID] = p
			connectHandoffFixture(t, i, p, failure)
			err := i.InspectPersonInputs(t.Context(), "chat")
			if (err != nil) != (failure != "") || p.PersonInput != (failure != "") || p.Sealed != (failure != "") || p.Control != "agent" {
				t.Fatal("lazy scan failed to retain a closed guard or clear a successful scan")
			}
		})
	}
}

func TestPersonInputPersistenceFailureRefusesDispatch(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	defer i.cancel()
	p := newPage(Tab{ID: "tab", Conversation: "chat", URL: "about:blank", Control: "agent"})
	i.pages[p.ID] = p
	blocked := filepath.Join(t.TempDir(), "blocked")
	must(t, os.WriteFile(blocked, []byte("fixture"), 0600))
	i.dir = filepath.Join(blocked, "profile")
	if _, err := i.Call(context.Background(), p.ID, "Input.insertText", map[string]any{"text": "fixture"}); err == nil {
		t.Fatal("input was dispatched without its durable guard")
	}
}

func TestPanelInputDocumentTimeout(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	defer i.cancel()
	p := newPage(Tab{ID: "tab", Conversation: "chat", URL: "about:blank", Control: "agent"})
	i.pages[p.ID] = p
	transport := &slowPersonDocumentTransport{handoffFixtureTransport: handoffFixtureTransport{replies: make(chan []byte, 1), done: make(chan struct{})}}
	c := cdp.New(transport, nil)
	i.conn = c
	p.binding = binding{conn: c, session: "fixture", target: p.ID}
	go func() { _ = c.Run(t.Context()) }()
	defer c.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	began := time.Now()
	_, err := i.Call(ctx, p.ID, "Input.insertText", map[string]any{"text": "ordinary"})
	must(t, err)
	if elapsed := time.Since(began); elapsed > time.Second {
		t.Fatalf("metadata delayed panel input for %s", elapsed)
	}
	if !transport.input || p.PersonDocument != unknownHandoffDocument {
		t.Fatal("timed-out metadata did not preserve input with an unknown marker")
	}
}

type slowPersonDocumentTransport struct {
	handoffFixtureTransport
	input bool
}

func (x *slowPersonDocumentTransport) Write(ctx context.Context, raw []byte) error {
	var request struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return err
	}
	if request.Method == "Page.getFrameTree" {
		<-ctx.Done()
		return ctx.Err()
	}
	if request.Method == "Input.insertText" {
		x.input = true
	}
	return x.handoffFixtureTransport.Write(ctx, raw)
}

func TestPersonDocumentUnknownAnchorsAndSurvivesRestart(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	defer i.cancel()
	p := newPage(Tab{ID: "tab", Conversation: "chat", URL: "about:blank", Control: "agent", PersonDocument: unknownHandoffDocument})
	i.pages[p.ID] = p
	connectHandoffFixture(t, i, p, "")
	if err := i.CheckPersonDocuments(t.Context(), "chat", p.ID); !errors.Is(err, ErrPersonDocument) {
		t.Fatal("unknown document was allowed")
	}
	if p.PersonDocument != "document" || p.Scripted {
		t.Fatal("unknown document was not anchored without marking scripted")
	}
	restored := newInstance(m, DefaultProfile)
	defer restored.cancel()
	q := restored.pages[p.ID]
	if q == nil || q.PersonDocument != "document" {
		t.Fatal("anchored document was not saved")
	}
	connectHandoffFixture(t, restored, q, "")
	if err := restored.CheckPersonDocuments(t.Context(), "chat", q.ID); !errors.Is(err, ErrPersonDocument) {
		t.Fatal("restart lost the document refusal")
	}
	q.PersonDocument = "older-document"
	must(t, restored.CheckPersonDocuments(t.Context(), "chat", q.ID))
}

func TestPanelInputMouseDoesNotGuardDocument(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	defer i.cancel()
	p := newPage(Tab{ID: "tab", Conversation: "chat", URL: "about:blank", Control: "agent"})
	i.pages[p.ID] = p
	connectHandoffFixture(t, i, p, "")
	_, err := i.Call(t.Context(), p.ID, "Input.dispatchMouseEvent", map[string]any{"type": "mouseMoved", "x": 1, "y": 1})
	must(t, err)
	if p.PersonInput || p.PersonDocument != "" {
		t.Fatal("mouse input restricted agent JavaScript")
	}
}

func TestChangesPageContent(t *testing.T) {
	for _, tc := range []struct {
		name, method string
		params       any
		want         bool
	}{
		{"insert", "Input.insertText", map[string]any{"text": "fixture"}, true},
		{"mouse", "Input.dispatchMouseEvent", map[string]any{"type": "mousePressed"}, false},
		{"text", "Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "text": "a"}, true},
		{"enter-text", "Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "text": "\r"}, true},
		{"char", "Input.dispatchKeyEvent", map[string]any{"type": "char", "text": "a"}, true},
		{"backspace", "Input.dispatchKeyEvent", map[string]any{"type": "rawKeyDown", "key": "Backspace"}, true},
		{"delete", "Input.dispatchKeyEvent", map[string]any{"type": "rawKeyDown", "code": "Delete"}, true},
		{"paste", "Input.dispatchKeyEvent", map[string]any{"type": "rawKeyDown", "key": "v", "modifiers": 2}, true},
		{"undo", "Input.dispatchKeyEvent", map[string]any{"type": "rawKeyDown", "key": "z", "modifiers": 4}, true},
		{"release-text", "Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "text": "\r"}, false},
		{"release-shortcut", "Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": "v", "modifiers": 2}, false},
		{"release-delete", "Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": "Backspace"}, false},
		{"shift-tab", "Input.dispatchKeyEvent", map[string]any{"type": "rawKeyDown", "key": "Tab", "modifiers": 8}, false},
		{"alt-arrow", "Input.dispatchKeyEvent", map[string]any{"type": "rawKeyDown", "key": "ArrowLeft", "modifiers": 1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := changesPageContent(tc.method, tc.params); got != tc.want {
				t.Fatalf("changesPageContent = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPanelNavigationKeysDoNotGuardDocument(t *testing.T) {
	for _, key := range []string{"Enter", "Tab", "ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown",
		"PageUp", "PageDown", "Home", "End", "Escape", "F1", "F12", "Shift", "a"} {
		for _, kind := range []string{"rawKeyDown", "keyUp"} {
			t.Run(key+"/"+kind, func(t *testing.T) {
				m := New(Config{DataDir: t.TempDir()})
				defer m.Close()
				i := newInstance(m, DefaultProfile)
				defer i.cancel()
				p := newPage(Tab{ID: "tab", Conversation: "chat", URL: "about:blank", Control: "agent"})
				i.pages[p.ID] = p
				// A frame lookup failure would install an unknown document guard.
				connectHandoffFixture(t, i, p, "Page.getFrameTree")
				_, err := i.Call(t.Context(), p.ID, "Input.dispatchKeyEvent", map[string]any{"type": kind, "key": key})
				must(t, err)
				if p.PersonInput || p.PersonDocument != "" {
					t.Fatal("non-editing key queried or guarded the document")
				}
			})
		}
	}
}
