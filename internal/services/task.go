package services

import (
	"context"
	"math"
	"net/http"
	"regexp"
	"sync"
	"time"

	"harnessly/internal/files"
	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
	"harnessly/internal/tmux"
)

const (
	taskFresh       = time.Minute
	taskEntries     = 200
	taskMaxBytes    = 400_000
	taskDescMax     = 20 * 1024
	taskCommentMax  = 4 * 1024
	taskComments    = 10
	taskChildrenMax = 50
	taskTextMax     = 300
	taskFields      = "idReadable,summary,description,resolved,updated,project(shortName),parent(issues(idReadable,summary))," +
		"subtasks(issues(idReadable,summary,resolved,customFields(name,value(name)))),customFields(name,value(name,login,fullName,avatarUrl))," +
		"comments(author(login,fullName),created,text)"
)

// Task errors and their statuses.
const (
	TaskNotFound = "not-found"
	TaskNoToken  = "no-token"
	TaskTracker  = "tracker"
)

type taskEntry struct {
	task *jsvalue.Object
	at   time.Time
}

type taskResult struct {
	task *jsvalue.Object
	err  string
}

type taskCall struct {
	done chan struct{}
	res  taskResult
}

type taskState struct {
	mu       sync.Mutex
	order    []string
	entries  map[string]taskEntry
	inflight map[string]*taskCall
}

func newTaskState() *taskState {
	return &taskState{entries: map[string]taskEntry{}, inflight: map[string]*taskCall{}}
}

// IsTaskKey: a key of a tracker space the card serves.
func (s *Service) IsTaskKey(v any) bool {
	k, ok := v.(string)
	return ok && s.cfg.TaskKey != nil && s.cfg.TaskKey.MatchString(k)
}

var crlfRe = regexp.MustCompile(`\r\n?`)

// cleanTaskText is untrusted text bound for the page: controls out (ESC stays visible), line ends unified, clipped to
// max units with an ellipsis.
func cleanTaskText(v any, max int) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	t := tmux.StripControlChars(crlfRe.ReplaceAllString(s, "\n"))
	if jscompat.Len(t) > max {
		return jscompat.Slice(t, max-1) + "…"
	}
	return t
}

func shortText(v any) any {
	t := jscompat.Trim(spaceRunRe.ReplaceAllString(cleanTaskText(v, taskTextMax), " "))
	if t == "" {
		return nil
	}
	return t
}

func isRecord(v any) bool { _, ok := v.(*jsvalue.Object); return ok }

func records(v any) []any {
	list, _ := v.([]any)
	var out []any
	for _, e := range list {
		if isRecord(e) {
			out = append(out, e)
		}
	}
	return out
}

func fieldValue(fields any, name string) any {
	for _, f := range records(fields) {
		if n, ok := opt(f, "name").(string); ok && n == name {
			if v := opt(f, "value"); isRecord(v) {
				return v
			}
			return nil
		}
	}
	return nil
}

func firstIssue(link any) any {
	var list any
	if isRecord(link) {
		list = opt(link, "issues")
	}
	if r := records(list); len(r) > 0 {
		return r[0]
	}
	return nil
}

func timestamp(v any) any {
	if f, ok := v.(float64); ok && !math.IsNaN(f) && !math.IsInf(f, 0) {
		return f
	}
	return nil
}

// or is a ?? b for the values short() gives.
func or(a, b any) any {
	if a == nil {
		return b
	}
	return a
}

