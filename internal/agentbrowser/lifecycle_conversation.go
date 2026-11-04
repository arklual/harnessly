package agentbrowser

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"

	"github.com/ly-solutions/harnessly/internal/agentbrowser/host"
	"github.com/ly-solutions/harnessly/internal/cdp"
)

// CloseConversation closes owned tabs in every profile, including dormant
// registries. It never launches an unused browser to perform cleanup.
func (m *Manager) CloseConversation(ctx context.Context, conv string) error {
	if conv == "" {
		return errors.New("conversation id is required")
	}
	ids := make(map[string]bool)
	m.mu.Lock()
	for id := range m.instances {
		ids[id] = true
	}
	m.mu.Unlock()
	entries, err := os.ReadDir(filepath.Join(m.cfg.DataDir, "agent-browser", "profiles"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() && profileID.MatchString(entry.Name()) {
			ids[entry.Name()] = true
		}
	}
	var failures []error
	for id := range ids {
		if err := m.closeConversationProfile(ctx, id, conv); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (m *Manager) closeConversationProfile(ctx context.Context, id, conv string) error {
	unlock, err := m.lockProfile(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	m.mu.Lock()
	i := m.instances[id]
	m.mu.Unlock()
	if i != nil {
		for _, tab := range i.Tabs(conv) {
			if err := i.CloseTab(ctx, tab.ID); err != nil {
				return err
			}
		}
		i.save()
		i.mu.Lock()
		defer i.mu.Unlock()
		return i.saveError
	}
	// A corrupt registry must never be silently rewritten as an empty one.
	dir := filepath.Join(m.cfg.DataDir, "agent-browser", "profiles", id)
	raw, err := os.ReadFile(filepath.Join(dir, "tabs.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var saved registry
	if err = json.Unmarshal(raw, &saved); err != nil {
		return err
	}
	i = newInstance(m, id)
	defer i.cancel()
	if len(i.Tabs(conv)) == 0 {
		return nil
	}
	// Connect only to an existing private host. No readiness check or host.Start
	// belongs on this path, even when the host exits between the test and dial.
	profile, profileErr := m.lookupProfile(id)
	if profileErr != nil && m.cfg.LookupProfile != nil {
		return profileErr
	}
	if host.Running(dir) || profile.Kind == "attached" {
		callCtx, cancel := cdp.WithTimeout(ctx)
		defer cancel()
		var c *cdp.Conn
		if profile.Kind == "attached" {
			c, err = cdp.Dial(callCtx, profile.CDP, nil)
		} else {
			socket := host.SocketPath(filepath.Join(m.cfg.DataDir, "agent-browser", "run"), id)
			var nc net.Conn
			nc, err = (&net.Dialer{}).DialContext(callCtx, "unix", socket)
			if err == nil {
				c = cdp.New(cdp.NewStream(nc), nil)
			}
		}
		if err != nil {
			return err
		}
		defer c.Close()
		go func() { _ = c.Run(callCtx) }()
		for _, tab := range i.Tabs(conv) {
			if _, err = c.Call(callCtx, "Target.closeTarget", map[string]any{"targetId": tab.ID}, ""); err != nil {
				return err
			}
		}
	}
	i.mu.Lock()
	for _, tab := range i.tabsLocked(conv) {
		i.removeLocked(tab.ID)
	}
	i.mu.Unlock()
	i.save()
	return i.saveError
}

// ConversationHasTabs checks active snapshots and dormant registries without
// exposing tab metadata or starting Chrome.
func (m *Manager) ConversationHasTabs(conv string) bool {
	if conv == "" {
		return false
	}
	active := make(map[string]bool)
	m.mu.Lock()
	instances := make(map[string]*Instance, len(m.instances))
	for id, i := range m.instances {
		instances[id] = i
	}
	m.mu.Unlock()
	for id, i := range instances {
		active[id] = true
		if len(i.Tabs(conv)) > 0 {
			return true
		}
	}
	root := filepath.Join(m.cfg.DataDir, "agent-browser", "profiles")
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() || !profileID.MatchString(entry.Name()) || active[entry.Name()] {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, entry.Name(), "tabs.json"))
		if err != nil {
			continue
		}
		var saved registry
		if json.Unmarshal(raw, &saved) != nil {
			continue
		}
		for _, tab := range saved.Tabs {
			if tab.Conversation == conv && Shown(tab.URL) {
				return true
			}
		}
	}
	return false
}
