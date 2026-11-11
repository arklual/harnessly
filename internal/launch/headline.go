package launch

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/transcriptindex"
	"github.com/ly-solutions/harnessly/internal/transcriptindex/tally"
)

// Headline is a transcript's last AI title and when the person last typed a prompt (epoch ms); nil when not found.
type Headline struct {
	Title        *string
	LastPromptAt *float64
}

type headlineScan struct {
	scanned int64
	Headline
}

const (
	headlineMaxChunk   = 8 << 20
	headlineFirstChunk = 256 << 10
)

var aiTitleRe = regexp.MustCompile(`"type":"ai-title","aiTitle":"((?:[^"\\]|\\.)*)"`)

// Headlines reads transcript headlines, each file's bytes once: a call reads only what was appended since the last.
type Headlines struct {
	// File, when set, keeps the headlines over a restart of the runtime: a transcript whose headline is known and
	// that did not grow is not read again by the first list after a start.
	File string
	// Lines, when set, gives what a transcript a conversation is live on gained, read once for all its readers, so
	// such a transcript is not opened for its headline; nil reads every transcript from the file.
	Lines *transcriptindex.Follower

	mu      sync.Mutex
	cache   map[string]headlineScan
	loaded  bool
	dirty   bool
	savedAt time.Time
}

// headlinesSaveEvery is how often, at most, the headlines are written to File.
const headlinesSaveEvery = 5 * time.Second

// keptHeadline is a headline as File holds it.
type keptHeadline struct {
	Scanned      int64    `json:"scanned"`
	Title        *string  `json:"title,omitempty"`
	LastPromptAt *float64 `json:"lastPromptAt,omitempty"`
}

// load reads File once. Called with the lock held.
func (h *Headlines) load() {
	if h.loaded {
		return
	}
	h.loaded = true
	if h.cache == nil {
		h.cache = map[string]headlineScan{}
	}
	if h.File == "" {
		return
	}
	raw, err := os.ReadFile(h.File)
	if err != nil {
		return
	}
	var kept map[string]keptHeadline
	if json.Unmarshal(raw, &kept) != nil {
		return
	}
	for path, k := range kept {
		h.cache[path] = headlineScan{scanned: k.Scanned, Headline: Headline{Title: k.Title, LastPromptAt: k.LastPromptAt}}
	}
}

// save writes File when the headlines changed, at most every headlinesSaveEvery. Called with the lock held.
func (h *Headlines) save(force bool) {
	if h.File == "" || !h.dirty || !force && time.Since(h.savedAt) < headlinesSaveEvery {
		return
	}
	kept := make(map[string]keptHeadline, len(h.cache))
	for path, s := range h.cache {
		kept[path] = keptHeadline{Scanned: s.scanned, Title: s.Title, LastPromptAt: s.LastPromptAt}
	}
	h.dirty, h.savedAt = false, time.Now()
	raw, err := json.Marshal(kept)
	if err != nil {
		return
	}
	tmp := h.File + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, h.File)
	}
}

// Flush writes the headlines read so far to File.
func (h *Headlines) Flush() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.save(true)
}

var harnessNotices = []string{"[Cross-session idle notice]", "[Cross-session delivery notice]", "Another Claude session sent a message:"}

// isHumanText: a typed prompt. A leading tag is a command echo or a wrapper, except an annotation chip.
func isHumanText(v any) bool {
	s, ok := v.(string)
	if !ok || jscompat.Trim(s) == "" {
		return false
	}
	start := strings.TrimLeftFunc(s, jscompat.IsSpace)
	for _, n := range harnessNotices {
		if strings.HasPrefix(start, n) {
			return false
		}
	}
	return !strings.HasPrefix(start, "<") || strings.HasPrefix(start, "<annotation ")
}

// PromptTimestamp is the time of a prompt the person typed on a Claude Code or Codex transcript line; NaN for any
// other line (tool results, meta, command echoes).
func PromptTimestamp(line string) float64 {
	nan := math.NaN()
	if !strings.Contains(line, `"user"`) && !strings.Contains(line, `"user_message"`) && !strings.Contains(line, `"enqueue"`) {
		return nan
	}
	if strings.Contains(line, `"tool_use_id"`) {
		return nan
	}
	v, err := jsvalue.Parse([]byte(line))
	if err != nil {
		return nan
	}
	e, ok := v.(*jsvalue.Object)
	if !ok {
		return nan
	}
	ts, _ := e.Get("timestamp")
	at := nan
	if jscompat.Truthy(ts) {
		at = jscompat.ParseDate(jscompat.String(ts))
	}
	if math.IsNaN(at) {
		return nan
	}
	typ, _ := e.Get("type")
	switch typ {
	case "queue-operation":
		op, _ := e.Get("operation")
		content, _ := e.Get("content")
		if op == "enqueue" && isHumanText(content) {
			return at
		}
		return nan
	case "event_msg":
		p := e.Object("payload")
		if p == nil {
			return nan
		}
		pt, _ := p.Get("type")
		msg, _ := p.Get("message")
		if pt == "user_message" && isHumanText(msg) {
			return at
		}
		return nan
	}
	meta, _ := e.Get("isMeta")
	compact, _ := e.Get("isCompactSummary")
	if typ != "user" || jscompat.Truthy(meta) || jscompat.Truthy(compact) {
		return nan
	}
	m := e.Object("message")
	if m == nil {
		return nan
	}
	content, _ := m.Get("content")
	switch c := content.(type) {
	case string:
		if isHumanText(c) {
			return at
		}
	case []any:
		for _, part := range c {
			if p, ok := part.(*jsvalue.Object); ok {
				t, _ := p.Get("type")
				text, _ := p.Get("text")
				if t == "text" && isHumanText(text) {
					return at
				}
			}
		}
	}
	return nan
}

