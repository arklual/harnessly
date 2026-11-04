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
	ID         string   `json:"nodeId"`
	Children   []string `json:"childIds"`
	Ignored    bool     `json:"ignored"`
	Backend    int      `json:"backendDOMNodeId"`
	Role       axValue  `json:"role"`
	Name       axValue  `json:"name"`
	Value      axValue  `json:"value"`
	Properties []struct {
		Name  string  `json:"name"`
		Value axValue `json:"value"`
	} `json:"properties"`
}
type frameTree struct {
	Frame struct {
		ID     string `json:"id"`
		Parent string `json:"parentId"`
	} `json:"frame"`
	Children []frameTree `json:"childFrames"`
}
type frameScope struct{ session, frame, parent string }

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
			out = append(out, frameScope{session, f.Frame.ID, f.Frame.Parent})
			for _, child := range f.Children {
				walk(child, false)
			}
		}
		walk(r.Tree, true)
	}
	return out, nil
}

type snapshotLine struct {
	text, id string
	ref      nodeRef
}

var interactiveRoles = map[string]bool{
	"button": true, "link": true, "textbox": true, "searchbox": true, "combobox": true,
	"checkbox": true, "radio": true, "switch": true, "slider": true, "spinbutton": true,
	"listbox": true, "option": true, "menuitem": true, "menuitemcheckbox": true,
	"menuitemradio": true, "tab": true, "treeitem": true, "scrollbar": true,
}

func showAX(n axNode, interactive bool) bool {
	if n.Ignored {
		return false
	}
	role, _ := n.Role.Value.(string)
	if interactive {
		if role == "heading" || interactiveRoles[role] {
			return true
		}
		for _, property := range n.Properties {
			if property.Name == "focusable" && property.Value.Value == true && role != "RootWebArea" {
				return true
			}
		}
		return false
	}
	switch role {
	case "InlineTextBox", "LineBreak":
		// StaticText already carries the complete text; inline boxes only duplicate it.
		return false
	case "generic", "none", "presentation":
		return n.Name.Value != nil && n.Name.Value != ""
	}
	return true
}

func (t Tools) snapshot(ctx context.Context, i *agentbrowser.Instance, st *tabState, a arguments) (string, error) {
	if a.Offset < 0 {
		return "", errors.New("offset must be nonnegative")
	}
	if a.Offset == 0 {
		if err := t.buildSnapshot(ctx, i, st, a); err != nil {
			return "", err
		}
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.snapshot) == 0 || st.interactive != a.InteractiveOnly || a.Offset >= len(st.snapshot) {
		return "", errors.New("Snapshot offset is unavailable; refresh with browser_snapshot offset 0")
	}
	var out strings.Builder
	end := a.Offset
	changed := map[string]bool{}
	for end < len(st.snapshot) {
		line := st.snapshot[end]
		if line.ref.frame != "" && line.ref.epoch != st.epochs[line.ref.frame] {
			if !changed[line.ref.frame] {
				notice := fmt.Sprintf("[Frame %s changed; its old rows are omitted. Refresh offset 0 to inspect it.]\n", line.ref.frame)
				if out.Len()+len(notice) > resultLimit-512 {
					break
				}
				out.WriteString(notice)
				changed[line.ref.frame] = true
			}
			end++
			continue
		}
		text := st.redactLocked(line.text)
		// Keep each ID and its line together, reserving space for the pagination instructions.
		if len(text) > resultLimit-1024 {
			text = strings.ToValidUTF8(text[:resultLimit-1100], "") + " [long node text truncated]"
		}
		if out.Len()+len(text)+1 > resultLimit-512 {
			break
		}
		out.WriteString(text + "\n")
		if line.id != "" {
			st.refs[line.id] = line.ref
		}
		end++
	}
	if end < len(st.snapshot) {
		fmt.Fprintf(&out, "\nnext_offset: %d\nContinue with browser_snapshot {\"tab\":%s,\"offset\":%d,\"interactive_only\":%t}. IDs from earlier parts remain valid.\n",
			end, jsonText(a.Tab), end, a.InteractiveOnly)
	} else {
		out.WriteString("\nnext_offset: null (end of snapshot)\n")
	}
	return out.String(), nil
}

