// Package settings keeps the runtime's settings files as the previous runtime did: the person's preferences in the
// config file (with the pair's secret and the automation token beside them), keyboard shortcuts, quick prompts, sidebar
// items and each workspace's sent messages. Same files, same bytes, same locks: this package is the one writer of
// these files in the process.
package settings

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// Store is the settings files of one data folder.
type Store struct {
	DataDir    string
	ConfigFile string
	Now        func() time.Time
	// RandomBytes are n random bytes (crypto/rand when nil).
	RandomBytes func(n int) []byte
	// NewID is a sent message's id (nanoid's 21 characters when nil).
	NewID func() string

	config   sync.Mutex // the config lock: every read-change-write of the config file
	written  string     // what this process last wrote to the config (without updatedAt): the same again is not written
	files    sync.Mutex
	fileLock map[string]*sync.Mutex
	history  sync.Mutex
	wsLocks  map[string]*wsLock
}

type wsLock struct {
	mu    sync.Mutex
	users int
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Store) random(n int) []byte {
	if s.RandomBytes != nil {
		return s.RandomBytes(n)
	}
	return cryptoRandom(n)
}

func (s *Store) newID() string {
	if s.NewID != nil {
		return s.NewID()
	}
	const alphabet = "useandom-26T198340PX75pxJACKVERYMINDBUSHWOLF_GQZbfghjklqvwyzrict"
	b := s.random(21)
	for i := range b {
		b[i] = alphabet[b[i]&63]
	}
	return string(b)
}

func cryptoRandom(n int) []byte {
	b := make([]byte, n)
	_, _ = io.ReadFull(rand.Reader, b)
	return b
}

// isoNow is new Date().toISOString().
func (s *Store) isoNow() string {
	iso, _ := jscompat.ISOString(float64(s.now().UnixMilli()))
	return iso
}

// Secret is 32 random bytes in hex, as the previous runtime made its secrets.
func (s *Store) Secret() string { return hex.EncodeToString(s.random(32)) }

// HashPassword is a stored password for plain, salted from the store's randomness.
func (s *Store) HashPassword(plain string) string { return hashPassword(plain, s.random(scryptSalt)) }

// readJSON is a file read as UTF-8 and parsed as JSON.parse would; ok is false when it is missing or does not parse.
func readJSON(file string) (any, bool) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, false
	}
	v, err := jsvalue.Parse([]byte(jscompat.DecodeUTF8(raw)))
	if err != nil {
		return nil, false
	}
	return v, true
}

// Spread copies what {...v} copies of a JSON value onto o: an object's keys, an array's or a string's elements under
// their indices; nothing of a number, a boolean or null.
func Spread(o *jsvalue.Object, v any) {
	switch x := v.(type) {
	case *jsvalue.Object:
		o.Assign(x)
	case []any:
		for i, e := range x {
			o.Set(itoa(i), e)
		}
	case string:
		for i, u := range utf16.Encode([]rune(x)) {
			o.Set(itoa(i), string(utf16.Decode([]uint16{u})))
		}
	}
}

func itoa(i int) string { return jscompat.Number(float64(i)) }

// lockFile serializes the writers of one file in this process.
func (s *Store) lockFile(file string) func() {
	s.files.Lock()
	if s.fileLock == nil {
		s.fileLock = map[string]*sync.Mutex{}
	}
	mu := s.fileLock[file]
	if mu == nil {
		mu = &sync.Mutex{}
		s.fileLock[file] = mu
	}
	s.files.Unlock()
	mu.Lock()
	return mu.Unlock
}

// writeInPlace is fs.writeFile: the file truncated and written where it is (its mode kept), created with mode when new;
// the folder is made first as the previous stores did.
func writeInPlace(file string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o777); err != nil {
		return err
	}
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// writeAtomic writes beside the file under a fixed temporary name and renames it over, as the config and the message
// history were written.
func writeAtomic(file string, data []byte, mode os.FileMode) error {
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, file); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
