package agentbrowser

import (
	"context"
	"encoding/json"

	"github.com/ly-solutions/harnessly/internal/cdp"
)

// Restore before ordinary reconciliation can discard target IDs from a stopped
// browser. IDs are Chrome-local; ownership, order, viewport and selection are not.
func (i *Instance) restoreTabs(ctx context.Context, c *cdp.Conn) error {
	if i.profile.Kind != "managed" {
		return nil
	}
	raw, err := c.Call(ctx, "Target.getTargets", nil, "")
	if err != nil {
		return err
	}
	var response struct {
		Targets []target `json:"targetInfos"`
	}
	if err = json.Unmarshal(raw, &response); err != nil {
		return err
	}
	live := make(map[string]bool)
	for _, t := range response.Targets {
		live[t.ID] = true
	}
	for _, old := range i.Tabs("") {
		if live[old.ID] {
			continue
		}
		i.mu.Lock()
		i.creating++
		i.mu.Unlock()
		raw, err = c.Call(ctx, "Target.createTarget", map[string]any{"url": "about:blank", "newWindow": true}, "")
		if err != nil {
			i.finishCreating()
			return err
		}
		var result struct {
			ID string `json:"targetId"`
		}
		if err = json.Unmarshal(raw, &result); err != nil {
			i.finishCreating()
			return err
		}
		p := newPage(old)
		p.ID, p.WindowID, p.Crashed = result.ID, 0, false
		i.mu.Lock()
		if before := i.pages[old.ID]; before != nil && before.login != nil {
			p.login = before.login
			p.login.mu.Lock()
			p.login.ask.Tab = p.ID
			p.login.mu.Unlock()
		}
		delete(i.pages, old.ID)
		i.pages[p.ID] = p
		for conv, id := range i.current {
			if id == old.ID {
				i.current[conv] = p.ID
			}
		}
		i.changedLocked()
		i.mu.Unlock()
		i.finishCreating()
		if _, err = i.prepare(ctx, p, c); err != nil {
			return err
		}
		if old.URL != "" && old.URL != "about:blank" {
			if _, err = i.Call(ctx, p.ID, "Page.navigate", map[string]any{"url": old.URL}); err != nil {
				return err
			}
		}
	}
	return nil
}
