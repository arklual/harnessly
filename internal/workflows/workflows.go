// Package workflows reads the dynamic workflows of a Claude Code conversation: a script the agent hands to its Workflow
// tool, which runs in the background and starts many subagents phase by phase. Claude Code keeps a run beside the
// conversation's own transcript:
//
//	<session>/workflows/scripts/<name>-<run>.js         the script; its `meta` names the workflow and its phases
//	<session>/subagents/workflows/<run>/journal.jsonl   one line per agent started, returned or failed
//	<session>/subagents/workflows/<run>/agent-<id>.jsonl and agent-<id>.meta.json, the agent's own transcript
//	<session>/workflows/<run>.json                      the run's final state, written once it is over
//
// The journal is read as it grows; the agents' files are read when they change.
package workflows

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/sessionmeta"
)

// States of a workflow's agent.
const (
	Running = "running"
	Done    = "done"
	Failed  = "failed"
	Stopped = "stopped"
)

// Agent is one agent a workflow started. Times are epoch milliseconds, 0 when unknown.
type Agent struct {
	ID        string
	Label     string
	Phase     string // the title of the phase it was started in
	Model     string
	State     string
	StartedAt float64
	EndedAt   float64
	Last      string // its last step, one line
	Result    string // the first words of what it returned
}

// Phase is a stage of the workflow, as its script names it, with the agents started in it so far.
type Phase struct {
	Title  string
	Agents []Agent
}

// View is a run as its files tell it.
type View struct {
	Name        string
	Description string
	// Status is the final state's ("completed", "failed", "killed"), "" while the run has written none.
	Status    string
	StartedAt float64
	EndedAt   float64
	Phases    []Phase
}

// Over says whether the run's final state was written.
func (v View) Over() bool { return v.Status != "" }

// Settle is the view of a run that ended with status: the agents it left at work were stopped with it.
func (v View) Settle(status string) View {
	if v.Status == "" {
		v.Status = status
	}
	phases := make([]Phase, len(v.Phases))
	for i, p := range v.Phases {
		agents := make([]Agent, len(p.Agents))
		for k, a := range p.Agents {
			if a.State == Running {
				a.State = Stopped
			}
			agents[k] = a
		}
		phases[i] = Phase{Title: p.Title, Agents: agents}
	}
	v.Phases = phases
	return v
}

// Meta is what a script's `export const meta = {...}` says.
type Meta struct {
	Name, Description string
	Phases            []string
}

