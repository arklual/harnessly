// Package notifybots keeps the Telegram bots the person linked in Settings for notifications: each bot with its token,
// the chat it writes to and the person's instruction of when an agent should use it. The runtime holds the tokens and
// makes every call to Telegram; the page and the agents see a bot by its name, never its token.
package notifybots

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// File is the bots' file in the runtime's v2 data folder.
const File = "notify-bots.json"

// Bot is one linked bot as it is stored.
type Bot struct {
	ID string `json:"id"`
	// Name is the person's name for it, the one agents pick it by.
	Name string `json:"name"`
	// Username and Title are the bot's @username and its display name as getMe said.
	Username string `json:"username"`
	Title    string `json:"title"`
	// TelegramID is the bot's own user id: a new token of the same bot keeps the chat.
	TelegramID int64  `json:"telegramId"`
	Token      string `json:"token"`
	// Chat is where the bot writes; nil until a chat is bound.
	Chat        *Chat  `json:"chat,omitempty"`
	Instruction string `json:"instruction"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

// Chat is the bound chat: a private chat with the person, a group or a channel.
type Chat struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type"`
}

type fileBody struct {
	Bots []Bot `json:"bots"`
}

// Store reads and writes the bots' file; every writer of this process goes through one Store.
type Store struct {
	Path string

	mu sync.Mutex
}

var (
	storesMu sync.Mutex
	stores   = map[string]*Store{}
)

// Open is the process's one Store of a data folder's file (the routes and the agents' launch share it).
func Open(dataDir string) *Store {
	path := filepath.Join(dataDir, "v2", File)
	storesMu.Lock()
	defer storesMu.Unlock()
	if s := stores[path]; s != nil {
		return s
	}
	s := &Store{Path: path}
	stores[path] = s
	return s
}

// ErrNotFound: no bot has that id.
var ErrNotFound = errors.New("notifybots: no such bot")

func (s *Store) read() ([]Bot, error) {
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return []Bot{}, nil
	}
	if err != nil {
		return nil, err
	}
	var body fileBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	if body.Bots == nil {
		body.Bots = []Bot{}
	}
	return body.Bots, nil
}

// write replaces the file through a temporary file beside it, readable by the runtime's user only.
func (s *Store) write(bots []Bot) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(fileBody{Bots: bots}, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.Path), "."+File+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(append(raw, '\n'))
	cerr := f.Close()
	if err := errors.Join(werr, cerr, os.Chmod(tmp, 0o600)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, s.Path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// List is every bot in the order they were added.
func (s *Store) List() ([]Bot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}

// Get is one bot by id.
func (s *Store) Get(id string) (Bot, error) {
	bots, err := s.List()
	if err != nil {
		return Bot{}, err
	}
	for _, b := range bots {
		if b.ID == id {
			return b, nil
		}
	}
	return Bot{}, ErrNotFound
}

// Add appends a new bot.
func (s *Store) Add(b Bot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	bots, err := s.read()
	if err != nil {
		return err
	}
	return s.write(append(bots, b))
}

// Update changes one bot under the lock and writes it back; the changed bot is returned.
func (s *Store) Update(id string, change func(*Bot) error) (Bot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bots, err := s.read()
	if err != nil {
		return Bot{}, err
	}
	for i := range bots {
		if bots[i].ID != id {
			continue
		}
		if err := change(&bots[i]); err != nil {
			return Bot{}, err
		}
		return bots[i], s.write(bots)
	}
	return Bot{}, ErrNotFound
}

// Remove drops one bot.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	bots, err := s.read()
	if err != nil {
		return err
	}
	kept := make([]Bot, 0, len(bots))
	for _, b := range bots {
		if b.ID != id {
			kept = append(kept, b)
		}
	}
	if len(kept) == len(bots) {
		return ErrNotFound
	}
	return s.write(kept)
}
