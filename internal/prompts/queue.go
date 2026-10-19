package prompts

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"harnessly/internal/jscompat"
)

// Item is a message waiting in a tab's queue: sent while the agent worked, or before it was up.
type Item struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	// Images are uploaded pictures (absolute paths), pasted before the text.
	Images    []string `json:"images"`
	CreatedAt int64    `json:"createdAt"`
}

const (
	// MaxQueuedPerTab is the most messages one tab may hold.
	MaxQueuedPerTab = 20
	// MaxQueuedText is the longest text kept, in UTF-16 units; a longer one is cut, not refused.
	MaxQueuedText = 100_000
	saveDelay     = 200 * time.Millisecond
)

// QueueError is a message the queue refuses; its text is shown to the person.
type QueueError struct{ Message string }

func (e *QueueError) Error() string { return e.Message }

var (
	errQueueFull  = &QueueError{"В очереди уже слишком много сообщений"}
	errQueueEmpty = &QueueError{"Пустое сообщение"}
)

// Queue is every tab's FIFO of messages, kept in one JSON file ({tabId: [items]}) written 200 ms after the last change.
// A tab without messages has no key.
type Queue struct {
	file  string
	clock Clock
	warn  func(msg string, kv ...any)

	// notify keeps listener calls in the order of the changes.
	notify   sync.Mutex
	mu       sync.Mutex
	queues   map[string][]Item
	saveT    Timer
	onChange func(tabID string, items []Item)
}

// NewQueue loads the queue file; "" keeps the queue in memory only. A missing or unreadable file starts empty, and
// entries of the wrong shape are dropped.
func NewQueue(file string, clock Clock) *Queue {
	q := &Queue{file: file, clock: clock, queues: map[string][]Item{}, warn: func(string, ...any) {}}
	q.load()
	return q
}

// OnChange sets the one listener told every change with a copy of the tab's items (empty once the tab has none).
func (q *Queue) OnChange(f func(tabID string, items []Item)) {
	q.mu.Lock()
	q.onChange = f
	q.mu.Unlock()
}

func (q *Queue) load() {
	if q.file == "" {
		return
	}
	raw, err := os.ReadFile(q.file)
	if err != nil {
		return
	}
	var data map[string]json.RawMessage
	if json.Unmarshal(raw, &data) != nil {
		return
	}
	for tabID, list := range data {
		var entries []json.RawMessage
		if json.Unmarshal(list, &entries) != nil {
			continue
		}
		var valid []Item
		for _, e := range entries {
			if it, ok := decodeItem(e); ok {
				valid = append(valid, it)
			}
		}
		if len(valid) > 0 {
			q.queues[tabID] = valid
		}
	}
}

// decodeItem accepts an entry with a string id and text, a numeric createdAt and string images.
func decodeItem(raw json.RawMessage) (Item, bool) {
	var e struct {
		ID        *string            `json:"id"`
		Text      *string            `json:"text"`
		CreatedAt *json.Number       `json:"createdAt"`
		Images    *[]json.RawMessage `json:"images"`
	}
	if json.Unmarshal(raw, &e) != nil || e.ID == nil || e.Text == nil || e.CreatedAt == nil || e.Images == nil {
		return Item{}, false
	}
	created, err := e.CreatedAt.Float64()
	if err != nil {
		return Item{}, false
	}
	images := make([]string, 0, len(*e.Images))
	for _, im := range *e.Images {
		var s string
		if json.Unmarshal(im, &s) != nil {
			return Item{}, false
		}
		images = append(images, s)
	}
	return Item{ID: *e.ID, Text: *e.Text, Images: images, CreatedAt: int64(created)}, true
}

func copyItems(items []Item) []Item { return append([]Item{}, items...) }

// List is a copy of the tab's queue, oldest first.
func (q *Queue) List(tabID string) []Item {
	q.mu.Lock()
	defer q.mu.Unlock()
	return copyItems(q.queues[tabID])
}

