// Package shots lists the pictures a Claude Code agent and its subagents looked at: the image files it Read, and the
// ones a tool such as a browser's screenshot saved. They come from the transcripts only (the main one and the
// subagents' beside it), read once from the tail and then by what each gained, as internal/sessionmeta does, so a
// busy agent's every write costs its new lines. The list holds paths the agent really touched and nothing else: a
// picture is served only by an id of this list, never by a path a caller names.
package shots

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/transcriptindex"
	"github.com/ly-solutions/harnessly/internal/transcriptindex/tally"
)

const (
	// Max is how many pictures the list holds.
	Max = 30
	// MaxBytes is the largest picture served.
	MaxBytes = 12 << 20

	mainTail = 8 << 20
	subTail  = 2 << 20
	// coldSubs is how many subagent transcripts, the most recently written first, a cold read looks into.
	coldSubs = 12
)

// Shot is one picture: the file the agent looked at, when, and the subagent that did (its description; "" for the
// agent itself).
type Shot struct {
	ID    string
	Path  string
	At    int64 // epoch milliseconds
	Agent string
}

// Name is the file's name.
func (s Shot) Name() string { return filepath.Base(s.Path) }

var (
	extRE   = regexp.MustCompile(`(?i)\.(?:png|jpe?g|gif|webp)$`)
	linkRE  = regexp.MustCompile(`\]\(([^)\s]+?\.(?i:png|jpe?g|gif|webp))\)`)
	idMark  = []byte(`"tool_use_id":"`)
	hasUse  = []byte(`"tool_use"`)
	hasRes  = []byte(`"tool_result"`)
	isError = []byte(`"is_error":true`)
	exts    = [][]byte{[]byte(".png"), []byte(".jpg"), []byte(".jpeg"), []byte(".gif"), []byte(".webp"),
		[]byte(".PNG"), []byte(".JPG"), []byte(".JPEG"), []byte(".WEBP")}
)

// IsImage says whether a path names a picture the list takes.
func IsImage(path string) bool { return extRE.MatchString(path) }

// ID is the id of a picture: its path and the time it was looked at, so a file written over again and read again is a
// new picture to a page that cached the old one.
func ID(path string, at int64) string {
	sum := sha256.Sum256([]byte(path + "\x00" + strconv.FormatInt(at, 10)))
	return hex.EncodeToString(sum[:6])
}

// pending is a tool call that may leave a picture, waiting for its result.
type pending struct {
	path   string // the file a Read named, else the file a screenshot tool was asked to write ("" when it chose)
	reads  bool
	at     int64
	cwd    string
	screen bool
}

// scan is what one transcript has shown so far.
type scan struct {
	agent string
	wait  map[string]pending
	found []Shot // oldest first
}

type line struct {
	Timestamp string `json:"timestamp"`
	Cwd       string `json:"cwd"`
	Message   struct {
		Content any `json:"content"`
	} `json:"message"`
}

func millis(ts string) int64 {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

func resolve(cwd, p string) string {
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) {
		if cwd == "" {
			return ""
		}
		p = filepath.Join(cwd, p)
	}
	return filepath.Clean(p)
}

func screenshotTool(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "screenshot") || strings.Contains(n, "screencast")
}

func hasExt(raw []byte) bool {
	for _, e := range exts {
		if bytes.Contains(raw, e) {
			return true
		}
	}
	return false
}

// feed takes the lines of a chunk in order.
func (s *scan) feed(raw []byte) {
	switch {
	case bytes.Contains(raw, hasUse) && (hasExt(raw) || bytes.Contains(raw, []byte("creenshot")) || bytes.Contains(raw, []byte("creencast"))):
		s.use(raw)
	case len(s.wait) > 0 && bytes.Contains(raw, hasRes):
		s.result(raw)
	}
}

func (s *scan) use(raw []byte) {
	var l line
	tally.Decode()
	if json.Unmarshal(raw, &l) != nil {
		return
	}
	s.useLine(l)
}

