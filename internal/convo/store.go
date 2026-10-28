package convo

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/outbox"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Store keeps the runtime's v2 state under <data>/v2: one file per conversation, the op answers, the projects.
// Every write is the whole last state, through a temporary file, fsync and rename: a crash leaves the old file or
// the new one, never half of one.
type Store struct {
	dir string

	// A conversation's file is written whole by a writer of its own once Async is on: the actor marshals its state
	// and goes on, and a disk that is slow to sync holds up no mail.
	mu      sync.Mutex
	async   bool
	queued  map[string][]byte
	writing bool
	kick    chan struct{}
	quit    chan struct{}
	onErr   func(path string, err error)
}

// NewStore is the store under dir (<data>/v2).
func NewStore(dir string) *Store { return &Store{dir: dir} }

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
}

func (s *Store) convPath(id string) string { return filepath.Join(s.dir, "convs", id+".json") }

// Async makes the conversations' writes happen on a goroutine of the store's, the last state of a file winning; onErr
// hears a write that failed. Close ends it after the writes it holds.
func (s *Store) Async(onErr func(path string, err error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.async {
		return
	}
	s.async, s.onErr = true, onErr
	s.queued, s.kick, s.quit = map[string][]byte{}, make(chan struct{}, 1), make(chan struct{})
	go s.writer(s.kick, s.quit)
}

func (s *Store) writer(kick, quit chan struct{}) {
	for {
		select {
		case <-kick:
		case <-quit:
			return
		}
		for {
			s.mu.Lock()
			var path string
			var raw []byte
			for path, raw = range s.queued {
				break
			}
			if raw == nil {
				s.writing = false
				s.mu.Unlock()
				break
			}
			delete(s.queued, path)
			s.writing = true
			s.mu.Unlock()
			if err := writeRaw(path, raw); err != nil && s.onErr != nil {
				s.onErr(path, err)
			}
		}
	}
}

// Flush returns once every write handed to the store has reached the disk.
func (s *Store) Flush() {
	for {
		s.mu.Lock()
		done := !s.async || len(s.queued) == 0 && !s.writing
		s.mu.Unlock()
		if done {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// Close flushes and ends the writer.
func (s *Store) Close() {
	s.Flush()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.async {
		close(s.quit)
		s.async = false
	}
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
	return writeRaw(path, raw)
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
