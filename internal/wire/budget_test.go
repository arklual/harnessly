package wire

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureSnapshot(t *testing.T, name string) SnapshotFrame {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := DecodeServerFrame(raw)
	if err != nil {
		t.Fatal(err)
	}
	return f.(SnapshotFrame)
}

func frameSize(t *testing.T, f SnapshotFrame) int {
	t.Helper()
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeServerFrame(b); err != nil {
		t.Fatalf("the fitted snapshot is out of contract: %v", err)
	}
	return len(b)
}

// hundred is 100 conversations made of the fixture's, each with a title of 40 characters and a snippet of
// SnippetMax; worst also gives every one a long folder, a model and a project.
func hundred(t *testing.T, worst bool) ConvsSnapshot {
	base := fixtureSnapshot(t, "snapshot.convs").Convs.Conversations
	var out ConvsSnapshot
	for i := 0; i < 100; i++ {
		c := base[i%len(base)]
		c.ID = fmt.Sprintf("tab-%08dxyz", i)
		c.Title = strings.Repeat("Разбор очереди ", 4)
		c.Title = string([]rune(c.Title)[:40])
		c.LastTurn = &TurnSummary{ID: ClaudeEntryID("3f1d2a9e-7c41-4b8e-9a51-0d6c2b7e8f10", 0), Status: TurnDone,
			StartedAt: 1792655880000, Snippet: strings.Repeat("ж", SnippetMax), ToolCount: 12}
		if worst {
			c.Cwd = "/home/coder/workspace/harnessly/.worktrees/abc-3142-b-wire/web/app"
			pid := "p7k2m9x4q1aa"
			c.ProjectID = &pid
			effort := EffortHigh
			c.Model = &Model{ID: "claude-opus-5-5", Effort: &effort}
		}
		out.Conversations = append(out.Conversations, c)
	}
	return out
}

func TestConvsSnapshotFitsItsBudget(t *testing.T) {
	fixture := fixtureSnapshot(t, "snapshot.convs")
	t.Logf("fixture convs snapshot, %d conversations: %d B", len(fixture.Convs.Conversations), frameSize(t, fixture))

	all := hundred(t, false)
	raw := frameSize(t, SnapshotFrame{Ch: ChannelConvs, Mode: SnapshotReplace, Convs: &all})
	fitted := FitConvs(all)
	got := frameSize(t, SnapshotFrame{Ch: ChannelConvs, Mode: SnapshotReplace, Convs: &fitted})
	t.Logf("100 conversations, full snippets: %d B raw, %d B fitted (budget %d)", raw, got, ConvsBudget)
	if got > ConvsBudget {
		t.Fatalf("convs snapshot %d B, over %d", got, ConvsBudget)
	}
	if len(fitted.Conversations) != 100 {
		t.Fatal("fitting drops no conversation")
	}
	if all.Conversations[0].LastTurn.Snippet != strings.Repeat("ж", SnippetMax) {
		t.Fatal("fitting changed the caller's records")
	}
	// At their largest (a long folder, a model, a project) the records alone are past the budget — such a record
	// without snippet is ~700 B: fitting takes what it can, the socket's deflate does the rest.
	worst := FitConvs(hundred(t, true))
	n := frameSize(t, SnapshotFrame{Ch: ChannelConvs, Mode: SnapshotReplace, Convs: &worst})
	t.Logf("100 conversations at their largest, fitted: %d B", n)
	if n > ConvsBudget*112/100 {
		t.Fatalf("the largest records fit to %d B", n)
	}
	// Typical records fit untouched.
	var typical ConvsSnapshot
	for i := 0; i < 100; i++ {
		c := fixture.Convs.Conversations[i%len(fixture.Convs.Conversations)]
		c.ID = fmt.Sprintf("tab-%08dxyz", i)
		typical.Conversations = append(typical.Conversations, c)
	}
	n = frameSize(t, SnapshotFrame{Ch: ChannelConvs, Mode: SnapshotReplace, Convs: &typical})
	t.Logf("100 conversations like the fixture's: %d B", n)
	if n > ConvsBudget || size(FitConvs(typical)) != size(typical) {
		t.Fatal("a snapshot within budget is not touched")
	}
}

