package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// The team's task list: a chat that delegates keeps a list of tasks its delegated chats take for themselves.
// A task may wait for others (after); a chat claims a free one whose tasks before it are done, and a claim is a lock:
// two chats never hold one task. A task may carry a check, a command run when it is reported done: a check
// that exits non-zero gives the task back to the chat with its output. A delegated chat that took part in the list and
// ends its turn with nothing in hand is given the next free task by itself, without its parent.
//
// The list is the team's: the chat that delegates (the lead) and its delegated chats share one, kept in a small file
// beside the book. The person sees it in the plan of the lead's chat (PlanTasks).

// TaskState is where a task of the list stands.
type TaskState string

const (
	TaskOpen    TaskState = "open"
	TaskClaimed TaskState = "claimed"
	TaskDone    TaskState = "done"
)

const (
	// TasksMax is how many tasks a team's list holds.
	TasksMax = 200
	// CheckMax is the longest a check runs; CheckWait how long task_done waits for it before it answers that the check
	// still runs (the result then comes as a message), CodexCheckWait the same for Codex, whose calls end at a minute.
	CheckMax       = 20 * time.Minute
	CheckWait      = 8 * time.Minute
	CodexCheckWait = 45 * time.Second
	// CheckOutputMax is how much of a check's output goes back, its end.
	CheckOutputMax = 4000
)

// TeamTask is one task of a team's list.
type TeamTask struct {
	ID     string    `json:"id"`
	Title  string    `json:"title"`
	Detail string    `json:"detail,omitempty"`
	After  []string  `json:"after,omitempty"`
	Check  string    `json:"check,omitempty"`
	State  TaskState `json:"state"`
	// Owner is the chat that holds or did it; By the chat that added it.
	Owner  string `json:"owner,omitempty"`
	By     string `json:"by"`
	Result string `json:"result,omitempty"`
	// Checking: its check runs now. Failed is the end of the last check's output that failed.
	Checking bool   `json:"checking,omitempty"`
	Failed   string `json:"failed,omitempty"`
	Created  int64  `json:"created"`
	Claimed  int64  `json:"claimed,omitempty"`
	Done     int64  `json:"done,omitempty"`
}

type teamBoard struct {
	Next  int        `json:"next"`
	Tasks []TeamTask `json:"tasks"`
	// Joined are the chats that took «the next free task» (task_claim without an id): one that ends its turn
	// empty-handed gets the next. A chat that claimed a task by name works on that one only: given others by itself,
	// it gave them back and the chats sent them around (HRN-1524 measurement).
	Joined []string `json:"joined,omitempty"`
}

// boards are the lists of all teams of the runtime, in one file.
type boards struct {
	mu     sync.Mutex
	loaded bool
	path   string
	teams  map[string]*teamBoard
}

func (b *boards) load(path string) {
	if b.loaded {
		return
	}
	b.loaded, b.path, b.teams = true, path, map[string]*teamBoard{}
	if path == "" {
		return
	}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &b.teams)
	}
	if b.teams == nil {
		b.teams = map[string]*teamBoard{}
	}
}

func (b *boards) save() {
	if b.path == "" {
		return
	}
	raw, err := json.Marshal(b.teams)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(b.path), 0o700)
	tmp := b.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, b.path)
	}
}

// with runs f on the team's list under the lock and writes it when f says it changed.
func (s *Service) withBoard(team string, f func(b *teamBoard) bool) {
	s.boards.mu.Lock()
	defer s.boards.mu.Unlock()
	path := ""
	if s.Book != nil && s.Book.Path != "" {
		path = filepath.Join(filepath.Dir(s.Book.Path), "delegation-tasks.json")
	}
	s.boards.load(path)
	b := s.boards.teams[team]
	if b == nil {
		b = &teamBoard{}
	}
	if f(b) {
		s.boards.teams[team] = b
		s.boards.save()
	}
}

// teamOf is the team a chat belongs to: its parent's, or its own when the person started it.
func teamOf(c wire.Conversation) string {
	if c.ParentID != nil {
		return *c.ParentID
	}
	return c.ID
}

func (b *teamBoard) find(id string) int {
	return slices.IndexFunc(b.Tasks, func(t TeamTask) bool { return t.ID == id })
}

// ready tells whether a task may be claimed: open, and every task before it done.
func (b *teamBoard) ready(t TeamTask) bool {
	if t.State != TaskOpen {
		return false
	}
	for _, a := range t.After {
		if i := b.find(a); i >= 0 && b.Tasks[i].State != TaskDone {
			return false
		}
	}
	return true
}

