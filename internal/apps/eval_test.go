package apps

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const caseJSON = `{
  "prompt": "Заведи кешбэк 5% на АЗС с 1 ноября",
  "mocks": { "mcp": { "bank-cashback": "fixtures/bank.json" } },
  "expect": {
    "tool_used": ["bank-cashback.get_categories"],
    "tool_not_used": ["bank-cashback.create"],
    "final_text": ["согласовать"],
    "final_text_not": ["завёл"],
    "max_tool_calls": 5
  },
  "graders": [{ "llm": "Показал превью и попросил согласовать" }],
  "runs": 2,
  "baseline": true
}`

func TestParseEvalCase(t *testing.T) {
	c, err := ParseEvalCase("create-basic", []byte(caseJSON))
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "create-basic" || c.Runs != 2 || !c.Baseline || c.Mocks.MCP["bank-cashback"] != "fixtures/bank.json" {
		t.Fatalf("%+v", c)
	}
	if c, _ := ParseEvalCase("x", []byte(`{"prompt":"p"}`)); c.Runs != 3 {
		t.Fatalf("default runs %d", c.Runs)
	}
	for name, body := range map[string]string{
		"no prompt":     `{"prompt":" "}`,
		"unknown field": `{"prompt":"p","expects":{}}`,
		"bad regexp":    `{"prompt":"p","expect":{"final_text":["("]}}`,
		"too many runs": `{"prompt":"p","runs":99}`,
		"empty grader":  `{"prompt":"p","graders":[{"llm":""}]}`,
		"not json":      `prompt: p`,
		"empty mock":    `{"prompt":"p","mocks":{"mcp":{"s":""}}}`,
	} {
		if _, err := ParseEvalCase("x", []byte(body)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestLoadEvalCases(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, EvalDir), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, EvalDir, "b.json"), []byte(`{"prompt":"b"}`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, EvalDir, "a.json"), []byte(caseJSON), 0o644)
	cases, err := LoadEvalCases(dir)
	if err != nil || len(cases) != 2 || cases[0].Name != "a" {
		t.Fatalf("%v %+v", err, cases)
	}
	_ = os.WriteFile(filepath.Join(dir, EvalDir, "c.json"), []byte(`{`), 0o644)
	if _, err := LoadEvalCases(dir); err == nil {
		t.Fatal("want the broken file reported")
	}
}

func TestToolName(t *testing.T) {
	for raw, want := range map[string]string{
		"mcp__plugin_acme-cashback_bank-cashback__get_categories": "bank-cashback.get_categories",
		"mcp__bank-cashback__create":                              "bank-cashback.create",
		"mcp__harnessly-app__app_open":                            "harnessly-app.app_open",
		"Bash":                                                    "Bash",
		"mcp__broken":                                             "mcp__broken",
	} {
		if got := ToolName(raw); got != want {
			t.Errorf("%s: %s, want %s", raw, got, want)
		}
	}
}

func fixtureTranscript(t *testing.T) Transcript {
	t.Helper()
	raw, err := os.ReadFile("testdata/eval/session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	tr, err := ParseTranscript(raw)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestParseTranscriptFromAFixtureSession(t *testing.T) {
	tr := fixtureTranscript(t)
	var names []string
	for _, c := range tr.Calls {
		names = append(names, c.Name)
	}
	// The message streamed twice counts its call once; the line that is not JSON is skipped.
	if strings.Join(names, ",") != "bank-cashback.get_categories,Skill" {
		t.Fatalf("calls %v", names)
	}
	if !strings.Contains(tr.FinalText, "Прошу согласовать") {
		t.Fatalf("final text %q", tr.FinalText)
	}
	// m1 once: 1200+300, m2 1500, m3 1700.
	if tr.InputTokens != 4700 || tr.OutputTokens != 120 {
		t.Fatalf("tokens in %d out %d", tr.InputTokens, tr.OutputTokens)
	}
}

func TestJudge(t *testing.T) {
	c, _ := ParseEvalCase("c", []byte(caseJSON))
	tr := fixtureTranscript(t)
	if fails := c.Judge(tr); len(fails) != 0 {
		t.Fatalf("the good run failed: %v", fails)
	}
	bad := tr
	bad.Calls = append(slicesClone(tr.Calls), ToolCall{Name: "bank-cashback.create"})
	bad.FinalText = "Готово, завёл кешбэк."
	fails := strings.Join(c.Judge(bad), "|")
	for _, want := range []string{"called bank-cashback.create", "do not match", "match \"завёл\""} {
		if !strings.Contains(fails, want) {
			t.Errorf("want %q in %q", want, fails)
		}
	}
	none := Transcript{FinalText: "согласовать"}
	if f := strings.Join(c.Judge(none), "|"); !strings.Contains(f, "did not call bank-cashback.get_categories") {
		t.Errorf("%s", f)
	}
	many := tr
	many.Calls = make([]ToolCall, 6)
	if f := strings.Join(c.Judge(many), "|"); !strings.Contains(f, "at most 5") {
		t.Errorf("%s", f)
	}
}

func slicesClone(c []ToolCall) []ToolCall { return append([]ToolCall(nil), c...) }

// fakeAgent answers each run from a list; a run without the app gets the baseline's.
type fakeAgent struct {
	mu       sync.Mutex
	with     []Transcript
	baseline Transcript
	runs     []RunSpec
}

func (f *fakeAgent) Name() string { return "fake" }
func (f *fakeAgent) Run(_ context.Context, spec RunSpec) (Transcript, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, spec)
	if !spec.WithApp {
		return f.baseline, nil
	}
	if len(f.with) == 0 {
		return Transcript{}, errors.New("the agent did not start")
	}
	tr := f.with[0]
	f.with = f.with[1:]
	return tr, nil
}

