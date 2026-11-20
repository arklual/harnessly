package antigravity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/wire"
)

const testID = "12345678-1234-1234-1234-123456789abc"

func ptr(s string) *string { return &s }

func TestIdentityAndBareCommand(t *testing.T) {
	if agents.DisplayName(ProviderID) != DisplayName || agents.PanelOf(ProviderID) != PanelType || agents.ProviderOfPanel(PanelType) != ProviderID {
		t.Fatal("identity mapping")
	}
	if args, ok := agents.BareAgentArgs("agy --model gemini\n", PanelType); !ok || args != " --model gemini" {
		t.Fatalf("bare args %q %v", args, ok)
	}
	if _, ok := agents.BareAgentArgs("antigravity", PanelType); ok {
		t.Fatal("provider is not executable")
	}
	if got := agents.WithInitialPrompt(ProviderID, Binary, "hello", ""); !strings.Contains(got, " --prompt-interactive ") {
		t.Fatal(got)
	}
}

func TestNativePermissionModes(t *testing.T) {
	for _, mode := range []string{"accept-edits", "auto", "plan"} {
		flags, err := (LaunchConfig{Mode: mode}).Flags("", nil, nil)
		if err != nil || !strings.Contains(flags, "--mode '"+mode+"'") {
			t.Fatalf("mode %q: %q %v", mode, flags, err)
		}
	}
	if _, err := (LaunchConfig{Mode: "default"}).Flags("", nil, nil); err == nil {
		t.Fatal("unsupported CLI default mode accepted")
	}
	flags, err := (LaunchConfig{Mode: "auto"}).Flags("", &agents.Launch{Permission: wire.PermissionAsk}, nil)
	if err != nil || strings.Contains(flags, "--mode") {
		t.Fatalf("ask must omit mode: %q %v", flags, err)
	}
	flags, err = (LaunchConfig{}).Flags("", nil, nil)
	if err != nil || strings.Contains(flags, "--mode") {
		t.Fatalf("default must omit mode: %q %v", flags, err)
	}
}

func TestLaunchNativeOnly(t *testing.T) {
	c := LaunchConfig{Launcher: "/opt/agy1.5.11", Settings: `{"trustedWorkspaces":[]}`, Sandbox: true}
	line, err := c.ResumeCommand(testID, "", &agents.Launch{Model: ptr("gemini-3-pro"), Effort: ptr("xhigh"), Permission: wire.PermissionPlan})
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--conversation", "--settings", "--sandbox", "--mode 'plan'", "--effort 'xhigh'"} {
		if !strings.Contains(line, flag) {
			t.Fatalf("missing %s: %s", flag, line)
		}
	}
	if strings.Contains(line, "skip-permissions") || strings.Contains(line, "--resume") || c.RunsWithHooks() {
		t.Fatal("foreign launch semantics")
	}
	for _, effort := range []string{"none", "minimal", "ultra", "ultracode"} {
		if _, err := LaunchFlags(&agents.Launch{Effort: ptr(effort)}); err == nil {
			t.Fatal(effort)
		}
	}
	for _, effort := range []string{"low", "medium", "high", "xhigh", "max"} {
		if _, err := LaunchFlags(&agents.Launch{Effort: ptr(effort)}); err != nil {
			t.Fatal(effort, err)
		}
	}
	for _, launch := range []*agents.Launch{{Fast: true}, {Ultracode: true}, {Profile: "other", Model: ptr("model")}, {Apps: []string{"test.app"}}, {Input: "chat"}, {Permission: wire.PermissionBypass}} {
		if _, err := c.LaunchCommand("", launch, nil); err == nil {
			t.Fatalf("accepted %+v", launch)
		}
	}
	if _, err := c.ResumeCommand("../oops", "", nil); err == nil {
		t.Fatal("invalid UUID accepted")
	}
	if _, err := c.LaunchCommand("", nil, []string{"/tmp/prompt"}); err == nil {
		t.Fatal("unsupported instruction file")
	}
}

