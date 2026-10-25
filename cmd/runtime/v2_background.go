package main

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/background"
	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/sessionmeta"
	"github.com/ly-solutions/harnessly/internal/wire"
	"github.com/ly-solutions/harnessly/internal/workflows"
)

// v2Background is the live runtime's view of an agent's background work: what the transcript says runs (read
// incrementally, shared with the Legacy feed), trimmed by the agent's own footer, and the processes of a shell or a
// monitor, ended by their output file.
type v2Background struct {
	reader *background.Reader
	flows  *workflows.Reader // nil reads no workflow's run: it is listed by what the transcript says
	now    func() time.Time  // nil for the clock
}

// Subagents that ended stay in the list for a while, so a person who comes back sees what they did: at most this many,
// none that ended longer ago than endedFor.
const (
	endedFor = time.Hour
	endedMax = 12
)

var (
	_ convo.Background = v2Background{}
	_ convo.Waker      = v2Background{}
)

// devServer: the command serves something a person opens in a browser.
var devServer = regexp.MustCompile(`(?i)\b(?:(?:npm|pnpm|yarn|bun)\s+(?:run\s+)?(?:dev|start|serve|preview)|vite|next\s+dev|ng\s+serve|webpack(?:-dev-server|\s+serve)|astro\s+dev|nuxt\s+dev|uvicorn|flask\s+run|rails\s+s(?:erver)?|php\s+-S|http\.server|live-server|dev\s*server)\b`)

func (b v2Background) Tasks(c wire.Conversation, pane string) []wire.BackgroundTask {
	out := []wire.BackgroundTask{}
	if c.Provider != wire.ProviderClaude || c.TranscriptPath == "" || b.reader == nil {
		return out
	}
	running, ended := b.reader.ReadAll(c.TranscriptPath)
	for _, t := range background.ReconcileWithFooter(running, pane, pane != "") {
		task := wire.BackgroundTask{ID: t.ID, ConversationID: c.ID, Title: t.Label, Status: wire.BgStatusRunning, OutputPath: t.Output}
		if t.StartedAt > 0 {
			task.StartedAt = wire.Ms(t.StartedAt)
		}
		switch t.Kind {
		case background.Shell, background.Monitor:
			task.Kind, task.Stoppable = wire.BgCommand, t.StartedAt > 0
			task.Monitor = t.Kind == background.Monitor
			task.DevServer = devServer.MatchString(t.Label)
			if task.Title == "" {
				task.Title = "Background command"
			}
		case background.Agent:
			task.Kind = wire.BgSubagent
			if task.Title == "" {
				task.Title = "Subagent"
			}
			describeSubagent(&task, sessionmeta.ReadAgent(c.TranscriptPath, t.ID))
		case background.Workflow:
			b.describeWorkflow(&task, c.TranscriptPath, t, "")
		default:
			continue // a wakeup is no work running beside the turn
		}
		out = append(out, task)
	}
	out = append(out, b.teammateTasks(c, b.reader.Teammates(c.TranscriptPath))...)
	return append(out, b.endedTasks(c, ended)...)
}

// teammateTasks are the named teammates as subagent rows: the name for a title, what it was started for as its role.
// One at work runs since its current stretch began, an idle one waits since it said so, and one that shut down is done
// and listed as long as an ended subagent would be.
func (b v2Background) teammateTasks(c wire.Conversation, team []background.Teammate) []wire.BackgroundTask {
	clock := b.now
	if clock == nil {
		clock = time.Now
	}
	oldest := clock().Add(-endedFor).UnixMilli()
	var out []wire.BackgroundTask
	gone := 0
	for _, m := range team {
		task := wire.BackgroundTask{ID: m.ID, ConversationID: c.ID, Kind: wire.BgSubagent, Teammate: true, Title: m.Name,
			Status: wire.BgStatusRunning}
		if m.WorkFrom > 0 {
			task.StartedAt = wire.Ms(m.WorkFrom)
		}
		if m.State != background.TeammateWorking {
			task.Status = wire.BgStatusIdle
			if m.Since > 0 {
				at := wire.Ms(m.Since)
				task.EndedAt = &at
			}
		}
		if m.State == background.TeammateShutDown {
			if gone == endedMax || int64(m.Since) < oldest {
				continue
			}
			gone++
			task.Status = wire.BgStatusDone
		}
		describeSubagent(&task, sessionmeta.ReadAgent(c.TranscriptPath, m.ID))
		if task.Model == nil && m.Model != "" {
			model := m.Model
			task.Model = &model
		}
		task.Role = nil // its kind is its own name
		if m.Description != "" {
			desc := m.Description
			task.Role = &desc
		}
		out = append(out, task)
	}
	return out
}

