package sources

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/messagecounts"
	"github.com/ly-solutions/harnessly/internal/nodejson"
	"github.com/ly-solutions/harnessly/internal/transcript"
)

const (
	maxIndexes = 64
	chunkBytes = 8 * 1024 * 1024
)

// Route answers GET <platform API>/sources?jsonlPath=&cwd=: the links and work paths of a whole conversation. The
// index of a transcript is kept and grows with the file: a request reads only the whole lines appended since the
// previous one, in the stretches the previous runtime read, so both see the same entries.
type Route struct {
	Links *Links
	// Home holds the transcripts the route may read (.claude/projects and .codex/sessions under it).
	Home string
	// Codex are the options Codex transcripts are read with.
	Codex transcript.CodexOptions
	// Authorized is the old API's own check (a page session or the cli-token), SessionOK the page session the handler
	// asks for besides.
	Authorized, SessionOK func(*http.Request) bool

	mu      sync.Mutex
	order   []string
	indexes map[string]*index
}

type index struct {
	mu        sync.Mutex
	ino       uint64
	offset    int64
	collector Collector
	mentions  Mentions
	paths     Paths
}

// Result is what the route answers for a transcript.
type Result struct {
	Links []Link
	// MRs are the merge requests the agent and the person wrote down, oldest first; they are not in Value().
	MRs       []MRMention
	WorkPaths []any
	Size      int64
}

// Value is the answer as the previous runtime wrote it.
func (r Result) Value() *jsvalue.Object {
	links := make([]any, len(r.Links))
	for i, l := range r.Links {
		links[i] = l.Value()
	}
	paths := r.WorkPaths
	if paths == nil {
		paths = []any{}
	}
	return jsvalue.NewObject("links", links, "workPaths", paths, "size", r.Size)
}

func errorBody(msg string) map[string]string { return map[string]string{"error": msg} }

// Answer is the whole answer, refusals included.
func (rt *Route) Answer(r *http.Request) (int, any, http.Header) {
	if !rt.Authorized(r) {
		return http.StatusUnauthorized, gate.Raw{ContentType: "application/json", Body: []byte(`{"error":"Unauthorized"}`)}, nil
	}
	if !rt.SessionOK(r) {
		return nodejson.Answer(r, http.StatusForbidden, errorBody("Forbidden"), nil)
	}
	if r.Method != http.MethodGet {
		return http.StatusMethodNotAllowed, nil, http.Header{"Allow": {"GET"}}
	}
	h := http.Header{"Cache-Control": {"no-store"}}
	q := jscompat.Query(r.URL.RawQuery)
	path, cwd := "", ""
	if v := q["jsonlPath"]; len(v) == 1 {
		path = v[0]
	}
	if v := q["cwd"]; len(v) == 1 && strings.HasPrefix(v[0], "/") {
		cwd = v[0]
	}
	res, found, failed := rt.Read(path, cwd)
	if failed {
		return http.StatusInternalServerError, gate.Raw{ContentType: "text/plain", Body: []byte("Internal Server Error")}, h
	}
	if !found {
		return nodejson.Answer(r, http.StatusNotFound, errorBody("Transcript not found"), h)
	}
	return nodejson.Answer(r, http.StatusOK, res.Value(), h)
}

// Read is the sources of a transcript: found is false for a path that is not an allowed transcript file, failed true
// where the previous runtime's parser threw (it answered 500).
func (rt *Route) Read(path, cwd string) (res Result, found, failed bool) {
	if path == "" || strings.Contains(path, "\x00") || !messagecounts.AllowedJsonl(rt.Home, path) {
		return Result{}, false, false
	}
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return Result{}, false, false
	}
	idx := rt.indexFor(path, st)
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if err := idx.advance(path, st.Size(), rt.parser(path)); err != nil {
		return Result{}, true, true
	}
	return Result{Links: idx.collector.List(), MRs: idx.mentions.List(), WorkPaths: idx.paths.List(cwd), Size: idx.offset}, true, false
}

func inode(st os.FileInfo) uint64 {
	if s, ok := st.Sys().(*syscall.Stat_t); ok {
		return uint64(s.Ino)
	}
	return 0
}

// indexFor is the file's index, a fresh one when the file was replaced or shrank; the most recently read are kept.
func (rt *Route) indexFor(path string, st os.FileInfo) *index {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.indexes == nil {
		rt.indexes = map[string]*index{}
	}
	idx := rt.indexes[path]
	if idx != nil {
		idx.mu.Lock()
		stale := idx.ino != inode(st) || idx.offset > st.Size()
		idx.mu.Unlock()
		if stale {
			idx = nil
		}
	}
	if idx == nil {
		idx = &index{ino: inode(st), collector: Collector{Links: rt.Links}, mentions: Mentions{Links: rt.Links}}
	}
	rt.indexes[path] = idx
	for i, k := range rt.order {
		if k == path {
			rt.order = append(rt.order[:i], rt.order[i+1:]...)
			break
		}
	}
	rt.order = append(rt.order, path)
	for len(rt.order) > maxIndexes {
		delete(rt.indexes, rt.order[0])
		rt.order = rt.order[1:]
	}
	return idx
}

type parseFunc func(content string) ([]*jsvalue.Object, bool)

func (rt *Route) parser(path string) parseFunc {
	if strings.Contains(path, "/.codex/sessions/") {
		return func(content string) ([]*jsvalue.Object, bool) {
			return transcript.ParseCodexContentChecked(content, rt.Codex)
		}
	}
	return transcript.ParseContentChecked
}

type parseError struct{}

func (parseError) Error() string { return "the transcript parser threw" }

// advance reads whole lines from the index's offset to size in bounded stretches; a line still being written is left
// for the next request, a single line longer than a stretch is skipped.
func (idx *index) advance(path string, size int64, parse parseFunc) error {
	if idx.offset >= size {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, min(chunkBytes, size-idx.offset))
	for idx.offset < size {
		length := min(int64(chunkBytes), size-idx.offset)
		n, err := f.ReadAt(buf[:length], idx.offset)
		if err != nil && err != io.EOF {
			return err
		}
		last := bytes.LastIndexByte(buf[:n], '\n')
		if last < 0 {
			if length < chunkBytes {
				return nil
			}
			idx.offset += int64(n)
			continue
		}
		entries, threw := parse(jscompat.DecodeUTF8(buf[:last+1]))
		if threw {
			return parseError{}
		}
		idx.collector.Add(entries)
		idx.mentions.Add(entries)
		idx.paths.Add(entries)
		idx.offset += int64(last + 1)
	}
	return nil
}
