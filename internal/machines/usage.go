package machines

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/pricing"
)

// The spend history of the limits forecast, from the person's Claude Code and Codex transcripts: every model call's
// API-equivalent cost. A transcript only grows, so after the first pass a refresh reads what was appended to each file
// since the last one; a file that shrank or whose start changed is read again whole.

const (
	activityRefresh = 2 * time.Minute
	// keepRows: rows older than the history plus a day can never count again (the clock only moves on).
	keepRows = (activityDays + 1) * day
)

// msgRow is one model call: its dedup key (streamed chunks and copied histories repeat it), time and usage.
type msgRow struct {
	key                                        string
	ts                                         float64
	model                                      string
	input, output, cacheRead, write5m, write1h float64
}

// transcript is what is known of one file so far.
type transcript struct {
	provider string
	size     int64
	mtime    time.Time
	offset   int64  // bytes parsed, up to the end of the last complete line
	head     []byte // its first bytes, to notice a file rewritten in place
	rows     []msgRow
	index    map[string]int // claude: row of a key
	// codex: the model of the current turn and the last cumulative total (input, cached input, cache write, output)
	model     string
	prevTotal [4]float64
	hasPrev   bool
}

const headLen = 512

func numField(v any) float64 {
	if f, ok := v.(float64); ok && !math.IsNaN(f) && !math.IsInf(f, 0) {
		return f
	}
	return 0
}

func tsSec(v any) float64 {
	s, ok := v.(string)
	if !ok {
		return 0
	}
	ms := jscompat.ParseDate(s)
	if math.IsNaN(ms) || math.IsInf(ms, 0) {
		return 0
	}
	return math.Floor(ms / 1000)
}

type claudeLine struct {
	Type              any `json:"type"`
	RequestID         any `json:"requestId"`
	UUID              any `json:"uuid"`
	Timestamp         any `json:"timestamp"`
	IsAPIErrorMessage any `json:"isApiErrorMessage"`
	Message           *struct {
		ID    any `json:"id"`
		Model any `json:"model"`
		Usage any `json:"usage"`
	} `json:"message"`
}

func keyPart(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "", false
	case string:
		return x, true
	}
	return jscompat.String(v), true
}

func (t *transcript) claudeLine(line []byte, keepFrom float64) {
	// The previous runtime skipped these record types before parsing; a line it skipped must not count here either.
	if !bytes.Contains(line, []byte(`"assistant"`)) || bytes.Contains(line, []byte(`"type":"file-history`)) ||
		bytes.Contains(line, []byte(`"type":"progress"`)) {
		return
	}
	var e claudeLine
	if json.Unmarshal(line, &e) != nil || e.Type != "assistant" || e.Message == nil {
		return
	}
	usage, _ := e.Message.Usage.(map[string]any)
	model, _ := e.Message.Model.(string)
	if !jscompat.Truthy(e.Message.Usage) || model == "" || model == "<synthetic>" || e.IsAPIErrorMessage == true {
		return
	}
	ts := tsSec(e.Timestamp)
	req, _ := e.RequestID.(string)
	id, ok := keyPart(e.Message.ID)
	if !ok {
		id, _ = keyPart(e.UUID)
	}
	key := req + ":" + id
	cache, _ := usage["cache_creation"].(map[string]any)
	cache5m := numField(cache["ephemeral_5m_input_tokens"])
	cache1h := numField(cache["ephemeral_1h_input_tokens"])
	cacheWrite := numField(usage["cache_creation_input_tokens"])
	// Older transcripts report only the total; it is the 5-minute tier.
	write5m := cacheWrite
	if cache5m != 0 || cache1h != 0 {
		write5m = cache5m
	}
	row := msgRow{key, ts, model, numField(usage["input_tokens"]), numField(usage["output_tokens"]),
		numField(usage["cache_read_input_tokens"]), write5m, cache1h}
	if i, has := t.index[key]; has {
		p := &t.rows[i]
		p.input, p.output = math.Max(p.input, row.input), math.Max(p.output, row.output)
		p.cacheRead, p.write5m, p.write1h = math.Max(p.cacheRead, row.cacheRead), math.Max(p.write5m, row.write5m), math.Max(p.write1h, row.write1h)
		return
	}
	if ts < keepFrom {
		return
	}
	if t.index == nil {
		t.index = map[string]int{}
	}
	t.index[key] = len(t.rows)
	t.rows = append(t.rows, row)
}