// Wakeup is when the agent's own scheduled wakeup comes, or 0: the transcript says it was scheduled and for when, and
// a wakeup past its time is no longer listed.
func (b v2Background) Wakeup(c wire.Conversation) wire.Ms {
	if c.Provider != wire.ProviderClaude || c.TranscriptPath == "" || b.reader == nil {
		return 0
	}
	running, _ := b.reader.ReadAll(c.TranscriptPath)
	for _, t := range running {
		if t.Kind == background.Wakeup && t.Until != nil {
			return wire.Ms(*t.Until)
		}
	}
	return 0
}

// endedTasks are the subagents that ended lately, the latest first, as the page draws them: done, failed or stopped.
func (b v2Background) endedTasks(c wire.Conversation, ended []background.Ended) []wire.BackgroundTask {
	clock := b.now
	if clock == nil {
		clock = time.Now
	}
	oldest := clock().Add(-endedFor).UnixMilli()
	var out []wire.BackgroundTask
	for _, e := range ended {
		if len(out) == endedMax || int64(e.EndedAt) < oldest {
			break
		}
		at := wire.Ms(e.EndedAt)
		task := wire.BackgroundTask{ID: e.ID, ConversationID: c.ID, Kind: wire.BgSubagent, Title: e.Label, EndedAt: &at,
			Status: endedStatus(e.Status)}
		if e.StartedAt > 0 {
			task.StartedAt = wire.Ms(e.StartedAt)
		}
		if e.Kind == background.Workflow {
			b.describeWorkflow(&task, c.TranscriptPath, e.Task, e.Status)
			out = append(out, task)
			continue
		}
		if task.Title == "" {
			task.Title = "Subagent"
		}
		describeSubagent(&task, sessionmeta.ReadAgent(c.TranscriptPath, e.ID))
		out = append(out, task)
	}
	return out
}

// describeWorkflow makes a task a workflow's: its run as its files tell it, the script's name for a title when the
// launch gave no summary. A run whose final state is written is over though the agent has not been told yet; ended is
// how the agent's notification said it ended, "" while it has not.
func (b v2Background) describeWorkflow(task *wire.BackgroundTask, transcript string, t background.Task, ended string) {
	task.Kind = wire.BgWorkflow
	run := &wire.WorkflowRun{Phases: []wire.WorkflowPhase{}}
	if t.ToolUseID != "" {
		use := t.ToolUseID
		run.ToolUseID = &use
	}
	task.Workflow = run
	var view workflows.View
	ok := false
	if b.flows != nil && t.RunID != "" {
		view, ok = b.flows.Read(transcript, t.RunID)
	}
	if ok && (ended != "" || view.Over()) {
		if ended == "" {
			ended = view.Status
		}
		view = view.Settle(ended)
		task.Status = endedStatus(ended)
		if task.EndedAt == nil && view.EndedAt > 0 {
			at := wire.Ms(view.EndedAt)
			task.EndedAt = &at
		}
	}
	run.Name, run.Description = view.Name, view.Description
	if task.Title == "" {
		task.Title = or(view.Description, view.Name, "Workflow")
	}
	for _, p := range view.Phases {
		phase := wire.WorkflowPhase{Title: p.Title, Agents: []wire.WorkflowAgent{}}
		for _, a := range p.Agents {
			phase.Agents = append(phase.Agents, workflowAgent(a))
		}
		run.Phases = append(run.Phases, phase)
	}
}

