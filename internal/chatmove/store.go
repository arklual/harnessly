package chatmove

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// HoldTTL is how long a hold stands without a seal: a cluster that died between the two does not freeze the source.
const HoldTTL = 5 * time.Minute

// Pointer is where a sealed conversation went on.
type Pointer struct {
	MachineID      string `json:"machineId"`
	Name           string `json:"name"`
	ConversationID string `json:"conversationId"`
}

type opRecord struct {
	TargetID string `json:"targetId"`
	Repo     string `json:"repo,omitempty"`
	Worktree string `json:"worktree,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Dir      string `json:"dir,omitempty"` // a folder made for a move without files
	At       int64  `json:"at"`
}

type sealRecord struct {
	OpID   string   `json:"opId"`
	HeldAt int64    `json:"heldAt,omitempty"`
	To     *Pointer `json:"to,omitempty"`
	At     int64    `json:"at"`
}

type state struct {
	Ops   map[string]opRecord   `json:"ops"`
	Seals map[string]sealRecord `json:"seals"`
}

// Store keeps what a runtime must remember across a restart: which operation made which conversation, and which
// conversations are held or sealed. One small private file.
type Store struct {
	mu   sync.Mutex
	path string
	now  func() time.Time
	st   state
}

// OpenStore reads the file at path (none is an empty store).
func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, now: time.Now, st: state{Ops: map[string]opRecord{}, Seals: map[string]sealRecord{}}}
	if path == "" {
		return s, nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &s.st); err != nil {
		return nil, err
	}
	if s.st.Ops == nil {
		s.st.Ops = map[string]opRecord{}
	}
	if s.st.Seals == nil {
		s.st.Seals = map[string]sealRecord{}
	}
	return s, nil
}

// save writes the file; the caller holds the lock.
func (s *Store) save() error {
	if s.path == "" {
		return nil
	}
	raw, err := json.Marshal(s.st)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) op(id string) (opRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.st.Ops[id]
	return r, ok
}

func (s *Store) putOp(id string, r opRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.At = s.now().UnixMilli()
	s.st.Ops[id] = r
	return s.save()
}

func (s *Store) dropOp(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.st.Ops, id)
	_ = s.save()
}

// Blocked says whether the conversation takes no messages: sealed, or held and the hold not run out.
func (s *Store) Blocked(conv string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.st.Seals[conv]
	if !ok {
		return false
	}
	if r.To != nil {
		return true
	}
	return r.HeldAt != 0 && s.now().UnixMilli() < r.HeldAt+HoldTTL.Milliseconds()
}

// Sealed is where a conversation went, nil when it did not.
func (s *Store) Sealed(conv string) *Pointer {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.st.Seals[conv]; ok {
		return r.To
	}
	return nil
}

var errSealed = errors.New("chatmove: the conversation is moved already")

func (s *Store) hold(conv, opID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.st.Seals[conv]
	if r.To != nil && r.OpID != opID {
		return errSealed
	}
	if r.To == nil {
		r.OpID, r.HeldAt, r.At = opID, s.now().UnixMilli(), s.now().UnixMilli()
		s.st.Seals[conv] = r
	}
	return s.save()
}

func (s *Store) seal(conv, opID string, to Pointer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.st.Seals[conv]
	if r.To != nil && r.OpID != opID {
		return errSealed
	}
	r.OpID, r.HeldAt, r.To, r.At = opID, 0, &to, s.now().UnixMilli()
	s.st.Seals[conv] = r
	return s.save()
}

// release drops a hold of the operation; a seal stays.
func (s *Store) release(conv, opID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.st.Seals[conv]
	if !ok || r.To != nil || r.OpID != opID {
		return nil
	}
	delete(s.st.Seals, conv)
	return s.save()
}
