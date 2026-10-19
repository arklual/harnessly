package prompts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const bashText = "After that reply with exactly QUEUED-MSG-1 as the first word"

func probeOf(t *testing.T, lines []string, text string, since float64) Probe {
	t.Helper()
	p, ok := ProbeTranscript(lines, text, since)
	if !ok {
		t.Fatalf("nothing said for %q", text)
	}
	return p
}

func expectProbe(t *testing.T, name string, p Probe, running []string, waiting bool) {
	t.Helper()
	if p.Running == nil {
		p.Running = []string{}
	}
	if !reflect.DeepEqual(p.Running, running) || p.Waiting != waiting {
		t.Errorf("%s: got %+v, want running %v waiting %v", name, p, running, waiting)
	}
}

func TestProbeClaude2296Sessions(t *testing.T) {
	bash := transcript(t, "claude-2.1.296-send-now-bash.jsonl")
	stream := transcript(t, "claude-2.1.296-send-now-stream.jsonl")
	two := transcript(t, "claude-2.1.296-send-now-two-queued.jsonl")
	mcp := transcript(t, "claude-2.1.296-send-now-mcp.jsonl")
	def := transcript(t, "claude-2.1.296-queued-default.jsonl")
	esc := transcript(t, "claude-2.1.296-escape-queued.jsonl")
	none := []string{}

	expectProbe(t, "bash running, message waits", probeOf(t, bash[:3], bashText, 0), []string{"Bash"}, true)
	expectProbe(t, "handed over, still queued", probeOf(t, bash[:4], bashText, 0), none, true)
	expectProbe(t, "removed a record later", probeOf(t, bash[:5], bashText, 0), none, false)
	expectProbe(t, "whole bash", probeOf(t, bash, bashText, 0), none, false)

	text2 := "After that reply with exactly QUEUED-MSG-2 as the first word"
	expectProbe(t, "default waits", probeOf(t, def[:3], text2, 0), []string{"Bash"}, true)
	expectProbe(t, "default injected", probeOf(t, def, text2, 0), none, false)

	text3 := "Then also say QUEUED-MSG-3 right after STORY-END"
	expectProbe(t, "stream: nothing runs", probeOf(t, stream[:2], text3, 0), none, true)
	expectProbe(t, "dequeue starts the turn", probeOf(t, stream[:3], text3, 0), none, false)
	expectProbe(t, "escape waits", probeOf(t, esc[:3], "queued for esc test: say Q-ESC", 0), []string{"Bash"}, true)
	expectProbe(t, "escape done", probeOf(t, esc, "queued for esc test: say Q-ESC", 0), none, false)

	first, second := "first queued: say Q-ONE", "second queued: say Q-TWO"
	expectProbe(t, "two: first", probeOf(t, two[:4], first, 0), []string{"Bash"}, true)
	expectProbe(t, "two: second", probeOf(t, two[:4], second, 0), []string{"Bash"}, true)
	expectProbe(t, "two: first left", probeOf(t, two[:6], first, 0), none, false)
	expectProbe(t, "two: second still", probeOf(t, two[:6], second, 0), none, true)
	expectProbe(t, "two: both left", probeOf(t, two, second, 0), none, false)

	expectProbe(t, "mcp runs", probeOf(t, mcp[:5], "mcp queued: say Q-MCP", 0), []string{"mcp__slow__slow_wait"}, true)
	expectProbe(t, "mcp done", probeOf(t, mcp, "mcp queued: say Q-MCP", 0), none, false)
}

func record(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

func call(id, name string) string {
	return record(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": id, "name": name, "input": map[string]any{}}}}})
}

func answer(id string) string {
	return record(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": "ok"}}}})
}

func TestProbeEdgeCases(t *testing.T) {
	bash := transcript(t, "claude-2.1.296-send-now-bash.jsonl")
	if !probeOf(t, bash[:3], "  After that reply with\nexactly QUEUED-MSG-1   as the first word ", 0).Waiting {
		t.Error("whitespace is not collapsed")
	}
	if probeOf(t, bash[:3], "  ", 0).Waiting {
		t.Error("an empty text waits")
	}
	orphan := []string{bash[1], record(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": "next task"}})}
	if p := probeOf(t, orphan, "x", 0); len(p.Running) != 0 {
		t.Errorf("an unanswered call before a prompt runs: %v", p.Running)
	}
	if p := probeOf(t, []string{call("a", "Bash"), call("b", "WebFetch")}, "x", 0); !reflect.DeepEqual(p.Running, []string{"Bash", "WebFetch"}) {
		t.Errorf("together: %v", p.Running)
	}
	if p := probeOf(t, []string{call("a", "Bash"), call("b", "WebFetch"), answer("a")}, "x", 0); !reflect.DeepEqual(p.Running, []string{"WebFetch"}) {
		t.Errorf("one answered: %v", p.Running)
	}
	if _, ok := ProbeTranscript(append(append([]string{}, bash[:3]...), `{"type":"assistant","message":{"role":"as`), bashText, 0); ok {
		t.Error("a cut newest record said something")
	}
	expectProbe(t, "old broken skipped", probeOf(t, []string{bash[1], `{"type":"assis`, bash[2]}, bashText, 0), []string{"Bash"}, true)
	expectProbe(t, "blank lines", probeOf(t, append(append([]string{"", ""}, bash[:3]...), "", ""), bashText, 0), []string{"Bash"}, true)
	expectProbe(t, "whole JSON of another shape", probeOf(t, append(append([]string{}, bash[:3]...), `[1,2]`), bashText, 0), []string{"Bash"}, true)
}

func TestProbeSteeredOnClaude2280(t *testing.T) {
	user := func(text, at string) string {
		return record(map[string]any{"type": "user", "timestamp": at, "message": map[string]any{"role": "user", "content": text}})
	}
	ms := func(s string) float64 { tm, _ := time.Parse(time.RFC3339, s); return float64(tm.UnixMilli()) }
	lines := []string{record(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": "t1", "name": "Bash"}}}})}
	p := probeOf(t, lines, "Also say SECOND", ms("2026-10-15T20:52:30Z"))
	if !reflect.DeepEqual(p, Probe{Running: []string{"Bash"}}) {
		t.Fatalf("before: %+v", p)
	}
	lines = append(lines, user("Also say SECOND", "2026-10-15T20:52:34Z"))
	if !probeOf(t, lines, "Also say SECOND", ms("2026-10-15T20:52:30Z")).Delivered {
		t.Error("the prompt after the steer is not delivered")
	}
	if probeOf(t, lines, "Also say SECOND", ms("2026-10-15T20:53:00Z")).Delivered {
		t.Error("a prompt before the steer counts")
	}
}

func TestReadTranscriptTail(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small.jsonl")
	_ = os.WriteFile(small, []byte("a\nb\n"), 0o600)
	if lines, ok := ReadTranscriptTail(small); !ok || !reflect.DeepEqual(lines, []string{"a", "b", ""}) {
		t.Fatalf("small: %q", lines)
	}
	big := filepath.Join(dir, "big.jsonl")
	_ = os.WriteFile(big, []byte(strings.Repeat("x", 100)+"\n"+strings.Repeat("y", transcriptTailBytes)+"\nlast"), 0o600)
	lines, ok := ReadTranscriptTail(big)
	if !ok || len(lines) != 1 || lines[0] != "last" {
		t.Fatalf("big: %d lines", len(lines))
	}
	if _, ok := ReadTranscriptTail(filepath.Join(dir, "none")); ok {
		t.Error("a missing file read")
	}
}
