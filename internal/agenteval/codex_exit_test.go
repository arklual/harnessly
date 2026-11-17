package agenteval

import (
	"os"
	"path/filepath"
	"testing"
)

// Regression: Codex's code-mode text(r.output) drops the command's exit status;
// the authoritative item_completed event must still prevent a false pass.
func TestCodexCommandEventFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	raw := `{"type":"response_item","payload":{"type":"custom_tool_call","call_id":"c1","name":"exec","input":"const r = await tools.exec_command({cmd:\"harnessly design export shop Каталог\"}); text(r.output);"}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","command":["/bin/bash","-lc","harnessly design export shop Каталог"],"status":"failed","exit_code":2}}}
{"type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"c1","output":[{"type":"input_text","text":"Script completed\nOutput:\nChrome is not installed"}]}}
{"type":"response_item","payload":{"type":"function_call","call_id":"c2","name":"exec_command","arguments":"{\"cmd\":\"true\"}"}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","command":["/bin/bash","-lc","true"],"status":"completed","exit_code":0}}}
{"type":"response_item","payload":{"type":"function_call_output","call_id":"c2","output":"done"}}
`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := ReadCodexRollout(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 2 {
		t.Fatalf("calls=%+v", f.Calls)
	}
	if !f.Calls[0].Error {
		t.Fatal("failed export marked successful despite exit_code=2")
	}
	if f.Calls[1].Error {
		t.Fatal("successful command marked failed")
	}
}
