// Package fileplace is what ties a file open in a chat's pane to the chat's agent: what the person has selected in the
// file, kept here as the page reports it for the agent to read, and the agent's request to show a place in a file.
package fileplace

import (
	"encoding/json"
	"net/http"
	"sync"
)

// maxBody bounds what a page may report: the selection is clipped there, this is only the door.
const maxBody = 256 << 10

// maxKept bounds the chats kept; the map starts over past it, and the next report of a chat puts it back.
const maxKept = 512

// Selection is what is selected in a file, as the page describes it. Content comes out of the file, so it is data.
type Selection struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Place   string `json:"place"`
	What    string `json:"what"`
	Content string `json:"content"`
}

// Store keeps each chat's selection while the page shows it. Only in memory: it means nothing once the page is gone.
type Store struct {
	mu     sync.Mutex
	byChat map[string]Selection
}

// NewStore is an empty store.
func NewStore() *Store { return &Store{byChat: map[string]Selection{}} }

// Put keeps the chat's selection; a nil one takes it away.
func (s *Store) Put(chat string, sel *Selection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sel == nil {
		delete(s.byChat, chat)
		return
	}
	if len(s.byChat) >= maxKept {
		s.byChat = map[string]Selection{}
	}
	s.byChat[chat] = *sel
}

// Of is the chat's selection.
func (s *Store) Of(chat string) (Selection, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sel, ok := s.byChat[chat]
	return sel, ok
}

type report struct {
	Conversation string     `json:"conversation"`
	Selection    *Selection `json:"selection"`
}

// ServeHTTP answers POST /api/v2/pane-selection {conversation, selection}: the page says what is selected now.
func (s *Store) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body report
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&body); err != nil || body.Conversation == "" {
		http.Error(w, "bad report", http.StatusBadRequest)
		return
	}
	s.Put(body.Conversation, body.Selection)
	w.WriteHeader(http.StatusNoContent)
}
