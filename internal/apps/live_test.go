package apps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A chat with no app on has the server all the same: it offers nothing and says nothing, so it costs the agent
// nothing; an app turned on brings the tools.
func TestHostToolsQuietWithoutApps(t *testing.T) {
	none, _ := hostRig(t, nil)
	if got := none.Tools(); len(got) != 0 {
		t.Fatalf("tools of a chat without apps: %d", len(got))
	}
	if got := none.Instructions(); got != "" {
		t.Fatalf("words of a chat without apps: %q", got)
	}
	on, _ := hostRig(t, []string{"acme.cash"})
	if len(on.Tools()) == 0 || on.Instructions() == "" {
		t.Fatal("an app on must bring the tools and their words")
	}
	if none.Offering() == on.Offering() {
		t.Fatal("the offering must change with the apps on")
	}
	// A server that does not know the chat (HostTools{} as a test makes it) lists everything, as before.
	if len(HostTools{}.Tools()) == 0 {
		t.Fatal("HostTools{} must list its tools")
	}
}

// An app that waits for the agent's next start refuses its tools with the reason, which the agent tells the person.
func TestHostToolsLiveRefusal(t *testing.T) {
	h, opened := hostRig(t, []string{"acme.cash"})
	h.Live = func(app string) string { return LiveRefusal("Cash", "rules for the agent's tools") }
	text, bad := call(t, h, "open")
	if !bad || !strings.Contains(text, "only a new start of the agent gives") || len(*opened) != 0 {
		t.Fatalf("refusal: %q %v", text, *opened)
	}
	h.Live = func(string) string { return "" }
	if text, bad := call(t, h, "open"); bad {
		t.Fatalf("a live app: %q", text)
	}
}

func writeApp(t *testing.T, manifest string, files map[string]string) *Package {
	t.Helper()
	dir := t.TempDir()
	files["harnessly/app.json"] = manifest
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pkg, ps := LoadPackage(dir)
	if pkg == nil || len(ps.Errors()) > 0 {
		t.Fatalf("package: %v", ps)
	}
	return pkg
}

const liveManifest = `{"id":"acme.cashback","name":"Кэшбэк","description":"Форма кэшбэка","version":"0.1.0","harnessly":">=0.16.0","trust":"sandboxed",
"targets":["chat","pane"],"permissions":["storage"],
"agent":{"instructions":"instructions.md","skills":["how-to"],"tools":{}},
"views":[{"id":"main","title":"Кэшбэк","entry":"ui/index.html","targets":["pane"]}]}`

func TestLaunchNeeds(t *testing.T) {
	files := func() map[string]string {
		return map[string]string{"instructions.md": "Форма в ключе offer.", "skills/how-to/SKILL.md": "---\nname: how-to\ndescription: x\n---\n", "ui/index.html": "<p>x</p>"}
	}
	plain := writeApp(t, liveManifest, files())
	if needs := LaunchNeeds(plain); needs != "" {
		t.Fatalf("an app of instructions, skills and views goes live: %q", needs)
	}
	ruled := writeApp(t, strings.Replace(liveManifest, `"tools":{}`, `"tools":{"deny":["Bash"]}`, 1), files())
	if needs := LaunchNeeds(ruled); !strings.Contains(needs, "rules for the agent's tools") {
		t.Fatalf("rules need the guard of a launch: %q", needs)
	}
	withHooks := files()
	withHooks["hooks/hooks.json"] = `{}`
	if needs := LaunchNeeds(writeApp(t, strings.Replace(liveManifest, `"sandboxed"`, `"trusted"`, 1), withHooks)); !strings.Contains(needs, "plugin parts (hooks/)") {
		t.Fatalf("a plugin's hooks come only with a launch: %q", needs)
	}
	a := Installed{ID: plain.Manifest.ID, Dir: plain.Dir, Manifest: plain.Manifest}
	brief, err := LiveBrief(a)
	if err != nil || !strings.Contains(brief, "## App: Кэшбэк (acme.cashback)") || !strings.Contains(brief, "Форма в ключе offer.") ||
		!strings.Contains(brief, filepath.Join(plain.Dir, "skills", "how-to", "SKILL.md")) {
		t.Fatalf("brief: %v %q", err, brief)
	}
	notice := LiveNotice([]string{brief}, []string{LiveRefusal("Other", "hooks")}, []string{"Old (acme.old)"})
	for _, want := range []string{"# Apps turned on in this chat", "mcp__harnessly-app__", "app_data_set", "Форма в ключе offer.",
		"# Apps that wait for the agent's next start", "Old (acme.old)"} {
		if !strings.Contains(notice, want) {
			t.Fatalf("notice lacks %q: %s", want, notice)
		}
	}
	if LiveNotice(nil, nil, nil) != "" {
		t.Fatal("no change, no notice")
	}
}

// The config of a chat without apps holds the person's token, as the runtime's own does: it lives in the data folder,
// which the wall of a launch with apps leaves unreadable, never in the registry's launch folder the wall shows.
func TestLauncherHostMCPWithoutApps(t *testing.T) {
	data := t.TempDir()
	reg := &Registry{Root: filepath.Join(data, "apps")}
	mcp := filepath.Join(data, "preview-mcp.json")
	_ = os.WriteFile(mcp, []byte(`{"mcpServers":{"host-preview":{"type":"http","url":"http://127.0.0.1:1/api/cli/preview/mcp","headers":{"x-cli-token":"tok"}}}}`), 0o600)
	l := &Launcher{Registry: reg, MCPConfigPath: mcp}
	path, err := l.HostMCP()
	if err != nil || path != filepath.Join(data, HostMCPFile) {
		t.Fatalf("path %q %v", path, err)
	}
	raw, _ := os.ReadFile(path)
	if s := string(raw); !strings.Contains(s, `"harnessly-app"`) || !strings.Contains(s, "preview/mcp?server=harnessly-app") || !strings.Contains(s, `"tok"`) {
		t.Fatalf("config: %s", s)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	if again, err := l.HostMCP(); err != nil || again != path {
		t.Fatalf("again: %q %v", again, err)
	}
	if got, err := (&Launcher{Registry: reg}).HostMCP(); err != nil || got != "" {
		t.Fatalf("no runtime config: %q %v", got, err)
	}
}