// Fitting shortens the conversations and leaves the rest of the snapshot as it was.
func TestFittingKeepsTheProjectsAndTheDefaultFolder(t *testing.T) {
	all := hundred(t, true)
	all.Projects = []Project{{ID: "p1", MachineID: "m", Path: "/w", Name: "w", Git: true, CreatedAt: 1}}
	all.DefaultCwd = "/home/coder"
	fitted := FitConvs(all)
	if size(fitted) >= size(all) {
		t.Fatal("the test needs a snapshot that fitting shortens")
	}
	if len(fitted.Projects) != 1 || fitted.DefaultCwd != "/home/coder" {
		t.Fatalf("fitted: %+v %q", fitted.Projects, fitted.DefaultCwd)
	}
}

// huge is a conversation whose last three turns did 120 commands each, every one with 5 KB of output, and 30 turns
// before them with long answers.
func huge() ConvSnapshot {
	s := ConvSnapshot{Entries: []Entry{}, Messages: []Message{}, Questions: []Question{}, Permissions: []Permission{},
		Background: []BackgroundTask{}, Queue: Queue{ConversationID: "tab-huge", Items: []string{}, Rev: 1}}
	out := strings.Repeat("ok  github.com/acme/app 0.4s\n", 180)
	for i := 0; i < 33; i++ {
		tid := OffsetEntryID(int64(i)*1_000_000, 0)
		answer := OffsetEntryID(int64(i)*1_000_000+999_999, 0)
		end := int64(1792655880000 + i)
		turn := Turn{ID: tid, Author: TurnAuthor{Provider: ProviderClaude}, StartedAt: end - 1000, EndedAt: &end, Status: TurnDone,
			AnswerEntryID: &answer, Files: []TurnFile{}}
		s.Entries = append(s.Entries, Entry{ID: tid, TurnID: &tid, Rev: 1, T: EntryUser, Text: "Запусти тесты", Attachments: []AttachmentRef{}})
		if i >= 30 {
			for k := 0; k < 120; k++ {
				ok := true
				s.Entries = append(s.Entries, Entry{ID: OffsetEntryID(int64(i)*1_000_000+int64(k)+1, 0), TurnID: &tid, Rev: 1, T: EntryTool,
					ToolUseID: fmt.Sprint("toolu_", i, k), Tool: "Bash", Summary: "go test", OK: &ok, Output: &ToolOutput{Text: out}})
			}
			turn.ToolCount, turn.StepCount = 120, 122
		}
		s.Entries = append(s.Entries, Entry{ID: answer, TurnID: &tid, Rev: 1, T: EntrySay, MD: strings.Repeat("Ответ. ", 300)})
		s.Turns = append(s.Turns, turn)
	}
	before := OffsetEntryID(0, 0)
	s.BeforeTurnID = &before
	return s
}

func TestConvSnapshotFitsItsBudget(t *testing.T) {
	fixture := fixtureSnapshot(t, "snapshot.conv")
	t.Logf("fixture conv snapshot: %d B", frameSize(t, fixture))

	s := huge()
	raw := frameSize(t, SnapshotFrame{Ch: ConvChannel("tab-huge"), Mode: SnapshotReplace, Conv: &s})
	fitted := FitConv(s)
	got := frameSize(t, SnapshotFrame{Ch: ConvChannel("tab-huge"), Mode: SnapshotReplace, Conv: &fitted})
	t.Logf("3 turns × 120 commands × 5 KB and 30 more turns: %d B raw, %d B fitted (budget %d), %d of %d entries, %d turns",
		raw, got, ConvBudget, len(fitted.Entries), len(s.Entries), len(fitted.Turns))
	if got > ConvBudget {
		t.Fatalf("conv snapshot %d B, over %d", got, ConvBudget)
	}
	last := fitted.Turns[len(fitted.Turns)-1]
	has := map[string]bool{}
	for _, e := range fitted.Entries {
		has[e.ID] = true
		if e.Output != nil && len(e.Output.Text) > ToolOutputMax {
			t.Fatal("an output over ToolOutputMax")
		}
	}
	if !has[last.ID] || !has[*last.AnswerEntryID] {
		t.Fatal("the last turn keeps its user entry and its answer")
	}
	for _, turn := range fitted.Turns {
		if !has[*turn.AnswerEntryID] {
			t.Fatalf("turn %s lost its answer: a folded turn cannot render", turn.ID)
		}
	}
	if fitted.BeforeTurnID == nil || *fitted.BeforeTurnID != fitted.Turns[0].ID && len(fitted.Turns) < len(s.Turns) {
		t.Fatal("beforeTurnId points at the oldest kept turn once older ones go")
	}
	// One command's output: cut to ToolOutputMax, and says of how much.
	one := FitConv(ConvSnapshot{Entries: s.Entries[61:62], Turns: s.Turns[30:31], Queue: s.Queue, Messages: []Message{}, Questions: []Question{},
		Permissions: []Permission{}, Background: []BackgroundTask{}})
	if o := one.Entries[0].Output; o.Truncated == nil || o.Truncated.Total != int64(len(s.Entries[61].Output.Text)) || len(o.Text) != ToolOutputMax {
		t.Fatalf("output %+v", o.Truncated)
	}
}

