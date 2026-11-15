// Package quickedit makes the voice's small edits fast: «сделай заголовок крупнее» on the open board, slide
// or file goes not to the chat's agent, which takes a turn of minutes, but to one short run of a fast model whose whole
// context is the file, what is picked in it and the words said. The run may only read and edit that file. The last
// edit of each file is kept so that «верни» puts it back.
package quickedit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Request is one spoken edit.
type Request struct {
	// File is the absolute path of what is open: a board, a slide, a file of the right pane.
	File string `json:"file"`
	// Selection is what the person picked in it (an element's HTML, a text range); "" for none.
	Selection string `json:"selection,omitempty"`
	// Ask is what the person said.
	Ask string `json:"ask"`
	// Chat is the chat the voice speaks in: a board another chat holds is not edited.
	Chat string `json:"chat,omitempty"`
}

// Result is how the edit went.
type Result struct {
	// Said is the model's one line on what it changed.
	Said string `json:"said"`
	// Changed says the file is not what it was.
	Changed bool `json:"changed"`
	// Ms is how long the run took, from the ask to the file written.
	Ms int64 `json:"ms"`
	// Hint is the changed lines as they are now: the page finds the block they make and flashes it.
	Hint string `json:"hint,omitempty"`
}

// Error is a refusal with the code the page shows.
type Error struct {
	Code   string `json:"error"`
	Status int    `json:"-"`
	Detail string `json:"detail,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Detail }

const (
	CodeBadRequest = "edit_bad_request"
	CodeNotText    = "edit_not_text"
	CodeOutside    = "edit_outside"
	CodeTaken      = "edit_taken"
	CodeFailed     = "edit_failed"
	CodeNothing    = "edit_nothing_to_undo"
)

// Runner runs an agent once on file with the prompt, allowed to read and edit that file only; it answers the agent's
// last words.
type Runner func(ctx context.Context, file, prompt string) (string, error)

// Planner asks a model, without tools, for the edit as replacements (PlanPrompt): no CLI to start, so it takes
// seconds. It answers the model's words.
type Planner func(ctx context.Context, prompt string) (string, error)

// Config sets a Service up.
type Config struct {
	// Roots are the folders an edit may touch (the design library, the person's home); a hidden path (a segment that
	// starts with a dot) and anything under Exclude never are.
	Roots   []string
	Exclude []string
	// Plans are the fast ways, asked at once: the first answer that applies is the edit (race); Run, an agent with
	// Read and Edit, is taken when none does.
	Plans []Planner
	// PlanNames name the Plans in Step ("claude", "chatgpt"); a missing name is "plan<i>".
	PlanNames []string
	Run       Runner
	// Step hears each way tried with how long it took and how it went (the runtime's journal).
	Step func(step string, took time.Duration, ok bool, note string)
	// Holder names who else holds the file (another chat's claim of a board); "" when nobody does.
	Holder func(file, chat string) string
	// Timeout bounds one run; 90 s when zero.
	Timeout time.Duration
	Now     func() time.Time
}

// Service makes the edits and keeps the last one of each file for «верни».
type Service struct {
	cfg  Config
	mu   sync.Mutex
	last map[string]undo
	// one run at a time per file: two edits said one after another go in that order.
	files map[string]*sync.Mutex
	// obj keeps the app objects' edits waiting for «да» and their undo (object.go).
	obj *objects
}

type undo struct{ before, after []byte }

const (
	fileMax   = 1 << 20
	inlineMax = 160 << 10
	askMax    = 2000
	pickMax   = 8000
)

func New(cfg Config) *Service {
	if cfg.Timeout == 0 {
		cfg.Timeout = 90 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Service{cfg: cfg, last: map[string]undo{}, files: map[string]*sync.Mutex{}}
}

// within says the path lies in one of the roots, symlinks resolved.
func (s *Service) within(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", &Error{Code: CodeBadRequest, Status: http.StatusBadRequest, Detail: "the file is named by its full path"}
	}
	real, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", &Error{Code: CodeBadRequest, Status: http.StatusNotFound, Detail: "no such file"}
	}
	for _, root := range s.cfg.Roots {
		r, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		if rel, err := filepath.Rel(r, real); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			if hidden(rel) || s.excluded(real) {
				return "", &Error{Code: CodeOutside, Status: http.StatusForbidden, Detail: "the voice does not edit this file"}
			}
			info, err := os.Stat(real)
			if err != nil || !info.Mode().IsRegular() || info.Size() > fileMax {
				return "", &Error{Code: CodeBadRequest, Status: http.StatusBadRequest, Detail: "not a file it edits"}
			}
			return real, nil
		}
	}
	return "", &Error{Code: CodeOutside, Status: http.StatusForbidden, Detail: "the file is outside the places the voice edits"}
}

// hidden says a path inside a root goes through a dot-folder or is a dot-file (settings, keys, the runtime's data).
func hidden(rel string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

func (s *Service) excluded(real string) bool {
	for _, x := range s.cfg.Exclude {
		r, err := filepath.EvalSymlinks(x)
		if err != nil {
			r = filepath.Clean(x)
		}
		if rel, err := filepath.Rel(r, real); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			return true
		}
	}
	return false
}

func (s *Service) lockOf(file string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.files[file]
	if m == nil {
		m = &sync.Mutex{}
		s.files[file] = m
	}
	return m
}

// Prompt is what the fast model is told: the one file, what is picked, the words; nothing else of the machine.
func Prompt(file string, content []byte, selection, ask string) string {
	var b strings.Builder
	b.WriteString("You make one small edit, fast, to one file, as a person asked out loud while looking at it.\n")
	b.WriteString("Edit only " + file + ", with the Edit tool, in place. Change exactly what was asked and nothing else: ")
	b.WriteString("keep the structure, the scripts, the styles you were not asked about. Do not run anything, do not ")
	b.WriteString("create files. Words like \"this\", \"here\", \"it\" mean the picked element when there is one.\n")
	b.WriteString("When done, answer with one short sentence in the person's language saying what you changed ")
	b.WriteString("(for example: «Заголовок стал крупнее»). If the ask is not an edit of this file, change nothing and say so.\n\n")
	b.WriteString("The person said: «" + ask + "»\n")
	if selection != "" {
		b.WriteString("\nPicked in the file:\n" + selection + "\n")
	}
	if len(content) <= inlineMax {
		b.WriteString("\nThe file as it is now (no need to Read it):\n<file>\n")
		b.Write(content)
		b.WriteString("\n</file>\n")
	}
	return b.String()
}

// Edit runs the fast model on the file and says how it went.
func (s *Service) Edit(ctx context.Context, req Request) (Result, error) {
	ask := strings.TrimSpace(req.Ask)
	if ask == "" || len(ask) > askMax || len(req.Selection) > pickMax {
		return Result{}, &Error{Code: CodeBadRequest, Status: http.StatusBadRequest, Detail: "say what to change"}
	}
	file, err := s.within(req.File)
	if err != nil {
		return Result{}, err
	}
	if s.cfg.Holder != nil {
		if who := s.cfg.Holder(file, req.Chat); who != "" {
			return Result{}, &Error{Code: CodeTaken, Status: http.StatusConflict, Detail: who}
		}
	}
	m := s.lockOf(file)
	m.Lock()
	defer m.Unlock()
	before, err := os.ReadFile(file)
	if err != nil {
		return Result{}, err
	}
	// A picture, a binary: not something a model edits by its words.
	if !utf8.Valid(before) || bytes.IndexByte(before, 0) >= 0 {
		return Result{}, &Error{Code: CodeNotText, Status: http.StatusUnprocessableEntity, Detail: "not a text file"}
	}
	start := s.cfg.Now()
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	if len(before) <= inlineMax {
		if res, ok := s.race(ctx, file, before, req.Selection, ask, start); ok {
			return res, nil
		}
	}
	if s.cfg.Run == nil {
		return Result{}, &Error{Code: CodeFailed, Status: http.StatusBadGateway, Detail: "no model answered"}
	}
	at := s.cfg.Now()
	said, runErr := s.cfg.Run(ctx, file, Prompt(file, before, req.Selection, ask))
	s.step("agent", s.cfg.Now().Sub(at), runErr == nil, lastLine(said))
	after, err := os.ReadFile(file)
	if err != nil {
		return Result{}, err
	}
	res := Result{Said: lastLine(said), Changed: !bytes.Equal(before, after), Ms: s.cfg.Now().Sub(start).Milliseconds()}
	if res.Changed {
		res.Hint = hintOf(before, after)
		s.mu.Lock()
		s.last[file] = undo{before: before, after: after}
		s.mu.Unlock()
	}
	if runErr != nil && !res.Changed {
		detail := runErr.Error()
		if errors.Is(runErr, context.DeadlineExceeded) {
			detail = "the edit took too long"
		}
		return res, &Error{Code: CodeFailed, Status: http.StatusBadGateway, Detail: clip(detail, 300)}
	}
	return res, nil
}

// PlanPrompt asks for the edit as exact replacements, which the runtime applies itself.
func PlanPrompt(content []byte, selection, ask string) string {
	var b strings.Builder
	b.WriteString("A person looks at the file below and asked out loud for one small change. Make exactly that change ")
	b.WriteString("and nothing else. Words like \"this\", \"here\", \"it\" mean the picked part when there is one.\n")
	b.WriteString("Answer with JSON only, no prose and no code fence: {\"said\": one short sentence in the person's ")
	b.WriteString("language on what changed, \"edits\": [{\"old\": an exact piece of the file that occurs once, short but ")
	b.WriteString("unique, \"new\": what replaces it}]}. If the ask is not an edit of this file: {\"said\": why, \"edits\": []}.\n\n")
	b.WriteString("The person said: «" + ask + "»\n")
	if selection != "" {
		b.WriteString("\nPicked in the file:\n" + selection + "\n")
	}
	if part, ok := windowOf(content, selection); ok {
		b.WriteString("\nThe part of the file around the picked element (the rest is not shown; \"old\" must come from here):\n<file-part>\n")
		b.Write(part)
		b.WriteString("\n</file-part>\n")
		return b.String()
	}
	b.WriteString("\n<file>\n")
	b.Write(content)
	b.WriteString("\n</file>\n")
	return b.String()
}

// A big file is shown to the model only around the picked element, when it is found: a smaller ask answers faster.
const (
	windowFrom = 24 << 10
	windowSide = 6 << 10
)

// windowOf is the piece of content around where the selection stands, cut at line ends; false when the file is small
// or the selection is not found in it.
func windowOf(content []byte, selection string) ([]byte, bool) {
	if len(content) <= windowFrom {
		return nil, false
	}
	at := -1
	for _, line := range strings.Split(selection, "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 12 {
			continue
		}
		if i := bytes.Index(content, []byte(line)); i >= 0 {
			at = i
			break
		}
	}
	if at < 0 {
		return nil, false
	}
	from := max(0, at-windowSide)
	if i := bytes.LastIndexByte(content[:from], '\n'); i >= 0 && from > 0 {
		from = i + 1
	}
	to := min(len(content), at+len(selection)+windowSide)
	if i := bytes.IndexByte(content[to:], '\n'); i >= 0 {
		to += i
	}
	return content[from:to], true
}

type plan struct {
	Said  string `json:"said"`
	Edits []struct {
		Old string `json:"old"`
		New string `json:"new"`
	} `json:"edits"`
}

// Apply makes the replacements of a plan on content; false when one of them does not stand exactly once.
func Apply(content []byte, answer string) ([]byte, string, bool) {
	answer = strings.TrimSpace(answer)
	answer = strings.TrimPrefix(strings.TrimPrefix(answer, "```json"), "```")
	answer = strings.TrimSuffix(strings.TrimSpace(answer), "```")
	var p plan
	if json.Unmarshal([]byte(answer), &p) != nil {
		return nil, "", false
	}
	out := string(content)
	for _, e := range p.Edits {
		if e.Old == "" || strings.Count(out, e.Old) != 1 {
			return nil, "", false
		}
		out = strings.Replace(out, e.Old, e.New, 1)
	}
	return []byte(out), clip(strings.TrimSpace(p.Said), 300), true
}

