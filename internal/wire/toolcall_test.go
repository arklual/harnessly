package wire

import (
	"encoding/json"
	"strings"
	"testing"
)

// a tool entry's call is optional both ways — an older runtime sends none and a newer page reads the entry
// whole; an entry without anything known of its call goes out without the field, as an older page expects.
func TestToolCallIsOptional(t *testing.T) {
	old := `{"id":"x:1#0","turnId":null,"at":1,"rev":1,"t":"tool","toolUseId":"tu","tool":"Bash","summary":"$ ls","ok":true,"output":null}`
	var e Entry
	if err := json.Unmarshal([]byte(old), &e); err != nil {
		t.Fatalf("an entry of an older runtime, without call: %v", err)
	}
	if e.Call != nil {
		t.Fatalf("call: %+v, want none", e.Call)
	}
	out, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), `"call"`) {
		t.Fatalf("an entry without a call writes one: %s", out)
	}

	exit, ms := int64(2), int64(1300)
	e.Call = &ToolCall{Description: "Runs the bench", DurationMs: &ms, Exit: &exit, Lines: 2, Tail: "boom"}
	out, err = json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var back Entry
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("round trip: %v (%s)", err, out)
	}
	if back.Call == nil || back.Call.Description != "Runs the bench" || *back.Call.Exit != 2 || back.Call.Offset != 0 {
		t.Fatalf("round trip: %+v", back.Call)
	}
	// Fields not known stay out: an empty offset or command is not sent.
	if strings.Contains(string(out), `"offset"`) || strings.Contains(string(out), `"command"`) {
		t.Fatalf("empty fields of the call are sent: %s", out)
	}
}
