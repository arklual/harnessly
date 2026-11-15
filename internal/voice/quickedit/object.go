package quickedit

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// An app's object (a board, a document) takes a quick edit through the app's own tools (manifest quickEdit):
// the app reads the object as text, the fast models plan replacements on that text, the app applies them. The runtime
// never writes the object itself; the app keeps its rules (rights, versions, what a line means).

// Object is one app object as the app's quickEdit tools give it.
type Object interface {
	// Read is the object as text and the version of that text.
	Read(ctx context.Context, selection string) (text, version string, err error)
	// Apply makes the replacements on the version; confirmed says the person said «да» to removing.
	Apply(ctx context.Context, version string, edits []Edit, confirmed bool) (Applied, error)
	// Undo puts back the apply that answered version.
	Undo(ctx context.Context, version string) error
}

// Edit is one exact replacement in the object's text.
type Edit struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// Applied is the app's answer to Apply.
type Applied struct {
	Changed Changed  `json:"changed"`
	Version string   `json:"version"`
	Hint    string   `json:"hint"`
	IDs     []string `json:"ids"`
}

// Changed is how many edits landed; an app may answer it as a count or as true/false.
type Changed int

func (c *Changed) UnmarshalJSON(b []byte) error {
	switch string(b) {
	case "true":
		*c = 1
	case "false", "null":
		*c = 0
	default:
		var n int
		if err := json.Unmarshal(b, &n); err != nil {
			return err
		}
		*c = Changed(n)
	}
	return nil
}

// AppError is a refusal of an app's quickEdit tool: {"code", "message"}.
type AppError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *AppError) Error() string { return e.Code + ": " + e.Message }

// CodeNeedsYes asks the page for the person's «да»: the edit removes something. The same request with confirm: true
// applies the edit planned before, without asking the models again.
const CodeNeedsYes = "edit_needs_yes"

// ObjectRequest is a spoken edit of an app's object.
type ObjectRequest struct {
	App       string `json:"app"`
	Object    string `json:"object"`
	Selection string `json:"selection,omitempty"`
	Ask       string `json:"ask"`
	// Confirm is the person's «да» to the edit the last answer asked about.
	Confirm bool `json:"confirm,omitempty"`
}

// ObjectResult is how the edit of an object went.
type ObjectResult struct {
	Said    string   `json:"said"`
	Changed bool     `json:"changed"`
	Ms      int64    `json:"ms"`
	Hint    string   `json:"hint,omitempty"`
	IDs     []string `json:"ids,omitempty"`
}

type pendingEdit struct {
	version string
	edits   []Edit
	said    string
	at      time.Time
}

// objects keeps per app object the edit waiting for «да» and the version to undo.
type objects struct {
	mu      sync.Mutex
	pending map[string]pendingEdit
	undo    map[string]string
	locks   map[string]*sync.Mutex
}

// pendingFor is how long a planned removal waits for «да».
const pendingFor = 2 * time.Minute

func objectKey(app, object string) string { return app + "\x00" + object }

func (s *Service) objs() *objects {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.obj == nil {
		s.obj = &objects{pending: map[string]pendingEdit{}, undo: map[string]string{}, locks: map[string]*sync.Mutex{}}
	}
	return s.obj
}

func (o *objects) lock(key string) *sync.Mutex {
	o.mu.Lock()
	defer o.mu.Unlock()
	m := o.locks[key]
	if m == nil {
		m = &sync.Mutex{}
		o.locks[key] = m
	}
	return m
}

// EditObject plans the spoken edit on the object's text with the fast models and has the app apply it.
func (s *Service) EditObject(ctx context.Context, req ObjectRequest, obj Object) (ObjectResult, error) {
	ask := strings.TrimSpace(req.Ask)
	if req.App == "" || req.Object == "" || len(req.Selection) > pickMax || len(ask) > askMax || (ask == "" && !req.Confirm) {
		return ObjectResult{}, &Error{Code: CodeBadRequest, Status: http.StatusBadRequest, Detail: "say what to change"}
	}
	o := s.objs()
	key := objectKey(req.App, req.Object)
	m := o.lock(key)
	m.Lock()
	defer m.Unlock()
	start := s.cfg.Now()
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	if req.Confirm {
		o.mu.Lock()
		p, ok := o.pending[key]
		delete(o.pending, key)
		o.mu.Unlock()
		if !ok || s.cfg.Now().Sub(p.at) > pendingFor {
			return ObjectResult{}, &Error{Code: CodeNothing, Status: http.StatusConflict, Detail: "nothing waits for yes"}
		}
		return s.applyObject(ctx, key, obj, p, true, start)
	}
	at := s.cfg.Now()
	text, version, err := obj.Read(ctx, req.Selection)
	s.step("app read", s.cfg.Now().Sub(at), err == nil, "")
	if err != nil {
		return ObjectResult{}, appRefusal(err)
	}
	if len(text) > inlineMax {
		return ObjectResult{}, &Error{Code: CodeNotText, Status: http.StatusUnprocessableEntity, Detail: "the object is too big for a quick edit"}
	}
	p, ok := s.planObject(ctx, []byte(text), req.Selection, ask)
	if !ok {
		return ObjectResult{}, &Error{Code: CodeFailed, Status: http.StatusBadGateway, Detail: "no model answered with an edit"}
	}
	if len(p.edits) == 0 {
		return ObjectResult{Said: p.said, Ms: s.cfg.Now().Sub(start).Milliseconds()}, nil
	}
	p.version = version
	return s.applyObject(ctx, key, obj, p, false, start)
}

