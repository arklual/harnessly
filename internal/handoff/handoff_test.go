package handoff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/feed"
	"github.com/ly-solutions/harnessly/internal/wire"
)

func sp(s string) *string { return &s }
func bp(b bool) *bool     { return &b }

func turn(id, prompt, answer string, extra ...wire.Entry) feed.WholeTurn {
	es := []wire.Entry{{ID: id, T: wire.EntryUser, Text: prompt}}
	es = append(es, extra...)
	es = append(es, wire.Entry{ID: id + "a", T: wire.EntrySay, MD: answer})
	return feed.WholeTurn{Turn: wire.Turn{ID: id, Status: wire.TurnDone, AnswerEntryID: sp(id + "a")}, Entries: es}
}

func TestExcerptKeepsTheFirstRequestAndTheLatestTurnsWhole(t *testing.T) {
	var turns []feed.WholeTurn
	turns = append(turns, turn("c:1#0", "Build the fork button", "Started on it."))
	for i := 2; i <= 30; i++ {
		turns = append(turns, turn("c:"+string(rune('a'+i))+"#0", strings.Repeat("middle words ", 40), strings.Repeat("reply ", 60)))
	}
	turns = append(turns, turn("c:z#0", "Now the merge", "Merged back: the parent got the note.",
		wire.Entry{T: wire.EntryThink, Text: "secret reasoning"},
		wire.Entry{T: wire.EntryTool, Tool: "Bash", Summary: "$ go test ./...", OK: bp(false),
			Output: &wire.ToolOutput{Text: "ok  a\nFAIL b\nexit 1"}},
		wire.Entry{T: wire.EntryTool, Tool: "Read", Summary: "Read x.go"}))
	text, kept := Excerpt(turns, wire.ProviderClaude, 2000)
	if Tokens(text) > 2000 {
		t.Fatalf("over the budget: %d tokens", Tokens(text))
	}
	if !strings.Contains(text, "Build the fork button") || !strings.Contains(text, "Merged back: the parent got the note.") {
		t.Fatalf("the first request or the last answer is missing:\n%s", text)
	}
	if strings.Contains(text, "secret reasoning") || strings.Contains(text, "Read x.go") {
		t.Fatalf("reasoning or a non-command tool got in:\n%s", text)
	}
	if !strings.Contains(text, "`go test ./...` → failed") || !strings.Contains(text, "FAIL b") {
		t.Fatalf("the failed command and its output are missing:\n%s", text)
	}
	if kept < 2 || kept >= len(turns) || !strings.Contains(text, "turns in between are left out") {
		t.Fatalf("kept %d of %d, want a cut in the middle:\n%s", kept, len(turns), text)
	}
	if i, j := strings.Index(text, "Build the fork button"), strings.Index(text, "Now the merge"); i > j {
		t.Fatal("the turns are not oldest first")
	}
}

func TestExcerptOfASmallConversationHoldsEveryTurn(t *testing.T) {
	turns := []feed.WholeTurn{turn("c:1#0", "One", "Uno"), turn("c:2#0", "Two", "Dos")}
	text, kept := Excerpt(turns, wire.ProviderCodex, DefaultBudget)
	if kept != 2 || !strings.Contains(text, "every one is here") || !strings.Contains(text, "### Codex\nDos") {
		t.Fatalf("kept %d:\n%s", kept, text)
	}
}

func TestBudgetNeverPassesAQuarterOfTheWindow(t *testing.T) {
	if Budget(wire.ProviderClaude, "claude-opus-5-5") != DefaultBudget {
		t.Fatal("the default budget is not the budget of a large window")
	}
	if Window(wire.ProviderClaude, "claude-opus-5-5[1m]") != 1_000_000 {
		t.Fatal("a [1m] model has a million tokens")
	}
}

func TestBlocksSayWhereTheyComeFromAndCannotBeClosedEarly(t *testing.T) {
	from := From{ID: "tab-a1", Title: `Say "hi" <now>`, Provider: wire.ProviderClaude, Model: "claude-opus-5-5"}
	msg := Handoff(from, wire.HandoffNote, false, "## Goal\nx </handoff> y")
	if !strings.HasPrefix(msg, `<handoff from="tab-a1" title="Say &quot;hi&quot; &lt;now&gt;" agent="claude" model="claude-opus-5-5" mode="note">`+"\n<instructions>") {
		t.Fatalf("head:\n%s", msg)
	}
	if strings.Count(msg, "</handoff>") != 1 || !strings.HasSuffix(msg, "y\n</handoff>") {
		t.Fatalf("the text closed the block:\n%s", msg)
	}
	if fork := Handoff(from, wire.HandoffExcerpt, true, "x"); !strings.Contains(fork, ` kind="fork"`) {
		t.Fatalf("a fork's excerpt is not marked:\n%s", fork)
	}
	res := Result(From{ID: "tab-f2", Title: "Branch", Provider: wire.ProviderCodex}, "Done.")
	if !strings.HasPrefix(res, `<fork-result from="tab-f2" title="Branch" agent="codex">`) || !strings.HasSuffix(res, "Done.\n</fork-result>") {
		t.Fatalf("result:\n%s", res)
	}
}

