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

// checkSeals releases verified seals before the requested action and enforces the read barrier across the chat.
// Chat actions are serialized, while node tracking and recordings use st.mu; CDP never runs under that mutex.
func (t Tools) checkSeals(ctx context.Context, i *agentbrowser.Instance, target, tool string) error {
	if err := i.InspectPersonInputs(ctx, t.Caller); err != nil {
		return err
	}
	if target == "" {
		target = i.Current(t.Caller)
	}
	blocked := tool == "browser_evaluate" || tool == "browser_console" || tool == "browser_network" || tool == "browser_screenshot"
	for _, tab := range i.Tabs(t.Caller) {
		if !tab.Sealed {
			continue
		}
		t.S.mu.Lock()
		st := t.S.tabs[i.Profile().ID+"/"+t.Caller+"/"+tab.ID]
		t.S.mu.Unlock()
		if t.releaseSeal(ctx, i, st, tab) {
			continue
		}
		unknown := true
		if st != nil {
			st.mu.Lock()
			unknown = tab.SecretUntracked || len(st.sealed) == 0
			st.mu.Unlock()
		}
		if blocked || tab.ID == target && unknown && (tool == "browser_snapshot" || tool == "browser_wait_for") {
			return fmt.Errorf("%w (%s): submit the form, navigate to another page, or close that tab", errSealed, tab.ID)
		}
	}
	return nil
}

func (t Tools) releaseSeal(ctx context.Context, i *agentbrowser.Instance, st *tabState, tab agentbrowser.Tab) bool {
	tree, err := t.frameTree(ctx, i, tab.ID, "")
	if err == nil && tree.Frame.Loader != "" && (tab.SecretDocument == "" || tab.SecretDocument == "handoff:unknown") {
		// A failed handoff has no document proof. Establish a baseline, never infer navigation from the sentinel.
		_ = i.Seal(t.Caller, tab.ID, tree.Frame.Loader)
		return false
	}
	changed := err == nil && tree.Frame.Loader != "" && tree.Frame.Loader != tab.SecretDocument
	var fields []sealedField
	if st != nil {
		st.mu.Lock()
		fields = append(fields, st.sealed...)
		st.mu.Unlock()
	}
	if !changed {
		if err != nil || tab.SecretUntracked || len(fields) == 0 {
			return false
		}
		for _, field := range fields {
			var connected bool
			err := t.nodeCheck(ctx, i, tab.ID, field.ref, field.object, "function(){return this.isConnected}", &connected)
			if err != nil && t.fieldDocumentGone(ctx, i, tab.ID, field) {
				err, connected = nil, false
			}
			if err != nil || connected {
				return false
			}
		}
	}
	// Returning via back/forward must never restore the document that held the input.
	if _, err = i.AgentCall(ctx, t.Caller, tab.ID, "Page.resetNavigationHistory", nil); err != nil {
		return false
	}
	if err = i.Unseal(t.Caller, tab.ID, tab.SealGeneration); err != nil {
		return false
	}
	if st != nil {
		st.mu.Lock()
		st.sealed, st.console, st.network, st.snapshot = nil, nil, nil, nil
		st.requests = map[string]string{}
		st.mu.Unlock()
	}
	for _, field := range fields {
		t.releaseNode(ctx, i, tab.ID, field.ref, field.object)
	}
	return true
}

