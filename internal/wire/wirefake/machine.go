package wirefake

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// LoadEvery is how often the machine channel carries machine_load while a page asked for it.
const LoadEvery = 2 * time.Second

// Machine is the fixtures' machine as a wire.MachineSource: always online, its load moving a little every 2 s.
type Machine struct {
	mu       sync.Mutex
	machine  wire.Machine
	load     wire.MachineLoad
	subs     map[int]func(wire.Event)
	next     int
	tool     *machineTool
	apps     *appsFeed
	settings *agentSettings
}

var _ wire.MachineSource = (*Machine)(nil)

// NewMachine reads the machine of the fixtures under id, announcing version and features.
func NewMachine(id, version string, features []wire.Feature) (*Machine, error) {
	shift := time.Now().UnixMilli() - fixtureNow
	snap, err := snapshotOf("snapshot.machine", shift, id)
	if err != nil {
		return nil, err
	}
	ev, err := eventOf("ev.machine_load", shift, id)
	if err != nil {
		return nil, err
	}
	m := snap.Machine.Machine
	m.ID, m.Name, m.Host, m.Load = id, "Fake", "localhost", nil
	m.Runtime = &wire.RuntimeInfo{Version: version, Proto: wire.Proto, Features: features}
	return &Machine{machine: m, load: ev.(wire.MachineLoadEv).Load, subs: map[int]func(wire.Event){}, tool: &machineTool{procs: fakeProcs()}, apps: newAppsFeed(), settings: newAgentSettings()}, nil
}

func (m *Machine) Snapshot(ctx context.Context, load bool) (wire.MachineSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	snap := wire.MachineSnapshot{Machine: clone(m.machine)}
	now := time.Now().UnixMilli()
	snap.Machine.LastSeenAt = &now
	if load {
		l := m.loadAt(time.Now())
		snap.Machine.Load = &l
	}
	return snap, nil
}

// loadAt is the fixture's load, its CPU on a slow wave so the page sees it move.
func (m *Machine) loadAt(t time.Time) wire.MachineLoad {
	l := m.load
	l.At = t.UnixMilli()
	l.CPU = math.Round((0.35+0.25*math.Sin(float64(t.Unix())/10))*100) / 100
	return l
}

func (m *Machine) Subscribe(load bool, fn func(wire.Event)) (cancel func()) {
	m.mu.Lock()
	id := m.next
	m.next++
	m.subs[id] = fn
	m.mu.Unlock()
	stop := make(chan struct{})
	if load {
		go func() {
			t := time.NewTicker(LoadEvery)
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case now := <-t.C:
					m.mu.Lock()
					l := m.loadAt(now)
					m.mu.Unlock()
					fn(wire.MachineLoadEv{Load: l})
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

// SetAgent changes the status of an agent CLI and tells the subscribers, as installing or logging in would.
func (m *Machine) SetAgent(p wire.Provider, st wire.AgentInstallStatus) {
	m.mu.Lock()
	for i := range m.machine.Agents {
		if m.machine.Agents[i].Provider == p {
			m.machine.Agents[i].Status = st
		}
	}
	m.tellLocked()
}

// SetHealth changes the machine's health and tells the subscribers, as the live runtime's judgement would.
func (m *Machine) SetHealth(h wire.MachineHealth) {
	m.mu.Lock()
	m.machine.Health = &h
	m.tellLocked()
}

// tellLocked sends machine_status to every subscriber and lets m.mu go.
func (m *Machine) tellLocked() {
	ev := wire.MachineStatusEv{Reach: m.machine.Reach, Runtime: m.machine.Runtime, Agents: clone(m.machine.Agents),
		Health: m.machine.Health}
	subs := make([]func(wire.Event), 0, len(m.subs))
	for _, fn := range m.subs {
		subs = append(subs, fn)
	}
	m.mu.Unlock()
	for _, fn := range subs {
		fn(ev)
	}
}
