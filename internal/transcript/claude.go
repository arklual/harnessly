package transcript

import (
	"bytes"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The Claude Code transcript (session-parser in the previous runtime): the same entries, ids aside, the same offsets,
// and the same failures — where it threw, a read here gives what its catch gave.

const (
	claudeChunk         = 256_000
	claudeTailThreshold = 1_048_576
	claudeTailRead      = 512_000
)

var claudeExcluded = map[string]bool{"progress": true, "system": true, "file-history-snapshot": true, "agent-name": true}

// ReadTail is the last max entries of a transcript (readTailEntries).
func ReadTail(path string, max int) Chunk {
	var out Chunk
	if claudeCatch(func() { out = readTail(path, max) }) {
		return Chunk{Entries: []*jsvalue.Object{}}
	}
	return out
}

// ReadBefore is up to max entries that end before beforeByte (readEntriesBefore).
func ReadBefore(path string, beforeByte int64, max int) Chunk {
	var out Chunk
	if claudeCatch(func() { out = readBefore(path, beforeByte, max) }) {
		return Chunk{Entries: []*jsvalue.Object{}}
	}
	return out
}

// ReadTailBytes is the entries of the last window bytes of a transcript, whole lines from the first line that starts
// in it, the last max of them: one read from the end, where ReadTail reads ever larger tails until it has max entries.
// A transcript whose lines are large (tool outputs, pictures) is read only as far back as asked.
func ReadTailBytes(path string, window int64, max int) Chunk {
	var out Chunk
	if claudeCatch(func() { out = readTailBytes(path, window, max) }) {
		return Chunk{Entries: []*jsvalue.Object{}}
	}
	return out
}

// ReadBeforeBytes is ReadTailBytes of what precedes beforeByte: the entries whose lines start in the window bytes
// before it.
func ReadBeforeBytes(path string, beforeByte, window int64, max int) Chunk {
	var out Chunk
	if claudeCatch(func() { out = readBeforeBytes(path, beforeByte, window, max) }) {
		return Chunk{Entries: []*jsvalue.Object{}}
	}
	return out
}

// ParseIncremental is what the file gained after offset (parseIncremental).
func ParseIncremental(path string, offset int64, pending string) Incremental {
	return ParseIncrementalCarry(path, offset, pending, Carry{})
}

// ParseIncrementalCarry is ParseIncremental going on from a read that left carry.
func ParseIncrementalCarry(path string, offset int64, pending string, carry Carry) Incremental {
	var out Incremental
	if claudeCatch(func() { out = parseIncremental(path, offset, pending, carry) }) {
		return Incremental{NewEntries: []*jsvalue.Object{}, NewOffset: offset, PendingBuffer: pending, Carry: carry}
	}
	return out
}

// ParseContent is the entries of transcript text (parseJsonlContent); nil where the previous runtime threw. The text is
// taken to be a whole file, so a line without a uuid is named by its offset in the text.
func ParseContent(content string) []*jsvalue.Object { return ParseContentAt(content, 0) }

// ParseContentAt is ParseContent of text that starts at byte start of its file, whole lines from a line's start.
func ParseContentAt(content string, start int64) []*jsvalue.Object {
	var out []*jsvalue.Object
	if claudeCatch(func() { out = parseClaude(content, textAt{start: start, firstLine: -1}).entries }) {
		return nil
	}
	return out
}

// ParseContentChecked is ParseContent that tells a throw of the previous runtime's parser from an empty result.
func ParseContentChecked(content string) (entries []*jsvalue.Object, threw bool) {
	threw = claudeCatch(func() { entries = parseClaude(content, textAt{firstLine: -1}).entries })
	return entries, threw
}

// ParseFile is the entries of a whole file, its last 512 000 bytes when it is over 1 MiB (parseJsonlFile). Where the
// previous runtime threw (a malformed line its parser trips on) this is empty.
func ParseFile(path string) []*jsvalue.Object {
	var out []*jsvalue.Object
	if claudeCatch(func() { out = parseFile(path) }) || out == nil {
		return []*jsvalue.Object{}
	}
	return out
}

func parseFile(path string) []*jsvalue.Object {
	st, err := os.Stat(path)
	if err != nil || st.Size() == 0 {
		return nil
	}
	size := st.Size()
	if size > claudeTailThreshold {
		read := min(size, claudeTailRead)
		buf, err := readRangeBytes(path, size-read, size)
		if err != nil {
			return nil
		}
		start := size - read
		content := claudeDecode(buf)
		if i := strings.IndexByte(content, '\n'); i >= 0 {
			content = content[i+1:]
			start += int64(bytes.IndexByte(buf, '\n') + 1)
		}
		return parseClaude(content, textAt{start: start, firstLine: -1}).entries
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return parseClaude(claudeDecode(data), textAt{firstLine: -1}).entries
}

// readRangeBytes is the bytes [from, to); what the file no longer holds reads as zero bytes, as in a Buffer.alloc.
func readRangeBytes(path string, from, to int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, to-from)
	if _, err := f.ReadAt(buf, from); err != nil && err != io.EOF {
		return nil, err
	}
	return buf, nil
}

// readChunk is the whole lines of [from, to): a chunk not starting the file drops what precedes its first newline.
// It answers where the text starts twice: validFrom as the previous runtime counted it (in the decoded text, so a
// broken character in the dropped part counts three bytes), and start, the byte the first line really begins at.
func readChunk(path string, from, to int64) (content string, validFrom, start int64, err error) {
	if to-from <= 0 {
		return "", from, from, nil
	}
	buf, err := readRangeBytes(path, from, to)
	if err != nil {
		return "", 0, 0, err
	}
	raw := claudeDecode(buf)
	if from == 0 {
		return raw, 0, 0, nil
	}
	i := strings.IndexByte(raw, '\n')
	if i < 0 {
		return "", to, to, nil
	}
	return raw[i+1:], from + int64(i+1), from + int64(bytes.IndexByte(buf, '\n')+1), nil
}

// wholeLines leaves a line still being written for the next read.
func wholeLines(content string) (string, int64) {
	end := strings.LastIndexByte(content, '\n') + 1
	return content[:end], int64(len(content) - end)
}

func (r claudeResult) chunk(max int, validFrom int64, fromStart bool) Chunk {
	entries := r.entries
	var start int64
	sliced := len(entries) > max
	switch {
	case sliced:
		cut := len(entries) - max
		start = validFrom + r.offsets[cut]
		entries = entries[cut:]
	case fromStart:
		start = 0
	default:
		start = validFrom
	}
	return Chunk{Entries: entries, StartByteOffset: start, HasMore: start > 0, ErrorCount: r.errorCount}
}

func readTail(path string, max int) Chunk {
	empty := Chunk{Entries: []*jsvalue.Object{}}
	st, err := os.Stat(path)
	if err != nil || st.Size() == 0 {
		return empty
	}
	size := st.Size()
	if size <= claudeChunk {
		data, err := os.ReadFile(path)
		if err != nil {
			return empty
		}
		content, partial := wholeLines(claudeDecode(data))
		r := parseClaude(content, textAt{firstLine: -1})
		c := r.chunk(max, 0, true)
		c.HasMore = len(r.entries) > max
		c.FileSize = size - partial
		c.Summary, c.CustomTitle, c.Carry = r.summary, r.customTitle, r.carry
		return c
	}
	for chunkSize := int64(claudeChunk); chunkSize < size*2; chunkSize *= 2 {
		from := max64(0, size-chunkSize)
		raw, validFrom, start, err := readChunk(path, from, size)
		if err != nil {
			return empty
		}
		content, partial := wholeLines(raw)
		if content == "" {
			continue
		}
		r := parseClaude(content, textAt{start: start, firstLine: -1})
		if len(r.entries) >= max || from == 0 {
			c := r.chunk(max, validFrom, from == 0)
			c.FileSize = size - partial
			c.Summary, c.CustomTitle, c.Carry = r.summary, r.customTitle, r.carry
			return c
		}
	}
	return empty
}

func readBefore(path string, beforeByte int64, max int) Chunk {
	empty := Chunk{Entries: []*jsvalue.Object{}}
	if beforeByte <= 0 {
		return empty
	}
	st, err := os.Stat(path)
	if err != nil {
		return empty
	}
	for chunkSize := int64(claudeChunk); ; chunkSize *= 2 {
		from := max64(0, beforeByte-chunkSize)
		content, validFrom, start, err := readChunk(path, from, beforeByte)
		if err != nil {
			return empty
		}
		if content != "" {
			r := parseClaude(content, textAt{start: start, firstLine: -1})
			if len(r.entries) >= max || from == 0 {
				c := r.chunk(max, validFrom, from == 0)
				c.FileSize = st.Size()
				return c
			}
		}
		if from == 0 {
			return empty
		}
	}
}

// contextLines is how many lines before a read by bytes are parsed with it: what a line is can depend on the lines
// before it (a slash command's expanded body follows the command, a skill's body its local command), so a window is
// parsed with them, and only the entries whose lines start in the window are kept. contextMax caps how far back.
const (
	contextLines = 256
	contextMax   = 1 << 20
)

// contextStart is the start of the line contextLines lines before the one at from (from itself may be inside a
// line), at most contextMax bytes before it; 0 at the file's start.
func contextStart(path string, from int64) int64 {
	if from <= 0 {
		return 0
	}
	f, err := os.Open(path)
	if err != nil {
		return from
	}
	defer f.Close()
	const block = 4096
	buf := make([]byte, block)
	floor := max64(0, from-contextMax)
	seen := 0
	for end := from; end > floor; {
		lo := max64(floor, end-block)
		n, _ := f.ReadAt(buf[:end-lo], lo)
		for i := n - 1; i >= 0; i-- {
			if buf[i] != '\n' {
				continue
			}
			if seen++; seen > contextLines {
				return lo + int64(i) + 1
			}
		}
		end = lo
	}
	return floor
}

// readWindow parses the whole lines of [from, to) with the lines before it as context, and keeps the entries whose
// lines start at from or later; their offsets are where their lines start in the file. partial is the length of a
// last line still being written.
func readWindow(path string, from, to int64) (r claudeResult, partial int64, ok bool) {
	ctx := contextStart(path, from)
	readFrom := ctx
	if ctx > 0 && ctx < from {
		readFrom = ctx - 1 // its newline: readChunk drops what precedes the first one, so the text starts at ctx
	}
	raw, validFrom, start, err := readChunk(path, readFrom, to)
	if err != nil {
		return claudeResult{}, 0, false
	}
	content, partial := wholeLines(raw)
	if content == "" {
		return claudeResult{}, partial, true
	}
	r = parseClaude(content, textAt{start: start, firstLine: -1})
	keep := len(r.entries)
	for i := range r.offsets {
		r.offsets[i] += validFrom
		if keep == len(r.entries) && r.offsets[i] >= from {
			keep = i
		}
	}
	r.entries, r.offsets = r.entries[keep:], r.offsets[keep:]
	return r, partial, true
}

func readTailBytes(path string, window int64, max int) Chunk {
	empty := Chunk{Entries: []*jsvalue.Object{}}
	st, err := os.Stat(path)
	if err != nil || st.Size() == 0 {
		return empty
	}
	size := st.Size()
	from := max64(0, size-window)
	r, partial, ok := readWindow(path, from, size)
	if !ok {
		return empty
	}
	if len(r.entries) == 0 {
		// No line starts and ends in the window (one longer than it): nothing read, a larger window is asked.
		return Chunk{Entries: []*jsvalue.Object{}, StartByteOffset: size - partial, HasMore: from > 0, FileSize: size - partial,
			Summary: r.summary, CustomTitle: r.customTitle, Carry: r.carry}
	}
	c := r.window(max, from == 0)
	c.FileSize = size - partial
	c.Summary, c.CustomTitle, c.Carry = r.summary, r.customTitle, r.carry
	return c
}

func readBeforeBytes(path string, beforeByte, window int64, max int) Chunk {
	empty := Chunk{Entries: []*jsvalue.Object{}}
	if beforeByte <= 0 {
		return empty
	}
	st, err := os.Stat(path)
	if err != nil {
		return empty
	}
	from := max64(0, beforeByte-window)
	r, _, ok := readWindow(path, from, beforeByte)
	if !ok {
		return empty
	}
	if len(r.entries) == 0 {
		return Chunk{Entries: []*jsvalue.Object{}, StartByteOffset: beforeByte, HasMore: from > 0, FileSize: st.Size()}
	}
	c := r.window(max, from == 0)
	c.FileSize = st.Size()
	return c
}

// window is the chunk of a read by bytes: the last max entries, from the line the first of them starts at.
func (r claudeResult) window(max int, fromStart bool) Chunk {
	entries, start := r.entries, int64(0)
	if len(entries) > max {
		cut := len(entries) - max
		entries, start = entries[cut:], r.offsets[cut]
	} else if !fromStart {
		start = r.offsets[0]
	}
	return Chunk{Entries: entries, StartByteOffset: start, HasMore: start > 0, ErrorCount: r.errorCount}
}

func parseIncremental(path string, offset int64, pending string, carry Carry) Incremental {
	keep := Incremental{NewEntries: []*jsvalue.Object{}, NewOffset: offset, PendingBuffer: pending, Carry: carry}
	f, err := os.Open(path)
	if err != nil {
		return keep
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return keep
	}
	size := st.Size()
	if size < offset {
		f.Close()
		data, err := os.ReadFile(path)
		if err != nil {
			return keep
		}
		r := parseClaude(claudeDecode(data), textAt{firstLine: -1})
		return Incremental{NewEntries: r.entries, NewOffset: size, Carry: r.carry}
	}
	if offset >= size {
		f.Close()
		return keep
	}
	buf := make([]byte, size-offset)
	_, err = f.ReadAt(buf, offset)
	where := textAt{start: offset, firstLine: -1}
	if pending != "" {
		where = pendingAt(f, offset, buf)
	}
	where.carry = carry
	f.Close()
	if err != nil && err != io.EOF {
		return keep
	}
	raw := pending + claudeDecode(buf)
	segments := strings.Split(raw, "\n")
	newPending := ""
	if !strings.HasSuffix(raw, "\n") {
		last := segments[len(segments)-1]
		segments = segments[:len(segments)-1]
		if last != "" {
			if _, err := claudeParseJSON(last); err == nil {
				segments = append(segments, last)
			} else {
				newPending = last
			}
		}
	}
	r := parseClaude(strings.Join(segments, "\n"), where)
	return Incremental{NewEntries: r.entries, NewOffset: size, PendingBuffer: newPending, Carry: r.carry}
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

type claudeRaw struct {
	base   *claudeBase
	raw    any
	offset int64
}

type claudeAgentHint struct{ agentType, description string }

type claudeResult struct {
	entries     []*jsvalue.Object
	offsets     []int64
	errorCount  int
	summary     *string
	customTitle *string
	carry       Carry
}

type claudeParse struct {
	entries []*jsvalue.Object
	offsets []int64

	// base is where the parsed text starts in the file; next counts, per line, the entries named so far.
	base int64
	next map[string]int
}

// name gives the entries a line made their ids, in the order they were made: "c:<uuid>#k" when the line has a uuid,
// else "x:<offset of the line in the file>#k", k counting from the first entry the line made (an entry folded into an
// earlier one still takes its k).
func (p *claudeParse) name(r claudeRaw, entries ...*jsvalue.Object) {
	uuid, _ := r.base.uuid.(string)
	line := "c:" + uuid
	byUUID := wire.ValidEntryID(wire.ClaudeEntryID(uuid, 0))
	if !byUUID {
		line = "x:" + strconv.FormatInt(p.base+r.offset, 10)
	}
	for _, e := range entries {
		k := p.next[line]
		p.next[line] = k + 1
		if byUUID {
			e.Set("id", wire.ClaudeEntryID(uuid, k))
		} else {
			e.Set("id", wire.OffsetEntryID(p.base+r.offset, k))
		}
	}
}

func (p *claudeParse) push(e *jsvalue.Object, offset int64) {
	p.entries = append(p.entries, e)
	p.offsets = append(p.offsets, offset)
}

// pushCommands folds an output into the command just before it, so both stay one card.
func (p *claudeParse) pushCommands(parsed []*jsvalue.Object, offset int64) {
	for _, e := range parsed {
		if typeOf(e) == "command-output" {
			if target := p.commandForOutput(); target >= 0 {
				cmd := p.entries[target].Copy()
				cmd.Set("output", get(e, "output"))
				if get(e, "isError") == true {
					cmd.Set("isError", true)
				}
				p.entries[target] = cmd
				continue
			}
		}
		p.push(e, offset)
	}
}

func (p *claudeParse) commandForOutput() int {
	stop := max(0, len(p.entries)-claudeCommandLookback)
	for i := len(p.entries) - 1; i >= stop; i-- {
		if typeOf(p.entries[i]) == "slash-command" && !p.entries[i].Has("output") {
			return i
		}
	}
	return -1
}

// firstPass reads one line into raw: false when it is an error (unparsable, refused by the schema, or a throw).
func (r *claudeResult) firstPass(line string, offset int64, raws *[]claudeRaw) (ok bool) {
	threw := claudeCatch(func() {
		raw, err := claudeParseJSON(line)
		if err != nil {
			claudeFail("bad JSON")
		}
		b, valid := zBaseEntry(raw)
		if !valid {
			ok = false
			return
		}
		ok = true
		o := raw.(*jsvalue.Object)
		if r.summary == nil && b.typ == "user" && get(o, "isCompactSummary") != true {
			if content, valid := zUserEntry(raw); valid {
				switch c := content.(type) {
				case string:
					if cleaned := stripProtocolTags(c); cleaned != "" {
						r.summary = &cleaned
					}
				case []any:
					for _, it := range c {
						item := it.(*jsvalue.Object)
						if isStr(get(item, "type"), "text") && item.Has("text") {
							if cleaned := stripProtocolTags(get(item, "text")); cleaned != "" {
								r.summary = &cleaned
								break
							}
						}
					}
				}
			}
		}
		switch b.typ {
		case "custom-title":
			title := coalesce(get(o, "title"), get(o, "customTitle"))
			if s, isText := title.(string); isText && jscompat.Trim(s) != "" {
				t := jscompat.Trim(s)
				r.customTitle = &t
			}
			return
		case "summary":
			text := coalesce(get(o, "summary"), get(o, "text"))
			if s, isText := text.(string); isText && jscompat.Trim(s) != "" && r.summary == nil {
				t := jscompat.Trim(s)
				r.summary = &t
			}
			return
		case "system":
			sub := get(o, "subtype")
			if isStr(sub, "stop_hook_summary") || isStr(sub, "turn_duration") || isStr(sub, "local_command") || systemNotice(o) != nil {
				*raws = append(*raws, claudeRaw{b, raw, offset})
			}
			return
		}
		if claudeExcluded[b.typ] {
			return
		}
		*raws = append(*raws, claudeRaw{b, raw, offset})
	})
	return ok && !threw
}

// textAt is where the text parseClaude reads lies in the file, which names the lines without a uuid.
type textAt struct {
	// start is the byte offset of the text's first line.
	start int64
	// firstLine is how many bytes that line holds in the file, or -1 when the text's own length is right: a line cut
	// between two reads is decoded in two halves, each broken character becoming a longer U+FFFD.
	firstLine int64
	// carry is what the lines before the text left.
	carry Carry
}

// pendingAt places the text of an incremental read that starts with a pending line: the line began after the last
// newline before offset, and goes on in buf up to its first newline.
func pendingAt(f *os.File, offset int64, buf []byte) textAt {
	start := lineStart(f, offset)
	first := int64(len(buf))
	if i := bytes.IndexByte(buf, '\n'); i >= 0 {
		first = int64(i)
	}
	return textAt{start: start, firstLine: offset - start + first}
}

// lineStart is where the line holding the byte before offset begins: just past the last newline before offset.
func lineStart(f *os.File, offset int64) int64 {
	const block = 4096
	buf := make([]byte, block)
	for end := offset; end > 0; {
		from := max64(0, end-block)
		n, _ := f.ReadAt(buf[:end-from], from)
		if i := bytes.LastIndexByte(buf[:n], '\n'); i >= 0 {
			return from + int64(i) + 1
		}
		end = from
	}
	return 0
}

// parseClaude reads content, which lies in the file as at says.
func parseClaude(content string, at textAt) claudeResult {
	var r claudeResult
	var raws []claudeRaw
	var pos int64
	for n, line := range strings.Split(content, "\n") {
		offset := pos
		if n == 0 && at.firstLine >= 0 {
			pos += at.firstLine + 1
		} else {
			pos += int64(len(line)) + 1
		}
		if jscompat.Trim(line) == "" {
			continue
		}
		if !r.firstPass(line, offset, &raws) {
			r.errorCount++
		}
	}

	p := &claudeParse{base: at.start, next: map[string]int{}}
	var hint *claudeAgentHint
	var running *claudeRunningCommand
	if at.carry.set {
		running = &claudeRunningCommand{uuid: at.carry.command, expandsPrompt: at.carry.expandsPrompt}
	}
	planFilePath := ""
	for i := 0; i < len(raws); {
		cur := raws[i]
		b, raw, offset := cur.base, cur.raw, cur.offset

		if b.typ == "queue-operation" {
			text, isText := get(raw, "content").(string)
			if isStr(get(raw, "operation"), "enqueue") && isText && jscompat.Trim(text) != "" &&
				(!strings.HasPrefix(trimStart(text), "<") || isPeerMessage(text)) && !strings.HasPrefix(trimStart(text), claudeDeliveryNotice) {
				e := obj("id", unsetID, "type", "user-message", "timestamp", b.stamp(), "text", dropHopChain(text))
				if !isHarnessNotice(text) {
					e.Set("queued", true)
					e.Set("pending", true)
				}
				p.name(cur, e)
				p.push(e, offset)
			}
			i++
			continue
		}

		if b.typ == "attachment" {
			att := get(raw, "attachment")
			var queued *claudePrompt
			if isStr(opt(att, "type"), "queued_command") && isStr(get(att, "commandMode"), "prompt") {
				switch prompt := get(att, "prompt").(type) {
				case string:
					queued = &claudePrompt{text: prompt}
				case []any:
					sp := splitPromptContent(prompt)
					queued = &sp
				}
			}
			if queued != nil && strings.Contains(queued.text, "<task-notification>") {
				if n := parseTaskNotification(queued.text, b.stamp()); n != nil {
					p.name(cur, n)
					p.push(n, offset)
					i++
					continue
				}
			}
			if queued != nil && (jscompat.Trim(queued.text) != "" || len(queued.images) > 0) {
				when := b.timestamp
				if s, ok := get(att, "timestamp").(string); ok {
					when = s
				}
				e := obj("id", unsetID, "type", "user-message", "timestamp", stamp(when), "text", queued.text)
				if len(queued.images) > 0 {
					e.Set("images", queued.images)
				}
				e.Set("queued", true)
				p.name(cur, e)
				p.push(e, offset)
				i++
				continue
			}
			if s, ok := opt(att, "planFilePath").(string); ok && isStr(opt(att, "type"), "plan_mode") {
				planFilePath = s
			}
			if planContent, ok := opt(att, "planContent").(string); ok && isStr(opt(att, "type"), "plan_file_reference") && planContent != "" {
				ts := b.stamp()
				filePath := any(undef)
				if s, ok := get(att, "planFilePath").(string); ok {
					filePath = s
				}
				plan := obj("id", unsetID, "type", "plan", "timestamp", ts, "toolUseId", "", "markdown", planContent, "filePath", filePath,
					"status", "success")
				p.name(cur, plan)
				p.push(plan, offset)
				if s, ok := filePath.(string); ok {
					planFilePath = s
				}
			}
		}

		if b.typ == "system" && !b.isSidechain && isStr(get(raw, "subtype"), "local_command") {
			var parsed []*jsvalue.Object
			if text, ok := get(raw, "content").(string); ok {
				parsed, _ = parseCommandProtocol(text, b.stamp())
			}
			p.name(cur, parsed...)
			p.pushCommands(parsed, offset)
			if hasType(parsed, "slash-command") {
				running = &claudeRunningCommand{uuid: b.uuid}
			}
			i++
			continue
		}

		// A subagent's system line belongs to its group wherever a read begins: its stop is not the turn's.
		if b.typ == "system" && !b.isSidechain {
			if notice := systemNotice(raw.(*jsvalue.Object)); notice != nil {
				e := obj(append([]any{"id", unsetID, "timestamp", b.stamp()}, notice...)...)
				p.name(cur, e)
				p.push(e, offset)
			} else {
				e := obj("id", unsetID, "type", "turn-end", "timestamp", b.stamp())
				p.name(cur, e)
				p.push(e, offset)
			}
			i++
			continue
		}

		if b.isSidechain {
			var group []claudeRaw
			for i < len(raws) && raws[i].base.isSidechain {
				group = append(group, raws[i])
				i++
			}
			p.push(p.agentGroup(group, hint), offset)
			hint = nil
			continue
		}

		if running != nil && b.typ == "user" && isExpandedCommandPrompt(raw, b, running) {
			i++
			continue
		}
		if b.typ == "assistant" {
			running = nil
		}

		parsed := p.single(raw, b)
		p.name(cur, parsed...)

		if b.typ == "user" && (hasType(parsed, "slash-command") || hasType(parsed, "command-output")) {
			p.pushCommands(parsed, offset)
			if hasType(parsed, "slash-command") {
				text, isText := opt(get(raw, "message"), "content").(string)
				running = &claudeRunningCommand{uuid: b.uuid, expandsPrompt: isText && commandRunRE.MatchString(text)}
			}
			i++
			continue
		}
		if b.typ == "user" && get(raw, "isMeta") != true {
			running = nil
		}

		for _, e := range parsed {
			if typeOf(e) == "tool-call" && isStr(get(e, "toolName"), "Agent") {
				if msg, ok := zAssistantEntry(raw); ok {
					for _, c := range msg.content {
						if isStr(get(c, "type"), "tool_use") && c.Has("name") && isStr(get(c, "name"), "Agent") {
							input := any(jsvalue.NewObject())
							if c.Has("input") {
								input = get(c, "input")
							}
							hint = &claudeAgentHint{
								agentType:   jsString(coalesce(coalesce(get(input, "subagent_type"), get(input, "type")), "Agent")),
								description: sliceJS(jsString(coalesce(coalesce(get(input, "description"), get(input, "prompt")), "")), 100),
							}
						}
					}
				}
			}
		}
		for _, e := range parsed {
			p.push(e, offset)
		}
		i++
	}

	if planFilePath != "" && !hasType(p.entries, "plan") {
		for j := len(p.entries) - 1; j >= 0; j-- {
			e := p.entries[j]
			newString, _ := opt(get(e, "diff"), "newString").(string)
			if typeOf(e) == "tool-call" && isStr(get(e, "toolName"), "Write") && isStr(get(e, "filePath"), planFilePath) && newString != "" {
				p.entries[j] = obj("id", get(e, "id"), "type", "plan", "timestamp", get(e, "timestamp"), "toolUseId", get(e, "toolUseId"),
					"markdown", newString, "filePath", planFilePath, "status", get(e, "status"))
				break
			}
		}
	}

	if running != nil {
		r.carry = Carry{command: running.uuid, expandsPrompt: running.expandsPrompt, set: true}
	}
	entries, offsets := dropDeliveredQueuedPrompts(p.entries, p.offsets)
	r.entries = mergeToolResults(entries)
	r.offsets = offsets
	return r
}

func hasType(entries []*jsvalue.Object, typ string) bool {
	for _, e := range entries {
		if typeOf(e) == typ {
			return true
		}
	}
	return false
}

// dropDeliveredQueuedPrompts: a waiting prompt goes once the same text shows up as delivered or as its own turn.
func dropDeliveredQueuedPrompts(entries []*jsvalue.Object, offsets []int64) ([]*jsvalue.Object, []int64) {
	removed := map[int]bool{}
	waiting := map[string][]int{}
	for i, e := range entries {
		if typeOf(e) != "user-message" {
			continue
		}
		key := pendingMatchKey(e.String("text"))
		if truthy(get(e, "pending")) && truthy(get(e, "queued")) {
			waiting[key] = append(waiting[key], i)
			continue
		}
		if w := waiting[key]; len(w) > 0 {
			removed[w[0]] = true
			waiting[key] = w[1:]
		}
	}
	if len(removed) == 0 {
		return entries, offsets
	}
	keptEntries := make([]*jsvalue.Object, 0, len(entries))
	keptOffsets := make([]int64, 0, len(offsets))
	for i, e := range entries {
		if !removed[i] {
			keptEntries = append(keptEntries, e)
		}
	}
	for i, o := range offsets {
		if !removed[i] {
			keptOffsets = append(keptOffsets, o)
		}
	}
	return keptEntries, keptOffsets
}
