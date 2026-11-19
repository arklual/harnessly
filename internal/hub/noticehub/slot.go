package noticehub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/notices"
)

// The slot of a machine's smart notice in the hub: the agent sends every question open on the machine each
// time the slot moves (hubproto.NoticeSlot); the hub keeps the ones that wait for an answer in its database, under the
// machine's own person (SlotPerson), and tells the streams that the slot moved. With the switch on, the page's stream
// of the slot is the hub's: one stream per person, whichever replica it is open on. The page still reads the list and
// answers on the machine, where the question waits.

var slotStream = regexp.MustCompile(`^/api/[A-Za-z0-9_-]+/notices/stream$`)

// SlotPerson is the person the hub keeps a machine's open questions under.
func SlotPerson(machineID string) string { return "slot:" + machineID }

// slotMark is the settings the hub writes for a machine the first time its agent sends the slot: from then on the hub
// answers the machine's slot stream itself (an older agent never sends it, so its page keeps the machine's stream).
var slotMark = json.RawMessage(`{"feeds":true}`)

type slotState struct {
	mu sync.Mutex
	// open are the waiting questions each machine had at its last list, as this replica wrote them.
	open map[string][]string
	// feeds are the machines known to send their slot.
	feeds map[string]bool
}

// Slot takes a machine's `noticeslot` event (machines.NoticeSlotSink).
func (s *Service) Slot(ctx context.Context, m store.Machine, raw json.RawMessage) {
	if s == nil || s.Notices == nil || m.ID == "" {
		return
	}
	var e hubproto.NoticeSlot
	if json.Unmarshal(raw, &e) != nil {
		return
	}
	person := SlotPerson(m.ID)
	var waiting []hubproto.NoticeSlotItem
	for _, n := range e.Notices {
		var head struct {
			Kind string `json:"kind"`
		}
		if n.ID == "" || len(n.ID) > notices.MaxKey || json.Unmarshal(n.Doc, &head) != nil {
			continue
		}
		if head.Kind != "status" && head.Kind != "progress" {
			waiting = append(waiting, n)
		}
	}
	ids := make([]string, 0, len(waiting))
	for _, n := range waiting {
		ids = append(ids, n.ID)
	}
	slices.Sort(ids)

	s.slot.mu.Lock()
	if s.slot.open == nil {
		s.slot.open, s.slot.feeds = map[string][]string{}, map[string]bool{}
	}
	was, known := s.slot.open[m.ID]
	marked := s.slot.feeds[m.ID]
	s.slot.mu.Unlock()

	if !marked {
		if _, err := s.Notices.Store.SaveSettings(ctx, person, slotMark, 0, nowMs(s)); err == nil || errors.Is(err, notices.ErrConflict) {
			s.markFeeds(m.ID)
		}
	}
	if !known || !slices.Equal(was, ids) {
		if !s.writeSlot(ctx, person, waiting, ids) {
			return
		}
		s.slot.mu.Lock()
		s.slot.open[m.ID] = ids
		s.slot.mu.Unlock()
	}
	// Every list is news, a progress too: the page reads the slot again.
	s.Notices.Tell(person, notices.Event{Type: notices.EventSlot})
}

// writeSlot puts the questions that are new and reads the ones that are gone; false when the database failed.
func (s *Service) writeSlot(ctx context.Context, person string, waiting []hubproto.NoticeSlotItem, ids []string) bool {
	st := s.Notices.Store
	have, err := st.Items(ctx, person, notices.MaxItems)
	if err != nil {
		return false
	}
	held := map[string]bool{}
	var gone []string
	for _, it := range have {
		held[it.Key] = true
		if _, found := slices.BinarySearch(ids, it.Key); !found {
			gone = append(gone, it.Key)
		}
	}
	for _, n := range waiting {
		if held[n.ID] || len(n.Doc) > notices.MaxDoc {
			continue
		}
		if _, err := st.Put(ctx, person, notices.Item{Key: n.ID, Chat: n.Conversation, At: n.CreatedAt, Doc: n.Doc}); err != nil {
			return false
		}
	}
	for len(gone) > 0 {
		k := min(len(gone), notices.MaxReadKeys)
		if _, err := st.MarkRead(ctx, person, gone[:k], nowMs(s)); err != nil {
			return false
		}
		gone = gone[k:]
	}
	return true
}

func (s *Service) markFeeds(machineID string) {
	s.slot.mu.Lock()
	if s.slot.feeds == nil {
		s.slot.open, s.slot.feeds = map[string][]string{}, map[string]bool{}
	}
	s.slot.feeds[machineID] = true
	s.slot.mu.Unlock()
}

// feeds tells a machine whose agent sends its slot: what this replica saw, else the mark in the database.
func (s *Service) feeds(ctx context.Context, machineID string) bool {
	s.slot.mu.Lock()
	known := s.slot.feeds[machineID]
	s.slot.mu.Unlock()
	if known {
		return true
	}
	set, err := s.Notices.Store.Settings(ctx, SlotPerson(machineID))
	if err != nil || set.Rev == 0 {
		return false
	}
	s.markFeeds(machineID)
	return true
}

// serveSlotStream answers the page's stream of the machine's slot from the hub's bus; false leaves it to the machine.
func (s *Service) serveSlotStream(w http.ResponseWriter, r *http.Request, rest string, u store.User, m store.Machine) bool {
	if r.Method != http.MethodGet || !slotStream.MatchString(rest) || s.On == nil {
		return false
	}
	if !s.On(r, strings.ToLower(strings.TrimSpace(u.Email)), m.ID) || !s.feeds(r.Context(), m.ID) {
		return false
	}
	return s.Notices.Stream(w, r, SlotPerson(m.ID))
}

func nowMs(s *Service) int64 {
	if s.Notices.Now != nil {
		return s.Notices.Now().UnixMilli()
	}
	return time.Now().UnixMilli()
}
