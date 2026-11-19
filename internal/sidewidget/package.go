package sidewidget

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
)

// A widget an agent makes for the chat's right column: a small app of its own, `local.widget-<name>`, with
// one view of the side target. It is an ordinary sandboxed package (an opaque frame, the bridge, no permission), so it
// reads nothing of the page but the data its manifest names; the person's widgets list (their settings) adds it once
// by itself. `harnessly widget create|update` writes it into a folder and installs it.

// Publisher is the publisher of the widgets an agent makes: they are the person's own, on their machine.
const Publisher = "local"

// View is the view id of such a widget.
const View = "side"

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,32}$`)

// Spec is what `harnessly widget create` is told.
type Spec struct {
	Name    string   `json:"name"`
	Title   string   `json:"title"`
	Icon    string   `json:"icon,omitempty"`
	HTML    string   `json:"html,omitempty"`
	CSS     string   `json:"css,omitempty"`
	JS      string   `json:"js,omitempty"`
	Size    string   `json:"size,omitempty"`
	Refresh int      `json:"refresh,omitempty"`
	Sources []string `json:"sources,omitempty"`
	Smart   string   `json:"smart,omitempty"`
	Every   int      `json:"every,omitempty"`
	// Scope is "chat" (shown in the chat it was made for, and where the person pins it) or "global" (every chat);
	// empty is chat when Chat is known. Chat is that conversation, put by the one who makes it.
	Scope string `json:"scope,omitempty"`
	Chat  string `json:"chat,omitempty"`
}

// Scopes of a widget.
const (
	ScopeChat   = "chat"
	ScopeGlobal = "global"
)

// ChatOf is the conversation a widget of this spec belongs to, "" for one of every chat.
func (s Spec) ChatOf() string {
	if s.Scope == ScopeGlobal {
		return ""
	}
	return s.Chat
}

// SideWidgetID is the app id of the widget named name.
func ID(name string) string { return Publisher + ".widget-" + name }

// SideWidgetName is the name of a widget's app id, or "" for an app that is not such a widget.
func NameOf(id string) string {
	name, ok := strings.CutPrefix(id, Publisher+".widget-")
	if !ok || !nameRe.MatchString(name) {
		return ""
	}
	return name
}

// SideWidgetPackage is the widget's package: its manifest and its files by path. The version is new each time, so an
// update installs over the last one.
func Package(s Spec, now time.Time) (*apps.Manifest, map[string]string, error) {
	if !nameRe.MatchString(s.Name) {
		return nil, nil, fmt.Errorf("%q: a widget's name is lower case latin letters, digits and '-', up to 33", s.Name)
	}
	title := strings.TrimSpace(s.Title)
	if title == "" {
		return nil, nil, fmt.Errorf("a widget needs --title")
	}
	if strings.TrimSpace(s.HTML) == "" && strings.TrimSpace(s.Smart) == "" && len(s.Sources) == 0 {
		return nil, nil, fmt.Errorf("a widget shows something: give --html, --smart or --sources")
	}
	if s.Scope != "" && s.Scope != ScopeChat && s.Scope != ScopeGlobal {
		return nil, nil, fmt.Errorf("--scope is chat or global, not %q", s.Scope)
	}
	w := &apps.ViewWidget{Size: s.Size, Refresh: s.Refresh, Sources: s.Sources, Chat: s.ChatOf()}
	if strings.TrimSpace(s.Smart) != "" {
		w.Smart = &apps.WidgetSmart{Prompt: strings.TrimSpace(s.Smart), Every: s.Every}
	}
	m := &apps.Manifest{
		ID:          ID(s.Name),
		Name:        title,
		Description: "A widget of the chat's right column, made by an agent",
		Version:     fmt.Sprintf("0.1.%d", now.Unix()),
		Harnessly:   ">=" + HostVersion,
		Trust:       apps.TrustSandboxed,
		Targets:     []string{apps.TargetSide},
		Permissions: []string{},
		Views: []apps.View{{ID: View, Title: title, Label: title, Icon: s.Icon, Entry: "ui/index.html",
			Targets: []string{apps.TargetSide}, Widget: w}},
	}
	if probs := m.Check().Errors(); len(probs) > 0 {
		return nil, nil, probs
	}
	body := widgetBody(s)
	js := s.JS
	patchable := true
	if strings.TrimSpace(js) == "" {
		js = defaultJS
	} else if !strings.Contains(js, "harnessly.connect") {
		// A widget's own script that never connects would leave the card on «не запустило SDK»: it connects first and
		// its code runs after, so a script that only draws (a canvas, a clock) works as written.
		js = s.JS
	} else {
		// A script that connects itself cannot be given the patch of its markup: a new version starts its frame anew.
		patchable = false
	}
	if patchable {
		js = "window.harnessly.connect().then(function (hl) {\n" + patchJS + "\n" + js + "\n});\n"
		w.Shell = shortHash(js)
	}
	rev := ContentOf(s).Rev
	files := map[string]string{
		"ui/index.html": `<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + html.EscapeString(title) + `</title>
<link rel="stylesheet" href="/__hl/hl-ui.css">
<link rel="stylesheet" href="style.css">
</head>
<body class="hl-body">
<div id="hl-widget" data-rev="` + rev + `">` + body + `</div>
<style id="hl-widget-css"></style>
<script src="/__hl/app-sdk.js"></script>
<script src="main.js"></script>
</body>
</html>
`,
		"ui/style.css": baseCSS + s.CSS,
		"ui/main.js":   js,
	}
	return m, files, nil
}

