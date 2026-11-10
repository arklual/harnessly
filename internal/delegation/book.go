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
	// Contract: the chat was started with the rules of a delegated chat (see Rules): its final answer ends with a
	// marker, and a turn that ends without one is a pause the platform answers with a reminder (HRN-828). Chats that
	// were delegated before the rules existed are not held to them.
	Contract bool `json:"contract,omitempty"`
	// Nudges are the reminders sent since AskedAt, Nudged the turn (or the end) the last one was for, Judged the turn
	// found to be an answer (or given up on after the reminders ran out), GaveUp that the reminders ran out.
	Nudges int    `json:"nudges,omitempty"`
	Nudged string `json:"nudged,omitempty"`
	Judged string `json:"judged,omitempty"`
	GaveUp bool   `json:"gaveUp,omitempty"`
	// HeldAt is when the chat's turn first ended with background work still running, Unix ms; EndedAt when it was
	// first seen not running in the middle of a turn.
	HeldAt  int64 `json:"heldAt,omitempty"`
	EndedAt int64 `json:"endedAt,omitempty"`
}

// asked is a new piece of work from the parent: the reminders and the verdicts of the work before it are forgotten.
func (p *Page) asked(at int64) {
	p.AskedAt = at
	p.Nudges, p.Nudged, p.Judged, p.GaveUp, p.HeldAt, p.EndedAt = 0, "", "", false, 0, 0
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

// watched are the chats the sweep looks at: those whose parents asked to be woken and still can be, and those held to
// the rules of a delegated chat.
func (b *Book) watched() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.load()
	var out []string
	for id, p := range b.chats {
		if p.Notify && p.Wakes < MaxWakes || p.Contract {
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

// nudge claims a reminder for key unless one went for it already or they ran out: true when this caller is the one to
// send it. Nudges is then the number sent including this one.
func (b *Book) nudge(id, key string, max int) (int, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.load()
	p, ok := b.chats[id]
	if !ok || p.Nudged == key || p.Nudges >= max {
		return p.Nudges, false
	}
	p.Nudged, p.Nudges = key, p.Nudges+1
	b.chats[id] = p
	b.save()
	return p.Nudges, true
}
