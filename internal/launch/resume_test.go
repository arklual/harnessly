package launch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/sleep"
	"github.com/ly-solutions/harnessly/internal/state"
)

// A turn that was running when the machine stopped is settled to idle by the status keeper as soon as it starts (the
// pane has no agent after a restart). The resume reads the state the last run left, not the settled one.
func TestResumeTargetsReadTheStateTheLastRunLeft(t *testing.T) {
	data := t.TempDir()
	write := func(name, body string) {
		p := filepath.Join(data, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	layout := func(cliState string) string {
		return `{"root":{"type":"pane","id":"pane-a","tabs":[{"id":"tab-w","sessionName":"s-w","name":"Claude Code","order":0,` +
			`"cwd":"/x","panelType":"claude-code","agentState":{"providerId":"claude","sessionId":"sid-w","jsonlPath":null,"summary":null},` +
			`"claudeSessionId":"sid-w","cliState":"` + cliState + `","lastEventName":null}],"activeTabId":"tab-w"},"activePaneId":"pane-a"}`
	}
	write("workspaces.json", `{"workspaces":[{"id":"ws-a","name":"A","directories":["/x"]}],"groups":[]}`)
	write("workspaces/ws-a/layout.json", layout("busy"))
	s := New(Config{Store: state.New(state.Config{DataDir: data})})
	rc := ResumeConfig{Installed: func(string) (bool, bool) { return true, true }, Sleep: sleep.Config{}}

	s.RememberStartStates()
	write("workspaces/ws-a/layout.json", layout("idle"))
	if got := s.ResumeTargets(rc); len(got) != 1 || got[0].TabID != "tab-w" || got[0].SessionID != "sid-w" {
		t.Fatalf("targets %+v, want the working tab", got)
	}
	// The remembered states serve one resume: a later one (after a tmux reset) reads the store.
	if got := s.ResumeTargets(rc); len(got) != 0 {
		t.Fatalf("second resume: targets %+v, want none", got)
	}
}

// The last activity of a tab is read off its transcript only when it can decide: a tab that was working resumes
// whatever it says, and with no window set an idle one stays asleep whatever it says.
func TestResumeTargetsReadNoActivityTheyDoNotNeed(t *testing.T) {
	data := t.TempDir()
	write := func(name, body string) {
		p := filepath.Join(data, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tab := func(id, cliState string) string {
		return `{"id":"` + id + `","sessionName":"s-` + id + `","name":"Claude Code","order":0,"cwd":"/x","panelType":"claude-code",` +
			`"agentState":{"providerId":"claude","sessionId":"sid-` + id + `","jsonlPath":null,"summary":null},` +
			`"claudeSessionId":"sid-` + id + `","cliState":"` + cliState + `","lastEventName":null}`
	}
	write("workspaces.json", `{"workspaces":[{"id":"ws-a","name":"A","directories":["/x"]}],"groups":[]}`)
	write("workspaces/ws-a/layout.json", `{"root":{"type":"pane","id":"pane-a","tabs":[`+tab("busy", "busy")+`,`+tab("idle", "idle")+
		`],"activeTabId":"busy"},"activePaneId":"pane-a"}`)
	var asked []string
	rc := ResumeConfig{Installed: func(string) (bool, bool) { return true, true },
		LastActivity: func(_, sid string) *float64 { asked = append(asked, sid); return nil }}

	s := New(Config{Store: state.New(state.Config{DataDir: data})})
	if got := s.ResumeTargets(rc); len(got) != 1 || got[0].TabID != "busy" {
		t.Fatalf("targets %+v, want the working tab", got)
	}
	if len(asked) != 0 {
		t.Fatalf("with no window, the activity of %v was read", asked)
	}

	rc.Sleep = sleep.Config{ResumeRecentMs: 60_000}
	s.ResumeTargets(rc)
	if len(asked) != 1 || asked[0] != "sid-idle" {
		t.Fatalf("with a window, the activity read was %v, want the idle tab's only", asked)
	}
}

// A tab whose session is a chat's is no target, whatever its state: the chat resumes its own agent.
func TestResumeTargetsLeaveChatsToTheirOwner(t *testing.T) {
	data := t.TempDir()
	write := func(name, body string) {
		p := filepath.Join(data, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tab := func(id string) string {
		return `{"id":"` + id + `","sessionName":"s-` + id + `","name":"Claude Code","order":0,"cwd":"/x","panelType":"claude-code",` +
			`"agentState":{"providerId":"claude","sessionId":"sid-` + id + `","jsonlPath":null,"summary":null},` +
			`"claudeSessionId":"sid-` + id + `","cliState":"busy","lastEventName":null}`
	}
	write("workspaces.json", `{"workspaces":[{"id":"ws-a","name":"A","directories":["/x"]}],"groups":[]}`)
	write("workspaces/ws-a/layout.json", `{"root":{"type":"pane","id":"pane-a","tabs":[`+tab("chat")+","+tab("tab")+
		`],"activeTabId":"chat"},"activePaneId":"pane-a"}`)
	s := New(Config{Store: state.New(state.Config{DataDir: data})})
	rc := ResumeConfig{Installed: func(string) (bool, bool) { return true, true }, Sleep: sleep.Config{},
		Owned: func(session string) bool { return session == "s-chat" }}
	if got := s.ResumeTargets(rc); len(got) != 1 || got[0].TabID != "tab" {
		t.Fatalf("targets %+v, want the tab that is no chat", got)
	}
	rc.Owned = nil
	if got := s.ResumeTargets(rc); len(got) != 2 {
		t.Fatalf("without an owner: targets %+v, want both", got)
	}
}
