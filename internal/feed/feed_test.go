package feed

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

func fixture(t *testing.T, provider, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "transcript", "testdata", provider, name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func source(provider wire.Provider, path string) Source {
	if provider == wire.ProviderCodex {
		return &CodexSource{Path: path}
	}
	return &ClaudeSource{Path: path}
}

// shape is what must be the same however the transcript was read: each entry's id, kind and turn, each turn's id and
// counters.
func shape(f *Feed) string {
	var b strings.Builder
	for _, e := range f.b.entries {
		turn := "-"
		if e.TurnID != nil {
			turn = *e.TurnID
		}
		fmt.Fprintf(&b, "%s %s %s\n", e.ID, e.T, turn)
	}
	for _, t := range f.b.turns {
		fmt.Fprintf(&b, "turn %s steps=%d tools=%d\n", t.rec.ID, t.rec.StepCount, t.rec.ToolCount)
	}
	return b.String()
}

func validate(t *testing.T, f *Feed) {
	t.Helper()
	for _, e := range f.b.entries {
		if err := e.Validate(); err != nil {
			t.Fatalf("entry %s: %v", e.ID, err)
		}
	}
	for _, tr := range f.b.turns {
		if err := tr.rec.Validate(); err != nil {
			t.Fatalf("turn %s: %v", tr.rec.ID, err)
		}
	}
}

// The feed is the same read whole and read as the file grows, a line at a time and in chunks that cut lines: the ids
// are the transcript's own bytes.
func TestIncrementalReadMatchesWhole(t *testing.T) {
	cases := []struct {
		provider wire.Provider
		dir      string
		name     string
	}{
		{wire.ProviderClaude, "claude", "recorded-2.1.282-context-local-command.jsonl"},
		{wire.ProviderClaude, "claude", "recorded-2.1.284-replies-after-idle-prompts.jsonl"},
		{wire.ProviderClaude, "claude", "recorded-2.1.284-teammate-messages.jsonl"},
		{wire.ProviderClaude, "claude", "recorded-2.1.296-send-now-bash.jsonl"},
		{wire.ProviderClaude, "claude", "recorded-2.1.284-pasted-images.jsonl"},
		{wire.ProviderClaude, "claude", "synthetic-tools.jsonl"},
		{wire.ProviderClaude, "claude", "synthetic-user.jsonl"},
		{wire.ProviderCodex, "codex", "corpus/basic.jsonl"},
		{wire.ProviderCodex, "codex", "corpus/big.jsonl"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw := fixture(t, c.dir, c.name)
			dir := t.TempDir()
			whole := filepath.Join(dir, "whole.jsonl")
			if err := os.WriteFile(whole, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			wf := New("c1", c.provider, source(c.provider, whole))
			wf.Load()
			validate(t, wf)
			want := shape(wf)
			if len(wf.b.entries) == 0 {
				t.Fatal("no entries")
			}
			sizes := []int{0, 97, 1000}
			if len(raw) > 200_000 {
				sizes = []int{0, 64 << 10}
			}
			for _, size := range sizes {
				path := filepath.Join(dir, fmt.Sprintf("grow-%d.jsonl", size))
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				f := New("c1", c.provider, source(c.provider, path))
				f.Load()
				for _, piece := range pieces(raw, size) {
					fh, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
					if err != nil {
						t.Fatal(err)
					}
					fh.Write(piece)
					fh.Close()
					f.Poll()
				}
				validate(t, f)
				if got := shape(f); got != want {
					t.Fatalf("pieces of %d: the feed differs from a whole read\n%s", size, diff(want, got))
				}
			}
		})
	}
}

// pieces cuts the file: size 0 is a line at a time, else size bytes, cutting lines.
func pieces(raw []byte, size int) [][]byte {
	var out [][]byte
	if size == 0 {
		for _, line := range strings.SplitAfter(string(raw), "\n") {
			if line != "" {
				out = append(out, []byte(line))
			}
		}
		return out
	}
	for len(raw) > 0 {
		n := min(size, len(raw))
		out = append(out, raw[:n])
		raw = raw[n:]
	}
	return out
}

func diff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < max(len(w), len(g)); i++ {
		var a, b string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			b = g[i]
		}
		if a != b {
			return fmt.Sprintf("line %d:\n want %q\n got  %q", i+1, a, b)
		}
	}
	return ""
}

