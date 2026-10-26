package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/machines"
	"github.com/ly-solutions/harnessly/internal/preflight"
	"github.com/ly-solutions/harnessly/internal/tmux"
	"github.com/ly-solutions/harnessly/internal/typist"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// What the live v2 reaches the machine through: the conversations' tmux panes and processes, the uploads' files and
// the machine channel's record. None of it is on the actors' path: the actors ask for it off their goroutines, and
// the machine channel answers from what it holds.

// v2Pane is an agent pane in tmux, typed into the way the Legacy queue types.
type v2Pane struct {
	tm      *tmux.Tmux
	session string
}

func (p v2Pane) CaptureStyled() (string, bool) { return p.tm.CaptureStyled(p.session) }
func (p v2Pane) Capture() (string, bool)       { return p.tm.Capture(p.session) }
func (p v2Pane) Paste(text string) error       { return p.tm.SendBracketedPasteOnly(p.session, text) }
func (p v2Pane) TypeLine(text string) error    { return p.tm.SendLiteralThenEnter(p.session, text) }
func (p v2Pane) TypeOnly(text string) error    { return p.tm.SendLiteral(p.session, text) }
func (p v2Pane) Keys(keys ...string) error     { return p.tm.SendKeySequence(p.session, keys...) }

// v2Tmux is convo.Machine over tmux. A pane that looks gone, or an agent that looks exited, is believed only when
// the look before agreed: one call that failed on a loaded machine must not stop a working agent.
type v2Tmux struct {
	tm *tmux.Tmux

	mu    sync.Mutex
	doubt map[string]doubt
}

type doubt struct{ noSession, shell bool }

func (m *v2Tmux) Pane(session string) typist.Pane {
	if session == "" {
		return nil
	}
	return v2Pane{tm: m.tm, session: session}
}

// Probe: what runs in the session. A look that says "no session" or "a shell" is reported only when the previous look
// said so too; the first is reported as the agent still running.
func (m *v2Tmux) Probe(session string) convo.Probe {
	cmd := m.tm.PaneCurrentCommand(session)
	has := cmd != "" || m.tm.HasSession(session)
	// tmux that did not answer (a loaded machine) said nothing about the foreground: not a shell.
	shell := tmux.SafeShells[cmd]
	m.mu.Lock()
	if m.doubt == nil {
		m.doubt = map[string]doubt{}
	}
	prev := m.doubt[session]
	m.doubt[session] = doubt{noSession: !has, shell: shell}
	m.mu.Unlock()
	switch {
	case !has && !prev.noSession:
		return convo.Probe{Session: true, Foreground: true}
	case !has:
		return convo.Probe{}
	case shell && !prev.shell:
		return convo.Probe{Session: true, Foreground: true}
	case shell:
		return convo.Probe{Session: true, Shell: true}
	}
	return convo.Probe{Session: true, Foreground: true}
}

// v2Uploads finds the files the page uploaded.
type v2Uploads struct{ up *wire.Uploads }

func (u v2Uploads) Resolve(ids []string) ([]wire.AttachmentRef, []string, wire.RefuseCode) {
	refs, images := make([]wire.AttachmentRef, 0, len(ids)), []string{}
	for _, id := range ids {
		ref, path, ok := u.up.Get(id)
		if !ok {
			return nil, nil, wire.RefuseAttachmentMissing
		}
		refs = append(refs, ref)
		if ref.Kind == wire.AttachmentImage {
			images = append(images, path)
		}
	}
	return refs, images, ""
}

// v2Log writes the conversations' journal lines.
type v2Log struct{}

func (v2Log) Info(msg string, kv ...any) { v2LogAt("info", msg, kv) }
func (v2Log) Warn(msg string, kv ...any) { v2LogAt("warn", msg, kv) }

func v2LogAt(level, msg string, kv []any) {
	fs := make([]agent.F, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		k, _ := kv[i].(string)
		v := kv[i+1]
		if err, ok := v.(error); ok {
			v = err.Error()
		}
		fs = append(fs, agent.F{K: k, V: v})
	}
	agent.Log(level, "v2: "+msg, fs...)
}

