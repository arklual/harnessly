package prompts

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
)

// Item is a message waiting in a tab's queue: sent while the agent worked, or before it was up.
type Item struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	// Images are uploaded pictures (absolute paths), pasted before the text.
	Images    []string `json:"images"`
	CreatedAt int64    `json:"createdAt"`
	// ClientID is the page's own key of the message: an add that repeats it gets the message it queued, not a second.
	ClientID string `json:"clientId,omitempty"`
	// Status is StatusFailed once delivery gave up on the message (Reason says why): it stays in the queue for the person
	// to retry or drop, and nothing types it by itself. A waiting message has none.
	Status string `json:"status,omitempty"`
	Reason string `json:"reason,omitempty"`
	// RetriedAt is when the person last asked for a failed message again; its lifetime counts from then.
	RetriedAt int64 `json:"retriedAt,omitempty"`
}

// StatusFailed marks a message delivery gave up on.
const StatusFailed = "failed"

// Why delivery gave up on a message.
const (
	// ReasonLaunchFailed: the tab's agent launch did not come up, and nothing launches it again by itself.
	ReasonLaunchFailed = "launch-failed"
	// ReasonNoSession: the agent is gone and has no session to resume.
	ReasonNoSession = "no-session"
	// ReasonNoTab: the runtime does not know the tab, or the tab has no tmux session.
	ReasonNoTab = "no-tab"
	// ReasonExpired: the message waited longer than QueuedLifetime.
	ReasonExpired = "expired"
	// ReasonNotDelivered: typing the message failed maxDeliveryTries times in a row.
	ReasonNotDelivered = "not-delivered"
	// ReasonInputOccupied: the agent's input box held someone else's text for occupiedLimit.
	ReasonInputOccupied = "input-occupied"
)

// Waiting: the message is still to be delivered.
func (it Item) Waiting() bool { return it.Status != StatusFailed }

const (
	// MaxQueuedPerTab is the most messages one tab may hold.
	MaxQueuedPerTab = 20
	// MaxQueuedText is the longest text kept, in UTF-16 units; a longer one is cut, not refused.
	MaxQueuedText = 100_000
	saveDelay     = 200 * time.Millisecond
	// QueuedLifetime is how long a message waits for its agent before delivery gives up on it.
	QueuedLifetime = 24 * time.Hour
	// clientIDWindow is how long an add's client id is remembered after its message left the queue.
	clientIDWindow = 10 * time.Minute
)

// QueueError is a message the queue refuses; its text is shown to the person.
type QueueError struct{ Message string }

func (e *QueueError) Error() string { return e.Message }

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
	added    map[clientKey]addedItem
	quiet    map[string]bool
	saveT    Timer
	onChange func(tabID string, items []Item)
}

