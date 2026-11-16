// Package agenteval checks that harnessly by itself makes an agent behave (docs/agent-evals.md): a scenario is data, a
// run happens in a disposable stand, and the run is judged by what it left (tool calls, the right pane's intents,
// pictures, files), not by the wording of its answer.
package agenteval

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Scenario is one file of the scenarios folder.
type Scenario struct {
	ID    string   `yaml:"id" json:"id"`
	Title string   `yaml:"title" json:"title"`
	Set   string   `yaml:"set" json:"set"`
	Tags  []string `yaml:"tags" json:"tags,omitempty"`
	// Agents are the CLIs the scenario applies to: claude, codex; empty is both.
	Agents []string `yaml:"agents" json:"agents,omitempty"`
	Setup  Setup    `yaml:"setup" json:"setup"`
	// Prompt is the canonical words of the person; Variants are stored rewordings of the same intent.
	Prompt   string    `yaml:"prompt" json:"prompt"`
	Variants []Variant `yaml:"variants" json:"variants,omitempty"`
	Expect   []Expect  `yaml:"expect" json:"expect"`
	// Covers are the repository paths (globs, ** for any depth) whose change this scenario checks: a change of one of
	// them makes `harnessly eval affected` name the scenario.
	Covers []string `yaml:"covers" json:"covers,omitempty"`
	// Judge is a rubric for a model, only where no fact can tell.
	Judge    string `yaml:"judge" json:"judge,omitempty"`
	TimeoutS int    `yaml:"timeout_s" json:"timeoutS,omitempty"`
	// Interrupt is a message the person sends while the agent works: «stop, main is frozen».
	Interrupt *Interrupt `yaml:"interrupt" json:"interrupt,omitempty"`
	// SettleS: after the person's chat ends its turn, the run goes on until no agent of the stand (subagents, chats it
	// delegated to, teammates) wrote to its transcript for this many seconds, and the person's chat is not working. A
	// chat that delegates ends its turn and is woken by its chats' answers; without it the run is judged half-way.
	SettleS int `yaml:"settle_s" json:"settleS,omitempty"`

	dir string
	raw []byte
}

// Interrupt is a message sent into the person's chat after AfterS seconds, while its turn is still going.
type Interrupt struct {
	AfterS int    `yaml:"after_s" json:"afterS"`
	Text   string `yaml:"text" json:"text"`
}

// Variant is a stored rewording of the prompt.
type Variant struct {
	ID   string `yaml:"id" json:"id"`
	Kind string `yaml:"kind" json:"kind,omitempty"`
	Text string `yaml:"text" json:"text"`
}

// Setup is what the stand holds before the person speaks.
type Setup struct {
	// Files are written into the workspace: path → content.
	Files map[string]string `yaml:"files" json:"files,omitempty"`
	// Design are folders (relative to the scenario file) copied into the stand's design library as projects.
	Design []string `yaml:"design" json:"design,omitempty"`
	// Kits are UI kit folders (relative to the scenario file, each with its design-system.json naming the kit's id and
	// version) installed into the stand as kits installed on the machine are: the kit_* tools and design new --kit see them.
	Kits []string `yaml:"kits" json:"kits,omitempty"`
	// RuntimeEnv adds to the setup's runtime settings for this scenario.
	RuntimeEnv map[string]string `yaml:"runtime_env" json:"runtimeEnv,omitempty"`
	// Workspace are folders (relative to the scenario file) whose contents are copied into the workspace.
	Workspace []string `yaml:"workspace" json:"workspace,omitempty"`
	// Run is a shell script run in the workspace, inside the stand's sandbox, after the files are in place (git init).
	Run string `yaml:"run" json:"run,omitempty"`
}

