package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/wire"
)

const liveTestManifest = `{"id":"%s","name":"%s","description":"x","version":"0.1.0","harnessly":">=0.16.0","trust":"sandboxed",
"targets":["chat","pane"],"permissions":["storage"],
"agent":{"instructions":"instructions.md","tools":%s},
"views":[{"id":"main","title":"x","entry":"ui/index.html","targets":["pane"]}]}`

func installLiveApp(t *testing.T, reg *apps.Registry, id, name, tools string) {
	t.Helper()
	dir := t.TempDir()
	for file, body := range map[string]string{
		"harnessly/app.json": fmt.Sprintf(liveTestManifest, id, name, tools),
		"instructions.md":    "Instructions of " + name + ".",
		"ui/index.html":      "<p>x</p>",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, file)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := reg.Install(dir, apps.InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
		t.Fatal(err)
	}
}

// an app turned on in a running Claude Code chat goes live when it needs nothing of a launch; the agent is told
// once on its next turn, and of an app turned off; an app with rules, or any app where the wall is required and the
// agent started without it, waits for the next start with the reason.
func TestLiveApps(t *testing.T) {
	reg := &apps.Registry{Root: filepath.Join(t.TempDir(), "apps")}
	installLiveApp(t, reg, "acme.cash", "Кэшбэк", `{}`)
	installLiveApp(t, reg, "acme.ruled", "Строгое", `{"deny":["Bash"]}`)
	launched := map[string][]string{"s-plain": nil, "s-with": {"acme.ruled"}}
	l := &liveApps{reg: reg, launched: func(session string) []string { return launched[session] }}
	rec := wire.Conversation{ID: "c1", Session: "s-plain", Provider: wire.ProviderClaude}

	if !l.changeIsLive(rec, []string{"acme.cash"}) {
		t.Fatal("a plain app goes live in a chat started without apps")
	}
	if l.changeIsLive(rec, []string{"acme.ruled"}) {
		t.Fatal("an app with rules needs the guard of a launch")
	}
	if why := l.hostRefusal("s-plain")("acme.ruled"); !strings.Contains(why, "rules for the agent's tools") {
		t.Fatalf("refusal: %q", why)
	}
	if l.changeIsLive(wire.Conversation{ID: "c2", Session: "s-with", Provider: wire.ProviderClaude}, []string{"acme.cash"}) {
		t.Fatal("turning off an app the agent started with needs its next start")
	}
	if !l.changeIsLive(wire.Conversation{ID: "c2", Session: "s-with", Provider: wire.ProviderClaude}, []string{"acme.ruled", "acme.cash"}) {
		t.Fatal("adding a plain app beside the launch's goes live")
	}
	if l.changeIsLive(wire.Conversation{ID: "c3", Session: "s-plain", Provider: wire.ProviderCodex}, []string{"acme.cash"}) {
		t.Fatal("a Codex chat takes apps at its next start")
	}

	rec.Apps = []string{"acme.cash"}
	first := l.notice(rec)
	if !strings.Contains(first, "# Apps turned on in this chat") || !strings.Contains(first, "Instructions of Кэшбэк.") {
		t.Fatalf("first notice: %q", first)
	}
	if again := l.notice(rec); again != "" {
		t.Fatalf("told once: %q", again)
	}
	rec.Apps = []string{"acme.cash", "acme.ruled"}
	if waits := l.notice(rec); !strings.Contains(waits, "wait for the agent's next start") || strings.Contains(waits, "Instructions of Кэшбэк.") {
		t.Fatalf("waiting notice: %q", waits)
	}
	rec.Apps = nil
	if off := l.notice(rec); !strings.Contains(off, "Кэшбэк (acme.cash)") || !strings.Contains(off, "turned off") {
		t.Fatalf("off notice: %q", off)
	}

	l.requireWall = true
	if why := l.hostRefusal("s-plain")("acme.cash"); !strings.Contains(why, "RUNTIME_APPS_REQUIRE_WALL") {
		t.Fatalf("a required wall: %q", why)
	}
	if why := l.hostRefusal("s-with")("acme.cash"); why != "" {
		t.Fatalf("a launch with apps has the wall: %q", why)
	}
}
