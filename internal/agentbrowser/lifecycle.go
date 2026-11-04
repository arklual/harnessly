package agentbrowser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser/host"
	"github.com/ly-solutions/harnessly/internal/cdp"
)

// Normal Chrome startup drops session cookies after graceful shutdown. Chrome
// session restoration can retain them, but managed tab restoration does not enable
// native session restore. Automatic shutdown is therefore opt-in.
func browserIdle(value string) time.Duration {
	d, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil || d < 0 {
		return 0
	}
	return d
}

func (m *Manager) lookupProfile(id string) (Profile, error) {
	if !profileID.MatchString(id) {
		return Profile{}, errors.New("invalid browser profile id")
	}
	if m.cfg.LookupProfile != nil {
		p, ok := m.cfg.LookupProfile(id)
		if !ok {
			return Profile{}, fmt.Errorf("unknown browser profile %q", id)
		}
		p.ID = id
		if p.Kind == "" {
			p.Kind = "managed"
		}
		if p.Kind != "managed" && p.Kind != "attached" {
			return Profile{}, errors.New("invalid browser profile kind")
		}
		if p.Kind == "attached" && strings.TrimSpace(p.CDP) == "" {
			return Profile{}, errors.New("attached browser profile needs a CDP endpoint")
		}
		return p, nil
	}
	p := Profile{ID: id, Name: id, Kind: "managed"}
	if id == DefaultProfile {
		p.Name = "Common"
	}
	if m.cfg.CDP != "" {
		p.Kind, p.CDP = "attached", m.cfg.CDP
	}
	return p, nil
}

// Serialize start/stop/erase for a profile without holding the manager mutex
// while Chrome responds. Waiters can still cancel independently.
func (m *Manager) lockProfile(ctx context.Context, id string) (func(), error) {
	if !profileID.MatchString(id) {
		return nil, errors.New("invalid browser profile id")
	}
	m.mu.Lock()
	lock := m.profileLocks[id]
	if lock == nil {
		lock = make(chan struct{}, 1)
		m.profileLocks[id] = lock
	}
	m.mu.Unlock()
	select {
	case lock <- struct{}{}:
		return func() { <-lock }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-m.ctx.Done():
		return nil, cdp.ErrClosed
	}
}

func (i *Instance) touch() {
	i.mu.Lock()
	i.lastActivity = time.Now()
	i.mu.Unlock()
}

func (i *Instance) endOperation() {
	i.mu.Lock()
	i.operations--
	i.lastActivity = time.Now()
	i.mu.Unlock()
	i.tasks.Done()
}

func (i *Instance) idleFor(now time.Time) time.Duration {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.idleForLocked(now)
}

func (i *Instance) idleForLocked(now time.Time) time.Duration {
	if i.operations > 0 || i.closing {
		i.lastActivity = now
		return 0
	}
	for _, p := range i.pages {
		if len(p.viewers) > 0 {
			i.lastActivity = now
			return 0
		}
	}
	return now.Sub(i.lastActivity)
}

func (m *Manager) stopIdle(i *Instance) {
	// Run cannot wait for its own completion. Use a manager worker so Close
	// also waits for an idle shutdown already in progress.
	m.mu.Lock()
	if m.ctx.Err() != nil {
		m.mu.Unlock()
		return
	}
	m.wg.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.wg.Done()
		ctx, cancel := context.WithTimeout(m.ctx, cdp.DefaultTimeout)
		defer cancel()
		unlock, err := m.lockProfile(ctx, i.profile.ID)
		if err != nil {
			return
		}
		defer unlock()
		m.mu.Lock()
		current := m.instances[i.profile.ID] == i
		m.mu.Unlock()
		if current && i.idleFor(time.Now()) >= m.cfg.Idle {
			_ = m.stopProfile(ctx, i.profile.ID, true)
		}
	}()
}

// StopProfile stops a managed browser and preserves its tab registry. Attached
// browsers are disconnected only. The next Instance access starts/reconnects it.
func (m *Manager) StopProfile(ctx context.Context, id string) error {
	unlock, err := m.lockProfile(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	return m.stopProfile(ctx, id, false)
}

func (m *Manager) stopProfile(ctx context.Context, id string, idle bool) error {
	profile, err := m.lookupProfile(id)
	if err != nil {
		return err
	}
	m.mu.Lock()
	i := m.instances[id]
	m.mu.Unlock()
	if i != nil {
		i.mu.Lock()
		if idle && i.idleForLocked(time.Now()) < m.cfg.Idle {
			i.mu.Unlock()
			return nil
		}
		i.closing = true
		conn := i.conn
		i.cancel()
		i.mu.Unlock()
		if conn != nil {
			_ = conn.Close()
		}
		select {
		case <-i.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if profile.Kind != "attached" {
		dir := filepath.Join(m.cfg.DataDir, "agent-browser", "profiles", id)
		socket := host.SocketPath(filepath.Join(m.cfg.DataDir, "agent-browser", "run"), id)
		if err = host.Stop(ctx, dir, socket); err != nil {
			return err
		}
	}
	m.mu.Lock()
	delete(m.instances, id)
	m.mu.Unlock()
	if i != nil {
		i.mu.Lock()
		err = i.saveError
		i.mu.Unlock()
	}
	return err
}

// ClearProfile stops Chrome and erases the managed profile, including cookies
// and the tab registry. An attached browser's data belongs to its operator.
func (m *Manager) ClearProfile(ctx context.Context, id string) error {
	unlock, err := m.lockProfile(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	p, err := m.lookupProfile(id)
	if err != nil {
		return err
	}
	if p.Kind == "attached" {
		return errors.New("cannot clear external browser data")
	}
	if err = m.stopProfile(ctx, id, false); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(m.cfg.DataDir, "agent-browser", "profiles", id))
}

// ProfileRunning reports a live connection or a managed host holding its lock.
// It never launches Chrome or connects to a dormant external browser.
func (m *Manager) ProfileRunning(id string) bool {
	if !profileID.MatchString(id) {
		return false
	}
	m.mu.Lock()
	i := m.instances[id]
	m.mu.Unlock()
	if i != nil {
		i.mu.Lock()
		up := !i.closing && i.state == "up" && i.conn != nil
		conn := i.conn
		i.mu.Unlock()
		if up {
			select {
			case <-conn.Done():
			default:
				return true
			}
		}
	}
	return host.Running(filepath.Join(m.cfg.DataDir, "agent-browser", "profiles", id))
}

func sandboxPreference(resolve func() bool) *bool {
	if resolve == nil {
		return nil
	}
	value := resolve()
	return &value
}

// RemoveProfile stops a profile and removes local data and metadata while holding
// its lifecycle lock, preventing a concurrent start between erasure and revocation.
// Attached Chrome is only disconnected; its external user-data directory is never touched.
func (m *Manager) RemoveProfile(ctx context.Context, id string, forget func() error) error {
	unlock, err := m.lockProfile(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	if err = m.stopProfile(ctx, id, false); err != nil {
		return err
	}
	if err = os.RemoveAll(filepath.Join(m.cfg.DataDir, "agent-browser", "profiles", id)); err != nil {
		return err
	}
	return forget()
}