func (b *teamBoard) holds(owner string) int {
	return slices.IndexFunc(b.Tasks, func(t TeamTask) bool { return t.State == TaskClaimed && t.Owner == owner })
}

// NewTask is a task to add.
type NewTask struct {
	Title, Detail, Check string
	After                []string
}

// AddTask puts a task on the caller's team list.
func (s *Service) AddTask(callerID string, n NewTask) (TeamTask, error) {
	caller, err := s.caller(callerID)
	if err != nil {
		return TeamTask{}, err
	}
	n.Title = strings.TrimSpace(n.Title)
	if n.Title == "" || len([]rune(n.Title)) > 200 {
		return TeamTask{}, refuse("bad-request", "title: 1 to 200 characters")
	}
	if len([]rune(n.Detail)) > PromptMax {
		return TeamTask{}, refuse("bad-request", "detail: at most %d characters", PromptMax)
	}
	if strings.TrimSpace(n.Check) != "" && !s.unasked(caller.ID) {
		return TeamTask{}, refuse("not-allowed", "a check is a command the machine runs for the team, so only a chat that runs without "+
			"asking for permissions may set one; leave check out and ask the chat to run the test itself and say so in task_done")
	}
	var out TeamTask
	err = nil
	s.withBoard(teamOf(caller), func(b *teamBoard) bool {
		if len(b.Tasks) >= TasksMax {
			err = refuse("too-many", "the team's list holds %d tasks, the most it may", TasksMax)
			return false
		}
		for _, a := range n.After {
			if b.find(a) < 0 {
				err = refuse("not-found", "no task %s on the team's list (after names tasks already added)", a)
				return false
			}
		}
		b.Next++
		out = TeamTask{ID: "t" + strconv.Itoa(b.Next), Title: n.Title, Detail: strings.TrimSpace(n.Detail), After: n.After,
			Check: strings.TrimSpace(n.Check), State: TaskOpen, By: caller.ID, Created: s.now().UnixMilli()}
		b.Tasks = append(b.Tasks, out)
		return true
	})
	return out, err
}

// TaskList is a team's list, in the order the tasks were added.
func (s *Service) TaskList(callerID string) ([]TeamTask, error) {
	caller, err := s.caller(callerID)
	if err != nil {
		return nil, err
	}
	return s.tasksOf(teamOf(caller)), nil
}

func (s *Service) tasksOf(team string) []TeamTask {
	var out []TeamTask
	s.withBoard(team, func(b *teamBoard) bool {
		out = slices.Clone(b.Tasks)
		return false
	})
	return out
}

// ClaimTask takes a task of the team's list for a chat: the one named, or the first free one whose tasks before it are
// done. A chat holds one task at a time. for_ is the chat to claim it for: the caller, or a chat the caller delegated.
func (s *Service) ClaimTask(callerID, id, for_ string) (TeamTask, error) {
	caller, err := s.caller(callerID)
	if err != nil {
		return TeamTask{}, err
	}
	owner := caller
	if for_ != "" && for_ != caller.ID {
		rec, ok := s.Convs.Record(for_)
		if !ok || rec.ParentID == nil || *rec.ParentID != caller.ID {
			return TeamTask{}, refuse("not-allowed", "chat %s was not delegated by this chat", for_)
		}
		owner = rec
	}
	var out TeamTask
	err = nil
	s.withBoard(teamOf(owner), func(b *teamBoard) bool {
		if i := b.holds(owner.ID); i >= 0 {
			err = refuse("busy", "%s already holds task %s «%s»; finish it with task_done (or task_release) first", owner.ID, b.Tasks[i].ID, b.Tasks[i].Title)
			return false
		}
		var i int
		if id != "" {
			if i = b.find(id); i < 0 {
				err = refuse("not-found", "no task %s on the team's list", id)
				return false
			}
			t := b.Tasks[i]
			switch {
			case t.State == TaskClaimed:
				err = refuse("taken", "task %s is held by %s", id, t.Owner)
				return false
			case t.State == TaskDone:
				err = refuse("taken", "task %s is done", id)
				return false
			case !b.ready(t):
				err = refuse("blocked", "task %s waits for %s to be done", id, strings.Join(t.After, ", "))
				return false
			}
		} else {
			i = slices.IndexFunc(b.Tasks, b.ready)
			if i < 0 {
				err = refuse("none-free", "no task is free: %s", boardSummary(b))
				return false
			}
		}
		b.Tasks[i].State, b.Tasks[i].Owner, b.Tasks[i].Claimed, b.Tasks[i].Failed = TaskClaimed, owner.ID, s.now().UnixMilli(), ""
		if id == "" && owner.ParentID != nil && !slices.Contains(b.Joined, owner.ID) {
			b.Joined = append(b.Joined, owner.ID)
		}
		out = b.Tasks[i]
		return true
	})
	return out, err
}

