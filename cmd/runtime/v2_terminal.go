package main

import (
	"context"
	"crypto/rand"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/terminal"
	"github.com/ly-solutions/harnessly/internal/tmux"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// v2Terminals are the terminals of v2 conversations: the pane of the conversation's agent, and shells of its own, each a
// tmux session of the machine that stays until it is closed. The socket is the Legacy terminal's (internal/terminal, a
// page attaches to the session through a pty); only the way to a session differs: it is named by the conversation.
type v2Terminals struct {
	rt *convo.Runtime
	// record is the conversation's record, and the machine's own place for wire.HomeConv (homeRecord).
	record func(conv string) (wire.Conversation, bool)
	tmux   *tmux.Tmux
	term   *terminal.Server
}

var _ wire.TerminalSource = (*v2Terminals)(nil)

// Serve attaches the request to the conversation's agent pane, or to one of its own terminals.
func (t *v2Terminals) Serve(w http.ResponseWriter, r *http.Request, conv, term string) error {
	rec, ok := t.record(conv)
	if !ok {
		return convo.ErrNotFound
	}
	session := rec.Session
	if term != "" {
		session = t.tmux.OwnShellName(conv, term)
	}
	if session == "" {
		return convo.ErrNotFound
	}
	if !t.tmux.HasSession(session) {
		// A stopped chat keeps no session: the person who opens its terminal gets a shell in the chat's folder, as the
		// pane had. A terminal stopped on purpose, and a chat's own terminal that is gone, stay gone.
		if _, _, _, tab := t.tmux.ParseSessionName(session); term != "" || !paneOwed(rec, tab) {
			return convo.ErrNotFound
		}
		if err := t.tmux.CreateSession(session, 80, 24, t.tmux.ResolveExistingDir(rec.Cwd)); err != nil {
			return err
		}
	}
	// The terminal server keeps one socket per page client id: a page that opens the same terminal again replaces its
	// old socket, and a page with several terminals open has one id for each.
	q := r.URL.Query()
	q.Set("session", session)
	if client := q.Get("clientId"); client != "" {
		q.Set("clientId", "v2:"+client+":"+session)
	}
	r2 := r.Clone(r.Context())
	r2.URL = &url.URL{Path: r.URL.Path, RawQuery: q.Encode()}
	t.term.Handle(w, r2)
	return nil
}

// paneOwed: the agent pane of the conversation, which has no session, is made again for the person who opens it. Only a
// tab's session of a chat whose agent has stopped: one stopped on purpose says so in the chat instead, and a chat whose
// agent is taken for running has lost its pane, which a new shell would hide.
func paneOwed(rec wire.Conversation, tabSession bool) bool {
	return tabSession && rec.TerminalStop == nil && rec.State.K == wire.StateStopped
}

// ownSessions are the sessions of the conversation's own terminals, oldest first.
func (t *v2Terminals) ownSessions(conv string) []wire.Terminal {
	var out []wire.Terminal
	for _, name := range t.tmux.ListSessions() {
		if id, ok := t.tmux.OwnShellID(conv, name); ok && wire.ValidTerminalID(id) {
			out = append(out, wire.Terminal{ID: id, CreatedAt: terminalBorn(id)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out
}

func (t *v2Terminals) List(_ context.Context, conv string) ([]wire.Terminal, error) {
	if _, ok := t.record(conv); !ok {
		return nil, convo.ErrNotFound
	}
	return t.ownSessions(conv), nil
}

// Create starts a shell in the conversation's folder. Its id is the time it was made in base 36 and three random
// letters, so the list is in order without the machine keeping anything of its own.
func (t *v2Terminals) Create(_ context.Context, conv string) (wire.Terminal, error) {
	rec, ok := t.record(conv)
	if !ok {
		return wire.Terminal{}, convo.ErrNotFound
	}
	if len(t.ownSessions(conv)) >= wire.TerminalsMax {
		return wire.Terminal{}, wire.ErrTerminalLimit
	}
	now := time.Now().UnixMilli()
	id := strconv.FormatInt(now, 36) + randomLetters(3)
	if err := t.tmux.CreateSession(t.tmux.OwnShellName(conv, id), 80, 24, t.tmux.ResolveExistingDir(rec.Cwd)); err != nil {
		return wire.Terminal{}, err
	}
	return wire.Terminal{ID: id, CreatedAt: wire.Ms(now)}, nil
}

func (t *v2Terminals) Close(_ context.Context, conv, id string) error {
	if _, ok := t.record(conv); !ok {
		return convo.ErrNotFound
	}
	name := t.tmux.OwnShellName(conv, id)
	if !t.tmux.HasSession(name) {
		return convo.ErrNotFound
	}
	t.tmux.KillSession(name)
	return nil
}

// closeAll ends every own terminal of a conversation: a deleted conversation leaves none behind.
func (t *v2Terminals) closeAll(conv string) {
	for _, term := range t.ownSessions(conv) {
		t.tmux.KillSession(t.tmux.OwnShellName(conv, term.ID))
	}
}

// terminalBorn is the time an own terminal was made, which its id begins with.
func terminalBorn(id string) wire.Ms {
	if len(id) <= 3 {
		return 0
	}
	ms, err := strconv.ParseInt(strings.ToLower(id[:len(id)-3]), 36, 64)
	if err != nil || ms < 0 {
		return 0
	}
	return wire.Ms(ms)
}

func randomLetters(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = letters[int(b[i])%len(letters)]
	}
	return string(b)
}