type codexLine struct {
	Timestamp any `json:"timestamp"`
	Type      any `json:"type"`
	Payload   any `json:"payload"`
}

func (t *transcript) codexLine(line []byte, keepFrom float64) {
	if !bytes.Contains(line, []byte(`"turn_context"`)) && !bytes.Contains(line, []byte(`"token_count"`)) {
		return
	}
	var e codexLine
	if json.Unmarshal(line, &e) != nil {
		return
	}
	payload, _ := e.Payload.(map[string]any)
	switch e.Type {
	case "turn_context":
		if m, ok := payload["model"].(string); ok {
			t.model = m
		}
		return
	case "event_msg":
	default:
		return
	}
	if payload["type"] != "token_count" {
		return
	}
	info, _ := payload["info"].(map[string]any)
	rawTotal := info["total_token_usage"]
	if !jscompat.Truthy(rawTotal) {
		return
	}
	total, _ := rawTotal.(map[string]any)
	cur := [4]float64{numField(total["input_tokens"]), numField(total["cached_input_tokens"]), numField(total["cache_write_input_tokens"]),
		numField(total["output_tokens"])}
	if t.hasPrev && cur[0] == t.prevTotal[0] && cur[3] == t.prevTotal[3] {
		return // snapshots repeat; only a growing cumulative total is new usage
	}
	d := cur
	if t.hasPrev {
		for i := range d {
			d[i] = cur[i] - t.prevTotal[i]
		}
	}
	in, cachedIn, cacheWrite, out := d[0], d[1], d[2], d[3]
	t.prevTotal, t.hasPrev = cur, true
	ts := tsSec(e.Timestamp)
	if ts < keepFrom {
		return
	}
	stamp := "undefined"
	if e.Timestamp != nil {
		stamp = jscompat.String(e.Timestamp)
	}
	model := t.model
	if model == "" {
		model = "gpt-5"
	}
	cached := math.Max(0, cachedIn)
	// Codex counts cached tokens inside the input; they are split out like Claude's.
	t.rows = append(t.rows, msgRow{
		key: stamp + ":" + jscompat.Number(cur[0]) + ":" + jscompat.Number(cur[3]),
		ts:  ts, model: model, input: math.Max(0, in-cached), output: math.Max(0, out), cacheRead: cached,
		write5m: math.Max(0, cacheWrite),
	})
}

func (t *transcript) line(line []byte, keepFrom float64) {
	if len(line) == 0 {
		return
	}
	if t.provider == "claude" {
		t.claudeLine(line, keepFrom)
	} else {
		t.codexLine(line, keepFrom)
	}
}

// clone is a copy a trailing line without its newline can be read into, keeping the transcript as it was.
func (t *transcript) clone() *transcript {
	c := *t
	c.rows = append([]msgRow(nil), t.rows...)
	if t.index != nil {
		c.index = make(map[string]int, len(t.index))
		for k, v := range t.index {
			c.index[k] = v
		}
	}
	return &c
}

// update brings the transcript up to the file; false when the file cannot be read.
func (t *transcript) update(path string, st os.FileInfo, keepFrom float64) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, min(int64(headLen), st.Size()))
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	if st.Size() < t.offset || !bytes.HasPrefix(head, t.head[:min(len(t.head), len(head))]) || len(t.head) > len(head) {
		*t = transcript{provider: t.provider}
	}
	t.head = head
	if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
		return false
	}
	r := bufio.NewReaderSize(io.LimitReader(f, st.Size()-t.offset), 256<<10)
	read := t.offset
	var long []byte // a line longer than the buffer, gathered piece by piece
	for {
		piece, err := r.ReadSlice('\n')
		read += int64(len(piece))
		if err == bufio.ErrBufferFull {
			long = append(long, piece...)
			continue
		}
		if err != nil {
			break // the rest, if any, is a line still being written
		}
		line := piece
		if long != nil {
			line = append(long, piece...)
			long = nil
		}
		t.line(bytes.TrimSuffix(line[:len(line)-1], []byte("\r")), keepFrom)
		t.offset = read
	}
	t.size, t.mtime = read, st.ModTime()
	return true
}