func scanLines(text string, known Headline) Headline {
	h := known
	for _, m := range aiTitleRe.FindAllStringSubmatch(text, -1) {
		var t string
		if json.Unmarshal([]byte(`"`+m[1]+`"`), &t) == nil {
			h.Title = &t
		}
	}
	for _, line := range strings.Split(text, "\n") {
		at := PromptTimestamp(line)
		last := 0.0
		if h.LastPromptAt != nil {
			last = *h.LastPromptAt
		}
		if !math.IsNaN(at) && at > last {
			a := at
			h.LastPromptAt = &a
		}
	}
	return h
}

func or[T any](a, b *T) *T {
	if a != nil {
		return a
	}
	return b
}

// Read is the headline of a transcript. It reads back from the end in growing steps until a prompt and a title turn up
// (Codex writes no titles, so only a prompt is looked for there); the last line, perhaps still being written, waits for
// the next call.
func (h *Headlines) Read(path string) Headline {
	if out, ok := h.readFollowed(path); ok {
		return out
	}
	st, err := os.Stat(path)
	if err != nil {
		return Headline{}
	}
	size := st.Size()
	h.mu.Lock()
	h.load()
	prev := h.cache[path]
	h.save(false)
	h.mu.Unlock()
	if size < prev.scanned {
		prev = headlineScan{}
	}
	if size == prev.scanned {
		return prev.Headline
	}
	floor := max(prev.scanned, size-headlineMaxChunk)
	f, err := os.Open(path)
	tally.Open()
	if err != nil {
		return Headline{}
	}
	defer f.Close()
	end := size
	var carry []byte
	step := int64(headlineFirstChunk)
	if prev.scanned > 0 {
		step = size - floor
	}
	var newer Headline
	scannedTo := floor
	titled := !strings.Contains(path, "/.codex/")
	for end > floor {
		start := max(floor, end-step)
		chunk := make([]byte, end-start)
		n, _ := f.ReadAt(chunk, start)
		chunk = chunk[:n]
		b := append(chunk, carry...)
		if end == size {
			b = b[:bytes.LastIndexByte(b, '\n')+1]
			scannedTo = start + int64(len(b))
		}
		head := 0
		if start > floor {
			head = bytes.IndexByte(b, '\n') + 1
			if head > 0 {
				carry = append([]byte(nil), b[:head]...)
			} else {
				carry = append([]byte(nil), b...)
			}
		} else {
			carry = nil
		}
		if start == floor || head > 0 {
			older := scanLines(strings.ToValidUTF8(string(b[head:]), "�"), Headline{})
			newer = Headline{Title: or(newer.Title, older.Title), LastPromptAt: or(newer.LastPromptAt, older.LastPromptAt)}
		}
		end = start
		if newer.LastPromptAt != nil && (newer.Title != nil || !titled) {
			break
		}
		step *= 4
	}
	out := Headline{Title: or(newer.Title, prev.Title), LastPromptAt: or(newer.LastPromptAt, prev.LastPromptAt)}
	h.mu.Lock()
	h.cache[path] = headlineScan{scanned: max(scannedTo, prev.scanned), Headline: out}
	h.dirty = true
	h.save(false)
	h.mu.Unlock()
	if scannedTo > floor || floor == prev.scanned {
		// The headline is read up to the end of a whole line: the lines after it may come from the shared read.
		h.Lines.Seat(path, max(scannedTo, prev.scanned))
	}
	return out
}

// readFollowed is Read of a transcript whose new lines the shared read holds: the headline known so far, moved on by
// the whole lines the transcript gained, without the file being opened. false leaves the read to the file.
func (h *Headlines) readFollowed(path string) (Headline, bool) {
	if h.Lines == nil {
		return Headline{}, false
	}
	h.mu.Lock()
	h.load()
	prev, known := h.cache[path]
	h.mu.Unlock()
	if !known || prev.scanned == 0 {
		return Headline{}, false
	}
	data, _, ok := h.Lines.Take(path, prev.scanned)
	if !ok {
		return Headline{}, false
	}
	out := prev.Headline
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(data) > 0 {
		newer := scanLines(strings.ToValidUTF8(string(data), "\uFFFD"), Headline{})
		out = Headline{Title: or(newer.Title, prev.Title), LastPromptAt: or(newer.LastPromptAt, prev.LastPromptAt)}
		h.cache[path] = headlineScan{scanned: prev.scanned + int64(len(data)), Headline: out}
		h.dirty = true
	}
	h.save(false)
	return out, true
}
