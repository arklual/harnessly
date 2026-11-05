package wire

import (
	"context"
	"errors"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

// MadeKind is what a chat made, opened or changed that the «made in this chat» block lists.
type MadeKind string

const (
	MadeDoc    MadeKind = "doc"
	MadeBoard  MadeKind = "board"
	MadeDesign MadeKind = "design"
	// MadeTask is a tracker task or epic the chat created or touched. It is only sent to a page that asked for tasks
	// (?tasks=1): a page that does not know the kind is never shown it.
	MadeTask MadeKind = "task"
)

// MadeKinds is every MadeKind, in the schema's order.
var MadeKinds = []MadeKind{MadeDoc, MadeBoard, MadeDesign, MadeTask}

// MadeMax is the most items one answer holds.
const MadeMax = 60

// MadeTaskRef is a task named by another one: its epic, or a subtask of an epic.
type MadeTaskRef struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	State    string `json:"state"`
	Resolved bool   `json:"resolved"`
}

// MadeTaskInfo is what the tracker says of a task the chat touched: its kind («Epic»), state, the epic it belongs to
// and, for an epic, its subtasks. Type and State are "" when the tracker does not say.
type MadeTaskInfo struct {
	Key      string        `json:"key"`
	Type     string        `json:"type"`
	State    string        `json:"state"`
	Resolved bool          `json:"resolved"`
	Parent   *MadeTaskRef  `json:"parent"`
	Children []MadeTaskRef `json:"children"`
}

// MadeTaskChildrenMax is the most subtasks of one epic an item carries.
const MadeTaskChildrenMax = 50

// MadeItem is one thing the chat made or touched. Ref is the page's URL for a doc or a board (a board keeps its ?k=
// key, it is what opens it; never show it), the design's id for a design, the task's address for a task. Title is "" when the runtime does not know
// it. Op is "created" or "changed" (opening counts as changed). Cover and DesignKind are set for a design only.
type MadeItem struct {
	Kind       MadeKind    `json:"kind"`
	Ref        string      `json:"ref"`
	Title      string      `json:"title"`
	Op         OutputOp    `json:"op"`
	At         Ms          `json:"at"`
	Cover      *string     `json:"cover"`
	DesignKind *DesignKind `json:"designKind"`
	// Task is set for a task only, and only when the tracker could be read; it is left out otherwise.
	Task *MadeTaskInfo `json:"task,omitempty"`
}

func (m MadeItem) Validate() error {
	errs := []error{in("kind", m.Kind, MadeKinds), need("ref", m.Ref), in("op", m.Op, OutputOps), nonNegative("at", m.At)}
	if m.Kind == MadeDesign {
		if m.DesignKind == nil {
			errs = append(errs, errors.New("designKind: null for a design"))
		} else {
			errs = append(errs, in("designKind", *m.DesignKind, DesignKinds))
		}
	} else if m.Cover != nil || m.DesignKind != nil {
		errs = append(errs, errors.New("cover and designKind are for a design"))
	}
	if m.Task != nil {
		if m.Kind != MadeTask {
			errs = append(errs, errors.New("task is for a task"))
		} else {
			errs = append(errs, need("task.key", m.Task.Key))
			if len(m.Task.Children) > MadeTaskChildrenMax {
				errs = append(errs, errors.New("task.children: over MadeTaskChildrenMax"))
			}
		}
	}
	return errors.Join(errs...)
}

// ChatMade is GET /api/v2/conversations/{id}/made: what the chat made, opened or changed, newest first, at most
// MadeMax. Complete is false while the runtime is still reading a long transcript behind the answer; ask again.
type ChatMade struct {
	Items    []MadeItem `json:"items"`
	Complete bool       `json:"complete"`
}

func (c ChatMade) Validate() error {
	errs := []error{each("items", c.Items)}
	if len(c.Items) > MadeMax {
		errs = append(errs, errors.New("items: over MadeMax"))
	}
	return errors.Join(errs...)
}

type madeTasksKey struct{}

// WithMadeTasks is ctx as the made handler gives it for a request with ?tasks=1.
func WithMadeTasks(ctx context.Context) context.Context {
	return context.WithValue(ctx, madeTasksKey{}, true)
}

// MadeWantsTasks is whether the page asked for tasks among what the chat made (?tasks=1); the runtime's answer to a page
// that did not ask has none.
func MadeWantsTasks(ctx context.Context) bool {
	v, _ := ctx.Value(madeTasksKey{}).(bool)
	return v
}

// made serves GET /api/v2/conversations/{id}/made. It reads and changes nothing.
func (s *Server) made(ctx context.Context, w http.ResponseWriter, r *http.Request, conv string) {
	if s.Made == nil {
		refuse(w, RefuseNotFound)
		return
	}
	if r.URL.Query().Get("tasks") == "1" {
		ctx = WithMadeTasks(ctx)
	}
	out, err := s.Made(ctx, conv)
	switch {
	case err != nil && s.NotFound != nil && s.NotFound(err):
		refuse(w, RefuseNotFound)
	case err != nil:
		jsonlog.Log("warn", "v2: made failed", jsonlog.F{K: "path", V: r.URL.Path}, jsonlog.F{K: "error", V: err.Error()})
		refuse(w, RefuseInternal)
	default:
		if out.Items == nil {
			out.Items = []MadeItem{}
		}
		s.writeChecked(w, r, out)
	}
}

type requestKey struct{}

// WithRequest carries the page's request into a service call, so the service can tell whose session asked (HRN-675:
// a design made from the page belongs to that person, not to the one the machine belongs to).
func WithRequest(ctx context.Context, r *http.Request) context.Context {
	return context.WithValue(ctx, requestKey{}, r)
}

// RequestOf is the page's request a call was made for; nil when the call came another way.
func RequestOf(ctx context.Context) *http.Request {
	r, _ := ctx.Value(requestKey{}).(*http.Request)
	return r
}
