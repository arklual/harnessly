package transcript

import (
	"encoding/json"
	"strings"
	"testing"
)

func codexExecLines(t *testing.T, input string) string {
	t.Helper()
	call, err := json.Marshal(map[string]any{"type": "response_item", "timestamp": "2026-01-01T00:00:00Z",
		"payload": map[string]any{"type": "custom_tool_call", "name": "exec", "call_id": "call_1", "status": "completed", "input": input}})
	if err != nil {
		t.Fatal(err)
	}
	out := `{"type":"response_item","timestamp":"2026-01-01T00:00:01Z","payload":{"type":"custom_tool_call_output","call_id":"call_1","output":"done"}}`
	return string(call) + "\n" + out + "\n"
}

// Code mode runs every tool through one "exec" call; the entry says what the script runs.
func TestCodexCodeModeExec(t *testing.T) {
	cases := []struct {
		name, input, tool, summary string
	}{
		{
			"two commands",
			"text(await tools.exec_command({cmd:\"cat /home/agent/.codex/skills/.system/openai-docs/SKILL.md\",\"max_output_tokens\":3500}));\ntext(await tools.exec_command({cmd:\"cat /x/SKILL.md\",\"max_output_tokens\":2500}));\n",
			"exec_command", "$ cat /home/agent/.codex/skills/.system/openai-docs/SKILL.md · cat /x/SKILL.md",
		},
		{
			"web search",
			"text(await tools.web__run({search_query:[{q:\"site.help.openai.com canvas ChatGPT\"}],response_length:\"short\"}));\n",
			"web_search", "site.help.openai.com canvas ChatGPT",
		},
		{
			"commands in Promise.allSettled",
			"const r = await Promise.allSettled([tools.exec_command({cmd:\"cat /a/SKILL.md\",max_output_tokens:8000}),tools.exec_command({cmd:\"cat /b/SKILL.md\",max_output_tokens:8000})]);for(let i=0;i<r.length;i++)text({i,result:r[i]});\n",
			"exec_command", "$ cat /a/SKILL.md · cat /b/SKILL.md",
		},
		{
			"no tool call",
			"text(ALL_TOOLS.filter(x=>/file|directory|terminal|exec|skill|artifact|board/i.test(x.name+\" \"+x.description)));\n",
			"exec", "code: text(ALL_TOOLS.filter(x=>/file|directory|terminal|exec|skill|artifact|board/i.test(x.name+\" \"+x.des…",
		},
		{
			"quoted keys",
			`text(await tools.exec_command({"cmd": "ls -la", "workdir": "/tmp"}));`,
			"exec_command", "$ ls -la",
		},
		{
			"escaped quotes and newlines",
			`text(await tools.exec_command({cmd:"echo \"hi there\"\nls",max_output_tokens:100}));`,
			"exec_command", `$ echo "hi there"`,
		},
		{
			"single quotes and a tool name in a string",
			`text("tools.exec_command({cmd:'no'})"); text(await tools.exec_command({cmd:'git status'}));`,
			"exec_command", "$ git status",
		},
		{
			"different tools",
			`text(await tools.exec_command({cmd:"ls"})); text(await tools.web__run({search_query:[{q:"go sync"}]}));`,
			"exec", "$ ls · go sync",
		},
		{
			"mcp tool",
			`text(await tools.docs__search_docs({query:"kafka topics",limit:5}));`,
			"mcp__docs__search_docs", "kafka topics",
		},
		{
			"image",
			`text(await tools.view_image({path:"/tmp/shot.png"}));`,
			"view_image", "/tmp/shot.png",
		},
		{
			"patch",
			"await tools.apply_patch(\"*** Begin Patch\\n*** Update File: a.go\\n@@\\n-x\\n+y\\n*** Add File: b.go\\n+z\\n*** End Patch\\n\");",
			"apply_patch", "a.go +1 more",
		},
		{
			"command from a variable",
			`const c = "ls"; text(await tools.exec_command({cmd: c}));`,
			"exec_command", "$ …",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entries := ParseCodexContent(codexExecLines(t, tc.input), CodexOptions{})
			if len(entries) != 2 {
				t.Fatalf("want a call and a result, got %d entries", len(entries))
			}
			call, result := entries[0], entries[1]
			if call.String("type") != "tool-call" || call.String("toolUseId") != "call_1" || call.String("status") != "success" {
				t.Errorf("call: %s", stringify(call))
			}
			if got := call.String("toolName"); got != tc.tool {
				t.Errorf("toolName: got %q, want %q", got, tc.tool)
			}
			if got := call.String("summary"); got != tc.summary {
				t.Errorf("summary: got %q, want %q", got, tc.summary)
			}
			if result.String("type") != "tool-result" || result.String("toolUseId") != "call_1" {
				t.Errorf("result: %s", stringify(result))
			}
		})
	}
}

func TestCodexCodeModeLongScriptIsCut(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 20; i++ {
		b.WriteString(`text(await tools.exec_command({cmd:"cat /some/long/path/to/file-number-` + strings.Repeat("x", 10) + `.md"}));`)
	}
	entries := ParseCodexContent(codexExecLines(t, b.String()), CodexOptions{})
	got := entries[0].String("summary")
	if len([]rune(got)) > 200 || !strings.Contains(got, " more") {
		t.Errorf("summary is not cut: %q", got)
	}
}