// NewQueue loads the queue file; "" keeps the queue in memory only. A missing or unreadable file starts empty, and
// entries of the wrong shape are dropped.
func NewQueue(file string, clock Clock) *Queue {
	q := &Queue{file: file, clock: clock, queues: map[string][]Item{}, added: map[clientKey]addedItem{}, quiet: map[string]bool{}, warn: func(string, ...any) {}}
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

// decodeItem accepts an entry with a string id and text, a numeric createdAt and string images; the later fields are
// optional and kept only when they are of their type.
func decodeItem(raw json.RawMessage) (Item, bool) {
	var e struct {
		ID        *string            `json:"id"`
		Text      *string            `json:"text"`
		CreatedAt *json.Number       `json:"createdAt"`
		Images    *[]json.RawMessage `json:"images"`
		ClientID  json.RawMessage    `json:"clientId"`
		Status    json.RawMessage    `json:"status"`
		Reason    json.RawMessage    `json:"reason"`
		RetriedAt json.RawMessage    `json:"retriedAt"`
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
	it := Item{ID: *e.ID, Text: *e.Text, Images: images, CreatedAt: int64(created)}
	it.ClientID, _ = jsonString(e.ClientID)
	if status, _ := jsonString(e.Status); status == StatusFailed {
		it.Status = status
		it.Reason, _ = jsonString(e.Reason)
	}
	var retried float64
	if json.Unmarshal(e.RetriedAt, &retried) == nil {
		it.RetriedAt = int64(retried)
	}
	return it, true
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
	quiet := q.quiet[tabID]
	q.mu.Unlock()
	if listener != nil && !quiet {
		listener(tabID, items)
	}
	return true
}

// Quietly runs f, a delivery attempt that takes the tab's head out and puts it back unless it went in, telling the
// listener once afterwards and only when the queue ended up different: the pages do not see a message the screen
// refused leave and come back on every try. A change made meanwhile (an add from a page) is told then too. Attempts on
// one tab do not overlap (the tab's single flight).
func (q *Queue) Quietly(tabID string, f func()) {
	before := q.List(tabID)
	q.mu.Lock()
	q.quiet[tabID] = true
	q.mu.Unlock()
	defer func() {
		q.notify.Lock()
		defer q.notify.Unlock()
		q.mu.Lock()
		delete(q.quiet, tabID)
		listener, after := q.onChange, copyItems(q.queues[tabID])
		q.mu.Unlock()
		if listener != nil && !reflect.DeepEqual(before, after) && (len(before) > 0 || len(after) > 0) {
			listener(tabID, after)
		}
	}()
	f()
}

// Waiting says whether the tab holds a message still to be delivered.
func (q *Queue) Waiting(tabID string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, it := range q.queues[tabID] {
		if it.Waiting() {
			return true
		}
	}
	return false
}

type clientKey struct{ tab, id string }

type addedItem struct {
	item Item
	at   time.Time
}

// Add appends a message. Text past MaxQueuedText is cut; a blank text without pictures and a full queue are refused.
func (q *Queue) Add(tabID, text string, images []string) (Item, error) {
	item, _, err := q.AddOnce(tabID, "", text, images)
	return item, err
}

// AddOnce is Add keyed by the page's client id: a repeat of an id the tab took within clientIDWindow (its answer was
// lost on the way, say) gives back the message it made, repeated true, and queues nothing. "" is never a repeat.
func (q *Queue) AddOnce(tabID, clientID, text string, images []string) (Item, bool, error) {
	text = jscompat.Slice(text, MaxQueuedText)
	clientID = jscompat.Slice(clientID, composerIDMax)
	now := q.clock.Now()
	item := Item{ID: "q-" + strconv.FormatInt(now.UnixMilli(), 36) + "-" + randomBase36(6), Text: text,
		Images: append([]string{}, images...), CreatedAt: now.UnixMilli(), ClientID: clientID}
	var err error
	var earlier *Item
	q.change(tabID, func(items []Item) ([]Item, bool) {
		for k, a := range q.added {
			if now.Sub(a.at) >= clientIDWindow {
				delete(q.added, k)
			}
		}
		if clientID != "" {
			for _, it := range items {
				if it.ClientID == clientID {
					earlier = &it
					return nil, false
				}
			}
			if a, seen := q.added[clientKey{tabID, clientID}]; seen {
				earlier = &a.item
				return nil, false
			}
		}
		switch {
		case len(items) >= MaxQueuedPerTab:
			err = &QueueError{msgQueueFull.String()}
		case blank(text) && len(images) == 0:
			err = &QueueError{msgQueueEmpty.String()}
		default:
			if clientID != "" {
				q.added[clientKey{tabID, clientID}] = addedItem{item, now}
			}
			return append(copyItems(items), item), true
		}
		return nil, false
	})
	switch {
	case earlier != nil:
		return *earlier, true, nil
	case err != nil:
		return Item{}, false, err
	}
	return item, false, nil
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

// Restore puts a message back at the head of what waits after a delivery the screen refused (failed messages before it
// keep their place), unless one with its id is queued.
func (q *Queue) Restore(tabID string, item Item) {
	q.change(tabID, func(items []Item) ([]Item, bool) {
		at := len(items)
		for i, it := range items {
			if it.ID == item.ID {
				return nil, false
			}
			if it.Waiting() && at == len(items) {
				at = i
			}
		}
		return slices.Insert(copyItems(items), at, item), true
	})
}

// Fail gives up on every waiting message of the tab that match accepts (nil: all), for reason; the count of them.
func (q *Queue) Fail(tabID, reason string, match func(Item) bool) int {
	n := 0
	q.change(tabID, func(items []Item) ([]Item, bool) {
		next := copyItems(items)
		for i, it := range next {
			if it.Waiting() && (match == nil || match(it)) {
				next[i].Status, next[i].Reason = StatusFailed, reason
				n++
			}
		}
		return next, n > 0
	})
	return n
}

// forget drops a client id the person took out of the queue themselves: a later add with it is a new message.
func (q *Queue) forget(tabID, clientID string) {
	q.mu.Lock()
	delete(q.added, clientKey{tabID, clientID})
	q.mu.Unlock()
}

// Retry makes a failed message wait again, in its place (one that waits already is left as it is); false when the tab
// has no message with that id.
func (q *Queue) Retry(tabID, id string) (Item, bool) {
	var retried Item
	found := false
	q.change(tabID, func(items []Item) ([]Item, bool) {
		for i, it := range items {
			if it.ID != id {
				continue
			}
			found, retried = true, it
			if it.Waiting() {
				return nil, false
			}
			next := copyItems(items)
			next[i].Status, next[i].Reason, next[i].RetriedAt = "", "", q.clock.Now().UnixMilli()
			retried = next[i]
			return next, true
		}
		return nil, false
	})
	return retried, found
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
