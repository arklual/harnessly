package agentbrowser

import (
	"context"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/cdp"
)

const unknownHandoffDocument = "handoff:unknown"

// ErrHandoffHistory keeps person control when old personal documents might still
// be reachable through the browser's back and forward history.
var ErrHandoffHistory = errors.New("cannot return browser control: navigation history could not be cleared")

type handoffCall func(context.Context, string, string, any) (json.RawMessage, error)

type handoffTree struct {
	Frame struct {
		ID     string `json:"id"`
		Loader string `json:"loaderId"`
	} `json:"frame"`
	Children []handoffTree `json:"childFrames"`
}

func (i *Instance) inspectPersonPage(ctx context.Context, id string) (string, bool, error) {
	call := func(ctx context.Context, session, method string, params any) (json.RawMessage, error) {
		if session == "" {
			return i.Call(ctx, id, method, params)
		}
		i.mu.Lock()
		f, ok := i.agentFrames[session]
		c := i.conn
		i.mu.Unlock()
		if !ok || c == nil || f.tab != id || f.conn != c {
			return nil, cdp.ErrClosed
		}
		if err := i.dialogError(id, method); err != nil {
			return nil, err
		}
		return c.Call(ctx, method, params, session)
	}
	frames := func() []AgentFrame {
		i.mu.Lock()
		defer i.mu.Unlock()
		var out []AgentFrame
		for _, f := range i.agentFrames {
			if f.tab == id && f.conn == i.conn {
				out = append(out, f.AgentFrame)
			}
		}
		return out
	}
	i.mu.Lock()
	login := i.pages[id] != nil && i.pages[id].login != nil
	i.mu.Unlock()
	return inspectHandoff(ctx, call, frames, i.RememberSecret, login)
}

// inspectHandoff uses human CDP access while agent access is still blocked. A
// missing frame, detached node or unsupported command seals the document too.
func inspectHandoff(ctx context.Context, call handoffCall, frames func() []AgentFrame, remember func(string), login bool) (string, bool, error) {
	marker, touched := unknownHandoffDocument, false
	var failure error
	record := func(err error) {
		if err != nil {
			// Chrome errors can include page-controlled text. Keep only a fixed error.
			failure = errors.New("browser handoff inspection was incomplete")
		}
	}
	_, err := call(ctx, "", "Page.resetNavigationHistory", nil)
	historyFailed := err != nil
	record(err)
	readTree := func(session string) (handoffTree, error) {
		raw, err := call(ctx, session, "Page.getFrameTree", nil)
		if err != nil {
			return handoffTree{}, err
		}
		var r struct {
			Tree handoffTree `json:"frameTree"`
		}
		if err = json.Unmarshal(raw, &r); err != nil {
			return handoffTree{}, err
		}
		if r.Tree.Frame.ID == "" || r.Tree.Frame.Loader == "" {
			return handoffTree{}, errors.New("missing browser document marker")
		}
		return r.Tree, nil
	}
	main, err := readTree("")
	record(err)
	if err == nil {
		marker = main.Frame.Loader
	}
	autoAttach := func(session string) {
		_, err := call(ctx, session, "Target.setAutoAttach", map[string]any{
			"autoAttach": true, "waitForDebuggerOnStart": false, "flatten": true,
		})
		record(err)
	}
	autoAttach("")
	attached := map[string]bool{}
	for round := 0; ; round++ {
		pending := false
		for _, f := range frames() {
			if attached[f.Session] {
				continue
			}
			pending = true
			attached[f.Session] = true
			_, err := call(ctx, f.Session, "Page.enable", nil)
			record(err)
			autoAttach(f.Session)
		}
		if !pending {
			break
		}
		if round >= 15 {
			record(errors.New("too many nested browser sessions"))
			break
		}
	}
	sessions := []string{""}
	separate := map[string]bool{}
	for _, f := range frames() {
		sessions = append(sessions, f.Session)
		separate[f.Frame] = true
	}
	for _, session := range sessions {
		tree, err := readTree(session)
		record(err)
		if err != nil {
			continue
		}
		var walk func(handoffTree, bool)
		walk = func(tree handoffTree, root bool) {
			if !root && separate[tree.Frame.ID] {
				return
			}
			err := scanHandoffFrame(ctx, call, session, tree.Frame.ID, login, func(value string) {
				touched = true
				if utf8.RuneCountInString(value) >= 4 {
					remember(value)
				}
			})
			record(err)
			for _, child := range tree.Children {
				walk(child, false)
			}
		}
		walk(tree, true)
		_, err = call(ctx, session, "Runtime.releaseObjectGroup", map[string]any{"objectGroup": "person-handoff"})
		record(err)
	}
	after, err := readTree("")
	record(err)
	if err != nil || after.Frame.Loader != marker {
		marker = unknownHandoffDocument
		record(errors.New("document changed during handoff"))
	}
	if historyFailed {
		return marker, touched, errors.Join(ErrHandoffHistory, failure)
	}
	return marker, touched, failure
}