// race asks every Plan at once and applies the first answer that applies (the owner's decision 15.11). The
// others are not cancelled but let finish and only measured, as Step "<name> lost", so the journal compares the ways
// on real edits. The file is written once, by the winner.
func (s *Service) race(ctx context.Context, file string, before []byte, selection, ask string, start time.Time) (Result, bool) {
	type answer struct {
		name  string
		took  time.Duration
		after []byte
		note  string
		ok    bool
	}
	prompt := PlanPrompt(before, selection, ask)
	rest, stop := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.Timeout)
	got := make(chan answer, len(s.cfg.Plans))
	n := 0
	for i, p := range s.cfg.Plans {
		if p == nil {
			continue
		}
		n++
		go func() {
			at := s.cfg.Now()
			out, err := p(rest, prompt)
			a := answer{name: s.planName(i), note: ""}
			if err != nil {
				a.note = clip(err.Error(), 200)
			} else if a.after, a.note, a.ok = Apply(before, out); !a.ok {
				a.note = "the answer does not apply"
			}
			a.took = s.cfg.Now().Sub(at)
			got <- a
		}()
	}
	// The answers after the winner (or after the edit gave up) are only journaled.
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
			return Result{}, false
		}
		if a.ok {
			if res, ok := s.write(file, before, a.after, a.note, start); ok {
				s.step(a.name, a.took, true, a.note)
				drain(left - 1)
				return res, true
			}
			a.ok, a.note = false, "could not write the file"
		}
		s.step(a.name, a.took, false, a.note)
	}
	stop()
	return Result{}, false
}

