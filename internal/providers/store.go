package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// Store keeps the keys of the profiles, one file each: <Dir>/<profile id>.key, 0600 in a folder of 0700, written
// whole under a temporary name and renamed over. A key is the person's, on this machine, and is read only by the file
// the CLI is told to read (apiKeyHelper, `cat`), by a test of the connection and by the mask the wire puts over the
// feed. It is never logged and never put in a launch line.
type Store struct {
	Dir string
	// Logins are the vendor CLIs' own sign-ins a profile runs on when no key is saved for it, by profile id (the Grok
	// CLI's for xai); nil for none.
	Logins map[string]Login

	mu sync.Mutex
	// masks are the keys the feed hides, each with the form it has in a JSON string; read on every frame, so kept here
	// and rebuilt when a key is saved or removed. nil until the first use.
	masks atomic.Pointer[[]mask]
}

type mask struct{ key, escaped, bullets []byte }

// ErrNoKey: no key is saved for the profile.
var ErrNoKey = errors.New("providers: no key saved")

// KeyFile is the file the key of a profile is kept in (it may not exist).
func (s *Store) KeyFile(id string) string { return filepath.Join(s.Dir, id+".key") }

// Set saves the key of a profile, replacing a saved one, and answers what the page may know of it.
func (s *Store) Set(id, key string) (wire.ProfileKey, error) {
	if _, ok := Find(id); !ok {
		return wire.ProfileKey{}, wire.ErrProfileUnknown
	}
	if err := (wire.ProfileKeyEdit{Key: key}).Validate(); err != nil {
		return wire.ProfileKey{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return wire.ProfileKey{}, err
	}
	if err := os.Chmod(s.Dir, 0o700); err != nil {
		return wire.ProfileKey{}, err
	}
	file := s.KeyFile(id)
	tmp := file + ".tmp"
	_ = os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return wire.ProfileKey{}, err
	}
	// No newline: the file is read by `cat` into a header.
	if _, err := f.WriteString(key); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return wire.ProfileKey{}, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return wire.ProfileKey{}, err
	}
	if err := os.Rename(tmp, file); err != nil {
		_ = os.Remove(tmp)
		return wire.ProfileKey{}, err
	}
	s.rebuildLocked()
	return tailOf(key), nil
}

// Remove deletes the saved key; a profile with none stays as it is.
func (s *Store) Remove(id string) (wire.ProfileKey, error) {
	if _, ok := Find(id); !ok {
		return wire.ProfileKey{}, wire.ErrProfileUnknown
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.KeyFile(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return wire.ProfileKey{}, err
	}
	s.rebuildLocked()
	return wire.ProfileKey{}, nil
}

// Key is the saved key of a profile; ErrNoKey when there is none. Callers use it for the request of a test and never
// keep it.
func (s *Store) Key(id string) (string, error) {
	if _, ok := Find(id); !ok {
		return "", wire.ErrProfileUnknown
	}
	raw, err := os.ReadFile(s.KeyFile(id))
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNoKey
	}
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(string(raw))
	if key == "" {
		return "", ErrNoKey
	}
	return key, nil
}

// State is whether a key is saved for the profile, and its tail.
func (s *Store) State(id string) wire.ProfileKey {
	key, err := s.Key(id)
	if err != nil {
		return wire.ProfileKey{}
	}
	return tailOf(key)
}

// Configured says whether a key is saved for the profile.
func (s *Store) Configured(id string) bool { return s.State(id).Configured }

// LoginOf is the account of the vendor CLI's sign-in the profile can run on; ok is false while there is none.
func (s *Store) LoginOf(id string) (account string, ok bool) {
	l := s.Logins[id]
	if l == nil {
		return "", false
	}
	return l.Account()
}

// Ready says whether a conversation can run on the profile: a key is saved, or the vendor CLI is signed in.
func (s *Store) Ready(id string) bool {
	if s.Configured(id) {
		return true
	}
	_, ok := s.LoginOf(id)
	return ok
}

// Credential is what a request of the profile is authorised with: the saved key, else the token of the vendor CLI's
// sign-in. A saved key is the person's choice on the Settings screen and goes first.
func (s *Store) Credential(ctx context.Context, id string) (string, error) {
	key, err := s.Key(id)
	if err == nil || !errors.Is(err, ErrNoKey) {
		return key, err
	}
	if l := s.Logins[id]; l != nil {
		if _, ok := l.Account(); ok {
			return l.Token(ctx)
		}
	}
	return "", ErrNoKey
}

// tailOf is «…» and the last four characters of key.
func tailOf(key string) wire.ProfileKey {
	runes := []rune(key)
	tail := "…" + string(runes[max(0, len(runes)-4):])
	return wire.ProfileKey{Configured: true, Tail: &tail}
}

// Mask is the form a saved key takes in the feed: bullets and the last four characters.
func Mask(key string) string {
	runes := []rune(key)
	return "•••" + string(runes[max(0, len(runes)-4):])
}

// rebuildLocked reads every saved key again into the masks.
func (s *Store) rebuildLocked() {
	masks := []mask{}
	for _, p := range presets {
		raw, err := os.ReadFile(s.KeyFile(p.ID))
		key := strings.TrimSpace(string(raw))
		// A key shorter than the least a page may save cannot be one of ours, and would mask words.
		if err != nil || len(key) < wire.ProfileKeyMin {
			continue
		}
		esc, _ := json.Marshal(key)
		masks = append(masks, mask{key: []byte(key), escaped: esc[1 : len(esc)-1], bullets: []byte(Mask(key))})
	}
	s.masks.Store(&masks)
}

// Redact puts the mask over every saved key in b (a frame as it goes on the wire), in the form it has in a JSON string
// too. It is for a key an agent printed: a `cat` of the key file, an environment dump. It returns b itself when there
// is nothing to hide.
func (s *Store) Redact(b []byte) []byte {
	masks := s.masks.Load()
	if masks == nil {
		s.mu.Lock()
		if s.masks.Load() == nil {
			s.rebuildLocked()
		}
		s.mu.Unlock()
		masks = s.masks.Load()
	}
	for _, m := range *masks {
		if bytes.Contains(b, m.key) {
			b = bytes.ReplaceAll(b, m.key, m.bullets)
		}
		if !bytes.Equal(m.escaped, m.key) && bytes.Contains(b, m.escaped) {
			b = bytes.ReplaceAll(b, m.escaped, m.bullets)
		}
	}
	return b
}
