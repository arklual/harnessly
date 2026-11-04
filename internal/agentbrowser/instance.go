package agentbrowser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser/host"
	"github.com/ly-solutions/harnessly/internal/cdp"
)

type Profile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"-"`
}
type Viewport struct {
	Mode string  `json:"mode"`
	W    int     `json:"w"`
	H    int     `json:"h"`
	DPR  float64 `json:"dpr"`
}
type Tab struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	URL          string   `json:"url"`
	Conversation string   `json:"conversation,omitempty"`
	By           string   `json:"by"`
	Viewport     Viewport `json:"viewport"`
	Crashed      bool     `json:"crashed"`
}
type page struct {
	Tab
	session string
	viewers map[*Viewer]bool
	last    *Frame
}
type registry struct {
	Tabs    []Tab             `json:"tabs"`
	Current map[string]string `json:"current"`
	Hidden  map[string]bool   `json:"hidden"`
}
type Instance struct {
	manager        *Manager
	profile        Profile
	dir            string
	op             sync.Mutex
	mu             sync.Mutex
	conn           *cdp.Conn
	pages          map[string]*page
	current        map[string]string
	hidden         map[string]bool
	state, failure string
	changes        map[chan struct{}]bool
	refresh        chan struct{}
	acks           chan ack
}
type ack struct {
	session string
	id      int64
}

func newInstance(m *Manager, id string) *Instance {
	kind := "managed"
	if m.cfg.CDP != "" {
		kind = "attached"
	}
	i := &Instance{manager: m, profile: Profile{id, "Common", kind}, dir: filepath.Join(m.cfg.DataDir, "agent-browser", "profiles", id), pages: map[string]*page{}, current: map[string]string{}, hidden: map[string]bool{}, state: "starting", changes: map[chan struct{}]bool{}, refresh: make(chan struct{}, 1), acks: make(chan ack, 256)}
	if b, e := os.ReadFile(filepath.Join(i.dir, "tabs.json")); e == nil {
		var r registry
		if json.Unmarshal(b, &r) == nil {
			for _, t := range r.Tabs {
				i.pages[t.ID] = &page{Tab: t, viewers: map[*Viewer]bool{}}
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
func (i *Instance) Profile() Profile { return i.profile }
func (i *Instance) State() (string, string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.state, i.failure
}
func (i *Instance) notifyLocked() {
	for c := range i.changes {
		select {
		case c <- struct{}{}:
		default:
		}
	}
}
func (i *Instance) setState(s string, err error) {
	i.mu.Lock()
	i.state = s
	i.failure = ""
	if err != nil {
		i.failure = err.Error()
	}
	i.notifyLocked()
	i.mu.Unlock()
}
func (i *Instance) Changes() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	i.mu.Lock()
	i.changes[ch] = true
	i.mu.Unlock()
	return ch, func() { i.mu.Lock(); delete(i.changes, ch); i.mu.Unlock() }
}
func (i *Instance) run() {
	ctx := i.manager.ctx
	defer i.setState("down", nil)
	for ctx.Err() == nil {
		i.setState("starting", nil)
		wasUp := false
		attempt, cancel := context.WithTimeout(ctx, 15*time.Second)
		c, err := i.connect(attempt)
		cancel()
		if err == nil {
			i.op.Lock()
			i.mu.Lock()
			i.conn = c
			for _, p := range i.pages {
				p.session = ""
				p.last = nil
			}
			i.mu.Unlock()
			go func() { _ = c.Run(ctx) }()
			_, err = c.Call(ctx, "Target.setDiscoverTargets", map[string]any{"discover": true}, "")
			if err == nil {
				err = i.reconcile(ctx, true)
			}
			i.op.Unlock()
			if err == nil {
				i.setState("up", nil)
				wasUp = true
				err = i.connected(ctx, c)
			}
			_ = c.Close()
		}
		if ctx.Err() != nil {
			return
		}
		if wasUp {
			i.setState("down", err)
		} else {
			i.setState("error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}
func (i *Instance) connected(ctx context.Context, c *cdp.Conn) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.Done():
			return cdp.ErrClosed
		case a := <-i.acks:
			_, _ = c.Call(ctx, "Page.screencastFrameAck", map[string]any{"sessionId": a.id}, a.session)
		case <-i.refresh:
			i.op.Lock()
			err := i.reconcile(ctx, false)
			i.op.Unlock()
			if err != nil {
				return err
			}
		}
	}
}
func (i *Instance) save() error {
	i.mu.Lock()
	r := registry{Current: i.current, Hidden: i.hidden, Tabs: make([]Tab, 0, len(i.pages))}
	for _, p := range i.pages {
		r.Tabs = append(r.Tabs, p.Tab)
	}
	sort.Slice(r.Tabs, func(a, b int) bool { return r.Tabs[a].ID < r.Tabs[b].ID })
	b, err := json.MarshalIndent(r, "", "  ")
	i.mu.Unlock()
	if err != nil {
		return err
	}
	if err = host.PrivateDir(i.dir); err != nil {
		return err
	}
	f, err := os.CreateTemp(i.dir, ".tabs-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(i.dir, "tabs.json"))
}

type target struct {
	ID     string `json:"targetId"`
	Type   string `json:"type"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Opener string `json:"openerId"`
}

