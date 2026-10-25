package design

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type linksRig struct {
	lib    *Library
	root   string
	claude string
	codex  string
	data   string
}

func newLinksRig(t *testing.T) linksRig {
	t.Helper()
	base := t.TempDir()
	r := linksRig{root: filepath.Join(base, "lib"), claude: filepath.Join(base, "claude"), codex: filepath.Join(base, ".codex", "sessions"),
		data: filepath.Join(base, "data")}
	for _, d := range []string{r.root, r.claude, r.codex, r.data} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	r.lib = New(Config{LibraryDir: r.root, ClaudeProjects: r.claude, CodexSessions: r.codex, Texts: Texts{CLI: "acme"}})
	return r
}

// toolUse is a Claude transcript line of one tool use, as Claude Code writes it.
func toolUse(t *testing.T, name string, input map[string]any) string {
	t.Helper()
	line := map[string]any{"parentUuid": "p", "isSidechain": false, "type": "assistant", "cwd": "/home/coder/workspace/acme",
		"sessionId": "s", "timestamp": "2026-10-24T23:31:00.000Z",
		"message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "tool_use", "id": "toolu_1", "name": name, "input": input}}}}
	raw, err := json.Marshal(line)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw) + "\n"
}

// toolResult is the line of what a tool answered: it names the paths it read, and is no link.
func toolResult(t *testing.T, text string) string {
	t.Helper()
	line := map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "tool_result", "tool_use_id": "toolu_1", "content": text}}}}
	raw, _ := json.Marshal(line)
	return string(raw) + "\n"
}

func codexLine(t *testing.T, payload map[string]any) string {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"timestamp": "2026-10-24T23:31:00.000Z", "type": "response_item", "payload": payload})
	return string(raw) + "\n"
}

func writeLines(t *testing.T, p string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(strings.Join(lines, "")); err != nil {
		t.Fatal(err)
	}
}

func TestChatLinksClaudeTools(t *testing.T) {
	r := newLinksRig(t)
	p := filepath.Join(r.claude, "-home-coder-workspace-acme", "564bd724.jsonl")
	board := filepath.Join(r.root, "ostrovok-dossier", "project", "Главная.dc.html")
	writeLines(t, p,
		`{"type":"last-prompt","sessionId":"s"}`+"\n",
		// Read only: what it looked at is no work on the design.
		toolUse(t, "Read", map[string]any{"file_path": filepath.Join(r.root, "harnessly", "project", "Главная.dc.html")}),
		toolUse(t, "Bash", map[string]any{"command": "cd " + r.root + "; ls -la; cat harnessly/artifact.json; ls " + r.root + "/acme-admin/project"}),
		toolUse(t, "Bash", map[string]any{"command": "ls " + r.root + "/quiet/project 2>&1 | head; cat " + r.root + "/quiet/a.json 2>/dev/null >/dev/null"}),
		toolResult(t, "written "+filepath.Join(r.root, "pitch-q4", "project", "a.dc.html")),
		toolUse(t, "Write", map[string]any{"file_path": board, "content": "<!DOCTYPE html>\n<html>…"}),
		toolUse(t, "Edit", map[string]any{"file_path": filepath.Join(r.root, "landing", "project", "Меню.dc.html"), "old_string": "a", "new_string": "b"}),
		toolUse(t, "Bash", map[string]any{"command": "harnessly open design cafe-menu 2>&1 | tail -3"}),
		toolUse(t, "Bash", map[string]any{"command": "acme design panel promo show --board \"Главная\" --set loading=true"}),
		toolUse(t, "Bash", map[string]any{"command": "cp /tmp/x.png " + r.root + "/assets-only/project/assets/x.png"}),
		toolUse(t, "Bash", map[string]any{"command": "acme design page create new-pages --title \"Меню\" 2>&1 | tail -2"}),
		toolUse(t, "Bash", map[string]any{"command": "acme design check checked --board \"Главная\""}),
		toolUse(t, "Write", map[string]any{"file_path": "/home/coder/notes.md", "content": "Главная.dc.html " + board}),
	)
	l := NewChatLinks(r.lib, "")
	got := l.Designs([]string{p})[p]
	want := []string{"ostrovok-dossier", "landing", "cafe-menu", "panel", "promo", "show", "assets-only", "page", "create", "new-pages", "check",
		"checked"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("designs = %v, want %v", got, want)
	}
}

