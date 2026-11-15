package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/bgmodel"
)

func TestBgAgentReadsClaudesStreamAsItComes(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Открою "}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"«Провайдеры»"}}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Открою «Провайдеры»"}]}}`,
		`{"type":"result","result":"Открою «Провайдеры»"}`,
	}, "\n")
	var got []string
	text := readClaudeStream(strings.NewReader(stream), func(s string) { got = append(got, s) })
	if text != "Открою «Провайдеры»" || len(got) != 2 {
		t.Fatalf("text %q, deltas %q", text, got)
	}
	// No deltas: the assistant's message comes whole.
	got = nil
	text = readClaudeStream(strings.NewReader(`{"type":"assistant","message":{"content":[{"type":"text","text":"Готово"}]}}`),
		func(s string) { got = append(got, s) })
	if text != "Готово" || len(got) != 1 {
		t.Fatalf("text %q, deltas %q", text, got)
	}
}

func TestBgAgentAsksClaudeWithTheFeaturesOwnRulesAndNothingOfTheMachine(t *testing.T) {
	args := bgClaudeArgs("haiku", "Ты отвечаешь по найденному.")
	for _, want := range []string{"--print", "--no-session-persistence", "--strict-mcp-config", "--disable-slash-commands"} {
		if !slices.Contains(args, want) {
			t.Fatalf("no %s in %q", want, args)
		}
	}
	if i := slices.Index(args, "--system-prompt"); i < 0 || args[i+1] != "Ты отвечаешь по найденному." {
		t.Fatalf("the feature's rules are not its system prompt: %q", args)
	}
	if i := slices.Index(args, "--tools"); i < 0 || args[i+1] != "" {
		t.Fatalf("tools are not off: %q", args)
	}
	if bgAgentPrompt(bgmodel.Call{System: "Правила", Prompt: "Вопрос"}) != "Правила\n\nВопрос" {
		t.Fatal("Codex does not get the rules first")
	}
}
