package apps

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

// The statuses of an app's documents: the manifest's `workflow` names them and the moves between them, each
// with the project role it takes; the host keeps every document's status and journal and checks who makes a move.
//
//   - A status belongs to a version of the document: its number and the hash of its content. The first status of the
//     list is where a new version starts (a draft); a status with `edit` lets the document change under it, and the
//     version follows the changes. A change to a version whose status has no `edit` (on review, approved…) does not
//     touch that version: it starts the next one, in the first status, and the earlier version keeps its word.
//   - The record lives in the app's doc area under WorkflowKey, which a view may not read or write as a key: under a hub
//     that area is the hub's, so the status is the same on every machine of the space.
//   - A view reads the status and moves it through hl.workflow; the agent of a chat that works on the document is told
//     the status at its turn, and the app's tool rules can depend on it (agent.tools.when).

// WorkflowKey is the key of the doc area that holds a document's statuses. Keys with the host's prefix are not a view's.
const WorkflowKey = "__hl_workflow"

// Limits of a workflow and of its record.
const (
	MaxWorkflowStatuses    = 16
	MaxWorkflowTransitions = 64
	MaxWorkflowJournal     = 200
	MaxWorkflowNote        = 500
	MaxWorkflowTitle       = 60
)

// Workflow is the manifest's `workflow`.
type Workflow struct {
	Statuses    []WorkflowStatus     `json:"statuses"`
	Transitions []WorkflowTransition `json:"transitions"`
}

// WorkflowStatus is one status. Tone is how the host may colour it: neutral, info, warning, success, danger.
type WorkflowStatus struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Edit  bool   `json:"edit,omitempty"`
	Tone  string `json:"tone,omitempty"`
}

// WorkflowTransition is a move from one status to another; Role is the least project role that may make it (view,
// comment, edit, manage, owner; edit when empty).
type WorkflowTransition struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Title string `json:"title"`
	Role  string `json:"role,omitempty"`
}

var (
	workflowIDRe    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	workflowTones   = map[string]bool{"": true, "neutral": true, "info": true, "warning": true, "success": true, "danger": true}
	projectRoleRank = map[string]int{"view": 1, "comment": 2, "edit": 3, "manage": 4, "owner": 5}
)

// RoleAtLeast says whether a project role is min or stronger; no role is never enough.
func RoleAtLeast(role, min string) bool {
	return projectRoleRank[role] > 0 && projectRoleRank[role] >= projectRoleRank[min]
}

func (t WorkflowTransition) role() string {
	if t.Role == "" {
		return "edit"
	}
	return t.Role
}

// checkWorkflow is the part of the manifest check about `workflow`.
func (m *Manifest) checkWorkflow(add func(path, format string, a ...any)) {
	w := m.Workflow
	var when []ToolWhen
	if m.Agent != nil && m.Agent.Tools != nil {
		when = m.Agent.Tools.When
	}
	if w == nil {
		if len(when) > 0 {
			add("agent.tools.when", "names statuses of the app's workflow: the manifest has no workflow")
		}
		return
	}
	sinopia := false
	for _, t := range m.Targets {
		if IsSinopiaTarget(t) {
			sinopia = true
		}
	}
	if !sinopia {
		add("workflow", "is for the app's documents: the app needs a sinopia.<kind> target")
	}
	if len(w.Statuses) == 0 || len(w.Statuses) > MaxWorkflowStatuses {
		add("workflow.statuses", "has 1 to %d statuses", MaxWorkflowStatuses)
	}
	ids := map[string]bool{}
	for i, s := range w.Statuses {
		path := fmt.Sprintf("workflow.statuses[%d]", i)
		if !workflowIDRe.MatchString(s.ID) {
			add(path+".id", "%q is not an id like on-review", s.ID)
		} else if ids[s.ID] {
			add(path+".id", "%q is named twice", s.ID)
		}
		ids[s.ID] = true
		if s.Title == "" || len([]rune(s.Title)) > MaxWorkflowTitle {
			add(path+".title", "is 1 to %d characters", MaxWorkflowTitle)
		}
		if !workflowTones[s.Tone] {
			add(path+".tone", "%q is not neutral, info, warning, success or danger", s.Tone)
		}
	}
	if len(w.Statuses) > 0 && !w.Statuses[0].Edit {
		add("workflow.statuses[0].edit", "the first status is where a new version starts: it must allow edits (edit: true)")
	}
	if len(w.Transitions) > MaxWorkflowTransitions {
		add("workflow.transitions", "has at most %d moves", MaxWorkflowTransitions)
	}
	for i, wr := range when {
		path := fmt.Sprintf("agent.tools.when[%d]", i)
		if len(wr.Status) == 0 {
			add(path+".status", "names at least one status")
		}
		for _, st := range wr.Status {
			if !ids[st] {
				add(path+".status", "%q is not a status of the workflow", st)
			}
		}
		if len(wr.Allow)+len(wr.Deny)+len(wr.Confirm) == 0 {
			add(path, "has no rules: allow, deny or confirm")
		}
	}
	seen := map[[2]string]bool{}
	for i, t := range w.Transitions {
		path := fmt.Sprintf("workflow.transitions[%d]", i)
		if !ids[t.From] {
			add(path+".from", "%q is not a status of the workflow", t.From)
		}
		if !ids[t.To] {
			add(path+".to", "%q is not a status of the workflow", t.To)
		}
		if t.From == t.To {
			add(path, "moves from a status to itself")
		}
		if seen[[2]string{t.From, t.To}] {
			add(path, "the move %s → %s is named twice", t.From, t.To)
		}
		seen[[2]string{t.From, t.To}] = true
		if t.Title == "" || len([]rune(t.Title)) > MaxWorkflowTitle {
			add(path+".title", "is 1 to %d characters: what the button says", MaxWorkflowTitle)
		}
		if t.Role != "" && projectRoleRank[t.Role] == 0 {
			add(path+".role", "%q is not view, comment, edit, manage or owner", t.Role)
		}
	}
}

