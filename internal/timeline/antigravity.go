package timeline

import (
	"io"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/transcript"
)

// nativeAntigravityPath accepts only a native UUID transcript under the configured
// brain root, never arbitrary JSONL or a sibling whose name shares the root prefix.
func nativeAntigravityPath(root, path string) bool {
	if root == "" {
		return false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil {
		return false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 4 || !agents.IsUUID(parts[0]) || parts[1] != ".system_generated" || parts[2] != "logs" || (parts[3] != "transcript.jsonl" && parts[3] != "transcript_full.jsonl") {
		return false
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return false
	}
	resolvedRoot, err := filepath.EvalSymlinks(base)
	if err != nil {
		return false
	}
	rel, err = filepath.Rel(resolvedRoot, resolved)
	return err == nil && rel == filepath.Join(parts...)
}

func (s *Server) isAntigravityPath(path string) bool {
	return nativeAntigravityPath(s.d.AntigravityRoot, path)
}

func antigravityIncremental(root, path string, offset int64) transcript.Incremental {
	f, err := transcript.OpenAntigravity(path, root)
	if err != nil {
		return transcript.Incremental{NewOffset: offset}
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return transcript.Incremental{NewOffset: offset}
	}
	if st.Size() < offset {
		offset = 0
	}
	if offset < 0 {
		offset = 0
	}
	raw := make([]byte, st.Size()-offset)
	n, err := f.ReadAt(raw, offset)
	raw = raw[:n]
	if err == io.EOF {
		err = nil
	}
	if err != nil {
		return transcript.Incremental{NewOffset: offset}
	}
	return transcript.ParseAntigravityAppended(raw, offset)
}
