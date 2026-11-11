// Package tally counts what reading transcripts costs the process: the files opened, the bytes read off them, the
// stats and the lines decoded as JSON. Every reader of a transcript, the shared index and the readers it replaces,
// counts here, so one measurement compares them.
package tally

import "sync/atomic"

var opens, bytes, stats, decodes atomic.Int64

// Counts is a reading of the counters.
type Counts struct {
	Opens   int64 // files opened
	Bytes   int64 // bytes read
	Stats   int64 // stat calls
	Decodes int64 // lines decoded as JSON
}

// Sub is what was counted since an earlier reading.
func (c Counts) Sub(before Counts) Counts {
	return Counts{Opens: c.Opens - before.Opens, Bytes: c.Bytes - before.Bytes, Stats: c.Stats - before.Stats, Decodes: c.Decodes - before.Decodes}
}

// Open counts a file opened.
func Open() { opens.Add(1) }

// Read counts n bytes read.
func Read(n int) { bytes.Add(int64(n)) }

// Stat counts a stat.
func Stat() { stats.Add(1) }

// Decode counts a line decoded.
func Decode() { decodes.Add(1) }

// Now is the counters as they stand.
func Now() Counts {
	return Counts{Opens: opens.Load(), Bytes: bytes.Load(), Stats: stats.Load(), Decodes: decodes.Load()}
}