// workflowAgent is an agent of a run as the page draws it.
func workflowAgent(a workflows.Agent) wire.WorkflowAgent {
	set := func(s string, n int) *string {
		if s = strings.TrimSpace(s); s == "" {
			return nil
		}
		if r := []rune(s); len(r) > n {
			s = string(r[:n-1]) + "…"
		}
		return &s
	}
	at := func(ms float64) *wire.Ms {
		if ms <= 0 {
			return nil
		}
		m := wire.Ms(ms)
		return &m
	}
	status := wire.BgStatusRunning
	switch a.State {
	case workflows.Done:
		status = wire.BgStatusDone
	case workflows.Failed:
		status = wire.BgStatusFailed
	case workflows.Stopped:
		status = wire.BgStatusStopped
	}
	label := a.Label
	if label == "" {
		label = a.ID
	}
	return wire.WorkflowAgent{ID: a.ID, Label: label, Status: status, StartedAt: at(a.StartedAt), EndedAt: at(a.EndedAt),
		Model: set(a.Model, 60), Last: set(a.Last, 140), Result: set(a.Result, 300)}
}

func or(s ...string) string {
	for _, x := range s {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

// endedStatus maps what the agent's notification said to the page's status.
func endedStatus(s string) wire.BackgroundStatus {
	switch s {
	case "completed":
		return wire.BgStatusDone
	case "killed", "stopped", "cancelled":
		return wire.BgStatusStopped
	}
	return wire.BgStatusFailed
}

// describeSubagent adds what the subagent's own files say: the model and effort it runs on, what it was started as
// and its last step.
func describeSubagent(task *wire.BackgroundTask, a sessionmeta.Agent) {
	set := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	task.Model, task.Role, task.Last = set(a.Model), set(a.Role), set(a.Last)
	if e := wire.Effort(a.Effort); slices.Contains(wire.Efforts, e) {
		task.Effort = &e
	}
}

// Stop ends the processes that write a shell's or a monitor's output file, and returns once they are gone. The agent
// notices on its own, and its transcript says so.
func (b v2Background) Stop(c wire.Conversation, id string) error {
	if b.reader == nil || c.TranscriptPath == "" {
		return errors.New("no transcript")
	}
	for _, t := range b.reader.Read(c.TranscriptPath) {
		if t.ID == id && (t.Kind == background.Shell || t.Kind == background.Monitor) {
			return endWriters(b.reader.OutputFile(c.TranscriptPath, t), 3*time.Second)
		}
	}
	return errors.New("the agent does not list that task")
}

var errNoWriter = errors.New("no process writes the task's output")

// endWriters sends SIGTERM to every process of this user whose stdout or stderr is the file, SIGKILL to those still
// there after grace, and returns when none is left.
func endWriters(file string, grace time.Duration) error {
	if !strings.HasSuffix(file, ".output") || !filepath.IsAbs(file) {
		return errors.New("not a task output file")
	}
	pids := writersOf(file)
	if len(pids) == 0 {
		return errNoWriter
	}
	for _, p := range pids {
		_ = syscall.Kill(p, syscall.SIGTERM)
	}
	deadline := time.Now().Add(grace)
	for killed := false; ; {
		left := alive(pids)
		if len(left) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			if killed {
				return errors.New("the process did not end")
			}
			for _, p := range left {
				_ = syscall.Kill(p, syscall.SIGKILL)
			}
			killed, deadline = true, time.Now().Add(time.Second)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// writersOf are the pids of this user's processes with the file as stdout or stderr.
func writersOf(file string) []int {
	entries, _ := os.ReadDir("/proc")
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		var st syscall.Stat_t
		if syscall.Stat("/proc/"+e.Name(), &st) != nil || int(st.Uid) != os.Getuid() {
			continue
		}
		for _, fd := range []string{"1", "2"} {
			if to, err := os.Readlink("/proc/" + e.Name() + "/fd/" + fd); err == nil && strings.TrimSuffix(to, " (deleted)") == file {
				pids = append(pids, pid)
				break
			}
		}
	}
	return pids
}

// alive are those of pids still running: a process that ended and waits to be reaped is not.
func alive(pids []int) []int {
	var out []int
	for _, p := range pids {
		raw, err := os.ReadFile("/proc/" + strconv.Itoa(p) + "/stat")
		if err != nil {
			continue
		}
		// pid (comm) S ...: the state follows the last ")".
		s := string(raw)
		if i := strings.LastIndexByte(s, ')'); i >= 0 && i+2 < len(s) && s[i+2] != 'Z' && s[i+2] != 'X' {
			out = append(out, p)
		}
	}
	return out
}