// --- the machine channel ---

// liveMachine is the machine channel of a runtime on its own: always online, its agents' state refreshed behind, its
// load read every 2 s for the sockets that asked. Snapshot never waits for a check: it answers what it holds.
type liveMachine struct {
	dataDir string
	busy    func() int
	check   func() preflight.Result
	models  func(wire.Provider) []wire.AgentModel
	health  *machines.HealthMonitor
	// offers are the profiles a provider's CLI can start a chat on; nil is a machine without profiles.
	offers func(wire.Provider) []wire.ProfileOffer

	mu      sync.Mutex
	machine wire.Machine
	subs    map[int]func(wire.Event)
	next    int
}

func newLiveMachine(id, version string, features []wire.Feature, dataDir string, busy func() int, check func() preflight.Result,
	models func(wire.Provider) []wire.AgentModel) *liveMachine {
	host, _ := os.Hostname()
	return &liveMachine{dataDir: dataDir, busy: busy, check: check, models: models, subs: map[int]func(wire.Event){},
		health: machines.NewHealthMonitor(machines.HealthDeps{Path: dataDir}),
		machine: wire.Machine{ID: id, Name: host, Host: host, Reach: wire.ReachOnline, Agents: []wire.MachineAgent{},
			Runtime: &wire.RuntimeInfo{Version: version, Proto: wire.Proto, Features: features}}}
}

var _ wire.MachineSource = (*liveMachine)(nil)

func (m *liveMachine) Snapshot(_ context.Context, load bool) (wire.MachineSnapshot, error) {
	m.mu.Lock()
	snap := m.machine
	snap.Agents = append([]wire.MachineAgent{}, m.machine.Agents...)
	m.mu.Unlock()
	now := time.Now().UnixMilli()
	snap.LastSeenAt = &now
	if load {
		l := m.readLoad()
		snap.Load = &l
	}
	return wire.MachineSnapshot{Machine: snap}, nil
}

func (m *liveMachine) Subscribe(load bool, fn func(wire.Event)) (cancel func()) {
	m.mu.Lock()
	id := m.next
	m.next++
	m.subs[id] = fn
	m.mu.Unlock()
	stop := make(chan struct{})
	if load {
		go func() {
			t := time.NewTicker(2 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case <-t.C:
					fn(wire.MachineLoadEv{Load: m.readLoad()})
				}
			}
		}()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			close(stop)
			m.mu.Lock()
			delete(m.subs, id)
			m.mu.Unlock()
		})
	}
}

// readLoad is the machine's load now: the 1-minute load over the cores, memory, the data folder's disk, the agents at work.
func (m *liveMachine) readLoad() wire.MachineLoad {
	h := machines.ReadHostStats()
	l := wire.MachineLoad{At: time.Now().UnixMilli(), MemTotal: int64(h.TotalMem), MemUsed: int64(h.TotalMem - h.FreeMem)}
	if h.CPUs > 0 {
		l.CPU = min(1, max(0, h.Load1/float64(h.CPUs)))
	}
	if m.busy != nil {
		l.BusyAgents = m.busy()
	}
	var st syscall.Statfs_t
	if syscall.Statfs(filepath.Clean(m.dataDir), &st) == nil {
		l.DiskTotal, l.DiskFree = int64(st.Blocks)*int64(st.Bsize), int64(st.Bavail)*int64(st.Bsize)
	}
	return l
}

