package mount

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBwrapShowsFolderAtMissingPath(t *testing.T) {
	bw, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("no bwrap")
	}
	src := t.TempDir()
	_ = os.WriteFile(filepath.Join(src, "f"), []byte("x"), 0o600)
	home, _ := os.UserHomeDir()
	for _, dst := range []string{"/Users/masha/proj", "/c/Users/m/proj", filepath.Join(home, "no-such-dir-connect", "proj")} {
		args := append(BwrapArgs(src, dst), "--chdir", dst, "--", "sh", "-c", "pwd; cat f; test -d "+home+" && echo home-ok")
		out, err := exec.Command(bw, args...).CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != dst+"\nx\nhome-ok" && strings.TrimSpace(string(out)) != dst+"\nxhome-ok" {
			t.Fatalf("%s: %v %q", dst, err, out)
		}
	}
}
