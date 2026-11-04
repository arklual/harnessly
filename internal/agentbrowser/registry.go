package agentbrowser

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser/host"
	"github.com/ly-solutions/harnessly/internal/cdp"
)

type registry struct {
	Tabs    []Tab             `json:"tabs"`
	Current map[string]string `json:"current"`
	Hidden  map[string]bool   `json:"hidden"`
}

func newPage(t Tab) *page {
	return &page{Tab: t, gate: make(chan struct{}, 1), viewers: make(map[*Viewer]bool)}
}

func newInstance(m *Manager, id string) *Instance {
	kind := "managed"
	if m.cfg.CDP != "" {
		kind = "attached"
	}
	i := &Instance{
		manager: m, profile: Profile{id, "Common", kind},
		dir:   filepath.Join(m.cfg.DataDir, "agent-browser", "profiles", id),
		pages: make(map[string]*page), current: make(map[string]string), hidden: make(map[string]bool),
		state: "starting", changes: make(map[chan struct{}]bool), refresh: make(chan struct{}, 1),
		ackNext: make(map[string]time.Time),
	}
	if b, err := os.ReadFile(filepath.Join(i.dir, "tabs.json")); err == nil {
		var r registry
		if json.Unmarshal(b, &r) == nil {
			for _, t := range r.Tabs {
				if t.Order == 0 {
					t.Order = i.sequence + 1
				}
				i.sequence = max(i.sequence, t.Order)
				i.pages[t.ID] = newPage(t)
			}
			if r.Current != nil {
				i.current = r.Current
			}
			if r.Hidden != nil {
				i.hidden = r.Hidden
			}
		}
	}
	return i
}

func (i *Instance) persist(stop <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			// The reader and restoration tasks have stopped before the final snapshot.
			i.save()
			return
		case <-ticker.C:
			i.save()
		}
	}
}

func (i *Instance) save() {
	i.mu.Lock()
	if i.revision == i.saved {
		i.mu.Unlock()
		return
	}
	revision := i.revision
	r := registry{Current: i.current, Hidden: i.hidden, Tabs: i.tabsLocked("")}
	raw, err := json.MarshalIndent(r, "", "  ")
	i.mu.Unlock()
	if err == nil {
		err = writeRegistry(i.dir, raw)
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.saveError = err
	if err == nil {
		i.saved = revision
		i.writes++
	}
	if err != nil {
		i.notifyLocked()
	}
}

func writeRegistry(dir string, raw []byte) error {
	if err := host.PrivateDir(dir); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tabs-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, "tabs.json"))
}

// Shown excludes browser internals from the conversation's page list.
func Shown(url string) bool {
	return !strings.HasPrefix(url, "devtools:") && !strings.HasPrefix(url, "chrome-extension:") &&
		!strings.Contains(url, ".top-chrome")
}

func (i *Instance) tabsLocked(conv string) []Tab {
	out := []Tab{}
	for _, p := range i.pages {
		if !p.provisional && Shown(p.URL) && (conv == "" || p.Conversation == conv) {
			out = append(out, p.Tab)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Order < out[b].Order })
	return out
}

// Tabs returns creation order; an empty conversation returns all visible tabs for the person.
func (i *Instance) Tabs(conv string) []Tab {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.tabsLocked(conv)
}

func (i *Instance) currentLocked(conv string) string {
	if p := i.pages[i.current[conv]]; p != nil && Shown(p.URL) {
		return p.ID
	}
	tabs := i.tabsLocked(conv)
	if i.closing || i.manager.ctx.Err() != nil {
		if len(tabs) > 0 {
			return tabs[len(tabs)-1].ID
		}
		return ""
	}
	if len(tabs) == 0 {
		delete(i.current, conv)
		return ""
	}
	id := tabs[len(tabs)-1].ID
	i.current[conv] = id
	i.changedLocked()
	return id
}

