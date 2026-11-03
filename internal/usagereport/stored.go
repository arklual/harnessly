package usagereport

import (
	"os"
	"sync"
	"time"
)

// AliveSuffix names the file a collector touches after each background pass (Collector.KeepFresh): the facts file
// itself is written only when a transcript changed, so its own time does not say whether anyone still keeps it.
const AliveSuffix = ".alive"

// Stored is the facts file another process keeps (the runtime's collector), read as it is: no transcript is read here.
// The machine's agent uses it so that the transcripts are parsed once per machine, not once per reader.
type Stored struct {
	Path string

	mu      sync.Mutex
	size    int64
	mtime   time.Time
	entries []Entry
}

// Alive says the keeper of the file has passed over the transcripts within maxAge.
func (s *Stored) Alive(maxAge time.Duration, now time.Time) bool {
	st, err := os.Stat(s.Path + AliveSuffix)
	return err == nil && now.Sub(st.ModTime()) <= maxAge
}

// Refresh reads the file again when its size or time changed.
func (s *Stored) Refresh() {
	st, err := os.Stat(s.Path)
	if err != nil {
		return
	}
	s.mu.Lock()
	same := st.Size() == s.size && st.ModTime().Equal(s.mtime)
	s.mu.Unlock()
	if same {
		return
	}
	// The previous read is let go first: two copies at once is what a small agent cannot hold.
	s.Forget()
	order, files := readFacts(s.Path)
	if files == nil {
		return
	}
	entries := make([]Entry, 0, len(order))
	for _, name := range order {
		f := files[name]
		entries = append(entries, Entry{Path: name, MtimeMs: f.mtimeMs, Facts: f.facts})
	}
	s.mu.Lock()
	s.size, s.mtime, s.entries = st.Size(), st.ModTime(), entries
	s.mu.Unlock()
}

// Entries are the transcripts of the last read, in the file's order.
func (s *Stored) Entries() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entries
}

// Forget drops what was read, so its memory is free while another source is used.
func (s *Stored) Forget() {
	s.mu.Lock()
	s.size, s.mtime, s.entries = 0, time.Time{}, nil
	s.mu.Unlock()
}

// KeepFresh passes over the transcripts every interval until stop closes, so the facts file stays current for its
// readers while nobody opens the report; after each pass it touches the alive file.
func (c *Collector) KeepFresh(every time.Duration, stop <-chan struct{}) {
	if c.cfg.CacheFile == "" || every <= 0 {
		return
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		c.Refresh()
		c.touchAlive()
		select {
		case <-stop:
			return
		case <-t.C:
		}
	}
}

func (c *Collector) touchAlive() {
	path := c.cfg.CacheFile + AliveSuffix
	now := time.Now()
	if err := os.Chtimes(path, now, now); err == nil {
		return
	}
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		_ = f.Close()
	}
}
