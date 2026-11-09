package hlcli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ly-solutions/harnessly/internal/apps"
)

func TestAppContextHookSendsOnlyWhatChanged(t *testing.T) {
	var calls atomic.Int32
	text := atomic.Value{}
	text.Store("Open reviews: 3")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]string{"text": text.Load().(string)})
	}))
	t.Cleanup(srv.Close)

	snap := apps.Snapshot{Apps: []apps.SnapshotApp{{ID: "acme.review", Name: "Ревью",
		Context: []apps.ContextItem{{ID: "queue", Tool: "api.queue", Mode: "on-activation"}}}}}
	raw, _ := json.Marshal(snap)
	sum := sha256.Sum256(raw)
	policy := filepath.Join(t.TempDir(), "policy-"+hex.EncodeToString(sum[:8])+".json")
	if err := os.WriteFile(policy, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"HARNESSLY_URL": srv.URL, "HARNESSLY_CLI_TOKEN": "tok", "HARNESSLY_SESSION": "tab-1"}
	run := func(stdin string) string {
		var out bytes.Buffer
		code := Run([]string{"hook", "app-context", "--policy", policy}, Env{Getenv: func(k string) string { return env[k] },
			Home: t.TempDir(), Stdin: strings.NewReader(stdin), Stdout: &out})
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
		return out.String()
	}
	prompt := `{"hook_event_name":"UserPromptSubmit"}`

	first := run(`{"hook_event_name":"SessionStart","source":"startup"}`)
	if !strings.Contains(first, "Open reviews: 3") || !strings.Contains(first, `"additionalContext"`) {
		t.Fatalf("the first context must go: %q", first)
	}
	if out := run(prompt); out != "" {
		t.Errorf("the context did not change, nothing goes: %q", out)
	}
	text.Store("Open reviews: 4")
	if out := run(prompt); !strings.Contains(out, "Open reviews: 4") {
		t.Errorf("a changed context goes: %q", out)
	}
	if out := run(prompt); out != "" {
		t.Errorf("sent once per change: %q", out)
	}
	if out := run(`{"hook_event_name":"SessionStart","source":"resume"}`); out != "" {
		t.Errorf("a resumed chat already has it: %q", out)
	}
	if out := run(`{"hook_event_name":"SessionStart","source":"clear"}`); !strings.Contains(out, "Open reviews: 4") {
		t.Errorf("a cleared chat gets it again: %q", out)
	}
	if calls.Load() == 0 {
		t.Error("the runtime was never asked")
	}
}

func TestAppValidateStrictCountsContextWeight(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pkg")
	if err := apps.Scaffold(dir, "acme.heavy", "", "chat"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "instructions.md"), []byte(strings.Repeat("x", 12<<10)), 0o644); err != nil {
		t.Fatal(err)
	}
	r := runWith(t, t.TempDir(), map[string]string{}, "", "app", "validate", "--strict", dir)
	if !strings.Contains(r.out, "context weight: ~3072 tokens") {
		t.Errorf("the line of weight is missing: %q", r.out)
	}
	if err := os.WriteFile(filepath.Join(dir, "instructions.md"), []byte(strings.Repeat("x", 15<<10)), 0o644); err != nil {
		t.Fatal(err)
	}
	r = runWith(t, t.TempDir(), map[string]string{}, "", "app", "validate", dir)
	if strings.Contains(r.out, "context weight") {
		t.Errorf("the weight is the strict check's: %q", r.out)
	}
}

// The live hook asks the runtime for the chat's session and hands its words to the agent; an empty answer, a
// failed one or another event prints nothing.
func TestAppContextLiveHook(t *testing.T) {
	text := atomic.Value{}
	text.Store("# Apps turned on in this chat")
	status := atomic.Int32{}
	status.Store(200)
	var asked atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		asked.Store(r.URL.Path + " " + b["session"])
		w.WriteHeader(int(status.Load()))
		_ = json.NewEncoder(w).Encode(map[string]string{"text": text.Load().(string)})
	}))
	t.Cleanup(srv.Close)
	env := map[string]string{"HARNESSLY_URL": srv.URL, "HARNESSLY_CLI_TOKEN": "tok", "HARNESSLY_SESSION": "tab-1"}
	run := func(stdin string) string {
		var out bytes.Buffer
		code := Run([]string{"hook", "app-context", "--live"}, Env{Getenv: func(k string) string { return env[k] },
			Home: t.TempDir(), Stdin: strings.NewReader(stdin), Stdout: &out})
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
		return out.String()
	}
	prompt := `{"hook_event_name":"UserPromptSubmit"}`
	got := run(prompt)
	if !strings.Contains(got, `"additionalContext":"# Apps turned on in this chat"`) || !strings.Contains(got, `"hookEventName":"UserPromptSubmit"`) {
		t.Fatalf("live words: %q", got)
	}
	if a, _ := asked.Load().(string); a != "/api/cli/apps/live-context tab-1" {
		t.Fatalf("asked %q", a)
	}
	if got := run(`{"hook_event_name":"SessionStart"}`); got != "" {
		t.Fatalf("another event: %q", got)
	}
	text.Store("")
	if got := run(prompt); got != "" {
		t.Fatalf("nothing changed: %q", got)
	}
	text.Store("x")
	status.Store(500)
	if got := run(prompt); got != "" {
		t.Fatalf("a failed answer: %q", got)
	}
}
