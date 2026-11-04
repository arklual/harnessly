package agentbrowser

import (
	"regexp"
	"strings"
)

// Input is what the person does in the pane.
type Input struct {
	Accept     bool    `json:"accept,omitempty"`
	Holder     string  `json:"holder,omitempty"`
	Mode       string  `json:"mode,omitempty"`
	W          int     `json:"w,omitempty"`
	H          int     `json:"h,omitempty"`
	DPR        float64 `json:"dpr,omitempty"`
	T          string  `json:"t"` // See the complete wire contract in protocol.go.
	Type       string  `json:"type,omitempty"`
	Button     string  `json:"button,omitempty"`
	Buttons    int     `json:"buttons,omitempty"`
	ClickCount int     `json:"clickCount,omitempty"`
	X          float64 `json:"x,omitempty"`
	Y          float64 `json:"y,omitempty"`
	DX         float64 `json:"dx,omitempty"`
	DY         float64 `json:"dy,omitempty"`
	Key        string  `json:"key,omitempty"`
	Code       string  `json:"code,omitempty"`
	KeyCode    int     `json:"keyCode,omitempty"`
	Text       string  `json:"text,omitempty"`
	ID         string  `json:"id,omitempty"`
	Action     string  `json:"action,omitempty"`
	URL        string  `json:"url,omitempty"`
	Alt        bool    `json:"alt,omitempty"`
	Ctrl       bool    `json:"ctrl,omitempty"`
	Meta       bool    `json:"meta,omitempty"`
	Shift      bool    `json:"shift,omitempty"`
}

func (in Input) modifiers() int {
	m := 0
	if in.Alt {
		m |= 1
	}
	if in.Ctrl {
		m |= 2
	}
	if in.Meta {
		m |= 4
	}
	if in.Shift {
		m |= 8
	}
	return m
}

var mouseTypes = map[string]bool{"mousePressed": true, "mouseReleased": true, "mouseMoved": true}

var buttons = map[string]bool{"none": true, "left": true, "middle": true, "right": true}

var schemeRe = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*:`)

// Calls is what an input asks of Chrome, in order; nothing for one the pane cannot send.
func (in Input) Calls() []Call {
	mods := in.modifiers()
	switch in.T {
	case "mouse":
		if !mouseTypes[in.Type] {
			return nil
		}
		b := in.Button
		if !buttons[b] {
			b = "none"
		}
		return []Call{{"Input.dispatchMouseEvent", map[string]any{"type": in.Type, "x": in.X, "y": in.Y, "button": b,
			"buttons": in.Buttons, "clickCount": in.ClickCount, "modifiers": mods}}}
	case "wheel":
		return []Call{{"Input.dispatchMouseEvent", map[string]any{"type": "mouseWheel", "x": in.X, "y": in.Y,
			"deltaX": in.DX, "deltaY": in.DY, "modifiers": mods}}}
	case "text":
		if in.Text == "" {
			return nil
		}
		return []Call{{"Input.insertText", map[string]any{"text": in.Text}}}
	case "key":
		if in.Key == "" {
			return nil
		}
		base := map[string]any{"key": in.Key, "code": in.Code, "windowsVirtualKeyCode": in.KeyCode,
			"nativeVirtualKeyCode": in.KeyCode, "modifiers": mods}
		text := ""
		if len([]rune(in.Key)) == 1 && !in.Ctrl && !in.Meta && !in.Alt {
			text = in.Key
		} else if in.Key == "Enter" {
			text = "\r"
		}
		down := map[string]any{"type": "rawKeyDown"}
		if text != "" {
			down = map[string]any{"type": "keyDown", "text": text, "unmodifiedText": text}
		}
		for k, val := range base {
			down[k] = val
		}
		up := map[string]any{"type": "keyUp"}
		for k, val := range base {
			up[k] = val
		}
		return []Call{{"Input.dispatchKeyEvent", down}, {"Input.dispatchKeyEvent", up}}
	case "nav":
		switch in.Action {
		case "reload":
			return []Call{{"Page.reload", map[string]any{}}}
		case "back", "forward":
			return []Call{{"Runtime.evaluate", map[string]any{"expression": "history." + in.Action + "()"}}}
		case "go":
			u := NormalizeURL(in.URL)
			if u == "" {
				return nil
			}
			return []Call{{"Page.navigate", map[string]any{"url": u}}}
		}
	}
	return nil
}

// Call is one CDP method and its params.
type Call struct {
	Method string
	Params map[string]any
}

// NormalizeURL supplies HTTPS for addresses without an explicit scheme.
func NormalizeURL(u string) string {
	u = strings.TrimSpace(u)
	if u != "" && !schemeRe.MatchString(u) {
		u = "https://" + u
	}
	return u
}
