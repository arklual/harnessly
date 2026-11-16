package agenteval

import (
	"encoding/json"
	"os"
	"testing"
)

func call(session int, name, args string) Call {
	return Call{Session: session, Name: name, Args: json.RawMessage(args)}
}

func TestMetricsOf(t *testing.T) {
	r := Result{TurnState: "needs-input", Facts: &Facts{TokensIn: 10, CacheRead: 100, CacheWrite: 20, TokensOut: 5, Sessions: 3, Calls: []Call{
		call(0, "Agent", `{"prompt":"back"}`),
		call(0, "Agent", `{"prompt":"front","team_name":"t","name":"fe"}`),
		call(0, "harnessly.delegate_task", `{"prompt":"x"}`),
		call(0, "Bash", `{"command":"harnessly tab create -t claude-code --prompt hi"}`),
		call(0, "AskUserQuestion", `{}`),
		call(0, "REPL", `{"code":"1"}`),
		call(0, "Agent", `{"prompt":"t","name":"mate-1"}`),
		call(2, "SendMessage", `{"to":"team-lead","message":"done"}`),
		call(0, "harnessly.send_to_conversation", `{"id":"x","text":"stop"}`),
		call(0, "Edit", `{"file_path":"/w/a.py","old_string":"x","new_string":"y"}`),
		call(1, "Edit", `{"file_path":"/w/a.py","old_string":"x","new_string":"y"}`),
		call(2, "Write", `{"file_path":"/w/b.py","content":"z"}`),
		call(2, "Write", `{"file_path":"/w/b.py","content":"z2"}`),
	}}}
	m := MetricsOf(r)
	want := Metrics{Tokens: 135, Sessions: 3, Subagents: 1, Teammates: 2, Messages: 2, Delegations: 2, Asks: 2, Repl: 1, LeadEdits: 1, Conflicts: 1, Duplicates: 1}
	if m != want {
		t.Fatalf("got %+v\nwant %+v", m, want)
	}
}

func TestCheckExpectation(t *testing.T) {
	sc, err := ParseScenario("c.yaml", []byte(`id: cc
prompt: do
settle_s: 30
interrupt: {after_s: 20, text: stop}
expect:
  - check: test -f a
  - any_of:
      - check: test -f b
      - text: ok
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := sc.Checks(); len(got) != 2 || got[0] != "test -f a" || got[1] != "test -f b" {
		t.Fatalf("checks %v", got)
	}
	v := sc.Check(Facts{Checks: []CheckResult{{Command: "test -f a", Exit: 0}, {Command: "test -f b", Exit: 1, Output: "no"}}})
	if !v[0].OK || v[1].OK {
		t.Fatalf("verdicts %+v", v)
	}
	if v := sc.Check(Facts{}); v[0].OK || v[0].Why != "the check did not run" {
		t.Fatalf("a check that did not run passed: %+v", v)
	}
	if _, err := ParseScenario("c.yaml", []byte("id: cc\nprompt: do\ntimeout_s: 60\ninterrupt: {after_s: 90, text: stop}\nexpect: [{text: x}]\n")); err == nil {
		t.Fatal("an interrupt after the timeout was taken")
	}
}

func TestPendingCalls(t *testing.T) {
	f := t.TempDir() + "/s.jsonl"
	lines := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"a","name":"Bash","input":{}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"a","content":"ok"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"b","name":"Bash","input":{}}]}}
`
	if err := os.WriteFile(f, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	if n := pendingCalls(f); n != 1 {
		t.Fatalf("pending %d, want 1", n)
	}
}
