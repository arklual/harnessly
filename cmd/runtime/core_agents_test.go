package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/coreapi/layoutapi"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/preflight"
)

// testdata/launch-golden.json holds the lines the previous runtime's own builders typed (testdata/launch/gen.mjs).
func TestLaunchLinesMatchThePreviousRuntime(t *testing.T) {
	raw, err := os.ReadFile("testdata/launch-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name  string
		Skip  bool
		Input struct{ Provider, Resume, WorkspaceID, Model, Effort, Launcher string }
		Line  *string
		Error *string
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		data := t.TempDir()
		_ = os.MkdirAll(filepath.Join(data, "bin"), 0o700)
		_ = os.WriteFile(filepath.Join(data, "bin", "my-editor"), []byte("#!/bin/sh\n"), 0o700)
		_ = os.WriteFile(filepath.Join(data, "codex-launcher.js"), []byte("// launcher\n"), 0o700)
		cfg := filepath.Join(data, "config.json")
		skip, _ := json.Marshal(map[string]bool{"dangerouslySkipPermissions": c.Skip})
		_ = os.WriteFile(cfg, skip, 0o600)
		a := newCoreAgents(coreAgentsConfig{DataDir: data, Home: t.TempDir(), ConfigFile: cfg, Launcher: c.Input.Launcher,
			EditorScript: filepath.Join(data, "bin", "my-editor"), HumanEditorVar: "MY_EDITOR_VAR"})
		var launch *agents.Launch
		if c.Input.Model != "" || c.Input.Effort != "" {
			launch = &agents.Launch{}
			if c.Input.Model != "" {
				launch.Model = &c.Input.Model
			}
			if c.Input.Effort != "" {
				launch.Effort = &c.Input.Effort
			}
		}
		var line string
		if c.Input.Resume != "" {
			line, err = a.ResumeLine(c.Input.Provider, c.Input.Resume, c.Input.WorkspaceID, launch)
		} else {
			line, err = a.LaunchLine(c.Input.Provider, c.Input.WorkspaceID, launch)
		}
		if c.Error != nil {
			if err == nil || err.Error() != *c.Error {
				t.Errorf("%s: %q %v, want error %q", c.Name, line, err, *c.Error)
			}
			continue
		}
		want := strings.NewReplacer("$DATA", data, "$EDITOR_VAR", "MY_EDITOR_VAR", "$EDITOR_NAME", "my-editor").Replace(*c.Line)
		if err != nil || line != want {
			t.Errorf("%s:\n got %q %v\nwant %q", c.Name, line, err, want)
		}
	}
}

func TestLaunchLinesWithoutTheOldRuntimesFiles(t *testing.T) {
	data := t.TempDir()
	a := newCoreAgents(coreAgentsConfig{DataDir: data, Home: t.TempDir(), ConfigFile: filepath.Join(data, "none.json"), EditorScript: filepath.Join(data, "bin", "my-editor"), HumanEditorVar: "V"})
	line, err := a.LaunchLine(agents.Claude, "ws", nil)
	if err != nil || strings.Contains(line, "VISUAL") {
		t.Fatalf("without the editor script Claude starts without it: %q %v", line, err)
	}
	if _, err := a.LaunchLine(agents.Codex, "ws", nil); err == nil {
		t.Fatal("a Codex line without its launcher")
	}
	if _, err := os.Stat(filepath.Join(data, "codex-launcher.js")); err == nil {
		t.Fatal("the launcher is the old runtime's to write")
	}
}

func TestAvailability(t *testing.T) {
	dir := "/home/u/.local/bin"
	cases := []struct {
		r     preflight.Result
		panel string
		want  string
	}{
		{preflight.Result{Claude: preflight.Claude{Installed: true}}, "claude-code", ""},
		{preflight.Result{Claude: preflight.Claude{BinaryPath: &dir}}, "claude-code", `{"Code":"agent-path-missing","ProviderID":"claude","ProviderDisplayName":"Claude Code","PanelType":"claude-code","SuggestedCommand":"claude-path"}`},
		{preflight.Result{}, "claude-code", `{"Code":"agent-not-installed","ProviderID":"claude","ProviderDisplayName":"Claude Code","PanelType":"claude-code","SuggestedCommand":"claude"}`},
		{preflight.Result{}, "codex-cli", `{"Code":"agent-not-installed","ProviderID":"codex","ProviderDisplayName":"Codex","PanelType":"codex-cli","SuggestedCommand":null}`},
		{preflight.Result{Codex: preflight.Codex{Installed: true}}, "codex-cli", ""},
		{preflight.Result{}, "terminal", ""},
	}
	for _, c := range cases {
		u := availability(c.r, c.panel)
		got := ""
		if u != nil {
			b, _ := json.Marshal(u)
			got = string(b)
		}
		if got != c.want {
			t.Errorf("%s: %s", c.panel, got)
		}
	}
	var _ layoutapi.Agents = (*coreAgents)(nil)
}

