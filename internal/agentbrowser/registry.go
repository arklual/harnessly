package agentbrowser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	if t.Sealed {
		t.SecretUntracked = true
	}
	if t.Control == "" {
		t.Control = "agent"
	}
	return &page{Tab: t, redactionLost: t.SecretTouched, gate: make(chan struct{}, 1), viewers: make(map[*Viewer]bool)}
}

func newInstance(m *Manager, id string) *Instance {
	profile, _ := m.lookupProfile(id)
	ctx, cancel := context.WithCancel(m.ctx)
	i := &Instance{
		manager: m, profile: profile, ctx: ctx, cancel: cancel, done: make(chan struct{}), lastActivity: time.Now(),
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
				t.Dialog = nil
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
	i.saveMu.Lock()
	defer i.saveMu.Unlock()
	i.mu.Lock()
	if i.revision == i.saved {
		i.mu.Unlock()
		return
	}
	revision := i.revision
	r := registry{Current: i.current, Hidden: i.hidden, Tabs: i.tabsLocked("")}
	for n := range r.Tabs {
		r.Tabs[n].Dialog = nil
		// Once a value has entered this browser, page metadata may contain it later.
		// Redaction keys are intentionally not persisted; omit metadata on restart too.
		if r.Tabs[n].SecretTouched {
			r.Tabs[n].URL, r.Tabs[n].Title = "about:blank", ""
		}
	}
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
			tab := tabSnapshot(p.Tab)
			tab.Title, tab.URL = i.redactLocked(tab.Title), i.redactLocked(tab.URL)
			if p.redactionLost {
				tab.Title, tab.URL = "", "about:blank"
			}
			if tab.Dialog != nil {
				tab.Dialog.Message = i.redactLocked(tab.Dialog.Message)
				tab.Dialog.DefaultPrompt = i.redactLocked(tab.Dialog.DefaultPrompt)
			}
			out = append(out, tab)
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
	if p := i.pages[i.current[conv]]; p != nil && p.Conversation == conv && !p.provisional && Shown(p.URL) {
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

// Current returns the agent working tab, falling back to the conversation's newest tab
// when no explicit selection survives.
func (i *Instance) Current(conv string) string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.currentLocked(conv)
}

// SetAgentTab selects an owned working tab without changing pinned panel views.
func (i *Instance) SetAgentTab(conv, id string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closing || i.manager.ctx.Err() != nil {
		return cdp.ErrClosed
	}
	if p := i.pages[id]; p == nil || !Shown(p.URL) {
		return errors.New("unknown browser tab")
	}
	if i.pages[id].Conversation != conv {
		return ErrNotOwner
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
	if p.login != nil {
		p.login.settle("cancelled", i.redactLocked(p.URL))
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
	return i.newTab(ctx, conv, by, url, nil)
}

func (i *Instance) newTab(ctx context.Context, conv, by, url string, login *loginRequest) (Tab, error) {
	if i.manager.cfg.ProfileOf != nil && i.manager.profileFor(conv) != i.profile.ID {
		return Tab{}, ErrProfileUnavailable
	}
	if err := i.beginOperation(); err != nil {
		return Tab{}, err
	}
	defer i.endOperation()
	ctx, cancel := cdp.WithTimeout(ctx)
	defer cancel()
	if by != "agent" && by != "person" {
		return Tab{}, errors.New("invalid tab author")
	}
	// targetCreated precedes createTarget's reply. Do not expose this brief unowned state.
	i.mu.Lock()
	if limit := i.manager.cfg.MaxTabs; limit > 0 && len(i.pages)+i.creating >= limit {
		i.mu.Unlock()
		return Tab{}, fmt.Errorf("%d tabs are open in this browser, the most it keeps (RUNTIME_BROWSER_MAX_TABS): "+
			"close one first (chrome_close_tab)", limit)
	}
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
	// Start agent pages blank: native window bounds include browser chrome and do not
	// guarantee CSS dimensions on Xvfb. Apply the viewport before the first site load.
	initialURL := url
	if by == "agent" {
		initialURL = "about:blank"
	}
	raw, err := c.Call(ctx, "Target.createTarget", map[string]any{"url": initialURL, "newWindow": true}, "")
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
	working := i.currentLocked(conv)
	p := i.upsertLocked(target{ID: r.ID, Type: "page", URL: url})
	p.Conversation, p.By = conv, by
	if login != nil {
		p.URL = initialURL
		p.Control = "person"
		p.login = login
		login.mu.Lock()
		login.instance = i
		login.ask.Tab = p.ID
		login.mu.Unlock()
	}
	if by == "person" {
		p.Viewport = Viewport{Mode: "fit", DPR: 1}
	}
	p.provisional = false
	i.attributeOpenersLocked()
	if by == "agent" || working == "" {
		i.current[conv] = r.ID
	}
	i.changedLocked()
	i.mu.Unlock()
	if login != nil {
		defer func() {
			if err != nil {
				cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = i.CloseTab(cleanup, p.ID)
			}
		}()
	}
	if _, err = i.prepare(ctx, p, c); err != nil {
		return Tab{}, err
	}
	if by == "agent" {
		if err = i.SetViewport(ctx, p.ID, agentViewport()); err != nil {
			return Tab{}, err
		}
		if url != "about:blank" {
			if _, err = i.Call(ctx, p.ID, "Page.navigate", map[string]any{"url": url}); err != nil {
				return Tab{}, err
			}
		}
	}
	// A switch may have closed the old registry while Chrome was creating this
	// target. Do not leave the late target behind in a now-unavailable profile.
	if i.manager.cfg.ProfileOf != nil && i.manager.profileFor(conv) != i.profile.ID {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = i.CloseTab(cleanup, p.ID)
		return Tab{}, ErrProfileUnavailable
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	return tabSnapshot(p.Tab), nil
}

// CloseTab bypasses a renderer's operation queue, including a renderer blocked in a dialog.
func (i *Instance) CloseTab(ctx context.Context, id string) error {
	if err := i.beginOperation(); err != nil {
		return err
	}
	defer i.endOperation()
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
	lastClose := time.Now()
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
		// A close acknowledged during a pending navigation can leave the target alive.
		// Retry the browser-side close instead of waiting out the entire call budget.
		if time.Since(lastClose) >= 250*time.Millisecond {
			if _, err = c.Call(ctx, "Target.closeTarget", map[string]any{"targetId": id}, ""); err != nil {
				return err
			}
			lastClose = time.Now()
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

func tabSnapshot(t Tab) Tab {
	if t.Dialog != nil {
		d := *t.Dialog
		t.Dialog = &d
	}
	return t
}
