package files

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Both sides of a changed file for the diff view: one side from a git revision, the other from the working tree.

const (
	diffMaxBytes = 1_000_000
	// gitShowCap is how much output git may print before the read is abandoned as failed, not as truncated.
	gitShowCap  = diffMaxBytes + 1024
	gitTimeout  = 10 * time.Second
	worktreeRef = "WORKTREE"
	// readFileLimit is the largest file a whole-file read takes; a bigger one fails.
	readFileLimit = 1<<31 - 1
)

var refPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_\-/.^~]{0,200}$`)

// Side is one side of the diff.
type Side struct {
	Content   string
	Truncated bool
	Binary    bool
}

// RepoPath is a path inside the repository, or ok false: absent, repeated, /dev/null, absolute, with a NUL, or
// climbing above the repository.
func RepoPath(values []string) (string, bool) {
	if len(values) != 1 || values[0] == "" {
		return "", false
	}
	p := values[0]
	if p == "/dev/null" || strings.Contains(p, "\x00") || strings.HasPrefix(p, "/") {
		return "", false
	}
	depth := 0
	for _, part := range strings.Split(p, "/") {
		switch part {
		case "", ".":
		case "..":
			if depth--; depth < 0 {
				return "", false
			}
		default:
			depth++
		}
	}
	return p, true
}

// Ref is a revision to read a side from: fallback when absent, WORKTREE for the working tree, ok false when it does
// not look like a revision.
func Ref(values []string, fallback string) (string, bool) {
	v := ""
	if len(values) > 0 {
		v = values[0]
	}
	if v == "" {
		return fallback, true
	}
	if v == worktreeRef {
		return v, true
	}
	if !refPattern.MatchString(v) {
		return "", false
	}
	return v, true
}

func sideOf(b []byte) Side {
	if len(b) > diffMaxBytes {
		return Side{Truncated: true}
	}
	if bytes.IndexByte(b, 0) >= 0 {
		return Side{Binary: true}
	}
	return Side{Content: DecodeUTF8(b)}
}

// limitWriter keeps up to cap bytes and notes that more came.
type limitWriter struct {
	buf  bytes.Buffer
	cap  int
	over bool
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > w.cap {
		w.over = true
		return 0, io.ErrShortWrite
	}
	return w.buf.Write(p)
}

func fromRef(cwd, ref, rel string) Side {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", cwd, "show", ref+":"+rel)
	out := &limitWriter{cap: gitShowCap}
	errs := &limitWriter{cap: gitShowCap}
	cmd.Stdout, cmd.Stderr = out, errs
	if err := cmd.Run(); err != nil || out.over || errs.over {
		return Side{}
	}
	return sideOf(out.buf.Bytes())
}

func fromWorktree(cwd, rel string) Side {
	resolved := filepath.Join(cwd, rel)
	if resolved != cwd && !strings.HasPrefix(resolved, cwd+"/") {
		return Side{}
	}
	f, err := os.Open(resolved)
	if err != nil {
		return Side{}
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		return Side{}
	}
	if st.Mode().IsRegular() {
		if st.Size() > readFileLimit {
			return Side{}
		}
		if st.Size() > diffMaxBytes {
			return Side{Truncated: true}
		}
	}
	// A device or a pipe has no size: read until it ends or is past the cap.
	b, err := io.ReadAll(io.LimitReader(f, diffMaxBytes+1))
	if err != nil {
		return Side{}
	}
	return sideOf(b)
}

// LoadSide reads one side; an empty relative path is an empty side.
func LoadSide(cwd, ref, rel string, has bool) Side {
	if !has {
		return Side{}
	}
	if ref == worktreeRef {
		return fromWorktree(cwd, rel)
	}
	return fromRef(cwd, ref, rel)
}

// FileContent is the answer of both sides.
func FileContent(oldSide, newSide Side) Object {
	truncated := oldSide.Truncated || newSide.Truncated
	binary := oldSide.Binary || newSide.Binary
	oldText, newText := oldSide.Content, newSide.Content
	if truncated || binary {
		oldText, newText = "", ""
	}
	return Object{{"oldContent", oldText}, {"newContent", newText}, {"truncated", truncated}, {"binary", binary}}
}