var (
	metaStart = regexp.MustCompile(`(?m)^\s*export\s+const\s+meta\s*=\s*\{`)
	metaEnd   = regexp.MustCompile(`(?m)^\}`)
	str       = `(?:'((?:\\.|[^'\\])*)'|"((?:\\.|[^"\\])*)"|` + "`((?:\\\\.|[^`\\\\])*)`" + `)`
	nameRE    = regexp.MustCompile(`(?m)^\s{0,4}name\s*:\s*` + str)
	descRE    = regexp.MustCompile(`(?m)^\s{0,4}description\s*:\s*` + str)
	titleRE   = regexp.MustCompile(`\btitle\s*:\s*` + str)
	unescape  = strings.NewReplacer(`\'`, `'`, `\"`, `"`, "\\`", "`", `\n`, " ", `\\`, `\`)
)

func literal(m []string) string {
	for _, s := range m[1:] {
		if s != "" {
			return unescape.Replace(s)
		}
	}
	return ""
}

// ParseMeta reads the meta of a workflow script; what it does not say is empty.
func ParseMeta(script string) Meta {
	loc := metaStart.FindStringIndex(script)
	if loc == nil {
		return Meta{}
	}
	block := script[loc[1]:]
	if end := metaEnd.FindStringIndex(block); end != nil {
		block = block[:end[0]]
	}
	var m Meta
	if s := nameRE.FindStringSubmatch(block); s != nil {
		m.Name = literal(s)
	}
	if s := descRE.FindStringSubmatch(block); s != nil {
		m.Description = literal(s)
	}
	for _, s := range titleRE.FindAllStringSubmatch(block, -1) {
		if t := literal(s); t != "" {
			m.Phases = append(m.Phases, t)
		}
	}
	return m
}

// Launch is what the Workflow tool's result says of the run it started.
type Launch struct {
	TaskID, RunID, Summary string
}

var (
	launchedRE = regexp.MustCompile(`Workflow launched in background\. Task ID: ([\w-]+)`)
	runIDRE    = regexp.MustCompile(`(?m)^Run ID: (wf_[\w-]+)`)
	summaryRE  = regexp.MustCompile(`(?m)^Summary: (.+)$`)
)

// ParseLaunch reads a Workflow tool result; ok is false when it started no run in the background.
func ParseLaunch(text string) (l Launch, ok bool) {
	m := launchedRE.FindStringSubmatch(text)
	if m == nil {
		return Launch{}, false
	}
	l.TaskID = m[1]
	if r := runIDRE.FindStringSubmatch(text); r != nil {
		l.RunID = r[1]
	}
	if s := summaryRE.FindStringSubmatch(text); s != nil {
		l.Summary = strings.TrimSpace(s[1])
	}
	return l, true
}

var runName = regexp.MustCompile(`^wf_[\w-]+$`)

// Files are where a conversation keeps a run: its folder of agents, its final state and its scripts.
func Files(transcript, runID string) (dir, state, scripts string) {
	session := strings.TrimSuffix(transcript, ".jsonl")
	return filepath.Join(session, "subagents", "workflows", runID), filepath.Join(session, "workflows", runID+".json"),
		filepath.Join(session, "workflows", "scripts")
}

// maxAgents bounds the agents a view lists: a run that started more keeps the newest.
const maxAgents = 120

// resultMax is how much of what an agent returned a view keeps.
const resultMax = 280

type agentFiles struct {
	headAt float64 // its first line's time, read once
	endAt  float64
}

type run struct {
	mu      sync.Mutex
	ino     uint64
	offset  int64
	order   []string
	agents  map[string]*Agent
	files   map[string]*agentFiles
	meta    *Meta
	stateAt time.Time
	state   *finalState
}

// Reader keeps one cursor per run.
type Reader struct {
	mu   sync.Mutex
	runs map[string]*run
}

func (r *Reader) runOf(key string) *run {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.runs == nil {
		r.runs = map[string]*run{}
	}
	x := r.runs[key]
	if x == nil {
		x = &run{agents: map[string]*Agent{}, files: map[string]*agentFiles{}}
		r.runs[key] = x
	}
	return x
}

// Forget drops what is kept of a conversation's runs.
func (r *Reader) Forget(transcript string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k := range r.runs {
		if strings.HasPrefix(k, transcript+"\x00") {
			delete(r.runs, k)
		}
	}
}

// Read is the run of a conversation as its files are now; ok is false when the conversation has no such run.
func (r *Reader) Read(transcript, runID string) (View, bool) {
	if !runName.MatchString(runID) {
		return View{}, false
	}
	dir, stateFile, scripts := Files(transcript, runID)
	if _, err := os.Stat(dir); err != nil {
		if _, err := os.Stat(stateFile); err != nil {
			return View{}, false
		}
	}
	x := r.runOf(transcript + "\x00" + runID)
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.meta == nil {
		if m, ok := readScript(scripts, runID); ok {
			x.meta = &m
		}
	}
	x.readState(stateFile)
	x.readJournal(filepath.Join(dir, "journal.jsonl"))
	return x.view(dir), true
}

func readScript(scripts, runID string) (Meta, bool) {
	found, _ := filepath.Glob(filepath.Join(scripts, "*-"+runID+".js"))
	if len(found) == 0 {
		return Meta{}, false
	}
	raw, err := os.ReadFile(found[0])
	if err != nil {
		return Meta{}, false
	}
	return ParseMeta(string(raw)), true
}

type finalState struct {
	Status       string  `json:"status"`
	WorkflowName string  `json:"workflowName"`
	Summary      string  `json:"summary"`
	StartTime    float64 `json:"startTime"`
	DurationMs   float64 `json:"durationMs"`
	Phases       []struct {
		Title string `json:"title"`
	} `json:"phases"`
	Progress []struct {
		Type       string  `json:"type"`
		Label      string  `json:"label"`
		PhaseTitle string  `json:"phaseTitle"`
		AgentID    string  `json:"agentId"`
		Model      string  `json:"model"`
		State      string  `json:"state"`
		StartedAt  float64 `json:"startedAt"`
		Last       string  `json:"lastToolSummary"`
	} `json:"workflowProgress"`
}

func (x *run) readState(file string) {
	st, err := os.Stat(file)
	if err != nil || st.ModTime().Equal(x.stateAt) {
		return
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return
	}
	var s finalState
	if json.Unmarshal(raw, &s) != nil {
		return
	}
	x.state, x.stateAt = &s, st.ModTime()
	for _, p := range s.Progress {
		if p.Type != "workflow_agent" || p.AgentID == "" {
			continue
		}
		a := x.agent(p.AgentID)
		if a.Label == "" {
			a.Label = p.Label
		}
		if a.Model == "" {
			a.Model = p.Model
		}
		if p.StartedAt > 0 {
			a.StartedAt = p.StartedAt
		}
		if a.Last == "" {
			a.Last = p.Last
		}
		if a.Phase == "" {
			a.Phase = p.PhaseTitle
		}
		switch p.State {
		case "done", "completed":
			a.State = Done
		case "failed", "error":
			a.State = Failed
		}
	}
}

func (x *run) agent(id string) *Agent {
	a := x.agents[id]
	if a == nil {
		a = &Agent{ID: id, State: Running}
		x.agents[id] = a
		x.order = append(x.order, id)
	}
	return a
}

type journalLine struct {
	Type    string          `json:"type"`
	AgentID string          `json:"agentId"`
	Label   string          `json:"label"`
	Phase   string          `json:"phase"`
	Result  json.RawMessage `json:"result"`
}

func (x *run) readJournal(file string) {
	st, err := os.Stat(file)
	if err != nil {
		return
	}
	ino := uint64(0)
	if s, ok := st.Sys().(*syscall.Stat_t); ok {
		ino = uint64(s.Ino)
	}
	if ino != x.ino || st.Size() < x.offset {
		x.ino, x.offset = ino, 0
	}
	if st.Size() == x.offset {
		return
	}
	f, err := os.Open(file)
	if err != nil {
		return
	}
	defer f.Close()
	buf := make([]byte, st.Size()-x.offset)
	n, _ := f.ReadAt(buf, x.offset)
	buf = buf[:n]
	complete := bytes.LastIndexByte(buf, '\n') + 1
	for _, line := range bytes.Split(buf[:complete], []byte{'\n'}) {
		var l journalLine
		if len(line) == 0 || json.Unmarshal(line, &l) != nil || l.AgentID == "" {
			continue
		}
		a := x.agent(l.AgentID)
		switch l.Type {
		case "started":
			a.Label, a.Phase, a.State, a.Result = l.Label, l.Phase, Running, ""
		case "result":
			a.State, a.Result = Done, resultOf(l.Result)
		case "failed":
			a.State = Failed
		}
	}
	x.offset += int64(complete)
}

// resultKeys are the fields of a returned object that sum it up, in the order they are looked for.
var resultKeys = []string{"summary", "headline", "verdict", "result", "title", "status", "recommendation"}

// resultOf is the first words of what an agent returned: its text, else the field of the object that sums it up, else
// the object's first text.
func resultOf(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return clip(s, resultMax)
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return ""
	}
	for _, k := range resultKeys {
		if json.Unmarshal(obj[k], &s) == nil && strings.TrimSpace(s) != "" {
			return clip(s, resultMax)
		}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil {
		return ""
	}
	for dec.More() {
		if _, err := dec.Token(); err != nil {
			return ""
		}
		var v json.RawMessage
		if dec.Decode(&v) != nil {
			return ""
		}
		if json.Unmarshal(v, &s) == nil && strings.TrimSpace(s) != "" {
			return clip(s, resultMax)
		}
	}
	return ""
}

var spaces = regexp.MustCompile(`\s+`)

func clip(s string, n int) string {
	s = strings.TrimSpace(spaces.ReplaceAllString(s, " "))
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return strings.TrimSpace(jscompat.Slice(s, n-1)) + "…"
}

// headTime is the time of the first line of a transcript that has one.
var headTime = regexp.MustCompile(`"timestamp"\s*:\s*"([^"]+)"`)