// rowsNow are the rows with a trailing unfinished line read too, as reading the whole file would.
func (t *transcript) rowsNow(path string, keepFrom float64) []msgRow {
	if t.size == t.offset {
		return t.rows
	}
	f, err := os.Open(path)
	if err != nil {
		return t.rows
	}
	defer f.Close()
	tail := make([]byte, t.size-t.offset)
	n, _ := f.ReadAt(tail, t.offset)
	c := t.clone()
	c.line(bytes.TrimSuffix(tail[:n], []byte("\r")), keepFrom)
	return c.rows
}

// Usage keeps the transcripts' rows and the activity built from them.
type Usage struct {
	ClaudeRoot string // <claude config>/projects
	CodexRoot  string // <codex home>/sessions
	// TZOffset (seconds east of UTC) is the zone whose weekend the usual hours know.
	TZOffset float64
	// CacheFile keeps what was read of the transcripts across restarts (none when empty): a first pass over weeks of
	// transcripts takes a while.
	CacheFile string
	Now       func() time.Time

	mu         sync.Mutex
	files      map[string]*transcript
	loaded     bool
	dirty      bool
	savedAt    time.Time
	activity   map[string]*Activity
	builtAt    time.Time
	refreshing bool
}

func (u *Usage) now() time.Time {
	if u.Now != nil {
		return u.Now()
	}
	return time.Now()
}

// walk lists the .jsonl files under root, at most maxDepth folders down; links are not followed.
func walk(root string, maxDepth int) []string {
	var out []string
	var visit func(dir string, depth int)
	visit = func(dir string, depth int) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			full := filepath.Join(dir, e.Name())
			switch {
			case e.IsDir():
				if depth < maxDepth {
					visit(full, depth+1)
				}
			case e.Type().IsRegular() && strings.HasSuffix(full, ".jsonl"):
				out = append(out, full)
			}
		}
	}
	visit(root, 0)
	return out
}

// Activity is the spend history per provider ("claude", "codex"). It never waits: the transcripts are read again in
// the background at most every two minutes, and until the first pass is done there is none (the forecast then goes by
// the pace alone).
func (u *Usage) Activity() map[string]*Activity {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.refreshing && u.now().Sub(u.builtAt) >= activityRefresh {
		u.refreshing = true
		go func() {
			a := u.Build()
			u.mu.Lock()
			u.activity, u.builtAt, u.refreshing = a, u.now(), false
			u.mu.Unlock()
		}()
	}
	return u.activity
}

// Build reads the transcripts and builds the activity now.
func (u *Usage) Build() map[string]*Activity {
	asOf := math.Floor(float64(u.now().UnixMilli()) / 1000)
	keepFrom := asOf - keepRows
	type item struct{ path, provider string }
	var list []item
	for _, p := range walk(u.ClaudeRoot, 3) {
		list = append(list, item{p, "claude"})
	}
	for _, p := range walk(u.CodexRoot, 4) {
		list = append(list, item{p, "codex"})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].path < list[j].path })
	u.mu.Lock()
	if !u.loaded {
		u.loaded = true
		u.files = u.loadCache()
	}
	if u.files == nil {
		u.files = map[string]*transcript{}
	}
	old := u.files
	u.mu.Unlock()
	dirty := false
	files := make(map[string]*transcript, len(list))
	from := asOf - activityDays*day
	seen := map[string]bool{}
	events := map[string][][2]float64{}
	for _, it := range list {
		st, err := os.Stat(it.path)
		if err != nil || float64(st.ModTime().Unix()) < keepFrom {
			continue
		}
		t := old[it.path]
		if t == nil || t.provider != it.provider {
			t = &transcript{provider: it.provider}
		}
		if st.Size() != t.size || !st.ModTime().Equal(t.mtime) {
			if !t.update(it.path, st, keepFrom) {
				continue
			}
			dirty = true
		}
		files[it.path] = t
		for _, r := range t.rowsNow(it.path, keepFrom) {
			if r.ts < from || seen[r.key] {
				continue
			}
			seen[r.key] = true
			events[it.provider] = append(events[it.provider], [2]float64{r.ts, rowCost(it.provider, r)})
		}
	}
	u.mu.Lock()
	u.dirty = u.dirty || dirty || len(files) != len(old)
	u.files = files
	save := u.dirty && u.CacheFile != "" && u.now().Sub(u.savedAt) >= cacheEvery
	if save {
		u.dirty, u.savedAt = false, u.now()
	}
	u.mu.Unlock()
	if save { // the transcripts are changed only by Build, which runs one at a time
		u.saveCache(files)
	}
	return map[string]*Activity{"claude": BuildActivity(events["claude"], asOf, u.TZOffset), "codex": BuildActivity(events["codex"], asOf, u.TZOffset)}
}

