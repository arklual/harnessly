package cliupdate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheListingSkipsOptWithoutACopyOnPath(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	cmd := exec.Command(sh, "-c", listScript("harnessly-no-such-cli"))
	cmd.Env = []string{"PATH=" + dir}
	out, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) != "" {
		t.Fatalf("out %q err %v", out, err)
	}
	bin := filepath.Join(dir, "harnessly-fake-cli")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'fake 1.2.3'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(sh, "-c", listScript("harnessly-fake-cli"))
	cmd.Env = []string{"PATH=" + dir + ":/usr/bin:/bin"}
	out, err = cmd.Output()
	copies := ParseCopies(string(out))
	if err != nil || len(copies) != 1 || copies[0].Path != bin || copies[0].Version != "1.2.3" {
		t.Fatalf("copies %+v out %q err %v", copies, out, err)
	}
}