// conversation writes a Claude transcript of n turns: a question, a tool call with its result, an answer.
func conversation(n int) string {
	var b strings.Builder
	for i := range n {
		ts := fmt.Sprintf("2026-10-12T10:%02d:%02d.000Z", i/60, i%60)
		fmt.Fprintf(&b, `{"type":"user","timestamp":%q,"message":{"role":"user","content":"question %d"}}`+"\n", ts, i)
		fmt.Fprintf(&b, `{"type":"assistant","timestamp":%q,"message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_%04d","name":"Bash","input":{"command":"ls %d"}}]}}`+"\n", ts, i, i)
		fmt.Fprintf(&b, `{"type":"user","timestamp":%q,"message":{"role":"user","content":[{"tool_use_id":"toolu_%04d","type":"tool_result","content":"file %d","is_error":false}]}}`+"\n", ts, i, i)
		fmt.Fprintf(&b, `{"type":"assistant","timestamp":%q,"message":{"role":"assistant","content":[{"type":"text","text":"answer %d"}]}}`+"\n", ts, i)
	}
	return b.String()
}

func write(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Pages of turns go back by the cursor through what was never read, turn by turn, oldest first in each page, and
// the ids of the turns read later are the ones a whole read gives.
func TestTurnPagesGoBackPastTheTail(t *testing.T) {
	path := write(t, conversation(120))
	whole := New("c1", wire.ProviderClaude, &ClaudeSource{Path: path})
	whole.tail = 100_000
	whole.Load()
	if len(whole.b.turns) != 120 {
		t.Fatalf("whole read: %d turns", len(whole.b.turns))
	}

	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: path})
	f.tail = 50
	f.Load()
	if len(f.b.turns) >= 120 {
		t.Fatalf("the tail read everything: %d turns", len(f.b.turns))
	}

	ts, es, before := f.Snapshot(30)
	if len(ts) == 0 || before == nil {
		t.Fatalf("snapshot: %d turns, before %v", len(ts), before)
	}
	full := map[string]bool{}
	for _, tr := range ts[max(0, len(ts)-3):] {
		full[tr.ID] = true
	}
	for _, e := range es {
		if e.TurnID != nil && !full[*e.TurnID] && (ts[0].ID > *e.TurnID) {
			t.Fatalf("snapshot entry %s of a turn outside it", e.ID)
		}
		if e.TurnID != nil && !full[*e.TurnID] && e.T != wire.EntrySay && e.ID != *e.TurnID {
			t.Fatalf("snapshot keeps %s of an older turn %s; only its opening and its answer are kept", e.T, *e.TurnID)
		}
	}

	var got []string
	cursor := ""
	for range 20 {
		page, ok := f.Turns(cursor, 30)
		if !ok {
			t.Fatalf("cursor %q unknown", cursor)
		}
		if len(page.Turns) > 30 {
			t.Fatalf("page of %d turns", len(page.Turns))
		}
		ids := make([]string, 0, len(page.Turns))
		for _, tr := range page.Turns {
			ids = append(ids, tr.ID)
			if tr.AnswerEntryID == nil {
				t.Fatalf("turn %s without its answer", tr.ID)
			}
		}
		opened := map[string]bool{}
		for _, e := range page.Entries {
			if e.TurnID != nil && e.ID == *e.TurnID {
				opened[e.ID] = true
				continue
			}
			if e.T != wire.EntrySay {
				t.Fatalf("page carries %s; a page of turns has their openings and answers only", e.T)
			}
		}
		for _, tr := range page.Turns {
			if !opened[tr.ID] {
				t.Fatalf("turn %s without the message that opened it", tr.ID)
			}
		}
		got = append(ids, got...)
		if page.BeforeTurnID == nil {
			break
		}
		cursor = *page.BeforeTurnID
	}
	if len(got) != 120 {
		t.Fatalf("paged %d turns, want 120", len(got))
	}
	for i, tr := range whole.b.turns {
		if got[i] != tr.rec.ID {
			t.Fatalf("turn %d: paged %s, whole read %s", i, got[i], tr.rec.ID)
		}
	}
	if shape(f) != shape(whole) {
		t.Fatalf("after paging back the feed differs from a whole read\n%s", diff(shape(whole), shape(f)))
	}
}

