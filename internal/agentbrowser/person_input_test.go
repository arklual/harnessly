package agentbrowser

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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
			if !restored.pages[p.ID].PersonInput || p.Control != "agent" {
				t.Fatal("panel input did not persist its guard without taking control")
			}
			writes := i.writes
			must(t, v.input(t.Context(), i, "chat", in))
			if i.writes != writes {
				t.Fatal("repeated input synchronously rewrote an already dirty guard")
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