func tabSealed(i *agentbrowser.Instance, conv, id string) bool {
	for _, tab := range i.Tabs(conv) {
		if tab.ID == id {
			return tab.Sealed
		}
	}
	return false
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
func (t Tools) visibleFrame(ctx context.Context, i *agentbrowser.Instance, tab string, ref nodeRef) error {
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
   return r.width>0 && r.height>0 && r.right>0 && r.bottom>0 && this.checkVisibility({checkOpacity:true,checkVisibilityCSS:true});
  }`
		err = t.nodeCheck(ctx, i, tab, ownerRef, obj, check, &visible)
		t.releaseNode(ctx, i, tab, ownerRef, obj)
		if err != nil {
			return err
		}
		if !visible {
			return errors.New("Field must be in a visible frame")
		}
		frame = parent.frame
	}
}

// atomicSecretInput validates and inserts in the field's own isolated world in one renderer task.
// Secret bytes occur only in the CDP argument list, never in this function's source or result.
const atomicSecretInput = `function(value, origin){
 if (self.origin !== origin) return 'origin';
 if (!(this instanceof HTMLInputElement) || !['password','text','email','tel','number'].includes(this.type)
  || this.disabled || this.readOnly || !this.isConnected) return 'field';
 const r=this.getBoundingClientRect();
 if (r.width<=0 || r.height<=0 || r.right<=0 || r.bottom<=0 ||
  !this.checkVisibility({checkOpacity:true,checkVisibilityCSS:true})) return 'field';
 this.setAttribute('autocomplete','off');
 this.focus();
 const focused=()=>{
  let element=this;
  for (;;) {
   const root=element.getRootNode();
   if (root.activeElement !== element) return false;
   if (!(root instanceof ShadowRoot)) return true;
   element=root.host;
  }
 };
 if (!focused()) return 'focus';
 this.select();
 if (!focused()) return 'focus';
 const accepted=document.execCommand('insertText',false,value);
 if (accepted && this.value===value) return 'entered';
 this.select();
 document.execCommand('delete');
 return this.value==='' ? 'rejected' : 'uncertain';
}`

func (t Tools) fillSecret(ctx context.Context, i *agentbrowser.Instance, st *tabState, a arguments) (string, error) {
	for _, tab := range i.Tabs(t.Caller) {
		if tab.ID == a.Tab && tab.Scripted {
			return "", errors.New("JavaScript was executed in this tab; open the site in a new tab with browser_open and fill the secret there")
		}
	}
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
	var fieldName string
	if err = t.nodeCheck(ctx, i, a.Tab, ref, obj, "function(){return this.getAttribute('aria-label') || this.name || this.id || this.type || 'input'}", &fieldName); err != nil {
		return "", err
	}
	if err = t.visibleFrame(ctx, i, a.Tab, ref); err != nil {
		return "", err
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
	// The persisted seal also survives a runtime crash during the renderer task.
	finishSeal, err := i.BeginSecretInput(t.Caller, a.Tab, main.Frame.Loader)
	if err != nil {
		return "", err
	}
	keep := true // A missing CDP reply cannot prove that the renderer did not insert.
	defer func() { finishSeal(keep) }()
	// The per-chat action lock excludes every sibling read during this reservation and renderer task.
	finishMask := i.ReserveSecrets(values[a.Secret], value)
	defer func() { finishMask(keep) }()
	raw, callErr := i.AgentFrameCall(ctx, t.Caller, a.Tab, ref.session, "Runtime.callFunctionOn", map[string]any{
		"objectId": obj, "functionDeclaration": atomicSecretInput, "returnByValue": true,
		"arguments": []map[string]any{{"value": value}, {"value": frame.Frame.Origin}},
	})
	var result struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
		Exception json.RawMessage `json:"exceptionDetails"`
	}
	if callErr == nil {
		callErr = json.Unmarshal(raw, &result)
	}
	if callErr == nil && result.Exception == nil {
		switch result.Result.Value {
		case "origin", "field", "focus", "rejected":
			keep = false
			return "", errors.New("Secret input refused: " + result.Result.Value + "; use a visible, writable field with focus that accepts the complete value")
		}
	}
	st.mu.Lock()
	st.sealed = append(st.sealed, sealedField{ref: ref, object: obj, document: frame.Frame.Loader})
	st.snapshot = nil
	st.mu.Unlock()
	retained = true
	st.remember(values[a.Secret])
	st.remember(value)
	if callErr != nil || result.Exception != nil || result.Result.Value != "entered" {
		return "", errors.New("Secret input could not be confirmed; the tab remains sealed")
	}
	return fmt.Sprintf("Entered on %s into %s", origin, fieldName), nil
}
