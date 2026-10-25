package convo

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/feed"
	"github.com/ly-solutions/harnessly/internal/handoff"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// ErrNoTranscript: the conversation's agent has written no transcript yet, so there is no context to carry.
var ErrNoTranscript = errors.New("the conversation has no transcript yet")

// source is a reader of a conversation's transcript of its own, beside the one its feed holds.
func (r *Runtime) source(rec wire.Conversation) feed.Source {
	switch {
	case r.d.Source != nil:
		return r.d.Source(rec)
	case rec.Provider == wire.ProviderCodex:
		return &feed.CodexSource{Path: rec.TranscriptPath}
	}
	return &feed.ClaudeSource{Path: rec.TranscriptPath}
}

// whole is every turn of a conversation's transcript, read off the actor.
func (r *Runtime) whole(rec wire.Conversation) []feed.WholeTurn {
	return feed.Whole(rec.ID, rec.Provider, r.source(rec))
}

func busy(k wire.AgentStateKind) bool {
	return k == wire.StateWorking || k == wire.StateWaitingQuestion || k == wire.StateWaitingPermission
}

func modelID(m *wire.Model) string {
	if m == nil {
		return ""
	}
	return m.ID
}

func fromOf(rec wire.Conversation) handoff.From {
	return handoff.From{ID: rec.ID, Title: rec.Title, Provider: rec.Provider, Model: modelID(rec.Model)}
}

// Excerpt is the draft of a handoff by excerpt: the conversation's context within the budget of the model it goes to
// (provider "" is the conversation's own agent).
func (r *Runtime) Excerpt(id string, provider wire.Provider, model string) (wire.HandoffDraft, error) {
	rec, ok := r.Record(id)
	if !ok {
		return wire.HandoffDraft{}, ErrNotFound
	}
	if rec.TranscriptPath == "" {
		return wire.HandoffDraft{}, ErrNoTranscript
	}
	if provider == "" {
		provider, model = rec.Provider, modelID(rec.Model)
	}
	text, kept := handoff.Excerpt(r.whole(rec), rec.Provider, handoff.Budget(provider, model))
	if strings.TrimSpace(text) == "" {
		return wire.HandoffDraft{}, ErrNoTranscript
	}
	return wire.HandoffDraft{Mode: wire.DraftExcerpt, Text: text, Tokens: handoff.Tokens(text), Turns: kept}, nil
}

// forkPoint is what a fork reads of the conversation it branches off, on its actor.
type forkPoint struct {
	rec  wire.Conversation
	keep wire.Turn
	next string
	last bool
	code wire.RefuseCode
}

func (r *Runtime) forkPoint(id, at string) forkPoint {
	var p forkPoint
	err := r.call(context.Background(), id, func(c *conv, now time.Time) {
		if c.deletedAt != 0 {
			p.code = wire.RefuseNotFound
			return
		}
		p.rec = c.rec
		turns := c.fd.Records()
		keep, next, ok := handoff.Cut(turns, at, busy(c.rec.State.K))
		switch {
		case !ok && at != "" && len(turns) > 0 && turns[len(turns)-1].ID == at:
			p.code = wire.RefuseAgentWorking
		case !ok:
			p.code = wire.RefuseNotFound
		default:
			p.keep, p.next, p.last = keep, next, next == ""
		}
	})
	if err != nil {
		p.code = refusal(err)
	}
	if p.code == "" && p.rec.TranscriptPath == "" {
		p.code = wire.RefuseUnsupported
	}
	return p
}

