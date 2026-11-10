package apps

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

// A view session is what the page issues when it mounts a view: the app, the view, the target it is shown in and the
// chat it works beside, kept here on the machine. The view server serves a view's files only under the session's token,
// and the storage and tool routes take the app, the target and the owner of an area from the session, never from the
// request body, so a view cannot name another app, another target or another chat's storage.

const (
	// SessionIdle is how long a session lives without a use; every use renews it.
	SessionIdle = time.Hour
	// SessionMax is the longest a session lives, used or not.
	SessionMax  = 12 * time.Hour
	maxSessions = 2000
)

// Session is one mounted view.
type Session struct {
	Token  string
	App    string
	View   string
	Target string
	// Conversation is the chat the view works beside; "" for a view with no chat.
	Conversation string
	// Document is the Sinopia document a view of a sinopia target is shown in; "" for any other view.
	Document string
	// Nonce is what the view's own document carries (a meta tag the view server writes into its pages) and the bridge
	// asks for in ui/initialize: a document the frame navigated to never saw it.
	Nonce string
	// Trace is the journal's trace of the page's action that mounted the view (HRN-976): the view server's lines of the
	// session carry it, so a file the view could not load reads in one chain with the page's lines.
	Trace string

	created time.Time
	seen    time.Time
}

// ViewSessions are the live sessions of a machine.
type ViewSessions struct {
	Now func() time.Time

	mu   sync.Mutex
	byID map[string]*Session
}

// NewViewSessions is an empty set of sessions.
func NewViewSessions() *ViewSessions {
	return &ViewSessions{Now: time.Now, byID: map[string]*Session{}}
}

// Issue starts a session for a view and returns its unguessable token.
func (s *ViewSessions) Issue(app, view, target, conversation string) (Session, error) {
	return s.IssueIn(app, view, target, conversation, "")
}

// IssueIn is Issue for a view shown in a Sinopia document.
func (s *ViewSessions) IssueIn(app, view, target, conversation, document string) (Session, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return Session{}, err
	}
	now := s.Now()
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return Session{}, err
	}
	sess := &Session{Token: base64.RawURLEncoding.EncodeToString(raw), Nonce: base64.RawURLEncoding.EncodeToString(nonce), App: app, View: view, Target: target, Conversation: conversation, Document: document, created: now, seen: now}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep(now)
	if len(s.byID) >= maxSessions {
		s.dropOldest()
	}
	s.byID[sess.Token] = sess
	return *sess, nil
}

// Get is the live session of a token, renewed by the use.
func (s *ViewSessions) Get(token string) (Session, bool) {
	if token == "" {
		return Session{}, false
	}
	now := s.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.byID[token]
	if sess == nil || s.expired(sess, now) {
		delete(s.byID, token)
		return Session{}, false
	}
	sess.seen = now
	return *sess, true
}

// Peek is the live session of a token, not renewed by the look; a token unknown or gone is a zero session.
func (s *ViewSessions) Peek(token string) (Session, bool) {
	if s == nil || token == "" {
		return Session{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.byID[token]
	if sess == nil || s.expired(sess, s.Now()) {
		return Session{}, false
	}
	return *sess, true
}

// SetTrace gives a live session the journal's trace of the action that mounted it.
func (s *ViewSessions) SetTrace(token, trace string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess := s.byID[token]; sess != nil {
		sess.Trace = trace
	}
}

// Renew keeps a live session for a view that is still mounted but unused, and says how long it now lives at least
// (the idle time, or what is left of the longest time). An expired or unknown token is refused; the session's app,
// view, target and chat are not touched.
func (s *ViewSessions) Renew(token string) (time.Duration, bool) {
	sess, ok := s.Get(token)
	if !ok {
		return 0, false
	}
	left := SessionMax - s.Now().Sub(sess.created)
	if left > SessionIdle {
		left = SessionIdle
	}
	return left, true
}

func (s *ViewSessions) expired(sess *Session, now time.Time) bool {
	return now.Sub(sess.seen) > SessionIdle || now.Sub(sess.created) > SessionMax
}

func (s *ViewSessions) sweep(now time.Time) {
	for k, sess := range s.byID {
		if s.expired(sess, now) {
			delete(s.byID, k)
		}
	}
}

func (s *ViewSessions) dropOldest() {
	var oldest string
	var at time.Time
	for k, sess := range s.byID {
		if oldest == "" || sess.seen.Before(at) {
			oldest, at = k, sess.seen
		}
	}
	delete(s.byID, oldest)
}
