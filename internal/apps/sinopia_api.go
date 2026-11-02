package apps

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/design"
)

// The Document API: one implementation that serves an app's view (hl.sinopia.*, over the bridge) and the agent
// (app.sinopia.* tools of the harnessly-app server). A call names the document it is about — a view's by its session,
// the agent's by the chat the editor shows it beside — and carries the app and the way it came; the permission of the
// method and the app's target for the document's kind are checked here, once, for both.

// SinopiaMethods are the methods of the Document API with the permission each needs.
var SinopiaMethods = map[string]string{
	"document":   "sinopia.read",
	"selection":  "sinopia.read",
	"node":       "sinopia.read",
	"query":      "sinopia.read",
	"fields":     "sinopia.read",
	"mark":       "sinopia.read",
	"patch":      "sinopia.write",
	"setFields":  "sinopia.write",
	"boards.add": "sinopia.write",
	"render":     "sinopia.export",
	"comment":    "sinopia.comment",
}

// SinopiaDoc is a document as document() answers.
type SinopiaDoc struct {
	ID     string              `json:"id"`
	Kind   string              `json:"kind"`
	Title  string              `json:"title"`
	Boards []design.DocPageRef `json:"boards"`
	Rev    int64               `json:"rev"`
}

// SinopiaDocs is the library's part the Document API stands on (cmd/runtime gives it over design.Library).
type SinopiaDocs interface {
	Document(project string) (SinopiaDoc, error)
	Nodes(project, page string) ([]design.DocNode, error)
	Query(project, selector, page string) ([]design.DocNode, error)
	Node(project, id string) (design.DocNode, error)
	Fields(project string) ([]design.DocField, error)
	Patch(project string, ops []design.DocOp, who design.DocActor) (design.DocPatched, error)
	AddBoard(project, title string, src []byte, who design.DocActor) (design.DocPageRef, error)
}

// SelectedNode is a node the person picked in the editor, as the page reports it.
type SelectedNode struct {
	ID          string            `json:"id,omitempty"`
	Page        string            `json:"page,omitempty"`
	Tpl         *int              `json:"tpl,omitempty"`
	Fingerprint string            `json:"fp,omitempty"`
	Selector    string            `json:"selector,omitempty"`
	Field       string            `json:"field,omitempty"`
	Tag         string            `json:"tag"`
	Text        string            `json:"text,omitempty"`
	Rect        *SelectedRect     `json:"rect,omitempty"`
	Hl          map[string]string `json:"hl,omitempty"`
}

// SelectedRect is where the node shows, in the board's pixels.
type SelectedRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Mark is a note on a node of a board: a finding of a check, an app's remark, a comment.
type Mark struct {
	Node   string `json:"node"`
	Level  string `json:"level"`
	Note   string `json:"note"`
	Source string `json:"source"`
	Rule   string `json:"rule,omitempty"`
	Fix    any    `json:"fix,omitempty"`
	At     int64  `json:"at"`
}

const (
	maxSelected     = 50
	maxMarksSource  = 200
	maxMarksProject = 1000
	maxNote         = 300
)

// SinopiaState is what the machine keeps of the open documents: the selection the page last reported, the marks, and
// which document each chat's editor shows. It is in memory: a restart empties it, and the page reports again.
type SinopiaState struct {
	Now func() time.Time

	mu    sync.Mutex
	sel   map[string]selection
	marks map[string][]Mark
	chats map[string]chatDoc
	rev   map[string]checked
	found map[string][]CheckResult
}

type selection struct {
	nodes []SelectedNode
	at    int64
}

type chatDoc struct {
	project string
	kind    string
	at      int64
}

// checked is the board revision the save checks last ran for, and whether they run now.
type checked struct {
	rev     int64
	running bool
}

// NewSinopiaState is an empty state.
func NewSinopiaState() *SinopiaState {
	return &SinopiaState{Now: time.Now, sel: map[string]selection{}, marks: map[string][]Mark{}, chats: map[string]chatDoc{}, rev: map[string]checked{}, found: map[string][]CheckResult{}}
}

// Open says the editor of a chat shows a document; the agent's calls find the document by it.
func (s *SinopiaState) Open(conv, project, kind string) {
	if conv == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chats[conv] = chatDoc{project: project, kind: kind, at: s.Now().UnixMilli()}
	if len(s.chats) > 500 {
		var oldest string
		var at int64 = math.MaxInt64
		for k, v := range s.chats {
			if v.at < at {
				oldest, at = k, v.at
			}
		}
		delete(s.chats, oldest)
	}
}