// feedLine is feed of a line the index read: its decode of the line, shared with the transcript's other readers,
// serves where the scan would decode the line itself.
func (s *scan) feedLine(l *transcriptindex.Line) {
	raw := l.Raw
	switch {
	case bytes.Contains(raw, hasUse) && (hasExt(raw) || bytes.Contains(raw, []byte("creenshot")) || bytes.Contains(raw, []byte("creencast"))):
		f := l.Fields()
		if f == nil {
			s.use(raw) // the index could not decode it: the scan's narrower read may
			return
		}
		var decoded line
		switch ts := f.Timestamp.(type) {
		case nil:
		case string:
			decoded.Timestamp = ts
		default:
			return // the scan's own decode refuses a time that is not text
		}
		decoded.Cwd, decoded.Message.Content = f.Cwd, f.Message.Content
		s.useLine(decoded)
	case len(s.wait) > 0 && bytes.Contains(raw, hasRes):
		s.result(raw)
	}
}

// useLine takes the tool calls of a decoded line that may leave a picture.
func (s *scan) useLine(l line) {
	blocks, _ := l.Message.Content.([]any)
	at := millis(l.Timestamp)
	for _, b := range blocks {
		block, _ := b.(map[string]any)
		if block["type"] != "tool_use" {
			continue
		}
		id, _ := block["id"].(string)
		name, _ := block["name"].(string)
		input, _ := block["input"].(map[string]any)
		if id == "" {
			continue
		}
		switch {
		case name == "Read":
			if p, _ := input["file_path"].(string); IsImage(p) && filepath.IsAbs(p) {
				s.wait[id] = pending{path: filepath.Clean(p), reads: true, at: at}
			}
		case screenshotTool(name):
			p, _ := input["filename"].(string)
			if p == "" {
				p, _ = input["path"].(string)
			}
			s.wait[id] = pending{path: resolve(l.Cwd, p), at: at, cwd: l.Cwd, screen: true}
		}
	}
}

func (s *scan) result(raw []byte) {
	i := bytes.Index(raw, idMark)
	if i < 0 {
		return
	}
	rest := raw[i+len(idMark):]
	end := bytes.IndexByte(rest, '"')
	if end < 0 {
		return
	}
	id := string(rest[:end])
	w, ok := s.wait[id]
	if !ok {
		return
	}
	delete(s.wait, id)
	if bytes.Contains(raw, isError) {
		return
	}
	if w.reads {
		s.add(w.path, w.at)
		return
	}
	// A screenshot tool names the file it wrote in its answer, relative to the folder the agent works in.
	found := false
	for _, m := range linkRE.FindAllSubmatch(raw, -1) {
		found = s.add(resolve(w.cwd, string(m[1])), w.at) || found
	}
	if !found && w.path != "" && IsImage(w.path) {
		s.add(w.path, w.at)
	}
}

func (s *scan) add(path string, at int64) bool {
	if path == "" || !IsImage(path) || !filepath.IsAbs(path) {
		return false
	}
	// The same file looked at again is the newer look only.
	s.found = slices.DeleteFunc(s.found, func(x Shot) bool { return x.Path == path })
	s.found = append(s.found, Shot{ID: ID(path, at), Path: path, At: at, Agent: s.agent})
	if len(s.found) > Max {
		s.found = s.found[len(s.found)-Max:]
	}
	return true
}

type cursor struct {
	// mu guards the cursor when the reader takes its lines from the index (Reader.Index), where the index's looks
	// fold lines into it beside the reader's own reads; sub is its place in the index then.
	mu  sync.Mutex
	sub *transcriptindex.Sub

	ino    uint64
	offset int64
	mtime  time.Time
	size   int64
	scan   *scan
}

// Reader keeps one cursor per transcript. Reads of one conversation take turns.
type Reader struct {
	// Index, when set, reads what a transcript gains for the reader and for the transcript's other readers at once:
	// the reader reads a transcript's tail itself and takes the lines after it from the index. nil has it read every
	// transcript itself.
	Index *transcriptindex.Index

	// mu guards cursors, and without an index the cursors themselves: a read holds it throughout. With an index a
	// read holds reads instead, and mu only to find a cursor.
	mu      sync.Mutex
	reads   sync.Mutex
	cursors map[string]*cursor
}

// Forget drops the cursors of a transcript and of the subagents beside it.
func (r *Reader) Forget(transcript string) {
	r.mu.Lock()
	dir := subDir(transcript) + string(filepath.Separator)
	var gone []*cursor
	for k, c := range r.cursors {
		if k == transcript || strings.HasPrefix(k, dir) {
			delete(r.cursors, k)
			gone = append(gone, c)
		}
	}
	r.mu.Unlock()
	if r.Index == nil {
		return
	}
	for _, c := range gone {
		c.mu.Lock()
		sub := c.sub
		c.sub = nil
		c.mu.Unlock()
		if sub != nil {
			sub.Close()
		}
	}
}

