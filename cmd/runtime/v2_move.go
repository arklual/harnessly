package main

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/chatmove"
	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// v2Move gives the move of a conversation to another machine the conversation runtime and the machine's agents.
type v2Move struct {
	rt      *convo.Runtime
	machine wire.MachineSource
}

func (m v2Move) Info(id string) (chatmove.Info, bool) {
	i, ok := m.rt.MoveInfo(id)
	return chatmove.Info{Title: i.Title, Cwd: i.Cwd, Busy: i.Busy, HasHistory: i.HasHistory}, ok
}

func (m v2Move) Snapshot(id string, max int64) ([]byte, string, error) {
	data, fence, err := m.rt.MoveSnapshot(id, max)
	if errors.Is(err, convo.ErrMoveNoHistory) {
		return nil, "", chatmove.ErrNoHistory
	}
	return data, fence, err
}

func (m v2Move) Fence(id string) (string, bool) { return m.rt.MoveFence(id) }

func (m v2Move) Open(in chatmove.Open) (string, error) {
	id, code := m.rt.MoveOpen(convo.MoveOpen{Title: in.Title, Cwd: in.Cwd, History: in.History, Provider: wire.Provider(in.Provider),
		Profile: in.Profile, Model: in.Model, Effort: in.Effort})
	switch code {
	case "":
		return id, nil
	case wire.RefuseOptionUnknown:
		return "", chatmove.ErrModel
	case wire.RefuseNotFound, wire.RefuseUnsupported:
		return "", chatmove.ErrAgent
	}
	return "", errors.New("the conversation was refused: " + string(code))
}

func (m v2Move) Delete(id string) { m.rt.MoveDelete(id) }

// agents are the ways this machine can start a conversation now: each CLI that is ready, and each profile with a key.
func (m v2Move) agents() []chatmove.Agent {
	snap, err := m.machine.Snapshot(context.Background(), false)
	if err != nil {
		return nil
	}
	short := map[wire.Provider]string{wire.ProviderClaude: "Claude", wire.ProviderCodex: "Codex"}
	models := func(list []wire.AgentModel) []chatmove.Model {
		out := make([]chatmove.Model, 0, len(list))
		for i, a := range list {
			out = append(out, chatmove.Model{ID: a.ID, Label: a.Label, Default: i == 0})
		}
		return out
	}
	var out []chatmove.Agent
	for _, a := range snap.Machine.Agents {
		if a.Status != wire.InstallReady {
			continue
		}
		if len(a.Models) > 0 {
			out = append(out, chatmove.Agent{Provider: string(a.Provider), Short: short[a.Provider], Models: models(a.Models)})
		}
		for _, p := range a.Profiles {
			if p.Configured && len(p.Models) > 0 {
				out = append(out, chatmove.Agent{Provider: string(a.Provider), Profile: p.ID, Short: p.Label, Models: models(p.Models)})
			}
		}
	}
	return out
}

// openMoveStore opens what the runtime remembers of moves; nil (blocking nothing) where it cannot be read.
func openMoveStore(d deps) *chatmove.Store {
	store, err := chatmove.OpenStore(filepath.Join(d.DataDir, "v2", "chat-moves.json"))
	if err != nil {
		agent.Log("warn", "v2: moves of chats are off", agent.F{K: "error", V: err.Error()})
		return nil
	}
	return store
}

// newV2Move assembles the move of this runtime; nil where it cannot run (no secret to seal the bundle with).
func newV2Move(d deps, rt *convo.Runtime, machine wire.MachineSource, store *chatmove.Store) *chatmove.Service {
	if len(d.Secret) == 0 || store == nil {
		return nil
	}
	m := v2Move{rt: rt, machine: machine}
	return &chatmove.Service{Conv: m, Agents: m.agents, Roots: []string{d.WorkspaceRoot}, Secret: d.Secret, Store: store, Home: d.WorkspaceRoot}
}
