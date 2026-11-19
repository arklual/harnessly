package transcript

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const agUser = `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-11-18T15:43:36Z","content":"<USER_REQUEST>hello</USER_REQUEST><ADDITIONAL_METADATA>secret</ADDITIONAL_METADATA>"}` + "\n"
const agReply = `{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","thinking":"hmm","content":"hi","model":"gemini-test","tool_calls":[{"name":"generate_image","args":{"Prompt":"mug"}}]}` + "\n"
const agResult = `{"step_index":2,"source":"MODEL","type":"GENERIC","status":"DONE","content":"Generated image","media":[{"mime_type":"image/jpeg","uri":"file:///tmp/mug.jpg"}]}` + "\n"

func TestAntigravityNormalization(t *testing.T) {
	entries := ParseAntigravityContent(agUser + agReply + agResult)
	if len(entries) != 5 {
		t.Fatalf("entries: %d", len(entries))
	}
	if entries[0].String("text") != "hello" {
		t.Fatal(entries[0])
	}
	want := []string{"user-message", "thinking", "assistant-message", "tool-call", "antigravity-result"}
	for i, e := range entries {
		if e.String("type") != want[i] {
			t.Fatal(e)
		}
		if e.String("status") != "" {
			t.Fatal("native DONE leaked")
		}
	}
	if got, _ := entries[4].Get("imagePaths"); !reflect.DeepEqual(got, []any{"/tmp/mug.jpg"}) {
		t.Fatal(got)
	}
	ignored := `{"source":"SYSTEM","type":"SYSTEM_MESSAGE","content":"not user"}` + "\n" + "broken\n" + `{"source":"FUTURE","type":"USER_INPUT","content":"not user"}` + "\n"
	if len(ParseAntigravityContent(ignored)) != 0 {
		t.Fatal("unknown/system record became message")
	}
	if len(ParseAntigravityContent(agUser[:len(agUser)-1])) != 0 {
		t.Fatal("partial last line consumed")
	}
}

func TestAntigravityPaging(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	content := agUser + agReply + agResult
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	all := ParseAntigravityContent(content)
	tail := ReadAntigravityTail(path, 1)
	before := ReadAntigravityBefore(path, tail.StartByteOffset, 100)
	got := append(before.Entries, tail.Entries...)
	if len(got) != len(all) {
		t.Fatalf("paged %d all %d", len(got), len(all))
	}
	for i := range all {
		if got[i].String("id") != all[i].String("id") {
			t.Fatal("unstable page ids")
		}
	}
	inc := ParseAntigravityAppended([]byte(agReply+"{partial"), int64(len(agUser)))
	if inc.NewOffset != int64(len(agUser+agReply)) || len(inc.NewEntries) != 3 {
		t.Fatal(inc)
	}
	for i, e := range inc.NewEntries {
		if e.String("id") != all[i+1].String("id") {
			t.Fatal("unstable incremental ids")
		}
	}
}
