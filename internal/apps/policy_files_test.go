package apps

import (
	"os"
	"path/filepath"
	"testing"
)

// File rules (HRN-799): an Edit rule names every tool that writes a file; a relative mask and a relative path are taken
// from the chat's folder; a mask with ** reads as file masks do, one without keeps the old, wide reading.
func TestFileRules(t *testing.T) {
	home, _ := os.UserHomeDir()
	app := func(deny ...string) []*Manifest {
		return []*Manifest{{ID: "a.b", Agent: &Agent{Tools: &ToolPolicy{Deny: deny}}}}
	}
	call := func(tool, path, cwd string) Call {
		key := "file_path"
		if tool == "NotebookEdit" {
			key = "notebook_path"
		}
		return Call{Tool: tool, Input: map[string]any{key: path}, Cwd: cwd}
	}
	for _, c := range []struct {
		rule string
		call Call
		deny bool
	}{
		{"Edit(/etc/*)", call("Write", "/etc/hosts", "/w"), true},
		{"Edit(/etc/*)", call("MultiEdit", "/etc/hosts", "/w"), true},
		{"Edit(/etc/*)", call("NotebookEdit", "/etc/x.ipynb", "/w"), true},
		{"Edit", call("Write", "/w/a.txt", "/w"), true},
		{"Edit(/etc/*)", call("Read", "/etc/hosts", "/w"), false},
		{"Write(/etc/*)", call("Edit", "/etc/hosts", "/w"), false},
		// Relative masks and paths are the chat's folder.
		{"Edit(docs/*)", call("Edit", "/w/docs/a.md", "/w"), true},
		{"Edit(./docs/*)", call("Edit", "docs/a.md", "/w"), true},
		{"Edit(docs/*)", call("Edit", "/other/docs/a.md", "/w"), false},
		{"Edit(/w/secret/*)", call("Edit", "secret/../secret/k", "/w"), true},
		{"Edit(/w/secret/*)", call("Edit", "/w/secret/../open/k", "/w"), false},
		{"Edit(//etc/*)", call("Edit", "/etc/hosts", "/w"), true},
		{"Edit(~/.ssh/*)", call("Edit", filepath.Join(home, ".ssh", "config"), "/w"), true},
		// ** as in file masks: * stays in one folder, ** crosses them.
		{"Edit(/w/src/**/*.go)", call("Edit", "/w/src/a/b/c.go", "/w"), true},
		{"Edit(/w/src/**/*.go)", call("Edit", "/w/src/c.go", "/w"), true},
		{"Edit(/w/src/**/*.go)", call("Edit", "/w/src/a/c.ts", "/w"), false},
		{"Edit(/w/**/gen/*)", call("Edit", "/w/x/gen/y/z", "/w"), false},
		{"Edit(/w/**)", call("Edit", "/w/x/y", "/w"), true},
		// Without ** the old reading: * across folders, so the denies of earlier apps hold as wide as they did.
		{"Edit(*/standup/*)", call("Write", "/w/a/standup/b/c.md", "/w"), true},
		{"Edit(/etc/*)", call("Edit", "/etc/ssl/certs/x", "/w"), true},
		// Non-ASCII masks match by letters, not bytes.
		{"Edit(/w/секреты/**)", call("Edit", "/w/секреты/ключ.txt", "/w"), true},
		{"Edit(/w/**/отчёт-*.md)", call("Write", "/w/a/отчёт-1.md", "/w"), true},
		{"Edit(/w/**/отчёт-*.md)", call("Write", "/w/a/отчет-1.md", "/w"), false},
		// A relative mask stays on the folder the chat started in: a cd of the agent does not move it.
		{"Edit(./secret/**)", Call{Tool: "Edit", Input: map[string]any{"file_path": "/w/secret/k"}, Cwd: "/w/src", Root: "/w"}, true},
		{"Edit(./secret/**)", Call{Tool: "Edit", Input: map[string]any{"file_path": "../secret/k"}, Cwd: "/w/src", Root: "/w"}, true},
		{"Edit(./secret/**)", Call{Tool: "Edit", Input: map[string]any{"file_path": "/w/src/secret/k"}, Cwd: "/w/src", Root: "/w"}, false},
	} {
		got := Decide(app(c.rule), c.call)
		if (got.Decision == DecideDeny) != c.deny {
			t.Errorf("%s vs %s %v: %s", c.rule, c.call.Tool, c.call.Input, got.Decision)
		}
	}
}
