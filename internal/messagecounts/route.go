package messagecounts

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unicode/utf8"

	"harnessly/internal/gate"
	"harnessly/internal/jscompat"
	"harnessly/internal/nodejson"
)

const (
	cacheLimit = 100
	headLen    = 512
	readChunk  = 4 << 20
)

// Route answers GET /api/timeline/message-counts?jsonlPath=. A transcript only grows, so each file's count is kept
// with the offset it reached, and a request reads only what was appended since; a file that shrank, was replaced or
// whose start changed is counted again from the start.
type Route struct {
	// Home holds the transcripts the route may read (.claude/projects and .codex/sessions under it).
	Home string
	// Authorized is the old API's own check: a page session or the cli-token.
	Authorized func(*http.Request) bool

	mu    sync.Mutex
	order []string
	files map[string]*file
}

// file is what is known of one transcript.
type file struct {
	mu     sync.Mutex
	ino    uint64
	offset int64 // up to the end of the last whole line counted
	head   []byte
	count  *counter
	// mtime and size of the last answer, and the answer itself.
	mtime, size int64
	answer      *Counts
}

func unauthorized() (int, any, http.Header) {
	return http.StatusUnauthorized, gate.Raw{ContentType: "application/json", Body: []byte(`{"error":"Unauthorized"}`)}, nil
}

func errorBody(msg string) map[string]string { return map[string]string{"error": msg} }

// Answer is the whole answer, refusals included.
func (rt *Route) Answer(r *http.Request) (int, any, http.Header) {
	if !rt.Authorized(r) {
		return unauthorized()
	}
	if r.Method != http.MethodGet {
		return nodejson.Answer(r, http.StatusMethodNotAllowed, errorBody("Method not allowed"), http.Header{"Allow": {"GET"}})
	}
	p := jscompat.Param{Values: jscompat.Query(r.URL.RawQuery)["jsonlPath"]}
	if !p.Truthy() {
		return nodejson.Answer(r, http.StatusBadRequest, errorBody("jsonlPath parameter required"), nil)
	}
	if p.Array() { // path.resolve threw on the array
		return http.StatusInternalServerError, gate.Raw{ContentType: "text/plain", Body: []byte("Internal Server Error")}, nil
	}
	path := p.Values[0]
	if !AllowedJsonl(rt.Home, path) {
		return nodejson.Answer(r, http.StatusForbidden, errorBody("Path not allowed"), nil)
	}
	return nodejson.Answer(r, http.StatusOK, rt.Count(path).Value(), nil)
}

// AllowedJsonl: a .jsonl under Claude Code's projects or Codex's sessions in the home, the path resolved first.
func AllowedJsonl(home, p string) bool {
	abs, err := filepath.Abs(p)
	if err != nil || !strings.HasSuffix(abs, ".jsonl") {
		return false
	}
	return strings.HasPrefix(abs, filepath.Join(home, ".claude", "projects")+"/") ||
		strings.HasPrefix(abs, filepath.Join(home, ".codex", "sessions")+"/")
}

func (rt *Route) entry(path string) *file {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.files == nil {
		rt.files = map[string]*file{}
	}
	f := rt.files[path]
	if f == nil {
		f = &file{}
		rt.files[path] = f
	}
	for i, k := range rt.order {
		if k == path {
			rt.order = append(rt.order[:i], rt.order[i+1:]...)
			break
		}
	}
	rt.order = append(rt.order, path)
	for len(rt.order) > cacheLimit {
		delete(rt.files, rt.order[0])
		rt.order = rt.order[1:]
	}
	return f
}

// Count is the transcript's counts; zeros when it cannot be read.
func (rt *Route) Count(path string) Counts {
	st, err := os.Stat(path)
	if err != nil {
		return Zero()
	}
	f := rt.entry(path)
	f.mu.Lock()
	defer f.mu.Unlock()
	mtime := st.ModTime().UnixMilli()
	if f.answer != nil && f.mtime == mtime && f.size == st.Size() {
		return *f.answer
	}
	f.answer = nil
	c, ok := f.advance(path, st)
	if !ok {
		f.count = nil
		return Zero()
	}
	if c.threw {
		return Zero()
	}
	out := c.counts()
	f.answer, f.mtime, f.size = &out, mtime, st.Size()
	return out
}

func inode(st os.FileInfo) uint64 {
	if s, ok := st.Sys().(*syscall.Stat_t); ok {
		return uint64(s.Ino)
	}
	return 0
}

// advance counts what the file gained and returns the count with its unfinished last line taken in too.
func (f *file) advance(path string, st os.FileInfo) (*counter, bool) {
	fd, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer fd.Close()
	size := st.Size()
	if f.count == nil || f.ino != inode(st) || size < f.offset || !f.sameHead(fd) {
		f.count, f.offset, f.head, f.ino = newCounter(), 0, nil, inode(st)
	}
	if _, err := fd.Seek(f.offset, io.SeekStart); err != nil {
		return nil, false
	}
	var carry []byte
	buf := make([]byte, readChunk)
	for f.offset+int64(len(carry)) < size {
		n, err := fd.Read(buf[:min(int64(len(buf)), size-f.offset-int64(len(carry)))])
		if n == 0 && err != nil {
			if err == io.EOF {
				break
			}
			return nil, false
		}
		data := append(carry, buf[:n]...)
		end := bytes.LastIndexByte(data, '\n')
		if end < 0 {
			carry = data
			continue
		}
		f.count.lines(decode(data[:end+1]))
		f.offset += int64(end + 1)
		carry = append([]byte(nil), data[end+1:]...)
	}
	if len(f.head) < headLen {
		f.readHead(fd)
	}
	if len(carry) == 0 {
		return f.count, true
	}
	tail := f.count.clone()
	tail.lines(decode(carry))
	return tail, true
}

func decode(b []byte) []byte {
	if utf8.Valid(b) {
		return b
	}
	return []byte(jscompat.DecodeUTF8(b))
}

func (f *file) readHead(fd *os.File) {
	n := min(int64(headLen), f.offset)
	head := make([]byte, n)
	if _, err := fd.ReadAt(head, 0); err == nil {
		f.head = head
	}
}

func (f *file) sameHead(fd *os.File) bool {
	if len(f.head) == 0 {
		return f.offset == 0
	}
	now := make([]byte, len(f.head))
	_, err := fd.ReadAt(now, 0)
	return err == nil && bytes.Equal(now, f.head)
}