// Cursors is how many files (transcripts and their subagents') the reader keeps a cursor of.
func (r *Reader) Cursors() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.cursors)
}

func subDir(transcript string) string {
	return filepath.Join(strings.TrimSuffix(transcript, ".jsonl"), "subagents")
}

func inode(st os.FileInfo) uint64 {
	if s, ok := st.Sys().(*syscall.Stat_t); ok {
		return uint64(s.Ino)
	}
	return 0
}

// Read is the pictures of a transcript and of its subagents', newest first, at most Max.
func (r *Reader) Read(transcript string) []Shot {
	advance, known := r.advance, func(path string) bool { return r.cursors[path] != nil }
	if r.Index == nil {
		r.mu.Lock()
		defer r.mu.Unlock()
	} else {
		r.reads.Lock()
		defer r.reads.Unlock()
		advance, known = r.advanceIndexed, func(path string) bool {
			r.mu.Lock()
			defer r.mu.Unlock()
			return r.cursors[path] != nil
		}
		r.mu.Lock()
	}
	if r.cursors == nil {
		r.cursors = map[string]*cursor{}
	}
	if r.Index != nil {
		r.mu.Unlock()
	}
	all := advance(transcript, "", mainTail)
	type sub struct {
		path string
		mod  time.Time
	}
	var subs []sub
	dir := subDir(transcript)
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			name := e.Name()
			if !strings.HasPrefix(name, "agent-") || !strings.HasSuffix(name, ".jsonl") {
				continue
			}
			if info, err := e.Info(); err == nil {
				subs = append(subs, sub{filepath.Join(dir, name), info.ModTime()})
			}
		}
	}
	slices.SortFunc(subs, func(a, b sub) int { return b.mod.Compare(a.mod) })
	cold := 0
	for _, s := range subs {
		if !known(s.path) {
			if cold >= coldSubs {
				continue
			}
			cold++
		}
		all = append(all, advance(s.path, label(s.path), subTail)...)
	}
	slices.SortStableFunc(all, func(a, b Shot) int {
		switch {
		case a.At != b.At:
			return int(b.At - a.At)
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		}
		return 0
	})
	// One picture once, by the latest look at it.
	seen := map[string]bool{}
	out := make([]Shot, 0, min(len(all), Max))
	for _, s := range all {
		if seen[s.Path] {
			continue
		}
		seen[s.Path] = true
		if out = append(out, s); len(out) == Max {
			break
		}
	}
	return out
}

// label is what a subagent is called: its description, else its kind, from the meta file beside its transcript.
func label(file string) string {
	raw, err := os.ReadFile(strings.TrimSuffix(file, ".jsonl") + ".meta.json")
	if err != nil {
		return "agent"
	}
	var m struct {
		Description string `json:"description"`
		AgentType   string `json:"agentType"`
	}
	_ = json.Unmarshal(raw, &m)
	switch {
	case strings.TrimSpace(m.Description) != "":
		return strings.TrimSpace(m.Description)
	case m.AgentType != "":
		return m.AgentType
	}
	return "agent"
}

// advance reads what the transcript gained since the last time, and gives its pictures.
func (r *Reader) advance(path, agent string, tail int64) []Shot {
	c := r.cursors[path]
	if c == nil {
		c = &cursor{}
		r.cursors[path] = c
	}
	found, ok := c.read(path, agent, tail)
	if !ok {
		delete(r.cursors, path)
	}
	return found
}