// HostVersion is the host that draws side widgets.
const HostVersion = "0.16.342"

// ManifestJSON is the manifest as the package's harnessly/app.json.
func ManifestJSON(m *apps.Manifest) ([]byte, error) {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

const baseCSS = `/* A widget is small: no margins of the page, the host's tokens (--t-*) and primitives (.hl-*) from hl-ui.css. */
html, body { margin: 0; background: transparent; }
body { padding: 4px 8px 8px; font-size: 13px; line-height: 18px; }
`

// The script of a widget written without one: the smart answer when there is one, else the data it was given.
const defaultJS = `function draw(ctx) {
  var root = document.getElementById('w');
  if (!root) return;
  var data = (ctx && ctx.data) || {};
  var smart = data.smart;
  if (smart && smart.text) { root.textContent = smart.text; return; }
  if (smart && smart.asking) { root.textContent = '…'; return; }
  root.textContent = JSON.stringify(data.sources || {}, null, 1);
}
draw(hl.context);
hl.on('context', draw);`

// patchJS puts a new version of the widget's markup and style in the live frame: the page hands them in
// hl.context.data.content when the agent updated the widget, and the card changes where it stands, with no new frame.
const patchJS = `(function () {
  var root = document.getElementById('hl-widget'), css = document.getElementById('hl-widget-css');
  var rev = root.getAttribute('data-rev');
  function patch(ctx) {
    var c = ctx && ctx.data && ctx.data.content;
    if (!c || !c.rev || c.rev === rev) return;
    rev = c.rev;
    root.innerHTML = c.html;
    css.textContent = c.css;
  }
  patch(hl.context);
  hl.on('context', patch);
})();`

// Content is what the page hands a live frame to change what the card shows without starting it anew.
type Content struct {
	Rev  string `json:"rev"`
	HTML string `json:"html"`
	CSS  string `json:"css"`
}

// widgetBody is the markup of the card's body: the agent's own, or the one place the smart answer and the data go.
func widgetBody(s Spec) string {
	if strings.TrimSpace(s.HTML) == "" {
		return `<main id="w" class="hl-text"></main>`
	}
	return s.HTML
}

// ContentOf is a widget's markup and style as the frame holds them, and their revision.
func ContentOf(s Spec) Content {
	c := Content{HTML: widgetBody(s), CSS: baseCSS + s.CSS}
	c.Rev = shortHash(c.HTML + "\x00" + c.CSS)
	return c
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:6])
}

// Place puts a new widget where it belongs: the chat of the one who makes it, unless it asks for every chat.
// A caller with no chat (a terminal that is no chat's) makes a widget for every chat.
func Place(sp Spec, chat string) (Spec, error) {
	switch sp.Scope {
	case "", ScopeChat:
		sp.Scope, sp.Chat = ScopeChat, chat
		if chat == "" {
			sp.Scope = ScopeGlobal
		}
	case ScopeGlobal:
		sp.Chat = ""
	default:
		return sp, fmt.Errorf("--scope is chat or global, not %q", sp.Scope)
	}
	return sp, nil
}

// PlacePatch is an update's change of scope with the caller's chat: «chat» means the caller's chat.
func PlacePatch(p Patch, chat string) (Patch, error) {
	if p.Scope == nil {
		return p, nil
	}
	switch *p.Scope {
	case ScopeChat:
		if chat == "" {
			return p, fmt.Errorf("no chat here: --scope chat is for a chat's own widget")
		}
		p.Chat = &chat
	case ScopeGlobal:
		empty := ""
		p.Chat = &empty
	default:
		return p, fmt.Errorf("--scope is chat or global, not %q", *p.Scope)
	}
	return p, nil
}
