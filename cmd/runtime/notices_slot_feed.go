package main

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/noticeslot"
)

// slotFeedDelay gathers the moves of the slot that come together (a question opened and its queue moved) into one list.
const slotFeedDelay = 250 * time.Millisecond

// slotFeed hands the agent the smart notice's open questions over the replica socket ({"k":"slot"}): the list
// now when the socket opens, then the whole list again each time the slot moves. The agent forwards it to the hub,
// which keeps the waiting questions and tells the person's pages. It is a wire.WorkFeed.
type slotFeed struct {
	slot *noticeslot.Slot

	mu       sync.Mutex
	watchers map[int]func([]byte)
	next     int
	timer    *time.Timer
}

// newSlotFeed follows the slot's moves. Changed is called under the slot's lock, so the list is read later, not there.
func newSlotFeed(slot *noticeslot.Slot) *slotFeed {
	f := &slotFeed{slot: slot, watchers: map[int]func([]byte){}}
	prev := slot.Changed
	slot.Changed = func() {
		if prev != nil {
			prev()
		}
		f.moved()
	}
	return f
}

func (f *slotFeed) moved() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.watchers) == 0 || f.timer != nil {
		return
	}
	f.timer = time.AfterFunc(slotFeedDelay, f.send)
}

func (f *slotFeed) send() {
	f.mu.Lock()
	f.timer = nil
	fns := make([]func([]byte), 0, len(f.watchers))
	for _, fn := range f.watchers {
		fns = append(fns, fn)
	}
	f.mu.Unlock()
	if len(fns) == 0 {
		return
	}
	frame := f.frame()
	for _, fn := range fns {
		fn(frame)
	}
}

// frame is the list as the replica socket says it: every open question (a status and a progress too, so each move is
// a new list), without a view of its own.
func (f *slotFeed) frame() []byte {
	pending := f.slot.Pending()
	items := make([]hubproto.NoticeSlotItem, 0, min(len(pending), hubproto.NoticeSlotMax))
	for _, n := range pending {
		if len(items) == hubproto.NoticeSlotMax {
			break
		}
		n.View = ""
		doc, err := json.Marshal(n)
		if err != nil {
			continue
		}
		items = append(items, hubproto.NoticeSlotItem{ID: n.ID, Conversation: n.Conversation, CreatedAt: n.CreatedAt, Doc: doc})
	}
	out, _ := json.Marshal(map[string]any{"k": "slot", "notices": items})
	return out
}

// WorkFrames is the list now (wire.WorkFeed).
func (f *slotFeed) WorkFrames() [][]byte { return [][]byte{f.frame()} }

// WatchWork calls fn with each new list until stop (wire.WorkFeed).
func (f *slotFeed) WatchWork(fn func(frame []byte)) (stop func()) {
	f.mu.Lock()
	id := f.next
	f.next++
	f.watchers[id] = fn
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		delete(f.watchers, id)
		f.mu.Unlock()
	}
}