// write puts the winning answer into the file and keeps the undo.
func (s *Service) write(file string, before, after []byte, said string, start time.Time) (Result, bool) {
	res := Result{Said: said, Changed: !bytes.Equal(before, after)}
	if res.Changed {
		res.Hint = hintOf(before, after)
		info, err := os.Stat(file)
		if err != nil || os.WriteFile(file, after, info.Mode().Perm()) != nil {
			return Result{}, false
		}
		s.mu.Lock()
		s.last[file] = undo{before: before, after: after}
		s.mu.Unlock()
	}
	res.Ms = s.cfg.Now().Sub(start).Milliseconds()
	return res, true
}

// Undo puts the file back as it was before its last edit, if nothing changed it since.
func (s *Service) Undo(path string) (Result, error) {
	file, err := s.within(path)
	if err != nil {
		return Result{}, err
	}
	m := s.lockOf(file)
	m.Lock()
	defer m.Unlock()
	s.mu.Lock()
	u, ok := s.last[file]
	s.mu.Unlock()
	if !ok {
		return Result{}, &Error{Code: CodeNothing, Status: http.StatusConflict}
	}
	now, err := os.ReadFile(file)
	if err != nil {
		return Result{}, err
	}
	if !bytes.Equal(now, u.after) {
		return Result{}, &Error{Code: CodeNothing, Status: http.StatusConflict, Detail: "the file changed since"}
	}
	info, err := os.Stat(file)
	if err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(file, u.before, info.Mode().Perm()); err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	delete(s.last, file)
	s.mu.Unlock()
	return Result{Changed: true}, nil
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[i+1:])
	}
	return clip(s, 300)
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut] + "…"
}

