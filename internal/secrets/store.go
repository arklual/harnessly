// Package secrets is the safe: the tokens and keys the person hands to the agents of this machine, kept encrypted on
// the machine and handed to a command's environment or a short-lived file, never to a conversation. A value comes in
// only from the person's page (Settings → Safe, or the card an agent's `harnessly secret ask` puts on screen) and goes
// out only through the CLI routes a command runs with; it is never logged, never in a list, never in an answer of the
// page's routes. See docs/safe.md for the model and what it does not protect against.
package secrets

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/chacha20poly1305"
)

// Reasons a change or a read is refused with; the page and the CLI put them in words.
const (
	ReasonBadName  = "bad-name"
	ReasonEmpty    = "empty"
	ReasonTooLarge = "too-large"
	ReasonNotFound = "not-found"
	ReasonKeyLost  = "key-lost"
	ReasonFailed   = "failed"
)

// MaxValue is the largest value the safe keeps (a private key with its chain fits).
const MaxValue = 64 << 10

// maxUsedBy is how many of the latest users an entry remembers.
const maxUsedBy = 5

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidName says whether name may name a secret: letters, digits, '.', '_' and '-', 64 at most, not starting with a
// sign, so it is safe in a path, a log line and a command line.
func ValidName(name string) bool { return nameRe.MatchString(name) }

// Refusal is an error with its reason.
type Refusal struct {
	Reason string
	Err    error
}

func (r *Refusal) Error() string {
	if r.Err != nil {
		return r.Reason + ": " + r.Err.Error()
	}
	return r.Reason
}

func (r *Refusal) Unwrap() error { return r.Err }

func refuse(reason string, err error) error { return &Refusal{Reason: reason, Err: err} }

// ReasonOf is the reason of an error of this package; failed for any other error.
func ReasonOf(err error) string {
	var r *Refusal
	if errors.As(err, &r) {
		return r.Reason
	}
	return ReasonFailed
}

// Use is one command's use of a secret: the conversation it ran from (its title as it was then), when.
type Use struct {
	Conv  string `json:"conv"`
	Title string `json:"title"`
	At    int64  `json:"at"`
}

