//go:build linux

package agenteval

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The test binary sandboxes itself when asked: what is outside the rules cannot be read or written.
func TestMain(m *testing.M) {
	if spec := os.Getenv("AGENTEVAL_SANDBOX_CHILD"); spec != "" {
		parts := strings.Split(spec, "|")
		if err := SandboxExec([]string{parts[0]}, systemRO, []string{"/bin/sh", "-c", parts[1]}); err != nil {
			os.Stderr.WriteString(err.Error())
			os.Exit(3)
		}
	}
	os.Exit(m.Run())
}

func TestSandboxKeepsAStandInItsFolder(t *testing.T) {
	if SandboxAvailable() == 0 {
		t.Skip("no Landlock")
	}
	own, other := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "secret"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(script string) error {
		cmd := exec.Command(os.Args[0], "-test.run=^$")
		cmd.Env = append(os.Environ(), "AGENTEVAL_SANDBOX_CHILD="+own+"|"+script)
		return cmd.Run()
	}
	if err := run("echo hi > " + own + "/f && cat " + own + "/f && cat /etc/hostname"); err != nil {
		t.Fatalf("the stand's own folder and the system: %v", err)
	}
	if err := run("cat " + other + "/secret"); err == nil {
		t.Fatal("read a file outside the stand")
	}
	if err := run("touch " + other + "/new"); err == nil {
		t.Fatal("wrote outside the stand")
	}
}
