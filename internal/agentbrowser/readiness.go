package agentbrowser

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser/provision"
	"github.com/ly-solutions/harnessly/internal/cdp"
)

// PreparingError asks an agent to retry while installation continues in the manager's lifetime.
type PreparingError struct{ Status Status }

// Error gives agents a concise retry instruction.
func (e *PreparingError) Error() string {
	return fmt.Sprintf("Preparing browser (%d%%): %s. Retry in 20 seconds.", e.Status.Progress, e.Status.Hint)
}

// Ready starts lazy preparation and returns immediately. State is preparing, up (executable ready), error or down.
// Tools should call this before InstanceFor; preparing includes progress and a retry hint, never a blocked download.
func (m *Manager) Ready(ctx context.Context) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Status{T: "status", State: "up"}
	if m.cfg.Off {
		s.State = "down"
		s.Error = ErrOff.Error()
		return s
	}
	if ctx.Err() != nil {
		s.State = "error"
		s.Error = ctx.Err().Error()
		return s
	}
	if m.ctx.Err() != nil {
		s.State = "down"
		s.Error = cdp.ErrClosed.Error()
		return s
	}
	if m.cfg.CDP != "" {
		s.Hint = "Using externally managed Chrome"
		return s
	}
	if m.preparation == nil || m.preparation.status.State == "error" &&
		time.Since(m.preparation.finished) >= 15*time.Second {
		m.preparation = &browserPreparation{status: Status{
			T: "status", State: "preparing", Hint: "Checking Chrome installation"}}
		m.wg.Add(1)
		go m.prepareBrowser()
	}
	return m.preparation.status
}

type browserPreparation struct {
	status   Status
	path     string
	finished time.Time
}

// Retry immediately rechecks a failed preparation after an operator repairs the machine.
// An in-flight preparation is shared rather than duplicated.
func (m *Manager) Retry(ctx context.Context) Status {
	m.mu.Lock()
	if m.preparation != nil && m.preparation.status.State == "error" {
		m.preparation.finished = time.Time{}
	}
	m.mu.Unlock()
	return m.Ready(ctx)
}

func (m *Manager) prepareBrowser() {
	defer m.wg.Done()
	cfg := provision.FromEnv(m.cfg.DataDir)
	if m.cfg.Chrome != "" {
		cfg.Chrome = m.cfg.Chrome
	}
	path, err := provision.Ensure(m.ctx, cfg, func(p provision.Progress) {
		m.mu.Lock()
		m.preparation.status.Progress = p.Percent
		m.preparation.status.Hint = p.Hint
		m.mu.Unlock()
	})
	var info provision.Inspection
	if err == nil {
		info, err = m.probe(m.ctx, path, m.cfg.DataDir)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.preparation.finished = time.Now()
	s := &m.preparation.status
	s.Mode = provision.Mode(m.cfg.Mode)
	s.Sandbox = info.Sandbox
	if err != nil {
		s.State = "error"
		s.Error = err.Error()
		var diagnostic *provision.Diagnostic
		if errors.As(err, &diagnostic) {
			s.Command = diagnostic.Command
		}
	} else {
		s.State = "up"
		s.Progress = 100
		s.Hint = info.Version + "; " + s.Mode + "; sandbox: " + info.Sandbox
		m.preparation.path = path
	}
}

func (m *Manager) preparedChrome(ctx context.Context) (string, error) {
	for {
		s := m.Ready(ctx)
		if s.State == "error" || s.State == "down" {
			return "", &provision.Diagnostic{Message: s.Error, Command: s.Command}
		}
		if s.State == "up" {
			m.mu.Lock()
			path := m.preparation.path
			m.mu.Unlock()
			return path, nil
		}
		// Preserve Instance's existing wait-for-startup contract for installed browsers, but never await a download.
		if s.Progress > 0 || strings.HasPrefix(s.Hint, "Downloading") || strings.Contains(s.Hint, "download lock") {
			return "", &PreparingError{Status: s}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}