func boardSummary(b *teamBoard) string {
	counts := map[TaskState]int{}
	blocked := 0
	for _, t := range b.Tasks {
		counts[t.State]++
		if t.State == TaskOpen && !b.ready(t) {
			blocked++
		}
	}
	return fmt.Sprintf("%d done, %d held, %d waiting for others, of %d", counts[TaskDone], counts[TaskClaimed], blocked, len(b.Tasks))
}

// ReleaseTask gives a held task back to the list.
func (s *Service) ReleaseTask(callerID, id, why string) (TeamTask, error) {
	caller, err := s.caller(callerID)
	if err != nil {
		return TeamTask{}, err
	}
	var out TeamTask
	err = nil
	s.withBoard(teamOf(caller), func(b *teamBoard) bool {
		i := b.find(id)
		if i < 0 {
			err = refuse("not-found", "no task %s on the team's list", id)
			return false
		}
		t := &b.Tasks[i]
		if t.State != TaskClaimed || t.Owner != caller.ID && teamOf(caller) != caller.ID {
			err = refuse("not-allowed", "task %s is not held by this chat", id)
			return false
		}
		t.State, t.Owner, t.Claimed, t.Checking = TaskOpen, "", 0, false
		if why = strings.TrimSpace(why); why != "" {
			t.Result = clip("given back: "+why, 2000)
		}
		out = *t
		return true
	})
	return out, err
}

// CheckRunner runs a check command in a folder: its output (stdout and stderr together) and exit code.
type CheckRunner func(ctx context.Context, dir, command string) (output string, code int, err error)

// unasked tells whether a chat runs without asking for permissions (bypass).
func (s *Service) unasked(conv string) bool {
	return s.Permission != nil && s.Permission(conv) == wire.PermissionBypass
}

// checkEnv is the environment a check runs with: the runtime's own minus its secrets and connections.
func checkEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "PATH", "HOME", "USER", "LOGNAME", "SHELL", "LANG", "LC_ALL", "TMPDIR", "GOPATH", "GOCACHE", "GOFLAGS", "NODE_OPTIONS",
			"HARNESSLY_HEAVY_REMOTE":
			out = append(out, kv)
		}
	}
	return out
}

func shCheck(ctx context.Context, dir, command string) (string, int, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir, cmd.Env = dir, checkEnv()
	// Its own process group, killed whole at the timeout: a child left behind would hold the pipe and the task.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	out := &tailBuffer{max: 4 * CheckOutputMax}
	cmd.Stdout, cmd.Stderr = out, out
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return out.String(), exit.ExitCode(), nil
	}
	if err != nil {
		return out.String(), -1, err
	}
	return out.String(), 0, nil
}

// tailBuffer keeps the last max bytes written to it: a noisy check does not fill the memory.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	b   []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if over := len(t.b) - t.max; over > 0 {
		t.b = append(t.b[:0], t.b[over:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.b)
}

// DoneOutcome is how task_done ended.
type DoneOutcome struct {
	Task TeamTask
	// Passed: the task is done (its check passed, or it had none). Pending: the check still runs and its result comes
	// as a message. Otherwise the check failed and Output says how.
	Passed, Pending bool
	Code            int
	Output          string
	// Next is a task the caller may claim now, when there is one.
	Next *TeamTask
}

