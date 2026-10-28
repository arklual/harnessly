package convo

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/handoff"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Moves says which conversations were moved to another machine: a conversation it blocks takes no more
// messages, because its work goes on elsewhere. nil blocks none.
type Moves interface {
	Blocked(conv string) bool
}

// ErrMoveNoHistory: the conversation has no complete record to carry.
var ErrMoveNoHistory = errors.New("the conversation has no history to move")

// MoveInfo is what a move reads of a conversation.
type MoveInfo struct {
	Title string
	Cwd   string
	// Busy: the agent works or waits for an answer; a conversation moves at rest.
	Busy bool
	// HasHistory: the agent has written a transcript.
	HasHistory bool
}

// MoveInfo reads the conversation a move starts from.
func (r *Runtime) MoveInfo(id string) (MoveInfo, bool) {
	rec, ok := r.Record(id)
	if !ok {
		return MoveInfo{}, false
	}
	return MoveInfo{Title: rec.Title, Cwd: rec.Cwd, Busy: busy(rec.State.K), HasHistory: rec.TranscriptPath != ""}, true
}

// MoveFence is a token that changes when the conversation's transcript does: the size it has now.
func (r *Runtime) MoveFence(id string) (string, bool) {
	rec, ok := r.Record(id)
	if !ok || rec.TranscriptPath == "" {
		return "", false
	}
	st, err := os.Stat(rec.TranscriptPath)
	if err != nil {
		return "", false
	}
	return strconv.FormatInt(st.Size(), 10), true
}

// MoveSnapshot is the conversation's whole history as provider-neutral JSONL, read once and left nowhere on this
// machine, and the fence it was read at. Larger than max bytes is refused.
func (r *Runtime) MoveSnapshot(id string, max int64) ([]byte, string, error) {
	rec, ok := r.Record(id)
	if !ok {
		return nil, "", ErrNotFound
	}
	if rec.TranscriptPath == "" || r.d.Handoffs == nil {
		return nil, "", ErrMoveNoHistory
	}
	fence, ok := r.MoveFence(id)
	if !ok {
		return nil, "", ErrMoveNoHistory
	}
	tag := "tab-" + newID()
	art, err := r.d.Handoffs.Snapshot(id, rec.TranscriptPath, tag)
	if errors.Is(err, handoff.ErrNoCompleteHistory) {
		return nil, "", ErrMoveNoHistory
	}
	if err != nil {
		return nil, "", err
	}
	data, err := r.d.Handoffs.Take(id, art.ID, tag, max)
	if err != nil {
		return nil, "", err
	}
	return data, fence, nil
}

// MoveOpen starts the conversation of a move: a new native session in cwd whose first prompt points at the history,
// held in a private file the first turn's end removes. The conversation names no other: it came from nowhere.
type MoveOpen struct {
	Title    string
	Cwd      string
	History  []byte
	Provider wire.Provider
	Profile  string
	Model    string
	Effort   string
}

// MoveOpen starts it; the id is the new conversation's.
func (r *Runtime) MoveOpen(in MoveOpen) (string, wire.RefuseCode) {
	if r.d.Launcher == nil || r.d.Handoffs == nil {
		return "", wire.RefuseUnsupported
	}
	var model *wire.Model
	if in.Model != "" {
		model = &wire.Model{ID: in.Model}
		if in.Effort != "" {
			e := wire.Effort(in.Effort)
			model.Effort = &e
		}
	}
	model, code := r.onProfile(in.Provider, model, in.Profile)
	if code != "" {
		return "", code
	}
	now := r.d.Clock.Now()
	id := "tab-" + newID()
	path, _, err := r.d.Handoffs.PutText(id, string(in.History))
	if err != nil {
		return "", wire.RefuseInternal
	}
	title := strings.TrimSpace(in.Title)
	rec := wire.Conversation{ID: id, MachineID: r.d.MachineID, Cwd: in.Cwd, Provider: in.Provider, Model: model, Title: title,
		TitleSource: wire.TitleFromFirstMessage, CreatedAt: ms(now), LastActivityAt: ms(now), LastPromptAt: ms(now), SeenAt: ms(now),
		State: wire.AgentState{K: wire.StateUnknown, Since: ms(now)}, Rev: 1}
	first := &wire.FirstMessage{ClientID: "move-" + newID(), Text: handoff.Bootstrap(handoff.From{}, wire.HandoffHistory, false, path),
		AttachmentIDs: []string{}}
	res := r.openConv(rec, LaunchRequest{Cwd: in.Cwd, Provider: in.Provider, Model: model}, first, true, path)
	if res.R == wire.ResultRefused {
		_ = r.d.Handoffs.Release(id, path)
		if res.Code != "" {
			return "", res.Code
		}
		return "", wire.RefuseInternal
	}
	return id, ""
}

// MoveDelete removes a conversation a move made and then undid; it is idempotent and takes nothing else.
func (r *Runtime) MoveDelete(id string) {
	if r.actor(id) == nil {
		return
	}
	force := true
	r.Do(context.Background(), "", wire.ConversationDeleteCmd{OpID: "move-discard-" + id, ID: id, Force: &force})
}
