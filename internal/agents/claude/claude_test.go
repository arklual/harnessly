package claude

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/proc"
	"github.com/ly-solutions/harnessly/internal/sessions"
)

func writeJSONL(t *testing.T, lines ...any) string {
	t.Helper()
	var b strings.Builder
	for _, l := range lines {
		raw, _ := json.Marshal(l)
		b.Write(raw)
		b.WriteByte('\n')
	}
	p := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

type m = map[string]any

func entries(t *testing.T, raw ...string) []*agents.Obj {
	t.Helper()
	var lines [][]byte
	for _, r := range raw {
		lines = append(lines, agents.Lines([]byte(r))...)
	}
	return Parse(lines)
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func str(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

// agent-runtime-snapshot: Claude JSONL snapshot behaviour behind the Claude provider.
func TestSnapshotFinishedTurn(t *testing.T) {
	p := writeJSONL(t, m{"timestamp": "2026-05-02T07:37:01.000Z", "type": "assistant",
		"message": m{"content": []any{m{"type": "text", "text": "Claude finished the task."}}, "stop_reason": "end_turn"}})
	s := NewSnapshotReader().Read(p, false)
	if !s.Idle || s.Stale || str(s.LastAssistantSnippet) != "Claude finished the task." || s.CurrentAction == nil ||
		s.CurrentAction.ToolName != nil || s.CurrentAction.Summary != "Claude finished the task." {
		t.Fatalf("%+v %+v", s, s.CurrentAction)
	}
	if s.LastEntryTs == nil || *s.LastEntryTs != 1777707421000 {
		t.Fatalf("lastEntryTs %v", s.LastEntryTs)
	}
}

const (
	assistantDone = `{"type":"assistant","timestamp":"2026-10-10T10:00:00.000Z","message":{"content":[{"type":"text","text":"Готово."}],"stop_reason":"end_turn"}}`
	prompt        = `{"type":"user","uuid":"p1","timestamp":"2026-10-10T10:01:00.000Z","message":{"content":"Сделай ещё"}}`
)

// claude-local-command-turn: local slash commands are not turns (TCK-2899).
func TestLocalCommandsAreNotTurns(t *testing.T) {
	scan := ScanLines(entries(t, assistantDone, fixture(t, "claude-2.1.282-effort-user-triplet.jsonl")), 1000)
	if !scan.Matched || !scan.Idle || scan.Interrupted {
		t.Fatalf("/effort: %+v", scan)
	}
	scan = ScanLines(entries(t, assistantDone, fixture(t, "claude-2.1.282-context-local-command.jsonl")), 1000)
	if !scan.Matched || !scan.Idle {
		t.Fatalf("/context: %+v", scan)
	}
	scan = ScanLines(entries(t, assistantDone, fixture(t, "claude-2.1.282-model-local-command.jsonl")), 1000)
	if !scan.Matched || !scan.Idle {
		t.Fatalf("/model: %+v", scan)
	}
	scan = ScanLines(entries(t, assistantDone, fixture(t, "claude-2.1.282-effort-user-triplet.jsonl"), prompt), 1000)
	if !scan.Matched || scan.Idle || !scan.Stale {
		t.Fatalf("a real prompt after a local command still starts a turn: %+v", scan)
	}
	notice := `{"type":"user","uuid":"n1","parentUuid":"x","isMeta":true,"timestamp":"2026-10-10T10:02:00.000Z","message":{"content":"Background task finished"}}`
	if scan := ScanLines(entries(t, assistantDone, notice), 1000); scan.Idle {
		t.Fatalf("a meta line that is not a command output still counts: %+v", scan)
	}
}

func TestScanLinesStates(t *testing.T) {
	streaming := `{"type":"assistant","timestamp":"2026-10-10T10:00:00.000Z","message":{"content":[{"type":"text","text":"…"}]}}`
	toolUse := `{"type":"assistant","message":{"content":[],"stop_reason":"tool_use"}}`
	interrupt := `{"type":"user","message":{"content":[{"type":"text","text":"[Request interrupted by user for tool use]"}]}}`
	summary := `{"type":"system","subtype":"stop_hook_summary"}`
	side := `{"type":"user","isSidechain":true,"message":{"content":"x"}}`
	for _, c := range []struct {
		name    string
		lines   []string
		elapsed float64
		want    ScanResult
	}{
		{"stream fresh", []string{streaming}, 1000, ScanResult{Matched: true, Stale: true, NeedsStaleRecheck: true, StaleMs: StaleInterrupted}},
		{"stream old", []string{streaming}, 20_001, ScanResult{Matched: true, Idle: true, Stale: true, StaleMs: StaleInterrupted}},
		{"tool use", []string{toolUse}, 1e6, ScanResult{Matched: true}},
		{"interrupt", []string{toolUse, interrupt}, 0, ScanResult{Matched: true, Idle: true, Interrupted: true}},
		{"hook summary", []string{prompt, summary, side}, 0, ScanResult{Matched: true, Idle: true}},
		{"prompt old", []string{prompt}, 90_001, ScanResult{Matched: true, Idle: true, Stale: true, StaleMs: StaleAwaitingAPI}},
		{"nothing", []string{`{"type":"progress"}`, `not json`}, 5, ScanResult{Stale: true, NeedsStaleRecheck: true, StaleMs: StaleAwaitingAPI}},
	} {
		got := ScanLines(entries(t, c.lines...), c.elapsed)
		got.LastEntryTs = nil
		if got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestExtractAssistant(t *testing.T) {
	edit := `{"type":"assistant","message":{"content":[{"type":"text","text":"Правлю"},{"type":"tool_use","name":"Edit","input":{"file_path":"/a.go","old_string":"a\nb\n","new_string":"a\nc\nd\n"}}]}}`
	a := ExtractAssistant(entries(t, edit))
	if a.Reset || str(a.LastSnippet) != "Правлю" || *a.CurrentAction.ToolName != "Edit" || a.CurrentAction.Summary != "Update /a.go (+2, -1)" {
		t.Fatalf("%+v %+v", a, a.CurrentAction)
	}
	toolResult := `{"type":"user","message":{"content":[{"type":"tool_result","content":"ok"}]}}`
	if a := ExtractAssistant(entries(t, edit, toolResult)); a.Reset || a.CurrentAction == nil {
		t.Fatal("a tool result is not a new prompt")
	}
	if a := ExtractAssistant(entries(t, edit, prompt)); !a.Reset || a.CurrentAction != nil || a.LastSnippet != nil {
		t.Fatalf("a prompt after the answer resets: %+v", a)
	}
	broken := `{"type":"assistant","message":{"content":[{"type":"text","text":"x"},null]}}`
	if a := ExtractAssistant(entries(t, edit, broken)); str(a.LastSnippet) != "Правлю" {
		t.Fatalf("a line with a null block is skipped as a whole: %+v", a)
	}
	long := strings.Repeat("ю", 250)
	a = ExtractAssistant(entries(t, `{"type":"assistant","message":{"content":[{"type":"text","text":"  `+long+`  "}]}}`))
	if str(a.LastSnippet) != strings.Repeat("ю", 200)+"…" {
		t.Fatal("snippet cut at 200")
	}
}

func TestSummarizeToolCall(t *testing.T) {
	obj := func(s string) any { v, _ := agents.Decode([]byte(s)); return v }
	for _, c := range []struct {
		name, input, want string
	}{
		{"Read", `{"file_path":"/x"}`, "Read /x"},
		{"Read", `{"path":"/y"}`, "Read /y"},
		{"Edit", `{"file_path":"/x","old_string":"","new_string":"a\nb"}`, "Create /x (+2, -0)"},
		{"Edit", `{"file_path":"/x","old_string":"a\r\nb\n","new_string":"a\nb\n"}`, "Update /x (+1, -1)"},
		{"Write", `{"file_path":"/x","content":"…"}`, "Write /x"},
		{"Bash", `{"command":"go test ./...\necho done","description":"d"}`, "$ go test ./..."},
		{"Grep", `{"pattern":"TODO"}`, `Grep "TODO"`},
		{"mcp__docs__search", `{"query":"` + strings.Repeat("q", 70) + `","limit":5}`, "mcp__docs__search " + strings.Repeat("q", 60)},
		{"Task", `{"7":"seven","description":"d"}`, "Task seven"},
		{"Task", `{"n":{"a":1}}`, "Task [object Object]"},
		{"Task", `{}`, "Task"},
		{"Task", `"abc"`, "Task a"},
	} {
		if got := SummarizeToolCall(c.name, obj(c.input)); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.name, c.input, got, c.want)
		}
	}
}

func TestSnapshotCacheAndEdges(t *testing.T) {
	r := NewSnapshotReader()
	now := time.Now()
	r.Now = func() time.Time { return now }
	empty := filepath.Join(t.TempDir(), "e.jsonl")
	_ = os.WriteFile(empty, nil, 0o600)
	if s := r.Read(empty, false); !s.Idle {
		t.Fatal("an empty transcript is idle")
	}
	if s := r.Read("/nonexistent/x.jsonl", false); s != (agents.Snapshot{}) {
		t.Fatal("unreadable says nothing")
	}
	p := writeJSONL(t, m{"type": "user", "message": m{"content": "go"}})
	mt := now.Add(-time.Second)
	_ = os.Chtimes(p, mt, mt)
	if s := r.Read(p, false); s.Idle || !s.Stale || s.StaleMs != StaleAwaitingAPI {
		t.Fatalf("fresh prompt: %+v", s)
	}
	// Unchanged file, later clock: the cached entry's idle flag follows the file's age.
	now = now.Add(2 * time.Minute)
	if s := r.Read(p, false); !s.Idle || !s.Stale {
		t.Fatalf("aged prompt: %+v", s)
	}
	// A big transcript decides from the extended tail when the last 8 KiB hold nothing deciding.
	var b strings.Builder
	raw, _ := json.Marshal(m{"type": "assistant", "message": m{"content": []any{m{"type": "text", "text": "done"}}, "stop_reason": "end_turn"}})
	b.Write(raw)
	b.WriteByte('\n')
	for b.Len() < 20_000 {
		b.WriteString(`{"type":"progress","data":"` + strings.Repeat("x", 100) + `"}` + "\n")
	}
	big := filepath.Join(t.TempDir(), "big.jsonl")
	_ = os.WriteFile(big, []byte(b.String()), 0o600)
	if s := r.Read(big, false); !s.Idle || s.Stale || str(s.LastAssistantSnippet) != "done" {
		t.Fatalf("extended tail: %+v", s)
	}
}

// agent-session-history-stats: Claude history stats parsing.
func TestHistoryStats(t *testing.T) {
	p := writeJSONL(t,
		m{"type": "file-history-snapshot"},
		m{"timestamp": "2026-05-02T07:37:01.000Z", "type": "user", "message": m{"content": []any{m{"type": "text", "text": "Please edit a file"}}}},
		m{"timestamp": "2026-05-02T07:37:02.000Z", "type": "assistant", "message": m{"content": []any{
			m{"type": "tool_use", "name": "Edit", "input": m{"file_path": "/workspace/app.ts", "old_string": "a", "new_string": "b"}},
			m{"type": "text", "text": "Updated the file."},
		}}},
		m{"timestamp": "2026-05-02T07:37:03.000Z", "type": "system", "subtype": "turn_duration", "durationMs": 2000},
	)
	s := ReadHistoryStats(p)
	got, _ := json.Marshal(s)
	want := `{"toolUsage":{"Edit":1},"touchedFiles":["/workspace/app.ts"],"lastAssistantText":"Updated the file.","lastUserText":"Please edit a file","firstUserTs":1777707421000,"lastAssistantTs":1777707422000,"turnDurationMs":2000}`
	if string(got) != want {
		t.Fatalf("%s\nwant %s", got, want)
	}
	if s := ReadHistoryStats("/nonexistent"); s.LastUserText != nil || s.TouchedFiles == nil {
		t.Fatal("empty stats")
	}
}

// agent-sleep: conversation activity is the newest user or assistant message.
func TestLastConversationAt(t *testing.T) {
	lines := entries(t,
		`{"type":"user","timestamp":"2026-09-28T08:00:00.000Z"}`,
		`{"type":"assistant","timestamp":"2026-09-28T08:05:00.000Z"}`,
		`{"type":"assistant","isSidechain":true,"timestamp":"2026-09-28T09:00:00.000Z"}`,
		`{"type":"cost-state","sessionId":"s"}`,
		`{"type":"bridge-session","sessionId":"s"}`,
		`{"type":"assis`,
	)
	if at := LastConversationAt(lines); at == nil || *at != 1790582700000 {
		t.Fatalf("%v", at)
	}
	if LastConversationAt(entries(t, `{"type":"cost-state"}`)) != nil {
		t.Fatal("nothing in the tail")
	}
	p := writeJSONL(t, m{"type": "user", "timestamp": "2026-09-28T08:00:00.000Z"})
	var r ActivityReader
	if at := r.At(p); at == nil || *at != 1790582400000 {
		t.Fatalf("reader %v", at)
	}
}

func TestTranslateHook(t *testing.T) {
	for _, k := range []string{"session-start", "prompt-submit", "stop", "stop-failure", "interrupt", "pre-compact", "post-compact"} {
		if ev, ok := TranslateHook(k, "ignored"); !ok || string(ev.Kind) != k || ev.NotificationType != "" {
			t.Errorf("%s: %+v", k, ev)
		}
	}
	if ev, ok := TranslateHook("notification", "permission_prompt"); !ok || ev.NotificationType != "permission_prompt" {
		t.Fatal("notification")
	}
	for _, k := range []string{"poll", "summary-update", ""} {
		if _, ok := TranslateHook(k, ""); ok {
			t.Errorf("%q accepted", k)
		}
	}
}

// preview-mcp-config: the hook settings allow the preview tools and name every hook.
func TestHookSettings(t *testing.T) {
	got := string(HookSettings(HookSettingsConfig{StatusHook: "/h/.data/status-hook.sh", Statusline: "/h/.data/statusline.sh", Allow: []string{"mcp__preview", "mcp__old-preview"}}))
	hook := func(name, ev string, last bool) string {
		s := `    "` + name + `": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "sh \"/h/.data/status-hook.sh\" ` + ev + `",
            "timeout": 3
          }
        ]
      }
    ]`
		if !last {
			s += ","
		}
		return s + "\n"
	}
	want := "{\n  \"hooks\": {\n" +
		hook("SessionStart", "session-start", false) + hook("UserPromptSubmit", "prompt-submit", false) +
		hook("Notification", "notification", false) + hook("Stop", "stop", false) + hook("StopFailure", "stop-failure", false) +
		hook("PreCompact", "pre-compact", false) + hook("PostCompact", "post-compact", true) +
		"  },\n  \"statusLine\": {\n    \"type\": \"command\",\n    \"command\": \"sh \\\"/h/.data/statusline.sh\\\"\"\n  },\n" +
		"  \"permissions\": {\n    \"allow\": [\n      \"mcp__preview\",\n      \"mcp__old-preview\"\n    ]\n  },\n" +
		"  \"crossSessionInbound\": \"accept\"\n}\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestPaneTitleAndProcess(t *testing.T) {
	if got := ParsePaneTitle("⠋ Fix the build  "); got == nil || *got != "Fix the build" {
		t.Fatalf("%v", got)
	}
	if ParsePaneTitle("bash") != nil || ParsePaneTitle("✳ ") != nil || ParsePaneTitle("") != nil {
		t.Fatal("not Claude's title")
	}
	if !MatchesProcess("claude", nil) || !MatchesProcess("node", []string{"/usr/bin/claude"}) || MatchesProcess("node", []string{"x.js"}) {
		t.Fatal("matchesProcess")
	}
	if SessionIDFromPath("/p/abc.jsonl") != "abc" || SessionIDFromPath("") != "" {
		t.Fatal("session id from path")
	}
}

type fakeProc struct {
	args     string
	children []int
	cwd      string
	env      []string
}

type fakeProcs map[int]fakeProc

func (f fakeProcs) procs() proc.Procs {
	return proc.Procs{
		Children: func(pid int) []int { return f[pid].children },
		Args: func(pid int) (string, bool) {
			p, ok := f[pid]
			return p.args, ok && p.args != ""
		},
		Cwd:     func(pid int) string { return f[pid].cwd },
		Environ: func(pid int) ([]string, bool) { return f[pid].env, f[pid].env != nil },
	}
}

// claude-platform-hooks: tells the managed launch, a settings file of its own and a bare claude apart.
func TestRunsWithHooks(t *testing.T) {
	dir := t.TempDir()
	own := filepath.Join(dir, "launch-settings.json")
	_ = os.WriteFile(own, []byte(`{"hooks":{"Stop":[{"hooks":[{"command":"sh \"/home/u/.olddata/status-hook.sh\" stop"}]}]}}`), 0o600)
	hooks := "/home/u/.data/hooks.json"
	for _, match := range []agents.Match{agents.MatchSubstring, agents.MatchArgv} {
		check := func(f fakeProcs, top []int, hooked, found bool) {
			t.Helper()
			d := &Detector{Procs: f.procs(), Match: match}
			if h, fd := d.RunsWithHooks(top, hooks, "status-hook.sh"); h != hooked || fd != found {
				t.Errorf("match %d %v: %v %v", match, f, h, fd)
			}
		}
		check(fakeProcs{10: {args: "python3 managed-claude --settings " + hooks + " --resume x", children: []int{11}}, 11: {args: "/opt/bin/claude-abc --settings " + own}}, []int{10}, true, true)
		check(fakeProcs{20: {args: "/opt/bin/claude-abc --settings " + own}}, []int{20}, true, true)
		check(fakeProcs{30: {args: "claude Собери отчёт"}}, []int{30}, false, true)
		check(fakeProcs{40: {args: "vim notes.md"}}, []int{40}, false, false)
		check(fakeProcs{50: {args: "claude --settings=" + hooks}}, []int{50}, true, true)
	}
}

func TestWithEnv(t *testing.T) {
	f := fakeProcs{10: {args: "bash", children: []int{11}, env: []string{"VISUAL=/d/bin/editor"}}, 11: {args: "claude", env: []string{"A=1", "VISUAL=/d/bin/editor"}}}
	d := &Detector{Procs: f.procs(), Match: agents.MatchArgv}
	if d.WithEnv([]int{10}, "VISUAL=/old/bin/editor", "VISUAL=/d/bin/editor") != 11 || d.WithEnv([]int{10}, "VISUAL=/x") != 0 {
		t.Fatal("prompt editor marker")
	}
}

func put(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

const (
	id1 = "11111111-2222-3333-4444-555555555555"
	id2 = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
)

func TestDetectFromFakes(t *testing.T) {
	home := t.TempDir()
	d := &Detector{Dir: filepath.Join(home, ".claude"), Match: agents.MatchArgv, RemoveStale: true}
	if info, _ := d.Detect([]int{1}); info.Status != agents.NotInstalled {
		t.Fatalf("%+v", info)
	}
	d.Installed = func() bool { return true }
	if info, _ := d.Detect([]int{1}); info.Status != agents.NotInitialized {
		t.Fatalf("%+v", info)
	}
	stale := filepath.Join(home, ".claude", "sessions", "20.json")
	put(t, stale, `{"pid":20,"sessionId":"old","cwd":"/x"}`)
	f := fakeProcs{10: {args: "bash", children: []int{20}}, 20: {args: "node /usr/lib/tools/run.js", cwd: "/y"}}
	d.Procs = f.procs()
	if info, _ := d.Detect(nil); info.Status != agents.NotRunning {
		t.Fatal("no children")
	}
	if info, err := d.Detect([]int{20}); err != nil || info.Status != agents.NotRunning {
		t.Fatalf("%+v %v", info, err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("the pid file of a process that is not Claude is removed")
	}
	// The resumed conversation's transcript stands in until the record's own exists.
	put(t, filepath.Join(home, ".claude", "sessions", "21.json"), `{"pid":21,"sessionId":"`+id1+`","cwd":"/w","startedAt":42}`)
	f[20] = fakeProc{args: "/opt/launcher", children: []int{21}}
	f[21] = fakeProc{args: "claude --resume " + id2}
	proj := filepath.Join(home, ".claude", "projects", sessions.ProjectName("/w"))
	put(t, filepath.Join(proj, id2+".jsonl"), "{}\n")
	info, err := d.Detect([]int{20})
	if err != nil || info.Status != agents.Running || info.SessionID != id2 || info.PID != 21 || *info.StartedAt != 42 || info.JSONLPath != filepath.Join(proj, id2+".jsonl") {
		t.Fatalf("%+v %v", info, err)
	}
	if d.IsRunning([]int{20}) {
		t.Fatal("only the shell's own children count, and /opt/launcher is not Claude")
	}
	if !d.IsRunning([]int{21}) {
		t.Fatal("running")
	}
	put(t, filepath.Join(home, ".claude", "sessions", "21.json"), `{"pid":21,"sessionId":"x"}`)
	if _, err := d.Detect([]int{20}); err != ErrBadRecord {
		t.Fatalf("a record without a folder: %v", err)
	}
}

// process-proc: finds the Claude session of a pane by its pid file.
func TestDetectByPidFileOnThisMachine(t *testing.T) {
	home := t.TempDir()
	pane := exec.Command("bash", "-c", "(exec -a claude sleep 30) & wait")
	if err := pane.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, c := range proc.ChildPIDs(pane.Process.Pid) {
			_ = exec.Command("kill", "-9", itoa(c)).Run()
		}
		_ = pane.Process.Kill()
		_ = pane.Wait()
	}()
	var claudePid int
	for i := 0; i < 150; i++ {
		if kids := proc.ChildPIDs(pane.Process.Pid); len(kids) == 1 {
			if args, _ := proc.Cmdline(kids[0]); strings.HasPrefix(args, "claude") {
				claudePid = kids[0]
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if claudePid == 0 {
		t.Fatal("no claude child")
	}
	sessionID := "0b1f7c9e-2a3d-4f5e-8a6b-1c2d3e4f5a6b"
	cwd := "/root/work"
	sessionsDir := filepath.Join(home, ".claude", "sessions")
	project := filepath.Join(home, ".claude", "projects", "-root-work")
	put(t, filepath.Join(project, sessionID+".jsonl"), "")
	for i := 0; i < 50; i++ {
		put(t, filepath.Join(sessionsDir, itoa(900_000+i)+".json"), `{"pid":`+itoa(900_000+i)+`,"sessionId":"`+sessionID+`","cwd":"`+cwd+`","startedAt":1}`)
	}
	put(t, filepath.Join(sessionsDir, itoa(claudePid)+".json"), `{"pid":`+itoa(claudePid)+`,"sessionId":"`+sessionID+`","cwd":"`+cwd+`","startedAt":42}`)
	d := NewDetector(home)
	children := proc.ChildPIDs(pane.Process.Pid)
	if !d.IsRunning(children) {
		t.Fatal("running")
	}
	info, err := d.Detect(children)
	if err != nil || info.Status != agents.Running || info.SessionID != sessionID || info.PID != claudePid || *info.StartedAt != 42 ||
		info.Cwd != cwd || info.JSONLPath != filepath.Join(project, sessionID+".jsonl") {
		t.Fatalf("%+v %v", info, err)
	}
	if got := FindTranscript(filepath.Join(home, ".claude", "projects"), sessionID); got != info.JSONLPath {
		t.Fatalf("find transcript %q", got)
	}
	entries, _ := os.ReadDir(sessionsDir)
	if !slices.ContainsFunc(entries, func(e os.DirEntry) bool { return e.Name() == "900000.json" }) {
		t.Fatal("other machines' records are not touched by a lookup by name")
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
