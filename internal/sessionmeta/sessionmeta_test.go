package sessionmeta

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func feedAll(lines ...string) Info {
	s := &scan{}
	for _, l := range lines {
		s.feed([]byte(l))
	}
	return s.info
}

const (
	req    = `{"type":"api-request","sessionId":"s","timestamp":"2026-10-24T10:00:00.000Z","querySource":"repl_main_thread","params":{"model":"claude-opus-5-5","output_config":{"effort":"%s"}}}`
	stdout = `{"type":"user","message":{"role":"user","content":"<local-command-stdout>%s</local-command-stdout>"},"timestamp":"2026-10-24T10:01:00.000Z"}`
)

func request(effort string) string { return strings.Replace(req, "%s", effort, 1) }
func said(text string) string      { return strings.Replace(stdout, "%s", text, 1) }

func TestEffortIsWhatTheLastRequestWentOutWith(t *testing.T) {
	if got := feedAll(request("high"), request("medium")).Effort; got != "medium" {
		t.Fatalf("got %q", got)
	}
	if got := feedAll(`{"type":"user","message":{"content":"hi"}}`).Effort; got != "" {
		t.Fatalf("an unknown effort is not made up: %q", got)
	}
}

func TestSideRequestsDoNotCount(t *testing.T) {
	side := strings.Replace(request("low"), "repl_main_thread", "agent:builtin:general-purpose", 1)
	if got := feedAll(request("high"), side).Effort; got != "high" {
		t.Fatalf("got %q", got)
	}
}

func TestEffortCommandsMoveItBeforeTheNextRequest(t *testing.T) {
	got := feedAll(request("medium"), said("Set effort level to high (saved as your default for new sessions): Comprehensive")).Effort
	if got != "high" {
		t.Fatalf("got %q", got)
	}
}

// Ultracode is a mode beside the effort (Claude Code 2.1.310): its own lines turn it on and off and say the level that
// stays; a level set leaves it, unless the /effort dialog's toggle says otherwise after it.
func TestUltracode(t *testing.T) {
	cases := map[string]struct {
		lines  []string
		ultra  Tri
		effort string
	}{
		"never said":                {[]string{request("xhigh")}, Unknown, "xhigh"},
		"turned on at a level":      {[]string{request("high"), said("Ultracode on (this session only): dynamic workflows on every task. Effort stays medium.")}, On, "medium"},
		"turned on, then a request": {[]string{said("Ultracode on (this session only): dynamic workflows on every task. Effort stays xhigh."), request("xhigh")}, On, "xhigh"},
		"turned off":                {[]string{said("Ultracode on (this session only): x. Effort stays high."), request("high"), said("Ultracode off. Effort stays high.")}, Off, "high"},
		"a level leaves it":         {[]string{said("Ultracode on (this session only): x. Effort stays xhigh."), said("Set effort level to medium (this session only): Balanced")}, On, "medium"},
		"the dialog's toggle":       {[]string{said("Ultracode on (this session only): x. Effort stays xhigh."), said("Set effort level to high (saved as your default for new sessions): Comprehensive · Ultracode off")}, Off, "high"},
		"auto, toggled off":         {[]string{said("Ultracode on (this session only): x. Effort stays xhigh."), said("Effort level set to auto · Ultracode off")}, Off, "xhigh"},
	}
	for name, c := range cases {
		info := feedAll(c.lines...)
		if info.Ultracode != c.ultra || info.Effort != c.effort {
			t.Errorf("%s: got %v %q, want %v %q", name, info.Ultracode, info.Effort, c.ultra, c.effort)
		}
	}
}

func TestQuotedCommandOutputIsNotACommand(t *testing.T) {
	quote := `{"type":"user","message":{"content":[{"type":"tool_result","content":"<local-command-stdout>Set effort level to low</local-command-stdout>"}]}}`
	if got := feedAll(request("high"), quote).Effort; got != "high" {
		t.Fatalf("got %q", got)
	}
}

func goalLine(met bool, sentinel bool, text string) string {
	m, s := "false", ""
	if met {
		m = "true"
	}
	if sentinel {
		s = `"sentinel":true,`
	}
	return `{"type":"attachment","timestamp":"2026-10-23T00:02:09.809Z","attachment":{"type":"goal_status","met":` + m + `,` + s + `"condition":"` + text + `"}}`
}

