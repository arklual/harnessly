package preview

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"harnessly/internal/preview/devtools"
)

var jsNumber = devtools.JSNumber

// Immutable static files of a stand, kept on disk: a second open, or another slot of the same person, does not fetch
// them from the stand again. One entry is the upstream's answer as it came (coding included); the rewrite and the
// bridge happen on the way out, so a hit is delivered exactly like a miss. The key holds the person: two people on one
// runtime never read each other's files.

const (
	DefaultMaxTotalBytes = 200 * 1024 * 1024
	DefaultMaxFileBytes  = 20 * 1024 * 1024
	minMaxAgeS           = 24 * 3600
	maxTTLS              = 30 * 24 * 3600
)

var (
	staticTypes  = regexp.MustCompile(`(?i)^(text/(css|javascript)|application/(javascript|x-javascript|wasm|font-woff2?|x-font-[a-z0-9-]+|vnd\.ms-fontobject)|image/|font/)`)
	storedDenied = map[string]bool{"set-cookie": true, "set-cookie2": true, "connection": true, "keep-alive": true, "transfer-encoding": true,
		"upgrade": true, "date": true, "age": true, "content-length": true}
	entryFile  = regexp.MustCompile(`^([0-9a-f]{64})\.(json|body)$`)
	noCacheReq = regexp.MustCompile(`(?i)\bno-(cache|store)\b`)
)

func assetCacheKey(person, acceptEncoding, url string) string {
	sum := sha256.Sum256([]byte(person + "\n" + acceptEncoding + "\n" + url))
	return hex.EncodeToString(sum[:])
}

// storeVerdict is the seconds an answer may be served from disk; false when it must not be kept.
func storeVerdict(method string, status int, h Headers) (float64, bool) {
	if method != "GET" || status != 200 || h.Has("set-cookie") || h.Has("set-cookie2") {
		return 0, false
	}
	for _, name := range strings.Split(h.Get("vary"), ",") {
		if name = strings.ToLower(strings.TrimSpace(name)); name != "" && name != "accept-encoding" {
			return 0, false
		}
	}
	if !staticTypes.MatchString(h.Get("content-type")) || h.Has("content-range") {
		return 0, false
	}
	f := freshnessOf(h.Get("cache-control"))
	if f.forbidden || !f.hasMaxAge || f.maxAge <= 0 || !f.immutable && f.maxAge < minMaxAgeS {
		return 0, false
	}
	ttl := math.Min(f.maxAge, maxTTLS)
	if h.Has("age") {
		if age := jsNumber(h.Get("age")); !math.IsNaN(age) && !math.IsInf(age, 0) && age > 0 {
			ttl -= age
		}
	}
	return ttl, ttl > 0
}

// requestAllowsCache: a browser asking for a fresh copy (reload with cache off) is not answered from disk.
func requestAllowsCache(method string, h Headers) bool {
	if method != "GET" || h.Has("range") {
		return false
	}
	cc, pragma := "", ""
	if h.Has("cache-control") {
		cc = h.Get("cache-control")
	}
	if h.Has("pragma") {
		pragma = h.Get("pragma")
	}
	return !noCacheReq.MatchString(cc + "," + pragma)
}

type entryMeta struct {
	Status    int            `json:"status"`
	Headers   map[string]any `json:"headers"`
	ExpiresAt float64        `json:"expiresAt"`
	Size      int64          `json:"size"`
}

// AssetCache is one directory of kept answers, oldest use first.
type AssetCache struct {
	dir               string
	maxTotal, maxFile int64
	now               func() time.Time
	mu                sync.Mutex
	order             []string
	sizes             map[string]int64
	writing           map[string]bool
	total             int64
	opened            bool
}

func NewAssetCache(dir string, maxTotal, maxFile int64) *AssetCache {
	return &AssetCache{dir: dir, maxTotal: maxTotal, maxFile: maxFile, now: time.Now, sizes: map[string]int64{}, writing: map[string]bool{}}
}

func (c *AssetCache) bodyPath(key string) string { return filepath.Join(c.dir, key[:2], key+".body") }
func (c *AssetCache) metaPath(key string) string { return filepath.Join(c.dir, key[:2], key+".json") }

func (c *AssetCache) forget(key string) {
	if size, ok := c.sizes[key]; ok {
		c.total -= size
		delete(c.sizes, key)
		for i, k := range c.order {
			if k == key {
				c.order = append(c.order[:i], c.order[i+1:]...)
				break
			}
		}
	}
}

func (c *AssetCache) remove(key string) {
	c.forget(key)
	_ = os.Remove(c.metaPath(key))
	_ = os.Remove(c.bodyPath(key))
}

func (c *AssetCache) open() {
	if c.opened {
		return
	}
	c.opened = true
	_ = os.MkdirAll(c.dir, 0o700)
	type found struct {
		key  string
		size int64
		used time.Time
	}
	var all []found
	shards, _ := os.ReadDir(c.dir)
	for _, shard := range shards {
		dir := filepath.Join(c.dir, shard.Name())
		files, _ := os.ReadDir(dir)
		for _, f := range files {
			m := entryFile.FindStringSubmatch(f.Name())
			if m == nil {
				if strings.Contains(f.Name(), ".tmp-") {
					_ = os.Remove(filepath.Join(dir, f.Name()))
				}
				continue
			}
			if m[2] != "json" {
				continue
			}
			meta, err1 := os.Stat(filepath.Join(dir, f.Name()))
			body, err2 := os.Stat(c.bodyPath(m[1]))
			if err1 != nil || err2 != nil {
				c.remove(m[1])
				continue
			}
			all = append(all, found{m[1], body.Size(), meta.ModTime()})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].used.Before(all[j].used) })
	for _, f := range all {
		c.order = append(c.order, f.key)
		c.sizes[f.key] = f.size
		c.total += f.size
	}
}

