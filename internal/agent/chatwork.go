package agent

import (
	"encoding/json"
	"sort"
	"sync"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The work of the chats (hubproto.ChatWork): the runtime's replica socket says it ({"k":"work"}), the agent keeps the
// newest record of each chat's worktree and forwards it to the hub links that take `chatwork`. A link that subscribes
// gets everything kept, so a hub that was away misses nothing the runtime said while the agent ran.

// workKeepMax bounds the records kept; the ones looked at longest ago go first.
const workKeepMax = 5000

type workCache struct {
	mu      sync.Mutex
	entries map[string]hubproto.ChatWorkEntry
}

// take keeps the records of a replica frame and returns the ones newer than what was kept.
func (c *workCache) take(frame []byte) []hubproto.ChatWorkEntry {
	var f struct {
		Entries []hubproto.ChatWorkEntry `json:"entries"`
	}
	if json.Unmarshal(frame, &f) != nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]hubproto.ChatWorkEntry{}
	}
	var fresh []hubproto.ChatWorkEntry
	for _, e := range f.Entries {
		if e.Conv == "" || e.Worktree == "" {
			continue
		}
		k := e.Conv + "\x00" + e.Worktree
		if old, ok := c.entries[k]; ok && old.At >= e.At {
			continue
		}
		c.entries[k] = e
		fresh = append(fresh, e)
	}
	if len(c.entries) > workKeepMax {
		all := c.listLocked()
		for _, e := range all[workKeepMax:] {
			delete(c.entries, e.Conv+"\x00"+e.Worktree)
		}
	}
	return fresh
}

// list is every record kept, the newest look first.
func (c *workCache) list() []hubproto.ChatWorkEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.listLocked()
}

func (c *workCache) listLocked() []hubproto.ChatWorkEntry {
	out := make([]hubproto.ChatWorkEntry, 0, len(c.entries))
	for _, e := range c.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At > out[j].At })
	return out
}

// workFrames are the hub frames of records, at most hubproto.ChatWorkFrameMax each.
func workFrames(list []hubproto.ChatWorkEntry) [][]byte {
	var out [][]byte
	for len(list) > 0 {
		n := min(len(list), hubproto.ChatWorkFrameMax)
		raw, err := json.Marshal(hubproto.ChatWork{Entries: list[:n]})
		list = list[n:]
		if err != nil {
			continue
		}
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		m["event"], _ = json.Marshal(hubproto.EventChatWork)
		if frame, err := marshal(m); err == nil {
			out = append(out, frame)
		}
	}
	return out
}

// takeWork is a `work` frame of the runtime: kept, and what is new sent to the hub links that take it.
func (h *hub) takeWork(frame []byte) {
	fresh := h.work.take(frame)
	if len(fresh) == 0 {
		return
	}
	frames := workFrames(fresh)
	for _, l := range h.hubLinks() {
		if l.wants(hubproto.EventChatWork) {
			for _, f := range frames {
				l.sendText(f)
			}
		}
	}
}

// sendWork sends a link everything kept: it has just subscribed.
func (h *hub) sendWork(l *link) {
	for _, f := range workFrames(h.work.list()) {
		l.sendText(f)
	}
}
