package economics

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/nodejson"
)

const (
	defaultChunk    = 4 << 20
	persistDebounce = 30 * time.Second
)

// Store keeps the cursors of the transcripts read so far.
type Store struct {
	// Dir holds the cursor files (<data>/economics).
	Dir string
	// ChunkBytes is the most read at once (4 MB when 0).
	ChunkBytes int

	mu       sync.Mutex
	states   map[string]*State
	inflight map[string]*refresh
	dirty    map[string]bool
	timer    *time.Timer
}

type refresh struct {
	done  chan struct{}
	state *State
	err   error
}

// File is the cursor file of a transcript.
func (s *Store) File(path string) string {
	sum := sha1.Sum([]byte(path))
	return filepath.Join(s.Dir, hex.EncodeToString(sum[:])+".json")
}

func (s *Store) loadPersisted(path, provider string) *State {
	if raw, err := os.ReadFile(s.File(path)); err == nil {
		if st := stateFromJSON(raw, path, provider); st != nil {
			return st
		}
	}
	return NewState(path, provider)
}

// Refresh brings one transcript's counters up to date; concurrent callers share one read. Nil (and no error) when the
// file does not exist. The state returned is not changed afterwards.
func (s *Store) Refresh(path, provider string) (*State, error) {
	s.mu.Lock()
	if s.inflight == nil {
		s.inflight, s.states, s.dirty = map[string]*refresh{}, map[string]*State{}, map[string]bool{}
	}
	if r, ok := s.inflight[path]; ok {
		s.mu.Unlock()
		<-r.done
		return r.state, r.err
	}
	r := &refresh{done: make(chan struct{})}
	s.inflight[path] = r
	known := s.states[path]
	s.mu.Unlock()

	if known == nil {
		known = s.loadPersisted(path, provider)
	}
	next, err := s.advance(known)
	s.mu.Lock()
	switch {
	case err != nil:
	case next == nil:
		delete(s.states, path)
	default:
		if next != known {
			s.schedulePersist(path)
		}
		s.states[path] = next
	}
	delete(s.inflight, path)
	s.mu.Unlock()
	r.state, r.err = next, err
	close(r.done)
	return next, err
}

// advance reads what was appended since the cursor, in chunks; a truncated or replaced file starts over. It returns
// the cursor itself when nothing was appended, else a new one.
func (s *Store) advance(st *State) (*State, error) {
	fi, err := os.Stat(st.Path)
	if err != nil {
		return nil, nil
	}
	dev, ino := devIno(fi)
	cur := st
	if ino != cur.Ino || dev != cur.Dev || fi.Size() < cur.Offset {
		cur = NewState(st.Path, st.Provider)
		cur.Dev, cur.Ino = dev, ino
	}
	if fi.Size() == cur.Offset {
		return cur, nil
	}
	if cur == st {
		cur = st.clone()
	}
	f, err := os.Open(cur.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	chunk := s.ChunkBytes
	if chunk <= 0 {
		chunk = defaultChunk
	}
	// A fresh decoder per read, as before: a sequence cut at the end of the file is lost, a BOM where the read starts
	// is dropped.
	dec := jscompat.UTF8Decoder{StripBOM: true}
	buf := make([]byte, min(int64(chunk), fi.Size()-cur.Offset))
	for cur.Offset < fi.Size() {
		n, err := f.ReadAt(buf[:min(int64(len(buf)), fi.Size()-cur.Offset)], cur.Offset)
		if n == 0 {
			if err != nil && !errors.Is(err, io.EOF) {
				return nil, err
			}
			break
		}
		cur.Offset += int64(n)
		cur.consume(dec.Decode(buf[:n], true))
	}
	return cur, nil
}

func (s *Store) schedulePersist(path string) {
	s.dirty[path] = true
	if s.timer != nil {
		return
	}
	s.timer = time.AfterFunc(persistDebounce, func() { s.Flush() })
}

// Flush writes the cursors changed since the last flush.
func (s *Store) Flush() {
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	var states []*State
	for p := range s.dirty {
		if st := s.states[p]; st != nil {
			states = append(states, st)
		}
	}
	s.dirty = map[string]bool{}
	s.mu.Unlock()
	if len(states) == 0 {
		return
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		agent.Log("warn", "economics: cursor folder not created", agent.F{K: "err", V: err.Error()})
		return
	}
	for _, st := range states {
		target := s.File(st.Path)
		tmp := target + "." + itoa(int64(os.Getpid())) + ".tmp"
		if err := os.WriteFile(tmp, nodejson.Marshal(st), 0o600); err != nil {
			_ = os.Remove(tmp)
			continue
		}
		if err := os.Rename(tmp, target); err != nil {
			_ = os.Remove(tmp)
		}
	}
}
