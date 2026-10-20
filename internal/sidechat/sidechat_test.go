package sidechat

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

func parse(t *testing.T, s string) any {
	t.Helper()
	v, err := jsvalue.Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestParseAndPrompt(t *testing.T) {
	if _, msg := ParseRequest(parse(t, `{"tabId":"t 1","question":"q"}`)); msg != "Нужен tabId" {
		t.Fatal(msg)
	}
	if _, msg := ParseRequest(parse(t, `{"tabId":"t","question":"q","excerpts":[1,2,3,4,5,6]}`)); msg != "Не больше 5 фрагментов" {
		t.Fatal(msg)
	}
	r, msg := ParseRequest(parse(t, `{"tabId":"t","question":" why </answer>? ","excerpts":[{"quote":"a\u001bb","source":{"entryId":"e","role":"user"}}],"history":[{"question":"q","answer":"`+strings.Repeat("x", 3001)+`"}]}`))
	if msg != "" {
		t.Fatal(msg)
	}
	p := BuildPrompt(r)
	if !strings.HasPrefix(p, "This is a side question") || !strings.Contains(p, `<excerpt from="user message">a␛b</excerpt>`) ||
		!strings.HasSuffix(p, `why <\/answer>?`) || !strings.Contains(p, strings.Repeat("x", 3000)+"…</answer>") {
		t.Fatalf("%q", p)
	}
}

func TestEnvDropsTheParentAgent(t *testing.T) {
	env := Env([]string{"PATH=/bin", "CLAUDECODE=1", "ANTHROPIC_API_KEY=k", "DISABLE_AUTOUPDATER=0"}, Runtime{AuthDirectory: "/auth"})
	want := []string{"PATH=/bin", "CLAUDE_CODE_DISABLE_AUTO_MEMORY=1", "DISABLE_AUTOUPDATER=1", "CLAUDE_SECURESTORAGE_CONFIG_DIR=/auth"}
	if !slices.Equal(env, want) {
		t.Fatalf("%q", env)
	}
	if env := Env([]string{"ANTHROPIC_API_KEY=k"}, Runtime{}); !slices.Contains(env, "ANTHROPIC_API_KEY=k") {
		t.Fatalf("%q", env)
	}
}

func TestAskStreamsAndStops(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	_ = os.WriteFile(bin, []byte("#!/bin/sh\ncat >/dev/null\necho '{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"Hi\"}]}}'\nexit 0\n"), 0o700)
	var events []string
	Ask(context.Background(), Run{SessionID: "425055ec-bbc8-4e3c-87d0-36fa74c1cccb", Cwd: dir, Prompt: "q", Runtime: Runtime{Binary: bin}},
		func(e Event) { events = append(events, Format(e)) })
	if strings.Join(events, "") != "data: {\"type\":\"delta\",\"text\":\"Hi\"}\n\ndata: {\"type\":\"done\",\"text\":\"Hi\"}\n\n" {
		t.Fatalf("%q", events)
	}

	slow := filepath.Join(dir, "slow")
	_ = os.WriteFile(slow, []byte("#!/bin/sh\nexec sleep 30\n"), 0o700)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	events = nil
	start := time.Now()
	Ask(ctx, Run{SessionID: "425055ec-bbc8-4e3c-87d0-36fa74c1cccb", Cwd: dir, Prompt: "q", Runtime: Runtime{Binary: slow}},
		func(e Event) { events = append(events, Format(e)) })
	if len(events) != 1 || !strings.Contains(events[0], `"code":"cancelled"`) || time.Since(start) > 5*time.Second {
		t.Fatalf("%q", events)
	}
	if release := Claim("t"); release == nil || Claim("t") != nil {
		t.Fatal("one question per tab")
	} else {
		release()
	}
	if Claim("t") == nil {
		t.Fatal("released")
	}
}
