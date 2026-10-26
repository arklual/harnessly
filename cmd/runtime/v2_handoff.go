package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/handoff"
	"github.com/ly-solutions/harnessly/internal/sidechat"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// v2Handoff writes the drafts of handoffs and merges: the excerpt from the transcript, or the note the conversation's
// agent writes on a throwaway fork of its session (internal/handoff). One note at a time per conversation.
type v2Handoff struct {
	rt *convo.Runtime
	// runtimeFile is the machine's runtime config naming the Claude binary and login.
	runtimeFile string
	write       func(ctx context.Context, run handoff.NoteRun) (string, error)
}

func (h *v2Handoff) Draft(ctx context.Context, conv string, req wire.HandoffDraftRequest) (wire.HandoffDraft, error) {
	if req.Mode == wire.DraftExcerpt {
		d, err := h.rt.Excerpt(conv, req.Provider, req.Model, req.Profile)
		if errors.Is(err, convo.ErrNoTranscript) {
			return d, wire.ErrDraftUnsupported
		}
		return d, err
	}
	rec, ok := h.rt.Record(conv)
	if !ok {
		return wire.HandoffDraft{}, convo.ErrNotFound
	}
	session := sessionOf(rec)
	if session == "" || req.Mode == wire.DraftResult && rec.Origin == nil {
		return wire.HandoffDraft{}, wire.ErrDraftUnsupported
	}
	release := sidechat.Claim("handoff:" + conv)
	if release == nil {
		return wire.HandoffDraft{}, wire.ErrDraftBusy
	}
	defer release()
	model := ""
	if rec.Model != nil {
		model = rec.Model.ID
	}
	write := h.write
	if write == nil {
		write = handoff.WriteNote
	}
	text, err := write(ctx, handoff.NoteRun{Provider: rec.Provider, SessionID: session, Cwd: rec.Cwd, Model: model,
		Prompt: handoff.PromptOf(req.Mode), BaseEnv: os.Environ(), Claude: sidechat.ReadRuntime(h.runtimeFile),
		ClaudeProjectDir: filepath.Dir(rec.TranscriptPath), Log: coreLog})
	if err != nil {
		return wire.HandoffDraft{}, err
	}
	if r := []rune(text); len(r) > wire.HandoffTextMax {
		text = string(r[:wire.HandoffTextMax-1]) + "…"
	}
	return wire.HandoffDraft{Mode: req.Mode, Text: text, Tokens: handoff.Tokens(text)}, nil
}

// sessionOf is the agent's own id of the conversation's session: the one it was attached with, or the one its
// transcript is named by (Claude's <id>.jsonl, Codex's rollout-…-<id>.jsonl); "" before it has one.
func sessionOf(rec wire.Conversation) string {
	if agents.IsUUID(rec.AgentSessionID) {
		return rec.AgentSessionID
	}
	name := strings.TrimSuffix(filepath.Base(rec.TranscriptPath), ".jsonl")
	if len(name) >= 36 && agents.IsUUID(name[len(name)-36:]) {
		return name[len(name)-36:]
	}
	return ""
}
