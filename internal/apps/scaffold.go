package apps

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// harnessly app new: a package that passes validate as it is, for the author (or the agent) to fill in.

// FirstVersion is the first host version with apps.
const FirstVersion = "0.16.36"

// TemplateVersion is the host version a new package asks for: the lowest that has what the templates use (the host's
// hl-ui.css, protocol 2, widgets). The guide (docs/build-apps) says the same, and a test keeps them equal.
const TemplateVersion = "0.16.68"

// ViteTemplateVersion is the host that serves a view's module scripts, fetch and fonts to its opaque-origin frame (the
// files carry open CORS and the slot lets "Origin: null" through): what a bundler's build needs.
const ViteTemplateVersion = "0.16.198"

// Templates are the starting points of `harnessly app new --template`.
var Templates = []string{"chat", "pane-chat", "widget", "vite"}

// Scaffold writes a new package for id into dir, which must not exist or be empty.
func Scaffold(dir, id, name, template string) error {
	if !idRe.MatchString(id) {
		return fmt.Errorf("%q is not <publisher>.<name> in lower case latin letters, digits and '-', e.g. acme.cashback", id)
	}
	if template == "" {
		template = "chat"
	}
	if !contains(Templates, template) {
		return fmt.Errorf("no template %q: %s", template, strings.Join(Templates, ", "))
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s is not empty", dir)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if name == "" {
		_, short, _ := strings.Cut(id, ".")
		name = strings.ToUpper(short[:1]) + strings.ReplaceAll(short[1:], "-", " ")
	}
	skill := "how-to"

	m := Manifest{
		ID:          id,
		Name:        name,
		Description: "What " + name + " does, in one line",
		Version:     "0.1.0",
		Harnessly:   ">=" + TemplateVersion,
		Trust:       TrustDeclarative,
		Targets:     []string{TargetChat},
		Activation:  &Activation{On: []string{OnManual}},
		Permissions: []string{},
		Agent: &Agent{
			Instructions: "instructions.md",
			Skills:       []string{skill},
			Tools:        &ToolPolicy{Deny: []string{}},
		},
	}
	files := map[string]string{
		"instructions.md": "# " + name + "\n\nWhat the agent of a chat must know and keep to while " + name +
			" is on. It is in the context of every turn: keep it short, put the reference into skills.\n",
		"skills/" + skill + "/SKILL.md": "---\nname: " + skill + "\ndescription: When the agent opens this skill and what it gets from it.\n---\n\n" +
			"# " + name + "\n\nThe reference the agent reads on demand.\n",
		"CHANGELOG.md": "# Changelog\n\n## 0.1.0\n\n- First version.\n",
	}
	slug := id[strings.Index(id, ".")+1:]
	if template == "pane-chat" || template == "vite" {
		m.Trust = TrustSandboxed
		m.Targets = append(m.Targets, TargetPane, TargetApp)
		m.Activation = &Activation{On: []string{OnManual}, OpenView: "main"}
		m.Permissions = []string{"chat.prompt", "pane.open", "storage"}
		m.Views = []View{{ID: "main", Title: name, Entry: "ui/index.html", Targets: []string{TargetPane, TargetApp}}}
	}
	if template == "pane-chat" {
		files["ui/index.html"] = fillTemplate(paneChatHTML, name, slug)
		files["ui/style.css"] = paneChatCSS
		files["ui/main.js"] = fillTemplate(paneChatJS, name, slug)
	}
	if template == "vite" {
		m.Harnessly = ">=" + ViteTemplateVersion
		files["index.html"] = fillTemplate(viteIndexHTML, name, slug)
		files["ui/index.html"] = fillTemplate(viteBuiltPlaceholder, name, slug)
		files["package.json"] = fillTemplate(vitePackageJSON, name, slug)
		files["vite.config.ts"] = viteConfig
		files["tsconfig.json"] = viteTSConfig
		files["src/main.tsx"] = viteMainTSX
		files["src/App.tsx"] = fillTemplate(viteAppTSX, name, slug)
		files["src/global.d.ts"] = viteGlobalDTS
		files["src/app-sdk.d.ts"] = string(sdkDTS)
		files[".gitignore"] = viteGitignore
	}
	if template == "widget" {
		// A widget of the chat: the agent shows it in the feed with app.widget.show (or `harnessly widget show`) and the
		// data to draw; the same id again sends the view new data.
		m.Trust = TrustSandboxed
		m.Permissions = []string{"chat.prompt"}
		m.Views = []View{{ID: "card", Title: name, Entry: "ui/card.html", Targets: []string{TargetChat}}}
		m.Chat = &Chat{Cards: []string{"card"}}
		files["instructions.md"] = "# " + name + "\n\nShow the result to the person as the widget of this app: call the tool " +
			"mcp__harnessly-app__widget_show with view \"card\" and the data as JSON ({\"title\": ..., \"value\": ...}). " +
			"To change it later (progress, fresh numbers), call it again with the id it answered.\n"
		files["ui/card.html"] = "<!doctype html>\n<html lang=\"ru\">\n<head>\n<meta charset=\"utf-8\">\n" +
			"<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n<title>" + name + "</title>\n" +
			"<link rel=\"stylesheet\" href=\"/__hl/widget.css\">\n</head>\n" +
			"<body>\n<p class=\"muted\" id=\"title\"></p>\n<p class=\"stat\" id=\"value\"></p>\n" +
			"<script src=\"/__hl/app-sdk.js\"></script>\n<script src=\"card.js\"></script>\n</body>\n</html>\n"
		files["ui/card.js"] = "// The widget's data is hl.context.data; when the agent shows the same id again, the `data` event brings the new one.\n" +
			"window.harnessly.connect().then(function (hl) {\n" +
			"  function draw(d) {\n    d = d || {};\n    document.getElementById('title').textContent = d.title || '';\n" +
			"    document.getElementById('value').textContent = d.value == null ? '' : String(d.value);\n  }\n" +
			"  draw(hl.context.data);\n  hl.on('data', draw);\n}).catch(function () {});\n"
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	files[ManifestPath] = string(raw) + "\n"
	for rel, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return writePluginManifest(dir, &m)
}