// All is a copy of every non-empty queue.
func (q *Queue) All() map[string][]Item {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make(map[string][]Item, len(q.queues))
	for k, v := range q.queues {
		out[k] = copyItems(v)
	}
	return out
}

// change runs mutate under the lock and, when it reports a change, saves later and tells the listener.
func (q *Queue) change(tabID string, mutate func(items []Item) ([]Item, bool)) bool {
	q.notify.Lock()
	defer q.notify.Unlock()
	q.mu.Lock()
	next, changed := mutate(q.queues[tabID])
	if !changed {
		q.mu.Unlock()
		return false
	}
	if len(next) == 0 {
		delete(q.queues, tabID)
	} else {
		q.queues[tabID] = next
	}
	q.scheduleSave()
	listener, items := q.onChange, copyItems(next)
	q.mu.Unlock()
	if listener != nil {
		listener(tabID, items)
	}
	return true
}

// Add appends a message. Text past MaxQueuedText is cut; a blank text without pictures and a full queue are refused.
func (q *Queue) Add(tabID, text string, images []string) (Item, error) {
	text = jscompat.Slice(text, MaxQueuedText)
	now := q.clock.Now().UnixMilli()
	item := Item{ID: "q-" + strconv.FormatInt(now, 36) + "-" + randomBase36(6), Text: text, Images: append([]string{}, images...), CreatedAt: now}
	var err error
	q.change(tabID, func(items []Item) ([]Item, bool) {
		switch {
		case len(items) >= MaxQueuedPerTab:
			err = errQueueFull
		case blank(text) && len(images) == 0:
			err = errQueueEmpty
		default:
			return append(copyItems(items), item), true
		}
		return nil, false
	})
	if err != nil {
		return Item{}, err
	}
	return item, nil
}

// Take removes a message and returns it: dropped, edited back into the composer, or on its way to the agent.
func (q *Queue) Take(tabID, id string) (Item, bool) {
	var taken Item
	found := q.change(tabID, func(items []Item) ([]Item, bool) {
		for i, it := range items {
			if it.ID == id {
				taken = it
				return append(copyItems(items[:i]), items[i+1:]...), true
			}
		}
		return nil, false
	})
	return taken, found
}

// Restore puts a message back at the head after a delivery the screen refused, unless one with its id is queued.
func (q *Queue) Restore(tabID string, item Item) {
	q.change(tabID, func(items []Item) ([]Item, bool) {
		for _, it := range items {
			if it.ID == item.ID {
				return nil, false
			}
		}
		return append([]Item{item}, items...), true
	})
}

// Drop deletes a tab's queue (the tab was closed).
func (q *Queue) Drop(tabID string) {
	q.change(tabID, func(items []Item) ([]Item, bool) { return nil, len(items) > 0 })
}

func (q *Queue) scheduleSave() {
	if q.file == "" {
		return
	}
	if q.saveT != nil {
		q.saveT.Stop()
	}
	q.saveT = q.clock.AfterFunc(saveDelay, q.save)
}

// Flush writes the file now if a save is pending.
func (q *Queue) Flush() {
	q.mu.Lock()
	pending := q.saveT != nil && q.saveT.Stop()
	q.mu.Unlock()
	if pending {
		q.save()
	}
}

// save writes the file as a temporary file renamed over it, readable by the owner only.
func (q *Queue) save() {
	q.mu.Lock()
	q.saveT = nil
	body, err := json.MarshalIndent(q.queues, "", "  ")
	q.mu.Unlock()
	if err == nil {
		err = os.MkdirAll(filepath.Dir(q.file), 0o700)
	}
	tmp := fmt.Sprintf("%s.%d.tmp", q.file, os.Getpid())
	if err == nil {
		err = os.WriteFile(tmp, body, 0o600)
	}
	if err == nil {
		err = os.Rename(tmp, q.file)
	}
	if err != nil {
		q.warn("saving the prompt queue failed", "err", err.Error())
	}
}

const base36 = "0123456789abcdefghijklmnopqrstuvwxyz"

func randomBase36(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = base36[int(b[i])%len(base36)]
	}
	return string(b)
}
