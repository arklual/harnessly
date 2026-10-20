// Package transcript reads the coding agents' conversation files (Claude Code and Codex jsonl) into timeline entries,
// the shape the conversation view draws, exactly as the previous runtime's parsers built them: the same entries, keys
// in the same order, the same byte offsets.
package transcript

import (
	"crypto/rand"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

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
}

// Incremental is what a transcript gained since an offset.
type Incremental struct {
	NewEntries []*jsvalue.Object
	NewOffset  int64
	// PendingBuffer is the start of a line still being written, read again whole next time.
	PendingBuffer string
}

const idAlphabet = "useandom-26T198340PX75pxJACKVERYMINDBUSHWOLF_GQZbfghjklqvwyzrict"

// NewID is an entry id (nanoid's 21 characters); tests replace it with a counter.
var NewID = func() string {
	var b [21]byte
	_, _ = rand.Read(b[:])
	for i := range b {
		b[i] = idAlphabet[b[i]&63]
	}
	return string(b[:])
}
