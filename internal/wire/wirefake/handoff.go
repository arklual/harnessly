package wirefake

import (
	"context"
	"slices"
	"time"

	"github.com/ly-solutions/harnessly/internal/feed"
	"github.com/ly-solutions/harnessly/internal/handoff"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// fakeNote is what the scripted agent writes when it is asked for a note.
const fakeNote = "## Goal\nFinish what the conversation set out to do.\n\n## Decisions\n- Keep the scripted agent's choices.\n\n" +
	"## Current state\nThe last turn is done.\n\n## Next steps\n- Carry on from the last answer.\n\n## Files\n- (none)\n\n" +
	"## Verify\n- Run the tests.\n\n## Gotchas\n- This is the fake machine."

// fakeResult is what the scripted agent of a fork writes to send back.
const fakeResult = "Done in the branch: the scripted agent answered every message. No files were touched."

// noteDelay is how long the scripted agent takes to write a note.
const noteDelay = 1500 * time.Millisecond

// whole is a conversation's turns with their entries, as an excerpt reads them.
func (c *conv) whole() []feed.WholeTurn {
	turns := append(slices.Clone(c.older), c.snap.Turns...)
	entries := append(slices.Clone(c.olderEntries), c.snap.Entries...)
	out := make([]feed.WholeTurn, 0, len(turns))
	for _, t := range turns {
		w := feed.WholeTurn{Turn: t}
		for _, e := range entries {
			if e.ID == t.ID || e.TurnID != nil && *e.TurnID == t.ID {
				w.Entries = append(w.Entries, e)
			}
		}
		out = append(out, w)
	}
	return out
}

// HandoffDraft is the excerpt of a conversation's turns, or the scripted agent's note after a moment.
func (s *Service) HandoffDraft(ctx context.Context, id string, req wire.HandoffDraftRequest) (wire.HandoffDraft, error) {
	s.mu.Lock()
	cv, err := s.get(id)
	var turns []feed.WholeTurn
	forked := false
	if err == nil {
		turns, forked = cv.whole(), cv.rec.Origin != nil
	}
	provider := cv.rec.Provider
	s.mu.Unlock()
	if err != nil {
		return wire.HandoffDraft{}, err
	}
	switch req.Mode {
	case wire.DraftExcerpt:
		text, kept := handoff.Excerpt(turns, provider, handoff.Budget(req.Provider, req.Model))
		if text == "" {
			return wire.HandoffDraft{}, wire.ErrDraftUnsupported
		}
		return wire.HandoffDraft{Mode: req.Mode, Text: text, Tokens: handoff.Tokens(text), Turns: kept}, nil
	case wire.DraftResult:
		if !forked {
			return wire.HandoffDraft{}, wire.ErrDraftUnsupported
		}
	}
	select {
	case <-time.After(noteDelay):
	case <-ctx.Done():
		return wire.HandoffDraft{}, ctx.Err()
	}
	text := fakeNote
	if req.Mode == wire.DraftResult {
		text = fakeResult
	}
	return wire.HandoffDraft{Mode: req.Mode, Text: text, Tokens: handoff.Tokens(text)}, nil
}

// branch is a new conversation made off another one, with where it came from: created as any, then on its origin.
func (s *Service) branch(src *conv, provider wire.Provider, model *wire.Model, origin wire.Origin, first *wire.FirstMessage) wire.CmdResult {
	res := s.create(wire.ConversationCreateCmd{OpID: "fork", Cwd: src.rec.Cwd, Provider: wire.ProviderClaude, Model: model, First: first})
	if res.R != wire.ResultAccepted {
		return res
	}
	cv := s.convs[*res.ID]
	cv.rec.Provider, cv.rec.ProjectID, cv.rec.Title, cv.rec.Origin = provider, src.rec.ProjectID, src.rec.Title, &origin
	res.Seq = s.upsert(cv)
	return res
}

// fork copies the turns of a conversation up to one into a new conversation, as a forked session holds them.
func (s *Service) fork(c wire.ConversationForkCmd) wire.CmdResult {
	src, err := s.get(c.ID)
	if err != nil {
		return wire.Refused(wire.RefuseNotFound)
	}
	turns := make([]wire.Turn, 0, len(src.snap.Turns))
	for _, t := range src.snap.Turns {
		if t.Status != wire.TurnLive {
			turns = append(turns, t)
		}
	}
	at := ""
	if c.AtTurn != nil {
		at = *c.AtTurn
	}
	keep, _, ok := handoff.Cut(turns, at, false)
	if !ok {
		return wire.Refused(wire.RefuseNotFound)
	}
	keepID := keep.ID
	origin := wire.Origin{K: wire.OriginFork, FromID: src.rec.ID, FromTitle: src.rec.Title, FromProvider: src.rec.Provider,
		FromModel: src.rec.Model, AtTurn: &keepID, Via: wire.ViaSession, At: s.now()}
	res := s.branch(src, src.rec.Provider, src.rec.Model, origin, nil)
	if res.R != wire.ResultAccepted {
		return res
	}
	cv := s.convs[*res.ID]
	for _, t := range src.snap.Turns {
		cv.snap.Turns = append(cv.snap.Turns, t)
		for _, e := range src.snap.Entries {
			if e.ID == t.ID || e.TurnID != nil && *e.TurnID == t.ID {
				cv.snap.Entries = append(cv.snap.Entries, e)
			}
		}
		if t.ID == keepID {
			break
		}
	}
	cv.rec.LastTurn = src.rec.LastTurn
	res.Seq = s.upsert(cv)
	return res
}

func (s *Service) handoffTo(c wire.ConversationHandoffCmd) wire.CmdResult {
	src, err := s.get(c.ID)
	if err != nil {
		return wire.Refused(wire.RefuseNotFound)
	}
	via := wire.ViaExcerpt
	if c.Mode == wire.HandoffNote {
		via = wire.ViaNote
	}
	origin := wire.Origin{K: wire.OriginHandoff, FromID: src.rec.ID, FromTitle: src.rec.Title, FromProvider: src.rec.Provider,
		FromModel: src.rec.Model, Via: via, At: s.now()}
	from := handoff.From{ID: src.rec.ID, Title: src.rec.Title, Provider: src.rec.Provider}
	if src.rec.Model != nil {
		from.Model = src.rec.Model.ID
	}
	first := &wire.FirstMessage{ClientID: c.OpID, Text: handoff.Handoff(from, c.Mode, false, c.Text), AttachmentIDs: []string{}}
	return s.branch(src, c.Provider, c.Model, origin, first)
}

func (s *Service) merge(c wire.ConversationMergeCmd) wire.CmdResult {
	fork, err := s.get(c.ID)
	if err != nil {
		return wire.Refused(wire.RefuseNotFound)
	}
	if fork.rec.Origin == nil {
		return wire.Refused(wire.RefuseUnsupported)
	}
	parent := fork.rec.Origin.FromID
	from := handoff.From{ID: fork.rec.ID, Title: fork.rec.Title, Provider: fork.rec.Provider}
	res := s.send(wire.SendCmd{OpID: c.OpID, ClientID: c.OpID, ConversationID: parent, Text: handoff.Result(from, c.Text),
		AttachmentIDs: []string{}, Mode: wire.ModeQueue, Kind: wire.KindPrompt})
	if res.R != wire.ResultRefused {
		res.ID = &parent
	}
	return res
}
