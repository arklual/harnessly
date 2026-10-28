package agents_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/claude"
)

func TestDecodeKeepsJavaScriptKeyOrder(t *testing.T) {
	v, err := agents.Decode([]byte(` {"b":1,"a":{"z":[1,"x",null,true],"y":2},"10":0,"2":0,"b":3,"01":4} `))
	if err != nil {
		t.Fatal(err)
	}
	o := agents.AsObj(v)
	if got := strings.Join(o.Keys(), ","); got != "2,10,b,a,01" {
		t.Fatalf("keys %s", got)
	}
	if o.Val("b") != 3.0 {
		t.Fatal("the last duplicate wins")
	}
	if got := agents.Stringify(o); got != `{"2":0,"10":0,"b":3,"a":{"z":[1,"x",null,true],"y":2},"01":4}` {
		t.Fatalf("stringify %s", got)
	}
	for _, bad := range []string{`{"a":1} x`, `{"a":`, ``, `1 2`} {
		if _, err := agents.Decode([]byte(bad)); err == nil {
			t.Errorf("%q decoded", bad)
		}
	}
	if agents.DecodeObj([]byte(`[1]`)) != nil || agents.DecodeObj([]byte(`5`)) != nil {
		t.Fatal("only objects")
	}
	if agents.JSString(o.Val("a")) != "[object Object]" || agents.JSString([]any{1.0, nil, "x"}) != "1,,x" {
		t.Fatal("String()")
	}
}

func TestCountsKeepOrder(t *testing.T) {
	var c agents.Counts
	for _, n := range []string{"Edit", "Bash", "Edit", "7"} {
		c.Add(n)
	}
	b, _ := json.Marshal(struct {
		C agents.Counts `json:"c"`
	}{c})
	if string(b) != `{"c":{"7":1,"Edit":2,"Bash":1}}` {
		t.Fatalf("%s", b)
	}
	var back agents.Counts
	if err := json.Unmarshal([]byte(`{"Read":3,"Bash":1}`), &back); err != nil || back.Get("Read") != 3 || strings.Join(back.Names(), ",") != "Read,Bash" {
		t.Fatalf("%v %v", err, back.Names())
	}
	empty, _ := json.Marshal(agents.EmptyHistoryStats())
	if string(empty) != `{"toolUsage":{},"touchedFiles":[],"lastAssistantText":null,"lastUserText":null,"firstUserTs":null,"lastAssistantTs":null,"turnDurationMs":null}` {
		t.Fatalf("%s", empty)
	}
}

func TestSessionInfoJSON(t *testing.T) {
	b, _ := json.Marshal(agents.Info(agents.NotRunning))
	if string(b) != `{"status":"not-running","sessionId":null,"jsonlPath":null,"pid":null,"startedAt":null,"cwd":null}` {
		t.Fatalf("%s", b)
	}
	at := 42.0
	b, _ = json.Marshal(agents.SessionInfo{Status: agents.Running, SessionID: "5", RawSessionID: 5.0, JSONLPath: "/p", PID: 7, StartedAt: &at, Cwd: "/w"})
	if string(b) != `{"status":"running","sessionId":5,"jsonlPath":"/p","pid":7,"startedAt":42,"cwd":"/w"}` {
		t.Fatalf("%s", b)
	}
}

func TestNotificationNeedsInput(t *testing.T) {
	for typ, want := range map[string]bool{"": true, "permission_prompt": true, "worker_permission_prompt": true, "idle_prompt": false, "auth_success": false} {
		if agents.NotificationNeedsInput(typ) != want {
			t.Errorf("%q", typ)
		}
	}
}

func TestMatch(t *testing.T) {
	for _, c := range []struct {
		args        string
		sub, strict bool
	}{
		{"claude --resume x", true, true},
		{"/opt/profile/bin/claude-5cd90aabd83f8a15 --settings /x", true, true},
		{"python3 /opt/profile/managed-claude --settings /x", true, true},
		{"node /usr/lib/node_modules/@anthropic-ai/claude-code/cli.js", true, true},
		{"node /home/u/.data/codex-launcher.js --workspace-id w", false, false},
		{"node --max-old-space-size=4096 /usr/bin/claude", true, true},
		{"/bin/bash -c source /home/u/.claude/shell-snapshots/snap.sh", true, false},
		{"less /home/u/.claude/settings.json", true, false},
		{"vim CLAUDE.md", false, false},
		{"bash", false, false},
	} {
		if got := agents.MatchSubstring.IsArgs(c.args, "claude"); got != c.sub {
			t.Errorf("substring %q: %v", c.args, got)
		}
		if got := agents.MatchArgv.IsArgs(c.args, "claude"); got != c.strict {
			t.Errorf("argv %q: %v", c.args, got)
		}
	}
}