// Current falls back to the conversation's newest tab when no explicit selection survives.
func (i *Instance) Current(conv string) string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.currentLocked(conv)
}

// Select changes this conversation's view; the authenticated person can inspect other owners' tabs.
func (i *Instance) Select(conv, id string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closing || i.manager.ctx.Err() != nil {
		return cdp.ErrClosed
	}
	if p := i.pages[id]; p == nil || !Shown(p.URL) {
		return errors.New("unknown browser tab")
	}
	i.current[conv] = id
	i.changedLocked()
	return nil
}

func (i *Instance) removeLocked(id string) {
	p := i.pages[id]
	if p == nil {
		return
	}
	delete(i.pages, id)
	for conv, current := range i.current {
		if current != id {
			continue
		}
		tabs := i.tabsLocked(conv)
		delete(i.current, conv)
		if len(tabs) > 0 {
			next := tabs[len(tabs)-1].ID
			for _, t := range tabs {
				if t.Order > p.Order {
					next = t.ID
					break
				}
			}
			i.current[conv] = next
		}
	}
	i.changedLocked()
}

// NewTab creates a separate window without waiting for work on any existing tab.
func (i *Instance) NewTab(ctx context.Context, conv, by, url string) (Tab, error) {
	if err := i.beginOperation(); err != nil {
		return Tab{}, err
	}
	defer i.tasks.Done()
	ctx, cancel := cdp.WithTimeout(ctx)
	defer cancel()
	if by != "agent" && by != "person" {
		return Tab{}, errors.New("invalid tab author")
	}
	// targetCreated precedes createTarget's reply. Do not expose this brief unowned state.
	i.mu.Lock()
	i.creating++
	i.mu.Unlock()
	defer i.finishCreating()
	url = NormalizeURL(url)
	if url == "" {
		url = "about:blank"
	}
	c := i.connection()
	if c == nil {
		return Tab{}, cdp.ErrClosed
	}
	raw, err := c.Call(ctx, "Target.createTarget", map[string]any{"url": url, "newWindow": true}, "")
	if err != nil {
		return Tab{}, err
	}
	var r struct {
		ID string `json:"targetId"`
	}
	if err = json.Unmarshal(raw, &r); err != nil {
		return Tab{}, err
	}
	i.mu.Lock()
	p := i.upsertLocked(target{ID: r.ID, Type: "page", URL: url})
	p.Conversation, p.By = conv, by
	p.provisional = false
	i.current[conv] = r.ID
	i.changedLocked()
	i.mu.Unlock()
	if _, err = i.prepare(ctx, p, c); err != nil {
		return Tab{}, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	return p.Tab, nil
}

// CloseTab bypasses a renderer's operation queue, including a renderer blocked in a dialog.
func (i *Instance) CloseTab(ctx context.Context, id string) error {
	if err := i.beginOperation(); err != nil {
		return err
	}
	defer i.tasks.Done()
	ctx, cancel := cdp.WithTimeout(ctx)
	defer cancel()
	_, c, err := i.lookup(id)
	if err != nil {
		return err
	}
	if _, err = c.Call(ctx, "Target.closeTarget", map[string]any{"targetId": id}, ""); err != nil {
		return err
	}
	// Chrome acknowledges before destruction. Events remove the tab as soon as they arrive.
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		raw, err := c.Call(ctx, "Target.getTargets", nil, "")
		if err != nil {
			return err
		}
		var r struct {
			Targets []target `json:"targetInfos"`
		}
		if err = json.Unmarshal(raw, &r); err != nil {
			return err
		}
		found := false
		for _, t := range r.Targets {
			if t.ID == id {
				found = true
				break
			}
		}
		if !found {
			i.mu.Lock()
			i.removeLocked(id)
			i.mu.Unlock()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (i *Instance) finishCreating() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.creating--
	if i.creating == 0 {
		// Pages created externally during the same interval remain visible and unowned.
		for _, p := range i.pages {
			p.provisional = false
		}
		i.changedLocked()
	}
}
