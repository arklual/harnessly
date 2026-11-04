package agentbrowser

import (
	"context"
	"errors"
)

// personInput guards human keyboard/text dispatch and persists its first dirty
// transition before Chrome receives input. AgentCall never uses this path.
func (i *Instance) personInput(id string) (func(), error) {
	i.mu.Lock()
	p := i.pages[id]
	i.mu.Unlock()
	if p == nil {
		return nil, errors.New("unknown browser tab")
	}
	p.controlMu.Lock()
	i.mu.Lock()
	if i.pages[id] != p {
		i.mu.Unlock()
		p.controlMu.Unlock()
		return nil, errors.New("unknown browser tab")
	}
	dirty := !p.PersonInput
	if dirty {
		p.PersonInput = true
		i.changedLocked()
	}
	i.mu.Unlock()
	if dirty {
		i.save()
		i.mu.Lock()
		err := i.saveError
		i.mu.Unlock()
		if err != nil {
			i.mu.Lock()
			p.PersonInput = false
			i.changedLocked()
			i.mu.Unlock()
			p.controlMu.Unlock()
			return nil, errors.New("cannot save browser input guard")
		}
	}
	return p.controlMu.Unlock, nil
}

// InspectPersonInputs checks human input before a chat tool reads or acts. The
// caller holds the conversation gate; CDP inspection never holds the instance mutex.
func (i *Instance) InspectPersonInputs(ctx context.Context, conv string) error {
	for _, tab := range i.Tabs(conv) {
		if !tab.PersonInput || tab.Control != "agent" {
			continue
		}
		if err := i.inspectPersonInput(ctx, conv, tab.ID); err != nil {
			return err
		}
	}
	return nil
}

func (i *Instance) inspectPersonInput(ctx context.Context, conv, id string) error {
	i.mu.Lock()
	p := i.pages[id]
	i.mu.Unlock()
	if p == nil {
		return nil
	}
	p.controlMu.Lock()
	defer p.controlMu.Unlock()
	i.mu.Lock()
	pending := i.pages[id] == p && p.Conversation == conv && p.Control == "agent" && p.PersonInput
	i.mu.Unlock()
	if !pending {
		return nil
	}
	marker, touched, err := i.inspectPersonPage(ctx, id)
	i.mu.Lock()
	if touched || err != nil {
		p.SealGeneration++
		p.Sealed, p.SecretTouched, p.SecretUntracked, p.SecretDocument = true, true, true, marker
	}
	if err == nil {
		p.PersonInput = false
	}
	i.changedLocked()
	i.mu.Unlock()
	i.save()
	i.mu.Lock()
	saveErr := i.saveError
	if saveErr != nil {
		p.PersonInput = true
		i.changedLocked()
	}
	i.mu.Unlock()
	if err != nil || saveErr != nil {
		return errors.New("browser tab sealed: personal input inspection failed; navigate or close the tab and retry")
	}
	return nil
}
