package wire

import (
	"context"
	"errors"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/agent"
)

// TaskStatus is where a task of the agent's list stands.
type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"
	TaskInProgress TaskStatus = "in_progress"
	TaskCompleted  TaskStatus = "completed"
	TaskBlocked    TaskStatus = "blocked"
)

// TaskStatuses is every TaskStatus, in the schema's order.
var TaskStatuses = []TaskStatus{TaskPending, TaskInProgress, TaskCompleted, TaskBlocked}

// Known reports whether s is one of TaskStatuses.
func (s TaskStatus) Known() bool {
	for _, k := range TaskStatuses {
		if s == k {
			return true
		}
	}
	return false
}

// Task is one line of the plan an agent keeps while it works (Claude's tasks, Codex's plan): the id the agent gave it
// (a number, or the line's text), what it is, where it stands.
type Task struct {
	ID      string     `json:"id"`
	Subject string     `json:"subject"`
	Status  TaskStatus `json:"status"`
}

func (t Task) Validate() error {
	return errors.Join(need("id", t.ID), in("status", t.Status, TaskStatuses))
}

// Tasks is the answer of GET /api/v2/conversations/{id}/tasks: the agent's task list, in the order the tasks were made;
// empty for an agent that keeps none.
type Tasks []Task

func (t Tasks) Validate() error { return each("tasks", t) }

// tasks serves GET /api/v2/conversations/{id}/tasks.
func (s *Server) tasks(ctx context.Context, w http.ResponseWriter, r *http.Request, conv string) {
	if s.Tasks == nil {
		refuse(w, RefuseNotFound)
		return
	}
	list, err := s.Tasks(ctx, conv)
	switch {
	case err != nil && s.NotFound != nil && s.NotFound(err):
		refuse(w, RefuseNotFound)
	case err != nil && ctx.Err() != nil:
		refuse(w, RefuseTimeout)
	case err != nil:
		agent.Log("warn", "v2: tasks failed", agent.F{K: "path", V: r.URL.Path}, agent.F{K: "error", V: err.Error()})
		refuse(w, RefuseInternal)
	default:
		out := Tasks(list)
		if out == nil {
			out = Tasks{}
		}
		s.writeChecked(w, r, out)
	}
}