// View is a secret as anyone may see it: everything but the value.
type View struct {
	Name       string `json:"name"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
	LastUsedAt int64  `json:"lastUsedAt"`
	UsedBy     []Use  `json:"usedBy"`
}

type entry struct {
	View
	// Nonce and Box are the value sealed with XChaCha20-Poly1305 under the installation's key, the name bound in.
	Nonce []byte `json:"nonce"`
	Box   []byte `json:"box"`
}

type file struct {
	Version int      `json:"version"`
	Secrets []*entry `json:"secrets"`
}

// Store is the safe's file and its key. The file (<dir>/safe.json) holds only sealed values; the key is a separate file,
// by default outside the runtime's data folder, which every agent of the machine knows (see DefaultKeyFile).
type Store struct {
	dir     string
	keyFile string
	Now     func() time.Time

	mu sync.Mutex
}

// Open is the safe in dir (created on the first write) with its key in keyFile.
func Open(dir, keyFile string) *Store {
	return &Store{dir: dir, keyFile: keyFile, Now: time.Now}
}

// DefaultKeyFile is where the key lies when RUNTIME_SECRETS_KEY_FILE names no other file: the user's state folder
// ($XDG_STATE_HOME/harnessly, else ~/.local/state/harnessly), not the runtime's data folder (which the agents' CLI reads)
// nor a workspace.
func DefaultKeyFile(getenv func(string) string, home string) string {
	if v := strings.TrimSpace(getenv("RUNTIME_SECRETS_KEY_FILE")); v != "" {
		return v
	}
	base := strings.TrimSpace(getenv("XDG_STATE_HOME"))
	if base == "" {
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "harnessly", "safe.key")
}

func (s *Store) path() string { return filepath.Join(s.dir, "safe.json") }

func (s *Store) now() int64 { return s.Now().UnixMilli() }

func (s *Store) read() (*file, error) {
	raw, err := os.ReadFile(s.path())
	if errors.Is(err, os.ErrNotExist) {
		return &file{Version: 1}, nil
	}
	if err != nil {
		return nil, refuse(ReasonFailed, err)
	}
	var f file
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, refuse(ReasonFailed, err)
	}
	return &f, nil
}

func (s *Store) write(f *file) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return refuse(ReasonFailed, err)
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return refuse(ReasonFailed, err)
	}
	tmp, err := os.CreateTemp(s.dir, ".safe-*.json")
	if err != nil {
		return refuse(ReasonFailed, err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return refuse(ReasonFailed, err)
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return refuse(ReasonFailed, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return refuse(ReasonFailed, err)
	}
	if err := tmp.Close(); err != nil {
		return refuse(ReasonFailed, err)
	}
	if err := os.Rename(name, s.path()); err != nil {
		return refuse(ReasonFailed, err)
	}
	return nil
}

// key reads the installation's key; create makes one when there is none (only a write does). A key that is gone
// while sealed values remain is key-lost: those values cannot be read any more and must be put again.
func (s *Store) key(create bool) ([]byte, error) {
	raw, err := os.ReadFile(s.keyFile)
	if err == nil {
		if len(raw) != chacha20poly1305.KeySize {
			return nil, refuse(ReasonKeyLost, errors.New("the key file is not a key"))
		}
		return raw, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, refuse(ReasonFailed, err)
	}
	if !create {
		return nil, refuse(ReasonKeyLost, err)
	}
	key := make([]byte, chacha20poly1305.KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, refuse(ReasonFailed, err)
	}
	if err := os.MkdirAll(filepath.Dir(s.keyFile), 0o700); err != nil {
		return nil, refuse(ReasonFailed, err)
	}
	// O_EXCL: two runtimes starting at once agree on one key.
	f, err := os.OpenFile(s.keyFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return s.key(false)
	}
	if err != nil {
		return nil, refuse(ReasonFailed, err)
	}
	if _, err := f.Write(key); err != nil {
		_ = f.Close()
		return nil, refuse(ReasonFailed, err)
	}
	if err := f.Close(); err != nil {
		return nil, refuse(ReasonFailed, err)
	}
	return key, nil
}

func aad(name string) []byte { return []byte("harnessly-safe/v1:" + name) }

// Clean is a value as it is kept: a one-line value (a token) without the spaces and line break a paste brings, a
// several-line one (a private key) as it was typed.
func Clean(value string) string {
	if strings.Contains(strings.TrimSpace(value), "\n") {
		return value
	}
	return strings.TrimSpace(value)
}

func find(f *file, name string) *entry {
	for _, e := range f.Secrets {
		if e.Name == name {
			return e
		}
	}
	return nil
}

func viewOf(e *entry) View {
	v := e.View
	v.UsedBy = append([]Use{}, e.UsedBy...)
	return v
}

// List is every secret without its value, by name.
func (s *Store) List() ([]View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.read()
	if err != nil {
		return nil, err
	}
	out := make([]View, 0, len(f.Secrets))
	for _, e := range f.Secrets {
		out = append(out, viewOf(e))
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// Has says whether a secret of that name is kept.
func (s *Store) Has(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.read()
	return err == nil && find(f, name) != nil
}

// Put keeps value under name, a new secret or a new value of one (its uses stay); created says which.
func (s *Store) Put(name, value string) (v View, created bool, err error) {
	if !ValidName(name) {
		return View{}, false, refuse(ReasonBadName, nil)
	}
	value = Clean(value)
	if value == "" {
		return View{}, false, refuse(ReasonEmpty, nil)
	}
	if len(value) > MaxValue {
		return View{}, false, refuse(ReasonTooLarge, nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.read()
	if err != nil {
		return View{}, false, err
	}
	key, err := s.key(true)
	if err != nil {
		return View{}, false, err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return View{}, false, refuse(ReasonFailed, err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return View{}, false, refuse(ReasonFailed, err)
	}
	now := s.now()
	e := find(f, name)
	if e == nil {
		e = &entry{View: View{Name: name, CreatedAt: now, UsedBy: []Use{}}}
		f.Secrets = append(f.Secrets, e)
		created = true
	}
	e.UpdatedAt = now
	e.Nonce = nonce
	e.Box = aead.Seal(nil, nonce, []byte(value), aad(name))
	if err := s.write(f); err != nil {
		return View{}, false, err
	}
	return viewOf(e), created, nil
}

// Remove forgets a secret.
func (s *Store) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.read()
	if err != nil {
		return err
	}
	kept := f.Secrets[:0]
	found := false
	for _, e := range f.Secrets {
		if e.Name == name {
			found = true
			continue
		}
		kept = append(kept, e)
	}
	if !found {
		return refuse(ReasonNotFound, nil)
	}
	f.Secrets = kept
	return s.write(f)
}

// Reveal opens the values of names for one command and records the use; nothing is opened when one of them is
// missing or cannot be read.
func (s *Store) Reveal(names []string, use Use) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.read()
	if err != nil {
		return nil, err
	}
	entries := make([]*entry, 0, len(names))
	for _, name := range names {
		e := find(f, name)
		if e == nil {
			return nil, refuse(ReasonNotFound, errors.New(name))
		}
		entries = append(entries, e)
	}
	key, err := s.key(false)
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, refuse(ReasonFailed, err)
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		plain, err := aead.Open(nil, e.Nonce, e.Box, aad(e.Name))
		if err != nil {
			return nil, refuse(ReasonKeyLost, errors.New(e.Name))
		}
		out[e.Name] = string(plain)
	}
	use.At = s.now()
	for _, e := range entries {
		e.LastUsedAt = use.At
		users := []Use{use}
		for _, u := range e.UsedBy {
			if u.Conv != use.Conv || use.Conv == "" {
				users = append(users, u)
			}
		}
		if len(users) > maxUsedBy {
			users = users[:maxUsedBy]
		}
		e.UsedBy = users
	}
	if err := s.write(f); err != nil {
		return nil, err
	}
	return out, nil
}