// DoneTask reports a held task done. A task with a check is done only when the check passes; a failing check keeps
// the task with the chat, and the answer carries its output. wait bounds how long the call waits for the check.
func (s *Service) DoneTask(ctx context.Context, callerID, id, result string, wait time.Duration) (DoneOutcome, error) {
	caller, err := s.caller(callerID)
	if err != nil {
		return DoneOutcome{}, err
	}
	team := teamOf(caller)
	var task TeamTask
	err = nil
	s.withBoard(team, func(b *teamBoard) bool {
		i := b.find(id)
		if i < 0 {
			err = refuse("not-found", "no task %s on the team's list", id)
			return false
		}
		t := &b.Tasks[i]
		switch {
		case t.State == TaskDone:
			err = refuse("taken", "task %s is already done", id)
			return false
		case t.State != TaskClaimed || t.Owner != caller.ID:
			err = refuse("not-allowed", "task %s is not held by this chat; claim it first (task_claim)", id)
			return false
		case t.Checking:
			err = refuse("busy", "the check of task %s runs already; its result comes as a message", id)
			return false
		}
		t.Result = clip(strings.TrimSpace(result), 4000)
		if t.Check != "" {
			t.Checking = true
		}
		task = *t
		return true
	})
	if err != nil {
		return DoneOutcome{}, err
	}
	if task.Check == "" {
		return s.finishTask(team, caller.ID, id, 0, ""), nil
	}
	if !s.unasked(caller.ID) || !s.unasked(task.By) {
		// The check runs as the machine: a chat that asks before it acts could run through it what it would be asked about.
		s.withBoard(team, func(b *teamBoard) bool {
			if i := b.find(id); i >= 0 {
				b.Tasks[i].Checking = false
			}
			return true
		})
		return DoneOutcome{}, refuse("not-allowed", "task %s has a check, and this chat (or the one that added it) asks before it acts: "+
			"run the check yourself (%s), then release the task to the chat that delegates with the result", id, task.Check)
	}
	got := make(chan DoneOutcome, 1)
	go func() {
		cctx, cancel := context.WithTimeout(context.Background(), CheckMax)
		defer cancel()
		run := s.Check
		if run == nil {
			run = shCheck
		}
		out, code, rerr := run(cctx, caller.Cwd, task.Check)
		if rerr != nil {
			out, code = strings.TrimSpace(out+"\n"+rerr.Error()), -1
		} else if cctx.Err() != nil {
			out, code = strings.TrimSpace(out+fmt.Sprintf("\n(the check ran longer than %d minutes and was stopped)", int(CheckMax/time.Minute))), -1
		}
		got <- s.finishTask(team, caller.ID, id, code, out)
	}()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case o := <-got:
		return o, nil
	case <-timer.C:
	case <-ctx.Done():
	}
	// The call gives up waiting; the check goes on and its result reaches the chat as a message.
	go func() {
		o := <-got
		cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s.sendTo(cctx, caller.ID, checkMessage(o))
	}()
	return DoneOutcome{Task: task, Pending: true}, nil
}

// finishTask records a check's end: done on code 0, back with the chat otherwise.
func (s *Service) finishTask(team, owner, id string, code int, output string) DoneOutcome {
	o := DoneOutcome{Code: code, Output: tail(output, CheckOutputMax), Passed: code == 0}
	s.withBoard(team, func(b *teamBoard) bool {
		i := b.find(id)
		if i < 0 {
			return false
		}
		t := &b.Tasks[i]
		if t.State != TaskClaimed || t.Owner != owner {
			// Released and taken again while the check ran: its result is the old owner's, not this task's now.
			o.Passed, o.Task = false, *t
			return false
		}
		t.Checking = false
		if o.Passed {
			t.State, t.Done, t.Failed = TaskDone, s.now().UnixMilli(), ""
		} else {
			t.Failed = o.Output
		}
		o.Task = *t
		if o.Passed {
			if j := slices.IndexFunc(b.Tasks, b.ready); j >= 0 {
				next := b.Tasks[j]
				o.Next = &next
			}
		}
		return true
	})
	return o
}

func tail(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return "…" + string(r[len(r)-max:])
}

// checkMessage is what a chat is told when the check of a task it reported done ends after task_done answered.
func checkMessage(o DoneOutcome) string {
	if o.Passed {
		return fmt.Sprintf("[automatic] The check of task %s «%s» passed: the task is done.", o.Task.ID, o.Task.Title)
	}
	return fmt.Sprintf("[automatic] The check of task %s «%s» failed (exit code %d), so the task is still yours. Fix it and report it "+
		"done again with task_done.\n\nThe check: %s\nIts output:\n%s", o.Task.ID, o.Task.Title, o.Code, o.Task.Check, o.Output)
}

// assignNext gives a delegated chat that took part in its team's list and ended its turn empty-handed the next free
// task, and tells it so. It reports whether it did: the chat works on, and its parent is not woken.
func (s *Service) assignNext(rec wire.Conversation) bool {
	if rec.ParentID == nil {
		return false
	}
	var next TeamTask
	s.withBoard(*rec.ParentID, func(b *teamBoard) bool {
		if !slices.Contains(b.Joined, rec.ID) || b.holds(rec.ID) >= 0 {
			return false
		}
		i := slices.IndexFunc(b.Tasks, b.ready)
		if i < 0 {
			return false
		}
		b.Tasks[i].State, b.Tasks[i].Owner, b.Tasks[i].Claimed, b.Tasks[i].Failed = TaskClaimed, rec.ID, s.now().UnixMilli(), ""
		next = b.Tasks[i]
		return true
	})
	if next.ID == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	asked := s.now().UnixMilli()
	if !s.sendTo(ctx, rec.ID, NextTaskText(next)) {
		_, _ = s.ReleaseTask(rec.ID, next.ID, "")
		return false
	}
	s.Book.Update(rec.ID, func(p *Page) { p.asked(asked) })
	return true
}

