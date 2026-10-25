package main

import (
	"errors"
	"path/filepath"

	"github.com/ly-solutions/harnessly/internal/checkpoints"
	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/machine"
	"github.com/ly-solutions/harnessly/internal/state"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// v2Rewinder is what a rewind of a chat asks of the machine: the layout's tab of the chat bound to another session of
// its agent (the resume reads it there), and the snapshots the timeline slice takes of the chat's folder when a turn
// ends (internal/checkpoints, the same repositories the checkpoints route shows).
type v2Rewinder struct{ c *core }

var _ convo.Rewinder = v2Rewinder{}

// snapshotSlackMs: a turn's end in the transcript and the snapshot taken after it come from two clocks of the same
// machine (the transcript's line, the Stop hook): a snapshot this much before the end still shows the folder after it.
const snapshotSlackMs = 5_000

func (r v2Rewinder) Bind(conv wire.Conversation, sessionID, transcript string) error {
	if conv.Session == "" {
		return errors.New("the conversation has no tab")
	}
	return r.c.store.StoreRecoveredAgentSession(conv.Session, state.Provider(conv.Provider), sessionID, transcript)
}

// cwd is the folder the snapshots of the chat were taken of: its pane's, as the turn's end saw it, while the pane is
// there; the chat's own once it is gone.
func (r v2Rewinder) cwd(conv wire.Conversation) string {
	if conv.Session != "" && r.c.cfg.Tmux != nil && r.c.cfg.Tmux.HasSession(conv.Session) {
		if d := machine.SessionDir(r.c.cfg.Tmux, conv.Session); d != "" {
			return d
		}
	}
	return conv.Cwd
}

func (r v2Rewinder) scope(tab, cwd string) (*checkpoints.Scope, error) {
	if cwd == "" {
		return nil, errors.New("no folder")
	}
	return r.c.checkpointStore().Scope(tab, filepath.Clean(cwd))
}

func (r v2Rewinder) Snapshot(conv wire.Conversation, from, to wire.Ms) (convo.Snapshot, bool) {
	cwd := r.cwd(conv)
	sc, err := r.scope(conv.ID, cwd)
	if err != nil {
		return convo.Snapshot{}, false
	}
	list, err := sc.List()
	if err != nil {
		return convo.Snapshot{}, false
	}
	for _, c := range list { // the newest first
		at := wire.Ms(c.At)
		if at <= to && at >= from-snapshotSlackMs {
			return convo.Snapshot{Tab: conv.ID, Cwd: cwd, ID: c.ID, At: at}, true
		}
	}
	return convo.Snapshot{}, false
}

func (r v2Rewinder) Changes(s convo.Snapshot) ([]wire.RewindChange, int, error) {
	sc, err := r.scope(s.Tab, s.Cwd)
	if err != nil {
		return nil, 0, err
	}
	d, err := sc.DiffTo(s.ID)
	if err != nil {
		return nil, 0, err
	}
	if d == nil {
		return nil, 0, errors.New("the snapshot is gone")
	}
	out := []wire.RewindChange{}
	for _, f := range d.Files {
		if len(out) == wire.RewindChangesMax {
			break
		}
		change := wire.FileChanged
		switch f.String("status") {
		case "A":
			change = wire.FileRestored
		case "D":
			change = wire.FileRemoved
		}
		out = append(out, wire.RewindChange{Path: f.String("path"), Change: change})
	}
	return out, len(d.Files) - len(out), nil
}

func (r v2Rewinder) Restore(s convo.Snapshot) (convo.Snapshot, error) {
	sc, err := r.scope(s.Tab, s.Cwd)
	if err != nil {
		return convo.Snapshot{}, err
	}
	// The agent is stopped before the folder is returned: nothing at work races the rewind.
	res, err := sc.Rewind(s.ID, checkpoints.Hooks{})
	if err != nil {
		return convo.Snapshot{}, err
	}
	if res.Reason != "" {
		return convo.Snapshot{}, errors.New("checkpoint rewind: " + res.Reason)
	}
	return convo.Snapshot{Tab: s.Tab, Cwd: s.Cwd, ID: res.Before.ID, At: wire.Ms(res.Before.At)}, nil
}
