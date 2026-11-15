package designaccess

import (
	"bytes"
	"compress/gzip"
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/webstatic"
)

// A design system's fonts.css carries every face of the kit inside (storefonts.go): about a megabyte that blocks the
// board's first paint. It was worked out again on every open, each font read from the store anew (2–5 s on prod), went
// out unpacked and with no validator, so a browser took the whole megabyte each time. The sheet of one
// revision is the same for everyone who may open the project: it is kept per replica, packed once, and named by its
// hash, so an open that has it answers 304.

// sheetCacheMax bounds the bytes the kept sheets take, plain and packed together.
const sheetCacheMax = 64 << 20

// sheetOut is a stylesheet as it goes out: its own bytes, packed bytes when packing pays, the hash that names them.
type sheetOut struct {
	header http.Header
	body   []byte
	gz     []byte
	etag   string
}

func newSheetOut(from http.Header, body []byte) *sheetOut {
	h := http.Header{}
	for _, k := range []string{"Content-Type", "Cache-Control", "Last-Modified"} {
		if v := from.Get(k); v != "" {
			h.Set(k, v)
		}
	}
	sum := sha256.Sum256(body)
	out := &sheetOut{header: h, body: body, etag: `"s-` + hex.EncodeToString(sum[:12]) + `"`}
	if len(body) >= 1024 {
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		_, _ = zw.Write(body)
		if zw.Close() == nil && buf.Len() < len(body) {
			out.gz = buf.Bytes()
		}
	}
	return out
}

func (o *sheetOut) size() int { return len(o.body) + len(o.gz) }

// write answers r with the sheet: 304 to a browser that holds it, packed to one that takes gzip.
func (o *sheetOut) write(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	for k, v := range o.header {
		h[k] = v
	}
	h.Set("ETag", o.etag)
	h.Add("Vary", "Accept-Encoding")
	if matchesETag(r.Header.Get("If-None-Match"), o.etag) {
		h.Del("Content-Type")
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body := o.body
	if o.gz != nil && webstatic.AcceptsGzip(r) {
		body = o.gz
		h.Set("Content-Encoding", "gzip")
	}
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func matchesETag(header, etag string) bool {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimPrefix(strings.TrimSpace(part), "W/")
		if part == "*" || part == etag {
			return true
		}
	}
	return false
}

func sheetKey(spaceID, project string, head int64, segs []string) string {
	return spaceID + "\x00" + project + "\x00" + strconv.FormatInt(head, 10) + "\x00" + strings.Join(segs, "/")
}

// sheetCache keeps the latest sheets up to max bytes; nil keeps nothing.
type sheetCache struct {
	mu    sync.Mutex
	max   int
	used  int
	order *list.List // front: the latest used
	byKey map[string]*list.Element
}

type sheetEntry struct {
	key string
	out *sheetOut
}

func newSheetCache(max int) *sheetCache {
	return &sheetCache{max: max, order: list.New(), byKey: map[string]*list.Element{}}
}

func (c *sheetCache) get(key string) *sheetOut {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.byKey[key]
	if !ok {
		return nil
	}
	c.order.MoveToFront(e)
	return e.Value.(*sheetEntry).out
}

func (c *sheetCache) put(key string, out *sheetOut) {
	if c == nil || out.size() > c.max/4 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.byKey[key]; ok {
		c.used -= e.Value.(*sheetEntry).out.size()
		c.order.Remove(e)
	}
	c.byKey[key] = c.order.PushFront(&sheetEntry{key: key, out: out})
	c.used += out.size()
	for c.used > c.max {
		last := c.order.Back()
		ent := last.Value.(*sheetEntry)
		c.order.Remove(last)
		delete(c.byKey, ent.key)
		c.used -= ent.out.size()
	}
}
