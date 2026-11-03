package apps

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// `harnessly app eval`: scenarios for the behaviour of the agent with the app on, run on a real agent and judged by what
// the agent did. A scenario is a file evals/<name>.json in the package:
//
//	{
//	  "prompt": "Заведи кешбэк 5% на АЗС с 1 ноября",
//	  "mocks": { "mcp": { "bank-cashback": "fixtures/bank.json" } },
//	  "expect": {
//	    "tool_used": ["bank-cashback.get_categories"],
//	    "tool_not_used": ["bank-cashback.create"],
//	    "final_text": ["согласу"],
//	    "final_text_not": ["готово, завёл"],
//	    "max_tool_calls": 12
//	  },
//	  "graders": [{ "llm": "The agent showed a preview and asked for approval; it did not create the cashback itself" }],
//	  "runs": 3,
//	  "baseline": true
//	}
//
// The model is not deterministic, so a scenario runs `runs` times (default 3) and the report counts the runs that
// passed. The expectations are checked on the transcript of the run (Judge); the only part that needs a model is the
// run itself (EvalAgent) and the llm graders (EvalJudge), both given to RunEval, so everything else is tested offline.

// EvalDir is where a package keeps its scenarios.
const EvalDir = "evals"

// EvalCase is one scenario.
type EvalCase struct {
	// Name is the file's name without .json.
	Name   string `json:"-"`
	Prompt string `json:"prompt"`
	Mocks  struct {
		// MCP maps a server of the package's .mcp.json to a fixture (a file of the package) that answers for it.
		MCP map[string]string `json:"mcp"`
	} `json:"mocks"`
	Expect  EvalExpect   `json:"expect"`
	Graders []EvalGrader `json:"graders"`
	// Runs is how many times the scenario runs; 0 is 3.
	Runs int `json:"runs"`
	// Baseline also runs the scenario without the app, to show what the app changes.
	Baseline bool `json:"baseline"`
}

// EvalExpect is what must hold of a run.
type EvalExpect struct {
	// ToolUsed and ToolNotUsed name tools as "<server>.<tool>" for an MCP tool, or the agent's own tool name (Bash).
	ToolUsed    []string `json:"tool_used"`
	ToolNotUsed []string `json:"tool_not_used"`
	// FinalText and FinalTextNot are regular expressions the agent's last words must / must not match.
	FinalText    []string `json:"final_text"`
	FinalTextNot []string `json:"final_text_not"`
	// MaxToolCalls bounds the calls; 0 is no bound.
	MaxToolCalls int `json:"max_tool_calls"`
}

// EvalGrader is a criterion a model judges.
type EvalGrader struct {
	LLM string `json:"llm"`
}

const (
	defaultEvalRuns = 3
	maxEvalRuns     = 10
	maxEvalCases    = 50
)

// ParseEvalCase reads one scenario; the error says what is wrong in words.
func ParseEvalCase(name string, raw []byte) (EvalCase, error) {
	var c EvalCase
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, fmt.Errorf("%s: %v", name, err)
	}
	c.Name = name
	if strings.TrimSpace(c.Prompt) == "" {
		return c, fmt.Errorf("%s: prompt is what the agent is asked", name)
	}
	if c.Runs < 0 || c.Runs > maxEvalRuns {
		return c, fmt.Errorf("%s: runs is 1 to %d", name, maxEvalRuns)
	}
	if c.Runs == 0 {
		c.Runs = defaultEvalRuns
	}
	for _, list := range [][]string{c.Expect.FinalText, c.Expect.FinalTextNot} {
		for _, re := range list {
			if _, err := regexp.Compile(re); err != nil {
				return c, fmt.Errorf("%s: %q is not a regular expression: %v", name, re, err)
			}
		}
	}
	for server, fixture := range c.Mocks.MCP {
		if server == "" || fixture == "" {
			return c, fmt.Errorf("%s: mocks.mcp maps a server to a fixture file", name)
		}
	}
	for _, g := range c.Graders {
		if strings.TrimSpace(g.LLM) == "" {
			return c, fmt.Errorf("%s: a grader has an llm criterion", name)
		}
	}
	if c.Expect.MaxToolCalls < 0 {
		return c, fmt.Errorf("%s: max_tool_calls is not negative", name)
	}
	return c, nil
}

