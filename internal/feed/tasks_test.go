package feed

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// claudeLines is a Claude transcript of the given messages, one line each: a user text, or an assistant turn with tool
// calls ("name:id:{input}"), or the user line with the results ("result:id:text").
func claudeLines(t *testing.T, msgs ...string) []byte {
	t.Helper()
	var b strings.Builder
	n := 0
	line := func(typ string, message any) {
		n++
		raw, err := json.Marshal(map[string]any{"parentUuid": fmt.Sprintf("00000000-0000-4000-8000-%012d", n-1), "isSidechain": false, "type": typ,
			"uuid": fmt.Sprintf("00000000-0000-4000-8000-%012d", n), "timestamp": fmt.Sprintf("2026-03-01T10:00:%02d.000Z", n), "userType": "external",
			"cwd": "/p", "sessionId": "11111111-2222-4333-8444-555555555555", "version": "2.1.300", "message": message})
		if err != nil {
			t.Fatal(err)
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	for _, m := range msgs {
		parts := strings.SplitN(m, ":", 3)
		switch {
		case len(parts) == 1:
			line("user", map[string]any{"role": "user", "content": m})
		case parts[0] == "result":
			line("user", map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": parts[1], "content": parts[2]}}})
		default:
			var input any
			if err := json.Unmarshal([]byte(parts[2]), &input); err != nil {
				t.Fatal(err)
			}
			line("assistant", map[string]any{"model": "claude-model-x", "id": "msg_" + parts[1], "type": "message", "role": "assistant",
				"content": []any{map[string]any{"type": "tool_use", "id": parts[1], "name": parts[0], "input": input}}})
		}
	}
	return []byte(b.String())
}

// The agent's tasks are a list of their own: a TaskCreate names its number only in its result, which a whole read of the
// file joins to the call and a file that grows brings later; both make the same list.
func TestTasksFromTheTranscript(t *testing.T) {
	raw := claudeLines(t,
		"Plan the work",
		`TaskCreate:tc1:{"subject":"Read the code","description":"d"}`,
		`TaskCreate:tc2:{"subject":"Write the page"}`,
		"result:tc1:Task #1 created successfully: Read the code",
		"result:tc2:Task #2 created successfully: Write the page",
		`TaskUpdate:tu1:{"taskId":"1","status":"in_progress"}`,
		"result:tu1:Updated task #1 status",
		`TaskUpdate:tu2:{"taskId":"1","status":"completed"}`,
		`TaskUpdate:tu3:{"taskId":"2","status":"in_progress"}`,
		`TaskCreate:tc3:{"subject":"Run the gates"}`,
		`TaskUpdate:tu4:{"taskId":"9","status":"completed"}`,
	)
	want := []wire.Task{
		{ID: "1", Subject: "Read the code", Status: wire.TaskCompleted},
		{ID: "2", Subject: "Write the page", Status: wire.TaskInProgress},
		{ID: "tc3", Subject: "Run the gates", Status: wire.TaskPending},
	}
	dir := t.TempDir()
	whole := filepath.Join(dir, "whole.jsonl")
	if err := os.WriteFile(whole, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	wf := New("c1", wire.ProviderClaude, source(wire.ProviderClaude, whole))
	wf.Load()
	if got := wf.Tasks(); !reflect.DeepEqual(got, want) {
		t.Fatalf("whole: %+v", got)
	}
	for _, size := range []int{0, 97, 1000} {
		path := filepath.Join(dir, fmt.Sprintf("grow-%d.jsonl", size))
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		f := New("c1", wire.ProviderClaude, source(wire.ProviderClaude, path))
		f.Load()
		for _, piece := range pieces(raw, size) {
			fh, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fh.Write(piece); err != nil {
				t.Fatal(err)
			}
			fh.Close()
			f.Poll()
		}
		if got := f.Tasks(); !reflect.DeepEqual(got, want) {
			t.Fatalf("pieces of %d: %+v", size, got)
		}
	}
	if got := New("c1", wire.ProviderClaude, source(wire.ProviderClaude, filepath.Join(dir, "none.jsonl"))); len(got.Tasks()) != 0 {
		t.Fatalf("a feed with no transcript has tasks: %+v", got.Tasks())
	}
}

// Codex sends its whole plan with each update: a line the list does not know is made, one it knows changes status.
func TestTasksOfACodexPlan(t *testing.T) {
	f := New("c1", wire.ProviderCodex, nil)
	for _, line := range []string{
		`{"id":"x:1#0","type":"task-progress","timestamp":1,"action":"update","taskId":"Read","subject":"Read","status":"in_progress"}`,
		`{"id":"x:2#0","type":"task-progress","timestamp":2,"action":"update","taskId":"Write","subject":"Write","status":"pending"}`,
		`{"id":"x:3#0","type":"task-progress","timestamp":3,"action":"update","taskId":"Read","subject":"Read","status":"completed"}`,
		`{"id":"x:4#0","type":"task-progress","timestamp":4,"action":"update","taskId":"Write","subject":"Write","status":"in_progress"}`,
	} {
		v, err := jsvalue.Parse([]byte(line))
		o, ok := v.(*jsvalue.Object)
		if err != nil || !ok {
			t.Fatal(err)
		}
		f.b.add(o, -1)
	}
	want := []wire.Task{{ID: "Read", Subject: "Read", Status: wire.TaskCompleted}, {ID: "Write", Subject: "Write", Status: wire.TaskInProgress}}
	if got := f.Tasks(); !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
}
