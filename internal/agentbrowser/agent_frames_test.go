package agentbrowser

import (
	"errors"
	"testing"
)

func TestAgentFrameGuards(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	p := newPage(Tab{ID: "tab-a", Conversation: "chat-a", URL: "about:blank", Control: "agent"})
	i.pages[p.ID] = p
	if err := i.AgentCheck("chat-b", p.ID, ""); !errors.Is(err, ErrNotOwner) {
		t.Fatal(err)
	}
	if _, err := i.AgentFrameCall(t.Context(), "chat-b", p.ID, "foreign", "Runtime.evaluate", nil); !errors.Is(err, ErrNotOwner) {
		t.Fatal(err)
	}
	p.Control = "person"
	if _, err := i.AgentFrameCall(t.Context(), "chat-a", p.ID, "foreign", "Runtime.evaluate", nil); !errors.Is(err, ErrPersonControl) {
		t.Fatal(err)
	}
	if err := i.AgentSetViewport(t.Context(), "chat-a", p.ID, Viewport{Mode: "fixed", W: 800, H: 600, DPR: 1}); !errors.Is(err, ErrPersonControl) {
		t.Fatal(err)
	}
	p.Control = "agent"
	p.Dialog = &Dialog{Type: "confirm", Message: "Continue?"}
	if err := i.AgentCheck("chat-a", p.ID, "Runtime.evaluate"); !errors.Is(err, ErrDialogOpen) {
		t.Fatal(err)
	}
	if err := i.AgentCheck("chat-a", p.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := i.AgentFrameCall(t.Context(), "chat-a", p.ID, "foreign", "Runtime.evaluate", nil); !errors.Is(err, ErrDialogOpen) {
		t.Fatal(err)
	}
}
