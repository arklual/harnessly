//go:build linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/background"
	"github.com/ly-solutions/harnessly/internal/wire"
	"github.com/ly-solutions/harnessly/internal/workflows"
)

func TestTheWritersOfAnOutputFileEndAndOnlyThey(t *testing.T) {
	dir := t.TempDir()
	out, other := filepath.Join(dir, "b1.output"), filepath.Join(dir, "b2.output")
	start := func(file string) *exec.Cmd {
		f, err := os.Create(file)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		c := exec.Command("sleep", "600")
		c.Stdout = f
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })
		return c
	}
	mine, theirs := start(out), start(other)
	reaped := make(chan struct{})
	go func() { _ = mine.Wait(); close(reaped) }()
	if err := endWriters(out, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reaped:
	case <-time.After(3 * time.Second):
		t.Fatal("the process is still there")
	}
	if len(writersOf(other)) != 1 || theirs.ProcessState != nil {
		t.Fatal("another task's process was touched")
	}
	if err := endWriters(out, time.Second); err != errNoWriter {
		t.Fatalf("a task with no process: %v", err)
	}
	if err := endWriters("/etc/passwd", time.Second); err == nil {
		t.Fatal("a file that is no task output")
	}
}

func TestATranscriptsTasksBecomeWireTasks(t *testing.T) {
	dir := t.TempDir()
	tr := filepath.Join(dir, "proj", "s.jsonl")
	if err := os.MkdirAll(filepath.Dir(tr), 0o755); err != nil {
		t.Fatal(err)
	}
	outFile := filepath.Join(dir, "b1.output")
	if err := os.WriteFile(outFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	lines := `{"timestamp":"2026-10-22T10:00:00.000Z","message":{"content":[{"type":"tool_use","id":"u1","name":"Bash","input":{"command":"npm run dev","description":"dev server","run_in_background":true}}]}}
{"timestamp":"2026-10-22T10:00:01.000Z","message":{"content":[{"type":"tool_result","tool_use_id":"u1","content":"Command running in background with ID: b1. Output is being written to: ` + outFile + `"}]}}
{"timestamp":"2026-10-22T10:00:02.000Z","message":{"content":[{"type":"tool_use","id":"u2","name":"Task","input":{"description":"review the diff"}}]}}
{"timestamp":"2026-10-22T10:00:03.000Z","message":{"content":[{"type":"tool_result","tool_use_id":"u2","content":"Async agent launched successfully.\nagentId: a7"}]}}
`
	if err := os.WriteFile(tr, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	b := v2Background{reader: &background.Reader{TempDir: dir, UID: os.Getuid(), Now: func() float64 { return float64(time.Now().UnixMilli()) }}}
	got := b.Tasks(wire.Conversation{ID: "tab-x", Provider: wire.ProviderClaude, TranscriptPath: tr}, "")
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	cmd, sub := got[0], got[1]
	if cmd.Kind != wire.BgCommand || cmd.ID != "b1" || cmd.Title != "dev server" || !cmd.Stoppable || cmd.OutputPath == nil || cmd.StartedAt == 0 ||
		cmd.Status != wire.BgStatusRunning || cmd.ConversationID != "tab-x" {
		t.Errorf("%+v", cmd)
	}
	if sub.Kind != wire.BgSubagent || sub.ID != "a7" || sub.Title != "review the diff" || sub.Stoppable {
		t.Errorf("%+v", sub)
	}
	for _, task := range got {
		if err := task.Validate(); err != nil {
			t.Errorf("%s: %v", task.ID, err)
		}
	}
	for label, want := range map[string]bool{"npm run dev": true, "vite --port 3000": true, "python -m http.server 8000": true, "go test ./...": false, "tail -f log": false} {
		if devServer.MatchString(label) != want {
			t.Errorf("%q: dev server %v", label, !want)
		}
	}
	if len(b.Tasks(wire.Conversation{Provider: wire.ProviderCodex, TranscriptPath: tr}, "")) != 0 {
		t.Error("a Codex conversation has no background tasks")
	}
}

// A chat that is opened with its subagents running and finished lists both: the running ones, then the ones that
// ended lately with how they ended; one that ended long ago is not listed.
func TestTasksListTheSubagentsThatEndedLately(t *testing.T) {
	dir := t.TempDir()
	tr := filepath.Join(dir, "sess.jsonl")
	use := func(id, desc, at string) string {
		return `{"timestamp":"` + at + `","message":{"content":[{"type":"tool_use","id":"` + id + `","name":"Agent","input":{"description":"` + desc + `","run_in_background":true}}]}}` + "\n"
	}
	launched := func(id, agent, at string) string {
		return `{"timestamp":"` + at + `","message":{"content":[{"type":"tool_result","tool_use_id":"` + id + `","content":[{"type":"text","text":"Async agent launched successfully.\nagentId: ` + agent + `"}]}]}}` + "\n"
	}
	notice := func(agent, status, at string) string {
		return `{"type":"queue-operation","timestamp":"` + at + `","content":"<task-notification>\n<task-id>` + agent + `</task-id>\n<status>` + status + `</status>\n</task-notification>"}` + "\n"
	}
	lines := use("u1", "old one", "2026-10-22T07:00:00.000Z") + launched("u1", "a1", "2026-10-22T07:00:01.000Z") +
		use("u2", "audit backend", "2026-10-22T10:00:00.000Z") + launched("u2", "a2", "2026-10-22T10:00:01.000Z") +
		use("u3", "audit shell", "2026-10-22T10:00:02.000Z") + launched("u3", "a3", "2026-10-22T10:00:03.000Z") +
		use("u4", "still going", "2026-10-22T10:00:04.000Z") + launched("u4", "a4", "2026-10-22T10:00:05.000Z") +
		notice("a1", "completed", "2026-10-22T07:30:00.000Z") +
		notice("a2", "completed", "2026-10-22T10:10:00.000Z") +
		notice("a3", "killed", "2026-10-22T10:20:00.000Z")
	if err := os.WriteFile(tr, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 22, 10, 30, 0, 0, time.UTC)
	b := v2Background{reader: &background.Reader{TempDir: dir, UID: os.Getuid(), Now: func() float64 { return float64(now.UnixMilli()) }},
		now: func() time.Time { return now }}
	got := b.Tasks(wire.Conversation{ID: "tab-x", Provider: wire.ProviderClaude, TranscriptPath: tr}, "")
	want := []struct {
		id     string
		status wire.BackgroundStatus
	}{{"a4", wire.BgStatusRunning}, {"a3", wire.BgStatusStopped}, {"a2", wire.BgStatusDone}}
	if len(got) != len(want) {
		t.Fatalf("%+v", got)
	}
	for i, w := range want {
		task := got[i]
		if task.ID != w.id || task.Status != w.status || task.Kind != wire.BgSubagent {
			t.Errorf("#%d: %+v", i, task)
		}
		if (w.status == wire.BgStatusRunning) != (task.EndedAt == nil) {
			t.Errorf("#%d ended at %v with %s", i, task.EndedAt, task.Status)
		}
		if err := task.Validate(); err != nil {
			t.Errorf("%s: %v", task.ID, err)
		}
	}
	if got[2].Title != "audit backend" || *got[2].EndedAt != wire.Ms(time.Date(2026, 10, 22, 10, 10, 0, 0, time.UTC).UnixMilli()) {
		t.Errorf("%+v", got[2])
	}
}

// A Monitor is a background command that says so, and a scheduled wakeup is no task but a time the agent waits for;
// the wakeup is gone once its time has come, and the monitor once its own expiry has.
func TestAMonitorAndAWakeupAreWhatTheAgentWaitsFor(t *testing.T) {
	dir := t.TempDir()
	tr := filepath.Join(dir, "sess.jsonl")
	lines := `{"timestamp":"2026-10-22T10:00:00.000Z","message":{"content":[{"type":"tool_use","id":"u1","name":"Monitor","input":{"description":"watch the build","command":"tail -f build.log"}}]}}
{"timestamp":"2026-10-22T10:00:01.000Z","message":{"content":[{"type":"tool_result","tool_use_id":"u1","content":"Monitor started (task bktwfrc6l, expires in 3m unless the source ends first)"}]}}
{"timestamp":"2026-10-22T10:00:02.000Z","message":{"content":[{"type":"tool_use","id":"u2","name":"ScheduleWakeup","input":{"delaySeconds":600,"reason":"check the deploy","prompt":"go on"}}]}}
{"timestamp":"2026-10-22T10:00:03.000Z","message":{"content":[{"type":"tool_result","tool_use_id":"u2","content":"Wakeup scheduled for 10:10:02 (in 600s)"}]}}
`
	if err := os.WriteFile(tr, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	// The agent's task output folder: a monitor whose file is gone is over.
	tasksDir := filepath.Join(dir, "claude-"+strconv.Itoa(os.Getuid()), filepath.Base(filepath.Dir(tr)), "sess", "tasks")
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tasksDir, "bktwfrc6l.output"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	conv := wire.Conversation{ID: "tab-x", Provider: wire.ProviderClaude, TranscriptPath: tr}
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	reader := func(now time.Time) v2Background {
		return v2Background{reader: &background.Reader{TempDir: dir, UID: os.Getuid(), Now: func() float64 { return float64(now.UnixMilli()) }},
			now: func() time.Time { return now }}
	}

	b := reader(at("2026-10-22T10:01:00Z"))
	tasks := b.Tasks(conv, "")
	if len(tasks) != 1 || tasks[0].Kind != wire.BgCommand || !tasks[0].Monitor || tasks[0].Title != "watch the build" {
		t.Fatalf("%+v", tasks)
	}
	if err := tasks[0].Validate(); err != nil {
		t.Fatal(err)
	}
	// The call's time (10:00:02) and its 600s.
	if got, want := b.Wakeup(conv), wire.Ms(at("2026-10-22T10:10:02Z").UnixMilli()); got != want {
		t.Fatalf("wakeup at %d, want %d", got, want)
	}

	// Past the monitor's three minutes it is no longer running, and the wakeup stays until its time.
	b = reader(at("2026-10-22T10:05:00Z"))
	if tasks := b.Tasks(conv, ""); len(tasks) != 0 {
		t.Fatalf("the monitor expired: %+v", tasks)
	}
	if b.Wakeup(conv) == 0 {
		t.Fatal("the wakeup is still to come")
	}
	b = reader(at("2026-10-22T10:11:00Z"))
	if got := b.Wakeup(conv); got != 0 {
		t.Fatalf("the wakeup came: %d", got)
	}
	if got := (v2Background{}).Wakeup(conv); got != 0 {
		t.Fatalf("no reader: %d", got)
	}
}

// The lead's transcript of internal/background/testdata/teammates.jsonl, read through the runtime: the teammates are
// subagent rows that say they are teammates, titled with their names, at work or idle, the one that shut down done;
// the model comes from their own files when those say it, else from the spawn.
func TestTeammatesBecomeSubagentRows(t *testing.T) {
	raw, err := os.ReadFile("../../internal/background/testdata/teammates.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	tr := filepath.Join(dir, "lead.jsonl")
	if err := os.WriteFile(tr, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	subs := filepath.Join(dir, "lead", "subagents")
	if err := os.MkdirAll(subs, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := `{"agentType":"impl-h1","description":"H1: access API","name":"impl-h1","model":"opus","taskKind":"in_process_teammate"}`
	if err := os.WriteFile(filepath.Join(subs, "agent-aimpl-h1-9dedc0ca6bb25a46.meta.json"), []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 25, 3, 10, 0, 0, time.UTC)
	b := v2Background{reader: &background.Reader{TempDir: dir, UID: os.Getuid(), Now: func() float64 { return float64(now.UnixMilli()) }},
		now: func() time.Time { return now }}
	got := b.Tasks(wire.Conversation{ID: "tab-x", Provider: wire.ProviderClaude, TranscriptPath: tr}, "")
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	h1, g1 := got[0], got[1]
	if h1.ID != "aimpl-h1-9dedc0ca6bb25a46" || h1.Title != "impl-h1" || !h1.Teammate || h1.Kind != wire.BgSubagent ||
		h1.Status != wire.BgStatusRunning || h1.EndedAt != nil || h1.Model == nil || *h1.Model != "opus" ||
		h1.Role == nil || *h1.Role != "H1: access API" || h1.Stoppable {
		t.Errorf("%+v", h1)
	}
	if g1.Title != "impl-g1" || g1.Status != wire.BgStatusDone || g1.EndedAt == nil || g1.Model == nil || *g1.Model != "sonnet" {
		t.Errorf("%+v", g1)
	}
	for _, task := range got {
		if err := task.Validate(); err != nil {
			t.Errorf("%s: %v", task.ID, err)
		}
	}
	// An hour after it shut down the teammate is no longer listed; the one at work is.
	now = now.Add(2 * time.Hour)
	if got := b.Tasks(wire.Conversation{ID: "tab-x", Provider: wire.ProviderClaude, TranscriptPath: tr}, ""); len(got) != 1 || got[0].Title != "impl-h1" {
		t.Fatalf("%+v", got)
	}
}

// A dynamic workflow is one task with its run: the phases of its script and the agents its journal started in each.
// testdata is a scrubbed copy of two runs: one at work and one whose notification and final state say it is over.
func TestAWorkflowListsItsPhasesAndAgents(t *testing.T) {
	tr, err := filepath.Abs(filepath.Join("..", "..", "internal", "workflows", "testdata", "conv.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 25, 10, 5, 0, 0, time.UTC)
	b := v2Background{reader: &background.Reader{TempDir: t.TempDir(), UID: os.Getuid(), Now: func() float64 { return float64(now.UnixMilli()) }},
		flows: &workflows.Reader{}, now: func() time.Time { return now }}
	got := b.Tasks(wire.Conversation{ID: "tab-x", Provider: wire.ProviderClaude, TranscriptPath: tr}, "")
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	run, done := got[0], got[1]
	for _, task := range got {
		if err := task.Validate(); err != nil {
			t.Errorf("%s: %v", task.ID, err)
		}
	}
	if run.ID != "wq7m2kx4d" || run.Kind != wire.BgWorkflow || run.Status != wire.BgStatusRunning || run.EndedAt != nil ||
		run.Title != "Review the checkout flow, then fix what the reviewers found" || run.Stoppable {
		t.Fatalf("running %+v", run)
	}
	w := run.Workflow
	if w.Name != "checkout-review" || w.ToolUseID == nil || *w.ToolUseID != "toolu_01Checkout" || len(w.Phases) != 3 {
		t.Fatalf("run %+v", w)
	}
	var states []wire.BackgroundStatus
	for _, a := range w.Phases[0].Agents {
		states = append(states, a.Status)
	}
	if !slices.Equal(states, []wire.BackgroundStatus{wire.BgStatusDone, wire.BgStatusDone, wire.BgStatusFailed}) ||
		w.Phases[1].Agents[0].Status != wire.BgStatusRunning || len(w.Phases[2].Agents) != 0 {
		t.Fatalf("phases %+v", w.Phases)
	}
	if a := w.Phases[0].Agents[0]; a.Label != "review:payments" || a.Model == nil || *a.Model != "claude-sonnet-5-5" || a.Result == nil {
		t.Fatalf("agent %+v", a)
	}
	if done.ID != "wz5n8rt1c" || done.Kind != wire.BgWorkflow || done.Status != wire.BgStatusDone || done.EndedAt == nil ||
		done.Workflow.Name != "pricing-research" || len(done.Workflow.Phases[0].Agents) != 2 {
		t.Fatalf("over %+v", done)
	}
}
