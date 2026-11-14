package notices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Service is the store with the bus: the one thing a hub or a runtime hands its page.
type Service struct {
	Store Store
	Bus   *Bus
	Now   func() time.Time
	// OnChange (may be nil) hears every change, for a hub's replica to tell the others.
	OnChange func(person string, e Event)
}

// New makes a service over a store.
func New(st Store) *Service { return &Service{Store: st, Bus: &Bus{}} }

func (s *Service) emit(person string, e Event) {
	s.Bus.Publish(person, e)
	if s.OnChange != nil {
		s.OnChange(person, e)
	}
}

// State is what a page reads when it opens: unread items, what was read lately, the switches.
type State struct {
	Items    []Item   `json:"items"`
	Reads    []Read   `json:"reads"`
	Settings Settings `json:"settings"`
}

func (s *Service) State(ctx context.Context, person string) (State, error) {
	items, err := s.Store.Items(ctx, person, MaxItems)
	if err != nil {
		return State{}, err
	}
	reads, err := s.Store.Reads(ctx, person, KeepReads)
	if err != nil {
		return State{}, err
	}
	set, err := s.Store.Settings(ctx, person)
	if err != nil {
		return State{}, err
	}
	return State{Items: items, Reads: reads, Settings: set}, nil
}

// Add keeps an item and tells the person's devices; a repeat of a key is no news.
func (s *Service) Add(ctx context.Context, person string, it Item, from string) (bool, error) {
	fresh, err := s.Store.Put(ctx, person, it)
	if err != nil || !fresh {
		return false, err
	}
	s.emit(person, Event{Type: EventNew, Item: &it, From: from})
	return true, nil
}

// ReadReq says what was read: keys, every item of a chat, or all.
type ReadReq struct {
	Keys []string `json:"keys"`
	Chat string   `json:"chatId"`
	All  bool     `json:"all"`
	From string   `json:"from"`
}

func (s *Service) Read(ctx context.Context, person string, r ReadReq) ([]string, error) {
	if len(r.Keys) > MaxReadKeys {
		return nil, ErrArgs
	}
	at := nowMs(s.Now)
	var marked []string
	var err error
	switch {
	case r.All:
		marked, err = s.Store.ReadAll(ctx, person, at)
	case r.Chat != "":
		marked, err = s.Store.ReadChat(ctx, person, r.Chat, at)
	default:
		marked, err = s.Store.MarkRead(ctx, person, r.Keys, at)
	}
	if err != nil {
		return nil, err
	}
	if len(marked) > 0 {
		s.emit(person, Event{Type: EventRead, Keys: marked, From: r.From})
	}
	return marked, nil
}

func (s *Service) SaveSettings(ctx context.Context, person string, doc json.RawMessage, fromRev int64, from string) (Settings, error) {
	set, err := s.Store.SaveSettings(ctx, person, doc, fromRev, nowMs(s.Now))
	if err != nil {
		return Settings{}, err
	}
	s.emit(person, Event{Type: EventSettings, Settings: &set, From: from})
	return set, nil
}

// Handler serves the page's routes under one prefix (the caller strips it and has already decided who the person is):
//
//	GET  /            the state (items, reads, settings)
//	POST /items       {key, chatId, at, doc, from}           a new notice
//	POST /read        {keys | chatId | all, from}            read or taken away
//	PUT  /settings    {doc, rev, from}                       the switches
//	GET  /stream      text/event-stream of the person's events
func (s *Service) Handler(personOf func(*http.Request) (string, bool)) http.Handler {
	mux := http.NewServeMux()
	jsonOut := func(w http.ResponseWriter, code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	fail := func(w http.ResponseWriter, err error) {
		switch {
		case errors.Is(err, ErrArgs):
			jsonOut(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
		case errors.Is(err, ErrConflict):
			jsonOut(w, http.StatusConflict, map[string]string{"error": "conflict"})
		default:
			jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		}
	}
	guard := func(method string, h func(w http.ResponseWriter, r *http.Request, person string)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method != method {
				jsonOut(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
				return
			}
			person, ok := personOf(r)
			if !ok || person == "" {
				jsonOut(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
				return
			}
			h(w, r, person)
		}
	}
	body := func(r *http.Request, v any) bool {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 2*MaxDoc))
		return err == nil && json.Unmarshal(raw, v) == nil
	}
	mux.HandleFunc("/{$}", guard(http.MethodGet, func(w http.ResponseWriter, r *http.Request, person string) {
		st, err := s.State(r.Context(), person)
		if err != nil {
			fail(w, err)
			return
		}
		jsonOut(w, http.StatusOK, st)
	}))
	mux.HandleFunc("/items", guard(http.MethodPost, func(w http.ResponseWriter, r *http.Request, person string) {
		var b struct {
			Item
			From string `json:"from"`
		}
		if !body(r, &b) {
			fail(w, ErrArgs)
			return
		}
		fresh, err := s.Add(r.Context(), person, b.Item, b.From)
		if err != nil {
			fail(w, err)
			return
		}
		jsonOut(w, http.StatusOK, map[string]bool{"fresh": fresh})
	}))
	mux.HandleFunc("/read", guard(http.MethodPost, func(w http.ResponseWriter, r *http.Request, person string) {
		var b ReadReq
		if !body(r, &b) {
			fail(w, ErrArgs)
			return
		}
		marked, err := s.Read(r.Context(), person, b)
		if err != nil {
			fail(w, err)
			return
		}
		jsonOut(w, http.StatusOK, map[string]any{"keys": marked})
	}))
	mux.HandleFunc("/settings", guard(http.MethodPut, func(w http.ResponseWriter, r *http.Request, person string) {
		var b struct {
			Doc  json.RawMessage `json:"doc"`
			Rev  int64           `json:"rev"`
			From string          `json:"from"`
		}
		if !body(r, &b) {
			fail(w, ErrArgs)
			return
		}
		set, err := s.SaveSettings(r.Context(), person, b.Doc, b.Rev, b.From)
		if err != nil {
			fail(w, err)
			return
		}
		jsonOut(w, http.StatusOK, set)
	}))
	mux.HandleFunc("/stream", guard(http.MethodGet, func(w http.ResponseWriter, r *http.Request, person string) {
		if !s.Stream(w, r, person) {
			jsonOut(w, http.StatusInternalServerError, map[string]string{"error": "no-stream"})
		}
	}))
	return mux
}

// Stream holds the response open as a text/event-stream of the person's events until the request ends; false when the
// response cannot stream.
func (s *Service) Stream(w http.ResponseWriter, r *http.Request, person string) bool {
	fl, ok := w.(http.Flusher)
	if !ok {
		return false
	}
	events, stop := s.Bus.Subscribe(person)
	defer stop()
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	fmt.Fprint(w, "retry: 3000\nevent: hello\ndata: {}\n\n")
	fl.Flush()
	beat := time.NewTicker(20 * time.Second)
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return true
		case <-beat.C:
			fmt.Fprint(w, ": beat\n\n")
			fl.Flush()
		case e, ok := <-events:
			if !ok {
				return true
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, e.json())
			fl.Flush()
		}
	}
}