func (c *AssetCache) evict() {
	for len(c.order) > 0 && c.total > c.maxTotal {
		c.remove(c.order[0])
	}
}

// Lookup is a kept answer: its status, headers (its length added) and body; false when there is none.
func (c *AssetCache) Lookup(key string) (int, Headers, *os.File, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.open()
	if _, ok := c.sizes[key]; !ok {
		return 0, nil, nil, false
	}
	raw, err := os.ReadFile(c.metaPath(key))
	var meta entryMeta
	if err != nil || json.Unmarshal(raw, &meta) != nil {
		c.remove(key)
		return 0, nil, nil, false
	}
	if meta.ExpiresAt <= float64(c.now().UnixMilli()) {
		c.remove(key)
		return 0, nil, nil, false
	}
	body, err := os.Open(c.bodyPath(key))
	if err != nil {
		c.remove(key)
		return 0, nil, nil, false
	}
	size := c.sizes[key]
	c.forget(key)
	c.order, c.sizes[key], c.total = append(c.order, key), size, c.total+size
	t := c.now()
	go func() { _ = os.Chtimes(c.metaPath(key), t, t) }()
	h := Headers{}
	for k, v := range meta.Headers {
		switch x := v.(type) {
		case string:
			h[k] = []string{x}
		case []any:
			for _, s := range x {
				if str, ok := s.(string); ok {
					h[k] = append(h[k], str)
				}
			}
		}
	}
	h.Set("content-length", strconv.FormatInt(meta.Size, 10))
	return meta.Status, h, body, true
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Tee wraps an upstream body: the same bytes come out, and a copy goes to disk when it ends whole and within the caps.
func (c *AssetCache) Tee(key string, status int, h Headers, ttl float64, body io.Reader) io.Reader {
	declared := int64(-1)
	if h.Has("content-length") {
		n := jsNumber(h.Get("content-length"))
		if math.IsNaN(n) || math.IsInf(n, 0) || n > float64(c.maxFile) {
			return body
		}
		declared = int64(n)
	}
	c.mu.Lock()
	c.open()
	if c.writing[key] {
		c.mu.Unlock()
		return body
	}
	c.writing[key] = true
	c.mu.Unlock()
	tmp := c.bodyPath(key) + ".tmp-" + randomHex(4)
	_ = os.MkdirAll(filepath.Dir(tmp), 0o700)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		c.done(key)
		return body
	}
	return &teeReader{c: c, key: key, status: status, h: h, ttl: ttl, r: body, f: f, tmp: tmp, declared: declared}
}

func (c *AssetCache) done(key string) {
	c.mu.Lock()
	delete(c.writing, key)
	c.mu.Unlock()
}

type teeReader struct {
	c        *AssetCache
	key      string
	status   int
	h        Headers
	ttl      float64
	r        io.Reader
	f        *os.File
	tmp      string
	size     int64
	declared int64
}

func (t *teeReader) give() {
	if t.f != nil {
		_ = t.f.Close()
		_ = os.Remove(t.tmp)
		t.f = nil
		t.c.done(t.key)
	}
}

func (t *teeReader) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if n > 0 && t.f != nil {
		t.size += int64(n)
		if t.size > t.c.maxFile {
			t.give()
		} else if _, werr := t.f.Write(p[:n]); werr != nil {
			t.give()
		}
	}
	if err == io.EOF && t.f != nil {
		t.commit()
	} else if err != nil {
		t.give()
	}
	return n, err
}

// Abandon drops a copy whose body did not end whole.
func (t *teeReader) Abandon() { t.give() }

func (t *teeReader) commit() {
	f := t.f
	t.f = nil
	defer t.c.done(t.key)
	if t.declared >= 0 && t.declared != t.size || f.Close() != nil {
		_ = os.Remove(t.tmp)
		return
	}
	stored := map[string]any{}
	for k, v := range t.h {
		if storedDenied[k] {
			continue
		}
		if k == "set-cookie" {
			list := make([]any, len(v))
			for i, s := range v {
				list[i] = s
			}
			stored[k] = list
			continue
		}
		stored[k] = strings.Join(v, ", ")
	}
	c := t.c
	meta := entryMeta{Status: t.status, Headers: stored, ExpiresAt: float64(c.now().UnixMilli()) + t.ttl*1000, Size: t.size}
	raw, _ := json.Marshal(meta)
	metaTmp := c.metaPath(t.key) + ".tmp-" + randomHex(4)
	if os.WriteFile(metaTmp, raw, 0o600) != nil || os.Rename(t.tmp, c.bodyPath(t.key)) != nil || os.Rename(metaTmp, c.metaPath(t.key)) != nil {
		_ = os.Remove(t.tmp)
		_ = os.Remove(metaTmp)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.forget(t.key)
	c.order, c.sizes[t.key], c.total = append(c.order, t.key), t.size, c.total+t.size
	c.evict()
}
