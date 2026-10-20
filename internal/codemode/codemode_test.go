package codemode

import "testing"

func TestCalls(t *testing.T) {
	cases := []struct {
		name, js string
		names    []string
	}{
		{"none", `text(1+1)`, nil},
		{"in a string", `text("tools.a({})")`, nil},
		{"in a comment", "// tools.a({})\n/* tools.b({}) */ tools.c({})", []string{"c"}},
		{"nested object and array", `tools.a({x:[{y:"}"}], z:{w:1}}); tools.b()`, []string{"a", "b"}},
		{"member of another object", `mytools.a({}); x.tools.b({}); tools.c({})`, []string{"c"}},
		{"not a call", `tools.a; tools.b`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := Calls(tc.js)
			if len(calls) != len(tc.names) {
				t.Fatalf("got %d calls, want %v", len(calls), tc.names)
			}
			for i, c := range calls {
				if c.Name != tc.names[i] {
					t.Errorf("call %d: %q, want %q", i, c.Name, tc.names[i])
				}
			}
		})
	}
}

func TestDescribe(t *testing.T) {
	cases := []struct {
		name, js, tool, summary string
	}{
		{"concatenated string", `tools.exec_command({cmd:"echo " + "a"})`, "exec_command", "$ echo a"},
		{"template without substitutions", "tools.exec_command({cmd:`ls -l`})", "exec_command", "$ ls -l"},
		{"template with substitution", "tools.exec_command({cmd:`ls ${d}`})", "exec_command", "$ …"},
		{"unicode escape", `tools.exec_command({cmd:"echo \u00e9\x41"})`, "exec_command", "$ echo éA"},
		{"web open", `tools.web__run({open:[{ref_id:"https://example.com/a"}]})`, "web_search", "open https://example.com/a"},
		{"web two queries", `tools.web__run({search_query:[{q:"a"},{q:"b"}]})`, "web_search", "a · b"},
		{"write_stdin", `tools.write_stdin({session_id:3,chars:"q"})`, "write_stdin", "Write stdin"},
		{"comment first", "// look around\n\ntext(await listAll())", "exec", "code: text(await listAll())"},
		{"empty", ``, "exec", "code: script"},
		{"spread and shorthand", `tools.exec_command({...opts, a, cmd:"pwd"})`, "exec_command", "$ pwd"},
		{"unterminated", `tools.exec_command({cmd:"ls`, "exec_command", "$ ls"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := Describe(tc.js)
			if d.Tool != tc.tool || d.Summary != tc.summary {
				t.Errorf("got %q / %q, want %q / %q", d.Tool, d.Summary, tc.tool, tc.summary)
			}
		})
	}
}
