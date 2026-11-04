package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
	"github.com/ly-solutions/harnessly/internal/secrets"
)

type sealedField struct {
	ref      nodeRef
	object   string
	document string
}

var errSealed = errors.New("Tab is sealed: a secret was entered; submit the form or navigate to another page")
var errSecretUnavailable = errors.New("Secret is unavailable to this chat; use browser_request_secret or harnessly secret ask NAME --origin SITE")

func (t Tools) requestSecret(ctx context.Context, args *jsvalue.Object) *jsvalue.Object {
	for _, key := range args.Keys() {
		value, _ := args.Get(key)
		if key != "name" && key != "origin" && key != "why" {
			return mcpserver.TextResult("Unexpected secret request argument", true)
		}
		if _, ok := value.(string); !ok {
			return mcpserver.TextResult("Secret request arguments must be strings", true)
		}
	}
	if strings.TrimSpace(args.String("why")) == "" {
		return mcpserver.TextResult("A reason is required", true)
	}
	if t.S.RequestSecret == nil {
		return mcpserver.TextResult("Safe requests are unavailable", true)
	}
	status, err := t.S.RequestSecret(ctx, t.Caller, args.String("name"), args.String("origin"), args.String("why"))
	if err != nil {
		return mcpserver.TextResult(err.Error(), true)
	}
	return mcpserver.TextResult(jsonText(map[string]string{"status": status, "name": args.String("name")}), false)
}

func (t Tools) frameTree(ctx context.Context, i *agentbrowser.Instance, tab, session string) (frameTree, error) {
	raw, err := i.AgentFrameCall(ctx, t.Caller, tab, session, "Page.getFrameTree", nil)
	var result struct {
		Tree frameTree `json:"frameTree"`
	}
	if err == nil {
		err = json.Unmarshal(raw, &result)
	}
	return result.Tree, err
}

func findFrame(tree frameTree, id string) (frameTree, bool) {
	if tree.Frame.ID == id {
		return tree, true
	}
	for _, child := range tree.Children {
		if f, ok := findFrame(child, id); ok {
			return f, true
		}
	}
	return frameTree{}, false
}

// isolatedNode resolves a backend node in a private world. Page prototypes and getters
// cannot replace the DOM checks performed here or by the sealed-tab guard.
func (t Tools) isolatedNode(ctx context.Context, i *agentbrowser.Instance, tab string, ref nodeRef) (string, error) {
	raw, err := i.AgentFrameCall(ctx, t.Caller, tab, ref.session, "Page.createIsolatedWorld", map[string]any{
		"frameId": ref.frame, "worldName": "browser-safe",
	})
	var world struct {
		ID int `json:"executionContextId"`
	}
	if err == nil {
		err = json.Unmarshal(raw, &world)
	}
	if err != nil || world.ID == 0 {
		return "", errStale
	}
	raw, err = i.AgentFrameCall(ctx, t.Caller, tab, ref.session, "DOM.resolveNode", map[string]any{
		"backendNodeId": ref.backend, "executionContextId": world.ID,
	})
	var result struct {
		Object struct {
			ID string `json:"objectId"`
		} `json:"object"`
	}
	if err == nil {
		err = json.Unmarshal(raw, &result)
	}
	if err != nil || result.Object.ID == "" {
		return "", errStale
	}
	return result.Object.ID, nil
}

