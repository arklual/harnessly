package codex

import "testing"

func TestSummarizeFunctionCallCodeMode(t *testing.T) {
	got := SummarizeFunctionCall("exec", `text(await tools.exec_command({cmd:"git status",max_output_tokens:2000}));`)
	if got != "$ git status" {
		t.Errorf("got %q", got)
	}
	if a := functionCallAction("exec", `text(await tools.web__run({search_query:[{q:"go"}]}))`); *a.ToolName != "WebSearch" || a.Summary != "go" {
		t.Errorf("got %v %q", *a.ToolName, a.Summary)
	}
}
