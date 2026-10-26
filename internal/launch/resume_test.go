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
