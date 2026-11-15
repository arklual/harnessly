package hlcli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A change of the agent's surface without a scenario exits 3 under --check; with a scenario it passes.
func TestEvalAffectedCheck(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	write := func(p, body string) {
		t.Helper()
		full := filepath.Join(repo, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	scenario := "id: card\ncovers: ['internal/delivery/**']\nprompt: скинь файлом\nexpect:\n  - called: Bash\n"
	write("evals/agent/surface.txt", "internal/delivery/**\n")
	write("evals/agent/card.yaml", scenario)
	write("internal/delivery/tools.go", "package delivery\n")
	git("init", "-q")
	git("add", ".")
	git("commit", "-qm", "base")
	run := func(args ...string) (int, string) {
		var out, errs bytes.Buffer
		code := Run(append([]string{"eval", "affected"}, args...), Env{Getenv: func(string) string { return "" }, Cwd: repo, Stdout: &out, Stderr: &errs})
		return code, out.String() + errs.String()
	}
	write("internal/delivery/tools.go", "package delivery\n// a new tool\n")
	git("commit", "-qam", "tool")
	if code, out := run("--check"); code != exitNeedsScenario || !strings.Contains(out, "card") {
		t.Fatalf("tool alone: %d %s", code, out)
	}
	if code, out := run("--names"); code != 0 || strings.TrimSpace(out) != "card" {
		t.Fatalf("names: %d %q", code, out)
	}
	write("evals/agent/card.yaml", scenario+"  - not_called: Write\n")
	git("commit", "-qam", "scenario")
	if code, out := run("--check", "--base", "HEAD~2"); code != 0 {
		t.Fatalf("with a scenario: %d %s", code, out)
	}
}