func (t Tools) buildSnapshot(ctx context.Context, i *agentbrowser.Instance, st *tabState, a arguments) error {
	scopes, err := t.scopes(ctx, i, a.Tab)
	if err != nil {
		return err
	}
	st.mu.Lock()
	versions := make(map[string]uint64)
	if st.epochs == nil {
		st.epochs = map[string]uint64{}
	}
	if st.parents == nil {
		st.parents = map[string]string{}
	}
	for _, scope := range scopes {
		versions[scope.frame] = st.epochs[scope.frame]
		if scope.parent != "" {
			st.parents[scope.frame] = scope.parent
		}
	}
	st.mu.Unlock()
	var lines []snapshotLine
	for _, scope := range scopes {
		raw, e := i.AgentFrameCall(ctx, t.Caller, a.Tab, scope.session, "Accessibility.getFullAXTree", map[string]any{"frameId": scope.frame})
		if e != nil {
			return e
		}
		var r struct {
			Nodes []axNode `json:"nodes"`
		}
		if e = json.Unmarshal(raw, &r); e != nil {
			return e
		}
		nodes := map[string]axNode{}
		for _, n := range r.Nodes {
			nodes[n.ID] = n
		}
		lines = append(lines, snapshotLine{text: "Frame " + scope.frame, ref: nodeRef{frame: scope.frame, epoch: versions[scope.frame]}})
		var walk func(string, int) error
		walk = func(nodeID string, depth int) error {
			n, ok := nodes[nodeID]
			if !ok {
				return nil
			}
			if showAX(n, a.InteractiveOnly) {
				ref := ""
				password := false
				if n.Backend != 0 && n.Value.Value != nil {
					// Only valued nodes can leak a password; inspecting every text node costs thousands of CDP round trips.
					data, e := i.AgentFrameCall(ctx, t.Caller, a.Tab, scope.session, "DOM.describeNode", map[string]any{"backendNodeId": n.Backend})
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
				}
				if n.Backend != 0 {
					st.next++
					ref = fmt.Sprintf("e%d", st.next)
				}
				value := ""
				if n.Value.Value != nil && !password {
					value = " value=" + jsonText(n.Value.Value)
				}
				line := fmt.Sprintf("%s[%s] %v %s%s%s", strings.Repeat("  ", min(depth, 24)),
					ref, n.Role.Value, jsonText(n.Name.Value), value, linkTarget(n))
				lines = append(lines, snapshotLine{line, ref, nodeRef{n.Backend, scope.session, scope.frame, versions[scope.frame]}})
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
				return err
			}
		}
	}
	if err = t.collectPasswords(ctx, i, st, a.Tab); err != nil {
		return err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	for frame, version := range versions {
		if st.epochs[frame] != version {
			return errors.New("Page changed during snapshot; refresh with browser_snapshot")
		}
	}
	st.snapshot, st.interactive = lines, a.InteractiveOnly
	st.refs = map[string]nodeRef{}
	return nil
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
func (t Tools) waitFor(ctx context.Context, i *agentbrowser.Instance, _ *tabState, a arguments) (string, error) {
	if a.Text == "" {
		return "", errors.New("text is required")
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		scopes, err := t.scopes(ctx, i, a.Tab)
		if err != nil {
			return "", err
		}
		for _, scope := range scopes {
			found, e := t.isolated(ctx, i, a.Tab, scope, `(() => {
 const needle = `+jsonText(a.Text)+`;
 function contains(root) {
  if ((root.innerText || '').includes(needle)) return true;
  for (const e of root.querySelectorAll('*')) {
   if (e.shadowRoot && [...e.shadowRoot.children].some(contains)) return true;
  }
  return false;
 }
 return contains(document.body || document.documentElement);
})()`)
			// Frames can navigate while waiting. Retry against the next current frame tree.
			if e == nil && string(found) == "true" {
				return "Text found in frame " + scope.frame + ": " + jsonText(a.Text), nil
			}
		}
		select {
		case <-ctx.Done():
			return "", errors.New("Timed out waiting for text")
		case <-ticker.C:
		}
	}
}

func (t Tools) isolated(ctx context.Context, i *agentbrowser.Instance, id string, scope frameScope, expression string) (json.RawMessage, error) {
	world, err := i.AgentFrameCall(ctx, t.Caller, id, scope.session, "Page.createIsolatedWorld", map[string]any{
		"frameId": scope.frame, "worldName": "browser-tools",
	})
	if err != nil {
		return nil, err
	}
	var w struct {
		ID int `json:"executionContextId"`
	}
	if err = json.Unmarshal(world, &w); err != nil {
		return nil, err
	}
	data, err := i.AgentFrameCall(ctx, t.Caller, id, scope.session, "Runtime.evaluate", map[string]any{
		"contextId": w.ID, "returnByValue": true, "expression": expression,
	})
	if err != nil {
		return nil, err
	}
	var result struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
	}
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result.Result.Value, nil
}

func linkTarget(n axNode) string {
	if n.Role.Value != "link" {
		return ""
	}
	for _, property := range n.Properties {
		if property.Name != "url" {
			continue
		}
		url, ok := property.Value.Value.(string)
		if !ok || url == "" {
			return ""
		}
		const limit = 240
		if len(url) > limit {
			url = strings.ToValidUTF8(url[:limit], "") + "… [URL truncated]"
		}
		return " href=" + jsonText(url)
	}
	return ""
}
