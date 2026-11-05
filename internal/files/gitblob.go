package files

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitBlob is a file as a git revision has it, for the editor's diff: read from the file's own folder by a
// path relative to it ("./name"), so any file of any repository works without knowing where the repository starts.
// inRepo is false outside a repository; tracked is false for a file the revision does not have.
func GitBlob(abs, ref string) (side Side, inRepo, tracked bool) {
	dir, name := filepath.Dir(abs), filepath.Base(abs)
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	probe := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--is-inside-work-tree")
	if out, err := probe.Output(); err != nil || strings.TrimSpace(string(out)) != "true" {
		return Side{}, false, false
	}
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "show", ref+":./"+name)
	out := &limitWriter{cap: gitShowCap}
	var errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = out, &errs
	err := cmd.Run()
	if out.over {
		return Side{Truncated: true}, true, true
	}
	if err != nil {
		return Side{}, true, false
	}
	return sideOf(out.buf.Bytes()), true, true
}
