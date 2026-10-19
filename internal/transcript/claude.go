package transcript

import (
	"io"
	"os"
	"strings"

	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
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

// ParseIncremental is what the file gained after offset (parseIncremental).
func ParseIncremental(path string, offset int64, pending string) Incremental {
	var out Incremental
	if claudeCatch(func() { out = parseIncremental(path, offset, pending) }) {
		return Incremental{NewEntries: []*jsvalue.Object{}, NewOffset: offset, PendingBuffer: pending}
	}
	return out
}

// ParseContent is the entries of transcript text (parseJsonlContent); nil where the previous runtime threw.
func ParseContent(content string) []*jsvalue.Object {
	var out []*jsvalue.Object
	if claudeCatch(func() { out = parseClaude(content).entries }) {
		return nil
	}
	return out
}

// ParseContentChecked is ParseContent that tells a throw of the previous runtime's parser from an empty result.
func ParseContentChecked(content string) (entries []*jsvalue.Object, threw bool) {
	threw = claudeCatch(func() { entries = parseClaude(content).entries })
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
		content, err := readRange(path, size-read, size)
		if err != nil {
			return nil
		}
		if i := strings.IndexByte(content, '\n'); i >= 0 {
			content = content[i+1:]
		}
		return parseClaude(content).entries
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return parseClaude(claudeDecode(data)).entries
}

// readRange is the bytes [from, to) decoded; what the file no longer holds reads as zero bytes, as in a Buffer.alloc.
func readRange(path string, from, to int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, to-from)
	if _, err := f.ReadAt(buf, from); err != nil && err != io.EOF {
		return "", err
	}
	return claudeDecode(buf), nil
}

// readChunk is the whole lines of [from, to): a chunk not starting the file drops what precedes its first newline.
func readChunk(path string, from, to int64) (string, int64, error) {
	if to-from <= 0 {
		return "", from, nil
	}
	raw, err := readRange(path, from, to)
	if err != nil {
		return "", 0, err
	}
	if from == 0 {
		return raw, 0, nil
	}
	i := strings.IndexByte(raw, '\n')
	if i < 0 {
		return "", to, nil
	}
	return raw[i+1:], from + int64(i+1), nil
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
		r := parseClaude(content)
		c := r.chunk(max, 0, true)
		c.HasMore = len(r.entries) > max
		c.FileSize = size - partial
		c.Summary, c.CustomTitle = r.summary, r.customTitle
		return c
	}
	for chunkSize := int64(claudeChunk); chunkSize < size*2; chunkSize *= 2 {
		from := max64(0, size-chunkSize)
		raw, validFrom, err := readChunk(path, from, size)
		if err != nil {
			return empty
		}
		content, partial := wholeLines(raw)
		if content == "" {
			continue
		}
		r := parseClaude(content)
		if len(r.entries) >= max || from == 0 {
			c := r.chunk(max, validFrom, from == 0)
			c.FileSize = size - partial
			c.Summary, c.CustomTitle = r.summary, r.customTitle
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
		content, validFrom, err := readChunk(path, from, beforeByte)
		if err != nil {
			return empty
		}
		if content != "" {
			r := parseClaude(content)
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

func parseIncremental(path string, offset int64, pending string) Incremental {
	keep := Incremental{NewEntries: []*jsvalue.Object{}, NewOffset: offset, PendingBuffer: pending}
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
		return Incremental{NewEntries: parseClaude(claudeDecode(data)).entries, NewOffset: size}
	}
	if offset >= size {
		f.Close()
		return keep
	}
	buf := make([]byte, size-offset)
	_, err = f.ReadAt(buf, offset)
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
	return Incremental{NewEntries: parseClaude(strings.Join(segments, "\n")).entries, NewOffset: size, PendingBuffer: newPending}
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
}

type claudeParse struct {
	entries []*jsvalue.Object
	offsets []int64
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

func parseClaude(content string) claudeResult {
	var r claudeResult
	var raws []claudeRaw
	var pos int64
	for _, line := range strings.Split(content, "\n") {
		offset := pos
		pos += int64(len(line)) + 1
		if jscompat.Trim(line) == "" {
			continue
		}
		if !r.firstPass(line, offset, &raws) {
			r.errorCount++
		}
	}

	p := &claudeParse{}
	var hint *claudeAgentHint
	var running *claudeRunningCommand
	planFilePath := ""
	for i := 0; i < len(raws); {
		b, raw, offset := raws[i].base, raws[i].raw, raws[i].offset

		if b.typ == "queue-operation" {
			text, isText := get(raw, "content").(string)
			if isStr(get(raw, "operation"), "enqueue") && isText && jscompat.Trim(text) != "" &&
				(!strings.HasPrefix(trimStart(text), "<") || isPeerMessage(text)) && !strings.HasPrefix(trimStart(text), claudeDeliveryNotice) {
				e := obj("id", NewID(), "type", "user-message", "timestamp", b.stamp(), "text", dropHopChain(text))
				if !isHarnessNotice(text) {
					e.Set("queued", true)
					e.Set("pending", true)
				}
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
					p.push(n, offset)
					i++
					continue
				}
			}
			if queued != nil && (jscompat.Trim(queued.text) != "" || len(queued.images) > 0) {
				id := NewID()
				when := b.timestamp
				if s, ok := get(att, "timestamp").(string); ok {
					when = s
				}
				e := obj("id", id, "type", "user-message", "timestamp", stamp(when), "text", queued.text)
				if len(queued.images) > 0 {
					e.Set("images", queued.images)
				}
				e.Set("queued", true)
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
				p.push(obj("id", NewID(), "type", "plan", "timestamp", ts, "toolUseId", "", "markdown", planContent, "filePath", filePath,
					"status", "success"), offset)
				if s, ok := filePath.(string); ok {
					planFilePath = s
				}
			}
		}

		if b.typ == "system" && isStr(get(raw, "subtype"), "local_command") {
			var parsed []*jsvalue.Object
			if text, ok := get(raw, "content").(string); ok {
				parsed, _ = parseCommandProtocol(text, b.stamp())
			}
			p.pushCommands(parsed, offset)
			if hasType(parsed, "slash-command") {
				running = &claudeRunningCommand{uuid: b.uuid}
			}
			i++
			continue
		}

		if b.typ == "system" {
			if notice := systemNotice(raw.(*jsvalue.Object)); notice != nil {
				e := obj(append([]any{"id", NewID(), "timestamp", b.stamp()}, notice...)...)
				p.push(e, offset)
			} else {
				p.push(obj("id", NewID(), "type", "turn-end", "timestamp", b.stamp()), offset)
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
