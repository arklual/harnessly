package transcript

import (
	"io"
	"os"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// The Codex transcript (session-parser-codex and codex-lines in the previous runtime). Offsets are counted the way that
// runtime counted them: in the bytes of the decoded text, so an invalid byte read as U+FFFD counts three.

const codexChunkSize = 256_000

// CodexOptions are what a Codex parse needs from the deployment.
type CodexOptions struct {
	// ImageURL is the page's address of an image the person attached (an upload path), "" when it is none.
	ImageURL func(path string) string
}

// CodexParser follows one Codex transcript; it keeps the state of calls still waiting for their output.
type CodexParser struct {
	path       string
	opts       CodexOptions
	lastOffset int64
	pending    string
	state      *codexState
}

func NewCodexParser(path string, opts CodexOptions) *CodexParser {
	return &CodexParser{path: path, opts: opts, state: newCodexState(opts.ImageURL)}
}

func (p *CodexParser) Path() string { return p.path }

// Offset is where the next incremental read starts.
func (p *CodexParser) Offset() int64 { return p.lastOffset }

func (p *CodexParser) Reset() {
	p.lastOffset = 0
	p.pending = ""
	p.state = newCodexState(p.opts.ImageURL)
}

func (p *CodexParser) Dispose() {
	p.state.clearInFlight()
	p.pending = ""
}

// FlushStale ends the calls still in flight as failed at ts (milliseconds), each once.
func (p *CodexParser) FlushStale(ts float64) []*jsvalue.Object {
	return p.state.flushStale(ts)
}

// codexParse is IParseResult.
type codexParse struct {
	codexLinesResult
	lastOffset int64
}

func splitCodexLines(content string, start int64) ([]string, []int64) {
	lines := strings.Split(content, "\n")
	offsets := make([]int64, len(lines))
	off := start
	for i, l := range lines {
		offsets[i] = off
		off += int64(len(l)) + 1
	}
	return lines, offsets
}

func parseCodexWithOffsets(content string, start int64, s *codexState) codexParse {
	lines, offsets := splitCodexLines(content, start)
	return codexParse{codexLinesResult: parseCodexLines(lines, s, offsets), lastOffset: start + int64(len(content))}
}

// readCodexBytes reads [from, to) as Buffer.alloc + read: bytes past the end of the file stay zero.
func readCodexBytes(path string, from, to int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, to-from)
	if _, err := f.ReadAt(buf, from); err != nil && err != io.EOF {
		return nil, err
	}
	return buf, nil
}

// readCodexChunk is readChunk: past a start inside the file, the text begins after the first newline.
func readCodexChunk(path string, from, to int64) (string, int64, error) {
	if to-from <= 0 {
		return "", from, nil
	}
	b, err := readCodexBytes(path, from, to)
	if err != nil {
		return "", 0, err
	}
	raw := decodeUTF8(b)
	if from == 0 {
		return raw, 0, nil
	}
	nl := strings.IndexByte(raw, '\n')
	if nl < 0 {
		return "", to, nil
	}
	return raw[nl+1:], from + int64(nl+1), nil
}

func readCodexExact(path string, from, to int64) (string, error) {
	if to-from <= 0 {
		return "", nil
	}
	b, err := readCodexBytes(path, from, to)
	if err != nil {
		return "", err
	}
	return decodeUTF8(b), nil
}

// jsSliceFrom is the start index of array.slice(-n).
func jsSliceFrom(length, n int) int {
	start := -n
	if start < 0 {
		return max(length+start, 0)
	}
	return min(start, length)
}

func sliceCodexChunk(r codexParse, fileSize, fallback int64, maxEntries int) Chunk {
	n := len(r.entries)
	sliced := n > maxEntries
	entries := r.entries
	start := fallback
	if sliced {
		entries = r.entries[jsSliceFrom(n, maxEntries):]
		if i := n - maxEntries; i >= 0 && i < len(r.entryOffsets) {
			start = r.entryOffsets[i]
		}
	}
	return Chunk{
		Entries:         entries,
		StartByteOffset: start,
		FileSize:        fileSize,
		HasMore:         start > 0,
		ErrorCount:      r.errorCount,
		Summary:         r.summary,
	}
}

