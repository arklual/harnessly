// Package transcript reads the coding agents' conversation files (Claude Code and Codex jsonl) into timeline entries,
// the shape the conversation view draws, as the previous runtime's parsers built them: the same entries, keys in the
// same order, the same byte offsets; only the ids differ.
//
// An entry id is made of the transcript's own bytes, never at random, so any read of the same bytes (the whole file, a
// tail, a page back, an incremental read) names an entry alike. A line's entries are numbered k = 0, 1, ... in the order
// they were made, and named wire.ClaudeEntryID(uuid, k) for a Claude line with a uuid, else wire.OffsetEntryID(offset,
// k) by the byte offset of the line in the file (every Codex line). Entries of a flush (CodexParser.FlushStale) belong
// to no line: they are named by the line that began their call, with k from 1000, and a re-read does not make them.
package transcript

import "github.com/ly-solutions/harnessly/internal/jsvalue"

// Chunk is a read of a stretch of a transcript.
type Chunk struct {
	Entries []*jsvalue.Object
	// StartByteOffset is where the line of the first entry starts.
	StartByteOffset int64
	// FileSize is where the read ends: for a tail, the end of the last whole line, so a live feed goes on from there.
	FileSize   int64
	HasMore    bool
	ErrorCount int
	// Summary and CustomTitle are nil when the transcript names none (undefined in the old runtime).
	Summary     *string
	CustomTitle *string
	// Carry is what the read leaves for an incremental read that goes on from FileSize (Claude).
	Carry Carry
}

// Incremental is what a transcript gained since an offset.
type Incremental struct {
	NewEntries []*jsvalue.Object
	NewOffset  int64
	// PendingBuffer is the start of a line still being written, read again whole next time.
	PendingBuffer string
	// Carry is what the read leaves for the next one (Claude).
	Carry Carry
}

// Carry is the state Claude lines leave for the lines after them: a command still running, whose expanded prompt
// may be the next line. A read that goes on from where another ended takes its carry, so it reads the lines as a
// whole read does.
type Carry struct {
	command       any
	expandsPrompt bool
	set           bool
}