func TestChatLinksCodex(t *testing.T) {
	r := newLinksRig(t)
	p := filepath.Join(r.codex, "2026", "10", "24", "rollout-1.jsonl")
	writeLines(t, p,
		codexLine(t, map[string]any{"type": "function_call", "name": "exec_command", "call_id": "c1",
			"arguments": `{"cmd":"sed -n 1,40p ` + r.root + `/read-only/project/a.dc.html"}`}),
		codexLine(t, map[string]any{"type": "function_call", "name": "shell", "call_id": "c2",
			"arguments": `{"command":["bash","-lc","echo hi > ` + r.root + `/shell-wrote/project/x.txt"]}`}),
		codexLine(t, map[string]any{"type": "custom_tool_call", "name": "apply_patch", "call_id": "c3", "status": "completed",
			"input": "*** Begin Patch\n*** Update File: " + r.root + "/patched/project/Главная.dc.html\n@@\n-a\n+b\n*** End Patch\n"}),
		codexLine(t, map[string]any{"type": "function_call_output", "call_id": "c3", "output": r.root + "/output-only/x"}),
	)
	l := NewChatLinks(r.lib, "")
	got := l.Designs([]string{p})[p]
	want := []string{"shell-wrote", "patched"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("designs = %v, want %v", got, want)
	}
}

func TestChatLinksGrowsAndKeeps(t *testing.T) {
	r := newLinksRig(t)
	p := filepath.Join(r.claude, "proj", "a.jsonl")
	file := filepath.Join(r.data, "designly-chats.json")
	writeLines(t, p, toolUse(t, "Write", map[string]any{"file_path": filepath.Join(r.root, "one", "project", "A.dc.html"), "content": "x"}))
	// A line still being written is not read until it is whole.
	half := toolUse(t, "Write", map[string]any{"file_path": filepath.Join(r.root, "two", "project", "B.dc.html"), "content": "x"})
	writeLines(t, p, half[:len(half)/2])
	l := NewChatLinks(r.lib, file)
	if got := l.Designs([]string{p})[p]; !reflect.DeepEqual(got, []string{"one"}) {
		t.Fatalf("first read = %v", got)
	}
	writeLines(t, p, half[len(half)/2:])
	if got := l.Designs([]string{p})[p]; !reflect.DeepEqual(got, []string{"one", "two"}) {
		t.Fatalf("after growth = %v", got)
	}
	if err := l.Save(); err != nil {
		t.Fatal(err)
	}
	// Another runtime reads the kept index: what was read is not read again.
	again := NewChatLinks(r.lib, file)
	again.mu.Lock()
	again.load()
	e := again.st.Files[p]
	again.mu.Unlock()
	st, _ := os.Stat(p)
	if e == nil || e.Offset != st.Size() || !reflect.DeepEqual(e.Designs, []string{"one", "two"}) {
		t.Fatalf("kept = %+v (size %d)", e, st.Size())
	}
}

func TestChatLinksSubagentsAndAllowed(t *testing.T) {
	r := newLinksRig(t)
	p := filepath.Join(r.claude, "proj", "sess.jsonl")
	writeLines(t, p, `{"type":"mode"}`+"\n")
	writeLines(t, filepath.Join(r.claude, "proj", "sess", "subagents", "agent-1.jsonl"),
		toolUse(t, "Write", map[string]any{"file_path": filepath.Join(r.root, "by-subagent", "project", "A.dc.html"), "content": "x"}))
	outside := filepath.Join(t.TempDir(), "x.jsonl")
	writeLines(t, outside, toolUse(t, "Write", map[string]any{"file_path": filepath.Join(r.root, "nope", "project", "A.dc.html")}))
	l := NewChatLinks(r.lib, "")
	got := l.Designs([]string{p, outside})
	if !reflect.DeepEqual(got, map[string][]string{p: {"by-subagent"}}) {
		t.Fatalf("designs = %v", got)
	}
}

func TestChatLinksLongGrowthReadBehind(t *testing.T) {
	r := newLinksRig(t)
	p := filepath.Join(r.claude, "proj", "big.jsonl")
	filler := toolResult(t, strings.Repeat("x", 64<<10))
	var lines []string
	for range (chatLinksSyncBytes / len(filler)) + 2 {
		lines = append(lines, filler)
	}
	lines = append(lines, toolUse(t, "Write", map[string]any{"file_path": filepath.Join(r.root, "late", "project", "A.dc.html")}))
	writeLines(t, p, lines...)
	l := NewChatLinks(r.lib, "")
	if got := l.Designs([]string{p})[p]; got != nil {
		t.Fatalf("a long growth is read behind, got %v at once", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); l.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if got := l.Designs([]string{p})[p]; reflect.DeepEqual(got, []string{"late"}) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Run did not read the transcript")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
}

func TestChatsOfOrdersByActivity(t *testing.T) {
	links := map[string][]string{"/a.jsonl": {"d1", "d2"}, "/b.jsonl": {"d1"}}
	got := ChatsOf(links, map[string]string{"tab-a": "/a.jsonl", "tab-b": "/b.jsonl", "tab-c": "/c.jsonl"},
		map[string]int64{"tab-a": 10, "tab-b": 20})
	want := map[string][]string{"d1": {"tab-b", "tab-a"}, "d2": {"tab-a"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ChatsOf = %v, want %v", got, want)
	}
}