func TestValidateLaunch(t *testing.T) {
	home := t.TempDir()
	a := newCoreAgents(coreAgentsConfig{DataDir: t.TempDir(), Home: home, ClaudeDir: filepath.Join(home, ".claude"), CodexDir: filepath.Join(home, ".codex")})
	for _, c := range []struct {
		provider      string
		model, effort any
		want          string
	}{
		{"claude", nil, nil, ""},
		{"claude", "Bad Model", nil, "Недопустимое имя модели"},
		{"claude", float64(1), nil, "Недопустимое имя модели"},
		{"claude", "opus", "ultra", "Недопустимая глубина рассуждения"},
		{"claude", "opus", "ultracode", ""},
		{"claude", nil, "high", ""},
		{"claude", "haiku", "high", "Модель haiku не поддерживает глубину «High»"},
		{"claude", "gpt-5", nil, "Модель «gpt-5» недоступна для Claude на этой машине"},
		{"codex", "gpt-5.4", nil, "Модель «gpt-5.4» недоступна для Codex на этой машине"},
		{"codex", nil, "xhigh", "Модель по умолчанию не поддерживает глубину «Extra high»"},
	} {
		_, got := a.ValidateLaunch(c.provider, c.model, c.effort)
		if got != c.want {
			t.Errorf("%s %v %v: %q, want %q", c.provider, c.model, c.effort, got, c.want)
		}
	}
	l, _ := a.ValidateLaunch("claude", "sonnet", nil)
	if l == nil || *l.Model != "sonnet" || l.Effort != nil {
		t.Fatal(l)
	}
}

func TestAgentStartGoesToTheOldRuntime(t *testing.T) {
	var got map[string]any
	answer := `{"ok":true,"conversation":{"id":"tab-new","title":"x"}}`
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/internal/core/agent-start" || r.Header.Get("x-tok") != "secret" {
			w.WriteHeader(404)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(answer))
	}))
	defer old.Close()
	data := t.TempDir()
	_ = os.WriteFile(filepath.Join(data, "cli-token"), []byte("secret\n"), 0o600)
	a := newCoreAgents(coreAgentsConfig{DataDir: data, Home: t.TempDir(),
		Old: &gate.OldRuntime{Upstream: strings.TrimPrefix(old.URL, "http://"), DataDir: data, TokenHeaders: []string{"x-tok"}}, OldAPI: "/api/internal"})
	model := "opus"
	r := a.Start(layoutapi.StartRequest{Provider: "claude", WorkspaceID: "ws-a", Prompt: "go", Launch: &agents.Launch{Model: &model}, TabName: nil})
	if r.ConversationID != "tab-new" || got["provider"] != "claude" || got["prompt"] != "go" || got["tabName"] != nil {
		t.Fatalf("%+v %v", r, got)
	}
	if l, _ := got["launch"].(map[string]any); l["model"] != "opus" || l["effort"] != nil {
		t.Fatal(got["launch"])
	}
	answer = `{"ok":false,"status":404,"body":{"error":"Проект не найден"}}`
	if r := a.Start(layoutapi.StartRequest{Provider: "claude"}); r.Status != 404 || string(r.Body) != `{"error":"Проект не найден"}` {
		t.Fatalf("%+v", r)
	}
	old.Close()
	if r := a.Start(layoutapi.StartRequest{Provider: "claude"}); r.Status != 502 {
		t.Fatalf("%+v", r)
	}
}

func TestAutoResumeWaitsForTheOldRuntime(t *testing.T) {
	var calls atomic.Int32
	done := make(chan struct{})
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable) // still starting
			return
		}
		w.WriteHeader(http.StatusOK)
		close(done)
	}))
	defer old.Close()
	saved := autoResumeRetry
	autoResumeRetry = 10 * time.Millisecond
	defer func() { autoResumeRetry = saved }()
	data := t.TempDir()
	a := newCoreAgents(coreAgentsConfig{DataDir: data, Home: t.TempDir(),
		Old: &gate.OldRuntime{Upstream: strings.TrimPrefix(old.URL, "http://"), DataDir: data, TokenHeaders: []string{"x-tok"}}, OldAPI: "/api/internal"})
	a.AutoResume()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("gave up after %d calls", calls.Load())
	}
}
