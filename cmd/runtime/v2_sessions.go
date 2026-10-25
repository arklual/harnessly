package main

import (
	"context"
	"sort"

	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/sessions"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// v2Sessions are the sessions the agent CLIs keep on the machine that no conversation of the runtime holds: what "continue
// a session from the terminal" offers. The listers keep what they read of each transcript, so a second look is cheap.
type v2Sessions struct {
	claude *sessions.ClaudeLister
	codex  *sessions.CodexLister
	// held are the sessions the conversations hold, by the agent's session id.
	held func() map[string]string
}

func newV2Sessions(home string, held func() map[string]string) *v2Sessions {
	return &v2Sessions{claude: sessions.NewClaudeLister(home), codex: sessions.NewCodexLister(home), held: held}
}

// leftOut says which session ids are not offered: the ones a conversation holds and the forks answering side questions.
func (s *v2Sessions) leftOut() func(id string) bool {
	held, forks := s.held(), sessions.ForkIDs()
	return func(id string) bool {
		_, taken := held[id]
		return taken || forks[id]
	}
}

// List is the newest wire.SessionsMax sessions of both agents.
func (s *v2Sessions) List(ctx context.Context) ([]wire.AgentSession, error) {
	skip := s.leftOut()
	found := s.claude.Recent(skip, wire.SessionsMax)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]wire.AgentSession, 0, 2*wire.SessionsMax)
	for _, r := range found {
		out = append(out, wireSession(wire.ProviderClaude, r))
	}
	for _, r := range s.codex.Recent(skip, wire.SessionsMax) {
		out = append(out, wireSession(wire.ProviderCodex, r))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	if len(out) > wire.SessionsMax {
		out = out[:wire.SessionsMax]
	}
	return out, nil
}

func wireSession(p wire.Provider, r sessions.Recent) wire.AgentSession {
	return wire.AgentSession{Provider: p, SessionID: r.SessionID, Title: r.Title, Cwd: r.Cwd, UpdatedAt: wire.Ms(r.UpdatedAt.UnixMilli()), Turns: r.Turns}
}

// Find is the session of an agent with this id, however far back it is (a conversation of the runtime takes it from here).
func (s *v2Sessions) Find(p wire.Provider, id string) (convo.AgentSession, bool) {
	var r sessions.Recent
	var ok bool
	switch p {
	case wire.ProviderClaude:
		r, ok = s.claude.Find(id)
	case wire.ProviderCodex:
		r, ok = s.codex.Find(id)
	}
	if !ok {
		return convo.AgentSession{}, false
	}
	return convo.AgentSession{Title: r.Title, Cwd: r.Cwd, TranscriptPath: r.Path}, true
}