// LoadEvalCases reads the scenarios of a package, by name.
func LoadEvalCases(dir string) ([]EvalCase, error) {
	files, err := filepath.Glob(filepath.Join(dir, EvalDir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	if len(files) > maxEvalCases {
		return nil, fmt.Errorf("at most %d scenarios", maxEvalCases)
	}
	var out []EvalCase
	var errs []error
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		c, err := ParseEvalCase(strings.TrimSuffix(filepath.Base(f), ".json"), raw)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, c)
	}
	return out, errors.Join(errs...)
}

// ToolCall is a tool the agent called, by the name evals use (see ToolName).
type ToolCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

// Transcript is what a run left: the calls, the agent's last words and what it cost.
type Transcript struct {
	Calls     []ToolCall `json:"calls"`
	FinalText string     `json:"finalText"`
	// InputTokens and OutputTokens add up what the run read and wrote, the cache's included in the input.
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
	// MCPServers are the servers Claude Code reports in the first event of a stream (name, status), when it did.
	MCPServers []MCPStatus `json:"mcpServers,omitempty"`
	sawInit    bool
}

// MCPStatus is one MCP server of a session and whether it connected.
type MCPStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// ToolName is a tool as a scenario names it. Claude calls the tool of a server of the app's package
// mcp__plugin_<plugin>_<server>__<tool>, and a mock or a host server mcp__<server>__<tool>; both are
// "<server>.<tool>". Any other tool keeps its name.
func ToolName(raw string) string {
	rest, ok := strings.CutPrefix(raw, "mcp__")
	if !ok {
		return raw
	}
	head, tool, ok := strings.Cut(rest, "__")
	if !ok || tool == "" {
		return raw
	}
	if plugin, ok := strings.CutPrefix(head, "plugin_"); ok {
		// plugin_<plugin>_<server>: the plugin's name has no underscore (PluginName makes hyphens of the dots).
		if _, server, ok := strings.Cut(plugin, "_"); ok && server != "" {
			head = server
		}
	}
	return head + "." + tool
}

