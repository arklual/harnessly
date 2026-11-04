package agentbrowser

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/cdp"
)

// ErrNotOwner means the requested tab does not belong to the calling conversation.
var ErrNotOwner = errors.New("browser tab belongs to another conversation")

// ErrPersonControl means the person has reserved the tab's controls.
var ErrPersonControl = errors.New("browser tab is controlled by the person")

// ErrDialogOpen means the command needs a renderer blocked by a JavaScript dialog.
// It applies to Runtime.*, DOM.*, DOMSnapshot.*, Accessibility.*, Page.getLayoutMetrics,
// Page.getFrameTree, Page.getResourceTree, Page.captureScreenshot, Input.insertText,
// Emulation.setDeviceMetricsOverride and Emulation.clearDeviceMetricsOverride.
// Mouse/key dispatch and Page.handleJavaScriptDialog remain available.
var ErrDialogOpen = errors.New("browser tab has an open JavaScript dialog")

// These renderer-routed commands stall behind a modal dialog in managed Chrome.
// Mouse/key dispatch and browser-side dialog handling remain available. Keep this
// list aligned with the raw CDP measurements in TestChromeDialogMethods.
func rendererCommand(method string) bool {
	return strings.HasPrefix(method, "Runtime.") || strings.HasPrefix(method, "DOM.") ||
		strings.HasPrefix(method, "DOMSnapshot.") || strings.HasPrefix(method, "Accessibility.") ||
		method == "Page.getLayoutMetrics" || method == "Page.getFrameTree" ||
		method == "Page.getResourceTree" || method == "Page.captureScreenshot" ||
		method == "Input.insertText" || method == "Emulation.setDeviceMetricsOverride" ||
		method == "Emulation.clearDeviceMetricsOverride"
}

func (i *Instance) dialogError(id, method string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if p := i.pages[id]; p != nil && p.Dialog != nil && rendererCommand(method) {
		return ErrDialogOpen
	}
	return nil
}

func (i *Instance) agentAllowed(conv, id, method string) error {
	if i.manager.cfg.ProfileOf != nil && i.manager.profileFor(conv) != i.profile.ID {
		return ErrProfileUnavailable
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	p := i.pages[id]
	if p == nil || p.Conversation != conv || p.provisional || !Shown(p.URL) {
		return ErrNotOwner
	}
	if p.Control == "person" {
		return ErrPersonControl
	}
	if p.Dialog != nil && rendererCommand(method) {
		return ErrDialogOpen
	}
	return nil
}

// AgentCall is the tab-scoped entry point for tools. It enforces ownership and control;
// renderer commands additionally fail with ErrDialogOpen while a dialog awaits an answer.
// Page.handleJavaScriptDialog is allowed through this entry point. Ownership and
// control are checked again after attachment, immediately before dispatch. Commands
// already dispatched are not cancelled when the person changes control.
func (i *Instance) AgentCall(ctx context.Context, conv, id, method string, params any) (json.RawMessage, error) {
	if err := i.beginOperation(); err != nil {
		return nil, err
	}
	defer i.endOperation()
	if err := i.agentAllowed(conv, id, method); err != nil {
		return nil, err
	}
	raw, err := i.tabCall(ctx, id, method, params, func() error { return i.agentAllowed(conv, id, method) })
	if err == nil {
		i.mu.Lock()
		if p := i.pages[id]; p != nil {
			p.AgentAt = time.Now().UnixMilli()
			i.changedLocked()
		}
		i.mu.Unlock()
	}
	return raw, err
}

// AgentNewTab creates and selects an agent-owned working tab in this conversation.
func (i *Instance) AgentNewTab(ctx context.Context, conv, url string) (Tab, error) {
	return i.NewTab(ctx, conv, "agent", url)
}

// AgentCloseTab closes only an owned, agent-controlled tab, including a tab with a dialog.
func (i *Instance) AgentCloseTab(ctx context.Context, conv, id string) error {
	if err := i.agentAllowed(conv, id, "Target.closeTarget"); err != nil {
		return err
	}
	return i.CloseTab(ctx, id)
}

// SetControl persists who may operate a tab; people may always use Call to take over.
func (i *Instance) SetControl(id, holder string) error {
	return i.setControl(id, holder, i.inspectPersonPage)
}

func (i *Instance) setControl(id, holder string, inspect func(context.Context, string) (string, bool, error)) error {
	if holder != "agent" && holder != "person" {
		return errors.New("invalid browser control holder")
	}
	if err := i.beginOperation(); err != nil {
		return err
	}
	defer i.endOperation()
	i.mu.Lock()
	p := i.pages[id]
	if p == nil || p.provisional || !Shown(p.URL) {
		i.mu.Unlock()
		return errors.New("unknown browser tab")
	}
	conv := p.Conversation
	i.mu.Unlock()
	if holder == "agent" {
		unlock := i.lockControlConversation(conv)
		defer unlock()
	}
	i.mu.Lock()
	present := i.pages[id] == p && p.Conversation == conv && !p.provisional && Shown(p.URL)
	i.mu.Unlock()
	if !present {
		return errors.New("browser tab is no longer available")
	}
	p.controlMu.Lock()
	defer p.controlMu.Unlock()
	i.mu.Lock()
	if i.pages[id] != p || p.Conversation != conv {
		i.mu.Unlock()
		return errors.New("browser tab is no longer available")
	}
	returning := holder == "agent" && p.Control == "person"
	i.mu.Unlock()
	if returning {
		ctx, cancel := context.WithTimeout(i.ctx, cdp.DefaultTimeout)
		defer cancel()
		marker, touched, err := inspect(ctx, id)
		i.mu.Lock()
		if err == nil {
			p.PersonInput = false
			i.changedLocked()
		}
		if touched || err != nil {
			p.SealGeneration++
			p.Sealed, p.SecretTouched, p.SecretDocument = true, true, marker
			p.SecretUntracked = true
			i.changedLocked()
		}
		i.mu.Unlock()
		if touched || err != nil {
			i.save()
			i.mu.Lock()
			saveErr := i.saveError
			i.mu.Unlock()
			if saveErr != nil {
				return errors.New("cannot save browser handoff")
			}
		}
		if errors.Is(err, ErrHandoffHistory) {
			return ErrHandoffHistory
		}
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.pages[id] != p || p.Conversation != conv {
		return errors.New("browser tab is no longer available")
	}
	p.Control = holder
	i.changedLocked()
	if holder == "agent" && p.login != nil {
		p.login.settle("done", i.redactLocked(p.URL))
		p.login = nil
	}
	return nil
}

// HandleDialog answers the current JavaScript dialog without waiting for the renderer.
func (i *Instance) HandleDialog(ctx context.Context, id string, accept bool, text string) error {
	_, err := i.Call(ctx, id, "Page.handleJavaScriptDialog", map[string]any{"accept": accept, "promptText": text})
	return err
}