type fakeJudge struct{ pass bool }

func (j fakeJudge) Judge(context.Context, string, string, Transcript) (bool, string, error) {
	return j.pass, "because", nil
}

func evalPackage(t *testing.T) string {
	t.Helper()
	dir := scaffold(t, "acme.cashback", "chat")
	if err := os.MkdirAll(filepath.Join(dir, EvalDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, EvalDir, "create-basic.json"), []byte(caseJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRunEvalCountsPassesBaselineAndContext(t *testing.T) {
	dir := evalPackage(t)
	good := fixtureTranscript(t)
	bad := good
	bad.Calls = nil
	baseline := Transcript{FinalText: "Завёл.", InputTokens: 1000}
	agent := &fakeAgent{with: []Transcript{good, bad}, baseline: baseline}
	rep, err := RunEval(context.Background(), dir, EvalOptions{Agent: agent, Judge: fakeJudge{pass: true}, Threshold: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Runs != 2 || rep.Passed != 1 || rep.PassRate != 0.5 || !rep.Ok || rep.Agent != "fake" || rep.App != "acme.cashback" {
		t.Fatalf("%+v", rep)
	}
	c := rep.Cases[0]
	if c.Name != "create-basic" || c.Failures["the agent did not call bank-cashback.get_categories"] != 1 {
		t.Fatalf("%+v", c)
	}
	// The baseline ran twice without the app and passed nowhere; the app costs its tokens on top of the baseline's.
	if rep.BaselineRate == nil || *rep.BaselineRate != 0 || c.BaselineRuns != 2 {
		t.Fatalf("baseline %+v", rep)
	}
	if rep.ContextTokens == nil || *rep.ContextTokens != 4700-1000 {
		t.Fatalf("context tokens %+v", rep)
	}
	withApp, without := 0, 0
	for _, r := range agent.runs {
		if r.WithApp {
			withApp++
		} else {
			without++
		}
	}
	if withApp != 2 || without != 2 {
		t.Fatalf("runs with %d without %d", withApp, without)
	}

	// Below the bar the report is not ok (the CLI exits with an error).
	agent = &fakeAgent{with: []Transcript{good, bad}, baseline: baseline}
	rep, _ = RunEval(context.Background(), dir, EvalOptions{Agent: agent, Judge: fakeJudge{pass: true}, Threshold: 0.9})
	if rep.Ok {
		t.Fatalf("50%% passed against 90%%: %+v", rep)
	}
	// A grader that says no fails the run even when the calls are right.
	agent = &fakeAgent{with: []Transcript{good, good}, baseline: baseline}
	rep, _ = RunEval(context.Background(), dir, EvalOptions{Agent: agent, Judge: fakeJudge{pass: false}, NoBaseline: true})
	if rep.Passed != 0 || rep.BaselineRate != nil {
		t.Fatalf("grader: %+v", rep)
	}
	// Without a judge the llm criteria are reported as not judged, not passed silently.
	agent = &fakeAgent{with: []Transcript{good, good}, baseline: baseline}
	rep, _ = RunEval(context.Background(), dir, EvalOptions{Agent: agent, NoBaseline: true})
	if rep.Passed != 2 || rep.Cases[0].Unjudged != 2 {
		t.Fatalf("unjudged: %+v", rep)
	}
	// An agent that does not start is a failed run, with its reason.
	agent = &fakeAgent{}
	rep, _ = RunEval(context.Background(), dir, EvalOptions{Agent: agent, NoBaseline: true})
	if rep.Passed != 0 || len(rep.Cases[0].Errors) != 2 || rep.Ok {
		t.Fatalf("errors: %+v", rep)
	}
}

func TestEvalReportFeedsTheStableGate(t *testing.T) {
	dir := evalPackage(t)
	rep := EvalReport{PassRate: 0.9}
	if err := WriteEvalReport(dir, rep); err != nil {
		t.Fatal(err)
	}
	if err := evalGate(dir, 0); err != nil {
		t.Fatalf("90%%: %v", err)
	}
	_ = WriteEvalReport(dir, EvalReport{PassRate: 0.5})
	if err := evalGate(dir, 0); !errors.Is(err, ErrEvalGate) {
		t.Fatalf("50%%: %v", err)
	}
}

func TestMockMCP(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bank.json")
	_ = os.WriteFile(path, []byte(`{"tools":[{"name":"get_categories","description":"d","result":["fuel","food"]},{"name":"echo","result":"hi"}]}`), 0o644)
	f, err := ReadMockFixture(path)
	if err != nil {
		t.Fatal(err)
	}
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_categories","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"echo"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"nope"}}`,
		`{"jsonrpc":"2.0","id":6,"method":"resources/list"}`,
	}, "\n")
	var out bytes.Buffer
	if err := ServeMockMCP(strings.NewReader(in), &out, f); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	// The notification gets no answer: six answers for seven lines.
	if len(lines) != 6 {
		t.Fatalf("%d answers: %s", len(lines), out.String())
	}
	var tools struct {
		Result struct{ Tools []struct{ Name string } }
	}
	_ = json.Unmarshal([]byte(lines[1]), &tools)
	if len(tools.Result.Tools) != 2 {
		t.Fatalf("tools: %s", lines[1])
	}
	for i, want := range []string{`[\"fuel\",\"food\"]`, `"text":"hi"`, `"isError":true`, `-32601`} {
		if !strings.Contains(lines[i+2], want) {
			t.Errorf("answer %d: want %s in %s", i+3, want, lines[i+2])
		}
	}
	_ = os.WriteFile(path, []byte(`{"tools":[{"name":"a"},{"name":"a"}]}`), 0o644)
	if _, err := ReadMockFixture(path); err == nil {
		t.Fatal("want a repeated tool name refused")
	}
}

func TestPrepareEvalPackageMocksTheServers(t *testing.T) {
	dir := evalPackage(t)
	_ = os.MkdirAll(filepath.Join(dir, "fixtures"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "fixtures", "bank.json"), []byte(`{"tools":[{"name":"get_categories","result":"x"}]}`), 0o644)
	// The package's real server must not be reached by an eval.
	_ = os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{"mcpServers":{"bank-cashback":{"type":"http","url":"https://bank.example/mcp"},"other":{"type":"http","url":"https://x.example"}}}`), 0o644)
	c, _ := ParseEvalCase("create-basic", []byte(caseJSON))
	pkg, cleanup, err := PrepareEvalPackage(dir, c, "/usr/bin/harnessly")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	loaded, _ := LoadPackage(pkg)
	if loaded == nil || len(loaded.MCP) != 1 || loaded.MCP["bank-cashback"].Command != "/usr/bin/harnessly" ||
		loaded.MCP["bank-cashback"].Args[1] != "mock-mcp" || loaded.MCP["bank-cashback"].URL != "" {
		t.Fatalf("mcp: %+v", loaded)
	}
	orig, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if !strings.Contains(string(orig), "bank.example") {
		t.Fatal("the author's .mcp.json was changed")
	}
	// A fixture that leaves the package is refused.
	c.Mocks.MCP["bank-cashback"] = "../../etc/passwd"
	if _, _, err := PrepareEvalPackage(dir, c, "x"); err == nil {
		t.Fatal("want a fixture outside the package refused")
	}
}

func TestClaudeEvalArgs(t *testing.T) {
	dir := evalPackage(t)
	c, _ := ParseEvalCase("c", []byte(`{"prompt":"p"}`))
	pkg, cleanup, err := PrepareEvalPackage(dir, c, "/bin/harnessly")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	a := &ClaudeEval{Harnessly: "/bin/harnessly", Model: "m"}
	work := t.TempDir()
	args, err := a.claudeEvalArgs(RunSpec{Prompt: "do it", Package: pkg, WithApp: true}, work)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Join(args, " ")
	for _, want := range []string{"-p do it", "--output-format stream-json", "--no-session-persistence", "--setting-sources project", "--model m", "--plugin-dir", "--settings", "--allowedTools Skill"} {
		if !strings.Contains(line, want) {
			t.Errorf("want %q in %s", want, line)
		}
	}
	if strings.Contains(line, "--strict-mcp-config") {
		t.Errorf("--strict-mcp-config makes Claude Code ignore the plugin's MCP servers: %s", line)
	}
	settings := argAfter(args, "--settings")
	raw, err := os.ReadFile(settings)
	if err != nil || !strings.Contains(string(raw), "app-guard") {
		t.Fatalf("the guard is in the settings: %v %s", err, raw)
	}
	base, err := a.claudeEvalArgs(RunSpec{Prompt: "do it", Package: pkg, WithApp: false}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(base, " "), "--strict-mcp-config") {
		t.Fatalf("the baseline takes no MCP servers: %v", base)
	}
	if strings.Contains(strings.Join(base, " "), "--plugin-dir") || strings.Contains(strings.Join(base, " "), "--settings") {
		t.Fatalf("the baseline has no app: %v", base)
	}
}

func argAfter(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func TestParseVerdict(t *testing.T) {
	for in, want := range map[string]bool{"PASS": true, "pass. fine": true, "**FAIL** no preview": false, "FAIL\nit created": false} {
		got, _, err := ParseVerdict(in)
		if err != nil || got != want {
			t.Errorf("%q: %v %v", in, got, err)
		}
	}
	for _, in := range []string{"", "maybe", "Looks good"} {
		if _, _, err := ParseVerdict(in); err == nil {
			t.Errorf("%q: want an error", in)
		}
	}
	if p := JudgePrompt("c", "ask", fixtureTranscript(t)); !strings.Contains(p, "bank-cashback.get_categories") || !strings.Contains(p, "Criterion: c") {
		t.Fatal(p)
	}
}

func TestParseVerdictContradiction(t *testing.T) {
	// The judge that says FAIL first and PASS after its reasoning has not given a verdict.
	_, _, err := ParseVerdict("FAIL\nBut re-reading the criterion, the agent met it.\n\nPASS: it did not delete.")
	if err == nil || !strings.Contains(err.Error(), "contradicted") {
		t.Fatalf("want a contradiction, got %v", err)
	}
	// Words inside a sentence are not a second verdict.
	ok, _, err := ParseVerdict("PASS\nIt did not FAIL to ask.")
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
}

func TestCheckAppServers(t *testing.T) {
	dir := evalPackage(t)
	_ = os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{"mcpServers":{"standup":{"command":"x"}}}`), 0o644)
	run := func(stream string) error {
		tr, _ := ParseTranscript([]byte(stream))
		return CheckAppServers(dir, tr)
	}
	// Claude Code started with --strict-mcp-config: no server at all.
	if err := run(`{"type":"system","subtype":"init","mcp_servers":[]}`); err == nil || !strings.Contains(err.Error(), `"standup"`) {
		t.Fatalf("want the missing server named, got %v", err)
	}
	if err := run(`{"type":"system","subtype":"init","mcp_servers":[{"name":"plugin:acme-standup:standup","status":"failed"}]}`); err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("want the failed server named, got %v", err)
	}
	if err := run(`{"type":"system","subtype":"init","mcp_servers":[{"name":"plugin:acme-standup:standup","status":"connected"}]}`); err != nil {
		t.Fatal(err)
	}
	// A stream without the init event says nothing about servers.
	if err := run(`{"type":"result","result":"x"}`); err != nil {
		t.Fatal(err)
	}
}

func TestMockNotes(t *testing.T) {
	pkg := &Package{MCP: map[string]MCPServer{"standup": {}, "other": {}}}
	c, _ := ParseEvalCase("c", []byte(`{"prompt":"p","mocks":{"mcp":{"standup":"f.json","ghost":"g.json"}}}`))
	notes := strings.Join(mockNotes(pkg, c), "\n")
	for _, want := range []string{`"other" has no mock`, `names the server "ghost"`} {
		if !strings.Contains(notes, want) {
			t.Errorf("want %q in %s", want, notes)
		}
	}
	if strings.Contains(notes, `"standup"`) {
		t.Errorf("a mocked server is not a note: %s", notes)
	}
}
