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

type axValue struct {
	Value any `json:"value"`
}
type axNode struct {
	ID       string   `json:"nodeId"`
	Children []string `json:"childIds"`
	Ignored  bool     `json:"ignored"`
	Backend  int      `json:"backendDOMNodeId"`
	Role     axValue  `json:"role"`
	Name     axValue  `json:"name"`
	Value    axValue  `json:"value"`
}
type frameTree struct {
	Frame struct {
		ID string `json:"id"`
	} `json:"frame"`
	Children []frameTree `json:"childFrames"`
}
type frameScope struct{ session, frame string }

func (t Tools) scopes(ctx context.Context, i *agentbrowser.Instance, id string) ([]frameScope, error) {
	frames, err := i.AgentFrames(ctx, t.Caller, id)
	if err != nil {
		return nil, err
	}
	// Auto-attachment is recursive only when each child session enables it too.
	for round := 0; round < 8; round++ {
		for _, f := range frames {
			for _, domain := range []string{"Page.enable", "Runtime.enable", "Network.enable", "DOM.enable"} {
				if _, err = i.AgentFrameCall(ctx, t.Caller, id, f.Session, domain, nil); err != nil {
					return nil, err
				}
			}
			if _, err = i.AgentFrameCall(ctx, t.Caller, id, f.Session, "Target.setAutoAttach", map[string]any{
				"autoAttach": true, "waitForDebuggerOnStart": false, "flatten": true,
			}); err != nil {
				return nil, err
			}
		}
		next, e := i.AgentFrames(ctx, t.Caller, id)
		if e != nil {
			return nil, e
		}
		if len(next) == len(frames) {
			break
		}
		frames = next
	}
	sessions := []string{""}
	for _, f := range frames {
		sessions = append(sessions, f.Session)
	}
	separate := map[string]bool{}
	for _, f := range frames {
		separate[f.Frame] = true
	}
	var out []frameScope
	for _, session := range sessions {
		raw, e := i.AgentFrameCall(ctx, t.Caller, id, session, "Page.getFrameTree", nil)
		if e != nil {
			return nil, e
		}
		var r struct {
			Tree frameTree `json:"frameTree"`
		}
		if e = json.Unmarshal(raw, &r); e != nil {
			return nil, e
		}
		var walk func(frameTree, bool)
		walk = func(f frameTree, root bool) {
			if !root && separate[f.Frame.ID] {
				return
			}
			out = append(out, frameScope{session, f.Frame.ID})
			for _, child := range f.Children {
				walk(child, false)
			}
		}
		walk(r.Tree, true)
	}
	return out, nil
}
func (t Tools) snapshot(ctx context.Context, i *agentbrowser.Instance, st *tabState, id string) (string, error) {
	scopes, err := t.scopes(ctx, i, id)
	if err != nil {
		return "", err
	}
	refs := map[string]nodeRef{}
	st.mu.Lock()
	epoch := st.epoch
	st.mu.Unlock()
	var out strings.Builder
	for _, scope := range scopes {
		raw, e := i.AgentFrameCall(ctx, t.Caller, id, scope.session, "Accessibility.getFullAXTree", map[string]any{"frameId": scope.frame})
		if e != nil {
			return "", e
		}
		var r struct {
			Nodes []axNode `json:"nodes"`
		}
		if e = json.Unmarshal(raw, &r); e != nil {
			return "", e
		}
		nodes := map[string]axNode{}
		for _, n := range r.Nodes {
			nodes[n.ID] = n
		}
		fmt.Fprintf(&out, "Frame %s\n", scope.frame)
		var walk func(string, int) error
		walk = func(nodeID string, depth int) error {
			n, ok := nodes[nodeID]
			if !ok || depth > 100 || out.Len() > resultLimit {
				return nil
			}
			if !n.Ignored {
				ref := ""
				password := false
				if n.Backend != 0 {
					// Inspect the DOM type instead of trusting an accessibility role or masked AX value.
					data, e := i.AgentFrameCall(ctx, t.Caller, id, scope.session, "DOM.describeNode", map[string]any{"backendNodeId": n.Backend})
					if e != nil {
						return e
					}
					var d struct {
						Node struct {
							Attributes []string `json:"attributes"`
						} `json:"node"`
					}
					if e = json.Unmarshal(data, &d); e != nil {
						return e
					}
					for k := 0; k+1 < len(d.Node.Attributes); k += 2 {
						if d.Node.Attributes[k] == "type" && strings.EqualFold(d.Node.Attributes[k+1], "password") {
							password = true
						}
					}
					st.next++
					ref = fmt.Sprintf("e%d", st.next)
					refs[ref] = nodeRef{n.Backend, scope.session, scope.frame, epoch}
				}
				value := ""
				if n.Value.Value != nil && !password {
					value = " value=" + jsonText(n.Value.Value)
				}
				fmt.Fprintf(&out, "%s[%s] %v %s%s\n", strings.Repeat("  ", depth), ref, n.Role.Value, jsonText(n.Name.Value), value)
				depth++
			}
			for _, child := range n.Children {
				if e := walk(child, depth); e != nil {
					return e
				}
			}
			return nil
		}
		if len(r.Nodes) > 0 {
			if err = walk(r.Nodes[0].ID, 0); err != nil {
				return "", err
			}
		}
	}
	st.mu.Lock()
	st.refs = refs
	st.mu.Unlock()
	_ = t.collectPasswords(ctx, i, st, id)
	return clip(st.redact(out.String()), resultLimit), nil
}
func (t Tools) evaluate(ctx context.Context, i *agentbrowser.Instance, id, session, expression string) (string, error) {
	raw, err := t.dispatchFrame(ctx, i, id, session, "Runtime.evaluate", map[string]any{
		"expression": expression, "awaitPromise": true, "returnByValue": true, "userGesture": true,
	})
	if err != nil {
		return "", err
	}
	if raw == nil && errors.Is(i.AgentCheck(t.Caller, id, "Runtime.evaluate"), agentbrowser.ErrDialogOpen) {
		return t.errorText(i, id, agentbrowser.ErrDialogOpen), nil
	}
	var r struct {
		Result struct {
			Value       json.RawMessage `json:"value"`
			Description string          `json:"description"`
		} `json:"result"`
		Exception json.RawMessage `json:"exceptionDetails"`
	}
	if err = json.Unmarshal(raw, &r); err != nil {
		return "", err
	}
	if r.Exception != nil {
		return "", errors.New("JavaScript evaluation failed")
	}
	if len(r.Result.Value) == 0 {
		return r.Result.Description, nil
	}
	return string(r.Result.Value), nil
}
func (t Tools) collectPasswords(ctx context.Context, i *agentbrowser.Instance, st *tabState, id string) error {
	scopes, err := t.scopes(ctx, i, id)
	if err != nil {
		return err
	}
	for _, scope := range scopes {
		world, e := i.AgentFrameCall(ctx, t.Caller, id, scope.session, "Page.createIsolatedWorld", map[string]any{
			"frameId": scope.frame, "worldName": "browser-tools",
		})
		if e != nil {
			return e
		}
		var w struct {
			ID int `json:"executionContextId"`
		}
		if e = json.Unmarshal(world, &w); e != nil {
			return e
		}
		data, e := i.AgentFrameCall(ctx, t.Caller, id, scope.session, "Runtime.evaluate", map[string]any{
			"contextId": w.ID, "returnByValue": true,
			"expression": `(() => {
 const out = [];
 function scan(root) {
  for (const e of root.querySelectorAll('*')) {
   if (e.tagName === 'INPUT' && e.type === 'password') out.push(e.value);
   if (e.shadowRoot) scan(e.shadowRoot);
  }
 }
 scan(document);
 return out;
})()`,
		})
		var result struct {
			Result struct {
				Value json.RawMessage `json:"value"`
			} `json:"result"`
		}
		if e == nil {
			e = json.Unmarshal(data, &result)
		}
		raw := string(result.Result.Value)
		if e != nil {
			return e
		}
		var values []string
		if json.Unmarshal([]byte(raw), &values) == nil {
			for _, v := range values {
				st.remember(v)
			}
		}
	}
	return nil
}
func (t Tools) waitFor(ctx context.Context, i *agentbrowser.Instance, st *tabState, a arguments) (string, error) {
	if a.Text == "" {
		return "", errors.New("text is required")
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		s, err := t.snapshot(ctx, i, st, a.Tab)
		if err != nil {
			return "", err
		}
		if strings.Contains(s, a.Text) {
			return s, nil
		}
		select {
		case <-ctx.Done():
			return "", errors.New("Timed out waiting for text")
		case <-ticker.C:
		}
	}
}
