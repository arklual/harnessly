package sidewidget

import (
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
	w := &apps.ViewWidget{Size: s.Size, Refresh: s.Refresh, Sources: s.Sources}
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
	body := s.HTML
	if strings.TrimSpace(body) == "" {
		body = `<main id="w" class="hl-text"></main>`
	}
	js := s.JS
	if strings.TrimSpace(js) == "" {
		js = defaultJS
	}
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
` + body + `
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
const defaultJS = `var root = document.getElementById('w');
function draw(ctx) {
  var data = (ctx && ctx.data) || {};
  var smart = data.smart;
  if (smart && smart.text) { root.textContent = smart.text; return; }
  if (smart && smart.asking) { root.textContent = '…'; return; }
  root.textContent = JSON.stringify(data.sources || {}, null, 1);
}
window.harnessly.connect().then(function (hl) {
  draw(hl.context);
  hl.on('context', draw);
}, function () { root.textContent = 'open inside harnessly'; });
`
