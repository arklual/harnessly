package agentbrowser

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestScriptedOpenerChainPersists(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	defer i.cancel()
	for _, tab := range []Tab{
		{ID: "parent", Conversation: "chat", URL: "about:blank"},
		{ID: "popup", Conversation: "chat", URL: "about:blank", Opener: "parent"},
		{ID: "child", Conversation: "chat", URL: "about:blank", Opener: "popup"},
		{ID: "sibling", Conversation: "other", URL: "about:blank", Opener: "parent"},
		{ID: "unrelated", Conversation: "chat", URL: "about:blank"},
	} {
		i.pages[tab.ID] = newPage(tab)
	}
	if err := i.MarkScripted("other", "child"); !errors.Is(err, ErrNotOwner) {
		t.Fatal(err)
	}
	i.pages["child"].Control = "person"
	if err := i.MarkScripted("chat", "child"); !errors.Is(err, ErrPersonControl) {
		t.Fatal(err)
	}
	i.pages["child"].Control = "agent"
	must(t, i.MarkScripted("chat", "child"))
	durable := newInstance(m, DefaultProfile)
	defer durable.cancel()
	if durable.pages["child"] == nil || !durable.pages["child"].Scripted || durable.pages["parent"] == nil || !durable.pages["parent"].Scripted {
		t.Fatal("evaluate could run before script provenance reached disk")
	}
	for id, p := range i.pages {
		if p.Scripted != (id != "unrelated") {
			t.Fatalf("component propagation failed for %s", id)
		}
	}
	i.mu.Lock()
	i.removeLocked("popup")
	i.upsertLocked(target{ID: "parent", Type: "page", URL: "https://example.org/next"})
	i.mu.Unlock()
	i.save()
	restored := newInstance(m, DefaultProfile)
	defer restored.cancel()
	for id, p := range restored.pages {
		if p.Scripted != (id != "unrelated") {
			t.Fatalf("closed popup or restart reset scripted state for %s", id)
		}
	}
	if restored.pages["child"].Opener != "popup" {
		t.Fatal("restart lost opener")
	}
	restored.mu.Lock()
	late := restored.upsertLocked(target{ID: "late", Type: "page", URL: "about:blank", Opener: "parent"})
	restored.mu.Unlock()
	if !late.Scripted || late.Conversation != "chat" {
		t.Fatal("new popup did not inherit persistent state")
	}
}

func TestScriptedPersistenceFailureBlocksExecution(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	defer i.cancel()
	i.pages["tab"] = newPage(Tab{ID: "tab", URL: "about:blank", Conversation: "chat"})
	blocked := filepath.Join(t.TempDir(), "blocked")
	must(t, os.WriteFile(blocked, []byte("fixture"), 0600))
	i.dir = filepath.Join(blocked, "profile")
	if err := i.MarkScripted("chat", "tab"); err == nil {
		t.Fatal("evaluate was allowed before script provenance could be saved")
	}
	if !i.pages["tab"].Scripted {
		t.Fatal("failed persistence reset script provenance in memory")
	}
}

func TestOrphanPopupLateAttribution(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	defer i.cancel()
	i.mu.Lock()
	child := i.upsertLocked(target{ID: "child", Type: "page", URL: "about:blank", Opener: "popup"})
	popup := i.upsertLocked(target{ID: "popup", Type: "page", URL: "about:blank", Opener: "parent"})
	parent := i.upsertLocked(target{ID: "parent", Type: "page", URL: "about:blank"})
	parent.Conversation, parent.By, parent.Control, parent.Scripted = "chat", "agent", "person", true
	i.current["chat"] = parent.ID
	i.attributeOpenersLocked()
	i.mu.Unlock()
	for _, p := range []*page{popup, child} {
		if p.Conversation != "chat" || p.By != "agent" || p.Control != "person" || !p.Scripted || p.provisional {
			t.Fatalf("late attribution failed: %+v", p.Tab)
		}
	}
	if i.Current("chat") != parent.ID {
		t.Fatal("person popup changed the agent selection")
	}
	i.save()
	restored := newInstance(m, DefaultProfile)
	defer restored.cancel()
	if restored.pages["child"].Control != "person" || !restored.pages["child"].Scripted {
		t.Fatal("late attribution was not persisted")
	}
}

func TestScriptedRefusesPersonOpenerComponent(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	defer i.cancel()
	for _, tab := range []Tab{
		{ID: "parent", Conversation: "chat", URL: "about:blank", Control: "person"},
		{ID: "child", Conversation: "chat", URL: "about:blank", Control: "agent", Opener: "parent"},
		{ID: "grandchild", Conversation: "chat", URL: "about:blank", Control: "agent", Opener: "child"},
		{ID: "separate", Conversation: "chat", URL: "about:blank", Control: "agent"},
	} {
		i.pages[tab.ID] = newPage(tab)
	}
	if err := i.MarkScripted("chat", "grandchild"); !errors.Is(err, ErrPersonControl) {
		t.Fatal("evaluation can reach a person-controlled opener")
	}
	for _, tab := range i.Tabs("chat") {
		if tab.Scripted {
			t.Fatal("refused evaluation changed script provenance")
		}
	}
	must(t, i.MarkScripted("chat", "separate"))
}
