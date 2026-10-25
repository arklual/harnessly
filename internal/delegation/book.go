package delegation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Book is what the runtime keeps of each delegated chat besides its record: whether its parent asked to be woken when
// it finishes, when the parent last gave it work, and the wake-ups sent so far. It lives in one small file; a book
// without a path keeps nothing (tests).
type Book struct {
	Path string

	mu     sync.Mutex
	loaded bool
	chats  map[string]Page
}

// Page is one delegated chat in the book.
type Page struct {
	// Notify: the parent is woken with the chat's result when it finishes a turn.
	Notify bool `json:"notify,omitempty"`
	// AskedAt is when the parent last gave the chat work (the task, or a message), Unix ms: a turn that ended before
	// it is not the answer to it.
	AskedAt int64 `json:"askedAt"`
	// Woken is the turn (or the end) the parent was last woken for, Wakes how many wake-ups went to it.
	Woken string `json:"woken,omitempty"`
	Wakes int    `json:"wakes,omitempty"`
}

func (b *Book) load() {
	if b.loaded {
		return
	}
	b.loaded, b.chats = true, map[string]Page{}
	if b.Path == "" {
		return
	}
	if raw, err := os.ReadFile(b.Path); err == nil {
		_ = json.Unmarshal(raw, &b.chats)
	}
	if b.chats == nil {
		b.chats = map[string]Page{}
	}
}

func (b *Book) save() {
	if b.Path == "" {
		return
	}
	raw, err := json.Marshal(b.chats)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(b.Path), 0o700)
	tmp := b.Path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, b.Path)
	}
}

// Get is the chat's page; false for a chat no agent delegated.
func (b *Book) Get(id string) (Page, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.load()
	p, ok := b.chats[id]
	return p, ok
}

// Update changes the chat's page (a new one starts empty) and writes the book.
func (b *Book) Update(id string, f func(p *Page)) Page {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.load()
	p := b.chats[id]
	f(&p)
	b.chats[id] = p
	b.save()
	return p
}

// notifying are the chats whose parents asked to be woken and still can be.
func (b *Book) notifying() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.load()
	var out []string
	for id, p := range b.chats {
		if p.Notify && p.Wakes < MaxWakes {
			out = append(out, id)
		}
	}
	return out
}

// claim marks the parent woken for key unless it already was or the wake-ups ran out: true when this caller is the one
// to wake it.
func (b *Book) claim(id, key string, max int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.load()
	p, ok := b.chats[id]
	if !ok || !p.Notify || p.Woken == key || p.Wakes >= max {
		return false
	}
	p.Woken, p.Wakes = key, p.Wakes+1
	b.chats[id] = p
	b.save()
	return true
}
