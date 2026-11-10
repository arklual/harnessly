package feed

import (
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// heavyConversation is a transcript the way a long agent session writes it: a few typed turns, each with many tool
// calls whose results are large (file reads, logs), so the tail the first read takes holds megabytes of text for
// few entries the feed keeps.
func heavyConversation(turns, calls, output int) string {
	var b strings.Builder
	big := strings.Repeat("x", output)
	for i := range turns {
		ts := fmt.Sprintf("2026-10-12T10:%02d:00.000Z", i)
		fmt.Fprintf(&b, `{"type":"user","timestamp":%q,"message":{"role":"user","content":"task %d"}}`+"\n", ts, i)
		for j := range calls {
			id := fmt.Sprintf("toolu_%02d_%04d", i, j)
			fmt.Fprintf(&b, `{"type":"assistant","timestamp":%q,"message":{"role":"assistant","content":[{"type":"tool_use","id":%q,"name":"Read","input":{"file_path":"/w/f%d"}}]}}`+"\n", ts, id, j)
			fmt.Fprintf(&b, `{"type":"user","timestamp":%q,"message":{"role":"user","content":[{"tool_use_id":%q,"type":"tool_result","content":"%s","is_error":false}]}}`+"\n", ts, id, big)
		}
		fmt.Fprintf(&b, `{"type":"assistant","timestamp":%q,"message":{"role":"assistant","content":[{"type":"text","text":"done %d"}]}}`+"\n", ts, i)
	}
	return b.String()
}

func heapInUse() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// A runtime restores every chat it had and reads each one's tail. A loaded feed keeps the entries it built, not the
// text it read them from: a hundred chats of large transcripts start in little memory, each with its first screen at
// once (the ids kept were slices of the read, and kept every read — gigabytes — alive).
func TestLoadedFeedsDoNotKeepTheirReads(t *testing.T) {
	if testing.Short() {
		t.Skip("reads a hundred transcripts")
	}
	path := write(t, heavyConversation(3, 40, 16<<10)) // ~2 MB, three turns: the first read takes all of it
	st, _ := os.Stat(path)
	const chats = 100
	base := heapInUse()
	feeds := make([]*Feed, 0, chats)
	var took []time.Duration
	for range chats {
		start := time.Now()
		f := New("c", wire.ProviderClaude, &ClaudeSource{Path: path})
		f.Load()
		if ts, _, _ := f.Snapshot(20); len(ts) != 3 {
			t.Fatalf("snapshot: %d turns, want 3", len(ts))
		}
		took = append(took, time.Since(start))
		feeds = append(feeds, f)
	}
	kept := heapInUse() - base
	read := uint64(chats) * uint64(st.Size())
	slices.Sort(took)
	t.Logf("%d chats read %d MB, keep %d MB; first screen median %v, slowest %v", chats, read>>20, kept>>20,
		took[len(took)/2], took[len(took)-1])
	if took[len(took)/2] > 2*time.Second {
		t.Fatalf("first screen takes %v", took[len(took)/2])
	}
	// What stays is the entries and the tools' whole outputs (the feed hands them out by entry); the read itself
	// kept, all of it and more stayed.
	if kept > read/4 {
		t.Fatalf("%d chats keep %d MB of the %d MB they read", chats, kept>>20, read>>20)
	}
	runtime.KeepAlive(feeds)
}
