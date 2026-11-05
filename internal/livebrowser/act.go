package livebrowser

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// The agent's actions on a page of the live browser: what the pane hands over after the person let the agent
// onto the site, the same ops as the proxy's bridge takes (web/app/next/features/pane/agent-acts.ts). The page is read
// and its fields set by act.js in an isolated world; the pointer and the keys are the browser's own input events, so
// the page sees a person's click, not a script's.

//go:embed act.js
var actScript string

// Act is one action: op is snapshot, locate, click, type, press, hover, select, scroll, wait, tools or call; navigate
// and back are the tab's (the pane does them through the socket's nav and back).
type Act struct {
	ID     string `json:"id"`
	Op     string `json:"op"`
	Ref    string `json:"ref"`
	Text   string `json:"text"`
	Key    string `json:"key"`
	Value  string `json:"value"`
	DY     int    `json:"dy"`
	MS     int    `json:"ms"`
	Submit bool   `json:"submit"`
	// Event is what a wait is for on a video or audio: ended, playing or paused.
	Event string `json:"event"`
	Tool  string `json:"tool"`
	Input string `json:"input"`
	// Host is the site the person let the agent act on: a page that moved to another site since is not acted on.
	Host string `json:"host"`
	// x, y: the point of a hit check, the browser's own.
	x, y float64
}

// Rect is an element's box in the page's viewport, CSS pixels.
type Rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Acted is what an action came to; Rect is its element's box, for the agent's cursor in the pane.
type Acted struct {
	ID   string `json:"id"`
	OK   bool   `json:"ok"`
	Text string `json:"text"`
	Rect *Rect  `json:"rect"`
}

const (
	actWorld   = "hx-act"
	actTimeout = 13 * time.Second
	// After a click or an Enter: how long a move of the page is waited for to begin, and then to end, before the
	// address is read for the answer.
	settleBegin = 300 * time.Millisecond
	settleLoad  = 3 * time.Second
)

// scriptResult is what act.js answers.
type scriptResult struct {
	OK     bool   `json:"ok"`
	Text   string `json:"text"`
	Rect   *Rect  `json:"rect"`
	What   string `json:"what"`
	Secret bool   `json:"secret"`
	Frame  bool   `json:"frame"`
}

const secretRefusal = "This field takes a password or card details: the person types it themselves."

func (r scriptResult) acted(id string) Acted {
	return Acted{ID: id, OK: r.OK, Text: r.Text, Rect: r.Rect}
}

func actFail(id, text string) Acted { return Acted{ID: id, OK: false, Text: text} }

