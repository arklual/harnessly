package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// Claude reads one --settings file. MCP permissions may extend hooks.json into a
// derived file, so validate the settings actually passed, not its basename.
func standaloneClaudeHookStarts(calls, data string) int {
	read := func(path string) map[string]any {
		raw, err := os.ReadFile(path)
		var settings map[string]any
		if err != nil || json.Unmarshal(raw, &settings) != nil {
			return nil
		}
		return settings
	}
	want := read(filepath.Join(data, "hooks.json"))
	hooks, ok := want["hooks"].(map[string]any)
	if !ok {
		return 0
	}
	// The baseline must itself carry the platform's lifecycle scripts.
	for hook, event := range map[string]string{"SessionStart": "session-start", "UserPromptSubmit": "prompt-submit", "Stop": "stop"} {
		entries, _ := hooks[hook].([]any)
		found := false
		for _, entry := range entries {
			e, _ := entry.(map[string]any)
			commands, _ := e["hooks"].([]any)
			for _, command := range commands {
				c, _ := command.(map[string]any)
				text, _ := c["command"].(string)
				if c["type"] == "command" && strings.Contains(text, filepath.Join(data, "status-hook.sh")) && strings.HasSuffix(text, " "+event) {
					found = true
				}
			}
		}
		if !found {
			return 0
		}
	}
	status, _ := want["statusLine"].(map[string]any)
	command, _ := status["command"].(string)
	if status["type"] != "command" || !strings.Contains(command, filepath.Join(data, "statusline.sh")) {
		return 0
	}
	// The stand-in logs argv joined with spaces; settings precede another flag.
	settingsArg := regexp.MustCompile(`(?:^|[ \t])--settings[ \t]+(.*?)(?:[ \t]+--[a-zA-Z-]+|$)`)
	count := 0
	for _, line := range strings.Split(calls, "\n") {
		match := settingsArg.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		rel, err := filepath.Rel(data, match[1])
		if err != nil || !filepath.IsLocal(rel) {
			continue
		}
		got := read(match[1])
		if reflect.DeepEqual(got["hooks"], want["hooks"]) && reflect.DeepEqual(got["statusLine"], want["statusLine"]) {
			count++
		}
	}
	return count
}

func TestStandaloneClaudeHookStartsChecksDerivedSettingsContents(t *testing.T) {
	data := filepath.Join(t.TempDir(), "data with spaces")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	hooks := map[string]any{}
	for hook, event := range map[string]string{"SessionStart": "session-start", "UserPromptSubmit": "prompt-submit", "Stop": "stop"} {
		hooks[hook] = []any{map[string]any{"matcher": "", "hooks": []any{map[string]any{
			"type": "command", "command": `sh "` + filepath.Join(data, "status-hook.sh") + `" ` + event, "timeout": 3,
		}}}}
	}
	settings := map[string]any{"hooks": hooks, "statusLine": map[string]any{
		"type": "command", "command": `sh "` + filepath.Join(data, "statusline.sh") + `"`,
	}}
	write := func(name string, settings map[string]any) string {
		t.Helper()
		raw, err := json.MarshalIndent(settings, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(data, name)
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	plain := write("hooks.json", settings)
	settings["permissions"] = map[string]any{"allow": []string{"mcp__personal"}}
	derived := write("hooks.allow-copy.json", settings)
	call := func(path string) string { return "--settings " + path + " --mcp-config servers.json -- task" }
	if got := standaloneClaudeHookStarts(call(plain)+"\n"+call(derived), data); got != 2 {
		t.Fatalf("plain and permission-extended hook settings: %d, want 2", got)
	}
	delete(settings, "hooks")
	missing := write("hooks.allow-missing.json", settings)
	if got := standaloneClaudeHookStarts(call(missing), data); got != 0 {
		t.Fatalf("settings without lifecycle hooks accepted: %d", got)
	}
	settings["hooks"] = hooks
	settings["statusLine"] = map[string]any{"type": "command", "command": "unrelated"}
	wrong := write("hooks.allow-wrong.json", settings)
	if got := standaloneClaudeHookStarts(call(wrong), data); got != 0 {
		t.Fatalf("settings with another status line accepted: %d", got)
	}
	settings["statusLine"] = map[string]any{"type": "command", "command": `sh "` + filepath.Join(data, "statusline.sh") + `"`}
	delete(hooks, "Stop")
	missingStop := write("hooks.allow-no-stop.json", settings)
	if got := standaloneClaudeHookStarts(call(missingStop), data); got != 0 {
		t.Fatalf("settings missing Stop accepted: %d", got)
	}
}
