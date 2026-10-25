package feed

import (
	"regexp"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The agent's task list (Claude's TaskCreate and TaskUpdate, Codex's plan updates) is not a step of the turn: the
// transcript's task-progress entries build a list of their own, in the order the tasks were made.

type task struct {
	id, subject string
	status      wire.TaskStatus
}

// taskList is the tasks the entries read so far have made.
type taskList struct {
	list []*task
	byID map[string]*task
	// byUse are Claude's tasks made without the number the agent gave them: a TaskCreate names it only in its result.
	byUse map[string]*task
}

var taskNumberRE = regexp.MustCompile(`^Task #(\d+)`)

func taskStatus(s string) wire.TaskStatus {
	if st := wire.TaskStatus(s); st.Known() {
		return st
	}
	return wire.TaskPending
}

func (l *taskList) init() {
	if l.byID == nil {
		l.byID, l.byUse = map[string]*task{}, map[string]*task{}
	}
}

func (l *taskList) put(t *task) {
	l.init()
	l.list = append(l.list, t)
	l.byID[t.id] = t
}

// progress takes one task-progress entry: a create makes a task (its number may come later, with the result of the
// call); an update sets the status of the task it names, and makes the task when it carries a subject and the list does
// not know it (Codex sends its whole plan each time).
func (l *taskList) progress(o *jsvalue.Object) {
	id, subject, use := str(o, "taskId"), str(o, "subject"), str(o, "toolUseId")
	status := taskStatus(str(o, "status"))
	if str(o, "action") == "create" {
		t := &task{id: id, subject: subject, status: status}
		if id == "" {
			t.id = use
			if use == "" {
				return
			}
			l.init()
			l.byUse[use] = t
		}
		l.put(t)
		return
	}
	if id == "" {
		return
	}
	t := l.byID[id]
	if t == nil {
		if subject == "" {
			return // made before the entries read, or by a call that never came
		}
		l.put(&task{id: id, subject: subject, status: status})
		return
	}
	t.status = status
	if subject != "" {
		t.subject = subject
	}
}

// result takes the result of a tool call: the one of a TaskCreate says the number of the task ("Task #3 created").
func (l *taskList) result(use, summary string) {
	t := l.byUse[use]
	if t == nil {
		return
	}
	delete(l.byUse, use)
	if m := taskNumberRE.FindStringSubmatch(summary); m != nil && l.byID[m[1]] == nil {
		delete(l.byID, t.id)
		t.id = m[1]
		l.byID[t.id] = t
	}
}

func (l *taskList) all() []wire.Task {
	out := make([]wire.Task, 0, len(l.list))
	for _, t := range l.list {
		out = append(out, wire.Task{ID: t.id, Subject: t.subject, Status: t.status})
	}
	return out
}
