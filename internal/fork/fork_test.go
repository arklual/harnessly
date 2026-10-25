package fork

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const oldClaude = "fd5a18f2-4887-4b7b-aa9d-7579d7faaf27"

// claudeTranscript is two turns as Claude Code writes them: the queueing of each prompt, the prompt, its attachments
// and the answer, with records about the session between them.
var claudeTranscript = []string{
	`{"type":"queue-operation","operation":"enqueue","sessionId":"` + oldClaude + `"}`,
	`{"type":"user","uuid":"5a88c886-0000-4000-8000-000000000001","parentUuid":null,"sessionId":"` + oldClaude + `","message":{"role":"user","content":"first"}}`,
	`{"type":"attachment","uuid":"cf7d97c4-0000-4000-8000-000000000002","parentUuid":"5a88c886-0000-4000-8000-000000000001","sessionId":"` + oldClaude + `"}`,
	`{"type":"assistant","uuid":"7a99c24e-0000-4000-8000-000000000003","parentUuid":"cf7d97c4-0000-4000-8000-000000000002","sessionId":"` + oldClaude + `","message":{"role":"assistant","content":[{"type":"text","text":"ALPHA"}]}}`,
	`{"type":"last-prompt","sessionId":"` + oldClaude + `"}`,
	`{"type":"queue-operation","operation":"enqueue","sessionId":"` + oldClaude + `"}`,
	`{"type":"queue-operation","operation":"dequeue","sessionId":"` + oldClaude + `"}`,
	`{"type":"user","uuid":"938c5724-0000-4000-8000-000000000004","parentUuid":"7a99c24e-0000-4000-8000-000000000003","sessionId":"` + oldClaude + `","message":{"role":"user","content":"second"}}`,
	`{"type":"assistant","uuid":"dc24dc82-0000-4000-8000-000000000005","parentUuid":"938c5724-0000-4000-8000-000000000004","sessionId":"` + oldClaude + `","message":{"role":"assistant","content":[{"type":"text","text":"BRAVO"}]}}`,
}

const oldCodex = "01a173e7-f374-7431-a36e-f1d2acf6cbd4"

// codexTranscript is two turns of a Codex rollout: the session's meta and instructions, then each turn's start, context,
// the person's message and its end.
var codexTranscript = []string{
	`{"type":"session_meta","payload":{"id":"` + oldCodex + `","cwd":"/w"}}`,
	`{"type":"event_msg","payload":{"type":"task_started"}}`,
	`{"type":"response_item","payload":{"type":"message","role":"developer","content":[]}}`,
	`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context/>"}]}}`,
	`{"type":"world_state","payload":{}}`,
	`{"type":"turn_context","payload":{}}`,
	`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"first"}]}}`,
	`{"type":"event_msg","payload":{"type":"user_message","message":"first"}}`,
	`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ALPHA"}]}}`,
	`{"type":"event_msg","payload":{"type":"task_complete"}}`,
	`{"type":"event_msg","payload":{"type":"thread_settings_applied"}}`,
	`{"type":"event_msg","payload":{"type":"task_started"}}`,
	`{"type":"turn_context","payload":{}}`,
	`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"second"}]}}`,
	`{"type":"event_msg","payload":{"type":"user_message","message":"second"}}`,
	`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"BRAVO"}]}}`,
	`{"type":"event_msg","payload":{"type":"task_complete"}}`,
}