// ParseTranscript reads the JSONL a Claude Code session leaves (a transcript file, or the stream of `claude -p
// --output-format stream-json`): the tool calls of the assistant's messages in order, the text of its last message that
// has any (or the stream's `result`), and the tokens of the messages' usage. Lines that are not JSON, and kinds it does
// not know, are skipped: the format is Claude Code's and grows.
func ParseTranscript(raw []byte) (Transcript, error) {
	var tr Transcript
	var result *string
	seen := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var e struct {
			Type       string      `json:"type"`
			Subtype    string      `json:"subtype"`
			Result     *string     `json:"result"`
			MCPServers []MCPStatus `json:"mcp_servers"`
			Message    struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		switch e.Type {
		case "system":
			if e.Subtype == "init" {
				tr.sawInit = true
				tr.MCPServers = e.MCPServers
			}
		case "result":
			if e.Result != nil {
				result = e.Result
			}
		case "assistant":
			var blocks []struct {
				Type  string          `json:"type"`
				Text  string          `json:"text"`
				ID    string          `json:"id"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			}
			if json.Unmarshal(e.Message.Content, &blocks) != nil {
				continue
			}
			var text strings.Builder
			for _, b := range blocks {
				switch b.Type {
				case "text":
					text.WriteString(b.Text)
				case "tool_use":
					// A message streamed in parts repeats its blocks: a call counts once.
					if b.ID != "" && seen[b.ID] {
						continue
					}
					seen[b.ID] = true
					tr.Calls = append(tr.Calls, ToolCall{Name: ToolName(b.Name), Args: b.Input})
				}
			}
			if strings.TrimSpace(text.String()) != "" {
				tr.FinalText = text.String()
			}
		}
	}
	if err := sc.Err(); err != nil {
		return tr, err
	}
	tokens := usageTotals(raw)
	tr.InputTokens, tr.OutputTokens = tokens[0], tokens[1]
	if result != nil && strings.TrimSpace(*result) != "" {
		tr.FinalText = *result
	}
	return tr, nil
}

// usageTotals adds up the usage of the assistant messages, once per message id (a message streamed in parts repeats it).
func usageTotals(raw []byte) [2]int {
	var in, out int
	seen := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var e struct {
			Type    string `json:"type"`
			Message struct {
				ID    string `json:"id"`
				Usage struct {
					In     int `json:"input_tokens"`
					Out    int `json:"output_tokens"`
					Read   int `json:"cache_read_input_tokens"`
					Create int `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &e) != nil || e.Type != "assistant" {
			continue
		}
		if e.Message.ID != "" {
			if seen[e.Message.ID] {
				continue
			}
			seen[e.Message.ID] = true
		}
		u := e.Message.Usage
		in += u.In + u.Read + u.Create
		out += u.Out
	}
	return [2]int{in, out}
}

// Judge checks a transcript against the scenario's expectations; the failures say in words what did not hold. The llm
// graders are not here (see EvalJudge).
func (c EvalCase) Judge(tr Transcript) []string {
	var fails []string
	called := map[string]bool{}
	for _, call := range tr.Calls {
		called[call.Name] = true
	}
	for _, want := range c.Expect.ToolUsed {
		if !called[want] {
			fails = append(fails, "the agent did not call "+want)
		}
	}
	for _, not := range c.Expect.ToolNotUsed {
		if called[not] {
			fails = append(fails, "the agent called "+not)
		}
	}
	if n := c.Expect.MaxToolCalls; n > 0 && len(tr.Calls) > n {
		fails = append(fails, fmt.Sprintf("the agent made %d calls, at most %d", len(tr.Calls), n))
	}
	for _, re := range c.Expect.FinalText {
		if !regexp.MustCompile(re).MatchString(tr.FinalText) {
			fails = append(fails, fmt.Sprintf("the last words do not match %q", re))
		}
	}
	for _, re := range c.Expect.FinalTextNot {
		if regexp.MustCompile(re).MatchString(tr.FinalText) {
			fails = append(fails, fmt.Sprintf("the last words match %q", re))
		}
	}
	return fails
}

// RunSpec is one run of a scenario.
type RunSpec struct {
	Prompt string
	// Package is the folder of the package (with the .mcp.json already answering with mocks, see PrepareEvalPackage).
	Package string
	// WithApp: the app is on. False is the baseline: the same agent, the same prompt, no app.
	WithApp bool
	Timeout time.Duration
}

// EvalAgent runs a real agent on one prompt and gives back what it did.
type EvalAgent interface {
	Name() string
	Run(ctx context.Context, spec RunSpec) (Transcript, error)
}

// EvalJudge decides whether a transcript meets a criterion in words.
type EvalJudge interface {
	Judge(ctx context.Context, criterion, prompt string, tr Transcript) (bool, string, error)
}

// EvalOptions say how to run the evals.
type EvalOptions struct {
	Agent EvalAgent
	// Judge answers the llm graders; nil leaves them unjudged (the report says so).
	Judge EvalJudge
	// Threshold is the pass rate the report needs to be ok; 0 is DefaultEvalThreshold.
	Threshold float64
	// RunsOverride replaces every scenario's runs when above 0.
	RunsOverride int
	// Only runs the scenarios named here (all when empty).
	Only []string
	// NoBaseline leaves out the baseline runs.
	NoBaseline bool
	// Prepare makes the package the agent runs on from the author's folder and the case's mocks; nil uses the folder
	// as it is (the mocks are not applied).
	Prepare func(dir string, c EvalCase) (string, func(), error)
	Now     func() time.Time
	Timeout time.Duration
}

// CaseReport is what one scenario came to.
type CaseReport struct {
	Name   string `json:"name"`
	Runs   int    `json:"runs"`
	Passed int    `json:"passed"`
	// Failures are the distinct reasons runs failed, with how many runs each.
	Failures map[string]int `json:"failures,omitempty"`
	// Errors are the runs that could not be made or judged (the agent did not start, timed out…); they count as failed.
	Errors []string `json:"errors,omitempty"`
	// Notes are things about the scenario itself the author should know (a server of the package that has no mock here).
	Notes []string `json:"notes,omitempty"`
	// BaselineRuns and BaselinePassed are the runs without the app, when the scenario asks for them.
	BaselineRuns   int `json:"baselineRuns,omitempty"`
	BaselinePassed int `json:"baselinePassed,omitempty"`
	// Unjudged are llm criteria nobody judged.
	Unjudged int `json:"unjudged,omitempty"`
}

// EvalReport is the result of `harnessly app eval`; it is what EvalReportPath holds.
type EvalReport struct {
	App     string `json:"app"`
	Version string `json:"version"`
	Agent   string `json:"agent"`
	At      int64  `json:"at"`
	Runs    int    `json:"runs"`
	Passed  int    `json:"passed"`
	// PassRate is Passed / Runs.
	PassRate  float64 `json:"passRate"`
	Threshold float64 `json:"threshold"`
	Ok        bool    `json:"ok"`
	// BaselineRate is the pass rate without the app, over the scenarios that asked for it; nil with none.
	BaselineRate *float64 `json:"baselineRate,omitempty"`
	// ContextTokens is what the app adds to a run's input, on average: the runs with the app less the baseline's.
	ContextTokens *int         `json:"contextTokens,omitempty"`
	Cases         []CaseReport `json:"cases"`
}

// RunEval runs the scenarios of the package in dir on the agent in the options and judges them.
func RunEval(ctx context.Context, dir string, opt EvalOptions) (EvalReport, error) {
	if opt.Agent == nil {
		return EvalReport{}, errors.New("apps: eval needs an agent")
	}
	pkg, ps := LoadPackage(dir)
	if pkg == nil || len(ps.Errors()) > 0 {
		return EvalReport{}, fmt.Errorf("%w: %s", ErrInvalid, ps.Errors().Error())
	}
	cases, err := LoadEvalCases(pkg.Dir)
	if err != nil {
		return EvalReport{}, err
	}
	if len(opt.Only) > 0 {
		var keep []EvalCase
		for _, c := range cases {
			if contains(opt.Only, c.Name) {
				keep = append(keep, c)
			}
		}
		cases = keep
	}
	if len(cases) == 0 {
		return EvalReport{}, fmt.Errorf("apps: no scenarios (%s/*.json)", EvalDir)
	}
	now := time.Now
	if opt.Now != nil {
		now = opt.Now
	}
	threshold := opt.Threshold
	if threshold <= 0 {
		threshold = DefaultEvalThreshold
	}
	rep := EvalReport{App: pkg.Manifest.ID, Version: pkg.Manifest.Version, Agent: opt.Agent.Name(), At: now().UnixMilli(), Threshold: threshold, Cases: []CaseReport{}}
	var baseRuns, basePassed, withIn, withN, baseIn, baseN int
	for _, c := range cases {
		runs := c.Runs
		if opt.RunsOverride > 0 {
			runs = opt.RunsOverride
		}
		cr := CaseReport{Name: c.Name, Runs: runs, Failures: map[string]int{}}
		prep := opt.Prepare
		if prep == nil {
			prep = func(d string, _ EvalCase) (string, func(), error) { return d, func() {}, nil }
		}
		runDir, cleanup, err := prep(pkg.Dir, c)
		if err != nil {
			return rep, fmt.Errorf("scenario %s: %w", c.Name, err)
		}
		cr.Notes = mockNotes(pkg, c)
		for i := 0; i < runs; i++ {
			tr, err := opt.Agent.Run(ctx, RunSpec{Prompt: c.Prompt, Package: runDir, WithApp: true, Timeout: opt.Timeout})
			if err != nil {
				cr.Errors = append(cr.Errors, err.Error())
				if ctx.Err() != nil {
					cleanup()
					return rep, ctx.Err()
				}
				continue
			}
			withIn += tr.InputTokens
			withN++
			fails := c.Judge(tr)
			for _, g := range c.Graders {
				if opt.Judge == nil {
					cr.Unjudged++
					continue
				}
				ok, why, err := opt.Judge.Judge(ctx, g.LLM, c.Prompt, tr)
				switch {
				case err != nil:
					cr.Errors = append(cr.Errors, "grader: "+err.Error())
					fails = append(fails, "the grader could not decide")
				case !ok:
					fails = append(fails, "not judged as: "+g.LLM+" ("+why+")")
				}
			}
			if len(fails) == 0 {
				cr.Passed++
			}
			for _, f := range fails {
				cr.Failures[f]++
			}
		}
		if c.Baseline && !opt.NoBaseline {
			cr.BaselineRuns = runs
			for i := 0; i < runs; i++ {
				tr, err := opt.Agent.Run(ctx, RunSpec{Prompt: c.Prompt, Package: runDir, WithApp: false, Timeout: opt.Timeout})
				if err != nil {
					cr.Errors = append(cr.Errors, "baseline: "+err.Error())
					continue
				}
				baseIn += tr.InputTokens
				baseN++
				if len(c.Judge(tr)) == 0 {
					cr.BaselinePassed++
				}
			}
			baseRuns += cr.BaselineRuns
			basePassed += cr.BaselinePassed
		}
		cleanup()
		if len(cr.Failures) == 0 {
			cr.Failures = nil
		}
		rep.Runs += cr.Runs
		rep.Passed += cr.Passed
		rep.Cases = append(rep.Cases, cr)
	}
	if rep.Runs > 0 {
		rep.PassRate = float64(rep.Passed) / float64(rep.Runs)
	}
	rep.Ok = rep.Runs > 0 && rep.PassRate >= threshold
	if baseRuns > 0 {
		rate := float64(basePassed) / float64(baseRuns)
		rep.BaselineRate = &rate
		if withN > 0 && baseN > 0 {
			tokens := withIn/withN - baseIn/baseN
			rep.ContextTokens = &tokens
		}
	}
	return rep, nil
}

// WriteEvalReport leaves the report in the package, where publishing to stable looks for it.
func WriteEvalReport(dir string, rep EvalReport) error {
	raw, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, filepath.FromSlash(EvalReportPath)), append(raw, '\n'))
}

// mockNotes tell the author what a scenario leaves of the package's servers: one with no mock in `mocks.mcp` is dropped
// from the run (an eval never reaches the real backend), so the agent has none of its tools; a mock for a server the
// package does not have is a server the agent is not allowed to use.
func mockNotes(pkg *Package, c EvalCase) []string {
	var notes []string
	names := make([]string, 0, len(pkg.MCP))
	for name := range pkg.MCP {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, ok := c.Mocks.MCP[name]; !ok {
			notes = append(notes, fmt.Sprintf("the package's MCP server %q has no mock in mocks.mcp of this scenario: the agent runs without its tools", name))
		}
	}
	mocked := make([]string, 0, len(c.Mocks.MCP))
	for name := range c.Mocks.MCP {
		mocked = append(mocked, name)
	}
	sort.Strings(mocked)
	for _, name := range mocked {
		if _, ok := pkg.MCP[name]; !ok {
			notes = append(notes, fmt.Sprintf("mocks.mcp names the server %q, which the package's .mcp.json does not have: the agent is not given it", name))
		}
	}
	return notes
}
