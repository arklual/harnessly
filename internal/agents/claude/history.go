package claude

import (
	"os"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/transcriptindex"
	"github.com/ly-solutions/harnessly/internal/transcriptindex/tally"
)

const activityTailSize = 1 << 20

// ReadHistoryStats reads a finished turn from the transcript's last 128 KiB, newest line first, back to the last
// file-history snapshot: the prompt and answer, when they were written, the turn's duration, the tools used and the
// files edited or written.
func ReadHistoryStats(path string) agents.HistoryStats {
	out := agents.EmptyHistoryStats()
	st, err := os.Stat(path)
	if err != nil || st.Size() == 0 {
		return out
	}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	raw, err := agents.ReadTail(f, st.Size(), extendedTailSize)
	if err != nil {
		return out
	}
	entries := Parse(agents.Lines(raw))
	touched := map[string]bool{}
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e == nil {
			continue
		}
		typ := e.Val("type")
		if typ == "file-history-snapshot" {
			break
		}
		if agents.Truthy(e.Val("isSidechain")) {
			continue
		}
		if typ == "system" && e.Val("subtype") == "turn_duration" {
			if d, ok := e.Val("durationMs").(float64); ok && (out.TurnDurationMs == nil || *out.TurnDurationMs == 0) {
				out.TurnDurationMs = &d
			}
		}
		ts := entryTime(e)
		if ts != nil && *ts == 0 {
			ts = nil
		}
		content, isArr := message(e).Val("content").([]any)
		switch typ {
		case "user":
			if ts != nil {
				out.FirstUserTs = ts
			}
			if out.LastUserText == nil && isArr {
				for _, b := range content {
					if b == nil {
						break
					}
					blk := agents.AsObj(b)
					if text, ok := blk.Str("text"); ok && blk.Val("type") == "text" && text != "" {
						out.LastUserText = &text
						break
					}
				}
			}
		case "assistant":
			if ts != nil && out.LastAssistantTs == nil {
				out.LastAssistantTs = ts
			}
			if !isArr {
				continue
			}
			var last *string
			for _, b := range content {
				if b == nil {
					last = nil
					break
				}
				blk := agents.AsObj(b)
				btype := blk.Val("type")
				if name := blk.Val("name"); btype == "tool_use" && agents.Truthy(name) {
					tool := agents.JSString(name)
					out.ToolUsage.Add(tool)
					if tool == "Edit" || tool == "Write" {
						if fp := agents.AsObj(blk.Val("input")).Val("file_path"); agents.Truthy(fp) {
							p := agents.JSString(fp)
							if !touched[p] {
								touched[p] = true
								out.TouchedFiles = append(out.TouchedFiles, p)
							}
						}
					}
				}
				if text, ok := blk.Str("text"); ok && btype == "text" && text != "" {
					last = &text
				}
			}
			if out.LastAssistantText == nil && last != nil {
				out.LastAssistantText = last
			}
		}
	}
	return out
}

// LastConversationAt is the time of the newest user or assistant entry (not a side chain) in epoch ms: when the
// conversation last moved. Not the file's mtime, which Claude bumps with bookkeeping lines when it starts and exits.
func LastConversationAt(entries []*agents.Obj) *float64 {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e == nil {
			continue
		}
		if typ := e.Val("type"); (typ != "user" && typ != "assistant") || agents.Truthy(e.Val("isSidechain")) || !agents.Truthy(e.Val("timestamp")) {
			continue
		}
		at := jscompat.ParseDate(agents.JSString(e.Val("timestamp")))
		if at == at { // not NaN
			return &at
		}
	}
	return nil
}

// ActivityReader answers LastConversationAt for a transcript's last 1 MiB, remembering the answer until the file
// changes.
type ActivityReader struct {
	// Lines, when set, gives what a transcript a conversation is live on gained, read once for all its readers: the
	// answer moves on by those lines instead of the tail being read again. nil reads the tail whenever the file
	// changed.
	Lines *transcriptindex.Follower

	mu    sync.Mutex
	cache map[string]activity
}

type activity struct {
	mtime time.Time
	size  int64
	at    *float64
	// end is where the file ended when it was read, when it ended with a whole line; -1 otherwise.
	end int64
}

// Forget drops what is remembered of a transcript (its tab is gone), so the cache does not grow with every path ever
// seen.
func (r *ActivityReader) Forget(path string) {
	r.mu.Lock()
	delete(r.cache, path)
	r.mu.Unlock()
	r.Lines.Forget(path)
}

// Len is the number of transcripts remembered.
func (r *ActivityReader) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.cache)
}

// At is the transcript's last conversation time, nil when it has none in its tail or cannot be read.
func (r *ActivityReader) At(path string) *float64 {
	if path == "" {
		return nil
	}
	if at, ok := r.followed(path); ok {
		return at
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	r.mu.Lock()
	c, ok := r.cache[path]
	r.mu.Unlock()
	if ok && c.mtime.Equal(st.ModTime()) && c.size == st.Size() {
		return c.at
	}
	f, err := os.Open(path)
	tally.Open()
	if err != nil {
		return nil
	}
	defer f.Close()
	raw, err := agents.ReadTail(f, st.Size(), activityTailSize)
	if err != nil {
		return nil
	}
	lines := agents.Lines(raw)
	if st.Size() > activityTailSize && len(lines) > 0 {
		lines = lines[1:]
	}
	at := LastConversationAt(Parse(lines))
	r.mu.Lock()
	if r.cache == nil {
		r.cache = map[string]activity{}
	}
	end := int64(-1)
	if len(raw) > 0 && raw[len(raw)-1] == '\n' {
		end = st.Size()
	}
	r.cache[path] = activity{mtime: st.ModTime(), size: st.Size(), at: at, end: end}
	r.mu.Unlock()
	if end >= 0 {
		r.Lines.Seat(path, end)
	}
	return at
}

// followed is At of a transcript whose new lines the shared read holds: the answer known so far, moved on by the
// conversation lines the transcript gained, without the file being opened. false leaves the answer to a read of the
// tail: nothing is known of the transcript yet, or a line is still being written.
func (r *ActivityReader) followed(path string) (*float64, bool) {
	if r.Lines == nil {
		return nil, false
	}
	r.mu.Lock()
	c, known := r.cache[path]
	r.mu.Unlock()
	if !known || c.end < 0 {
		return nil, false
	}
	data, partial, ok := r.Lines.Take(path, c.end)
	if !ok || partial {
		return nil, false
	}
	if len(data) == 0 {
		return c.at, true
	}
	if at := LastConversationAt(Parse(agents.Lines(data))); at != nil {
		c.at = at
	}
	// The file's time and size are not known here: a read of the tail, should one come, reads the file as it is.
	c.end, c.mtime, c.size = c.end+int64(len(data)), time.Time{}, -1
	r.mu.Lock()
	r.cache[path] = c
	r.mu.Unlock()
	return c.at, true
}