// advanceIndexed is advance with the index: the lines the transcript gained come from the index's one read of them,
// folded into the cursor's scan as they are read (take). Where the index does not serve the cursor — it is new, the
// file was replaced, cut or is gone — the cursor reads the file itself, as without an index, and takes its place
// again.
func (r *Reader) advanceIndexed(path, agent string, tail int64) []Shot {
	r.mu.Lock()
	c := r.cursors[path]
	if c == nil {
		c = &cursor{}
		r.cursors[path] = c
	}
	r.mu.Unlock()
	c.mu.Lock()
	if c.sub == nil {
		c.sub = r.Index.Subscribe(path, "shots", transcriptindex.SinkFunc(func(b *transcriptindex.Batch) bool { return c.take(b) }))
	}
	sub := c.sub
	c.mu.Unlock()
	// The look hands the lines to take, which locks the cursor: the cursor is not held over it.
	st, _ := sub.Sync(false)
	c.mu.Lock()
	if st == transcriptindex.Fresh && c.scan != nil {
		defer c.mu.Unlock()
		return slices.Clone(c.scan.found)
	}
	found, ok := c.read(path, agent, tail)
	offset, ino := c.offset, c.ino
	if !ok && c.sub == sub {
		c.sub = nil
	}
	c.mu.Unlock()
	// The index is told with the cursor let go: its looks hold the file and then take the cursor (take).
	if !ok {
		r.mu.Lock()
		if r.cursors[path] == c {
			delete(r.cursors, path)
		}
		r.mu.Unlock()
		sub.Close()
		return nil
	}
	sub.Seat(offset, ino)
	return found
}

// take folds the lines the index read into the cursor's scan.
func (c *cursor) take(b *transcriptindex.Batch) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.scan == nil || b.From != c.offset {
		return false // not where the cursor stands: it reads for itself
	}
	b.Each(c.scan.feedLine)
	// The file's time and size are not the cursor's any more: a read of its own starts with the file as it is.
	c.offset, c.mtime, c.size = b.To, time.Time{}, 0
	return true
}

// read brings the cursor up to the file by a read of its own; false when the file is not there.
func (c *cursor) read(path, agent string, tail int64) ([]Shot, bool) {
	st, err := os.Stat(path)
	tally.Stat()
	if err != nil {
		return nil, false
	}
	if c.scan != nil && c.mtime.Equal(st.ModTime()) && c.size == st.Size() {
		return slices.Clone(c.scan.found), true
	}
	fromTail := false
	if c.scan == nil || c.ino != inode(st) || st.Size() < c.offset || st.Size()-c.offset > tail {
		c.ino, c.offset = inode(st), max(0, st.Size()-tail)
		c.scan = &scan{agent: agent, wait: map[string]pending{}}
		fromTail = c.offset > 0
	}
	chunk := readChunk(path, c.offset, st.Size())
	start := 0
	if fromTail {
		start = bytes.IndexByte(chunk, '\n') + 1 // the tail starts inside a line
	}
	complete := bytes.LastIndexByte(chunk, '\n') + 1
	if complete > start {
		for _, raw := range bytes.Split(chunk[start:complete], []byte{'\n'}) {
			if len(raw) > 0 {
				c.scan.feed(raw)
			}
		}
	}
	c.offset += int64(complete)
	c.mtime, c.size = st.ModTime(), st.Size()
	return slices.Clone(c.scan.found), true
}

func readChunk(path string, from, to int64) []byte {
	f, err := os.Open(path)
	tally.Open()
	if err != nil {
		return nil
	}
	defer f.Close()
	buf := make([]byte, to-from)
	n, _ := f.ReadAt(buf, from)
	tally.Read(n)
	return buf[:n]
}

// ErrNotServed is the answer for a picture that cannot be handed over: gone, not a plain file, too big, not a picture.
var ErrNotServed = errors.New("shots: not served")

// File is a picture opened to be served.
type File struct {
	Body    *os.File
	Mime    string
	ModTime time.Time
}

// Open opens a listed picture: a plain file of at most MaxBytes whose first bytes are those of a PNG, a JPEG, a GIF or
// a WebP — the name says what the agent meant, the bytes what it is. Anything else is ErrNotServed.
func Open(path string) (File, error) {
	if !IsImage(path) {
		return File{}, ErrNotServed
	}
	f, err := os.Open(path)
	if err != nil {
		return File{}, ErrNotServed
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() == 0 || st.Size() > MaxBytes {
		f.Close()
		return File{}, ErrNotServed
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	mime := http.DetectContentType(head[:n])
	if _, err := f.Seek(0, io.SeekStart); err != nil || !slices.Contains(servedMimes, mime) {
		f.Close()
		return File{}, ErrNotServed
	}
	return File{Body: f, Mime: mime, ModTime: st.ModTime()}, nil
}

var servedMimes = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}