func emptyCodexChunk() Chunk { return Chunk{Entries: []*jsvalue.Object{}} }

// recoverCodex turns a throw of the previous runtime into the answer its catch gave.
func recoverCodex(on func()) {
	if r := recover(); r != nil {
		if _, ok := r.(codexThrow); !ok {
			panic(r)
		}
		on()
	}
}

func fileSize(path string) (int64, error) {
	st, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

// readCodexRange parses [fromByte, toByte) and keeps the entries whose line starts at displayFrom or later.
func readCodexRange(path string, fromByte, toByte, displayFrom int64, opts CodexOptions) (out Chunk) {
	defer recoverCodex(func() { out = emptyCodexChunk() })
	size, err := fileSize(path)
	if err != nil {
		return emptyCodexChunk()
	}
	from := max(0, min(fromByte, size))
	to := max(from, min(toByte, size))
	if to <= from {
		c := emptyCodexChunk()
		c.FileSize = size
		return c
	}
	content, err := readCodexExact(path, from, to)
	if err != nil {
		return emptyCodexChunk()
	}
	r := parseCodexWithOffsets(content, from, newCodexState(opts.ImageURL))
	entries := []*jsvalue.Object{}
	for i, e := range r.entries {
		off := from
		if i < len(r.entryOffsets) {
			off = r.entryOffsets[i]
		}
		if off >= displayFrom {
			entries = append(entries, e)
		}
	}
	start := max(from, displayFrom)
	return Chunk{Entries: entries, StartByteOffset: start, FileSize: size, HasMore: start > 0, ErrorCount: r.errorCount,
		Summary: r.summary}
}

// ParseAll reads the whole file afresh (parseAll); the entries and the offset the next incremental read starts at.
func (p *CodexParser) ParseAll() (entries []*jsvalue.Object, offset int64) {
	defer recoverCodex(func() { entries, offset = []*jsvalue.Object{}, p.lastOffset })
	return p.parseAll()
}

func (p *CodexParser) parseAll() ([]*jsvalue.Object, int64) {
	p.lastOffset = 0
	p.pending = ""
	p.state = newCodexState(p.opts.ImageURL)
	st, err := os.Stat(p.path)
	if err != nil || st.Size() == 0 {
		return []*jsvalue.Object{}, 0
	}
	b, err := os.ReadFile(p.path)
	if err != nil {
		panic(codexThrow{})
	}
	content := decodeUTF8(b)
	r := parseCodexWithOffsets(content, 0, p.state)
	p.lastOffset = int64(len(content))
	return r.entries, p.lastOffset
}

// ParseTail resets the parser to the tail of the file (parseTail): the last max entries, read in chunks that double
// until they hold enough.
func (p *CodexParser) ParseTail(maxEntries int) (out Chunk) {
	p.Reset()
	defer recoverCodex(func() { out = emptyCodexChunk() })
	size, err := fileSize(p.path)
	if err != nil || size == 0 {
		return emptyCodexChunk()
	}
	chunk := int64(codexChunkSize)
	for {
		from := size - chunk
		if from < 0 {
			from = 0
		}
		content, validFrom, err := readCodexChunk(p.path, from, size)
		if err != nil {
			return emptyCodexChunk()
		}
		if content == "" {
			if from == 0 {
				return emptyCodexChunk()
			}
			chunk *= 2
			continue
		}
		p.state = newCodexState(p.opts.ImageURL)
		r := parseCodexWithOffsets(content, validFrom, p.state)
		if len(r.entries) >= maxEntries || from == 0 {
			p.lastOffset = size
			p.pending = ""
			fallback := validFrom
			if from == 0 {
				fallback = 0
			}
			return sliceCodexChunk(r, size, fallback, maxEntries)
		}
		chunk *= 2
	}
}

// ParseIncremental is what the file gained since the last read (parseIncremental). A last line still being written
// waits in the pending buffer, unless it already parses as JSON.
func (p *CodexParser) ParseIncremental() (out Incremental) {
	f, err := os.Open(p.path)
	if err != nil {
		return Incremental{NewEntries: []*jsvalue.Object{}, NewOffset: p.lastOffset, PendingBuffer: p.pending}
	}
	defer f.Close()
	defer recoverCodex(func() {
		out = Incremental{NewEntries: []*jsvalue.Object{}, NewOffset: p.lastOffset, PendingBuffer: p.pending}
	})
	fail := Incremental{NewEntries: []*jsvalue.Object{}, NewOffset: p.lastOffset, PendingBuffer: p.pending}
	st, err := f.Stat()
	if err != nil {
		return fail
	}
	size := st.Size()
	if size < p.lastOffset {
		p.Reset()
		entries, off := p.parseAll()
		return Incremental{NewEntries: entries, NewOffset: off, PendingBuffer: ""}
	}
	if p.lastOffset >= size {
		return fail
	}
	buf := make([]byte, size-p.lastOffset)
	if _, err := f.ReadAt(buf, p.lastOffset); err != nil && err != io.EOF {
		return fail
	}
	raw := p.pending + decodeUTF8(buf)
	segments := strings.Split(raw, "\n")
	newPending := ""
	if !strings.HasSuffix(raw, "\n") {
		last := segments[len(segments)-1]
		segments = segments[:len(segments)-1]
		if last != "" {
			if _, ok := parseJSON(last); ok {
				segments = append(segments, last)
			} else {
				newPending = last
			}
		}
	}
	r := parseCodexLines(segments, p.state, nil)
	p.lastOffset = size
	p.pending = newPending
	return Incremental{NewEntries: r.entries, NewOffset: size, PendingBuffer: newPending}
}

// ParseCodexContent is the entries of Codex transcript text (parseCodexContent); nil where the previous runtime threw.
func ParseCodexContent(content string, opts CodexOptions) (out []*jsvalue.Object) {
	defer recoverCodex(func() { out = nil })
	return parseCodexLines(strings.Split(content, "\n"), newCodexState(opts.ImageURL), nil).entries
}

// ParseCodexContentChecked is ParseCodexContent that tells a throw of the previous runtime's parser from an empty
// result.
func ParseCodexContentChecked(content string, opts CodexOptions) (entries []*jsvalue.Object, threw bool) {
	defer recoverCodex(func() { entries, threw = nil, true })
	return parseCodexLines(strings.Split(content, "\n"), newCodexState(opts.ImageURL), nil).entries, false
}

// ReadCodexTail is readTailCodexEntries.
func ReadCodexTail(path string, maxEntries int, opts CodexOptions) Chunk {
	return NewCodexParser(path, opts).ParseTail(maxEntries)
}

// ReadCodexBefore is readCodexEntriesBefore: the page of up to max entries before beforeByte, read on to until (< 0 for
// the end of the file) so the calls of the page meet their ends.
func ReadCodexBefore(path string, beforeByte int64, maxEntries int, until int64, opts CodexOptions) (out Chunk) {
	defer recoverCodex(func() { out = emptyCodexChunk() })
	if beforeByte <= 0 {
		return emptyCodexChunk()
	}
	size, err := fileSize(path)
	if err != nil {
		return emptyCodexChunk()
	}
	current := min(beforeByte, size)
	if current < 0 {
		current = 0
	}
	end := size
	if until >= 0 {
		end = min(until, size)
	}
	end = max(current, end)
	chunk := int64(codexChunkSize)
	for {
		from := current - chunk
		if from < 0 {
			from = 0
		}
		content, validFrom, err := readCodexChunk(path, from, current)
		if err != nil {
			return emptyCodexChunk()
		}
		if content != "" {
			r := parseCodexWithOffsets(content, validFrom, newCodexState(opts.ImageURL))
			if len(r.entries) >= maxEntries || from == 0 {
				start := validFrom
				if from == 0 {
					start = 0
				}
				prev := sliceCodexChunk(r, size, start, maxEntries)
				return readCodexRange(path, start, end, prev.StartByteOffset, opts)
			}
		}
		if from == 0 {
			return readCodexRange(path, 0, end, 0, opts)
		}
		chunk *= 2
	}
}