func (s *Service) applyObject(ctx context.Context, key string, obj Object, p pendingEdit, confirmed bool, start time.Time) (ObjectResult, error) {
	o := s.objs()
	at := s.cfg.Now()
	got, err := obj.Apply(ctx, p.version, p.edits, confirmed)
	s.step("app apply", s.cfg.Now().Sub(at), err == nil, p.said)
	if err != nil {
		if ae, ok := err.(*AppError); ok && ae.Code == "needs_yes" {
			p.at = s.cfg.Now()
			o.mu.Lock()
			o.pending[key] = p
			o.mu.Unlock()
			return ObjectResult{}, &Error{Code: CodeNeedsYes, Status: http.StatusConflict, Detail: p.said}
		}
		return ObjectResult{}, appRefusal(err)
	}
	if got.Changed > 0 && got.Version != "" {
		o.mu.Lock()
		o.undo[key] = got.Version
		o.mu.Unlock()
	}
	return ObjectResult{Said: p.said, Changed: got.Changed > 0, Ms: s.cfg.Now().Sub(start).Milliseconds(),
		Hint: clip(got.Hint, hintMax), IDs: got.IDs}, nil
}

// UndoObject puts back the last quick edit of the object.
func (s *Service) UndoObject(ctx context.Context, app, object string, obj Object) (ObjectResult, error) {
	o := s.objs()
	key := objectKey(app, object)
	m := o.lock(key)
	m.Lock()
	defer m.Unlock()
	o.mu.Lock()
	version, ok := o.undo[key]
	o.mu.Unlock()
	if !ok {
		return ObjectResult{}, &Error{Code: CodeNothing, Status: http.StatusConflict}
	}
	if err := obj.Undo(ctx, version); err != nil {
		return ObjectResult{}, appRefusal(err)
	}
	o.mu.Lock()
	delete(o.undo, key)
	o.mu.Unlock()
	return ObjectResult{Changed: true}, nil
}

// planObject asks every Plan at once and takes the first answer whose replacements stand exactly once in the text.
func (s *Service) planObject(ctx context.Context, text []byte, selection, ask string) (pendingEdit, bool) {
	type answer struct {
		name string
		took time.Duration
		p    pendingEdit
		note string
		ok   bool
	}
	prompt := PlanPrompt(text, selection, ask)
	rest, stop := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.Timeout)
	got := make(chan answer, len(s.cfg.Plans))
	n := 0
	for i, plan := range s.cfg.Plans {
		if plan == nil {
			continue
		}
		n++
		go func() {
			at := s.cfg.Now()
			out, err := plan(rest, prompt)
			a := answer{name: s.planName(i)}
			if err != nil {
				a.note = clip(err.Error(), 200)
			} else if a.p, a.ok = parsePlan(text, out); !a.ok {
				a.note = "the answer does not apply"
			} else {
				a.note = a.p.said
			}
			a.took = s.cfg.Now().Sub(at)
			got <- a
		}()
	}
	drain := func(left int) {
		go func() {
			defer stop()
			for range left {
				a := <-got
				s.step(a.name+" lost", a.took, a.ok, a.note)
			}
		}()
	}
	for left := n; left > 0; left-- {
		var a answer
		select {
		case a = <-got:
		case <-ctx.Done():
			drain(left)
			return pendingEdit{}, false
		}
		s.step(a.name, a.took, a.ok, a.note)
		if a.ok {
			drain(left - 1)
			return a.p, true
		}
	}
	stop()
	return pendingEdit{}, false
}

// parsePlan reads a model's answer as replacements that each stand exactly once in text.
func parsePlan(text []byte, answer string) (pendingEdit, bool) {
	answer = strings.TrimSpace(answer)
	answer = strings.TrimPrefix(strings.TrimPrefix(answer, "```json"), "```")
	answer = strings.TrimSuffix(strings.TrimSpace(answer), "```")
	var p plan
	if json.Unmarshal([]byte(answer), &p) != nil {
		return pendingEdit{}, false
	}
	out := string(text)
	edits := make([]Edit, 0, len(p.Edits))
	for _, e := range p.Edits {
		if e.Old == "" || strings.Count(out, e.Old) != 1 {
			return pendingEdit{}, false
		}
		out = strings.Replace(out, e.Old, e.New, 1)
		edits = append(edits, Edit{Old: e.Old, New: e.New})
	}
	return pendingEdit{edits: edits, said: clip(strings.TrimSpace(p.Said), 300)}, true
}

// appRefusal is an app's refusal as the page reads it: the app's code kept, a conflict as 409.
func appRefusal(err error) error {
	ae, ok := err.(*AppError)
	if !ok {
		return &Error{Code: CodeFailed, Status: http.StatusBadGateway, Detail: clip(err.Error(), 300)}
	}
	status := http.StatusUnprocessableEntity
	switch ae.Code {
	case "conflict", "nothing_to_undo":
		status = http.StatusConflict
	case "forbidden":
		status = http.StatusForbidden
	}
	return &Error{Code: "edit_" + ae.Code, Status: status, Detail: clip(ae.Message, 300)}
}
