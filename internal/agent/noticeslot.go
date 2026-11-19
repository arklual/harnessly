package agent

import (
	"bytes"
	"encoding/json"
	"sync"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The slot of the smart notice (hubproto.NoticeSlot): the runtime's replica socket says every open question
// ({"k":"slot"}) when it opens and each time the slot moves; the agent keeps the newest list and forwards it to the hub
// links that take `noticeslot`. A link that subscribes gets the list kept, so a hub that was away sees what is open now.

type slotCache struct {
	mu    sync.Mutex
	frame []byte // the hub frame of the newest list; nil before the runtime said one
}

// take keeps a replica frame's list as the hub frame; nil when it is no news (the same list again, or not a list).
func (c *slotCache) take(frame []byte) []byte {
	var f struct {
		Notices []hubproto.NoticeSlotItem `json:"notices"`
	}
	if json.Unmarshal(frame, &f) != nil {
		return nil
	}
	kept := make([]hubproto.NoticeSlotItem, 0, min(len(f.Notices), hubproto.NoticeSlotMax))
	for _, n := range f.Notices {
		if n.ID != "" && len(kept) < hubproto.NoticeSlotMax {
			kept = append(kept, n)
		}
	}
	out, err := marshal(map[string]any{"event": hubproto.EventNoticeSlot, "notices": kept})
	if err != nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if bytes.Equal(c.frame, out) {
		return nil
	}
	c.frame = out
	return out
}

func (c *slotCache) last() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.frame
}

// takeSlot is a `slot` frame of the runtime: kept, and sent to the hub links that take it when it changed.
func (h *hub) takeSlot(frame []byte) {
	out := h.slot.take(frame)
	if out == nil {
		return
	}
	for _, l := range h.hubLinks() {
		if l.wants(hubproto.EventNoticeSlot) {
			l.sendText(out)
		}
	}
}

// sendSlot sends a link the list kept: it has just subscribed.
func (h *hub) sendSlot(l *link) {
	if f := h.slot.last(); f != nil {
		l.sendText(f)
	}
}