// WorkflowRecord is what the host keeps of a document: its versions (the last is the current one) and the journal.
type WorkflowRecord struct {
	Versions []WorkflowVersion `json:"versions"`
	Journal  []WorkflowEntry   `json:"journal"`
}

// WorkflowVersion is a version of the document with its status.
type WorkflowVersion struct {
	N      int    `json:"n"`
	Hash   string `json:"hash"`
	Status string `json:"status"`
}

// WorkflowEntry is one line of the journal: a move, or a new version started by a change.
type WorkflowEntry struct {
	At      int64  `json:"at"` // ms
	Who     string `json:"who,omitempty"`
	From    string `json:"from,omitempty"`
	To      string `json:"to"`
	Version int    `json:"version"`
	Hash    string `json:"hash"`
	Note    string `json:"note,omitempty"`
	// Rev is the project's named version the move was made on (Sinopia's history): the decision is on that point.
	Rev string `json:"rev,omitempty"`
	// Link opens Rev read only; it is filled in what a view reads, never kept.
	Link string `json:"link,omitempty"`
}

// Errors of a move. ErrWorkflowStale: the version the move names is not the current one any more.
var (
	ErrNoWorkflow    = errors.New("apps: the app has no workflow")
	ErrWorkflowMove  = errors.New("apps: no such move from the current status")
	ErrWorkflowRole  = errors.New("apps: the person's role does not allow this move")
	ErrWorkflowStale = errors.New("apps: the document changed: the move names a version that is not the current one")
	ErrWorkflowNote  = errors.New("apps: the note is too long")
)

func (w *Workflow) status(id string) (WorkflowStatus, bool) {
	for _, s := range w.Statuses {
		if s.ID == id {
			return s, true
		}
	}
	return WorkflowStatus{}, false
}

func (r *WorkflowRecord) log(e WorkflowEntry) {
	r.Journal = append(r.Journal, e)
	if len(r.Journal) > MaxWorkflowJournal {
		r.Journal = append([]WorkflowEntry(nil), r.Journal[len(r.Journal)-MaxWorkflowJournal:]...)
	}
}

// Follow brings the record up to the document's content now (hash): the first version of a new document, the hash of
// a version that may be edited, or the next version when one that may not be edited was changed. A status the
// workflow no longer has counts as the first one. It says whether the record changed.
func (w *Workflow) Follow(r *WorkflowRecord, hash string, now time.Time) bool {
	first := w.Statuses[0].ID
	if len(r.Versions) == 0 {
		r.Versions = []WorkflowVersion{{N: 1, Hash: hash, Status: first}}
		r.log(WorkflowEntry{At: now.UnixMilli(), To: first, Version: 1, Hash: hash})
		return true
	}
	cur := &r.Versions[len(r.Versions)-1]
	st, known := w.status(cur.Status)
	if !known {
		cur.Status, st = first, w.Statuses[0]
	}
	if cur.Hash == hash {
		return !known
	}
	if st.Edit {
		cur.Hash = hash
		return true
	}
	n := cur.N + 1
	r.Versions = append(r.Versions, WorkflowVersion{N: n, Hash: hash, Status: first})
	r.log(WorkflowEntry{At: now.UnixMilli(), From: cur.Status, To: first, Version: n, Hash: hash, Note: "changed"})
	return true
}