// DocumentOf is the document and kind a chat's editor shows; false when it shows none.
func (s *SinopiaState) DocumentOf(conv string) (project, kind string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.chats[conv]
	return c.project, c.kind, ok
}

// SetSelection keeps what the page reports as picked (an empty list clears it).
func (s *SinopiaState) SetSelection(project string, nodes []SelectedNode) {
	if len(nodes) > maxSelected {
		nodes = nodes[:maxSelected]
	}
	clipped := make([]SelectedNode, len(nodes))
	for i, n := range nodes {
		n.Text = clipText(n.Text, 200)
		n.Selector = clipText(n.Selector, 512)
		n.Tag = clipText(n.Tag, 32)
		n.Field = clipText(n.Field, 80)
		if len(n.Hl) > 20 {
			n.Hl = nil
		}
		clipped[i] = n
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(clipped) == 0 {
		delete(s.sel, project)
		return
	}
	s.sel[project] = selection{nodes: clipped, at: s.Now().UnixMilli()}
}

// Selection is the nodes last picked in a document.
func (s *SinopiaState) Selection(project string) []SelectedNode {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]SelectedNode{}, s.sel[project].nodes...)
}

func clipText(s string, max int) string {
	if r := []rune(s); len(r) > max {
		return string(r[:max])
	}
	return s
}

// Marks are the marks of a document, in the order they were put.
func (s *SinopiaState) Marks(project string) []Mark {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Mark{}, s.marks[project]...)
}

// AddMarks puts marks on a document; replace takes the earlier marks of the same source away first. Marks past the
// limits of a source and of a document are dropped.
func (s *SinopiaState) AddMarks(project, source string, replace bool, marks []Mark) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now().UnixMilli()
	kept := s.marks[project][:0:0]
	own := 0
	for _, m := range s.marks[project] {
		if m.Source == source {
			if replace {
				continue
			}
			own++
		}
		kept = append(kept, m)
	}
	for _, m := range marks {
		if own >= maxMarksSource || len(kept) >= maxMarksProject {
			break
		}
		m.Source, m.At = source, now
		m.Note = clipText(m.Note, maxNote)
		kept = append(kept, m)
		own++
	}
	if len(kept) == 0 {
		delete(s.marks, project)
		return
	}
	s.marks[project] = kept
}

// SetResults keeps the last run of the checks of a document: results of the same app and check replace the earlier.
func (s *SinopiaState) SetResults(project string, results []CheckResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.found[project][:0:0]
	for _, old := range s.found[project] {
		replaced := false
		for _, r := range results {
			replaced = replaced || (r.App == old.App && r.Check == old.Check)
		}
		if !replaced {
			kept = append(kept, old)
		}
	}
	s.found[project] = append(kept, results...)
}

// Results are the last results of the checks of a document.
func (s *SinopiaState) Results(project string) []CheckResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]CheckResult{}, s.found[project]...)
}

// ClaimCheck says a save check for a revision should run now: false when it already ran for it or runs.
func (s *SinopiaState) ClaimCheck(project string, rev int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.rev[project]
	if c.running || c.rev == rev {
		return false
	}
	s.rev[project] = checked{rev: rev, running: true}
	return true
}

// DoneCheck ends a save check; rev is the revision it looked at.
func (s *SinopiaState) DoneCheck(project string, rev int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rev[project] = checked{rev: rev}
}

// SinopiaCall is one call of the Document API.
type SinopiaCall struct {
	Docs  SinopiaDocs
	State *SinopiaState
	App   Installed
	// Pkg is the app's package (its templates and field schema); nil when it could not be read.
	Pkg     *Package
	Project string
	// Who is what the history names the writer: the app's name.
	Who string
	// Tab is the chat tab whose claims on a board do not stop a write, "" for the person's own page.
	Tab string
	// Agent says the call is the agent's, not a view's.
	Agent bool
}

