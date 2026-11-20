package sessions

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/transcript"
)

// AntigravityLister lists verified native brain session directories. It never selects
// a session merely because it is newest, or treats an artifact as a transcript.
type AntigravityLister struct{ Root string }

func NewAntigravityLister(home string) *AntigravityLister {
	return &AntigravityLister{Root: filepath.Join(home, ".gemini", "antigravity-cli", "brain")}
}

// Find accepts only a UUID and the exact native transcript filename. Cwd remains
// unknown: the native JSONL records do not establish a working directory.
func (l *AntigravityLister) Find(id string) (Recent, bool) {
	if !agents.IsUUID(id) {
		return Recent{}, false
	}
	dir := filepath.Join(l.Root, id)
	// Do not follow session-directory or transcript symlinks outside the native root.
	logs := filepath.Join(dir, ".system_generated", "logs")
	for _, p := range []string{dir, filepath.Join(dir, ".system_generated"), logs} {
		st, err := os.Lstat(p)
		if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return Recent{}, false
		}
	}
	path := filepath.Join(logs, "transcript.jsonl")
	st, err := os.Lstat(path)
	// Both filenames are native. Prefer canonical whenever present; only fall
	// back to the official full transcript when canonical is absent, not unsafe.
	if os.IsNotExist(err) {
		path = filepath.Join(logs, "transcript_full.jsonl")
		st, err = os.Lstat(path)
	}
	if err != nil || !st.Mode().IsRegular() {
		return Recent{}, false
	}
	// The initial user message supplies a title; scanning bounded pages avoids
	// loading a long transcript's tool output into memory all at once.
	r := Recent{SessionID: id, Path: path, UpdatedAt: st.ModTime()}
	before := int64(-1)
	for {
		c := transcript.ReadAntigravityBefore(path, before, 256, l.Root)
		for _, e := range c.Entries {
			if e.String("type") == "user-message" {
				r.Turns++
				if r.Title == "" {
					r.Title = e.String("text")
				}
			}
		}
		// Older pages replace the title with their first real user message.
		for _, e := range c.Entries {
			if e.String("type") == "user-message" {
				r.Title = e.String("text")
				break
			}
		}
		if !c.HasMore || c.StartByteOffset <= 0 || before >= 0 && c.StartByteOffset >= before {
			break
		}
		before = c.StartByteOffset
	}
	if r.Turns == 0 {
		return Recent{}, false
	}
	return r, true
}

func (l *AntigravityLister) Recent(skip func(string) bool, limit int) []Recent {
	out := []Recent{}
	if limit <= 0 {
		return out
	}
	dirs, err := os.ReadDir(l.Root)
	if err != nil {
		return out
	}
	for _, d := range dirs {
		if !d.IsDir() || !agents.IsUUID(d.Name()) || skip != nil && skip(d.Name()) {
			continue
		}
		if r, ok := l.Find(d.Name()); ok {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].SessionID < out[j].SessionID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
