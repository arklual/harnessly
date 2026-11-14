// Package notices keeps the person's notifications (stage 4 of «the hub is the source of truth»): the
// notices of the window (a chat waits, a turn is done, a failure, a limit), which of them the person has read or taken
// away, and the switches of Settings → Notifications. One package with one storage interface, built into the hub
// (Postgres, behind the switch hub-notices) and into the runtime (SQLite beside its data, or Postgres): the page asks
// one path and whoever is there answers. What the page keeps in localStorage is a cache of this.
//
// A read on one device is an event to every device of the person: Bus carries «new», «read» and «settings» to the
// streams the pages hold open (Handler serves them as text/event-stream), so a page needs no poll while its stream lives.
package notices

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Item is one notice of the window as the page makes it: the chat and what happened in it. Key is the event (a repeat
// of a key is no news); Doc is the rest of the page's notice, opaque to the store.
type Item struct {
	Key  string          `json:"key"`
	Chat string          `json:"chatId"`
	At   int64           `json:"at"`
	Doc  json.RawMessage `json:"doc"`
}

// Read is the person taking a notice away or reading it: from then on no device of theirs shows it.
type Read struct {
	Key string `json:"key"`
	At  int64  `json:"at"`
}

// Settings is the person's switches document at a revision; revision 0 is «never saved».
type Settings struct {
	Doc json.RawMessage `json:"doc"`
	Rev int64           `json:"rev"`
}

// Limits.
const (
	MaxItems    = 60       // notices of a person the store lists (the window shows fewer)
	MaxDoc      = 16 << 10 // a notice's or the switches' JSON
	MaxKey      = 200
	KeepReads   = 400 // reads kept per person: older ones are no news to any device
	MaxReadKeys = 100 // keys in one read call
)

var (
	ErrArgs     = errors.New("notices: bad request")
	ErrConflict = errors.New("notices: the settings changed meanwhile")
)

// Store is where notices live. Every method is safe for concurrent use.
type Store interface {
	// Put keeps an item; the same (person, key) twice changes nothing and says false.
	Put(ctx context.Context, person string, it Item) (fresh bool, err error)
	// Items are the person's unread items, newest first, at most limit.
	Items(ctx context.Context, person string, limit int) ([]Item, error)
	// MarkRead records the reads (a key already read keeps its first time) and says which keys were new reads.
	MarkRead(ctx context.Context, person string, keys []string, at int64) (marked []string, err error)
	// ReadChat reads every unread item of a chat and says its keys.
	ReadChat(ctx context.Context, person, chat string, at int64) (marked []string, err error)
	// ReadAll reads every unread item of the person.
	ReadAll(ctx context.Context, person string, at int64) (marked []string, err error)
	// Reads are the person's latest reads, newest first.
	Reads(ctx context.Context, person string, limit int) ([]Read, error)
	Settings(ctx context.Context, person string) (Settings, error)
	// SaveSettings saves over the revision the caller read (0 for never saved); ErrConflict when it moved on.
	SaveSettings(ctx context.Context, person string, doc json.RawMessage, fromRev int64, at int64) (Settings, error)
	// Prune forgets reads and read items older than before (unix ms).
	Prune(ctx context.Context, before int64) error
}

// Now is the clock in unix milliseconds.
func nowMs(now func() time.Time) int64 {
	if now == nil {
		return time.Now().UnixMilli()
	}
	return now().UnixMilli()
}