func TestAfterAnEntry(t *testing.T) {
	path := write(t, conversation(3))
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: path})
	f.Load()
	first := f.b.entries[0].ID
	ts, es, ok := f.After(first)
	if !ok || len(es) != len(f.b.entries)-1 || len(ts) != 3 {
		t.Fatalf("after %s: ok=%v %d entries %d turns", first, ok, len(es), len(ts))
	}
	if _, _, ok := f.After("x:999999#0"); ok {
		t.Fatal("an entry the feed does not have is a cursor it does not know")
	}
}

// A poll reports the entries it added with their turn's counters, the turn it opened and the tool entry its result
// updated.
func TestPollReportsWhatChanged(t *testing.T) {
	text := conversation(2)
	lines := strings.SplitAfter(text, "\n")
	path := write(t, strings.Join(lines[:4], ""))
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: path})
	f.Load()

	fh, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	fh.WriteString(lines[4] + lines[5])
	fh.Close()
	u := f.Poll()
	if len(u.Steps) != 2 || len(u.Started) != 1 || len(u.Ended) != 1 {
		t.Fatalf("poll: %d steps %d started %d ended", len(u.Steps), len(u.Started), len(u.Ended))
	}
	if u.Steps[1].Entry.T != wire.EntryTool || u.Steps[1].Counts == nil || u.Steps[1].Counts.ToolCount != 1 {
		t.Fatalf("tool step: %+v", u.Steps[1])
	}
	var opened bool
	for _, s := range u.Signals {
		if s.K == SignalTurnOpened && s.Offset >= 0 {
			opened = true
		}
	}
	if !opened {
		t.Fatalf("no turn-opened with its offset: %+v", u.Signals)
	}

	fh, _ = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	fh.WriteString(lines[6])
	fh.Close()
	u = f.Poll()
	if len(u.Steps) != 0 || len(u.Updated) != 1 || u.Updated[0].Rev != 2 || u.Updated[0].OK == nil {
		t.Fatalf("result: %d steps, updated %+v", len(u.Steps), u.Updated)
	}
}

// What the runtime sets on the feed (a message's client id, a turn it ended) stays when a page of the past builds the
// feed again.
func TestRuntimeMarksSurviveARebuild(t *testing.T) {
	path := write(t, conversation(10))
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: path})
	f.tail = 8
	f.Load()
	last := f.LastTurn()
	if last == nil || last.Status != wire.TurnLive {
		t.Fatalf("last turn: %+v", last)
	}
	user, ok := f.FindUser(0, 0)
	if !ok {
		t.Fatal("no user entry")
	}
	if _, ok := f.SetClientID(user, "m-1"); !ok {
		t.Fatal("client id not set")
	}
	failure := &wire.TurnFailure{Code: wire.TurnFailedApiError}
	if tr, ok := f.EndTurn(last.ID, wire.TurnFailed, failure, last.StartedAt+5); !ok || tr.Status != wire.TurnFailed {
		t.Fatalf("end: %+v", tr)
	}
	if _, ok := f.FindUser(0, 0); ok && f.b.entries[f.b.byID[user]].ClientID == nil {
		t.Fatal("found the same entry again")
	}

	if _, ok := f.Turns("", 30); !ok {
		t.Fatal("pages")
	}
	if len(f.b.turns) != 10 {
		t.Fatalf("after paging: %d turns", len(f.b.turns))
	}
	e, _ := f.Entry(user)
	if e.ClientID == nil || *e.ClientID != "m-1" {
		t.Fatalf("client id lost: %+v", e)
	}
	tr, _ := f.Turn(last.ID)
	if tr.Status != wire.TurnFailed || tr.Failure == nil || tr.EndedAt == nil {
		t.Fatalf("turn end lost: %+v", tr)
	}
	if f.OpenTurn() != "" {
		t.Fatalf("turn %s still open", f.OpenTurn())
	}
}