type handoffNode struct {
	Backend  int           `json:"backendNodeId"`
	Name     string        `json:"nodeName"`
	Children []handoffNode `json:"children"`
	Shadows  []handoffNode `json:"shadowRoots"`
}

// Browser DOM traversal includes closed shadow roots. Native getters in the
// isolated world avoid page-defined getters or forged querySelector methods.
func scanHandoffFrame(ctx context.Context, call handoffCall, session, frame string, login bool, remember func(string)) error {
	raw, err := call(ctx, session, "Page.createIsolatedWorld", map[string]any{
		"frameId": frame, "worldName": "person-handoff",
	})
	if err != nil {
		return err
	}
	var world struct {
		ID int `json:"executionContextId"`
	}
	if err = json.Unmarshal(raw, &world); err != nil || world.ID == 0 {
		return errors.New("isolated world unavailable")
	}
	raw, err = call(ctx, session, "Runtime.evaluate", map[string]any{
		"expression": "document", "contextId": world.ID, "objectGroup": "person-handoff",
	})
	if err != nil {
		return err
	}
	doc, err := handoffObject(raw)
	if err != nil {
		return err
	}
	raw, err = call(ctx, session, "DOM.describeNode", map[string]any{"objectId": doc, "depth": -1, "pierce": true})
	if err != nil {
		return err
	}
	var root struct {
		Node handoffNode `json:"node"`
	}
	if err = json.Unmarshal(raw, &root); err != nil || root.Node.Backend == 0 {
		return errors.New("browser DOM unavailable")
	}
	var walk func(handoffNode) error
	walk = func(n handoffNode) error {
		if n.Name == "INPUT" {
			raw, err := call(ctx, session, "DOM.resolveNode", map[string]any{
				"backendNodeId": n.Backend, "executionContextId": world.ID, "objectGroup": "person-handoff",
			})
			if err != nil {
				return err
			}
			// DOM.resolveNode names its RemoteObject differently from Runtime.evaluate.
			var node struct {
				Object struct {
					ID string `json:"objectId"`
				} `json:"object"`
			}
			if json.Unmarshal(raw, &node) != nil || node.Object.ID == "" {
				return errors.New("browser input unavailable")
			}
			raw, err = call(ctx, session, "Runtime.callFunctionOn", map[string]any{
				"objectId": node.Object.ID, "returnByValue": true,
				"arguments": []any{map[string]any{"value": login}},
				"functionDeclaration": `function(login) {
 const get = name => Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, name).get.call(this);
 const attr = name => Element.prototype.getAttribute.call(this, name) || '';
 const type = get('type');
 if (type !== 'password' && !['text','email','tel','url','search','number'].includes(type)) return {sealed:false};
 const value = get('value');
 if (!value) return {sealed:false};
 const secret = type === 'password' || getComputedStyle(this).getPropertyValue('-webkit-text-security') !== 'none' &&
   getComputedStyle(this).getPropertyValue('-webkit-text-security') !== '' ||
   /(?:^|\s)(?:current-password|new-password|one-time-code|cc-number|cc-csc)(?:\s|$)/i.test(get('autocomplete')) ||
   /pass|pwd|парол|secret|otp|token|\bpin\b|cvv|cvc/i.test([get('name'), attr('id'), attr('aria-label'), attr('placeholder')].join(' '));
 if (secret) return {sealed:true, value};
 return {sealed:login && ['text','email','tel','url','search','number'].includes(type)};
}`,
			})
			if err != nil {
				return err
			}
			var value struct {
				Result struct {
					Value *struct {
						Sealed bool   `json:"sealed"`
						Value  string `json:"value"`
					} `json:"value"`
				} `json:"result"`
				Exception json.RawMessage `json:"exceptionDetails"`
			}
			if json.Unmarshal(raw, &value) != nil || value.Exception != nil || value.Result.Value == nil {
				return errors.New("browser input inspection failed")
			}
			if value.Result.Value.Sealed {
				remember(value.Result.Value.Value)
			}
		}
		for _, list := range [][]handoffNode{n.Children, n.Shadows} {
			for _, child := range list {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(root.Node)
}

func handoffObject(raw json.RawMessage) (string, error) {
	var r struct {
		Result struct {
			ID string `json:"objectId"`
		} `json:"result"`
		Exception json.RawMessage `json:"exceptionDetails"`
	}
	if json.Unmarshal(raw, &r) != nil || r.Exception != nil || r.Result.ID == "" {
		return "", errors.New("browser document unavailable")
	}
	return r.Result.ID, nil
}
