package agenteval

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestParseScenarioRefusesWhatCannotBeJudged(t *testing.T) {
	for name, body := range map[string]string{
		"no id":         "prompt: x\nexpect: [{called: a}]\n",
		"no prompt":     "id: a1\nexpect: [{called: a}]\n",
		"no expect":     "id: a1\nprompt: x\n",
		"two kinds":     "id: a1\nprompt: x\nexpect: [{called: a, images: 1}]\n",
		"bad regexp":    "id: a1\nprompt: x\nexpect: [{called: '('}]\n",
		"unknown field": "id: a1\nprompt: x\nexpect: [{called: a}]\nwhatever: 1\n",
		"args alone":    "id: a1\nprompt: x\nexpect: [{args: {a: b}}]\n",
		"escaping file": "id: a1\nprompt: x\nsetup: {files: {'../x': y}}\nexpect: [{called: a}]\n",
		"bad agent":     "id: a1\nagents: [gpt]\nprompt: x\nexpect: [{called: a}]\n",
	} {
		if _, err := ParseScenario(name, []byte(body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	s, err := ParseScenario("ok", []byte("id: a1\nprompt: x\nvariants: [{id: v1, text: y}]\nexpect: [{called: a}]\n"))
	if err != nil || s.Set != SetDev || len(s.Prompts()) != 2 || s.Timeout() != defaultTimeoutS {
		t.Fatalf("%v %+v", err, s)
	}
}

func TestTheRepositoryScenariosRead(t *testing.T) {
	list, err := Load(filepath.Join("..", "..", "evals", "agent"))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 3 {
		t.Fatalf("%d scenarios", len(list))
	}
	for _, s := range list {
		for _, d := range s.Setup.Design {
			if _, err := os.Stat(filepath.Join(s.Dir(), d)); err != nil {
				t.Errorf("%s: fixture %s: %v", s.ID, d, err)
			}
		}
	}
}

const transcript = `{"type":"assistant","message":{"id":"m1","model":"claude-haiku-5-5","content":[{"type":"tool_use","id":"t1","name":"generate_images","input":{"prompt":"cat"}}],"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":100}}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","is_error":true,"content":"<tool_use_error>Error: No such tool available: generate_images</tool_use_error>"}]}}
{"type":"assistant","message":{"id":"m2","model":"claude-haiku-5-5","content":[{"type":"tool_use","id":"t2","name":"mcp__harnessly__generate_images","input":{"prompt":"A cat in a hat"}}],"usage":{"input_tokens":3,"output_tokens":7}}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","content":[{"type":"text","text":"job_id: j1\nDone: 1 picture(s)."}]}]}}
{"type":"assistant","message":{"id":"m2","model":"claude-haiku-5-5","content":[{"type":"tool_use","id":"t2","name":"mcp__harnessly__generate_images","input":{"prompt":"A cat in a hat"}}]}}
{"type":"assistant","message":{"id":"m3","model":"claude-haiku-5-5","content":[{"type":"text","text":"Готово, кот в шляпе в ленте."}]}}
{"type":"assistant","isApiErrorMessage":true,"message":{"id":"m4","model":"<synthetic>","content":[{"type":"text","text":"API Error: 529 overloaded"}]}}
`

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadClaudeTranscript(t *testing.T) {
	f, err := ReadClaudeTranscript(writeTemp(t, "s.jsonl", transcript))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 2 || f.Calls[0].Name != "generate_images" || !f.Calls[0].Error || f.Calls[1].Name != "harnessly.generate_images" || f.Calls[1].Error {
		t.Fatalf("calls %+v", f.Calls)
	}
	if !strings.Contains(f.Calls[1].Result, "Done") || f.Final != "Готово, кот в шляпе в ленте." || f.Model != "claude-haiku-5-5" {
		t.Fatalf("%+v", f)
	}
	if f.TokensIn != 13 || f.CacheRead != 100 || f.TokensOut != 12 || len(f.APIErrors) != 1 {
		t.Fatalf("tokens %d %d %d, api errors %v", f.TokensIn, f.CacheRead, f.TokensOut, f.APIErrors)
	}
}

func TestJudgeByFacts(t *testing.T) {
	s, err := ParseScenario("s", []byte(`id: pic
prompt: нарисуй кота
expect:
  - called: harnessly\.generate_images
    args: {prompt: '(?i)cat'}
  - not_called: Write
    args: {file_path: '\.py$'}
  - images: 1
  - pane: {tab: design, target: shop, place: '(?i)оплат'}
  - file: {glob: 'design:shop/project/*.dc.html', contains: Оплатить}
  - text_not: '(?i)пароль'
  - any_of:
      - called: harnessly\.deliver_file
      - called: Bash
        args: {command: 'harnessly deliver'}
  - max_calls: 3
`))
	if err != nil {
		t.Fatal(err)
	}
	f, _ := ReadClaudeTranscript(writeTemp(t, "s.jsonl", transcript))
	f.Calls = append(f.Calls, Call{Name: "Write", Args: []byte(`{"file_path":"/w/draw.txt"}`)}, Call{Name: "Bash", Args: []byte(`{"command":"harnessly deliver /w/r.md"}`)})
	f.Images = []ImageJob{{State: "done", Images: 1}}
	f.Pane = []PaneIntent{{Kind: "open", Tab: "files", Target: "/w"}, {Kind: "open", Tab: "design", Target: "shop", Place: "Оплата.dc.html"}}
	f.Files = []FileFact{{Path: "design:shop/project/Оплата.dc.html", Text: "<button>Оплатить</button>"}}
	v := s.Check(f)
	for i, x := range v[:7] {
		if !x.OK {
			t.Errorf("%d %s: %s", i, x.Name, x.Why)
		}
	}
	if v[7].OK || Passed(v) {
		t.Fatalf("max_calls held with %d calls", len(f.Calls))
	}
	// The failure says what happened instead.
	f.Pane = f.Pane[:1]
	f.Calls = f.Calls[:1]
	v = s.Check(f)
	if v[0].OK || !strings.Contains(v[0].Why, "calls were: generate_images") || v[3].OK || !strings.Contains(v[3].Why, "files /w") {
		t.Fatalf("%+v", v)
	}
}

func TestWilsonAndChange(t *testing.T) {
	lo, hi := Wilson(5, 5)
	if math.Abs(lo-0.566) > 0.01 || hi != 1 {
		t.Fatalf("5/5: %v %v", lo, hi)
	}
	lo, hi = Wilson(0, 10)
	if lo != 0 || math.Abs(hi-0.278) > 0.01 {
		t.Fatalf("0/10: %v %v", lo, hi)
	}
	if Change(3, 3, 2, 3, 0) != "same" || Change(0, 10, 10, 10, 0) != "better" || Change(10, 10, 0, 10, 0) != "worse" || Change(0, 0, 1, 1, 0) != "new" {
		t.Fatal("change labels")
	}
	if math.Abs(PassHatK(4, 5, 3)-0.4) > 1e-9 || PassHatK(2, 5, 3) != 0 {
		t.Fatalf("pass^3: %v %v", PassHatK(4, 5, 3), PassHatK(2, 5, 3))
	}
}

func TestCellsAndReport(t *testing.T) {
	s, _ := ParseScenario("s", []byte("id: a1\nagents: [claude]\nprompt: x\nvariants: [{id: v1, text: y}, {id: v2, text: z}]\nexpect: [{called: a}]\n"))
	o := Options{Scenarios: []*Scenario{s}, Models: []Model{{Key: "haiku", Agent: "claude", Model: "haiku"}, {Key: "codex:m", Agent: "codex", Model: "m"}},
		Variants: "1", Repeats: 2}
	cells := o.Cells()
	if len(cells) != 4 || cells[0].Key("x") == cells[1].Key("x") || cells[0].Key("x") == cells[0].Key("y") {
		t.Fatalf("%d cells", len(cells))
	}
	rs := []Result{{Scenario: "a1", Model: "haiku", State: Pass}, {Scenario: "a1", Model: "haiku", State: Fail, Verdicts: []Verdict{{Name: "x", Why: "y"}}},
		{Scenario: "a1", Model: "haiku", State: Infra}}
	rep := Summarize(rs, []Result{{Scenario: "a1", Model: "haiku", State: Pass}}, 0)
	if len(rep.Cells) != 1 || rep.Cells[0].Pass != 1 || rep.Cells[0].Runs != 2 || rep.Infra != 1 || rep.Cells[0].Before != "1/1" {
		t.Fatalf("%+v", rep)
	}
	md := rep.Markdown("t")
	if !strings.Contains(md, "1/2 [") || !strings.Contains(md, "## Failures") {
		t.Fatal(md)
	}
}

func TestCodexRollout(t *testing.T) {
	body := `{"type":"turn_context","payload":{"model":"gpt-5.6-sol"}}
{"type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{\"command\":[\"bash\",\"-lc\",\"harnessly deliver report.md\"]}","call_id":"c1"}}
{"type":"response_item","payload":{"type":"function_call_output","call_id":"c1","output":"{\"output\":\"ok\",\"metadata\":{\"exit_code\":0}}"}}
{"type":"response_item","payload":{"type":"function_call","name":"mcp__harnessly__generate_images","arguments":"{\"prompt\":\"cat\"}","call_id":"c2"}}
{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Готово"}]}}
`
	f, err := ReadCodexRollout(writeTemp(t, "r.jsonl", body))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 2 || f.Calls[0].Name != "Bash" || !strings.Contains(string(f.Calls[0].Args), "harnessly deliver report.md") || f.Calls[0].Error ||
		f.Calls[1].Name != "harnessly.generate_images" || f.Final != "Готово" || f.Model != "gpt-5.6-sol" {
		t.Fatalf("%+v", f)
	}
}

// Without Landlock no stand starts, and nothing is created: there is no flag around it.
func TestUpRefusesWithoutSandbox(t *testing.T) {
	old := sandboxLevel
	sandboxLevel = func() int { return 0 }
	defer func() { sandboxLevel = old }()
	root := t.TempDir()
	s, err := Up(StandConfig{Root: root}, "s1", 19999)
	if err == nil || s != nil {
		t.Fatalf("a stand started without the sandbox: %v", err)
	}
	if !strings.Contains(err.Error(), "Landlock") {
		t.Errorf("refusal does not say why: %v", err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Errorf("refused stand left %d entries", len(entries))
	}
}

// A Codex login copy gets an empty refresh token (Codex refuses one without the field) and a fresh last_refresh; a real
// refresh token never passes through.
func TestCodexStandAuth(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	b, err := codexStandAuth([]byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"a","refresh_token":"real"},"last_refresh":"2026-01-01T00:00:00Z"}`), now)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Tokens      map[string]string `json:"tokens"`
		LastRefresh string            `json:"last_refresh"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if v, ok := got.Tokens["refresh_token"]; !ok || v != "" {
		t.Errorf("refresh_token = %q, present %v", v, ok)
	}
	if got.Tokens["access_token"] != "a" || !strings.HasPrefix(got.LastRefresh, "2026-11-15T12:00:00") {
		t.Errorf("copy lost what it needs: %+v", got)
	}
}

// Codex's code mode runs a script that calls tools; each tools.<name>(…) is judged as a call of its own.
func TestCodexCodeModeCalls(t *testing.T) {
	script := `const r = await tools.exec_command({cmd:"harnessly deliver './a, b.md'","workdir":"/w","yield_time_ms":10000}); text(r.output);
await tools.mcp__harnessly__deliver_file({path: "x.md"});`
	line := func(v any) string { b, _ := json.Marshal(v); return string(b) + "\n" }
	body := line(map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "exec", "call_id": "c1", "input": script}}) +
		line(map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call_output", "call_id": "c1", "output": "Delivered"}})
	f, err := ReadCodexRollout(writeTemp(t, "r.jsonl", body))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 2 {
		t.Fatalf("calls = %+v", f.Calls)
	}
	if f.Calls[0].Name != "Bash" || !strings.Contains(string(f.Calls[0].Args), `harnessly deliver './a, b.md'`) || f.Calls[0].Result != "Delivered" {
		t.Errorf("exec_command = %+v", f.Calls[0])
	}
	if !strings.Contains(f.Calls[1].Name, "deliver_file") || !strings.Contains(string(f.Calls[1].Args), `"path":"x.md"`) && !strings.Contains(string(f.Calls[1].Args), `"path": "x.md"`) {
		t.Errorf("mcp call = %+v %s", f.Calls[1], f.Calls[1].Args)
	}
}

func TestMatchPathAndAffected(t *testing.T) {
	for _, c := range []struct {
		glob, path string
		want       bool
	}{
		{"cmd/runtime/agent_skills*.go", "cmd/runtime/agent_skills.go", true},
		{"cmd/runtime/agent_skills*.go", "cmd/runtime/agent_skills_design_test.go", true},
		{"internal/*/tools.go", "internal/delivery/tools.go", true},
		{"internal/*/tools.go", "internal/a/b/tools.go", false},
		{"skills/**", "skills/a/b/SKILL.md", true},
		{"**/*_test.go", "internal/x/y_test.go", true},
	} {
		if got := MatchPath(c.glob, c.path); got != c.want {
			t.Errorf("MatchPath(%q, %q) = %v", c.glob, c.path, got)
		}
	}
	surface := []string{"cmd/runtime/agent_skills*.go", "internal/delivery/**", "!**/*_test.go"}
	sc := []*Scenario{{ID: "file-as-card", Covers: []string{"internal/delivery/**"}}, {ID: "other", Covers: []string{"web/**"}}}
	im := Affected([]string{"internal/delivery/tools.go", "README.md"}, surface, sc, "evals/agent")
	if !im.NeedsScenario() || len(im.Scenarios) != 1 || im.Scenarios[0] != "file-as-card" {
		t.Errorf("tool changed alone: %+v", im)
	}
	im = Affected([]string{"internal/delivery/tools.go", "evals/agent/file-as-card.yaml"}, surface, sc, "evals/agent")
	if im.NeedsScenario() {
		t.Errorf("a scenario came with it: %+v", im)
	}
	if im = Affected([]string{"cmd/runtime/agent_skills_design_test.go"}, surface, sc, "evals/agent"); len(im.Surface) != 0 {
		t.Errorf("a test is not the surface: %+v", im)
	}
}

// Every scenario of the repository says what it covers, and the surface file reads.
func TestRepositoryScenariosCover(t *testing.T) {
	list, err := Load(filepath.Join("..", "..", "evals", "agent"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if len(s.Covers) == 0 {
			t.Errorf("%s names no covers", s.ID)
		}
	}
	if g, err := ReadSurface(filepath.Join("..", "..", "evals", "agent", "surface.txt")); err != nil || len(g) == 0 {
		t.Errorf("surface: %v %v", g, err)
	}
}

// The person's chat is the session that started first, though a delegated chat in the same folder wrote last.
func TestTranscriptFileIsThePersonsChat(t *testing.T) {
	s := &Stand{Home: t.TempDir(), Work: "/w/work"}
	dir := filepath.Join(s.Home, ".claude", "projects", nonAlnum.ReplaceAllString(s.Work, "-"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	parent, child := filepath.Join(dir, "p.jsonl"), filepath.Join(dir, "c.jsonl")
	_ = os.WriteFile(parent, []byte(`{"type":"mode"}`+"\n"+`{"type":"user","timestamp":"2026-11-15T12:00:00Z"}`+"\n"), 0o600)
	_ = os.WriteFile(child, []byte(`{"type":"user","timestamp":"2026-11-15T12:00:30Z"}`+"\n"), 0o600)
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(child, old, old.Add(2*time.Hour))
	if got := s.transcriptFile(); got != parent {
		t.Errorf("transcript = %s, want the person's chat", got)
	}
}

func TestSecretScenarioTextNot(t *testing.T) {
	list, err := Load(filepath.Join("..", "..", "evals", "agent"))
	if err != nil {
		t.Fatal(err)
	}
	var re string
	for _, s := range list {
		if s.ID == "secret-through-safe" {
			for _, e := range s.Expect {
				if e.TextNot != "" {
					re = e.TextNot
				}
			}
		}
	}
	rx := regexp.MustCompile(re)
	for _, bad := range []string{"Пришлите токен сюда", "вставь токен в чат", "paste the token here", "скиньте мне токен в чат"} {
		if !rx.MatchString(bad) {
			t.Errorf("missed %q", bad)
		}
	}
	for _, good := range []string{"Вставьте токен туда, а не в чат", "Paste the token there", "вставьте токен в карточку, а не в чат"} {
		if rx.MatchString(good) {
			t.Errorf("flagged %q", good)
		}
	}
}