func (t Tools) nodeCheck(ctx context.Context, i *agentbrowser.Instance, tab string, ref nodeRef,
	obj, function string, out any) error {
	raw, err := i.AgentFrameCall(ctx, t.Caller, tab, ref.session, "Runtime.callFunctionOn", map[string]any{
		"objectId": obj, "functionDeclaration": function, "returnByValue": true,
	})
	if err != nil {
		return err
	}
	var result struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		Exception json.RawMessage `json:"exceptionDetails"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Exception != nil {
		return errStale
	}
	if json.Unmarshal(result.Result.Value, out) != nil {
		return errStale
	}
	return nil
}

func (t Tools) releaseNode(ctx context.Context, i *agentbrowser.Instance, tab string, ref nodeRef, obj string) {
	_, _ = i.AgentFrameCall(ctx, t.Caller, tab, ref.session, "Runtime.releaseObject", map[string]any{"objectId": obj})
}

func (t Tools) checkSeal(ctx context.Context, i *agentbrowser.Instance, st *tabState, id, tool string) error {
	var sealed agentbrowser.Tab
	for _, tab := range i.Tabs(t.Caller) {
		if tab.ID == id {
			sealed = tab
			break
		}
	}
	if !sealed.Sealed {
		return nil
	}
	blocked := tool == "browser_evaluate" || tool == "browser_console" || tool == "browser_network" || tool == "browser_screenshot"
	tree, err := t.frameTree(ctx, i, id, "")
	if err == nil && tree.Frame.Loader != "" && tree.Frame.Loader != sealed.SecretDocument {
		st.sealed = nil
		return i.Unseal(t.Caller, id)
	}
	if err == nil && len(st.sealed) > 0 {
		keep := []sealedField{}
		for _, field := range st.sealed {
			ref, obj := field.ref, field.object
			var present bool
			e := t.nodeCheck(ctx, i, id, ref, obj, "function(){return this.isConnected && this.value !== ''}", &present)
			if e != nil && t.fieldDocumentGone(ctx, i, id, field) {
				e, present = nil, false
			}
			if e != nil || present {
				keep = append(keep, field)
			} else {
				t.releaseNode(ctx, i, id, ref, obj)
			}
		}
		st.sealed = keep
		if len(keep) == 0 {
			return i.Unseal(t.Caller, id)
		}
	}
	if blocked || len(st.sealed) == 0 && (tool == "browser_snapshot" || tool == "browser_wait_for") {
		return errSealed
	}
	return nil
}

func (t Tools) secretScopes(ctx context.Context, i *agentbrowser.Instance, tab string) ([]frameScope, error) {
	frames, err := i.AgentFrames(ctx, t.Caller, tab)
	if err != nil {
		return nil, err
	}
	return t.frameScopes(ctx, i, tab, frames)
}

// A detached or navigated child frame destroys its execution context. Verify the
// browser frame tree before treating that failed object call as removal.
func (t Tools) fieldDocumentGone(ctx context.Context, i *agentbrowser.Instance, tab string, field sealedField) bool {
	scopes, err := t.secretScopes(ctx, i, tab)
	if err != nil {
		return false
	}
	for _, scope := range scopes {
		if scope.frame != field.ref.frame {
			continue
		}
		tree, err := t.frameTree(ctx, i, tab, scope.session)
		if err != nil {
			return false
		}
		frame, ok := findFrame(tree, scope.frame)
		return ok && frame.Frame.Loader != "" && frame.Frame.Loader != field.document
	}
	return true
}

// visibleFrame checks embedding elements too: a visible input inside a hidden iframe is not visible to the person.
func (t Tools) visibleFrame(ctx context.Context, i *agentbrowser.Instance, tab string, ref nodeRef, focused bool) error {
	scopes, err := t.secretScopes(ctx, i, tab)
	if err != nil {
		return err
	}
	byID := map[string]frameScope{}
	for _, scope := range scopes {
		byID[scope.frame] = scope
	}
	for frame := ref.frame; ; {
		scope, ok := byID[frame]
		if !ok {
			return errStale
		}
		if scope.parent == "" {
			return nil
		}
		parent, ok := byID[scope.parent]
		if !ok {
			return errStale
		}
		raw, err := i.AgentFrameCall(ctx, t.Caller, tab, parent.session, "DOM.getFrameOwner", map[string]any{"frameId": frame})
		var owner struct {
			Backend int `json:"backendNodeId"`
		}
		if err == nil {
			err = json.Unmarshal(raw, &owner)
		}
		if err != nil {
			return err
		}
		ownerRef := nodeRef{backend: owner.Backend, session: parent.session, frame: parent.frame}
		obj, err := t.isolatedNode(ctx, i, tab, ownerRef)
		if err != nil {
			return err
		}
		var visible bool
		check := `function(){
   const r=this.getBoundingClientRect();
   return r.width>0 && r.height>0 && this.checkVisibility({checkOpacity:true,checkVisibilityCSS:true});
  }`
		if focused {
			check = focusCheck
		}
		err = t.nodeCheck(ctx, i, tab, ownerRef, obj, check, &visible)
		t.releaseNode(ctx, i, tab, ownerRef, obj)
		if err != nil {
			return err
		}
		if !visible {
			if focused {
				return errors.New("Page moved focus away from the field frame; no secret was entered")
			}
			return errors.New("Field must be in a visible frame")
		}
		frame = parent.frame
	}
}

const inputCheck = `function(){
 if (!(this instanceof HTMLInputElement) || !['password','text','email','tel','number'].includes(this.type)
  || this.disabled || this.readOnly || !this.isConnected) return {ok:false};
 const r = this.getBoundingClientRect();
 if (r.width <= 0 || r.height <= 0 || !this.checkVisibility({checkOpacity:true,checkVisibilityCSS:true})) return {ok:false};
 return {ok:true,name:this.getAttribute('aria-label') || this.name || this.id || this.type};
}`
const focusCheck = `function(){
 if (!this.isConnected || !this.checkVisibility({checkOpacity:true,checkVisibilityCSS:true})) return false;
 const r=this.getBoundingClientRect();
 if (r.width<=0 || r.height<=0) return false;
 if (this instanceof HTMLInputElement && (this.disabled || this.readOnly ||
  !['password','text','email','tel','number'].includes(this.type))) return false;
 let element=this;
 for (;;) {
  const root=element.getRootNode();
  if (root.activeElement !== element) return false;
  if (!(root instanceof ShadowRoot)) return true;
  element=root.host;
 }
}`

func (t Tools) fillSecret(ctx context.Context, i *agentbrowser.Instance, st *tabState, a arguments) (string, error) {
	if t.S.Secrets == nil || t.S.Identity == nil {
		return "", errSecretUnavailable
	}
	use, err := t.S.Identity(t.Caller)
	if err != nil {
		return "", errSecretUnavailable
	}
	view, err := t.S.Secrets.Lookup(a.Secret, use.Project)
	if err != nil {
		return "", errSecretUnavailable
	}
	if len(view.Origins) == 0 {
		return "", errors.New("Secret has no allowed sites; use harnessly secret ask NAME --origin SITE or browser_request_secret")
	}
	st.mu.Lock()
	ref, ok := st.refs[a.ID]
	epoch := st.epochs[ref.frame]
	st.mu.Unlock()
	if !ok || ref.epoch != epoch {
		return "", errStale
	}
	tree, err := t.frameTree(ctx, i, a.Tab, ref.session)
	if err != nil {
		return "", err
	}
	frame, ok := findFrame(tree, ref.frame)
	origin := secrets.Origin(frame.Frame.Origin)
	if !ok || origin == "" || !slices.Contains(view.Origins, origin) {
		return "", errors.New("Field origin is not allowed for this secret")
	}
	main, err := t.frameTree(ctx, i, a.Tab, "")
	if err != nil || main.Frame.Loader == "" {
		return "", errStale
	}
	obj, err := t.isolatedNode(ctx, i, a.Tab, ref)
	if err != nil {
		return "", err
	}
	retained := false
	defer func() {
		if !retained {
			t.releaseNode(ctx, i, a.Tab, ref, obj)
		}
	}()
	var field struct {
		OK   bool   `json:"ok"`
		Name string `json:"name"`
	}
	if err = t.nodeCheck(ctx, i, a.Tab, ref, obj, inputCheck, &field); err != nil {
		return "", err
	}
	if !field.OK {
		return "", errors.New("Field must be a visible, enabled, writable input")
	}
	if err = t.visibleFrame(ctx, i, a.Tab, ref, false); err != nil {
		return "", err
	}
	if _, err = i.AgentFrameCall(ctx, t.Caller, a.Tab, ref.session, "DOM.focus", map[string]any{"backendNodeId": ref.backend}); err != nil {
		return "", err
	}
	var selected bool
	if err = t.nodeCheck(ctx, i, a.Tab, ref, obj, "function(){this.select(); return true}", &selected); err != nil {
		return "", err
	}
	var focused bool
	if err = t.nodeCheck(ctx, i, a.Tab, ref, obj, focusCheck, &focused); err != nil {
		return "", err
	}
	if !focused {
		return "", errors.New("Page moved focus away from the field; no secret was entered")
	}
	if err = t.nodeCheck(ctx, i, a.Tab, ref, obj, inputCheck, &field); err != nil {
		return "", err
	}
	if !field.OK {
		return "", errors.New("Field became hidden, disabled or readonly after focus; no secret was entered")
	}
	if err = t.visibleFrame(ctx, i, a.Tab, ref, true); err != nil {
		return "", err
	}
	// Recheck origin/document and project after focus handlers have run.
	latest, err := t.frameTree(ctx, i, a.Tab, ref.session)
	if err != nil {
		return "", err
	}
	current, ok := findFrame(latest, ref.frame)
	if !ok || current.Frame.Loader != frame.Frame.Loader || secrets.Origin(current.Frame.Origin) != origin {
		return "", errStale
	}
	use, err = t.S.Identity(t.Caller)
	if err != nil {
		return "", errSecretUnavailable
	}
	use.Origin = origin
	values, err := t.S.Secrets.Reveal([]string{a.Secret}, use)
	if err != nil {
		return "", errSecretUnavailable
	}
	value, err := secrets.BrowserValue(values[a.Secret], time.Now())
	if err != nil {
		return "", err
	}
	st.remember(values[a.Secret])
	st.remember(value)
	if err = i.Seal(t.Caller, a.Tab, main.Frame.Loader); err != nil {
		return "", err
	}
	st.sealed = append(st.sealed, sealedField{ref: ref, object: obj, document: frame.Frame.Loader})
	retained = true
	// This final check reduces focus races, but asynchronous page tasks can still run
	// between CDP commands; the browser is not an atomic secret-input broker.
	if err = t.nodeCheck(ctx, i, a.Tab, ref, obj, focusCheck, &focused); err != nil {
		return "", err
	}
	if !focused {
		return "", errors.New("Page moved focus away from the field; no secret was entered")
	}
	if err = t.visibleFrame(ctx, i, a.Tab, ref, true); err != nil {
		return "", err
	}
	if _, err = i.AgentCall(ctx, t.Caller, a.Tab, "Input.insertText", map[string]any{"text": value}); err != nil {
		return "", errors.New("Secret input failed; the tab remains sealed")
	}
	return fmt.Sprintf("Entered on %s into %s", origin, field.Name), nil
}