func bashPrintf(t *testing.T, quoted string) string {
	t.Helper()
	out, err := exec.Command("bash", "-c", "printf %s "+quoted).Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// initial-prompt: round-trips quotes, backslashes and newlines through sh and bash, on one typed line.
func TestPrintfQuoteRoundTrip(t *testing.T) {
	text := "Привет, это 'тест'\nвторая строка \\ $HOME `id` \"q\"\tтаб\x01\x7f %s \\c"
	quoted := agents.PrintfQuote(text)
	if strings.ContainsAny(quoted, "\n\t\x01\x7f") {
		t.Fatalf("control characters in the typed line: %q", quoted)
	}
	if got := bashPrintf(t, quoted); got != text {
		t.Fatalf("%q", got)
	}
	if out, err := exec.Command("sh", "-c", "printf %s "+quoted).Output(); err != nil || string(out) != text {
		t.Fatalf("sh: %q %v", out, err)
	}
}

// initial-prompt: appends the prompt per provider.
func TestWithInitialPrompt(t *testing.T) {
	for _, c := range []struct{ provider, cmd, prompt, want string }{
		{"claude", "claude --x", " hi ", `claude --x -- "$(printf %b 'hi')"`},
		{"codex", "node 'l.js'", "hi", `node 'l.js' --prompt "$(printf %b 'hi')"`},
		{"claude", "claude", "   ", "claude"},
		{"claude", "claude", "", "claude"},
		{"other", "x", "hi", "x"},
	} {
		if got := agents.WithInitialPrompt(c.provider, c.cmd, c.prompt, ""); got != c.want {
			t.Errorf("%q: %q", c.want, got)
		}
	}
	long := strings.Repeat("я", agents.MaxInitialPrompt+5)
	if got := agents.InitialPromptText(long); len([]rune(got)) != agents.MaxInitialPrompt {
		t.Fatalf("capped to %d", len([]rune(got)))
	}
}

// initial-prompt: reads a long prompt back from a file and removes the file.
func TestLongPromptThroughFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "prompt 'dir' ")
	text := "Событие:\n" + strings.Repeat(`{"a":"b\"c", 'd'} $HOME `, 2000)
	file, err := agents.WriteLongPrompt(dir, text)
	if err != nil || !strings.HasPrefix(filepath.Base(file), "initial-") {
		t.Fatalf("%q %v", file, err)
	}
	if st, _ := os.Stat(file); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	if got := bashPrintf(t, agents.PromptFromFile(file)); got != text {
		t.Fatal("read back differs")
	}
	if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the file must be gone")
	}
	if got := agents.WithInitialPrompt("claude", "claude", text, file); got != "claude -- "+agents.PromptFromFile(file) {
		t.Fatalf("%q", got)
	}
	if short, _ := agents.WriteLongPrompt(dir, "short"); short != "" {
		t.Fatal("a short prompt goes inline")
	}
}

// agent-start: env keys and values are checked before anything is typed.
func TestFormatStartEnv(t *testing.T) {
	got, err := agents.FormatStartEnv([]agents.EnvVar{{"APP_TASK", "PROJ-1"}, {"APP_MODE", "a.b_c"}}, "APP_")
	if err != nil || got != "APP_TASK='PROJ-1' APP_MODE='a.b_c' " {
		t.Fatalf("%q %v", got, err)
	}
	if got, _ := agents.FormatStartEnv(nil, "APP_"); got != "" {
		t.Fatal("empty")
	}
	for _, c := range []struct {
		env []agents.EnvVar
		msg string
	}{
		{[]agents.EnvVar{{"PATH", "x"}}, "Недопустимая переменная окружения PATH"},
		{[]agents.EnvVar{{"APP_X", "$(id)"}}, "Недопустимое значение переменной APP_X"},
		{[]agents.EnvVar{{"APP_x", "1"}}, "Недопустимая переменная окружения APP_x"},
		{make([]agents.EnvVar, 9), "Слишком много переменных окружения"},
	} {
		if _, err := agents.FormatStartEnv(c.env, "APP_"); err == nil || err.Error() != c.msg {
			t.Errorf("%v: %v", c.env, err)
		}
	}
}