func rowCost(provider string, r msgRow) float64 {
	if provider == "claude" {
		return pricing.ClaudeCostByFullID(r.model, r.input, r.output, r.write5m, r.write1h, r.cacheRead)
	}
	c, _ := pricing.OpenAICost(r.model, r.input+r.cacheRead, r.cacheRead, r.output)
	return c
}

// cacheEvery: what was read is written down at most this often (and after the first pass).
const cacheEvery = 10 * time.Minute

const cacheVersion = 1

type cachedRow struct {
	Key                             string
	Ts                              float64
	Model                           string
	In, Out, Read, Write5m, Write1h float64
}

type cachedFile struct {
	Provider      string
	Size, MtimeNs int64
	Offset        int64
	Head          []byte
	Rows          []cachedRow
	Model         string
	Prev          [4]float64
	HasPrev       bool
}

type usageCache struct {
	Version int
	Files   map[string]cachedFile
}

func (u *Usage) loadCache() map[string]*transcript {
	if u.CacheFile == "" {
		return nil
	}
	f, err := os.Open(u.CacheFile)
	if err != nil {
		return nil
	}
	defer f.Close()
	var c usageCache
	if gob.NewDecoder(bufio.NewReader(f)).Decode(&c) != nil || c.Version != cacheVersion {
		return nil
	}
	models := map[string]string{}
	out := make(map[string]*transcript, len(c.Files))
	for path, cf := range c.Files {
		t := &transcript{provider: cf.Provider, size: cf.Size, mtime: time.Unix(0, cf.MtimeNs), offset: cf.Offset, head: cf.Head,
			model: cf.Model, prevTotal: cf.Prev, hasPrev: cf.HasPrev, rows: make([]msgRow, len(cf.Rows))}
		for i, r := range cf.Rows {
			m, ok := models[r.Model]
			if !ok {
				m, models[r.Model] = r.Model, r.Model
			}
			t.rows[i] = msgRow{r.Key, r.Ts, m, r.In, r.Out, r.Read, r.Write5m, r.Write1h}
		}
		if t.provider == "claude" {
			t.index = make(map[string]int, len(t.rows))
			for i, r := range t.rows {
				t.index[r.key] = i
			}
		}
		out[path] = t
	}
	return out
}

func (u *Usage) saveCache(files map[string]*transcript) {
	c := usageCache{Version: cacheVersion, Files: make(map[string]cachedFile, len(files))}
	for path, t := range files {
		cf := cachedFile{Provider: t.provider, Size: t.size, MtimeNs: t.mtime.UnixNano(), Offset: t.offset, Head: t.head,
			Model: t.model, Prev: t.prevTotal, HasPrev: t.hasPrev, Rows: make([]cachedRow, len(t.rows))}
		for i, r := range t.rows {
			cf.Rows[i] = cachedRow{r.key, r.ts, r.model, r.input, r.output, r.cacheRead, r.write5m, r.write1h}
		}
		c.Files[path] = cf
	}
	tmp := u.CacheFile + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	w := bufio.NewWriter(f)
	err = gob.NewEncoder(w).Encode(c)
	if err == nil {
		err = w.Flush()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return
	}
	_ = os.Rename(tmp, u.CacheFile)
}
