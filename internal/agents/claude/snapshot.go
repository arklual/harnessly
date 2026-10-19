package claude

import (
	"errors"
	"math"
	"os"
	"regexp"
	"strings"
	"time"

	"harnessly/internal/agents"
	"harnessly/internal/jscompat"
)

const (
	tailSize         = 8192
	extendedTailSize = 131_072
	// StaleInterrupted: an answer without a stop reason is taken for an interrupted stream after this long.
	StaleInterrupted = 20_000
	// StaleAwaitingAPI: a prompt without an answer is taken for a lost turn after this long.
	StaleAwaitingAPI = 90_000
	snapshotCacheMax = 256
	snippetMax       = 200
	// InterruptPrefix starts the user line Claude writes when the person interrupts a turn.
	InterruptPrefix = "[Request interrupted by user"
)

// ScanResult is what the newest deciding line of a transcript says.
type ScanResult struct {
	Matched           bool
	Idle              bool
	Stale             bool
	NeedsStaleRecheck bool
	StaleMs           int64
	LastEntryTs       *float64
	Interrupted       bool
}

// Parse decodes transcript lines; a line that is not a JSON object is nil.
func Parse(lines [][]byte) []*agents.Obj {
	out := make([]*agents.Obj, len(lines))
	for i, l := range lines {
		out[i] = agents.DecodeObj(l)
	}
	return out
}

func message(e *agents.Obj) *agents.Obj { return agents.AsObj(e.Val("message")) }

func entryTime(e *agents.Obj) *float64 {
	ts := e.Val("timestamp")
	if !agents.Truthy(ts) {
		return nil
	}
	ms := jscompat.DateValue(ts)
	if math.IsNaN(ms) {
		return nil
	}
	return &ms
}

var localCommandTag = regexp.MustCompile(`^` + jscompat.SpaceClass + `*<(?:command-name|command-message|command-args|local-command-[a-z-]+)>`)

func userText(e *agents.Obj) string {
	switch c := message(e).Val("content").(type) {
	case string:
		return c
	case []any:
		if len(c) > 0 {
			if s, ok := agents.AsObj(c[0]).Str("text"); ok {
				return s
			}
		}
	}
	return ""
}

// localCommands are the uuids of lines a slash command the TUI runs by itself (/effort, /model, /context…) writes: they
// never reach the model, and taken for a prompt they would start a turn no Stop hook ever ends.
func localCommands(entries []*agents.Obj) map[string]bool {
	out := map[string]bool{}
	for _, e := range entries {
		uuid, ok := e.Str("uuid")
		if !ok {
			continue
		}
		typ := e.Val("type")
		if (typ == "system" && e.Val("subtype") == "local_command") || (typ == "user" && localCommandTag.MatchString(userText(e))) {
			out[uuid] = true
		}
	}
	return out
}

func isLocalCommand(e *agents.Obj, local map[string]bool) bool {
	if e.Val("type") != "user" {
		return false
	}
	if uuid, ok := e.Str("uuid"); ok && local[uuid] {
		return true
	}
	parent, ok := e.Str("parentUuid")
	return e.Val("isMeta") == true && ok && local[parent]
}

func isInterrupt(e *agents.Obj) bool {
	c, ok := message(e).Val("content").([]any)
	if !ok || len(c) != 1 {
		return false
	}
	text, ok := agents.AsObj(c[0]).Str("text")
	return ok && strings.HasPrefix(text, InterruptPrefix)
}

// ScanLines reads the turn's state from the newest line that decides it, skipping side chains and local slash
// commands; elapsed is the transcript's age in milliseconds.
func ScanLines(entries []*agents.Obj, elapsed float64) ScanResult {
	local := localCommands(entries)
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e == nil || agents.Truthy(e.Val("isSidechain")) || isLocalCommand(e, local) {
			continue
		}
		ts := entryTime(e)
		switch e.Val("type") {
		case "system":
			if sub := e.Val("subtype"); sub == "stop_hook_summary" || sub == "turn_duration" {
				return ScanResult{Matched: true, Idle: true, LastEntryTs: ts}
			}
		case "assistant":
			stop := message(e).Val("stop_reason")
			if !agents.Truthy(stop) {
				idle := elapsed > StaleInterrupted
				return ScanResult{Matched: true, Idle: idle, Stale: true, NeedsStaleRecheck: !idle, StaleMs: StaleInterrupted, LastEntryTs: ts}
			}
			return ScanResult{Matched: true, Idle: stop != "tool_use", LastEntryTs: ts}
		case "user":
			if isInterrupt(e) {
				return ScanResult{Matched: true, Idle: true, LastEntryTs: ts, Interrupted: true}
			}
			idle := elapsed > StaleAwaitingAPI
			return ScanResult{Matched: true, Idle: idle, Stale: true, NeedsStaleRecheck: !idle, StaleMs: StaleAwaitingAPI, LastEntryTs: ts}
		}
	}
	return ScanResult{Idle: elapsed > StaleAwaitingAPI, Stale: true, NeedsStaleRecheck: elapsed <= StaleAwaitingAPI, StaleMs: StaleAwaitingAPI}
}

// Assistant is what the newest answer shows.
type Assistant struct {
	LastSnippet   *string
	CurrentAction *agents.CurrentAction
	// Reset: a real prompt (not a tool result) is newer than the newest answer.
	Reset bool
}

// errBadBlock stands for a line JavaScript gave up on halfway (a null block, a text that is not a string): the line is
// skipped as a whole.
var errBadBlock = errors.New("malformed content block")

