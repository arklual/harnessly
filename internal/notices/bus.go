package notices

import (
	"encoding/json"
	"sync"
)

// Event kinds a stream carries.
const (
	EventNew      = "new"      // an item arrived
	EventRead     = "read"     // keys were read or taken away, on some device of the person
	EventSettings = "settings" // the switches changed
)

// Event is what a person's devices are told.
type Event struct {
	Type string `json:"type"`
	// Item is the new item (EventNew).
	Item *Item `json:"item,omitempty"`
	// Keys are the keys read (EventRead).
	Keys     []string  `json:"keys,omitempty"`
	Settings *Settings `json:"settings,omitempty"`
	// From is the device that made the change; the page that sent it can skip its own echo.
	From string `json:"from,omitempty"`
}

// Bus delivers a person's events to the streams that are open for them, in this process.
type Bus struct {
	mu   sync.Mutex
	subs map[string]map[chan Event]struct{}
}

// Subscribe opens a feed of the person's events; stop closes it. A slow reader loses events (its page asks the list
// again when its stream reconnects), never blocks the writer.
func (b *Bus) Subscribe(person string) (events <-chan Event, stop func()) {
	ch := make(chan Event, 32)
	b.mu.Lock()
	if b.subs == nil {
		b.subs = map[string]map[chan Event]struct{}{}
	}
	if b.subs[person] == nil {
		b.subs[person] = map[chan Event]struct{}{}
	}
	b.subs[person][ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.subs[person][ch]; ok {
			delete(b.subs[person], ch)
			close(ch)
		}
		if len(b.subs[person]) == 0 {
			delete(b.subs, person)
		}
	}
}

// Publish tells the person's open streams.
func (b *Bus) Publish(person string, e Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs[person] {
		select {
		case ch <- e:
		default:
		}
	}
}

// Listeners counts the open streams of a person (tests).
func (b *Bus) Listeners(person string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs[person])
}

func (e Event) json() []byte {
	raw, _ := json.Marshal(e)
	return raw
}
