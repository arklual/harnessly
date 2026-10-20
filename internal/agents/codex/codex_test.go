package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/proc"
	"github.com/ly-solutions/harnessly/internal/sessions"
)

type m = map[string]any

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

func ev(ts, typ string, payload m) m {
	payload["type"] = typ
	return m{"timestamp": ts, "type": "event_msg", "payload": payload}
}

func sp(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func ms(s string) float64 {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return float64(t.UnixMilli())
}

// agent-runtime-snapshot: Codex assistant snippets from event_msg agent_message records.
func TestSnapshotFinished(t *testing.T) {
	p := writeJSONL(t,
		ev("2026-05-02T07:37:01.000Z", "user_message", m{"message": "implement it"}),
		ev("2026-05-02T07:37:02.000Z", "agent_message", m{"message": "Codex finished the task."}),
		ev("2026-05-02T07:37:03.000Z", "task_complete", m{}),
	)
	s := NewSnapshotReader().Read(p, false)
	if !s.Idle || s.Stale || sp(s.LastAssistantSnippet) != "Codex finished the task." || s.CurrentAction != nil || s.Reset {
		t.Fatalf("%+v", s)
	}
	if *s.LastEntryTs != ms("2026-05-02T07:37:03.000Z") {
		t.Fatal("lastEntryTs is the newest entry's")
	}
}

// agent-runtime-snapshot: Codex in-flight command actions from unmatched exec begin events.
func TestSnapshotInFlightCommand(t *testing.T) {
	p := writeJSONL(t,
		ev("2026-05-02T07:38:01.000Z", "user_message", m{"message": "run tests"}),
		ev("2026-05-02T07:38:02.000Z", "exec_command_begin", m{"call_id": "exec-1", "command": "pnpm test"}),
	)
	s := NewSnapshotReader().Read(p, false)
	if s.Idle || s.CurrentAction == nil || *s.CurrentAction.ToolName != "Bash" || s.CurrentAction.Summary != "$ pnpm test" {
		t.Fatalf("%+v %+v", s, s.CurrentAction)
	}
}

// agent-runtime-snapshot: reset when a user message follows the last assistant output.
func TestSnapshotReset(t *testing.T) {
	p := writeJSONL(t,
		ev("2026-05-02T07:39:01.000Z", "agent_message", m{"message": "Previous answer."}),
		ev("2026-05-02T07:39:02.000Z", "user_message", m{"message": "next task"}),
	)
	s := NewSnapshotReader().Read(p, false)
	if !s.Reset || sp(s.LastAssistantSnippet) != "Previous answer." || s.CurrentAction != nil {
		t.Fatalf("%+v", s)
	}
}

func entries(t *testing.T, lines ...any) []*agents.Obj {
	t.Helper()
	var out []*agents.Obj
	for _, l := range lines {
		raw, _ := json.Marshal(l)
		out = append(out, agents.DecodeObj(raw))
	}
	return out
}

func TestScanLinesActions(t *testing.T) {
	fc := func(name string, args any, call string) m {
		return m{"type": "response_item", "payload": m{"type": "function_call", "name": name, "arguments": args, "call_id": call}}
	}
	for _, c := range []struct {
		name  string
		lines []any
		tool  string
		sum   string
	}{
		{"shell json args", []any{fc("exec_command", `{"cmd":"go test ./...\nmore"}`, "c1")}, "Bash", "$ go test ./..."},
		{"empty shell", []any{fc("shell", `{}`, "c1")}, "Bash", "$ "},
		{"patch", []any{fc("apply_patch", "*** Begin Patch", "c1")}, "Edit", "Apply patch"},
		{"stdin", []any{fc("write_stdin", `{"chars":"y"}`, "c1")}, "write_stdin", "Write stdin"},
		{"first key", []any{fc("view_image", `{"path":"/a b\n c.png","x":1}`, "c1")}, "view_image", "view_image /a b c.png"},
		{"object value", []any{fc("tool", `{"opts":{"b":1,"a":[1,"x"]}}`, "c1")}, "tool", `tool {"b":1,"a":[1,"x"]}`},
		{"not json", []any{fc("tool", `not json`, "c1")}, "tool", "tool"},
		{"command array", []any{ev("", "exec_command_begin", m{"call_id": "e", "command": []any{"bash", "-lc", "ls\nx"}})}, "Bash", "$ bash -lc ls"},
		{"web search", []any{m{"type": "response_item", "payload": m{"type": "web_search_call", "status": "in_progress", "query": "go 1.26"}}}, "WebSearch", `WebSearch "go 1.26"`},
	} {
		s := ScanLines(entries(t, c.lines...), 0)
		if s.CurrentAction == nil || sp(s.CurrentAction.ToolName) != c.tool || s.CurrentAction.Summary != c.sum {
			t.Errorf("%s: %+v", c.name, s.CurrentAction)
		}
	}
	done := ScanLines(entries(t, fc("exec_command", `{"cmd":"x"}`, "c1"), m{"type": "response_item", "payload": m{"type": "function_call_output", "call_id": "c1"}}), 0)
	if done.CurrentAction != nil {
		t.Fatal("a finished call is not the current action")
	}
	aborted := ScanLines(entries(t, ev("", "user_message", m{"message": "x"}), ev("", "turn_aborted", m{})), 0)
	if !aborted.Idle || !aborted.Interrupted || aborted.Stale {
		t.Fatalf("%+v", aborted)
	}
}

// Found while porting: every line of the tail is read, so an earlier turn's completion makes a running turn idle.
func TestScanLinesEarlierCompletionQuirk(t *testing.T) {
	s := ScanLines(entries(t,
		ev("", "task_complete", m{}),
		ev("", "user_message", m{"message": "next"}),
		ev("", "exec_command_begin", m{"call_id": "e", "command": "make"}),
	), 1000)
	if !s.Idle || s.Stale || s.CurrentAction == nil {
		t.Fatalf("%+v", s)
	}
	// And a prompt read when it is already old never turns idle by age.
	old := ScanLines(entries(t, ev("", "user_message", m{"message": "x"})), 100_000)
	if old.Idle || old.Stale || old.NeedsStaleRecheck || old.StaleMs != StaleAwaitingAPI {
		t.Fatalf("%+v", old)
	}
}

func TestSnapshotCacheRecheck(t *testing.T) {
	r := NewSnapshotReader()
	now := time.Now()
	r.Now = func() time.Time { return now }
	p := writeJSONL(t, ev("", "agent_message", m{"message": "working"}))
	mt := now.Add(-time.Second)
	_ = os.Chtimes(p, mt, mt)
	if s := r.Read(p, false); s.Idle || !s.Stale || s.StaleMs != StaleInterrupted {
		t.Fatalf("%+v", s)
	}
	now = now.Add(time.Minute)
	if s := r.Read(p, false); !s.Idle || s.Stale {
		t.Fatalf("cached and aged: %+v", s)
	}
	empty := filepath.Join(t.TempDir(), "e.jsonl")
	_ = os.WriteFile(empty, nil, 0o600)
	if s := r.Read(empty, false); !s.Idle {
		t.Fatal("empty is idle")
	}
	// A big rollout drops its cut first line.
	var b strings.Builder
	b.WriteString(`{"type":"event_msg","payload":{"type":"task_complete"}}` + "\n")
	for b.Len() < tailSize+1000 {
		b.WriteString(`{"type":"event_msg","payload":{"type":"token_count","pad":"` + strings.Repeat("x", 200) + `"}}` + "\n")
	}
	big := filepath.Join(t.TempDir(), "big.jsonl")
	_ = os.WriteFile(big, []byte(b.String()), 0o600)
	es, _ := TailEntries(big, int64(b.Len()))
	if es[0] == nil || len(es) == 0 {
		t.Fatal("first kept line must be whole")
	}
	if s := r.Read(big, true); s.Idle {
		t.Fatal("the completion is outside the tail")
	}
}

// agent-session-history-stats: Codex response-backed prompts and replies, injected context ignored.
func TestHistoryStatsResponses(t *testing.T) {
	resp := func(role, text, ts string) m {
		kind := "input_text"
		if role == "assistant" {
			kind = "output_text"
		}
		return m{"timestamp": ts, "type": "response_item", "payload": m{"type": "message", "role": role, "content": []any{m{"type": kind, "text": text}}}}
	}
	p := writeJSONL(t,
		resp("user", "# AGENTS.md instructions for /project", "2026-09-25T10:00:00.000Z"),
		resp("user", "First request", "2026-09-25T10:00:01.000Z"),
		resp("assistant", "First reply", "2026-09-25T10:00:02.000Z"),
		resp("user", "Latest request", "2026-09-25T10:00:03.000Z"),
		resp("assistant", "Latest reply", "2026-09-25T10:00:04.000Z"),
		resp("developer", "Hidden instructions", "2026-09-25T10:00:05.000Z"),
		resp("user", "<environment_context>cwd</environment_context>", "2026-09-25T10:00:06.000Z"),
	)
	s := ReadHistoryStats(p)
	if sp(s.LastUserText) != "Latest request" || sp(s.LastAssistantText) != "Latest reply" ||
		*s.FirstUserTs != ms("2026-09-25T10:00:01.000Z") || *s.LastAssistantTs != ms("2026-09-25T10:00:04.000Z") {
		t.Fatalf("%+v", s)
	}
}

// agent-session-history-stats: Codex prompt, result, tools and touched files.
func TestHistoryStatsTools(t *testing.T) {
	p := writeJSONL(t,
		ev("2026-05-02T07:38:01.000Z", "user_message", m{"message": "Implement Codex stats"}),
		ev("2026-05-02T07:38:02.000Z", "exec_command_begin", m{"call_id": "exec-1", "command": "pnpm test"}),
		ev("2026-05-02T07:38:03.000Z", "exec_command_end", m{"call_id": "exec-1", "exit_code": 0, "duration": m{"secs": 1, "nanos": 500_000_000}}),
		m{"timestamp": "2026-05-02T07:38:04.000Z", "type": "response_item", "payload": m{"type": "custom_tool_call", "call_id": "patch-1", "name": "apply_patch",
			"input": "*** Begin Patch\n*** Update File: src/app.ts\n@@\n-old\n+new\n*** End Patch\n"}},
		ev("2026-05-02T07:38:05.000Z", "agent_message", m{"message": "Implemented Codex stats."}),
		ev("2026-05-02T07:38:06.000Z", "task_complete", m{}),
	)
	got, _ := json.Marshal(ReadHistoryStats(p))
	want := `{"toolUsage":{"apply_patch":1,"Bash":1},"touchedFiles":["src/app.ts"],"lastAssistantText":"Implemented Codex stats.","lastUserText":"Implement Codex stats","firstUserTs":` +
		jsonNum(ms("2026-05-02T07:38:01.000Z")) + `,"lastAssistantTs":` + jsonNum(ms("2026-05-02T07:38:05.000Z")) + `,"turnDurationMs":1500}`
	if string(got) != want {
		t.Fatalf("%s\nwant %s", got, want)
	}
}

func jsonNum(f float64) string { b, _ := json.Marshal(f); return string(b) }

// codex-hook-handler: prompt hooks become provider-neutral meta and events.
func TestProcessHookPrompt(t *testing.T) {
	tr := ProcessHook(m{"hook_event_name": "UserPromptSubmit", "session_id": "12345678-aaaa-bbbb-cccc-1234567890ab", "transcript_path": "/tmp/codex.jsonl", "prompt": "Implement the provider boundary"})
	if tr.Event == nil || tr.Event.Kind != agents.PromptSubmit {
		t.Fatalf("%+v", tr.Event)
	}
	if sp(tr.Meta.SessionID) != "12345678-aaaa-bbbb-cccc-1234567890ab" || sp(tr.Meta.JSONLPath) != "/tmp/codex.jsonl" ||
		sp(tr.Meta.LastUserMessage) != "Implement the provider boundary" || sp(tr.Meta.AgentSummary) != "Implement the provider boundary" {
		t.Fatalf("%+v", tr.Meta)
	}
	long := strings.Repeat("я", 100)
	if tr := ProcessHook(m{"hook_event_name": "UserPromptSubmit", "prompt": long}); len([]rune(sp(tr.Meta.AgentSummary))) != 80 || tr.Meta.SessionID != nil {
		t.Fatal("summary is the first 80 units; no session id keeps the stored one")
	}
	if tr := ProcessHook(m{"hook_event_name": "Elicitation"}); tr.Event != nil {
		t.Fatal("unknown events translate to nothing")
	}
}

// codex-hook-handler: SessionStart side effects are returned as data.
func TestProcessHookSessionStart(t *testing.T) {
	tr := ProcessHook(m{"hook_event_name": "SessionStart", "session_id": "12345678-aaaa-bbbb-cccc-1234567890ab", "transcript_path": "/tmp/codex.jsonl", "cwd": "/workspace/app", "source": "clear"})
	if tr.Event == nil || tr.Event.Kind != agents.SessionStart || !tr.ClearSession || !tr.Meta.ClearMessages {
		t.Fatalf("%+v", tr)
	}
	info := tr.SessionInfo
	if info == nil || info.Status != agents.Running || info.SessionID != "12345678-aaaa-bbbb-cccc-1234567890ab" || info.JSONLPath != "/tmp/codex.jsonl" || info.Cwd != "/workspace/app" {
		t.Fatalf("%+v", info)
	}
}

// codex-hook-handler: non-clear SessionStart only for inactive or unknown tabs.
func TestShouldEmit(t *testing.T) {
	p := m{"hook_event_name": "SessionStart", "source": "startup"}
	if !ShouldEmit(p, "inactive") || !ShouldEmit(p, "unknown") || ShouldEmit(p, "idle") {
		t.Fatal("startup")
	}
	if !ShouldEmit(m{"hook_event_name": "SessionStart", "source": "clear"}, "idle") {
		t.Fatal("clear")
	}
	if !ShouldEmit(m{"hook_event_name": "Stop"}, "busy") || ShouldEmit(m{"hook_event_name": "x"}, "busy") {
		t.Fatal("others")
	}
}

// codex-hook-handler: permission requests become meta patches and notification events.
func TestProcessHookPermission(t *testing.T) {
	tr := ProcessHook(m{"hook_event_name": "PermissionRequest", "session_id": "12345678-aaaa-bbbb-cccc-1234567890ab", "request_type": "ExecApprovalRequest",
		"exec_command": m{"command": "pnpm test", "cwd": "/workspace/app"}})
	if tr.Event == nil || tr.Event.Kind != agents.Notification || tr.Event.NotificationType != "permission_prompt" {
		t.Fatalf("%+v", tr.Event)
	}
	r := tr.Meta.Permission
	if !tr.Meta.SetPermission || r == nil || r.Type != "ExecApprovalRequest" || r.Command != "pnpm test" || r.Cwd != "/workspace/app" {
		t.Fatalf("%+v", r)
	}
	if tr := ProcessHook(m{"hook_event_name": "Stop"}); !tr.Meta.SetPermission || tr.Meta.Permission != nil {
		t.Fatal("Stop clears the request")
	}
}

func TestParsePermissionRequest(t *testing.T) {
	pr := func(p m) string {
		p["hook_event_name"] = "PermissionRequest"
		b, _ := json.Marshal(ParsePermissionRequest(p))
		return string(b)
	}
	for _, c := range []struct {
		p    m
		want string
	}{
		{m{"requested_permissions": []any{"network", "", 3}, "call_id": "c"}, `{"type":"RequestPermissions","permissions":["network"],"callId":"c"}`},
		{m{"apply_patch": m{"patches": []any{m{"path": "a.go", "operation": "modify", "diff": "d"}, m{"path": "b", "operation": "bad"}}, "call_id": "p"}}, `{"type":"ApplyPatchApprovalRequest","patches":[{"path":"a.go","operation":"modify","diff":"d"}],"callId":"p"}`},
		{m{"exec_command": m{"command": "ls", "env": m{"A": "1", "B": 2}}, "call_id": "", "cwd": "/w"}, `{"type":"ExecApprovalRequest","command":"ls","cwd":"/w","env":{"A":"1"}}`},
		{m{"tool_name": " Apply_Patch ", "tool_input": m{"command": "*** Add File: n.txt\n+x\n*** Delete File: o.txt"}}, `{"type":"ApplyPatchApprovalRequest","patches":[{"path":"n.txt","operation":"create","diff":"*** Add File: n.txt\n+x\n*** Delete File: o.txt"},{"path":"o.txt","operation":"delete","diff":"*** Add File: n.txt\n+x\n*** Delete File: o.txt"}]}`},
		{m{"tool_name": "apply_patch", "tool_input": m{"command": "raw"}}, `{"type":"ApplyPatchApprovalRequest","patches":[{"path":"apply_patch","operation":"modify","diff":"raw"}]}`},
		{m{"tool_name": "Bash", "tool_input": m{"command": "rm x"}, "cwd": "/w"}, `{"type":"ExecApprovalRequest","command":"rm x","cwd":"/w"}`},
		{m{}, `null`},
	} {
		if got := pr(c.p); got != c.want {
			t.Errorf("%v:\n%s\nwant %s", c.p, got, c.want)
		}
	}
	if ParsePermissionRequest(m{"hook_event_name": "Stop", "requested_permissions": []any{"x"}}) != nil {
		t.Fatal("only PermissionRequest")
	}
}

func TestResumeID(t *testing.T) {
	const id1, id2 = "11111111-2222-3333-4444-555555555555", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	for in, want := range map[string]string{
		"codex resume " + id1:                      id1,
		"codex resume \"" + id1 + "\" --x":         id1,
		"codex RESUME " + id1:                      id1,
		"codex --resume-session-id=" + id1:         id1,
		"codex --resume-session-id   " + id1:       id1,
		"codex xresume " + id1:                     "",
		"codex resume " + id1 + "x":                "",
		"codex resume " + id1 + "'x resume " + id2: id2,
		"resume " + id1:                            id1,
		"codex resume=" + id1 + "'":                id1,
		"codex resume 1234":                        "",
	} {
		if got := ResumeID(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func meta(id, cwd, ts string) string {
	b, _ := json.Marshal(m{"type": "session_meta", "timestamp": ts, "payload": m{"id": id, "timestamp": ts, "cwd": cwd}})
	return string(b) + "\n"
}

func put(t *testing.T, path, body string, mtime time.Time) {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if !mtime.IsZero() {
		_ = os.Chtimes(path, mtime, mtime)
	}
}

// codex-session-detection: the most recently modified Codex session for a cwd; lookup by id ignores cwd recency.
func TestFindSessions(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.Local)
	day := sessions.CodexDayDir(root, now)
	at := func(s string) time.Time { tm, _ := time.Parse(time.RFC3339, s); return tm }
	put(t, filepath.Join(day, "rollout-old.jsonl"), meta("11111111-1111-4111-8111-111111111111", "/tmp/project-a", "2026-05-02T01:00:00.000Z"), at("2026-05-02T01:00:00Z"))
	latest := filepath.Join(day, "rollout-latest.jsonl")
	put(t, latest, meta("22222222-2222-4222-8222-222222222222", "/tmp/project-a", "2026-05-02T02:00:00.000Z"), at("2026-05-02T03:00:00Z"))
	put(t, filepath.Join(day, "rollout-other.jsonl"), meta("33333333-3333-4333-8333-333333333333", "/tmp/project-b", "2026-05-02T03:00:00.000Z"), at("2026-05-02T04:00:00Z"))
	s, ok := FindLatestForCwd(root, "/tmp/project-a", now, 1)
	if !ok || s.SessionID != "22222222-2222-4222-8222-222222222222" || s.JSONLPath != latest || *s.StartedAt != ms("2026-05-02T02:00:00.000Z") {
		t.Fatalf("%+v %v", s, ok)
	}
	id := "44444444-4444-4444-8444-444444444444"
	byID := filepath.Join(day, "rollout-2026-05-02T03-00-00-"+id+".jsonl")
	put(t, byID, meta(id, "/tmp/project-a", "2026-05-02T03:00:00.000Z"), time.Time{})
	if s, ok := FindSessionByID(root, id, now, 1); !ok || s.SessionID != id || s.JSONLPath != byID {
		t.Fatalf("%+v %v", s, ok)
	}
	if _, ok := FindSessionByID(root, id, now.AddDate(0, 0, 2), 1); ok {
		t.Fatal("outside the window")
	}
	crlf := filepath.Join(day, "rollout-crlf.jsonl")
	put(t, crlf, strings.TrimSuffix(meta("55555555-5555-4555-8555-555555555555", "/c", "2026-05-02T03:00:00Z"), "\n")+"\r\n{}\n", time.Time{})
	if s, ok := ReadMeta(crlf); !ok || s.Cwd != "/c" {
		t.Fatal("a CR ends the first line")
	}
}

type fakeProc struct {
	args     string
	children []int
	cwd      string
	start    time.Time
}

func fakeProcs(f map[int]fakeProc) proc.Procs {
	return proc.Procs{
		Children:  func(pid int) []int { return f[pid].children },
		Args:      func(pid int) (string, bool) { p, ok := f[pid]; return p.args, ok },
		Cwd:       func(pid int) string { return f[pid].cwd },
		StartTime: func(pid int) (time.Time, bool) { return f[pid].start, !f[pid].start.IsZero() },
	}
}

// codex-active-session-fallback: the latest cwd session is attached only when the fallback is explicit.
func TestCwdFallbackOnlyWhenAllowed(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	sid := "55555555-5555-4555-8555-555555555555"
	cwd := "/tmp/project-a"
	path := filepath.Join(sessions.CodexDayDir(filepath.Join(home, ".codex", "sessions"), now), "rollout-"+sid+".jsonl")
	put(t, path, meta(sid, cwd, now.Add(-5*time.Second).UTC().Format(time.RFC3339Nano)), time.Time{})
	d := &Detector{Dir: filepath.Join(home, ".codex"), Match: agents.MatchArgv, Now: func() time.Time { return now },
		Procs: fakeProcs(map[int]fakeProc{1234: {args: "codex", cwd: cwd, start: now.Add(-10 * time.Second)}})}
	info := d.Detect([]int{1234}, false)
	if info.Status != agents.Running || info.SessionID != "" || info.JSONLPath != "" || info.Cwd != cwd || info.PID != 1234 {
		t.Fatalf("default: %+v", info)
	}
	info = d.Detect([]int{1234}, true)
	if info.Status != agents.Running || info.SessionID != sid || info.JSONLPath != path {
		t.Fatalf("fallback: %+v", info)
	}
	// A session that started well before the process is someone else's.
	d.Procs = fakeProcs(map[int]fakeProc{1234: {args: "codex", cwd: cwd, start: now.Add(2 * time.Minute)}})
	if info := d.Detect([]int{1234}, true); info.SessionID != "" {
		t.Fatalf("older session: %+v", info)
	}
}

func TestDetectResume(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.Local)
	root := filepath.Join(home, ".codex", "sessions")
	const id1 = "11111111-2222-3333-4444-555555555555"
	path := filepath.Join(sessions.CodexDayDir(root, now.AddDate(0, 0, -2)), "rollout-x-"+id1+".jsonl")
	put(t, path, meta(id1, "/w", "2026-10-15T10:00:00Z"), time.Time{})
	procs := map[int]fakeProc{
		20: {args: "node /data/codex-launcher.js --resume-session-id '" + id1 + "'", children: []int{21}, cwd: "/launch"},
		21: {args: "/usr/lib/codex/codex resume " + id1, cwd: "/w2"},
	}
	d := &Detector{Dir: filepath.Join(home, ".codex"), Match: agents.MatchArgv, Now: func() time.Time { return now }, Procs: fakeProcs(procs)}
	if !d.IsRunning([]int{20}) || d.IsRunning(nil) {
		t.Fatal("the launcher counts")
	}
	info := d.Detect([]int{20}, false)
	if info.SessionID != id1 || info.JSONLPath != path || info.PID != 20 || info.Cwd != "/w" || info.StartedAt == nil {
		t.Fatalf("%+v", info)
	}
	procs[20] = fakeProc{args: "codex resume aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", cwd: "/x"}
	if info := d.Detect([]int{20}, false); info.SessionID != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" || info.JSONLPath != "" || info.Cwd != "/x" {
		t.Fatalf("unknown transcript: %+v", info)
	}
	if info := (&Detector{Dir: filepath.Join(home, "none")}).Detect([]int{1}, false); info.Status != agents.NotInstalled {
		t.Fatal("not installed")
	}
	if SessionIDFromPath("/s/rollout-2026-"+id1+".jsonl") != id1 || SessionIDFromPath("/s/x.jsonl") != "" {
		t.Fatal("session id from path")
	}
}