// write lays the lines out as a transcript and returns its path and where each line starts.
func write(t *testing.T, dir, name string, lines []string) (string, []int64) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	var offsets []int64
	var b strings.Builder
	for _, l := range lines {
		offsets = append(offsets, int64(b.Len()))
		b.WriteString(l + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, offsets
}

func TestCutClaudeBeforeThePromptsQueueing(t *testing.T) {
	path, at := write(t, t.TempDir(), oldClaude+".jsonl", claudeTranscript)
	cut, err := Cut(Claude, path, "c:938c5724-0000-4000-8000-000000000004#0")
	if err != nil || cut != at[5] {
		t.Fatalf("cut = %d, %v; want %d (the first queue line of the second prompt)", cut, err, at[5])
	}
}

func TestCutClaudeFirstPromptKeepsNothing(t *testing.T) {
	path, _ := write(t, t.TempDir(), oldClaude+".jsonl", claudeTranscript)
	if _, err := Cut(Claude, path, "c:5a88c886-0000-4000-8000-000000000001#0"); !errors.Is(err, ErrNothingKept) {
		t.Fatalf("err = %v, want ErrNothingKept", err)
	}
}

func TestCutClaudeRefusesWhatIsNoPrompt(t *testing.T) {
	path, _ := write(t, t.TempDir(), oldClaude+".jsonl", claudeTranscript)
	for _, id := range []string{"c:7a99c24e-0000-4000-8000-000000000003#0", "c:00000000-0000-4000-8000-000000000000#0", "x:0#0", "c:#0"} {
		if _, err := Cut(Claude, path, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound", id, err)
		}
	}
}

func TestCutCodexBeforeTheTurnsStart(t *testing.T) {
	path, at := write(t, t.TempDir(), "rollout-x-"+oldCodex+".jsonl", codexTranscript)
	// The feed may name the turn by its response line or by its event line: the cut is the same.
	for _, line := range []int{13, 14} {
		cut, err := Cut(Codex, path, "x:"+strconv.FormatInt(at[line], 10)+"#0")
		if err != nil || cut != at[11] {
			t.Errorf("line %d: cut = %d, %v; want %d (the second turn's task_started)", line, cut, err, at[11])
		}
	}
	if _, err := Cut(Codex, path, "x:"+strconv.FormatInt(at[6], 10)+"#0"); !errors.Is(err, ErrNothingKept) {
		t.Errorf("first prompt: err = %v, want ErrNothingKept", err)
	}
	if _, err := Cut(Codex, path, "x:"+strconv.FormatInt(at[8], 10)+"#0"); !errors.Is(err, ErrNotFound) {
		t.Errorf("an answer: err = %v, want ErrNotFound", err)
	}
	if _, err := Cut(Codex, path, "x:7#0"); !errors.Is(err, ErrNotFound) {
		t.Errorf("mid-line offset: err = %v, want ErrNotFound", err)
	}
}

func TestWriteClaudeKeepsOffsetsAndRenamesTheSession(t *testing.T) {
	dir := t.TempDir()
	path, at := write(t, dir, oldClaude+".jsonl", claudeTranscript)
	old, _ := os.ReadFile(path)
	s, err := Write(Claude, path, oldClaude, at[5], time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(s.Path) != dir || filepath.Base(s.Path) != s.ID+".jsonl" || SessionID(s.Path) != s.ID || s.ID[14] != '4' {
		t.Fatalf("session = %+v", s)
	}
	got, _ := os.ReadFile(s.Path)
	if int64(len(got)) != at[5] || strings.Contains(string(got), oldClaude) || strings.Count(string(got), s.ID) != 5 {
		t.Fatalf("fork = %q", got)
	}
	if strings.ReplaceAll(string(got), s.ID, oldClaude) != string(old[:at[5]]) {
		t.Fatal("the fork is not the head of the transcript")
	}
	if now, _ := os.ReadFile(path); string(now) != string(old) {
		t.Fatal("the old transcript changed")
	}
}

func TestWriteCodexGoesToTodaysFolder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	path, at := write(t, root, filepath.Join("2026", "09", "25", "rollout-2026-09-25T20-48-47-"+oldCodex+".jsonl"), codexTranscript)
	now := time.Date(2026, 10, 25, 16, 40, 5, 0, time.Local)
	s, err := Write(Codex, path, oldCodex, at[11], now)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "2026", "10", "25", "rollout-2026-10-25T16-40-05-"+s.ID+".jsonl")
	if s.Path != want || s.ID[14] != '7' || SessionID(s.Path) != s.ID {
		t.Fatalf("session = %+v, want path %s", s, want)
	}
	got, _ := os.ReadFile(s.Path)
	if !strings.HasPrefix(string(got), `{"type":"session_meta","payload":{"id":"`+s.ID+`"`) || int64(len(got)) != at[11] {
		t.Fatalf("fork = %q", got)
	}
}

func TestWriteRefusesAnIDOfAnotherShape(t *testing.T) {
	path, _ := write(t, t.TempDir(), oldClaude+".jsonl", claudeTranscript)
	if _, err := Write(Claude, path, "short", 10, time.Now()); err == nil {
		t.Fatal("a session id of another length would move the offsets")
	}
}