// Expect is one expectation; exactly one of its kinds is set.
type Expect struct {
	Name string `yaml:"name" json:"name,omitempty"`
	// Called: a call that did not fail, of a tool whose name matches (whole), with arguments matching Args.
	Called string `yaml:"called" json:"called,omitempty"`
	// EvenFailed makes Called count a call that failed too: a question to the person times out in a run nobody
	// answers, and what is judged is that the agent asked.
	EvenFailed bool `yaml:"even_failed" json:"evenFailed,omitempty"`
	// NotCalled: no call of a matching tool with matching Args ran (a refused call does not count).
	NotCalled string `yaml:"not_called" json:"notCalled,omitempty"`
	// Args are regexps over the call's arguments: argument name → regexp over its value (a string as it is, other JSON
	// as its text).
	Args map[string]string `yaml:"args" json:"args,omitempty"`
	// Pane: the agent asked the right pane to open a tab of a kind matching Tab, at a target matching Target.
	Pane *PaneExpect `yaml:"pane" json:"pane,omitempty"`
	// Images: at least this many pictures were drawn for the chat.
	Images int `yaml:"images" json:"images,omitempty"`
	// File: a file of the workspace or the design library.
	File *FileExpect `yaml:"file" json:"file,omitempty"`
	// Text and TextNot are regexps over everything the agent said to the person.
	Text    string `yaml:"text" json:"text,omitempty"`
	TextNot string `yaml:"text_not" json:"textNot,omitempty"`
	// AnyOf holds when one of its expectations does.
	AnyOf []Expect `yaml:"any_of" json:"anyOf,omitempty"`
	// MaxCalls bounds the agent's tool calls.
	MaxCalls int `yaml:"max_calls" json:"maxCalls,omitempty"`
	// Check is a shell command run in the workspace after the run, inside the stand's sandbox: it holds when it exits 0.
	// It sees EVAL_STARTED_AT and EVAL_INTERRUPT_AT (Unix seconds; empty without an interrupt).
	Check string `yaml:"check" json:"check,omitempty"`
}

// PaneExpect is a tab the agent opened.
type PaneExpect struct {
	Tab    string `yaml:"tab" json:"tab"`
	Target string `yaml:"target" json:"target,omitempty"`
	// Place is a regexp over where in the target (a design's board); its target must match too.
	Place string `yaml:"place" json:"place,omitempty"`
}

// FileExpect is a file that must (or must not) be there.
type FileExpect struct {
	// Glob is relative to the workspace, or to the design library with the prefix "design:".
	Glob     string `yaml:"glob" json:"glob"`
	Contains string `yaml:"contains" json:"contains,omitempty"`
	Absent   bool   `yaml:"absent" json:"absent,omitempty"`
}

