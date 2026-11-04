package agentbrowser

import "errors"

// MarkScripted records arbitrary agent execution before it is dispatched. Opener
// links permit script access in both directions, even after a tab navigates.
func (i *Instance) MarkScripted(conv, id string) error {
	if err := i.agentAllowed(conv, id, "Runtime.evaluate"); err != nil {
		return err
	}
	i.mu.Lock()
	p := i.pages[id]
	if p == nil || p.Conversation != conv {
		i.mu.Unlock()
		return ErrNotOwner
	}
	if p.Control == "person" {
		i.mu.Unlock()
		return ErrPersonControl
	}
	i.markScriptedLocked(id)
	i.mu.Unlock()
	// An evaluate may survive a runtime restart in Chrome. Its provenance must
	// reach disk before the caller is allowed to dispatch it.
	i.save()
	i.mu.Lock()
	err := i.saveError
	i.mu.Unlock()
	if err != nil {
		return errors.New("cannot save browser script provenance")
	}
	return nil
}

func (i *Instance) markScriptedLocked(id string) {
	connected := map[string]bool{id: true}
	for changed := true; changed; {
		changed = false
		for _, p := range i.pages {
			if p.Opener == "" {
				continue
			}
			if connected[p.ID] && !connected[p.Opener] {
				connected[p.Opener], changed = true, true
			}
			if connected[p.Opener] && !connected[p.ID] {
				connected[p.ID], changed = true, true
			}
		}
	}
	for id := range connected {
		if p := i.pages[id]; p != nil && !p.Scripted {
			p.Scripted = true
			i.changedLocked()
		}
	}
}

// attributeOpenersLocked also runs after late parent attribution. Repeating until
// stable handles nested popups arriving in any discovery or event order.
func (i *Instance) attributeOpenersLocked() {
	for changed := true; changed; {
		changed = false
		for _, p := range i.pages {
			parent := i.pages[p.Opener]
			if parent == nil {
				continue
			}
			before := p.Tab
			if p.Conversation == "" {
				p.Conversation, p.By, p.Control = parent.Conversation, parent.By, parent.Control
				if p.Conversation != "" {
					p.provisional = false
				}
				if parent.Control != "person" && i.current[parent.Conversation] == parent.ID {
					i.current[parent.Conversation] = p.ID
				}
				if p.By == "agent" && p.Viewport.Mode == "" {
					p.Viewport = agentViewport()
					p.defaultsPending = true
				}
			}
			if parent.Scripted && !p.Scripted || p.Scripted && !parent.Scripted {
				i.markScriptedLocked(p.ID)
			}
			if before != p.Tab {
				i.changedLocked()
				changed = true
			}
		}
	}
}