func TestNativeSessionPath(t *testing.T) {
	home := t.TempDir()
	path := TranscriptPath(home, testID)
	if SessionIDFromPath(path) != testID || !ValidateTranscriptPath(home, path, testID) {
		t.Fatal(path)
	}
	full := filepath.Join(filepath.Dir(path), "transcript_full.jsonl")
	if SessionIDFromPath(full) != testID || !ValidateTranscriptPath(home, full, testID) {
		t.Fatal(full)
	}
	for _, name := range []string{"transcript_backup.jsonl", "transcript_full.jsonl.bak", "transcript.jsonl.bak"} {
		bad := filepath.Join(filepath.Dir(path), name)
		if SessionIDFromPath(bad) != "" || ValidateTranscriptPath(home, bad, testID) {
			t.Fatal("arbitrary transcript accepted", bad)
		}
	}
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	if meta, ok := NewDetector(home).FindSession(testID); !ok || meta.JSONLPath != full {
		t.Fatalf("full transcript detection %+v %v", meta, ok)
	}
	if err := os.RemoveAll(filepath.Join(Dir(home), "brain")); err != nil {
		t.Fatal(err)
	}
	if ValidateTranscriptPath(home, path, "87654321-1234-1234-1234-123456789abc") {
		t.Fatal("cross-session binding")
	}
	if SessionIDFromPath(filepath.Join(home, testID+".jsonl")) != "" {
		t.Fatal("filename inferred")
	}
	outside := t.TempDir()
	if err := os.MkdirAll(Dir(home), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(Dir(home), "brain")); err != nil {
		t.Fatal(err)
	}
	if ValidateTranscriptPath(home, path, testID) || ValidateTranscriptPath(home, full, testID) {
		t.Fatal("symlink escape accepted")
	}
}

func TestProcessAndResume(t *testing.T) {
	for _, name := range []string{"agy", "/home/user/.local/bin/agy1.5.11"} {
		if !MatchesProcess(name, nil) {
			t.Fatal(name)
		}
	}
	for _, name := range []string{"claude", "codex", "node", "agy-malicious"} {
		if MatchesProcess(name, []string{"agy"}) {
			t.Fatal(name)
		}
	}
	for _, arg := range []string{"agy --conversation " + testID, "agy --conversation='" + testID + "'"} {
		if ResumeID(arg) != testID {
			t.Fatal(arg)
		}
	}
	if ResumeID("agy --conversation "+testID+"extra") != "" {
		t.Fatal("partial UUID")
	}
}

func TestModelsBounded(t *testing.T) {
	for _, input := range []string{`[{"id":"gemini-3-pro","label":"Gemini 3 Pro"}]`, `{"models":[{"id":"gemini-3-pro","name":"Gemini 3 Pro"}]}`, "gemini-3-pro  Gemini 3 Pro\n"} {
		models, err := ParseModels([]byte(input))
		if err != nil || len(models) != 1 || models[0].Label != "Gemini 3 Pro" {
			t.Fatalf("%+v %v", models, err)
		}
	}
	for _, input := range []string{"", `[{"id":"bad;command"}]`, strings.Repeat("x", MaxModelsOutput+1)} {
		if _, err := ParseModels([]byte(input)); err == nil {
			t.Fatal("invalid listing accepted")
		}
	}
}

func TestNativeSnapshotDoesNotFinishOnStepDone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := TranscriptPath(home, testID)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	data := `{"step_index":1,"source":"USER_EXPLICIT","type":"USER_INPUT","content":"hello"}` + "\n" + `{"step_index":2,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","content":"answer","model":"gemini-3-pro"}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	snap, ok := ReadSnapshot(path)
	if !ok || snap.Idle || snap.LastAssistantSnippet == nil || *snap.LastAssistantSnippet != "answer" {
		t.Fatalf("%+v %v", snap, ok)
	}
	stats := ReadHistoryStats(path)
	if stats.LastUserText == nil || *stats.LastUserText != "hello" || stats.LastAssistantText == nil || *stats.LastAssistantText != "answer" {
		t.Fatalf("%+v", stats)
	}
}

func TestConservativeSnapshotAndHook(t *testing.T) {
	if snap, ok := ReadSnapshot("anything"); ok || snap.Idle {
		t.Fatal("unvalidated completion")
	}
	home := t.TempDir()
	if _, err := ParseHookPayload(home, []byte(`{"conversationId":"`+testID+`","transcriptPath":"`+TranscriptPath(home, testID)+`"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseHookPayload(home, []byte(`{"conversationId":"`+testID+`","transcriptPath":"/tmp/transcript.jsonl"}`)); err == nil {
		t.Fatal("foreign transcript")
	}
	if _, err := ParseHookPayload(home, []byte(`{}`)); err == nil {
		t.Fatal("unbound hook")
	}
}