func TestGoalLifecycle(t *testing.T) {
	paused := `{"type":"system","subtype":"informational","content":"Goal paused · goal checks kept finding it unmet this turn · send a message to continue","timestamp":"2026-10-23T03:50:03.023Z"}`
	g := feedAll(goalLine(false, true, "ship it")).Goal
	if g == nil || g.Text != "ship it" || g.Status != GoalActive || g.Since == 0 {
		t.Fatalf("set: %+v", g)
	}
	if g = feedAll(goalLine(false, true, "ship it"), goalLine(false, false, "ship it"), paused).Goal; g.Status != GoalPaused {
		t.Fatalf("paused: %+v", g)
	}
	if g = feedAll(goalLine(false, true, "ship it"), paused, goalLine(false, true, "ship it")).Goal; g.Status != GoalActive {
		t.Fatalf("taken up again: %+v", g)
	}
	if g = feedAll(goalLine(false, true, "ship it"), goalLine(true, false, "ship it")).Goal; g.Status != GoalMet {
		t.Fatalf("met: %+v", g)
	}
	cleared := `{"type":"system","subtype":"informational","content":"Goal cleared: ship it","timestamp":"2026-10-23T05:00:00.000Z"}`
	if g = feedAll(goalLine(false, true, "ship it"), cleared).Goal; g != nil {
		t.Fatalf("cleared: %+v", g)
	}
	if g = feedAll(goalLine(false, true, "ship it"), goalLine(false, true, "ship more")).Goal; g.Text != "ship more" {
		t.Fatalf("replaced: %+v", g)
	}
}

func TestReaderGoesOnFromWhatItRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	write := func(s string) {
		f, _ := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		defer f.Close()
		_, _ = f.WriteString(s)
	}
	var r Reader
	write(request("medium") + "\n")
	if got := r.Read(path).Effort; got != "medium" {
		t.Fatalf("got %q", got)
	}
	write(request("high") + "\n" + goalLine(false, true, "g") + "\n")
	got := r.Read(path)
	if got.Effort != "high" || got.Goal == nil {
		t.Fatalf("got %+v", got)
	}
	write(request("max")) // a line still being written
	if got := r.Read(path).Effort; got != "high" {
		t.Fatalf("got %q", got)
	}
}

func TestAgentReadsItsFiles(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "sess.jsonl")
	jsonl, meta := AgentFiles(transcript, "a1")
	if err := os.MkdirAll(filepath.Dir(jsonl), 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(meta, []byte(`{"agentType":"Explore","model":"sonnet","finished":false}`), 0o600)
	_ = os.WriteFile(jsonl, []byte(`{"type":"user","message":{"content":"go"}}
{"type":"api-request","querySource":"agent:builtin:Explore","params":{"model":"claude-sonnet-5-5","output_config":{"effort":"high"}}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Looking"},{"type":"tool_use","name":"Read","input":{"file_path":"/home/coder/workspace/harnessly/a.go"}}]}}
`), 0o600)
	got := ReadAgent(transcript, "a1")
	want := Agent{Model: "claude-sonnet-5-5", Effort: "high", Role: "Explore", Last: "Read · harnessly/a.go"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if none := ReadAgent(transcript, "nope"); none != (Agent{}) {
		t.Fatalf("a subagent without files: %+v", none)
	}
}

func TestGoalSetAtFindsTheShortLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	long := `{"type":"user","message":{"content":"quote: Goal set: ship it"},"pad":"` + strings.Repeat("x", 3000) + `"}`
	set := `{"type":"queue-operation","operation":"enqueue","timestamp":"2026-10-23T00:02:12.227Z","content":"Goal set: ship it"}`
	body := set + "\n" + long + "\n" + goalLine(false, true, "ship it") + "\n"
	_ = os.WriteFile(path, []byte(body), 0o600)
	if at := goalSetAt(path, int64(len(body)), "ship it"); at != millis("2026-10-23T00:02:12.227Z") {
		t.Fatalf("got %d", at)
	}
	if at := goalSetAt(path, int64(len(body)), "another"); at != 0 {
		t.Fatalf("got %d", at)
	}
}

func TestRecapIsTheLatestAwaySummaryWithoutTheHint(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "recorded-2.1.284-away-summaries.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if r := feedAll(lines[0]).Recap; r == nil || r.At != millis("2026-10-08T21:42:38.361Z") || strings.Contains(r.Text, "disable recaps") ||
		!strings.HasPrefix(r.Text, "Ты просил разобраться") || !strings.HasSuffix(r.Text, "заказ не пришёл.") {
		t.Fatalf("first: %+v", r)
	}
	r := feedAll(lines...).Recap
	if r == nil || r.At != millis("2026-10-09T00:10:55.294Z") || !strings.HasPrefix(r.Text, "Цель — убрать массовые ошибки") || strings.Contains(r.Text, "/config") {
		t.Fatalf("latest: %+v", r)
	}
	if got := feedAll(`{"type":"user","message":{"content":"hi"}}`).Recap; got != nil {
		t.Fatalf("a recap out of nowhere: %+v", got)
	}
	empty := `{"type":"system","subtype":"away_summary","content":" (disable recaps in /config)","timestamp":"2026-10-09T00:10:55.294Z"}`
	if got := feedAll(lines[0], empty).Recap; got == nil || got.At != millis("2026-10-08T21:42:38.361Z") {
		t.Fatalf("an empty recap replaces the last one: %+v", got)
	}
}

func TestReaderCarriesTheRecapOnFromTheTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	raw, _ := os.ReadFile(filepath.Join("testdata", "recorded-2.1.284-away-summaries.jsonl"))
	_ = os.WriteFile(path, raw, 0o600)
	var r Reader
	if got := r.Read(path).Recap; got == nil || !strings.HasPrefix(got.Text, "Цель — убрать") {
		t.Fatalf("got %+v", got)
	}
}