func TestResolvePromptFiles(t *testing.T) {
	data := t.TempDir()
	in := filepath.Join(data, "a.md")
	_ = os.WriteFile(in, []byte("x"), 0o600)
	outside := filepath.Join(t.TempDir(), "b.md")
	_ = os.WriteFile(outside, []byte("x"), 0o600)
	link := filepath.Join(data, "link.md")
	_ = os.Symlink(outside, link)
	if got, err := agents.ResolvePromptFiles("claude", []string{in}, data, "~/.data"); err != nil || len(got) != 1 {
		t.Fatalf("%v %v", got, err)
	}
	for _, c := range []struct {
		provider string
		files    []string
		msg      string
	}{
		{"codex", []string{in}, "Файлы инструкций поддерживает только Claude"},
		{"claude", []string{in, in, in, in, in}, "Слишком много файлов инструкций"},
		{"claude", []string{link}, "Файл инструкций не найден в ~/.data"},
		{"claude", []string{"a.md"}, "Файл инструкций не найден в ~/.data"},
		{"claude", []string{data}, "Файл инструкций не найден в ~/.data"},
	} {
		if _, err := agents.ResolvePromptFiles(c.provider, c.files, data, "~/.data"); err == nil || err.Error() != c.msg {
			t.Errorf("%v: %v", c.files, err)
		}
	}
}

// launch-options: reads a stored or forwarded choice by shape only; re-checks before shell text.
func TestLaunchShape(t *testing.T) {
	if l, ok := agents.ReadLaunchShape(nil, nil); !ok || l != nil {
		t.Fatal("empty")
	}
	if l, ok := agents.ReadLaunchShape("gpt-5.4", "xhigh"); !ok || *l.Model != "gpt-5.4" || *l.Effort != "xhigh" {
		t.Fatal("valid")
	}
	if _, ok := agents.ReadLaunchShape("x y", nil); ok {
		t.Fatal("model")
	}
	if _, ok := agents.ReadLaunchShape(nil, "turbo"); ok {
		t.Fatal("effort")
	}
	bad := "opus'; rm -rf ~; '"
	if err := agents.CheckLaunch(&agents.Launch{Model: &bad}); err != agents.ErrLaunchModel {
		t.Fatal(err)
	}
	id := "$(id)"
	if err := agents.CheckLaunch(&agents.Launch{Effort: &id}); err != agents.ErrLaunchEffort {
		t.Fatal(err)
	}
	model, effort := "k3.1-pro", "high"
	if err := agents.CheckLaunch(&agents.Launch{Model: &model, Effort: &effort, Profile: "kimi-code"}); err != nil {
		t.Fatalf("a profile effort has a valid launch shape: %v", err)
	}
	if err := agents.CheckLaunch(&agents.Launch{Model: &model, Effort: &effort, Profile: "kimi-code", Ultracode: true}); err != agents.ErrLaunchProfile {
		t.Fatalf("a profile still takes no ultracode: %v", err)
	}
}

// claude-platform-hooks: recognises a bare agent command only for the tab's own agent.
func TestBareAgentArgs(t *testing.T) {
	for _, c := range []struct {
		content, panel, want string
		ok                   bool
	}{
		{"claude 'do x'\n", "claude-code", " 'do x'", true},
		{"claude", "claude-code", "", true},
		{`codex "fix"`, "codex-cli", ` "fix"`, true},
		{`codex "fix"`, "claude-code", "", false},
		{"claude x", "terminal", "", false},
		{"ls && claude x", "claude-code", "", false},
		{"  claude  a b  \r\n", "claude-code", "  a b", true},
	} {
		got, ok := agents.BareAgentArgs(c.content, c.panel)
		if got != c.want || ok != c.ok {
			t.Errorf("%q %s: %q %v", c.content, c.panel, got, ok)
		}
	}
}

const (
	sid   = "425055ec-bbc8-4e3c-87d0-36fa74c1cccb"
	other = "0b1f6a12-7c1e-4a55-9a0e-3d5f0b9a1c11"
	tpath = "/home/agent/.claude/projects/-srv-design-admin/" + sid + ".jsonl"
)

func ptr(s string) *string { return &s }

func tab(patch func(*agents.TabAgent)) agents.TabAgent {
	t := agents.TabAgent{ID: "tab-yg2wgj"}
	if patch != nil {
		patch(&t)
	}
	return t
}

// The tab of the failure: a valid transcript path and no session id.
func lost(t *agents.TabAgent) {
	t.AgentState = &agents.AgentState{ProviderID: "claude", JSONLPath: ptr(tpath), Summary: ptr("x")}
	t.ClaudeJSONLPath = ptr(tpath)
}

