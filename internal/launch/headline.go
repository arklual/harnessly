package launch

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
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
	mu    sync.Mutex
	cache map[string]headlineScan
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
	st, err := os.Stat(path)
	if err != nil {
		return Headline{}
	}
	size := st.Size()
	h.mu.Lock()
	if h.cache == nil {
		h.cache = map[string]headlineScan{}
	}
	prev := h.cache[path]
	h.mu.Unlock()
	if size < prev.scanned {
		prev = headlineScan{}
	}
	if size == prev.scanned {
		return prev.Headline
	}
	floor := max(prev.scanned, size-headlineMaxChunk)
	f, err := os.Open(path)
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
	h.mu.Unlock()
	return out
}
