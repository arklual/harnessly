package agentbrowser

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser/host"
	"github.com/ly-solutions/harnessly/internal/cdp"
)

const helloTimeout = 3 * time.Second

// hostBuild asks the host on the other end of a fresh connection for its build: the first exchange of the stream,
// before any CDP client uses it. A host too old to know Host.hello answers with Chrome's error: build "".
func hostBuild(conn net.Conn, stream *cdp.Stream) (string, error) {
	_ = conn.SetDeadline(time.Now().Add(helloTimeout))
	defer func() { _ = conn.SetDeadline(time.Time{}) }()
	ctx, cancel := context.WithTimeout(context.Background(), helloTimeout)
	defer cancel()
	if err := stream.Write(ctx, []byte(`{"id":1,"method":"`+host.HelloMethod+`"}`)); err != nil {
		return "", err
	}
	for {
		raw, err := stream.Read(ctx)
		if err != nil {
			return "", err
		}
		var msg struct {
			ID     *int `json:"id"`
			Result struct {
				Build string `json:"build"`
			} `json:"result"`
		}
		if json.Unmarshal(raw, &msg) != nil || msg.ID == nil || *msg.ID != 1 {
			continue
		}
		return msg.Result.Build, nil
	}
}

func (m *Manager) profileDir(id string) string {
	return filepath.Join(m.cfg.DataDir, "agent-browser", "profiles", id)
}

func (m *Manager) profileSocket(id string) string {
	return host.SocketPath(filepath.Join(m.cfg.DataDir, "agent-browser", "run"), id)
}

// runningHosts are the managed profiles whose host is running now, whoever started it.
func (m *Manager) runningHosts() []string {
	entries, err := os.ReadDir(filepath.Join(m.cfg.DataDir, "agent-browser", "profiles"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && profileID.MatchString(e.Name()) && host.Running(m.profileDir(e.Name())) {
			out = append(out, e.Name())
		}
	}
	return out
}

// staleHost says whether the host of a profile is not this runtime's build (or does not answer).
func (m *Manager) staleHost(ctx context.Context, id string) bool {
	conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", m.profileSocket(id))
	if err != nil {
		return true
	}
	stream := cdp.NewStream(conn)
	defer stream.Close()
	build, err := hostBuild(conn, stream)
	return err != nil || build != host.BuildID()
}

// Sweep stops the hosts this runtime will not use, at its start: every one with RUNTIME_BROWSER=off, otherwise those
// of another build (an update). They start again, of this build, on the next use. A profile in use is left alone.
func (m *Manager) Sweep(ctx context.Context) {
	for _, id := range m.runningHosts() {
		unlock, err := m.lockProfile(ctx, id)
		if err != nil {
			return
		}
		m.mu.Lock()
		live := m.instances[id] != nil
		m.mu.Unlock()
		if !live && (m.cfg.Off || m.staleHost(ctx, id)) {
			stop, cancel := context.WithTimeout(ctx, cdp.DefaultTimeout)
			_ = host.Stop(stop, m.profileDir(id), m.profileSocket(id))
			cancel()
		}
		unlock()
	}
}

// tryLockProfile is lockProfile without waiting: a profile someone is starting or stopping is not evicted.
func (m *Manager) tryLockProfile(id string) (func(), bool) {
	m.mu.Lock()
	lock := m.profileLocks[id]
	if lock == nil {
		lock = make(chan struct{}, 1)
		m.profileLocks[id] = lock
	}
	m.mu.Unlock()
	select {
	case lock <- struct{}{}:
		return func() { <-lock }, true
	default:
		return nil, false
	}
}

// idleness is how long a running host has been unused: a host no instance of this runtime holds (left by another
// runtime) first, a busy one (a call, a viewer) never.
func (m *Manager) idleness(id string, now time.Time) (time.Duration, bool) {
	m.mu.Lock()
	i := m.instances[id]
	m.mu.Unlock()
	if i == nil {
		return time.Duration(1<<63 - 1), true
	}
	if i.profile.Kind != "managed" {
		return 0, false
	}
	idle := i.idleFor(now)
	return idle, idle > 0
}

// makeRoom keeps the running hosts under MaxHosts before profile self starts one: it closes the longest idle, and
// refuses while every one is busy.
func (m *Manager) makeRoom(ctx context.Context, self string) error {
	if m.cfg.MaxHosts <= 0 {
		return nil
	}
	tried := map[string]bool{}
	for {
		running := 0
		victim, longest := "", time.Duration(-1)
		now := time.Now()
		for _, id := range m.runningHosts() {
			if id == self {
				continue
			}
			running++
			if tried[id] {
				continue
			}
			if idle, ok := m.idleness(id, now); ok && idle > longest {
				victim, longest = id, idle
			}
		}
		if running < m.cfg.MaxHosts {
			return nil
		}
		if victim == "" {
			return fmt.Errorf("%d agent browsers are running and busy, the most this machine runs at once "+
				"(RUNTIME_BROWSER_MAX_HOSTS): try again when one is idle", m.cfg.MaxHosts)
		}
		tried[victim] = true
		unlock, ok := m.tryLockProfile(victim)
		if !ok {
			continue
		}
		m.mu.Lock()
		orphan := m.instances[victim] == nil
		m.mu.Unlock()
		if orphan {
			_ = host.Stop(ctx, m.profileDir(victim), m.profileSocket(victim))
		} else if _, idle := m.idleness(victim, time.Now()); idle {
			_ = m.stopProfile(ctx, victim, false)
		}
		unlock()
	}
}
