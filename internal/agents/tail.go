package agents

import (
	"bytes"
	"container/list"
	"io"
	"os"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/jscompat"
)

// ReadTail reads the last n bytes of an open file of the given size.
func ReadTail(f *os.File, size, n int64) ([]byte, error) {
	n = min(n, size)
	buf := make([]byte, n)
	got, err := f.ReadAt(buf, size-n)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf[:got], nil
}

// Lines splits text read from a transcript into its non-blank lines (JavaScript's split('\n') kept where trim() leaves
// something); invalid UTF-8 reads as U+FFFD as Node's decoder makes it.
func Lines(b []byte) [][]byte {
	if !utf8.Valid(b) {
		b = bytes.ToValidUTF8(b, []byte("�"))
	}
	var out [][]byte
	for len(b) > 0 {
		i := bytes.IndexByte(b, '\n')
		var line []byte
		if i < 0 {
			line, b = b, nil
		} else {
			line, b = b[:i], b[i+1:]
		}
		if !blank(line) {
			out = append(out, line)
		}
	}
	return out
}

func blank(line []byte) bool {
	for _, r := range string(line) {
		if !jscompat.IsSpace(r) {
			return false
		}
	}
	return true
}

// MillisSince is the age of t in milliseconds as Date.now() - mtimeMs counts it.
func MillisSince(now, t time.Time) float64 {
	return float64(now.Sub(t).Nanoseconds()) / 1e6
}

// LRU is a small cache keyed by path: insertion order is eviction order and a hit moves the entry to the end.
type LRU[V any] struct {
	Max int

	mu    sync.Mutex
	order *list.List
	items map[string]*list.Element
}

type lruEntry[V any] struct {
	key string
	val V
}

// Get is the cached value of key; a hit refreshes its place.
func (c *LRU[V]) Get(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var zero V
	if c.items == nil {
		return zero, false
	}
	el, ok := c.items[key]
	if !ok {
		return zero, false
	}
	c.order.MoveToBack(el)
	return el.Value.(*lruEntry[V]).val, true
}

// Put stores val under key, dropping the oldest entry when full.
func (c *LRU[V]) Put(key string, val V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.items == nil {
		c.items = map[string]*list.Element{}
		c.order = list.New()
	}
	if el, ok := c.items[key]; ok {
		el.Value.(*lruEntry[V]).val = val
		c.order.MoveToBack(el)
		return
	}
	if c.Max > 0 && c.order.Len() >= c.Max {
		oldest := c.order.Front()
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*lruEntry[V]).key)
	}
	c.items[key] = c.order.PushBack(&lruEntry[V]{key: key, val: val})
}

// Len is the number of cached entries.
func (c *LRU[V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.order == nil {
		return 0
	}
	return c.order.Len()
}