// A turn the runtime ended ahead of its transcript (a stop hook) takes the lines that come after, up to the
// transcript's own end; the page is told the turn's record again, and the next user message opens a new turn.
func TestLinesAfterARuntimeEndStayInTheTurn(t *testing.T) {
	lines := strings.SplitAfter(conversation(2), "\n")
	path := write(t, lines[0])
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: path})
	f.Load()
	open := f.OpenTurn()
	if _, ok := f.EndTurn(open, wire.TurnDone, nil, 1); !ok {
		t.Fatal("not ended")
	}
	append := func(text string) Update {
		fh, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		fh.WriteString(text)
		fh.Close()
		return f.Poll()
	}
	u := append(lines[1] + lines[2] + lines[3])
	if len(u.Steps) != 2 {
		t.Fatalf("steps: %d", len(u.Steps))
	}
	for _, s := range u.Steps {
		if s.Entry.TurnID == nil || *s.Entry.TurnID != open || s.Counts == nil {
			t.Fatalf("a late step outside its turn: %+v", s)
		}
	}
	tr, _ := f.Turn(open)
	if tr.Status != wire.TurnDone || tr.AnswerEntryID == nil || tr.ToolCount != 1 || tr.StepCount != 2 {
		t.Fatalf("turn: %+v", tr)
	}
	var told wire.Turn
	for _, e := range u.Events {
		if ev, ok := e.(wire.TurnFinishedEv); ok {
			told = ev.Turn
		}
	}
	if told.AnswerEntryID == nil || told.ToolCount != 1 {
		t.Fatalf("the page was not told the turn again: %+v", told)
	}
	u = append(lines[4])
	if len(u.Started) != 1 || f.OpenTurn() == open {
		t.Fatalf("the next user message did not open a turn: %+v", u.Started)
	}
	if len(u.Steps) != 1 || *u.Steps[0].Entry.TurnID == open {
		t.Fatalf("the next question went into the old turn: %+v", u.Steps)
	}
}

// A background task's notification that wakes the agent after its turn ended opens a turn of its own: the agent's
// answer to it belongs to a turn, as Legacy shows it, and is not left outside any.
func TestATaskNotificationBetweenTurnsOpensATurn(t *testing.T) {
	text := conversation(1) +
		`{"type":"system","subtype":"turn_duration","durationMs":1000,"timestamp":"2026-10-12T10:00:01.000Z"}` + "\n" +
		`{"type":"user","timestamp":"2026-10-12T10:01:00.000Z","message":{"role":"user","content":"<task-notification>\n<task-id>b1</task-id>\n<status>completed</status>\n<summary>CI finished</summary>\n</task-notification>"}}` + "\n" +
		`{"type":"assistant","timestamp":"2026-10-12T10:01:01.000Z","message":{"role":"assistant","content":[{"type":"text","text":"CI is green"}]}}` + "\n"
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: write(t, text)})
	f.Load()
	validate(t, f)
	if len(f.b.turns) != 2 {
		t.Fatalf("turns:\n%s", shape(f))
	}
	woke := f.b.turns[1].rec
	for _, e := range f.b.entries {
		if e.TurnID == nil {
			t.Fatalf("an entry outside any turn:\n%s", shape(f))
		}
	}
	if woke.AnswerEntryID == nil || f.b.entries[f.b.byID[*woke.AnswerEntryID]].MD != "CI is green" {
		t.Fatalf("the woken turn's answer: %+v\n%s", woke, shape(f))
	}
}

// A background agent shows as one subagent entry that its task notification settles with what the agent returned; the
// notification that wakes the agent after its turn is a copy of the entry that opens the woken turn, not a notice.
func TestASubagentTakesItsResultFromTheTaskNotification(t *testing.T) {
	text := `{"type":"user","timestamp":"2026-10-12T10:00:00.000Z","message":{"role":"user","content":"q"}}` + "\n" +
		`{"type":"assistant","uuid":"00000000-0000-4000-8000-000000000001","timestamp":"2026-10-12T10:00:01.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t-ag","name":"Agent","input":{"description":"Speed up CI","name":"ci","model":"opus","run_in_background":true,"prompt":"go"}}]}}` + "\n" +
		`{"type":"user","uuid":"00000000-0000-4000-8000-000000000002","timestamp":"2026-10-12T10:00:02.000Z","toolUseResult":{"isAsync":true,"status":"async_launched","agentId":"a77"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t-ag","content":"Async agent launched successfully.\nagentId: a77"}]}}` + "\n" +
		`{"type":"system","subtype":"turn_duration","durationMs":1000,"timestamp":"2026-10-12T10:00:03.000Z"}` + "\n" +
		`{"type":"user","timestamp":"2026-10-12T10:41:00.000Z","message":{"role":"user","content":"<task-notification>\n<task-id>a77</task-id>\n<tool-use-id>t-ag</tool-use-id>\n<status>completed</status>\n<summary>Agent \"Speed up CI\" finished</summary>\n<result>Cut 12 minutes.\nSee the MR.</result>\n<duration_ms>2464000</duration_ms>\n</task-notification>"}}` + "\n"
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: write(t, text)})
	f.Load()
	validate(t, f)
	var subs []wire.Entry
	for _, e := range f.b.entries {
		if e.T == wire.EntrySubagent {
			subs = append(subs, e)
		}
		if e.T == wire.EntryNotice {
			t.Fatalf("the notification shows as a notice:\n%s", shape(f))
		}
	}
	if len(subs) != 2 {
		t.Fatalf("subagent entries: %d\n%s", len(subs), shape(f))
	}
	call, card := subs[0], subs[1]
	if call.AgentID != "a77" || call.AgentType != "ci" || call.Model == nil || *call.Model != "opus" ||
		call.Status != wire.SubagentDone || call.Output == nil || call.Output.Text != "Cut 12 minutes.\nSee the MR." ||
		call.DurationMs == nil || *call.DurationMs != 2464000 {
		t.Fatalf("the launch entry: %+v", call)
	}
	if card.AgentID != "a77" || card.Status != wire.SubagentDone || card.Output == nil || card.ID != *card.TurnID {
		t.Fatalf("the woken turn's opener: %+v", card)
	}
}

