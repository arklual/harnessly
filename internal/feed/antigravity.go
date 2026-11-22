package feed

import (
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/transcript"
	"github.com/ly-solutions/harnessly/internal/transcriptindex"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// AntigravitySource reads the native brain/<uuid>/.system_generated/logs/transcript.jsonl.
// DONE belongs to one native step, never to the conversation's turn.
type AntigravitySource struct {
	Root   string
	Path   string
	Index  *transcriptindex.Index
	offset int64
}

func (s *AntigravitySource) Tail(max int) transcript.Chunk {
	c := transcript.ReadAntigravityTail(s.Path, max, s.roots()...)
	s.Seat(c.FileSize)
	return c
}
func (s *AntigravitySource) Before(before int64, max int) transcript.Chunk {
	return transcript.ReadAntigravityBefore(s.Path, before, max, s.roots()...)
}
func (s *AntigravitySource) AtRest() bool { return true }
func (s *AntigravitySource) roots() []string {
	if s.Root != "" {
		return []string{s.Root}
	}
	return nil
}
func (s *AntigravitySource) Seat(offset int64) { s.offset = offset }
func (s *AntigravitySource) Close()            {}
func (s *AntigravitySource) Since() transcript.Incremental {
	f, err := transcript.OpenAntigravity(s.Path, s.roots()...)
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