// Current is the current version; zero before Follow.
func (r *WorkflowRecord) Current() WorkflowVersion {
	if len(r.Versions) == 0 {
		return WorkflowVersion{}
	}
	return r.Versions[len(r.Versions)-1]
}

// WorkflowMove is a move offered from the current status, and whether this person may make it.
type WorkflowMove struct {
	To      string `json:"to"`
	Title   string `json:"title"`
	Role    string `json:"role"`
	Allowed bool   `json:"allowed"`
}

// Moves are the moves from a status, in the manifest's order.
func (w *Workflow) Moves(status, role string) []WorkflowMove {
	out := []WorkflowMove{}
	for _, t := range w.Transitions {
		if t.From == status {
			out = append(out, WorkflowMove{To: t.To, Title: t.Title, Role: t.role(), Allowed: RoleAtLeast(role, t.role())})
		}
	}
	return out
}

// Move makes a move of the current version to `to`, by who with role; version and hash, when given, are the version
// the person saw, and the move is refused when the document is at another one.
func (w *Workflow) Move(r *WorkflowRecord, to, who, role string, version int, hash, note string, now time.Time) error {
	if len([]rune(note)) > MaxWorkflowNote {
		return ErrWorkflowNote
	}
	cur := r.Current()
	if cur.N == 0 || (version != 0 && version != cur.N) || (hash != "" && hash != cur.Hash) {
		return ErrWorkflowStale
	}
	var move *WorkflowTransition
	for i := range w.Transitions {
		if w.Transitions[i].From == cur.Status && w.Transitions[i].To == to {
			move = &w.Transitions[i]
		}
	}
	if move == nil {
		return ErrWorkflowMove
	}
	if !RoleAtLeast(role, move.role()) {
		return ErrWorkflowRole
	}
	r.Versions[len(r.Versions)-1].Status = to
	r.log(WorkflowEntry{At: now.UnixMilli(), Who: who, From: cur.Status, To: to, Version: cur.N, Hash: cur.Hash, Note: note})
	return nil
}

// ParseWorkflowRecord reads a kept record; nothing kept is an empty one.
func ParseWorkflowRecord(raw json.RawMessage) (WorkflowRecord, error) {
	var r WorkflowRecord
	if len(raw) == 0 || string(raw) == "null" {
		return r, nil
	}
	err := json.Unmarshal(raw, &r)
	return r, err
}

// WorkflowState is what a view and the agent are told: the current version, its status, the moves and the journal.
type WorkflowState struct {
	Version  int               `json:"version"`
	Hash     string            `json:"hash"`
	Status   WorkflowStatus    `json:"status"`
	Statuses []WorkflowStatus  `json:"statuses"`
	Moves    []WorkflowMove    `json:"moves"`
	Role     string            `json:"role"`
	Journal  []WorkflowEntry   `json:"journal"`
	Versions []WorkflowVersion `json:"versions"`
}

// State is the record as a view reads it, for a person with role.
func (w *Workflow) State(r WorkflowRecord, role string) WorkflowState {
	cur := r.Current()
	st, _ := w.status(cur.Status)
	journal := append([]WorkflowEntry{}, r.Journal...)
	return WorkflowState{Version: cur.N, Hash: cur.Hash, Status: st, Statuses: w.Statuses,
		Moves: w.Moves(cur.Status, role), Role: role, Journal: journal, Versions: r.Versions}
}

// MarkRev puts the named version a move was made on in its journal entry (the one at at, of that version); it says
// whether there was one.
func (r *WorkflowRecord) MarkRev(at int64, version int, rev string) bool {
	for i := len(r.Journal) - 1; i >= 0; i-- {
		if e := &r.Journal[i]; e.At == at && e.Version == version && e.From != "" {
			if e.Rev != "" {
				return false
			}
			e.Rev = rev
			return true
		}
	}
	return false
}

// Status is a status of the workflow by id.
func (w *Workflow) Status(id string) (WorkflowStatus, bool) { return w.status(id) }
