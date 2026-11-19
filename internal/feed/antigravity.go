package feed

import (
	"os"
	"runtime"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/transcript"
	"github.com/ly-solutions/harnessly/internal/transcriptindex"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// AntigravitySource reads the native brain/<uuid>/.system_generated/logs/transcript.jsonl.
// DONE belongs to one native step, never to the conversation's turn.
type AntigravitySource struct {
	Path   string
	Index  *transcriptindex.Index
	offset int64
	q      *transcriptindex.Queue
}

func (s *AntigravitySource) Tail(max int) transcript.Chunk {
	c := transcript.ReadAntigravityTail(s.Path, max)
	s.Seat(c.FileSize)
	return c
}
func (s *AntigravitySource) Before(before int64, max int) transcript.Chunk {
	return transcript.ReadAntigravityBefore(s.Path, before, max)
}
func (s *AntigravitySource) AtRest() bool { return true }
func (s *AntigravitySource) Seat(offset int64) {
	s.offset = offset
	if s.Index == nil {
		return
	}
	if s.q == nil {
		s.q = s.Index.Queue(s.Path, "feed", 0)
		runtime.AddCleanup(s, func(q *transcriptindex.Queue) { q.Close() }, s.q)
	}
	s.q.Seat(offset)
}
func (s *AntigravitySource) Close() {
	if s.q != nil {
		s.q.Close()
		s.q = nil
	}
}
func (s *AntigravitySource) Since() transcript.Incremental {
	if s.q != nil {
		data, from, partial, state := s.q.Take()
		if state == transcriptindex.Fresh && (len(data) == 0 || from == s.offset) && !partial {
			inc := transcript.ParseAntigravityAppended(data, s.offset)
			s.Seat(inc.NewOffset)
			return inc
		}
	}
	f, err := os.Open(s.Path)
	if err != nil {
		return transcript.Incremental{NewEntries: []*jsvalue.Object{}, NewOffset: s.offset}
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return transcript.Incremental{NewOffset: s.offset}
	}
	if st.Size() < s.offset {
		s.offset = 0
	}
	// Read only the newly appended bytes; the final partial line is retried from its start.
	data := make([]byte, st.Size()-s.offset)
	n, _ := f.ReadAt(data, s.offset)
	inc := transcript.ParseAntigravityAppended(data[:n], s.offset)
	s.Seat(inc.NewOffset)
	return inc
}

// antigravityResult is deliberately not builder.result: native GENERIC records do not
// identify their calls or prove successful completion. Images have explicit media evidence.
func (b *builder) antigravityResult(s *step, o *jsvalue.Object, id string, at wire.Ms, offset int64) {
	e := wire.Entry{ID: id, At: at, Rev: 1, T: wire.EntryTool, ToolUseID: id, Tool: "antigravity-result", Summary: firstLine(str(o, "stdout")), Output: b.output(id, str(o, "stdout"))}
	if v, ok := o.Get("imagePaths"); ok {
		if list, ok := v.([]any); ok {
			paths := jsvalue.Strings(list)
			if len(paths) > 0 {
				e.Images = &wire.ImageGen{Count: len(paths), State: "done", Done: paths}
			}
		}
	}
	b.push(s, e, offset)
}