// Call runs a method with its parameters and returns the result for JSON.
func (c SinopiaCall) Call(method string, p map[string]any) (any, error) {
	need, ok := SinopiaMethods[method]
	if !ok {
		return nil, ErrUnknownMethod
	}
	m := c.App.Manifest
	if m == nil {
		return nil, ErrNotInstalled
	}
	doc, err := c.Docs.Document(c.Project)
	if err != nil {
		return nil, sinopiaErr(err)
	}
	if !m.HasTarget("sinopia." + doc.Kind) {
		return nil, &BridgeError{Code: CodeNotInTarget, Message: "the app does not work in a " + doc.Kind + " document"}
	}
	if !m.Allows(need) {
		return nil, denied("%s needs the permission %s", method, need)
	}
	switch method {
	case "document":
		return doc, nil
	case "selection":
		return map[string]any{"document": c.Project, "nodes": c.State.Selection(c.Project)}, nil
	case "node":
		id, err := textParam(p, "id", 80)
		if err != nil {
			return nil, err
		}
		n, err := c.Docs.Node(c.Project, id)
		if err != nil {
			return nil, sinopiaErr(err)
		}
		return n, nil
	case "query":
		sel, err := textParam(p, "selector", 512)
		if err != nil {
			return nil, err
		}
		page, _ := p["page"].(string)
		nodes, err := c.Docs.Query(c.Project, sel, page)
		if err != nil {
			return nil, sinopiaErr(err)
		}
		const shown = 500
		out := map[string]any{"nodes": nodes, "count": len(nodes)}
		if len(nodes) > shown {
			out["nodes"], out["truncated"] = nodes[:shown], true
		}
		return out, nil
	case "fields":
		return c.fields()
	case "setFields":
		return c.setFields(p)
	case "patch":
		return c.patch(p)
	case "mark":
		return c.mark(p, false)
	case "comment":
		return c.mark(p, true)
	case "boards.add":
		return c.addBoard(p)
	case "render":
		return nil, &BridgeError{Code: CodeHostUnavailable, Message: "render is not available yet: the editor draws a board in the browser, and the host cannot export one on request"}
	}
	return nil, ErrUnknownMethod
}

func textParam(p map[string]any, key string, max int) (string, error) {
	s, ok := p[key].(string)
	if !ok || s == "" || len(s) > max {
		return "", &BridgeError{Code: CodeInvalidParams, Message: key + " is a text"}
	}
	return s, nil
}

// sinopiaErr turns a refusal of the library into the error a caller sees.
func sinopiaErr(err error) error {
	var de *design.DocError
	if !errors.As(err, &de) {
		return &BridgeError{Code: CodeHostUnavailable, Message: "the document could not be read or written", Detail: err.Error()}
	}
	code := CodeInvalidParams
	switch de.Code {
	case design.DocErrClaimed, design.DocErrLocked, design.DocErrChanged, design.DocErrDynamic:
		code = CodePolicyBlocked
	case design.DocErrFailed:
		code = CodeHostUnavailable
	}
	return &BridgeError{Code: code, Message: de.Msg}
}

func (c SinopiaCall) actor() design.DocActor { return design.DocActor{Subject: c.Who, Tab: c.Tab} }

