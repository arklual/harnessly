package chatrole

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// a planning chat may look but not change; HRN-1530: a coordinator does not edit or run suites.
func TestVerdict(t *testing.T) {
	cases := []struct {
		role    Role
		tool    string
		command string
		refused bool
	}{
		{Plan, "Read", "", false},
		{Plan, "Grep", "", false},
		{Plan, "Edit", "", true},
		{Plan, "Write", "", true},
		{Plan, "apply_patch", "", true},
		{Plan, "Bash", "git log --oneline -5 && grep -rn foo internal | head", false},
		{Plan, "Bash", "sed -n 1,40p a.go 2>/dev/null", false},
		{Plan, "Bash", "sed -i s/a/b/ a.go", true},
		{Plan, "Bash", "echo x > a.go", true},
		{Plan, "Bash", "git checkout -- a.go", true},
		{Plan, "Bash", "rm -rf build", true},
		{Plan, "Bash", "find . -name '*.tmp' -delete", true},
		{Plan, "exec_command", "cat go.mod", false},
		{Plan, "Bash", "sed -n '1,5w out.txt' a.go", true},
		{Plan, "Bash", "cat $(touch x) a", true},
		{Plan, "Bash", "awk '{print > \"f\"}' a", true},
		{Plan, "Bash", "env sh -c 'rm a'", true},
		{Plan, "Bash", "ls | xargs rm", true},
		{Plan, "Bash", "git branch new-one", true},
		{Plan, "Bash", "git branch -d old", true},
		{Plan, "Bash", "git branch -m a b", true},
		{Plan, "Bash", "git branch --set-upstream-to=origin/x", true},
		{Plan, "Bash", "git branch -a --contains HEAD", false},
		{Plan, "Bash", "git branch --list agent/x", false},
		{Plan, "Bash", "sort -u a.txt | head", false},
		{Plan, "Bash", "sort -o a.txt a.txt", true},
		{Plan, "Bash", "sort -uo a.txt a.txt", true},
		{Plan, "Bash", "sort --compress-program=sh a", true},
		{Plan, "Bash", "sort --outp=a a", true},
		{Plan, "Bash", "sort -t, -k2,2n a.csv", false},
		{Plan, "Bash", "sort \"-o\" a a", true},
		{Plan, "Bash", "sort '--output=a' a", true},
		{Plan, "Bash", "sort \\-o a a", true},
		{Plan, "Bash", "git branch '-d' old", true},
		{Plan, "mcp__harnessly__submit_plan", "", false},
		{Coordinate, "Edit", "", true},
		{Coordinate, "Bash", "go test ./internal/x", true},
		{Coordinate, "Bash", "cd web && pnpm vitest run a.test.ts", true},
		{Coordinate, "Bash", "git status && git log -3", false},
		{Coordinate, "Read", "", false},
		{None, "Edit", "", false},
	}
	for _, c := range cases {
		if got := Verdict(c.role, c.tool, c.command) != ""; got != c.refused {
			t.Errorf("%s %s %q: refused %v, want %v", c.role, c.tool, c.command, got, c.refused)
		}
	}
}

func TestMarkAndAnswer(t *testing.T) {
	dir := t.TempDir()
	if Of(dir, "hl-abc") != None {
		t.Fatal("no mark is no role")
	}
	if err := Mark(dir, "hl-abc", Plan); err != nil || Of(dir, "hl-abc") != Plan {
		t.Fatalf("mark: %v", err)
	}
	if Mark(dir, "../x", Plan) == nil {
		t.Fatal("a bad session name was taken")
	}
	out := Answer([]byte(`{"tool_name":"Edit","tool_input":{"file_path":"/a"}}`), Plan)
	if !strings.Contains(string(out), `"permissionDecision":"deny"`) || !strings.Contains(string(out), "submit_plan") {
		t.Fatalf("answer: %s", out)
	}
	if look := Answer([]byte(`{"tool_name":"Bash","tool_input":{"command":["bash","-lc","cat a.go"]}}`), Plan); look != nil {
		t.Fatalf("a look of Codex was refused: %s", look)
	}
	if err := Mark(dir, "hl-abc", None); err != nil || Of(dir, "hl-abc") != None {
		t.Fatalf("unmark: %v", err)
	}
}

// A hook that crashes refuses the call of a chat with a role and lets a chat without one through (fail-closed).
func TestGuardCommandFailsClosed(t *testing.T) {
	dir := t.TempDir()
	run := func(cmd, session string) string {
		t.Helper()
		c := exec.Command("sh", "-c", GuardCommand(cmd, dir)+" 2>/dev/null || true")
		c.Env = append(os.Environ(), "HARNESSLY_SESSION="+session)
		c.Stdin = strings.NewReader(`{"tool_name":"Edit"}`)
		out, err := c.Output()
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	if err := Mark(dir, "hl-plan", Plan); err != nil {
		t.Fatal(err)
	}
	if out := run("exit 3", "hl-plan"); !strings.Contains(out, `"permissionDecision":"deny"`) || !strings.Contains(out, "could not run") {
		t.Fatalf("crash with a role: %q", out)
	}
	if out := run("/no/such/harnessly hook voice-guard", "hl-plan"); !strings.Contains(out, "deny") {
		t.Fatalf("missing program with a role: %q", out)
	}
	if out := run("exit 3", "hl-free"); out != "" {
		t.Fatalf("crash without a role: %q", out)
	}
	if out := run("cat", "hl-plan"); out != `{"tool_name":"Edit"}` {
		t.Fatalf("a working hook's answer and input: %q", out)
	}
}
