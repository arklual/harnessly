package noticehub

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ly-solutions/harnessly/internal/notices"
)

// relayRoom is the longest event a replica passes as it is; a longer one goes without its document, and the replicas
// that hear it read the document from the database (the relay's own limit is a little higher).
const relayRoom = 6000

// Relay joins the notices of the hub's replicas: what a person was told on this replica (a new notice, a
// read, the switches, a machine's slot) reaches their streams open on the others. send passes an event to the other
// replicas; the returned deliver gives theirs to this replica's streams, without passing it on again.
func (s *Service) Relay(send func(person string, data json.RawMessage)) (deliver func(person string, data json.RawMessage)) {
	if s == nil || s.Notices == nil || send == nil {
		return func(string, json.RawMessage) {}
	}
	s.Notices.OnChange = func(person string, e notices.Event) {
		for _, raw := range relayed(e) {
			send(person, raw)
		}
	}
	return func(person string, data json.RawMessage) {
		var e notices.Event
		if json.Unmarshal(data, &e) != nil || e.Type == "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if !s.fill(ctx, person, &e) {
			return
		}
		s.Notices.Bus.Publish(person, e)
	}
}

// relayed is the event as the other replicas get it: whole when it fits, else without its documents (they read them)
// or, for a long read, in parts.
func relayed(e notices.Event) []json.RawMessage {
	raw, err := json.Marshal(e)
	if err != nil {
		return nil
	}
	if len(raw) <= relayRoom {
		return []json.RawMessage{raw}
	}
	switch {
	case e.Item != nil:
		it := *e.Item
		it.Doc = nil
		e.Item = &it
	case e.Settings != nil:
		e.Settings = &notices.Settings{Rev: e.Settings.Rev}
	case len(e.Keys) > 1:
		half := len(e.Keys) / 2
		first, second := e, e
		first.Keys, second.Keys = e.Keys[:half], e.Keys[half:]
		return append(relayed(first), relayed(second)...)
	default:
		return nil
	}
	if raw, err = json.Marshal(e); err != nil || len(raw) > relayRoom {
		return nil
	}
	return []json.RawMessage{raw}
}

// fill reads from the database the document a short event came without; false when it is no longer there.
func (s *Service) fill(ctx context.Context, person string, e *notices.Event) bool {
	empty := func(d json.RawMessage) bool { return len(d) == 0 || string(d) == "null" }
	switch {
	case e.Item != nil && empty(e.Item.Doc):
		items, err := s.Notices.Store.Items(ctx, person, notices.MaxItems)
		if err != nil {
			return false
		}
		for _, it := range items {
			if it.Key == e.Item.Key {
				e.Item = &it
				return true
			}
		}
		return false
	case e.Settings != nil && empty(e.Settings.Doc):
		set, err := s.Notices.Store.Settings(ctx, person)
		if err != nil {
			return false
		}
		e.Settings = &set
	}
	return true
}
