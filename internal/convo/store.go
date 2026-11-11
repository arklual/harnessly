package convo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/outbox"
	"github.com/ly-solutions/harnessly/internal/safego"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Store keeps the runtime's v2 state under <data>/v2: one file per conversation, the op answers, the projects.
// Every write is the whole last state, through a temporary file, fsync and rename: a crash leaves the old file or
// the new one, never half of one.
type Store struct {
	dir string
	// write puts a file on the disk; a test makes it fail.
	write func(path string, raw []byte) error

	// A conversation's file is written whole by a writer of its own once Async is on: the actor marshals its state
	// and goes on, and a disk that is slow to sync holds up no mail.
	mu     sync.Mutex
	async  bool
	queued map[string][]byte
	// writingPath and writingRaw are the file the writer is putting down now.
	writingPath string
	writingRaw  []byte
	// fails counts a file's failed writes in a row, retryAt is when it is tried again.
	fails   map[string]int
	retryAt map[string]time.Time
	// changed is closed and made anew after every write the writer tried: what a flush waits on.
	changed  chan struct{}
	kick     chan struct{}
	quit     chan struct{}
	onResult func(path string, fails int, err error)
}

// A write that failed is tried again after WriteRetry, twice as long after every failure in a row, up to
// WriteRetryMax. FlushWait is how long Close waits for the writes it holds.
var (
	WriteRetry    = time.Second
	WriteRetryMax = time.Minute
	FlushWait     = 10 * time.Second
)

// tempKept is how old a temporary file a killed write left must be before a start removes it.
const tempKept = time.Hour

// NewStore is the store under dir (<data>/v2).
func NewStore(dir string) *Store { return &Store{dir: dir, write: writeRaw} }

// Fresh: the store holds nothing yet — the first start of v2, when the Legacy tabs and queue are imported.
func (s *Store) Fresh() bool {
	_, err := os.Stat(filepath.Join(s.dir, "convs"))
	return errors.Is(err, fs.ErrNotExist)
}

// convFile is a conversation as it is written.
type convFile struct {
	Conv                    wire.Conversation `json:"conv"`
	Session                 string            `json:"session"`
	TranscriptPath          string            `json:"transcriptPath"`
	AgentSessionID          string            `json:"agentSessionId"`
	HandoffArtifact         string            `json:"handoffArtifact,omitempty"`
	HandoffArtifactClientID string            `json:"handoffArtifactClientId,omitempty"`
	Outbox                  outbox.State      `json:"outbox"`
	Questions               []wire.Question   `json:"questions"`
	Permissions             []wire.Permission `json:"permissions"`
	DeletedAt               wire.Ms           `json:"deletedAt"`
	Rewinds                 []rewindRec       `json:"rewinds,omitempty"`
	Left                    []string          `json:"left,omitempty"`
	// Rest is what a conversation at rest read out of its transcript, tied to the file (rest.go).
	Rest *restFile `json:"rest,omitempty"`
}

func (s *Store) convPath(id string) string { return filepath.Join(s.dir, "convs", id+".json") }

// convOfPath is the conversation a file of the store is of, "" for another file.
func (s *Store) convOfPath(path string) string {
	if filepath.Dir(path) != filepath.Join(s.dir, "convs") {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(path), ".json")
}