func history(entries ...agents.HistoryEntry) func() ([]agents.HistoryEntry, error) {
	return func() ([]agents.HistoryEntry, error) { return entries, nil }
}

func entry(tabID, id string, completed float64) agents.HistoryEntry {
	return agents.HistoryEntry{TabID: tabID, ProviderID: "claude", AgentSessionID: ptr(id), CompletedAt: completed}
}

func deps(patch func(*agents.RecoveryDeps)) agents.RecoveryDeps {
	d := agents.RecoveryDeps{
		Exists:         func(string) bool { return true },
		FindTranscript: func(string) string { return "" },
		History:        history(),
	}
	if patch != nil {
		patch(&d)
	}
	return d
}

// agent-session-recovery: the session id is the transcript file name.
func TestSessionIDFromTranscript(t *testing.T) {
	if agents.SessionIDFromTranscript(tpath) != sid || agents.SessionIDFromTranscript("/x/not-a-session.jsonl") != "" || agents.SessionIDFromTranscript("") != "" {
		t.Fatal("from path")
	}
	tabs := claude.Tabs{}
	if tabs.ReadSessionID(tab(lost)) != sid {
		t.Fatal("a tab with a transcript path and no id still knows its session")
	}
	withID := tab(func(t *agents.TabAgent) {
		t.AgentState = &agents.AgentState{ProviderID: "claude", SessionID: ptr(other), JSONLPath: ptr(tpath)}
	})
	if tabs.ReadSessionID(withID) != other || tabs.ReadSessionID(tab(nil)) != "" {
		t.Fatal("stored id first; none without either")
	}
}

// agent-session-recovery: recoverTabSession precedence.
func TestRecoverTabSession(t *testing.T) {
	tabs := claude.Tabs{}
	if got := agents.RecoverTabSession(tab(lost), tabs, deps(nil)); got == nil || *got != (agents.Recovered{SessionID: sid, JSONLPath: tpath}) {
		t.Fatalf("the transcript the tab kept wins: %+v", got)
	}
	found := "/home/agent/.claude/projects/-moved/" + other + ".jsonl"
	stored := tab(func(t *agents.TabAgent) {
		t.AgentState = &agents.AgentState{ProviderID: "claude", SessionID: ptr(other)}
	})
	got := agents.RecoverTabSession(stored, tabs, deps(func(d *agents.RecoveryDeps) {
		d.FindTranscript = func(id string) string {
			if id == other {
				return found
			}
			return ""
		}
	}))
	if got == nil || *got != (agents.Recovered{SessionID: other, JSONLPath: found}) {
		t.Fatalf("a stored id is found in whatever project folder holds it: %+v", got)
	}
	foundP := "/home/agent/.claude/projects/-p/" + other + ".jsonl"
	got = agents.RecoverTabSession(tab(lost), tabs, deps(func(d *agents.RecoveryDeps) {
		d.Exists = func(string) bool { return false }
		d.FindTranscript = func(id string) string {
			if id == other {
				return foundP
			}
			return ""
		}
		d.History = history(entry("tab-yg2wgj", "ffffffff-0000-4000-8000-000000000000", 10), entry("tab-yg2wgj", other, 90))
	}))
	if got == nil || *got != (agents.Recovered{SessionID: other, JSONLPath: foundP}) {
		t.Fatalf("a gone transcript falls back to the tab's history: %+v", got)
	}
	got = agents.RecoverTabSession(tab(nil), tabs, deps(func(d *agents.RecoveryDeps) {
		d.FindTranscript = func(id string) string { return "/p/" + id + ".jsonl" }
		d.History = history(entry("tab-other", "aaaaaaaa-0000-4000-8000-000000000000", 999), entry("tab-yg2wgj", other, 5), entry("tab-yg2wgj", sid, 50))
	}))
	if got == nil || got.SessionID != sid {
		t.Fatalf("only this tab's history, newest first: %+v", got)
	}
	if got := agents.RecoverTabSession(tab(nil), tabs, deps(nil)); got != nil {
		t.Fatal("a tab that never had a session recovers nothing")
	}
	if got := agents.RecoverTabSession(tab(nil), tabs, deps(func(d *agents.RecoveryDeps) {
		d.History = func() ([]agents.HistoryEntry, error) { return nil, errors.New("disk") }
	})); got != nil {
		t.Fatal("an unreadable history is no reason to fail")
	}
}
