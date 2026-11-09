package feed

import (
	"os"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// A bash-mode line typed between turns («! git push …», Claude 2.1.319) was an entry outside any turn: the page
// draws turns only, so the line, its output and the agent's answer to it vanished. It opens a turn of its own now: the
// line as the person's message, the command with its output as a step, the answer as the turn's answer.
func TestABashModeLineBetweenTurnsOpensATurn(t *testing.T) {
	real, err := os.ReadFile("testdata/claude-2.1.319-bash-mode-between-turns.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	text := conversation(1) +
		`{"type":"system","subtype":"turn_duration","durationMs":1000,"timestamp":"2026-10-12T10:00:01.000Z"}` + "\n" +
		string(real)
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: write(t, text)})
	u := f.Load()
	validate(t, f)
	ts, es, _ := f.Snapshot(3)
	if len(ts) != 2 {
		t.Fatalf("turns: %+v", ts)
	}
	turn := ts[1]
	if turn.Status != wire.TurnDone || turn.ToolCount != 1 || turn.AnswerEntryID == nil {
		t.Fatalf("the bash turn: %+v", turn)
	}
	var user, cmd, answer *wire.Entry
	for i, e := range es {
		if e.TurnID == nil {
			t.Fatalf("an entry outside any turn: %+v", e)
		}
		if *e.TurnID != turn.ID {
			continue
		}
		switch e.T {
		case wire.EntryUser:
			user = &es[i]
		case wire.EntryTool:
			cmd = &es[i]
		case wire.EntrySay:
			answer = &es[i]
		}
	}
	const line = "! cd ~/projects/gl-harnessly && source ~/.config/acme/github.env && " +
		"git push github 9cbbf8044599223208aa9230653d643561e23f19:refs/tags/v0.16.196"
	if user == nil || user.ID != turn.ID || user.Text != line {
		t.Fatalf("the person's line: %+v", user)
	}
	if cmd == nil || cmd.Tool != "command" || cmd.Output == nil || !strings.Contains(cmd.Output.Text, "[new tag]") {
		t.Fatalf("the command: %+v", cmd)
	}
	if answer == nil || answer.ID != *turn.AnswerEntryID || !strings.Contains(answer.MD, "Тег v0.16.196") {
		t.Fatalf("the answer: %+v", answer)
	}
	// The message on its way is found by its words, and is the turn's opener.
	id, ok := f.FindUserText(0, 0, line)
	if !ok || id != turn.ID {
		t.Fatalf("found %q %v, want %q", id, ok, turn.ID)
	}
	opened := false
	for _, s := range u.Signals {
		opened = opened || s.K == SignalTurnOpened && s.TurnID == turn.ID
	}
	if !opened {
		t.Fatalf("signals: %+v", u.Signals)
	}
}

// A bash-mode line typed while the agent works stays a step of the running turn, as before.
func TestABashModeLineInATurnIsItsStep(t *testing.T) {
	text := `{"type":"user","uuid":"00000000-0000-4000-8000-0000000000aa","timestamp":"2026-10-12T10:00:00.000Z","message":{"role":"user","content":"question"}}` + "\n" +
		`{"type":"user","uuid":"00000000-0000-4000-8000-0000000000cc","timestamp":"2026-10-12T10:00:01.000Z","message":{"role":"user","content":"<bash-input> ls</bash-input>"}}` + "\n" +
		`{"type":"user","uuid":"00000000-0000-4000-8000-0000000000dd","timestamp":"2026-10-12T10:00:02.000Z","message":{"role":"user","content":"<bash-stdout>a.txt</bash-stdout><bash-stderr></bash-stderr>"}}` + "\n"
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: write(t, text)})
	f.Load()
	validate(t, f)
	ts, es, _ := f.Snapshot(3)
	if len(ts) != 1 || ts[0].ToolCount != 1 {
		t.Fatalf("turns: %+v", ts)
	}
	for _, e := range es {
		if e.T == wire.EntryTool && (e.Output == nil || e.Output.Text != "a.txt" || *e.TurnID != ts[0].ID) {
			t.Fatalf("the command: %+v", e)
		}
	}
}