func (i *Instance) reconcile(ctx context.Context, initial bool) error {
	raw, err := i.conn.Call(ctx, "Target.getTargets", nil, "")
	if err != nil {
		return err
	}
	var r struct {
		Targets []target `json:"targetInfos"`
	}
	if err = json.Unmarshal(raw, &r); err != nil {
		return err
	}
	// Establish a service window before exposing tabs, including when Chrome initially has no targets.
	if initial && i.profile.Kind == "managed" {
		i.mu.Lock()
		reserved := false
		for _, t := range r.Targets {
			if i.hidden[t.ID] {
				reserved = true
			}
		}
		i.mu.Unlock()
		if !reserved {
			raw, e := i.conn.Call(ctx, "Target.createTarget", map[string]any{"url": "about:blank", "newWindow": true, "background": true}, "")
			if e != nil {
				return e
			}
			var service struct {
				ID string `json:"targetId"`
			}
			if e = json.Unmarshal(raw, &service); e != nil {
				return e
			}
			i.mu.Lock()
			i.hidden[service.ID] = true
			i.mu.Unlock()
			r.Targets = append(r.Targets, target{ID: service.ID, Type: "page", URL: "about:blank"})
		}
	}
	i.mu.Lock()
	live := map[string]bool{}
	for _, t := range r.Targets {
		if t.Type != "page" {
			continue
		}
		live[t.ID] = true
		if i.hidden[t.ID] {
			continue
		}
		p := i.pages[t.ID]
		if p == nil {
			p = &page{Tab: Tab{ID: t.ID, By: "person"}, viewers: map[*Viewer]bool{}}
			i.pages[t.ID] = p
		}
		p.Title = t.Title
		p.URL = t.URL
	}
	// Resolve openers in a second pass: Chrome need not return parents first.
	for _, t := range r.Targets {
		if p, parent := i.pages[t.ID], i.pages[t.Opener]; p != nil && parent != nil && p.Conversation == "" {
			p.Conversation = parent.Conversation
			p.By = parent.By
		}
	}
	for id := range i.hidden {
		if !live[id] {
			delete(i.hidden, id)
		}
	}
	for id := range i.pages {
		if !live[id] {
			delete(i.pages, id)
		}
	}
	for conv, id := range i.current {
		if i.pages[id] == nil {
			delete(i.current, conv)
		}
	}
	pages := make([]*page, 0, len(i.pages))
	for _, p := range i.pages {
		pages = append(pages, p)
	}
	i.mu.Unlock()
	for _, p := range pages {
		if p.session == "" {
			raw, err = i.conn.Call(ctx, "Target.attachToTarget", map[string]any{"targetId": p.ID, "flatten": true}, "")
			if err != nil {
				continue
			}
			var a struct {
				Session string `json:"sessionId"`
			}
			if err = json.Unmarshal(raw, &a); err != nil {
				return err
			}
			i.mu.Lock()
			p.session = a.Session
			p.Crashed = false
			i.mu.Unlock()
			if _, err = i.conn.Call(ctx, "Emulation.setFocusEmulationEnabled", map[string]any{"enabled": true}, a.Session); err != nil {
				return err
			}
			if _, err = i.conn.Call(ctx, "Inspector.enable", nil, a.Session); err != nil {
				return err
			}
			if p.Viewport.W > 0 {
				if err = i.viewport(ctx, p, p.Viewport); err != nil {
					return err
				}
			}
			i.mu.Lock()
			viewers := len(p.viewers)
			i.mu.Unlock()
			if viewers > 0 {
				if err = i.startCast(ctx, p); err != nil {
					return err
				}
			}
		}
	}
	if err = i.save(); err != nil {
		return err
	}
	i.mu.Lock()
	i.notifyLocked()
	i.mu.Unlock()
	return nil
}
func (i *Instance) event(method, session string, raw json.RawMessage) {
	switch method {
	case "Target.targetCreated", "Target.targetInfoChanged", "Target.targetDestroyed", "Target.detachedFromTarget":
		select {
		case i.refresh <- struct{}{}:
		default:
		}
	case "Inspector.targetCrashed":
		i.mu.Lock()
		for _, p := range i.pages {
			if p.session == session {
				p.Crashed = true
			}
		}
		i.notifyLocked()
		i.mu.Unlock()
	case "Page.screencastFrame":
		var f struct {
			Data     string `json:"data"`
			ID       int64  `json:"sessionId"`
			Metadata struct {
				W float64 `json:"deviceWidth"`
				H float64 `json:"deviceHeight"`
			} `json:"metadata"`
		}
		if json.Unmarshal(raw, &f) != nil {
			return
		}
		select {
		case i.acks <- ack{session, f.ID}:
		default:
		}
		jpeg, err := base64.StdEncoding.DecodeString(f.Data)
		if err != nil {
			return
		}
		frame := Frame{int(f.Metadata.W), int(f.Metadata.H), jpeg}
		i.mu.Lock()
		defer i.mu.Unlock()
		for _, p := range i.pages {
			if p.session == session {
				p.last = &frame
				for v := range p.viewers {
					latest(v.frames, frame)
				}
				break
			}
		}
	}
}
func latest[T any](ch chan T, v T) {
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- v:
	default:
	}
}
func (i *Instance) Tabs(conv string) []Tab {
	i.mu.Lock()
	defer i.mu.Unlock()
	out := []Tab{}
	for _, p := range i.pages {
		if conv == "" || p.Conversation == conv {
			out = append(out, p.Tab)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}
func (i *Instance) Current(conv string) string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.current[conv]
}
func (i *Instance) Select(conv, id string) error {
	i.op.Lock()
	defer i.op.Unlock()
	i.mu.Lock()
	if i.pages[id] == nil {
		i.mu.Unlock()
		return errors.New("unknown browser tab")
	}
	i.current[conv] = id
	i.notifyLocked()
	i.mu.Unlock()
	return i.save()
}
func (i *Instance) NewTab(ctx context.Context, conv, by, url string) (Tab, error) {
	i.op.Lock()
	defer i.op.Unlock()
	if by != "agent" && by != "person" {
		return Tab{}, errors.New("invalid tab author")
	}
	url = NormalizeURL(url)
	if url == "" {
		url = "about:blank"
	}
	raw, err := i.conn.Call(ctx, "Target.createTarget", map[string]any{"url": url, "newWindow": true}, "")
	if err != nil {
		return Tab{}, err
	}
	var r struct {
		ID string `json:"targetId"`
	}
	if err = json.Unmarshal(raw, &r); err != nil {
		return Tab{}, err
	}
	p := &page{Tab: Tab{ID: r.ID, URL: url, Conversation: conv, By: by}, viewers: map[*Viewer]bool{}}
	i.mu.Lock()
	i.pages[r.ID] = p
	i.current[conv] = r.ID
	i.mu.Unlock()
	if err = i.reconcile(ctx, false); err != nil {
		return Tab{}, err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	return p.Tab, nil
}
func (i *Instance) CloseTab(ctx context.Context, id string) error {
	i.op.Lock()
	defer i.op.Unlock()
	i.mu.Lock()
	p := i.pages[id]
	i.mu.Unlock()
	if p == nil {
		return errors.New("unknown browser tab")
	}
	_, err := i.conn.Call(ctx, "Target.closeTarget", map[string]any{"targetId": id}, "")
	if err != nil {
		return err
	}
	// closeTarget acknowledges the request before targetDestroyed, especially under Xvfb.
	for n := 0; n < 150; n++ {
		raw, e := i.conn.Call(ctx, "Target.getTargets", nil, "")
		if e != nil {
			return e
		}
		var r struct {
			Targets []target `json:"targetInfos"`
		}
		if e = json.Unmarshal(raw, &r); e != nil {
			return e
		}
		found := false
		for _, t := range r.Targets {
			if t.ID == id {
				found = true
				break
			}
		}
		if !found {
			return i.reconcile(ctx, false)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	return errors.New("browser tab did not close")
}
func (i *Instance) Call(ctx context.Context, id, method string, params any) (json.RawMessage, error) {
	i.op.Lock()
	defer i.op.Unlock()
	i.mu.Lock()
	p := i.pages[id]
	i.mu.Unlock()
	if p == nil {
		return nil, fmt.Errorf("unknown browser tab %q", id)
	}
	return i.conn.Call(ctx, method, params, p.session)
}
