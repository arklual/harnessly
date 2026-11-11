package main

import (
	"os"
	"sync/atomic"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/transcriptindex"
)

// transcriptIndexEnv set to 0 keeps every reader of a transcript reading the file with a cursor of its own, as
// before the readers shared one read; anything else, or nothing, shares it.
const transcriptIndexEnv = "RUNTIME_TRANSCRIPT_INDEX"

func transcriptIndexOn(getenv func(string) string) bool { return getenv(transcriptIndexEnv) != "0" }

// transcripts is the runtime's one reader of what transcripts gain, for the readers built where the core is not at
// hand (the routes); nil until the core is made, and for good when switched off.
var transcripts atomic.Pointer[transcriptindex.Index]

// transcriptLines is a place in the shared read for a reader that reads transcripts when asked; nil, which such a
// reader takes for reading every file itself, when the shared read is off.
func transcriptLines(reader string) *transcriptindex.Follower {
	return transcripts.Load().Follower(reader, 0)
}

// transcriptNumbers is what the shared read counts, for the health probe: the transcripts followed and their readers
// now, and since the start the looks at them, the reads and their bytes, the readers that lost their place and the
// files found replaced; nil when the shared read is off.
func transcriptNumbers() any {
	ix := transcripts.Load()
	if ix == nil {
		return nil
	}
	c := ix.Counters()
	return jsvalue.NewObject("files", c.Files, "readers", c.Subs, "looks", c.Looks, "opens", c.Opens, "bytes", c.Bytes,
		"lost", c.Overflows+c.Gaps, "resets", c.Resets)
}

// newTranscriptIndex is the runtime's one reader of what transcripts gain; nil when switched off, which every reader
// takes for reading by itself.
func newTranscriptIndex() *transcriptindex.Index {
	if !transcriptIndexOn(os.Getenv) {
		agent.Log("info", "transcripts: every reader reads the file itself ("+transcriptIndexEnv+"=0)")
		transcripts.Store(nil)
		return nil
	}
	ix := makeTranscriptIndex()
	transcripts.Store(ix)
	return ix
}

func makeTranscriptIndex() *transcriptindex.Index {
	return transcriptindex.New(transcriptindex.Options{Warn: func(msg string, kv ...any) {
		fs := make([]agent.F, 0, len(kv)/2)
		for i := 0; i+1 < len(kv); i += 2 {
			if k, ok := kv[i].(string); ok {
				fs = append(fs, agent.F{K: k, V: kv[i+1]})
			}
		}
		agent.Log("warn", msg, fs...)
	}})
}
