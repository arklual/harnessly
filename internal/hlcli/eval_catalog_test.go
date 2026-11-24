package hlcli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvalListOutsideCheckout(t *testing.T) {
	cwd := t.TempDir()
	var out, diagnostic bytes.Buffer
	env := Env{Cwd: cwd, Stdout: &out, Stderr: &diagnostic}
	if rc := Run([]string{"eval", "list"}, env); rc != 0 || !strings.Contains(out.String(), "open-merge-request") {
		t.Fatalf("default catalog: rc=%d: %s", rc, diagnostic.String())
	}
	for _, dir := range []string{"absent", "empty"} {
		if dir == "empty" {
			if err := os.Mkdir(filepath.Join(cwd, dir), 0700); err != nil {
				t.Fatal(err)
			}
		}
		diagnostic.Reset()
		if rc := Run([]string{"eval", "list", "--dir", dir}, env); rc == 0 || diagnostic.Len() == 0 {
			t.Fatalf("invalid explicit catalog %s: rc=%d", dir, rc)
		}
	}
}
