package hubtest

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Event is one event of a space's stream.
type Event struct {
	Name string
	Data json.RawMessage
}

// Stream is a person's open event stream of a space; it keeps every event it receives.
type Stream struct {
	p      *Person
	mu     sync.Mutex
	events []Event
	wake   chan struct{}
	cancel context.CancelFunc
	// Status is the HTTP status of the stream request.
	Status int
}

// Events opens the space's event stream as this person and waits until the hub has registered it, so that nothing
// published afterwards is missed.
func (p *Person) Events(spaceID string) *Stream {
	p.S.T.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	st := &Stream{p: p, wake: make(chan struct{}, 1), cancel: cancel}
	p.S.T.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.S.URL+"/api/spaces/"+spaceID+"/events", nil)
	if err != nil {
		p.S.T.Fatal(err)
	}
	res, err := p.Client.Do(req)
	if err != nil {
		p.S.T.Fatal(err)
	}
	st.Status = res.StatusCode
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return st
	}
	go func() {
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
		var name string
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				st.mu.Lock()
				st.events = append(st.events, Event{Name: name, Data: json.RawMessage(strings.TrimPrefix(line, "data: "))})
				st.mu.Unlock()
				select {
				case st.wake <- struct{}{}:
				default:
				}
			}
		}
	}()
	return st
}

// Close ends the stream.
func (s *Stream) Close() { s.cancel() }

// All are the events received so far.
func (s *Stream) All() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Event(nil), s.events...)
}

// Named are the events of one kind, in order.
func (s *Stream) Named(name string) []Event {
	var out []Event
	for _, e := range s.All() {
		if e.Name == name {
			out = append(out, e)
		}
	}
	return out
}

// Await waits until an event satisfies cond (looking at those already received first) and returns it.
func (s *Stream) Await(what string, cond func(Event) bool) Event {
	s.p.S.T.Helper()
	deadline := time.After(8 * time.Second)
	seen := 0
	for {
		all := s.All()
		for ; seen < len(all); seen++ {
			if cond(all[seen]) {
				return all[seen]
			}
		}
		select {
		case <-s.wake:
		case <-deadline:
			s.p.S.T.Fatalf("timed out waiting for %s; received %d events", what, len(all))
			return Event{}
		}
	}
}