func firstTime(file string) float64 {
	f, err := os.Open(file)
	if err != nil {
		return 0
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64<<10)
	head, _ := r.Peek(64 << 10)
	if m := headTime.FindSubmatch(head); m != nil {
		if at := jscompat.ParseDate(string(m[1])); at == at {
			return at
		}
	}
	return 0
}

func (x *run) view(dir string) View {
	var v View
	if x.meta != nil {
		v.Name, v.Description = x.meta.Name, x.meta.Description
	}
	titles := []string{}
	if x.meta != nil {
		titles = append(titles, x.meta.Phases...)
	}
	if s := x.state; s != nil {
		v.Status = s.Status
		if v.Name == "" {
			v.Name = s.WorkflowName
		}
		if v.Description == "" {
			v.Description = s.Summary
		}
		v.StartedAt = s.StartTime
		if s.StartTime > 0 && s.DurationMs > 0 {
			v.EndedAt = s.StartTime + s.DurationMs
		}
		if len(titles) == 0 {
			for _, p := range s.Phases {
				titles = append(titles, p.Title)
			}
		}
	}
	ids := x.order
	if len(ids) > maxAgents {
		ids = ids[len(ids)-maxAgents:]
	}
	byPhase := map[string][]Agent{}
	for _, id := range ids {
		a := x.agents[id]
		x.describe(dir, a)
		p := a.Phase
		if _, known := byPhase[p]; !known && !contains(titles, p) {
			titles = append(titles, p)
		}
		byPhase[p] = append(byPhase[p], *a)
	}
	for _, t := range titles {
		v.Phases = append(v.Phases, Phase{Title: t, Agents: byPhase[t]})
	}
	return v
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// describe adds what the agent's own files say: the model it ran on, its last step and when it started and ended.
func (x *run) describe(dir string, a *Agent) {
	jsonl := filepath.Join(dir, "agent-"+a.ID+".jsonl")
	got := sessionmeta.ReadAgentFiles(jsonl, filepath.Join(dir, "agent-"+a.ID+".meta.json"))
	if got.Model != "" {
		a.Model = got.Model
	}
	if got.Last != "" {
		a.Last = got.Last
	}
	f := x.files[a.ID]
	if f == nil {
		f = &agentFiles{}
		x.files[a.ID] = f
	}
	if f.headAt == 0 {
		f.headAt = firstTime(jsonl)
	}
	if f.headAt > 0 && a.StartedAt == 0 {
		a.StartedAt = f.headAt
	}
	if a.State == Running {
		a.EndedAt = 0
		return
	}
	if f.endAt == 0 {
		if st, err := os.Stat(jsonl); err == nil {
			f.endAt = jscompat.TimeMs(st.ModTime())
		}
	}
	a.EndedAt = f.endAt
}
