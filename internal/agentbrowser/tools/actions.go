package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser"
)

var errStale = errors.New("Element ID is stale or missing; refresh the snapshot with browser_snapshot")

func (t Tools) resolve(ctx context.Context, i *agentbrowser.Instance, st *tabState, a arguments) (nodeRef, string, error) {
	st.mu.Lock()
	ref, ok := st.refs[a.ID]
	epoch := st.epochs[ref.frame]
	st.mu.Unlock()
	if !ok || ref.epoch != epoch {
		return ref, "", errStale
	}
	raw, err := i.AgentFrameCall(ctx, t.Caller, a.Tab, ref.session, "DOM.resolveNode", map[string]any{"backendNodeId": ref.backend})
	if err != nil {
		return ref, "", errStale
	}
	var r struct {
		Object struct {
			ID string `json:"objectId"`
		} `json:"object"`
	}
	if json.Unmarshal(raw, &r) != nil || r.Object.ID == "" {
		return ref, "", errStale
	}
	raw, err = i.AgentFrameCall(ctx, t.Caller, a.Tab, ref.session, "Runtime.callFunctionOn", map[string]any{
		"objectId": r.Object.ID, "functionDeclaration": "function(){return this.isConnected}", "returnByValue": true,
	})
	if err != nil {
		return ref, "", err
	}
	var connected struct {
		Result struct {
			Value bool `json:"value"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &connected) != nil || !connected.Result.Value {
		return ref, "", errStale
	}
	return ref, r.Object.ID, nil
}
func (t Tools) action(ctx context.Context, i *agentbrowser.Instance, st *tabState, name string, a arguments) error {
	if name == "browser_press_key" {
		return t.pressKey(ctx, i, a.Tab, a.Key)
	}
	ref, obj, err := t.resolve(ctx, i, st, a)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = i.AgentFrameCall(ctx, t.Caller, a.Tab, ref.session, "Runtime.releaseObject", map[string]any{"objectId": obj})
	}()
	call := func(method string, p any) (json.RawMessage, error) {
		return i.AgentFrameCall(ctx, t.Caller, a.Tab, ref.session, method, p)
	}
	switch name {
	case "browser_click", "browser_hover":
		x, y, e := t.point(ctx, i, a.Tab, ref)
		if e != nil {
			return e
		}
		if _, e = t.dispatch(ctx, i, a.Tab, "Input.dispatchMouseEvent", map[string]any{"type": "mouseMoved", "x": x, "y": y}); e != nil {
			return e
		}
		if name == "browser_hover" {
			return nil
		}
		for _, kind := range []string{"mousePressed", "mouseReleased"} {
			if _, e = t.dispatch(ctx, i, a.Tab, "Input.dispatchMouseEvent", map[string]any{
				"type": kind, "x": x, "y": y, "button": "left", "clickCount": 1,
			}); e != nil {
				return e
			}
		}
		return nil
	case "browser_type":
		raw, e := call("Runtime.callFunctionOn", map[string]any{
			"objectId": obj, "functionDeclaration": "function(){return this.type==='password'}", "returnByValue": true,
		})
		if e != nil {
			return e
		}
		var r struct {
			Result struct {
				Value bool `json:"value"`
			} `json:"result"`
		}
		if e = json.Unmarshal(raw, &r); e != nil {
			return e
		}
		if r.Result.Value {
			st.remember(a.Text)
		}
		if _, e = call("DOM.focus", map[string]any{"backendNodeId": ref.backend}); e != nil {
			return e
		}
		if _, e = call("Runtime.callFunctionOn", map[string]any{
			"objectId": obj, "functionDeclaration": "function(){if(this.select)this.select()}",
		}); e != nil {
			return e
		}
		_, e = i.AgentCall(ctx, t.Caller, a.Tab, "Input.insertText", map[string]any{"text": a.Text})
		return e
	case "browser_select":
		raw, e := call("Runtime.callFunctionOn", map[string]any{"objectId": obj, "functionDeclaration": `function(value) {
 if (this.tagName !== 'SELECT' || ![...this.options].some(o => o.value === value)) return false;
 this.value = value;
 this.dispatchEvent(new Event('input', {bubbles:true}));
 this.dispatchEvent(new Event('change', {bubbles:true}));
 return true;
}`,
			"arguments": []any{map[string]any{"value": a.Value}}, "returnByValue": true})
		if e != nil {
			return e
		}
		var r struct {
			Result struct {
				Value bool `json:"value"`
			} `json:"result"`
		}
		if json.Unmarshal(raw, &r) != nil || !r.Result.Value {
			return errors.New("Element is not a select or the option does not exist")
		}
		return nil
	case "browser_upload":
		if len(a.Files) == 0 {
			return errors.New("files must contain at least one path")
		}
		files := make([]string, 0, len(a.Files))
		for _, path := range a.Files {
			p, e := insideFile(t.Cwd, path)
			if e != nil {
				return e
			}
			files = append(files, p)
		}
		_, e := call("DOM.setFileInputFiles", map[string]any{"backendNodeId": ref.backend, "files": files})
		return e
	}
	return fmt.Errorf("Unknown browser tool %s", name)
}
func (t Tools) point(ctx context.Context, i *agentbrowser.Instance, id string, ref nodeRef) (float64, float64, error) {
	if _, err := i.AgentFrameCall(ctx, t.Caller, id, ref.session, "DOM.scrollIntoViewIfNeeded", map[string]any{
		"backendNodeId": ref.backend,
	}); err != nil {
		return 0, 0, err
	}
	quad, err := t.quad(ctx, i, id, ref.session, ref.backend)
	if err != nil {
		return 0, 0, err
	}
	x, y := (quad[0]+quad[2]+quad[4]+quad[6])/4, (quad[1]+quad[3]+quad[5]+quad[7])/4
	frames, err := i.AgentFrames(ctx, t.Caller, id)
	if err != nil {
		return 0, 0, err
	}
	bySession := map[string]agentbrowser.AgentFrame{}
	for _, f := range frames {
		bySession[f.Session] = f
	}
	for session := ref.session; session != ""; {
		f, ok := bySession[session]
		if !ok {
			return 0, 0, errStale
		}
		raw, e := i.AgentFrameCall(ctx, t.Caller, id, f.Parent, "DOM.getFrameOwner", map[string]any{"frameId": f.Frame})
		if e != nil {
			return 0, 0, e
		}
		var owner struct {
			Backend int `json:"backendNodeId"`
		}
		if e = json.Unmarshal(raw, &owner); e != nil {
			return 0, 0, e
		}
		q, e := t.quad(ctx, i, id, f.Parent, owner.Backend)
		if e != nil {
			return 0, 0, e
		}
		// Child quads use their local viewport. The owner's content quad excludes its border.
		// Bilinear mapping also preserves coordinates of scaled or rotated iframe elements.
		raw, e = i.AgentFrameCall(ctx, t.Caller, id, session, "Page.getLayoutMetrics", nil)
		if e != nil {
			return 0, 0, e
		}
		var metrics struct {
			Viewport struct {
				Width  float64 `json:"clientWidth"`
				Height float64 `json:"clientHeight"`
			} `json:"cssLayoutViewport"`
		}
		if e = json.Unmarshal(raw, &metrics); e != nil {
			return 0, 0, e
		}
		if metrics.Viewport.Width <= 0 || metrics.Viewport.Height <= 0 {
			return 0, 0, errors.New("Frame has no visible viewport")
		}
		u, v := x/metrics.Viewport.Width, y/metrics.Viewport.Height
		x = q[0]*(1-u)*(1-v) + q[2]*u*(1-v) + q[4]*u*v + q[6]*(1-u)*v
		y = q[1]*(1-u)*(1-v) + q[3]*u*(1-v) + q[5]*u*v + q[7]*(1-u)*v
		session = f.Parent
	}
	return x, y, nil
}
func (t Tools) quad(ctx context.Context, i *agentbrowser.Instance, id, session string, backend int) ([]float64, error) {
	raw, err := i.AgentFrameCall(ctx, t.Caller, id, session, "DOM.getContentQuads", map[string]any{"backendNodeId": backend})
	if err != nil {
		return nil, err
	}
	var r struct {
		Quads [][]float64 `json:"quads"`
	}
	if err = json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	if len(r.Quads) == 0 || len(r.Quads[0]) != 8 {
		return nil, errors.New("Element has no visible content quad; refresh the snapshot")
	}
	return r.Quads[0], nil
}
func (t Tools) pressKey(ctx context.Context, i *agentbrowser.Instance, id, key string) error {
	parts := strings.Split(key, "+")
	key = parts[len(parts)-1]
	mod := 0
	for _, p := range parts[:len(parts)-1] {
		switch p {
		case "Alt":
			mod |= 1
		case "Control", "Ctrl":
			mod |= 2
		case "Meta":
			mod |= 4
		case "Shift":
			mod |= 8
		default:
			return errors.New("Unknown key modifier")
		}
	}
	codes := map[string]int{
		"Enter": 13, "Tab": 9, "Escape": 27, "Backspace": 8, "Delete": 46,
		"ArrowLeft": 37, "ArrowUp": 38, "ArrowRight": 39, "ArrowDown": 40,
		"Home": 36, "End": 35, "PageUp": 33, "PageDown": 34,
	}
	code := codes[key]
	text := ""
	if code == 0 {
		if len([]rune(key)) != 1 {
			return errors.New("Unsupported key")
		}
		text = key
		code = int([]rune(strings.ToUpper(key))[0])
	}
	if key == "Enter" {
		text = "\r"
	}
	if mod&7 != 0 {
		text = ""
	}
	for _, kind := range []string{"keyDown", "keyUp"} {
		p := map[string]any{"type": kind, "key": key, "windowsVirtualKeyCode": code, "modifiers": mod}
		if kind == "keyDown" {
			p["text"] = text
		}
		if _, err := t.dispatch(ctx, i, id, "Input.dispatchKeyEvent", p); err != nil {
			return err
		}
	}
	return nil
}

// Input replies may wait for the dialog they opened. Stop waiting once the core sees it;
// the event has already been delivered and browser_dialog is the next operation.
func (t Tools) dispatch(ctx context.Context, i *agentbrowser.Instance, id, method string, p any) (json.RawMessage, error) {
	return t.dispatchFrame(ctx, i, id, "", method, p)
}
func (t Tools) dispatchFrame(ctx context.Context, i *agentbrowser.Instance, id, session, method string, p any) (json.RawMessage, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	defer close(done)
	go func() {
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				if errors.Is(i.AgentCheck(t.Caller, id, "Runtime.evaluate"), agentbrowser.ErrDialogOpen) {
					cancel()
					return
				}
			}
		}
	}()
	raw, err := i.AgentFrameCall(ctx, t.Caller, id, session, method, p)
	if errors.Is(err, context.Canceled) && errors.Is(i.AgentCheck(t.Caller, id, "Runtime.evaluate"), agentbrowser.ErrDialogOpen) {
		return nil, nil
	}
	return raw, err
}
