package agentbrowser

import (
	"context"
	"encoding/json"

	"github.com/ly-solutions/harnessly/internal/cdp"
)

type target struct {
	ID     string `json:"targetId"`
	Type   string `json:"type"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Opener string `json:"openerId"`
}

func (i *Instance) upsertLocked(t target) *page {
	if t.Type != "page" || i.hidden[t.ID] {
		return nil
	}
	p := i.pages[t.ID]
	var before Tab
	if p != nil {
		before = p.Tab
	}
	if p == nil {
		i.sequence++
		p = newPage(Tab{ID: t.ID, By: "person", Order: i.sequence})
		p.provisional = i.creating > 0
		i.pages[t.ID] = p
	}
	p.Title, p.URL = t.Title, t.URL
	if parent := i.pages[t.Opener]; parent != nil && p.Conversation == "" {
		p.Conversation, p.By = parent.Conversation, parent.By
		// Site-created targets follow their agent-controlled opener without changing a pinned panel view.
		if parent.Control != "person" && i.current[parent.Conversation] == parent.ID {
			i.current[parent.Conversation] = p.ID
		}
	}
	if before != p.Tab {
		i.changedLocked()
	}
	if p.Conversation != "" {
		p.provisional = false
	}
	return p
}

func (i *Instance) reconcile(ctx context.Context, c *cdp.Conn, initial bool) error {
	i.mu.Lock()
	revision := i.revision
	i.mu.Unlock()
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
	if initial && i.profile.Kind == "managed" {
		i.mu.Lock()
		reserved := false
		fresh := len(i.hidden) == 0 && len(i.pages) == 0
		for _, t := range r.Targets {
			if i.hidden[t.ID] {
				reserved = true
			}
			if fresh && !reserved && t.Type == "page" && t.URL == "about:blank" {
				i.hidden[t.ID] = true
				reserved = true
			}
		}
		i.mu.Unlock()
		if !reserved {
			raw, err = c.Call(ctx, "Target.createTarget", map[string]any{"url": "about:blank",
				"newWindow": true, "background": true}, "")
			if err != nil {
				return err
			}
			var service struct {
				ID string `json:"targetId"`
			}
			if err = json.Unmarshal(raw, &service); err != nil {
				return err
			}
			i.mu.Lock()
			i.hidden[service.ID] = true
			i.mu.Unlock()
			r.Targets = append(r.Targets, target{ID: service.ID, Type: "page", URL: "about:blank"})
		}
	}
	i.mu.Lock()
	// Events or commands that arrived during discovery are newer than this snapshot.
	if !initial && revision != i.revision {
		i.mu.Unlock()
		return nil
	}
	live := map[string]bool{}
	for _, t := range r.Targets {
		live[t.ID] = true
		i.upsertLocked(t)
	}
	// Parents need not precede their popups in the discovery response.
	for _, t := range r.Targets {
		if p, parent := i.pages[t.ID], i.pages[t.Opener]; p != nil && parent != nil && p.Conversation == "" {
			p.Conversation, p.By = parent.Conversation, parent.By
		}
	}
	for id := range i.hidden {
		if !live[id] {
			delete(i.hidden, id)
			i.changedLocked()
		}
	}
	for id := range i.pages {
		if !live[id] {
			i.removeLocked(id)
		}
	}
	pages := make([]*page, 0, len(i.pages))
	for _, p := range i.pages {
		pages = append(pages, p)
	}
	if initial {
		i.changedLocked()
	}
	i.mu.Unlock()
	for _, p := range pages {
		i.schedulePrepare(p, c)
	}
	return nil
}

func (i *Instance) schedulePrepare(p *page, c *cdp.Conn) {
	i.mu.Lock()
	if p.preparing || (p.binding.conn == c && p.binding.session != "") {
		i.mu.Unlock()
		return
	}
	p.preparing = true
	i.mu.Unlock()
	i.task(func() {
		_, _ = i.prepare(i.manager.ctx, p, c)
		i.mu.Lock()
		p.preparing = false
		i.mu.Unlock()
	})
}