// blockText is a text block's trimmed text ("" when empty or absent).
func blockText(b *agents.Obj) (string, error) {
	switch t := b.Val("text").(type) {
	case nil:
		return "", nil
	case string:
		return jscompat.Trim(t), nil
	}
	return "", errBadBlock
}

func toCurrentAction(b *agents.Obj) *agents.CurrentAction {
	name := "Tool"
	if v := b.Val("name"); v != nil {
		name = agents.JSString(v)
	}
	input := b.Val("input")
	if input == nil {
		input = agents.NewObj()
	}
	return agents.Tool(name, SummarizeToolCall(name, input))
}

// assistantOf reads one assistant entry's content; an error skips the line.
func assistantOf(content []any) (Assistant, error) {
	var out Assistant
	for j := len(content) - 1; j >= 0; j-- {
		b, ok := content[j].(*agents.Obj)
		if content[j] == nil || (ok && b == nil) {
			return Assistant{}, errBadBlock
		}
		typ := b.Val("type")
		if typ == "tool_use" {
			out.CurrentAction = toCurrentAction(b)
			break
		}
		if typ == "text" {
			text, err := blockText(b)
			if err != nil {
				return Assistant{}, err
			}
			if text != "" {
				out.CurrentAction = agents.Text(jscompat.Truncate(text, snippetMax))
				break
			}
		}
	}
	for j := len(content) - 1; j >= 0; j-- {
		b, ok := content[j].(*agents.Obj)
		if content[j] == nil || (ok && b == nil) {
			return Assistant{}, errBadBlock
		}
		if b.Val("type") == "text" {
			text, err := blockText(b)
			if err != nil {
				return Assistant{}, err
			}
			if text != "" {
				s := jscompat.Truncate(text, snippetMax)
				out.LastSnippet = &s
				break
			}
		}
	}
	return out, nil
}

// ExtractAssistant reads the newest answer: its last text and its last tool call or text as the current action.
func ExtractAssistant(entries []*agents.Obj) Assistant {
	userSeen := false
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e == nil || agents.Truthy(e.Val("isSidechain")) {
			continue
		}
		msg := message(e)
		switch e.Val("type") {
		case "user":
			toolResult, bad := false, false
			if c, ok := msg.Val("content").([]any); ok {
				for _, b := range c {
					if b == nil {
						bad = true
						break
					}
					if agents.AsObj(b).Val("type") == "tool_result" {
						toolResult = true
						break
					}
				}
			}
			if !bad && !toolResult {
				userSeen = true
			}
			continue
		case "assistant":
		default:
			continue
		}
		content := msg.Val("content")
		if !agents.Truthy(content) {
			continue
		}
		if userSeen {
			return Assistant{Reset: true}
		}
		arr, ok := content.([]any)
		if !ok {
			continue
		}
		a, err := assistantOf(arr)
		if err != nil {
			continue
		}
		return a
	}
	return Assistant{}
}

type cached struct {
	mtime    time.Time
	snapshot agents.Snapshot
	recheck  bool
}

// SnapshotReader reads transcript snapshots, keeping the last 256 by path until the file changes.
type SnapshotReader struct {
	Now   func() time.Time
	cache agents.LRU[cached]
}

// NewSnapshotReader reads with the wall clock.
func NewSnapshotReader() *SnapshotReader {
	return &SnapshotReader{Now: time.Now, cache: agents.LRU[cached]{Max: snapshotCacheMax}}
}

// Read is the snapshot of the transcript at path: the last 8 KiB, the last 128 KiB when those decide nothing. force
// skips the cache. An empty transcript is idle; an unreadable one says nothing.
func (r *SnapshotReader) Read(path string, force bool) agents.Snapshot {
	st, err := os.Stat(path)
	if err != nil {
		return agents.Snapshot{}
	}
	if st.Size() == 0 {
		return agents.Snapshot{Idle: true}
	}
	now := r.Now()
	if c, ok := r.cache.Get(path); ok && !force && c.mtime.Equal(st.ModTime()) {
		s := c.snapshot
		switch {
		case s.Idle:
		case c.recheck:
			s.Idle = agents.MillisSince(now, st.ModTime()) > float64(s.StaleMs)
			s.Stale = true
		default:
			s.Stale = false
		}
		return s
	}
	f, err := os.Open(path)
	if err != nil {
		return agents.Snapshot{}
	}
	defer f.Close()
	elapsed := agents.MillisSince(now, st.ModTime())
	raw, err := agents.ReadTail(f, st.Size(), tailSize)
	if err != nil {
		return agents.Snapshot{}
	}
	entries := Parse(agents.Lines(raw))
	scan := ScanLines(entries, elapsed)
	extracted := ExtractAssistant(entries)
	if !scan.Matched && st.Size() > tailSize {
		raw, err := agents.ReadTail(f, st.Size(), extendedTailSize)
		if err != nil {
			return agents.Snapshot{}
		}
		ext := Parse(agents.Lines(raw))
		scan = ScanLines(ext, elapsed)
		if extracted.LastSnippet == nil && extracted.CurrentAction == nil {
			extracted = ExtractAssistant(ext)
		}
	}
	s := agents.Snapshot{
		Idle:                 scan.Idle,
		Stale:                scan.Stale,
		LastAssistantSnippet: extracted.LastSnippet,
		CurrentAction:        extracted.CurrentAction,
		Reset:                extracted.Reset,
		LastEntryTs:          scan.LastEntryTs,
		StaleMs:              scan.StaleMs,
		Interrupted:          scan.Interrupted,
	}
	r.cache.Put(path, cached{mtime: st.ModTime(), snapshot: s, recheck: scan.NeedsStaleRecheck})
	return s
}