// parseTask reduces the tracker's answer for one issue to what the card shows; nil when it is not an issue of a known
// space.
func (s *Service) parseTask(raw any, avatarOf func(login string, source any) any) *jsvalue.Object {
	if !isRecord(raw) || !s.IsTaskKey(opt(raw, "idReadable")) {
		return nil
	}
	title := shortText(opt(raw, "summary"))
	if title == nil {
		return nil
	}
	fields := opt(raw, "customFields")
	nameOf := func(field string) any { return shortText(opt(fieldValue(fields, field), "name")) }
	person := fieldValue(fields, "Assignee")
	var login any
	if l, ok := opt(person, "login").(string); ok && person != nil {
		login = l
	}
	var assigneeName any
	if person != nil {
		assigneeName = or(or(shortText(opt(person, "fullName")), shortText(opt(person, "name"))), login)
	}
	var assignee any
	if truthy(assigneeName) {
		var av any
		if truthy(login) {
			var src any
			if v, ok := opt(person, "avatarUrl").(string); ok {
				src = v
			}
			av = avatarOf(login.(string), src)
		}
		assignee = obj("name", assigneeName, "avatar", av)
	}
	var parentOut any
	if parent := firstIssue(opt(raw, "parent")); parent != nil {
		if key := opt(parent, "idReadable"); s.IsTaskKey(key) {
			parentOut = obj("key", key, "title", or(shortText(opt(parent, "summary")), key))
		}
	}
	var project any
	if p := opt(raw, "project"); isRecord(p) {
		project = shortText(opt(p, "shortName"))
	}
	var subtasks any
	if st := opt(raw, "subtasks"); isRecord(st) {
		subtasks = opt(st, "issues")
	}
	children := []any{}
	for _, c := range records(subtasks) {
		key := opt(c, "idReadable")
		if !s.IsTaskKey(key) {
			continue
		}
		if len(children) == taskChildrenMax {
			break
		}
		children = append(children, obj("key", key, "title", or(shortText(opt(c, "summary")), key),
			"state", shortText(opt(fieldValue(opt(c, "customFields"), "State"), "name")), "resolved", !nullish(opt(c, "resolved"))))
	}
	all := records(opt(raw, "comments"))
	if len(all) > taskComments {
		all = all[len(all)-taskComments:]
	}
	comments := make([]any, 0, len(all))
	for _, c := range all {
		var author any
		if a := opt(c, "author"); isRecord(a) {
			author = or(shortText(opt(a, "fullName")), shortText(opt(a, "login")))
		}
		comments = append(comments, obj("author", or(author, "Без имени"), "at", timestamp(opt(c, "created")),
			"text", cleanTaskText(opt(c, "text"), taskCommentMax)))
	}
	return obj("key", opt(raw, "idReadable"), "title", title, "project", project, "type", nameOf("Type"), "state", nameOf("State"),
		"priority", nameOf("Priority"), "resolved", !nullish(opt(raw, "resolved")), "assignee", assignee, "parent", parentOut,
		"children", children, "description", cleanTaskText(opt(raw, "description"), taskDescMax), "comments", comments,
		"updated", timestamp(opt(raw, "updated")))
}

func (s *Service) loadTask(ctx context.Context, key string) taskResult {
	token := s.trackerToken()
	if token == "" {
		return taskResult{err: TaskNoToken}
	}
	resp, err := s.fetch(ctx, s.trackerOrigin()+"/api/issues/"+files.EncodeURIComponent(key)+"?fields="+files.EncodeURIComponent(taskFields),
		http.Header{"Accept": {"application/json"}, "Authorization": {"Bearer " + token}}, refuse, serviceTimeout, 0)
	if resp == nil {
		return taskResult{err: TaskTracker}
	}
	if resp.status == http.StatusNotFound {
		return taskResult{err: TaskNotFound}
	}
	if !resp.ok() || err != nil {
		return taskResult{err: TaskTracker}
	}
	text := decodeText(resp.body)
	if jscompat.Len(text) > taskMaxBytes {
		return taskResult{err: TaskTracker}
	}
	raw, perr := jsvalue.Parse([]byte(text))
	if perr != nil {
		return taskResult{err: TaskTracker}
	}
	avatarOf := func(login string, source any) any {
		s.rememberAvatar(login, source)
		return s.avatarPath(login)
	}
	if task := s.parseTask(raw, avatarOf); task != nil {
		return taskResult{task: task}
	}
	return taskResult{err: TaskTracker}
}

// Task is one tracker task: one request per task, kept a minute, shared while in flight; a failure is never kept.
// err is one of the Task* errors.
func (s *Service) Task(ctx context.Context, key string) (*jsvalue.Object, string) {
	st := s.tasks
	st.mu.Lock()
	if e, ok := st.entries[key]; ok && time.Since(e.at) < taskFresh {
		st.mu.Unlock()
		return e.task, ""
	}
	if call, ok := st.inflight[key]; ok {
		st.mu.Unlock()
		<-call.done
		return call.res.task, call.res.err
	}
	call := &taskCall{done: make(chan struct{})}
	st.inflight[key] = call
	st.mu.Unlock()
	call.res = s.loadTask(context.WithoutCancel(ctx), key)
	st.mu.Lock()
	if call.res.task != nil {
		if _, ok := st.entries[key]; ok {
			for i, k := range st.order {
				if k == key {
					st.order = append(st.order[:i:i], st.order[i+1:]...)
					break
				}
			}
		}
		st.entries[key] = taskEntry{task: call.res.task, at: time.Now()}
		st.order = append(st.order, key)
		if len(st.order) > taskEntries {
			delete(st.entries, st.order[0])
			st.order = st.order[1:]
		}
	}
	delete(st.inflight, key)
	st.mu.Unlock()
	close(call.done)
	return call.res.task, call.res.err
}
