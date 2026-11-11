package transcriptindex

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/transcriptindex/tally"
)

// span is one read of a file: whole lines from base on, cut and decoded once for all its subscribers.
type span struct {
	path  string
	base  int64
	data  []byte
	lines []*Line
	cut   bool
}

func (sp *span) split() []*Line {
	if sp.cut {
		return sp.lines
	}
	sp.cut = true
	at := sp.base
	for rest := sp.data; len(rest) > 0; {
		i := bytes.IndexByte(rest, '\n')
		raw := rest[:i] // the data ends with a newline
		rest = rest[i+1:]
		if len(raw) > 0 {
			sp.lines = append(sp.lines, &Line{Offset: at, End: at + int64(i) + 1, Raw: raw})
		}
		at += int64(i) + 1
	}
	return sp.lines
}

// Batch is the whole lines a transcript gained from a subscriber's place on. Valid only while the Sink is called.
type Batch struct {
	Path string
	// From is the start of the first line and To the end of the last, its newline included.
	From, To int64
	blk      *span
}

// Bytes is the batch as it is in the file: whole lines, each with its newline.
func (b *Batch) Bytes() []byte { return b.blk.data[b.From-b.blk.base:] }

// Each calls fn with every line that is not empty, in order.
func (b *Batch) Each(fn func(l *Line)) {
	for _, l := range b.blk.split() {
		if l.Offset >= b.From {
			fn(l)
		}
	}
}

// Line is one line of a transcript, without its newline. Valid only while the Sink is called.
type Line struct {
	// Offset is where the line starts in the file, End where the next one does.
	Offset, End int64
	Raw         []byte

	text    string
	hasText bool
	fields  *Fields
	decoded bool
}

// Text is the line as valid UTF-8 text. It is made once, whoever asks.
func (l *Line) Text() string {
	if !l.hasText {
		l.text, l.hasText = strings.ToValidUTF8(string(l.Raw), "�"), true
	}
	return l.text
}

// Fields is what a transcript line says, as far as the index's subscribers read it: one decode serves them all.
type Fields struct {
	Type        string `json:"type"`
	Subtype     string `json:"subtype"`
	Operation   string `json:"operation"`
	Timestamp   any    `json:"timestamp"`
	IsSidechain any    `json:"isSidechain"`
	QuerySource string `json:"querySource"`
	Cwd         string `json:"cwd"`
	Content     any    `json:"content"`
	AITitle     string `json:"aiTitle"`
	CustomTitle string `json:"customTitle"`
	Message     struct {
		Content any `json:"content"`
	} `json:"message"`
	Params struct {
		OutputConfig struct {
			Effort string `json:"effort"`
		} `json:"output_config"`
	} `json:"params"`
	Attachment struct {
		Type      string `json:"type"`
		Met       bool   `json:"met"`
		Condition string `json:"condition"`
		HookName  string `json:"hookName"`
	} `json:"attachment"`
}

// Fields is the line decoded, once, whoever asks; nil when it does not decode into Fields. A line may still decode
// into a narrower shape (a field Fields types strictly holds something else): a subscriber that cares decodes Raw.
func (l *Line) Fields() *Fields {
	if !l.decoded {
		l.decoded = true
		var f Fields
		tally.Decode()
		if json.Unmarshal(l.Raw, &f) == nil {
			l.fields = &f
		}
	}
	return l.fields
}

// DefaultQueueBytes is how much of a transcript a Queue holds for its reader.
const DefaultQueueBytes = 1 << 20

// Queue is a subscriber that reads on its own goroutine, in its own time: the lines the transcript gained wait in it,
// as they are in the file, until it takes them. It holds max bytes at most: lines that do not fit are not kept, the
// queue is lost, and its reader reads the file from its own place.
type Queue struct {
	sub *Sub
	max int

	mu   sync.Mutex
	buf  []byte
	from int64
}

// Queue subscribes a queue of max bytes (DefaultQueueBytes when 0) to the transcript.
func (ix *Index) Queue(path, name string, max int) *Queue {
	if max <= 0 {
		max = DefaultQueueBytes
	}
	q := &Queue{max: max}
	q.sub = ix.Subscribe(path, name, q)
	return q
}

// Lines keeps a copy of the batch for the reader, or nothing at all when it does not fit.
func (q *Queue) Lines(b *Batch) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	data := b.Bytes()
	if len(q.buf)+len(data) > q.max {
		q.buf = nil
		return false
	}
	if len(q.buf) == 0 {
		q.from = b.From
	}
	q.buf = append(q.buf, data...)
	return true
}

// Seat says the reader has read the file up to offset itself.
func (q *Queue) Seat(offset int64) {
	q.sub.Seat(offset, 0)
	q.mu.Lock()
	if len(q.buf) > 0 && q.from != offset {
		q.buf = nil // lines of the place before
	}
	q.mu.Unlock()
}

// Take looks at the file and gives the reader the whole lines it gained since the reader's place: data starts at
// byte from of the file and is the reader's to keep. partial says a line is still being written after them. Anything
// but Fresh gives no lines: the reader reads from its own place and seats again.
func (q *Queue) Take() (data []byte, from int64, partial bool, st State) {
	st, partial = q.sub.Sync(true)
	q.mu.Lock()
	data, from = q.buf, q.from
	q.buf = nil
	q.mu.Unlock()
	if st != Fresh {
		return nil, 0, false, st
	}
	return data, from, partial, st
}

// Close ends the queue's subscription and drops what it holds.
func (q *Queue) Close() {
	q.sub.Close()
	q.mu.Lock()
	q.buf = nil
	q.mu.Unlock()
}
