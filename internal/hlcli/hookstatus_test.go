package hlcli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hookspool"
	"github.com/ly-solutions/harnessly/internal/screen"
)

func runHook(t *testing.T, env map[string]string, session, stdin string, args ...string) result {
	t.Helper()
	var out, errs bytes.Buffer
	code := Run(args, Env{Getenv: func(k string) string { return env[k] }, Home: t.TempDir(), Session: func() string { return session },
		Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errs})
	return result{code, out.String(), errs.String()}
}

func spoolEvents(t *testing.T, dir string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, hookspool.File))
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var e map[string]any
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		out = append(out, e)
	}
	return out
}

// With the runtime away (no port file) the event stays in the spool, and the hook neither fails nor prints.
func TestStatusHookLeavesItsEventInTheSpoolWhenTheRuntimeIsAway(t *testing.T) {
	dir := t.TempDir()
	env := map[string]string{"HARNESSLY_DATA_DIR": dir, "HARNESSLY_SESSION": "hs-tab"}
	r := runHook(t, env, "", `{"session_id":"11111111-2222-4333-8444-555555555555","hook_event_name":"Stop"}`, "hook", "status", "stop")
	if r.code != 0 || r.out != "" || r.errs != "" {
		t.Fatalf("%+v", r)
	}
	events := spoolEvents(t, dir)
	if len(events) != 1 {
		t.Fatalf("%v", events)
	}
	e := events[0]
	if e["event"] != "stop" || e["session"] != "hs-tab" || e["agentSessionId"] != "11111111-2222-4333-8444-555555555555" ||
		e["id"] == "" || e["id"] == nil {
		t.Fatalf("%v", e)
	}
	if at, _ := e["at"].(float64); at < 1e12 {
		t.Fatalf("at %v is not milliseconds", e["at"])
	}
}

// With the runtime up the same event is delivered at once, with the id the spool holds it under.
func TestStatusHookDeliversWhatItWrote(t *testing.T) {
	f := newFake(t)
	dir := t.TempDir()
	env := map[string]string{"HARNESSLY_DATA_DIR": dir, "HARNESSLY_URL": f.srv.URL, "HARNESSLY_CLI_TOKEN": f.token}
	r := runHook(t, env, "hs-pane", `{"session_id":"s-1","notification_type":"permission_prompt"}`, "hook", "status", "notification")
	if r.code != 0 || r.out != "" || r.errs != "" {
		t.Fatalf("%+v", r)
	}
	f.mu.Lock()
	calls := append([]call(nil), f.calls...)
	f.mu.Unlock()
	if len(calls) != 1 || calls[0].Method != "POST" || calls[0].Path != "/api/status/hook" || calls[0].Token != f.token {
		t.Fatalf("%+v", calls)
	}
	sent, kept := calls[0].Body, spoolEvents(t, dir)[0]
	if sent["event"] != "notification" || sent["session"] != "hs-pane" || sent["notificationType"] != "permission_prompt" ||
		sent["agentSessionId"] != "s-1" || sent["id"] != kept["id"] || sent["at"] != kept["at"] {
		t.Fatalf("sent %v, kept %v", sent, kept)
	}
}

// The prompt-submit event names the prompt the agent took by its hash, never by its words; other events name none.
func TestStatusHookNamesThePromptByItsHash(t *testing.T) {
	dir := t.TempDir()
	env := map[string]string{"HARNESSLY_DATA_DIR": dir, "HARNESSLY_SESSION": "hs-tab"}
	runHook(t, env, "", `{"session_id":"s-1","prompt":"[Image #1] проверь  логи\n"}`, "hook", "status", "prompt-submit")
	runHook(t, env, "", `{"session_id":"s-1","prompt":"проверь логи"}`, "hook", "status", "stop")
	events := spoolEvents(t, dir)
	if len(events) != 2 {
		t.Fatalf("%v", events)
	}
	if hash, _ := events[0]["promptHash"].(string); hash == "" || hash != screen.MessageHash("проверь логи") {
		t.Fatalf("prompt-submit: %v", events[0])
	}
	if _, has := events[1]["promptHash"]; has {
		t.Fatalf("stop names a prompt: %v", events[1])
	}
	raw, _ := os.ReadFile(filepath.Join(dir, hookspool.File))
	if strings.Contains(string(raw), "логи") {
		t.Fatalf("the spool holds the prompt's words: %s", raw)
	}
}