// Describe is a refusal as the page reads it.
func Describe(err error) (int, any) {
	var e *Error
	if errors.As(err, &e) {
		return e.Status, e
	}
	return http.StatusInternalServerError, &Error{Code: CodeFailed, Detail: fmt.Sprint(clip(err.Error(), 300))}
}

func (s *Service) step(name string, took time.Duration, ok bool, note string) {
	if s.cfg.Step != nil {
		s.cfg.Step(name, took, ok, note)
	}
}

func (s *Service) planName(i int) string {
	if i < len(s.cfg.PlanNames) && s.cfg.PlanNames[i] != "" {
		return s.cfg.PlanNames[i]
	}
	return "plan" + strconv.Itoa(i)
}

const hintMax = 400

// hintOf is the lines of after that differ from before (the common head and tail cut away), trimmed: what the page
// looks for to flash the block that changed.
func hintOf(before, after []byte) string {
	head := 0
	for head < len(before) && head < len(after) && before[head] == after[head] {
		head++
	}
	tail := 0
	for tail < len(before)-head && tail < len(after)-head && before[len(before)-1-tail] == after[len(after)-1-tail] {
		tail++
	}
	from := bytes.LastIndexByte(after[:head], '\n') + 1
	to := len(after) - tail
	if i := bytes.IndexByte(after[to:], '\n'); i >= 0 {
		to += i
	} else {
		to = len(after)
	}
	if to <= from {
		return ""
	}
	return clip(strings.TrimSpace(string(after[from:to])), hintMax)
}