func TestTitleIsTheLatestAITitleAndAPersonsOwnBeatsIt(t *testing.T) {
	ai := func(s string) string { return `{"type":"ai-title","aiTitle":"` + s + `","sessionId":"s"}` }
	custom := `{"type":"custom-title","customTitle":"acme-ui-kit","sessionId":"s"}`
	if got := feedAll(`{"type":"user","message":{"content":"hi"}}`).Title; got != "" {
		t.Fatalf("a title out of nowhere: %q", got)
	}
	if got := feedAll(ai("Доступ к dev-машине"), ai("Доступ к dev-машине Acme Admin")).Title; got != "Доступ к dev-машине Acme Admin" {
		t.Fatalf("latest ai-title: %q", got)
	}
	if got := feedAll(ai("A"), custom, ai("B")).Title; got != "acme-ui-kit" {
		t.Fatalf("custom title: %q", got)
	}
	if got := feedAll(ai("A"), `{"type":"ai-title","aiTitle":"  ","sessionId":"s"}`).Title; got != "A" {
		t.Fatalf("a blank title replaces nothing: %q", got)
	}
	// Quoted inside a message the words are not a title line.
	if got := feedAll(`{"type":"user","message":{"content":"{\"type\":\"ai-title\",\"aiTitle\":\"x\"}"}}`).Title; got != "" {
		t.Fatalf("quoted: %q", got)
	}
}

// recorded is the ultracode lines of a Claude Code 2.1.309–2.1.310 transcript, in their order: turned on by the
// command and confirmed by its reminder, kept over a compaction, turned off by the /effort dialog's toggle and its exit
// reminder, turned on again by the command.
func recorded(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "recorded-2.1.310-ultracode.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

// The mode is the last word of the transcript on it, Claude Code's own reminders among them: on, off, on again.
func TestUltracodeIsTheLastWordOnIt(t *testing.T) {
	lines := recorded(t)
	wants := []Tri{Unknown, Unknown, On, On, On, On, Off, Off, On}
	for i, want := range wants {
		if got := feedAll(lines[:i+1]...).Ultracode; got != want {
			t.Errorf("after line %d: got %v, want %v", i+1, got, want)
		}
	}
	exit := `{"type":"attachment","attachment":{"type":"ultra_effort_exit"},"timestamp":"2026-10-25T01:31:00.000Z"}`
	if got := feedAll(append(lines, exit)...).Ultracode; got != Off {
		t.Fatalf("the exit reminder turns it off: %v", got)
	}
	// A skill's text that names the mode is no word on it.
	skill := `{"type":"attachment","attachment":{"type":"skill_listing","content":"Ultracode is on for the session (a system-reminder confirms it)"}}`
	if got := feedAll(lines[6], skill).Ultracode; got != Off {
		t.Fatalf("quoted: %v", got)
	}
}

// Claude Code keeps the mode for one process: a resumed or started process has not said it yet, a compaction keeps it.
func TestANewProcessHasNotSaidTheMode(t *testing.T) {
	lines := recorded(t)
	resume := strings.Replace(lines[0], "SessionStart:startup", "SessionStart:resume", 1)
	if got := feedAll(lines[2], resume).Ultracode; got != Unknown {
		t.Fatalf("after a resume: %v", got)
	}
	if got := feedAll(lines[2], lines[4]).Ultracode; got != On {
		t.Fatalf("after a compaction: %v", got)
	}
}

// A cold read takes the tail; the mode said only before it is still the mode (turned off hours ago, it is off), and
// what a process started in the tail has not said is not taken from the one before.
func TestAColdReadFindsTheModeBeforeTheTail(t *testing.T) {
	old := tailBytes
	tailBytes = 4 << 10
	t.Cleanup(func() { tailBytes = old })
	lines := recorded(t)
	// A request's copy of the conversation quotes the command's output: a long line, no word on the mode.
	blob := `{"type":"api-request-blob","content":"<local-command-stdout>Ultracode on (this session only): x. Effort stays high.</local-command-stdout>` + strings.Repeat("x", ultraLineMax) + `"}`
	pad := strings.Repeat(request("high")+"\n", 64)
	cases := map[string]struct {
		before []string
		tail   string
		want   Tri
	}{
		"off before the tail":            {lines[:8], pad, Off},
		"on again before the tail":       {lines, pad, On},
		"a quote after it is not a word": {append(slices.Clone(lines[:8]), blob), pad, Off},
		"never said":                     {lines[:2], pad, Unknown},
		"a process started in the tail":  {lines[:3], pad + strings.Replace(lines[0], "startup", "resume", 1) + "\n" + pad, Unknown},
	}
	for name, c := range cases {
		path := filepath.Join(t.TempDir(), "s.jsonl")
		body := strings.Join(c.before, "\n") + "\n" + c.tail
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		var r Reader
		if got := r.Read(path); got.Ultracode != c.want || got.Effort != "high" {
			t.Errorf("%s: got %v %q, want %v", name, got.Ultracode, got.Effort, c.want)
		}
	}
}
