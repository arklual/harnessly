package agentbrowser

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// changesPageContent excludes key releases and navigation keys from the durable
// human-input guard. Control/Meta shortcuts can paste, cut or undo page content.
func changesPageContent(method string, params any) bool {
	if method == "Input.insertText" {
		return true
	}
	if method != "Input.dispatchKeyEvent" {
		return false
	}
	var key struct {
		Type, Text, Key, Code string
		Modifiers             int
	}
	raw, err := json.Marshal(params)
	if err != nil || json.Unmarshal(raw, &key) != nil {
		return true // Malformed input must not bypass the guard.
	}
	if key.Type == "keyUp" {
		return false
	}
	return key.Text != "" || key.Key == "Backspace" || key.Key == "Delete" ||
		key.Code == "Backspace" || key.Code == "Delete" || key.Modifiers&(2|4) != 0
}

// personInput guards human keyboard/text dispatch and persists dirty/document
// transitions before Chrome receives input. AgentCall never uses this path.
func (i *Instance) personInput(ctx context.Context, id string, changesContent bool) (func(), error) {
	i.mu.Lock()
	p := i.pages[id]
	i.mu.Unlock()
	if p == nil {
		return nil, errors.New("unknown browser tab")
	}
	p.controlMu.Lock()
	if !changesContent {
		return p.controlMu.Unlock, nil
	}
	readCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	marker, err := i.personDocument(readCtx, id)
	cancel()
	if err != nil {
		marker = unknownHandoffDocument
	}
	i.mu.Lock()
	if i.pages[id] != p {
		i.mu.Unlock()
		p.controlMu.Unlock()
		return nil, errors.New("unknown browser tab")
	}
	dirty := !p.PersonInput || p.PersonDocument != marker
	if dirty {
		p.PersonInput, p.PersonDocument = true, marker
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

// personDocument reads browser-owned metadata without running page JavaScript.
func (i *Instance) personDocument(ctx context.Context, id string) (string, error) {
	raw, err := i.Call(ctx, id, "Page.getFrameTree", nil)
	if err != nil {
		return "", err
	}
	var result struct {
		Tree handoffTree `json:"frameTree"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Tree.Frame.Loader == "" {
		return "", errors.New("browser document unavailable")
	}
	return result.Tree.Frame.Loader, nil
}

// ErrPersonDocument keeps agent JavaScript away from a document containing human input.
var ErrPersonDocument = errors.New("The person typed on this page; JavaScript is unavailable here until the tab loads " +
	"a new document (navigating to the same address works). Use chrome_snapshot.")

// CheckPersonDocuments checks the live opener component before marking or executing
// agent JavaScript. Other tools and independent tabs do not use this guard.
func (i *Instance) CheckPersonDocuments(ctx context.Context, conv, id string) error {
	if err := i.agentAllowed(conv, id, "Runtime.evaluate"); err != nil {
		return err
	}
	i.mu.Lock()
	var pages []*page
	for related := range i.openerComponentLocked(id) {
		if p := i.pages[related]; p != nil {
			pages = append(pages, p)
		}
	}
	i.mu.Unlock()
	for _, p := range pages {
		if err := i.checkPersonDocument(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

func (i *Instance) checkPersonDocument(ctx context.Context, p *page) error {
	p.controlMu.Lock()
	defer p.controlMu.Unlock()
	i.mu.Lock()
	present, marker, control := i.pages[p.ID] == p, p.PersonDocument, p.Control
	i.mu.Unlock()
	if !present {
		return nil
	}
	if control == "person" {
		return ErrPersonControl
	}
	if marker == "" {
		return nil
	}
	readCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	current, err := i.personDocument(readCtx, p.ID)
	cancel()
	if err != nil {
		return ErrPersonDocument
	}
	if marker == unknownHandoffDocument {
		i.mu.Lock()
		p.PersonDocument = current
		i.changedLocked()
		i.mu.Unlock()
		i.save()
		// Refuse this call even after the unknown marker has been anchored.
		return ErrPersonDocument
	}
	if marker == current {
		return ErrPersonDocument
	}
	return nil
}