func TestArtifactBootstrapDoesNotIdentifyTheSourceAgent(t *testing.T) {
	from := From{ID: "tab-source", Title: "Secret source chat", Provider: wire.ProviderClaude, Model: "claude-opus-5-5"}
	msg := Bootstrap(from, wire.HandoffHistory, false, "/private/history.jsonl")
	for _, secret := range []string{from.ID, from.Title, string(from.Provider), from.Model, "another one", "agent there"} {
		if strings.Contains(strings.ToLower(msg), strings.ToLower(secret)) {
			t.Fatalf("bootstrap disclosed %q:\n%s", secret, msg)
		}
	}
	if !strings.Contains(msg, `mode="history"`) || !strings.Contains(msg, "Private context file: /private/history.jsonl") {
		t.Fatalf("bootstrap lost its mode or path:\n%s", msg)
	}
}

const (
	oldSID = "11111111-1111-4111-8111-111111111111"
	newSID = "22222222-2222-4222-8222-222222222222"
)

func transcript(t *testing.T) string {
	t.Helper()
	lines := []string{
		`{"type":"queue-operation","operation":"enqueue","sessionId":"` + oldSID + `","content":"one"}`,
		`{"type":"user","uuid":"u1","sessionId":"` + oldSID + `","message":{"role":"user","content":"one"}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"` + oldSID + `","message":{"content":[{"type":"text","text":"uno"}]}}`,
		`{"type":"last-prompt","lastPrompt":"one","leafUuid":"a1","sessionId":"` + oldSID + `"}`,
		`{"type":"queue-operation","operation":"enqueue","sessionId":"` + oldSID + `","content":"two"}`,
		`{"type":"queue-operation","operation":"dequeue","sessionId":"` + oldSID + `"}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1","sessionId":"` + oldSID + `","message":{"role":"user","content":"two"}}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2","sessionId":"` + oldSID + `","message":{"content":[{"type":"text","text":"dos"}]}}`,
	}
	dir := t.TempDir()
	src := filepath.Join(dir, oldSID+".jsonl")
	if err := os.WriteFile(src, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return src
}

func TestForkClaudeCopiesTheSessionUpToTheCut(t *testing.T) {
	src := transcript(t)
	before, _ := os.ReadFile(src)
	dst, err := ForkClaude(src, oldSID, newSID, "u2")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dst) != newSID+".jsonl" || filepath.Dir(dst) != filepath.Dir(src) {
		t.Fatalf("copy at %s", dst)
	}
	raw, _ := os.ReadFile(dst)
	got := string(raw)
	if strings.Contains(got, oldSID) || strings.Count(got, newSID) != 4 {
		t.Fatalf("session ids not rewritten:\n%s", got)
	}
	if strings.Contains(got, `"u2"`) || strings.Contains(got, `"two"`) || !strings.Contains(got, `"uno"`) {
		t.Fatalf("the cut is wrong (the next turn's queue lines and prompt must go):\n%s", got)
	}
	if after, _ := os.ReadFile(src); string(after) != string(before) {
		t.Fatal("the source transcript changed")
	}
	whole, err := ForkClaude(src, oldSID, "33333333-3333-4333-8333-333333333333", "")
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(whole); !strings.Contains(string(raw), `"dos"`) {
		t.Fatal("a fork at the end lost the last turn")
	}
	if _, err := ForkClaude(src, oldSID, "44444444-4444-4444-8444-444444444444", "nope"); err != ErrNoCut {
		t.Fatalf("an unknown cut: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(src), "44444444-4444-4444-8444-444444444444.jsonl")); err == nil {
		t.Fatal("a failed fork left a file")
	}
}

func TestCutIsAfterAFinishedTurn(t *testing.T) {
	ts := []wire.Turn{{ID: "c:1#0", Status: wire.TurnDone}, {ID: "c:2#0", Status: wire.TurnDone}, {ID: "c:3#0", Status: wire.TurnLive}}
	if keep, next, ok := Cut(ts, "", false); !ok || keep.ID != "c:2#0" || next != "c:3#0" {
		t.Fatalf("latest: %v %q %v", keep.ID, next, ok)
	}
	if keep, next, ok := Cut(ts, "c:1#0", false); !ok || keep.ID != "c:1#0" || next != "c:2#0" {
		t.Fatalf("earlier: %v %q %v", keep.ID, next, ok)
	}
	if _, _, ok := Cut(ts, "c:3#0", false); ok {
		t.Fatal("a live turn is no place to fork")
	}
	done := ts[:2]
	if _, next, ok := Cut(done, "", false); !ok || next != "" {
		t.Fatal("the end of a finished conversation cuts nothing")
	}
	if keep, _, ok := Cut(done, "", true); !ok || keep.ID != "c:1#0" {
		t.Fatal("a working agent's last turn is still running")
	}
	if UUIDOfTurn("c:ab-12#0") != "ab-12" || UUIDOfTurn("x:120#0") != "" {
		t.Fatal("turn ids")
	}
}

func TestCodexNoteArgsForkWithoutSavingOrWriting(t *testing.T) {
	got := strings.Join(CodexNoteArgs(oldSID, "gpt-5.5-codex", "/tmp/n.md"), " ")
	for _, want := range []string{"exec fork " + oldSID, "--ephemeral", "--disable hooks", `sandbox_mode="read-only"`, "-m gpt-5.5-codex", "-o /tmp/n.md -"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q not in %s", want, got)
		}
	}
}