// Async makes the conversations' writes happen on a goroutine of the store's, the last state of a file winning. A
// write that failed stays with the store and is tried again after a pause that grows. onResult hears every failure
// with how many the file has had in a row, and (0, nil) once a file that had failed is written. Close ends it after
// the writes it holds.
func (s *Store) Async(onResult func(path string, fails int, err error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.async {
		return
	}
	s.async, s.onResult = true, onResult
	s.queued, s.fails, s.retryAt = map[string][]byte{}, map[string]int{}, map[string]time.Time{}
	s.kick, s.quit, s.changed = make(chan struct{}, 1), make(chan struct{}), make(chan struct{})
	safego.Go("convo store writer", func() { s.writer(s.kick, s.quit) })
}

// next takes the file to write now off the queue; when every file left waits for its retry, wait is how long until
// the first of them is due.
func (s *Store) next(now time.Time) (path string, raw []byte, wait time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for p, r := range s.queued {
		if at, held := s.retryAt[p]; held && now.Before(at) {
			if d := at.Sub(now); wait == 0 || d < wait {
				wait = d
			}
			continue
		}
		delete(s.queued, p)
		s.writingPath, s.writingRaw = p, r
		return p, r, 0
	}
	return "", nil, wait
}

// wrote takes the end of a write: a failure puts the file back, unless a newer state came meanwhile, and sets its
// retry.
func (s *Store) wrote(path string, raw []byte, err error, now time.Time) {
	s.mu.Lock()
	had := s.fails[path]
	fails := 0
	if err != nil {
		fails = had + 1
		s.fails[path] = fails
		s.retryAt[path] = now.Add(retryPause(fails))
		if _, newer := s.queued[path]; !newer {
			s.queued[path] = raw
		}
	} else {
		delete(s.fails, path)
		delete(s.retryAt, path)
	}
	s.writingPath, s.writingRaw = "", nil
	close(s.changed)
	s.changed = make(chan struct{})
	onResult := s.onResult
	s.mu.Unlock()
	if onResult != nil && (err != nil || had > 0) {
		onResult(path, fails, err)
	}
}

// retryPause is the pause before the write that follows fails failures in a row.
func retryPause(fails int) time.Duration {
	d := WriteRetry
	for i := 1; i < fails && d < WriteRetryMax; i++ {
		d *= 2
	}
	return min(d, WriteRetryMax)
}

func (s *Store) writer(kick, quit chan struct{}) {
	for {
		path, raw, wait := s.next(time.Now())
		if raw != nil {
			s.wrote(path, raw, s.write(path, raw), time.Now())
			continue
		}
		var retry <-chan time.Time
		if wait > 0 {
			t := time.NewTimer(wait)
			retry = t.C
			select {
			case <-kick:
			case <-retry:
			case <-quit:
				t.Stop()
				return
			}
			t.Stop()
			continue
		}
		select {
		case <-kick:
		case <-quit:
			return
		}
	}
}

// Flush returns once every write handed to the store has reached the disk, with nil. It gives the files that wait
// for a retry one more try, and returns an error when some still fail or when ctx ends first (a file system that
// hangs holds a write for good).
func (s *Store) Flush(ctx context.Context) error {
	s.mu.Lock()
	if !s.async {
		s.mu.Unlock()
		return nil
	}
	// The files that wait for their retry are tried once more now; one that fails again waits again.
	clear(s.retryAt)
	s.mu.Unlock()
	select {
	case s.kick <- struct{}{}:
	default:
	}
	for {
		s.mu.Lock()
		now, due := time.Now(), 0
		for p := range s.queued {
			if at, held := s.retryAt[p]; !held || !now.Before(at) {
				due++
			}
		}
		busy := s.writingRaw != nil || due > 0
		left := len(s.queued)
		changed := s.changed
		s.mu.Unlock()
		switch {
		case !busy && left == 0:
			return nil
		case !busy:
			return fmt.Errorf("convo: %d files not written", left)
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Close flushes within FlushWait and ends the writer; the error is the flush's.
func (s *Store) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), FlushWait)
	defer cancel()
	return s.CloseWithin(ctx)
}

// CloseWithin is Close with the caller's term.
func (s *Store) CloseWithin(ctx context.Context) error {
	err := s.Flush(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.async {
		close(s.quit)
		s.async = false
	}
	return err
}

func (s *Store) isAsync() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.async
}

func (s *Store) saveConv(f convFile) error {
	raw, err := json.Marshal(f)
	if err != nil {
		return err
	}
	path := s.convPath(f.Conv.ID)
	s.mu.Lock()
	if s.async {
		s.queued[path] = raw
		s.mu.Unlock()
		select {
		case s.kick <- struct{}{}:
		default:
		}
		return nil
	}
	s.mu.Unlock()
	return s.write(path, raw)
}

// loadConv reads one conversation as the store last took it: the state its writer still holds, or the file.
func (s *Store) loadConv(id string) (convFile, error) {
	path := s.convPath(id)
	s.mu.Lock()
	raw := s.queued[path]
	if raw == nil && s.writingPath == path {
		raw = s.writingRaw
	}
	s.mu.Unlock()
	var f convFile
	if raw != nil {
		if err := json.Unmarshal(raw, &f); err != nil {
			return f, &os.PathError{Op: "decode", Path: path, Err: err}
		}
		return f, nil
	}
	err := readJSON(path, &f)
	return f, err
}

// sweepTemps removes the temporary files a write that was killed left beside the store's files: only the store's
// own (.<file>.json.<digits>, as writeRaw names them) and only those older than tempKept, so a write of a runtime
// that is still going is never touched. It returns how many went.
func (s *Store) sweepTemps(now time.Time) int {
	n := 0
	for _, dir := range []string{s.dir, filepath.Join(s.dir, "convs")} {
		names, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range names {
			if !e.Type().IsRegular() || !isTempName(e.Name()) {
				continue
			}
			if info, err := e.Info(); err != nil || now.Sub(info.ModTime()) < tempKept {
				continue
			}
			if os.Remove(filepath.Join(dir, e.Name())) == nil {
				n++
			}
		}
	}
	return n
}

// isTempName: the name is one os.CreateTemp gave a write of the store: ".<name>.json.<digits>".
func isTempName(name string) bool {
	if !strings.HasPrefix(name, ".") {
		return false
	}
	i := strings.LastIndex(name, ".")
	base, tail := name[1:i+1], name[i+1:]
	if !strings.HasSuffix(base, ".json.") || len(base) == len(".json.") || tail == "" {
		return false
	}
	for _, r := range tail {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// loadConvs reads every conversation written; a file that does not parse is skipped and named in the error.
func (s *Store) loadConvs() ([]convFile, error) {
	dir := filepath.Join(s.dir, "convs")
	names, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []convFile
	var errs []error
	for _, n := range names {
		if n.IsDir() || !strings.HasSuffix(n.Name(), ".json") {
			continue
		}
		var f convFile
		if err := readJSON(filepath.Join(dir, n.Name()), &f); err != nil {
			errs = append(errs, err)
			continue
		}
		if !wire.ValidConvID(f.Conv.ID) {
			continue
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Conv.CreatedAt < out[j].Conv.CreatedAt })
	return out, errors.Join(errs...)
}

// opRecord is the answer to one opId.
type opRecord struct {
	Hash   string         `json:"hash"`
	Result wire.CmdResult `json:"result"`
	At     wire.Ms        `json:"at"`
}

const (
	// OpsKept is how long the answer to an opId is kept, OpsMax how many at most.
	OpsKept = 24 * time.Hour
	OpsMax  = 5000
)

func (s *Store) saveOps(ops map[string]opRecord) error {
	return writeJSON(filepath.Join(s.dir, "ops.json"), ops)
}

func (s *Store) loadOps() (map[string]opRecord, error) {
	ops := map[string]opRecord{}
	err := readJSON(filepath.Join(s.dir, "ops.json"), &ops)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]opRecord{}, nil
	}
	return ops, err
}

func (s *Store) saveProjects(ps []wire.Project) error {
	return writeJSON(filepath.Join(s.dir, "projects.json"), ps)
}

func (s *Store) loadProjects() ([]wire.Project, error) {
	var ps []wire.Project
	err := readJSON(filepath.Join(s.dir, "projects.json"), &ps)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return ps, err
}

// favorites.json: how the pinned conversations are grouped and ordered.
func (s *Store) saveFavorites(f wire.Favorites) error {
	return writeJSON(filepath.Join(s.dir, "favorites.json"), f)
}

func (s *Store) loadFavorites() (wire.Favorites, error) {
	f := wire.EmptyFavorites()
	err := readJSON(filepath.Join(s.dir, "favorites.json"), &f)
	if errors.Is(err, fs.ErrNotExist) {
		return wire.EmptyFavorites(), nil
	}
	return f.Clone(), err
}

// drafts.json: the composers' drafts with their revisions, empty ones too (a clear keeps its revision).
func (s *Store) saveDrafts(b draftBook) error {
	return writeJSON(filepath.Join(s.dir, "drafts.json"), b)
}

// loadDrafts reads the drafts back; a record that does not hold up is dropped, and the seqs, which were of the last
// run's epoch, are forgotten.
func (s *Store) loadDrafts() (draftBook, error) {
	b := draftBook{}
	err := readJSON(filepath.Join(s.dir, "drafts.json"), &b)
	if errors.Is(err, fs.ErrNotExist) {
		return draftBook{}, nil
	}
	out := make(draftBook, len(b))
	for k, v := range b {
		if v.Draft.Key == k && v.Draft.Validate() == nil {
			out[k] = keptDraft{Draft: v.Draft}
		}
	}
	return out, err
}

func readJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return &os.PathError{Op: "decode", Path: path, Err: err}
	}
	return nil
}

// writeJSON writes v whole: a temporary file beside the target, fsync, rename, fsync of the folder.
func writeJSON(path string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeRaw(path, raw)
}

func writeRaw(path string, raw []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync() // the file is already renamed; syncing the directory only makes it durable sooner
		d.Close()
	}
	return nil
}

// legacy-projects.json: the conversations Legacy's organizer has already put into a project, so that each is put
// there once and a move the person makes in v2 stays.
func (s *Store) saveLegacyPlaced(ids []string) error {
	return writeJSON(filepath.Join(s.dir, "legacy-projects.json"), ids)
}

func (s *Store) loadLegacyPlaced() ([]string, error) {
	var ids []string
	err := readJSON(filepath.Join(s.dir, "legacy-projects.json"), &ids)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return ids, err
}