// Steps without output: the older turns' steps go first, then the oldest turns, then the oldest steps of the last.
func TestConvSnapshotDropsStepsLast(t *testing.T) {
	s := huge()
	var entries []Entry
	for _, e := range s.Entries {
		if e.T == EntryTool {
			e = Entry{ID: e.ID, TurnID: e.TurnID, Rev: 1, T: EntrySay, MD: strings.Repeat("шаг ", 400)}
			for k := 0; k < 4; k++ {
				c := e
				c.ID = strings.Replace(e.ID, "#0", fmt.Sprint("#", k), 1)
				entries = append(entries, c)
			}
			continue
		}
		entries = append(entries, e)
	}
	s.Entries = entries
	fitted := FitConv(s)
	got := frameSize(t, SnapshotFrame{Ch: ConvChannel("tab-huge"), Mode: SnapshotReplace, Conv: &fitted})
	t.Logf("3 turns × 480 steps of 1.6 KB: %d B raw, %d B fitted, %d of %d entries, %d turns",
		size(s), got, len(fitted.Entries), len(s.Entries), len(fitted.Turns))
	if got > ConvBudget {
		t.Fatalf("conv snapshot %d B, over %d", got, ConvBudget)
	}
	last := fitted.Turns[len(fitted.Turns)-1]
	kept := 0
	for _, e := range fitted.Entries {
		if e.TurnID != nil && *e.TurnID == last.ID {
			kept++
		}
	}
	if kept < 3 || kept >= last.StepCount {
		t.Fatalf("the last turn keeps %d of %d steps", kept, last.StepCount)
	}
	if size(fitted) < ConvBudget*9/10 {
		t.Fatalf("fitting dropped far more than needed: %d B", size(fitted))
	}
}

// An older turn opened by a message with pictures the transcript holds inline keeps that message, without the
// pictures' bytes: a folded turn shows what opened it, and its page brings the pictures back.
func TestConvSnapshotKeepsAnOlderPromptWithoutInlinePictures(t *testing.T) {
	s := huge()
	picture := AttachmentRef{ID: "data:image/png;base64," + strings.Repeat("A", ConvBudget), Name: "image-1.png", Kind: AttachmentImage, Mime: "image/png"}
	first := s.Turns[30].ID
	for i, e := range s.Entries {
		if e.ID == first {
			s.Entries[i].Text, s.Entries[i].Attachments = "Опиши картинку", []AttachmentRef{picture}
		}
	}
	fitted := FitConv(s)
	if got := frameSize(t, SnapshotFrame{Ch: ConvChannel("tab-huge"), Mode: SnapshotReplace, Conv: &fitted}); got > ConvBudget {
		t.Fatalf("conv snapshot %d B, over %d", got, ConvBudget)
	}
	for _, e := range fitted.Entries {
		if e.ID == first {
			if e.Text != "Опиши картинку" || len(e.Attachments) != 0 {
				t.Fatalf("prompt %q with %d attachments", e.Text, len(e.Attachments))
			}
			return
		}
	}
	t.Fatal("the older turn lost the message that opened it")
}