// NextTaskText is what a free chat is told when the platform gives it the next task of its team's list.
func NextTaskText(t TeamTask) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[automatic] You are free, so the next task of your team's list is yours now: %s «%s».", t.ID, t.Title)
	if t.Detail != "" {
		b.WriteString("\n\n" + t.Detail)
	}
	if t.Check != "" {
		b.WriteString("\n\nWhen you report it done, this check runs and must pass: " + t.Check)
	}
	b.WriteString("\n\nDo it, then report it with task_done (id " + t.ID + ") and end your turn with " + MarkDone + "; if you cannot, task_release it with why.")
	return b.String()
}

// TaskLine is a task in a line, as the tools show it.
func TaskLine(t TeamTask, ready bool) string {
	state := string(t.State)
	switch {
	case t.State == TaskClaimed && t.Checking:
		state = "checking, " + t.Owner
	case t.State == TaskClaimed:
		state = "held by " + t.Owner
	case t.State == TaskOpen && !ready:
		state = "waits for " + strings.Join(t.After, ", ")
	case t.State == TaskOpen:
		state = "free"
	case t.State == TaskDone:
		state = "done by " + t.Owner
	}
	line := fmt.Sprintf("%s [%s] %s", t.ID, state, t.Title)
	if t.Check != "" {
		line += "\n  check: " + t.Check
	}
	if t.Failed != "" && t.State != TaskDone {
		line += "\n  last check failed: " + clip(strings.ReplaceAll(t.Failed, "\n", " "), 300)
	}
	if t.Result != "" {
		line += "\n  result: " + clip(strings.ReplaceAll(t.Result, "\n", " "), 300)
	}
	return line
}

// ListText is the team's list as task_list answers it.
func (s *Service) ListText(callerID string) (string, error) {
	caller, err := s.caller(callerID)
	if err != nil {
		return "", err
	}
	var text string
	s.withBoard(teamOf(caller), func(b *teamBoard) bool {
		if len(b.Tasks) == 0 {
			text = "The team's list is empty. task_add puts a task on it."
			return false
		}
		lines := make([]string, 0, len(b.Tasks)+1)
		lines = append(lines, boardSummary(b)+".")
		for _, t := range b.Tasks {
			if t.Detail != "" && t.State != TaskDone {
				lines = append(lines, TaskLine(t, b.ready(t))+"\n  "+clip(strings.ReplaceAll(t.Detail, "\n", " "), 400))
				continue
			}
			lines = append(lines, TaskLine(t, b.ready(t)))
		}
		text = strings.Join(lines, "\n")
		return false
	})
	return text, nil
}

// PlanTasks are the team's tasks of a chat that leads one, as the plan of its chat shows them (GET …/tasks): the title
// with the chat that holds it.
func (s *Service) PlanTasks(conv string, titleOf func(id string) string) []wire.Task {
	var out []wire.Task
	s.withBoard(conv, func(b *teamBoard) bool {
		for _, t := range b.Tasks {
			st := wire.TaskPending
			switch {
			case t.State == TaskDone:
				st = wire.TaskCompleted
			case t.State == TaskClaimed:
				st = wire.TaskInProgress
			case !b.ready(t):
				st = wire.TaskBlocked
			}
			subject := t.Title
			if t.Owner != "" && titleOf != nil {
				if name := titleOf(t.Owner); name != "" {
					subject += " — " + name
				}
			}
			out = append(out, wire.Task{ID: "team-" + t.ID, Subject: subject, Status: st})
		}
		return false
	})
	return out
}

// DoneText is the answer of task_done.
func DoneText(o DoneOutcome) string {
	switch {
	case o.Pending:
		return fmt.Sprintf("task %s is reported; its check (%s) still runs. Its result comes to you as a message: if it fails, the task is yours to fix.", o.Task.ID, o.Task.Check)
	case !o.Passed:
		return fmt.Sprintf("refused: the check of task %s failed (exit code %d), so the task is still yours. Fix it and call task_done again.\n\nThe check: %s\nIts output:\n%s",
			o.Task.ID, o.Code, o.Task.Check, o.Output)
	}
	text := "task " + o.Task.ID + " «" + o.Task.Title + "» is done"
	if o.Task.Check != "" {
		text += " (its check passed)"
	}
	text += "."
	if o.Next != nil {
		text += "\nNext free task: " + o.Next.ID + " «" + o.Next.Title + "» — task_claim it, or end your turn and it is given to you."
	} else {
		text += "\nNo task is free now."
	}
	return text
}