// Act runs one action on the page and never fails loudly: a page that breaks it answers with words.
func (t *Tab) Act(ctx context.Context, a Act) Acted {
	timeout := actTimeout
	if a.Op == "wait" && a.Event != "" {
		// The end of a clip may be far off: such a wait has its own time on top.
		timeout += time.Duration(a.MS) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	before, _ := t.Info()
	if why := offSite(before.URL, a.Host); why != "" {
		return actFail(a.ID, why)
	}
	// note says where the page went, when a step moved it: a move that has begun is given a moment to end.
	note := func(text string) string {
		after := t.settle(ctx, before)
		if after.URL != before.URL {
			return text + "; the page is now " + after.URL
		}
		return text
	}
	switch a.Op {
	case "snapshot", "read", "wait", "select", "locate", "tools", "call":
		r, err := t.script(ctx, a)
		if err != nil {
			return actFail(a.ID, err.Error())
		}
		return r.acted(a.ID)
	case "scroll":
		r, err := t.script(ctx, a)
		if err != nil {
			return actFail(a.ID, err.Error())
		}
		return r.acted(a.ID)
	case "type":
		r, err := t.script(ctx, a)
		if err != nil {
			return actFail(a.ID, err.Error())
		}
		if !r.OK {
			return r.acted(a.ID)
		}
		text := r.Text
		if a.Submit {
			// The Enter is the browser's: the field has to hold the focus still, on the same site.
			h, err := t.script(ctx, Act{Op: "holds", Ref: a.Ref, Host: a.Host})
			if err != nil {
				return actFail(a.ID, err.Error())
			}
			if !h.OK {
				return h.acted(a.ID)
			}
			if err := t.press(ctx, "Enter"); err != nil {
				return actFail(a.ID, err.Error())
			}
			text += " and pressed Enter"
		}
		return Acted{ID: a.ID, OK: true, Text: note(text), Rect: r.Rect}
	case "click", "hover":
		r, err := t.script(ctx, Act{Op: "prepare", Ref: a.Ref, Value: a.Op, Host: a.Host})
		if err != nil {
			return actFail(a.ID, err.Error())
		}
		if !r.OK || r.Rect == nil {
			return r.acted(a.ID)
		}
		x, y := r.Rect.X+r.Rect.Width/2, r.Rect.Y+r.Rect.Height/2
		if err := t.Mouse("mouseMoved", x, y, "none", 0, 0, 0); err != nil {
			return actFail(a.ID, err.Error())
		}
		if a.Op == "hover" {
			return Acted{ID: a.ID, OK: true, Text: "hovered " + r.Text, Rect: r.Rect}
		}
		// The click is the browser's and lands on whatever is at the point: right before it, the document is asked
		// whether that is still this element, on the site the person allowed.
		h, err := t.script(ctx, Act{Op: "hit", Ref: a.Ref, Host: a.Host, x: x, y: y})
		if err != nil {
			return actFail(a.ID, err.Error())
		}
		if !h.OK {
			return h.acted(a.ID)
		}
		if err := t.Mouse("mousePressed", x, y, "left", 1, 0, 1); err != nil {
			return actFail(a.ID, err.Error())
		}
		if err := t.Mouse("mouseReleased", x, y, "left", 0, 0, 1); err != nil {
			return actFail(a.ID, err.Error())
		}
		return Acted{ID: a.ID, OK: true, Text: note("clicked " + r.Text), Rect: r.Rect}
	case "press":
		if a.Key == "" {
			return actFail(a.ID, "press needs a key.")
		}
		// A key is the browser's key: it would type, delete or paste in a password field like anyone's, and inside a
		// frame it reaches a document that cannot be read from here. Only the keys that move about are sent while a
		// secret field has the focus; nothing is sent into a frame.
		r, err := t.script(ctx, Act{Op: "active", Host: a.Host})
		if err != nil {
			return actFail(a.ID, err.Error())
		}
		if !r.OK {
			// Another site, or a page in the way: no key.
			return r.acted(a.ID)
		}
		if r.Frame {
			return actFail(a.ID, "The focus is inside a frame of the page, which the agent cannot read: keys are not sent there.")
		}
		if r.Secret && !movesOnly(a.Key) {
			return actFail(a.ID, secretRefusal)
		}
		if err := t.press(ctx, a.Key); err != nil {
			return actFail(a.ID, err.Error())
		}
		text := note("pressed " + a.Key)
		var rect *Rect
		if r, err := t.script(ctx, Act{Op: "active", Host: a.Host}); err == nil {
			rect = r.Rect
		}
		return Acted{ID: a.ID, OK: true, Text: text, Rect: rect}
	default:
		return actFail(a.ID, "Unknown action "+a.Op+".")
	}
}

// offSite says why the page is not the site the person allowed: "" when it is, or when no site was named.
func offSite(pageURL, host string) string {
	if host == "" {
		return ""
	}
	u, err := url.Parse(pageURL)
	if err != nil || !strings.EqualFold(u.Hostname(), host) {
		return "The page is on another site now (" + hostOf(pageURL) + "), not " + host + ": the person has to allow it; take a snapshot."
	}
	return ""
}

func hostOf(pageURL string) string {
	if u, err := url.Parse(pageURL); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return "no site"
}

// settle waits a moment for a move of the page to begin after a step, and for one that began to end, and gives the
// page as it is then.
func (t *Tab) settle(ctx context.Context, before Info) Info {
	now := before
	begin := time.Now().Add(settleBegin)
	for time.Now().Before(begin) {
		if info, err := t.Info(); err == nil {
			now = info
			if now.Loading || now.URL != before.URL {
				break
			}
		}
		if !sleepCtx(ctx, 20*time.Millisecond) {
			return now
		}
	}
	load := time.Now().Add(settleLoad)
	for now.Loading && time.Now().Before(load) {
		if !sleepCtx(ctx, 50*time.Millisecond) {
			return now
		}
		if info, err := t.Info(); err == nil {
			now = info
		}
	}
	return now
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

// press sends a key, with the modifiers of a chord like Control+a.
func (t *Tab) press(ctx context.Context, chord string) error {
	k := keyOf(chord)
	if err := t.Key("keyDown", k.key, k.code, k.text, k.vk, k.modifiers); err != nil {
		return err
	}
	return t.Key("keyUp", k.key, k.code, "", k.vk, k.modifiers)
}

// movesOnly says whether a key only moves the focus or the caret, or submits: safe in a field for a password.
func movesOnly(chord string) bool {
	k := keyOf(chord)
	if k.modifiers&^8 != 0 {
		// A chord with Control, Alt or Meta may paste or do anything.
		return false
	}
	switch k.key {
	case "Tab", "Escape", "Enter", "ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown", "Home", "End", "PageUp", "PageDown":
		return true
	}
	return false
}

type keyPress struct {
	key, code, text string
	vk, modifiers   int
}

var virtualKeys = map[string]int{
	"Backspace": 8, "Tab": 9, "Enter": 13, "Shift": 16, "Control": 17, "Alt": 18, "Pause": 19, "CapsLock": 20,
	"Escape": 27, " ": 32, "PageUp": 33, "PageDown": 34, "End": 35, "Home": 36, "ArrowLeft": 37, "ArrowUp": 38,
	"ArrowRight": 39, "ArrowDown": 40, "Insert": 45, "Delete": 46, "Meta": 91, "ContextMenu": 93,
	"F1": 112, "F2": 113, "F3": 114, "F4": 115, "F5": 116, "F6": 117, "F7": 118, "F8": 119, "F9": 120, "F10": 121, "F11": 122, "F12": 123,
}

// keyOf reads a key or a chord (Control+a, Shift+Enter) the way the proxy's bridge does, into what the protocol wants.
func keyOf(chord string) keyPress {
	parts := []string{chord}
	if len(chord) > 1 && strings.Contains(chord, "+") {
		parts = strings.Split(chord, "+")
	}
	main := parts[len(parts)-1]
	if main == "" {
		main = "+"
	}
	k := keyPress{key: main, code: main}
	for _, part := range parts[:len(parts)-1] {
		switch strings.ToLower(part) {
		case "alt":
			k.modifiers |= 1
		case "control", "ctrl":
			k.modifiers |= 2
		case "meta", "cmd", "command":
			k.modifiers |= 4
		case "shift":
			k.modifiers |= 8
		}
	}
	switch {
	case main == " ":
		k.code = "Space"
	case len(main) == 1 && ((main[0] >= 'a' && main[0] <= 'z') || (main[0] >= 'A' && main[0] <= 'Z')):
		k.code = "Key" + strings.ToUpper(main)
	case len(main) == 1 && main[0] >= '0' && main[0] <= '9':
		k.code = "Digit" + main
	}
	if vk, ok := virtualKeys[main]; ok {
		k.vk = vk
	} else if len(main) == 1 {
		k.vk = int(strings.ToUpper(main)[0])
	}
	if k.modifiers&(2|4) == 0 {
		if main == "Enter" {
			k.text = "\r"
		} else if len([]rune(main)) == 1 {
			k.text = main
		}
	}
	return k
}

// script runs act.js for an act in the page's isolated world, making the world when the page has none (its first act,
// or a new document since the last one).
func (t *Tab) script(ctx context.Context, a Act) (scriptResult, error) {
	arg := map[string]any{"op": a.Op, "ref": a.Ref}
	if a.Host != "" {
		// The site is checked by the script on the document it runs on, not on a cached address.
		arg["host"] = a.Host
	}
	switch a.Op {
	case "hit":
		arg["x"], arg["y"] = a.x, a.y
	case "wait":
		if a.Event != "" {
			arg["event"] = a.Event
		}
		if a.Text != "" {
			arg["text"] = a.Text
		}
		if a.MS > 0 || a.Text == "" {
			arg["ms"] = a.MS
		}
	case "scroll":
		arg["dy"] = a.DY
	case "type":
		arg["text"] = a.Text
	case "select":
		arg["value"] = a.Value
	case "prepare":
		arg["for"] = a.Value
	}
	raw, err := json.Marshal(arg)
	if err != nil {
		return scriptResult{}, err
	}
	expression := "__hxAct(" + string(raw) + ")"
	for attempt := 0; attempt < 2; attempt++ {
		id, err := t.world(ctx, attempt > 0)
		if err != nil {
			return scriptResult{}, err
		}
		var r struct {
			Result struct {
				Value json.RawMessage `json:"value"`
			} `json:"result"`
			ExceptionDetails *struct {
				Text      string `json:"text"`
				Exception *struct {
					Description string `json:"description"`
				} `json:"exception"`
			} `json:"exceptionDetails"`
		}
		err = t.conn.Call(ctx, t.sessionID, "Runtime.evaluate", map[string]any{
			"expression": expression, "contextId": id, "returnByValue": true, "awaitPromise": true, "userGesture": true,
		}, &r)
		if err != nil {
			if attempt == 0 && isGoneContext(err) {
				continue
			}
			return scriptResult{}, err
		}
		if r.ExceptionDetails != nil {
			text := r.ExceptionDetails.Text
			if r.ExceptionDetails.Exception != nil {
				text = r.ExceptionDetails.Exception.Description
			}
			if attempt == 0 && strings.Contains(text, "__hxAct") {
				// The world is there but the script is not (a new document kept the context id): install again.
				continue
			}
			return scriptResult{}, errors.New("the page got in the way of the action: " + clipText(text, 200))
		}
		var out scriptResult
		if err := json.Unmarshal(r.Result.Value, &out); err != nil {
			return scriptResult{}, errors.New("the page answered oddly")
		}
		return out, nil
	}
	return scriptResult{}, errors.New("the page's world could not be made")
}

func isGoneContext(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "context") || strings.Contains(msg, "Execution context")
}

// world is the isolated world's context id; fresh makes a new one and installs the script.
func (t *Tab) world(ctx context.Context, fresh bool) (int, error) {
	t.mu.Lock()
	id := t.actCtx
	t.mu.Unlock()
	if id != 0 && !fresh {
		return id, nil
	}
	frame := t.mainFrame
	if frame == "" {
		var tree struct {
			FrameTree struct {
				Frame struct {
					ID string `json:"id"`
				} `json:"frame"`
			} `json:"frameTree"`
		}
		if err := t.conn.Call(ctx, t.sessionID, "Page.getFrameTree", nil, &tree); err != nil {
			return 0, err
		}
		frame = tree.FrameTree.Frame.ID
		t.mainFrame = frame
	}
	var made struct {
		ExecutionContextID int `json:"executionContextId"`
	}
	if err := t.conn.Call(ctx, t.sessionID, "Page.createIsolatedWorld", map[string]any{"frameId": frame, "worldName": actWorld}, &made); err != nil {
		return 0, fmt.Errorf("the page's world: %w", err)
	}
	if err := t.conn.Call(ctx, t.sessionID, "Runtime.evaluate", map[string]any{
		"expression": actScript, "contextId": made.ExecutionContextID, "returnByValue": true,
	}, nil); err != nil {
		return 0, fmt.Errorf("the page's script: %w", err)
	}
	t.mu.Lock()
	t.actCtx = made.ExecutionContextID
	t.mu.Unlock()
	return made.ExecutionContextID, nil
}

func clipText(s string, max int) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return string(r)
}