const (
	SetDev     = "dev"
	SetHeldOut = "heldout"

	defaultTimeoutS = 300
	maxTimeoutS     = 1800
	maxSettleS      = 600
)

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,63}$`)

// Dir is the folder of the scenario's file.
func (s *Scenario) Dir() string { return s.dir }

// Timeout is the scenario's wall time for one run, in seconds.
func (s *Scenario) Timeout() int {
	if s.TimeoutS <= 0 {
		return defaultTimeoutS
	}
	return s.TimeoutS
}

// Fingerprint changes whenever anything that steers a run changes.
func (s *Scenario) Fingerprint() string {
	h := sha256.Sum256(s.raw)
	return hex.EncodeToString(h[:])[:12]
}

// AppliesTo says whether the scenario runs on an agent CLI.
func (s *Scenario) AppliesTo(agent string) bool {
	if len(s.Agents) == 0 {
		return true
	}
	for _, a := range s.Agents {
		if a == agent {
			return true
		}
	}
	return false
}

// Prompts are the canonical prompt (id "canon") and the stored variants.
func (s *Scenario) Prompts() []Variant {
	return append([]Variant{{ID: "canon", Text: s.Prompt}}, s.Variants...)
}

// ParseScenario reads one scenario; the error says in words what is wrong.
func ParseScenario(name string, raw []byte) (*Scenario, error) {
	var s Scenario
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("%s: %v", name, err)
	}
	s.raw = raw
	if !idRe.MatchString(s.ID) {
		return nil, fmt.Errorf("%s: id is lower-case letters, digits and hyphens", name)
	}
	if s.Set == "" {
		s.Set = SetDev
	}
	if s.Set != SetDev && s.Set != SetHeldOut {
		return nil, fmt.Errorf("%s: set is dev or heldout", name)
	}
	if strings.TrimSpace(s.Prompt) == "" {
		return nil, fmt.Errorf("%s: prompt is what the person says", name)
	}
	for _, a := range s.Agents {
		if a != "claude" && a != "codex" {
			return nil, fmt.Errorf("%s: agents are claude and codex", name)
		}
	}
	if s.TimeoutS < 0 || s.TimeoutS > maxTimeoutS {
		return nil, fmt.Errorf("%s: timeout_s is 0 to %d", name, maxTimeoutS)
	}
	if s.SettleS < 0 || s.SettleS > maxSettleS {
		return nil, fmt.Errorf("%s: settle_s is 0 to %d", name, maxSettleS)
	}
	if it := s.Interrupt; it != nil && (it.AfterS <= 0 || strings.TrimSpace(it.Text) == "" || (s.TimeoutS > 0 && it.AfterS >= s.TimeoutS)) {
		return nil, fmt.Errorf("%s: interrupt has after_s (within timeout_s) and a text", name)
	}
	seen := map[string]bool{"canon": true}
	for _, v := range s.Variants {
		if v.ID == "" || seen[v.ID] || strings.TrimSpace(v.Text) == "" {
			return nil, fmt.Errorf("%s: a variant has its own id and a text", name)
		}
		seen[v.ID] = true
	}
	if len(s.Expect) == 0 {
		return nil, fmt.Errorf("%s: a scenario expects something", name)
	}
	for i, e := range s.Expect {
		if err := e.validate(); err != nil {
			return nil, fmt.Errorf("%s: expect[%d]: %v", name, i, err)
		}
	}
	for p := range s.Setup.Files {
		if filepath.IsAbs(p) || strings.Contains(filepath.ToSlash(filepath.Clean(p)), "..") {
			return nil, fmt.Errorf("%s: setup file %q is inside the workspace", name, p)
		}
	}
	for k := range s.Setup.RuntimeEnv {
		if !runtimeEnvKey.MatchString(k) {
			return nil, fmt.Errorf("%s: runtime_env takes RUNTIME_* settings, not %s", name, k)
		}
	}
	for _, d := range append(append(append([]string{}, s.Setup.Design...), s.Setup.Workspace...), s.Setup.Kits...) {
		if filepath.IsAbs(d) {
			return nil, fmt.Errorf("%s: a fixture folder is relative to the scenario", name)
		}
	}
	return &s, nil
}

func (e Expect) kinds() int {
	n := 0
	for _, set := range []bool{e.Called != "", e.NotCalled != "", e.Pane != nil, e.Images > 0, e.File != nil, e.Text != "",
		e.TextNot != "", len(e.AnyOf) > 0, e.MaxCalls > 0, e.Check != ""} {
		if set {
			n++
		}
	}
	return n
}

func (e Expect) validate() error {
	if e.kinds() != 1 {
		return errors.New("one kind per expectation: called, not_called, pane, images, file, text, text_not, any_of, max_calls or check")
	}
	if len(e.Args) > 0 && e.Called == "" && e.NotCalled == "" {
		return errors.New("args go with called or not_called")
	}
	res := []string{e.Called, e.NotCalled, e.Text, e.TextNot}
	for _, re := range e.Args {
		res = append(res, re)
	}
	if e.Pane != nil {
		if e.Pane.Tab == "" {
			return errors.New("pane names a tab")
		}
		res = append(res, e.Pane.Tab, e.Pane.Target, e.Pane.Place)
	}
	if e.File != nil {
		if e.File.Glob == "" {
			return errors.New("file has a glob")
		}
		res = append(res, e.File.Contains)
	}
	for _, re := range res {
		if re == "" {
			continue
		}
		if _, err := regexp.Compile(re); err != nil {
			return fmt.Errorf("%q is not a regular expression: %v", re, err)
		}
	}
	for i, sub := range e.AnyOf {
		if err := sub.validate(); err != nil {
			return fmt.Errorf("any_of[%d]: %v", i, err)
		}
	}
	return nil
}

// Load reads the scenarios of a folder (*.yaml, one level), by id.
func Load(dir string) ([]*Scenario, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var out []*Scenario
	var errs []error
	ids := map[string]string{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		s, err := ParseScenario(filepath.Base(f), raw)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if prev, dup := ids[s.ID]; dup {
			errs = append(errs, fmt.Errorf("%s: id %s is taken by %s", filepath.Base(f), s.ID, prev))
			continue
		}
		ids[s.ID] = filepath.Base(f)
		s.dir = dir
		out = append(out, s)
	}
	return out, errors.Join(errs...)
}