// Thinking the agent kept to itself (no text) is not a step of the feed.
func TestThinkingWithoutTextIsNotShown(t *testing.T) {
	text := `{"type":"user","timestamp":"2026-10-12T10:00:00.000Z","message":{"role":"user","content":"q"}}` + "\n" +
		`{"type":"assistant","timestamp":"2026-10-12T10:00:01.000Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"","signature":"abc"}]}}` + "\n" +
		`{"type":"assistant","timestamp":"2026-10-12T10:00:02.000Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"weighing it"}]}}` + "\n" +
		`{"type":"assistant","timestamp":"2026-10-12T10:00:03.000Z","message":{"role":"assistant","content":[{"type":"text","text":"a"}]}}` + "\n"
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: write(t, text)})
	f.Load()
	thoughts := 0
	for _, e := range f.b.entries {
		if e.T == wire.EntryThink {
			thoughts++
			if e.Text != "weighing it" {
				t.Fatalf("thought: %q", e.Text)
			}
		}
	}
	if thoughts != 1 {
		t.Fatalf("thoughts: %d\n%s", thoughts, shape(f))
	}
}

// A turn whose agent exited is noticed late, maybe days on: it worked until its last line, not until it was noticed.
func TestAFailureNoticedLateEndsTheTurnAtItsLastLine(t *testing.T) {
	text := `{"type":"user","timestamp":"2026-10-12T10:00:00.000Z","message":{"role":"user","content":"q"}}` + "\n" +
		`{"type":"assistant","timestamp":"2026-10-12T10:00:30.000Z","message":{"role":"assistant","content":[{"type":"text","text":"a"}]}}` + "\n"
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: write(t, text)})
	f.Load()
	last := f.LastTurn()
	if last == nil || last.Status != wire.TurnLive {
		t.Fatalf("last turn: %+v", last)
	}
	failure := &wire.TurnFailure{Code: wire.TurnFailedAgentExited}
	tr, ok := f.EndTurn(last.ID, wire.TurnFailed, failure, last.StartedAt+3*24*3600*1000)
	if !ok || tr.DurationMs == nil || *tr.DurationMs != 30_000 {
		t.Fatalf("end: %+v", tr)
	}
}

// A message to another agent (SendMessage) is a tool step whose summary names the addressee and carries the text, so
// the feed draws it as an outgoing message.
func TestSendMessageStepCarriesTheAddresseeAndTheText(t *testing.T) {
	text := `{"type":"user","timestamp":"2026-10-12T10:00:00.000Z","message":{"role":"user","content":"q"}}` + "\n" +
		`{"type":"assistant","timestamp":"2026-10-12T10:00:01.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"SendMessage","input":{"to":"acme-ui-kit","summary":"s","message":"проверь\nдифф"}}]}}` + "\n"
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: write(t, text)})
	f.Load()
	for _, e := range f.b.entries {
		if e.T == wire.EntryTool && e.Tool == "SendMessage" {
			if e.Summary != "acme-ui-kit\nпроверь\nдифф" {
				t.Fatalf("summary: %q", e.Summary)
			}
			return
		}
	}
	t.Fatalf("no SendMessage step\n%s", shape(f))
}