// fork branches a conversation off another one after a finished turn. Claude forks its own session: the runtime copies
// the transcript up to the cut under a new session id and resumes the copy, so any finished turn will do. Codex forks
// with its CLI, which takes the whole session: at the last turn of an agent at rest; anywhere else the fork starts with
// an excerpt of the history up to the turn instead, on the same model.
func (r *Runtime) fork(m wire.ConversationForkCmd) wire.CmdResult {
	if r.d.Launcher == nil {
		return wire.Refused(wire.RefuseUnsupported)
	}
	at := ""
	if m.AtTurn != nil {
		at = *m.AtTurn
	}
	p := r.forkPoint(m.ID, at)
	if p.code != "" {
		return wire.Refused(p.code)
	}
	src, now := p.rec, r.d.Clock.Now()
	keep := p.keep.ID
	rec := wire.Conversation{ID: "tab-" + newID(), MachineID: r.d.MachineID, ProjectID: src.ProjectID, Cwd: src.Cwd,
		Provider: src.Provider, Model: src.Model, Title: src.Title, TitleSource: wire.TitleFromFirstMessage, CreatedAt: ms(now),
		LastActivityAt: ms(now), LastPromptAt: ms(now), SeenAt: ms(now), State: wire.AgentState{K: wire.StateUnknown, Since: ms(now)},
		Rev: 1, Origin: &wire.Origin{K: wire.OriginFork, FromID: src.ID, FromTitle: src.Title, FromProvider: src.Provider,
			FromModel: src.Model, AtTurn: &keep, Via: wire.ViaSession, At: ms(now)}}
	req := LaunchRequest{Cwd: src.Cwd, Provider: src.Provider, Model: src.Model}
	switch {
	case src.Provider == wire.ProviderClaude:
		oldID := strings.TrimSuffix(filepath.Base(src.TranscriptPath), ".jsonl")
		newSID := newUUID()
		path, err := handoff.ForkClaude(src.TranscriptPath, oldID, newSID, handoff.UUIDOfTurn(p.next))
		if err != nil {
			r.d.Log.Warn("fork: the session was not copied", "conv", src.ID, "err", err)
			return wire.Refused(wire.RefuseInternal)
		}
		rec.TranscriptPath, rec.AgentSessionID = path, newSID
		req.ResumeSessionID, req.TranscriptPath = newSID, path
	case p.last && src.AgentSessionID != "":
		req.ResumeSessionID, req.Fork = src.AgentSessionID, true
	default:
		turns := r.whole(src)
		for i, t := range turns {
			if t.Turn.ID == keep {
				turns = turns[:i+1]
				break
			}
		}
		text, _ := handoff.Excerpt(turns, src.Provider, handoff.Budget(src.Provider, modelID(src.Model)))
		if strings.TrimSpace(text) == "" {
			return wire.Refused(wire.RefuseUnsupported)
		}
		rec.Origin.Via = wire.ViaExcerpt
		first := &wire.FirstMessage{ClientID: "fork-" + newID(), Text: handoff.Handoff(fromOf(src), wire.HandoffExcerpt, true, text),
			AttachmentIDs: []string{}}
		return r.openConv(rec, req, first)
	}
	return r.openConv(rec, req, nil)
}

// handoff opens a conversation on another agent or model with the context of one: the text the person approved goes
// in its first message, in a block that says where it comes from.
func (r *Runtime) handoff(m wire.ConversationHandoffCmd) wire.CmdResult {
	if r.d.Launcher == nil {
		return wire.Refused(wire.RefuseUnsupported)
	}
	src, ok := r.Record(m.ID)
	if !ok {
		return wire.Refused(wire.RefuseNotFound)
	}
	// A handoff does not inherit the profile of the conversation it comes from: it names its own, or none.
	model, code := r.onProfile(m.Provider, m.Model, m.Profile)
	if code != "" {
		return wire.Refused(code)
	}
	m.Model = model
	now := r.d.Clock.Now()
	via := wire.ViaExcerpt
	if m.Mode == wire.HandoffNote {
		via = wire.ViaNote
	}
	rec := wire.Conversation{ID: "tab-" + newID(), MachineID: r.d.MachineID, ProjectID: src.ProjectID, Cwd: src.Cwd,
		Provider: m.Provider, Model: m.Model, Title: src.Title, TitleSource: wire.TitleFromFirstMessage, CreatedAt: ms(now),
		LastActivityAt: ms(now), LastPromptAt: ms(now), SeenAt: ms(now), State: wire.AgentState{K: wire.StateUnknown, Since: ms(now)},
		Rev: 1, Origin: &wire.Origin{K: wire.OriginHandoff, FromID: src.ID, FromTitle: src.Title, FromProvider: src.Provider,
			FromModel: src.Model, Via: via, At: ms(now)}}
	first := &wire.FirstMessage{ClientID: m.OpID, Text: handoff.Handoff(fromOf(src), m.Mode, false, m.Text), AttachmentIDs: []string{}}
	return r.openConv(rec, LaunchRequest{Cwd: src.Cwd, Provider: m.Provider, Model: m.Model}, first)
}

// merge sends a fork's result into the conversation it came from, as a message of its queue: the answer is that
// message's, on the other conversation's channel.
func (r *Runtime) merge(device string, m wire.ConversationMergeCmd, finish func(wire.CmdResult)) error {
	fork, ok := r.Record(m.ID)
	if !ok {
		return ErrNotFound
	}
	if fork.Origin == nil {
		finish(wire.Refused(wire.RefuseUnsupported))
		return nil
	}
	parent := fork.Origin.FromID
	a := r.actor(parent)
	if a == nil {
		finish(wire.Refused(wire.RefuseNotFound))
		return nil
	}
	send := wire.SendCmd{OpID: m.OpID, ClientID: m.OpID, ConversationID: parent, Text: handoff.Result(fromOf(fork), m.Text),
		AttachmentIDs: []string{}, Mode: wire.ModeQueue, Kind: wire.KindPrompt}
	return r.on(a, func(c *conv, now time.Time) {
		res := c.do(device, send, now)
		if res.R != wire.ResultRefused {
			res.ID = &parent
		}
		finish(res)
	})
}

// newUUID is a random UUID (version 4): an agent session's id.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