// agentsNow is the agent CLIs as the preflight finds them, with the models each takes. Only Claude changes its model
// in a running conversation (/model in its box).
func agentsNow(r preflight.Result, models func(wire.Provider) []wire.AgentModel) []wire.MachineAgent {
	list := func(p wire.Provider) []wire.AgentModel {
		if models == nil {
			return []wire.AgentModel{}
		}
		return models(p)
	}
	claude := wire.MachineAgent{Provider: wire.ProviderClaude, Version: r.Claude.Version, Status: wire.InstallReady,
		Models: list(wire.ProviderClaude), CanSwitchModel: true}
	switch {
	case !r.Claude.Installed:
		claude.Status = wire.InstallMissing
	case !r.Claude.LoggedIn:
		claude.Status = wire.InstallLoginRequired
	}
	codex := wire.MachineAgent{Provider: wire.ProviderCodex, Version: r.Codex.Version, Status: wire.InstallReady,
		Models: list(wire.ProviderCodex)}
	switch {
	case !r.Codex.Installed:
		codex.Status = wire.InstallMissing
	case !r.Codex.LoggedIn:
		codex.Status = wire.InstallLoginRequired
	}
	return []wire.MachineAgent{claude, codex}
}

// refresh asks the preflight (a login shell, seconds on a loaded machine) and tells the sockets what changed.
func (m *liveMachine) refresh() {
	if m.check == nil {
		return
	}
	agents := m.withOffers(agentsNow(m.check(), m.models))
	m.mu.Lock()
	changed := !reflect.DeepEqual(agents, m.machine.Agents)
	m.machine.Agents = agents
	ev, subs := m.statusLocked()
	m.mu.Unlock()
	if changed {
		for _, fn := range subs {
			fn(ev)
		}
	}
}

// withOffers adds the profiles each agent CLI offers.
func (m *liveMachine) withOffers(agents []wire.MachineAgent) []wire.MachineAgent {
	if m.offers == nil {
		return agents
	}
	for i := range agents {
		agents[i].Profiles = m.offers(agents[i].Provider)
	}
	return agents
}

// republishProfiles tells the sockets that a key was saved or removed: the agents as held, with the profiles anew.
func (m *liveMachine) republishProfiles() {
	m.mu.Lock()
	agents := m.withOffers(append([]wire.MachineAgent{}, m.machine.Agents...))
	changed := !reflect.DeepEqual(agents, m.machine.Agents)
	m.machine.Agents = agents
	ev, subs := m.statusLocked()
	m.mu.Unlock()
	if changed {
		for _, fn := range subs {
			fn(ev)
		}
	}
}

// statusLocked is the machine_status of the record and the sockets to tell it; m.mu is held.
func (m *liveMachine) statusLocked() (wire.MachineStatusEv, []func(wire.Event)) {
	ev := wire.MachineStatusEv{Reach: m.machine.Reach, Runtime: m.machine.Runtime,
		Agents: append([]wire.MachineAgent{}, m.machine.Agents...), Health: m.machine.Health}
	subs := make([]func(wire.Event), 0, len(m.subs))
	for _, fn := range m.subs {
		subs = append(subs, fn)
	}
	return ev, subs
}

// judgeHealth samples the health and tells the sockets when it changed. Every socket of the machine channel hears it,
// with load or without: a page shows the health of the machines it does not have open too.
func (m *liveMachine) judgeHealth() {
	if m.health == nil {
		return
	}
	h := m.health.Sample()
	m.mu.Lock()
	if m.machine.Health != nil && reflect.DeepEqual(*m.machine.Health, h) {
		m.mu.Unlock()
		return
	}
	m.machine.Health = &h
	ev, subs := m.statusLocked()
	m.mu.Unlock()
	for _, fn := range subs {
		fn(ev)
	}
}

// modelsOf is the models the last look found for a provider.
func (m *liveMachine) modelsOf(p wire.Provider) []wire.AgentModel {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.machine.Agents {
		if a.Provider == p {
			return a.Models
		}
	}
	return nil
}

// run refreshes the agents' state every 30 s and the health every 5 s until ctx ends; the first looks are at once.
// The preflight takes seconds on a loaded machine: the health is judged beside it, never behind it.
func (m *liveMachine) run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		m.judgeHealth()
		t := time.NewTicker(machines.HealthEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.judgeHealth()
			}
		}
	}()
	defer wg.Wait()
	m.refresh()
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.refresh()
		}
	}
}