func (c SinopiaCall) patch(p map[string]any) (any, error) {
	raw, ok := p["ops"].([]any)
	if !ok || len(raw) == 0 {
		return nil, &BridgeError{Code: CodeInvalidParams, Message: "ops is a list of changes"}
	}
	buf, err := json.Marshal(raw)
	if err != nil {
		return nil, &BridgeError{Code: CodeInvalidParams, Message: "ops is a list of changes"}
	}
	var ops []design.DocOp
	dec := json.NewDecoder(strings.NewReader(string(buf)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ops); err != nil {
		return nil, &BridgeError{Code: CodeInvalidParams, Message: "an op is {op, node, …}: " + err.Error()}
	}
	res, err := c.Docs.Patch(c.Project, ops, c.actor())
	if err != nil {
		return nil, sinopiaErr(err)
	}
	return res, nil
}

func (c SinopiaCall) fields() (any, error) {
	list, err := c.Docs.Fields(c.Project)
	if err != nil {
		return nil, sinopiaErr(err)
	}
	schema := c.schema()
	values := map[string]any{}
	for _, f := range list {
		values[f.Field] = schema.coerce(f.Field, f.Value)
	}
	return map[string]any{"fields": values, "bindings": list}, nil
}

func (c SinopiaCall) setFields(p map[string]any) (any, error) {
	patch, ok := p["patch"].(map[string]any)
	if !ok || len(patch) == 0 || len(patch) > 100 {
		return nil, &BridgeError{Code: CodeInvalidParams, Message: "patch is an object of fields"}
	}
	if err := c.schema().validate(patch); err != nil {
		return nil, &BridgeError{Code: CodeInvalidParams, Message: err.Error()}
	}
	names := make([]string, 0, len(patch))
	for name := range patch {
		names = append(names, name)
	}
	sort.Strings(names)
	ops := make([]design.DocOp, len(names))
	for i, name := range names {
		ops[i] = design.DocOp{Op: "setField", Field: name, Value: patch[name]}
	}
	res, err := c.Docs.Patch(c.Project, ops, c.actor())
	if err != nil {
		return nil, sinopiaErr(err)
	}
	return res, nil
}

func (c SinopiaCall) mark(p map[string]any, comment bool) (any, error) {
	var nodes []string
	var note string
	if comment {
		id, err := textParam(p, "node", 80)
		if err != nil {
			return nil, err
		}
		nodes = []string{id}
		if note, err = textParam(p, "text", 2000); err != nil {
			return nil, err
		}
	} else {
		list, _ := p["nodes"].([]any)
		for _, v := range list {
			s, ok := v.(string)
			if !ok {
				return nil, &BridgeError{Code: CodeInvalidParams, Message: "nodes are ids"}
			}
			nodes = append(nodes, s)
		}
		note, _ = p["note"].(string)
	}
	if len(nodes) > maxMarksSource {
		return nil, &BridgeError{Code: CodeInvalidParams, Message: "too many nodes"}
	}
	for _, id := range nodes {
		if _, _, ok := design.ParseDocNodeID(id); !ok {
			return nil, &BridgeError{Code: CodeInvalidParams, Message: "a node id is <page>:<number>"}
		}
	}
	level, _ := p["level"].(string)
	switch level {
	case "":
		level = LevelInfo
		if comment {
			level = "comment"
		}
	case LevelError, LevelWarning, LevelInfo:
	default:
		return nil, &BridgeError{Code: CodeInvalidParams, Message: "level is error, warning or info"}
	}
	replace, _ := p["replace"].(bool)
	if len(nodes) > 0 && strings.TrimSpace(note) == "" {
		return nil, &BridgeError{Code: CodeInvalidParams, Message: "a mark has a note"}
	}
	source := "app:" + c.App.ID
	if comment {
		source = "comment:" + c.App.ID
		replace = false
	}
	marks := make([]Mark, len(nodes))
	for i, id := range nodes {
		marks[i] = Mark{Node: id, Level: level, Note: note}
	}
	c.State.AddMarks(c.Project, source, replace, marks)
	return map[string]any{"marks": len(marks)}, nil
}

func (c SinopiaCall) addBoard(p map[string]any) (any, error) {
	name, err := textParam(p, "template", 200)
	if err != nil {
		return nil, err
	}
	if c.Pkg == nil {
		return nil, &BridgeError{Code: CodeHostUnavailable, Message: "the app's package could not be read"}
	}
	sin, err := c.App.Manifest.SinopiaOf()
	if err != nil {
		return nil, &BridgeError{Code: CodeHostUnavailable, Message: "the app's manifest could not be read"}
	}
	dir := ""
	for _, t := range sin.Templates {
		t = strings.TrimSuffix(t, "/")
		if t == strings.TrimSuffix(name, "/") || t[strings.LastIndex(t, "/")+1:] == name {
			dir = t
		}
	}
	if dir == "" {
		return nil, &BridgeError{Code: CodeInvalidParams, Message: "the app has no template " + name}
	}
	src, err := c.Pkg.File(dir + "/" + TemplateBoard)
	if err != nil {
		return nil, &BridgeError{Code: CodeHostUnavailable, Message: "the template could not be read"}
	}
	data, _ := p["data"].(map[string]any)
	if len(data) > 0 {
		if err := c.schema().validate(data); err != nil {
			return nil, &BridgeError{Code: CodeInvalidParams, Message: err.Error()}
		}
	}
	title, _ := p["title"].(string)
	if strings.TrimSpace(title) == "" {
		title = c.App.Manifest.Name
	}
	page, err := c.Docs.AddBoard(c.Project, title, src, c.actor())
	if err != nil {
		return nil, sinopiaErr(err)
	}
	// The data goes into the new board's bound elements only: another board may bind the same field.
	if len(data) > 0 {
		bound, err := c.Docs.Query(c.Project, "[data-hl-field]", page.ID)
		if err != nil {
			return nil, sinopiaErr(err)
		}
		var ops []design.DocOp
		for _, n := range bound {
			v, has := data[n.Field]
			if !has {
				continue
			}
			if attr := n.Attrs["data-hl-attr"]; attr != "" {
				ops = append(ops, design.DocOp{Op: "setAttr", Node: n.ID, Name: attr, Value: v})
			} else {
				ops = append(ops, design.DocOp{Op: "setText", Node: n.ID, Value: v})
			}
		}
		if len(ops) > 0 {
			if _, err := c.Docs.Patch(c.Project, ops, c.actor()); err != nil {
				return nil, sinopiaErr(err)
			}
		}
	}
	return map[string]any{"board": page}, nil
}

// TemplateBoard is the board file inside a template folder.
const TemplateBoard = "board.dc.html"

// fieldSchema is the part of a JSON Schema the host checks a field's value against: type, enum, minimum, maximum,
// minLength, maxLength, pattern, and which fields exist.
type fieldSchema struct {
	props map[string]fieldRule
}

type fieldRule struct {
	Type      string   `json:"type"`
	Enum      []any    `json:"enum"`
	Minimum   *float64 `json:"minimum"`
	Maximum   *float64 `json:"maximum"`
	MinLength *int     `json:"minLength"`
	MaxLength *int     `json:"maxLength"`
	Pattern   string   `json:"pattern"`
}

// schema is the field schema of the app's bindings; with none, a value is any text, number or switch.
func (c SinopiaCall) schema() fieldSchema {
	if c.Pkg == nil {
		return fieldSchema{}
	}
	sin, err := c.App.Manifest.SinopiaOf()
	if err != nil || sin.Bindings == nil || sin.Bindings.Schema == "" {
		return fieldSchema{}
	}
	raw, err := c.Pkg.File(sin.Bindings.Schema)
	if err != nil {
		return fieldSchema{}
	}
	var doc struct {
		Properties map[string]fieldRule `json:"properties"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return fieldSchema{}
	}
	return fieldSchema{props: doc.Properties}
}

func (s fieldSchema) validate(patch map[string]any) error {
	for name, v := range patch {
		if s.props == nil {
			switch v.(type) {
			case string, float64, bool:
				continue
			}
			return fmt.Errorf("the field %s is a text, a number or a switch", name)
		}
		r, ok := s.props[name]
		if !ok {
			return fmt.Errorf("the field %s is not in the app's schema", name)
		}
		if err := r.check(name, v); err != nil {
			return err
		}
	}
	return nil
}

func (r fieldRule) check(name string, v any) error {
	switch r.Type {
	case "string":
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("the field %s is a text", name)
		}
		n := len([]rune(s))
		if r.MinLength != nil && n < *r.MinLength || r.MaxLength != nil && n > *r.MaxLength {
			return fmt.Errorf("the field %s is not the right length", name)
		}
		if r.Pattern != "" {
			if re, err := regexp.Compile(r.Pattern); err == nil && !re.MatchString(s) {
				return fmt.Errorf("the field %s is not in the form the schema asks", name)
			}
		}
	case "number", "integer":
		f, ok := v.(float64)
		if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("the field %s is a number", name)
		}
		if r.Type == "integer" && f != math.Trunc(f) {
			return fmt.Errorf("the field %s is a whole number", name)
		}
		if r.Minimum != nil && f < *r.Minimum || r.Maximum != nil && f > *r.Maximum {
			return fmt.Errorf("the field %s is out of range", name)
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("the field %s is a switch", name)
		}
	}
	if len(r.Enum) > 0 {
		for _, e := range r.Enum {
			if e == v {
				return nil
			}
		}
		return fmt.Errorf("the field %s is not one of the allowed values", name)
	}
	return nil
}

// coerce reads a field's text as the schema says it is: a number, a switch.
func (s fieldSchema) coerce(name, text string) any {
	r, ok := s.props[name]
	if !ok {
		return text
	}
	switch r.Type {
	case "number", "integer":
		if f, err := strconv.ParseFloat(strings.TrimSpace(text), 64); err == nil && !math.IsNaN(f) {
			return f
		}
	case "boolean":
		if b, err := strconv.ParseBool(strings.TrimSpace(text)); err == nil {
			return b
		}
	}
	return text
}
