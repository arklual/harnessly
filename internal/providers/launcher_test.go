package providers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/claude"
	"github.com/ly-solutions/harnessly/internal/agents/codex"
)

func claudeCfg(t *testing.T, st *Store, withProfiles bool) claude.LaunchConfig {
	t.Helper()
	hooks := filepath.Join(t.TempDir(), "hooks.json")
	if err := os.WriteFile(hooks, []byte(`{"hooks":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := claude.LaunchConfig{HooksPath: hooks, JoinDir: filepath.Join(t.TempDir(), "j")}
	if withProfiles {
		c.Profiles = Launcher{Store: st}
	}
	return c
}

func codexCfg(t *testing.T, st *Store, withProfiles bool) codex.LaunchConfig {
	t.Helper()
	c := codex.LaunchConfig{LauncherScript: filepath.Join(t.TempDir(), "launcher.js"), LauncherContent: []byte("x"),
		PassServiceEnv: true, ShellEnvExclude: []string{"RT_TOKEN"},
		Hooks: &codex.HookConfig{Script: "/data/codex-hook.sh", UserConfig: filepath.Join(t.TempDir(), "none.toml")},
		MCP:   codex.MCP{Server: "rt-preview", URL: codex.MCPURL(8022, "/api/cli/preview/mcp"), TokenHeader: "x-rt-token", TokenEnv: "RT_PREVIEW_TOKEN"}}
	if withProfiles {
		c.Profiles = Launcher{Store: st}
	}
	return c
}

func choose(model, profile string) *agents.Launch {
	l := &agents.Launch{Model: &model, Profile: profile}
	return l
}

// A chat without a profile starts exactly as it did before profiles: the same line and flags whether the deployment
// has profiles configured or not.
func TestNoProfileLaunchUnchanged(t *testing.T) {
	st := newStore(t)
	if _, err := st.Set("xai", fakeKey); err != nil {
		t.Fatal(err)
	}
	cc, cp := claudeCfg(t, st, true), claudeCfg(t, st, false)
	cp.HooksPath, cp.JoinDir = cc.HooksPath, cc.JoinDir
	for _, l := range []*agents.Launch{nil, choose("opus", ""), choose("sonnet", "")} {
		with, err1 := cc.LaunchCommand("ws", l, nil)
		without, err2 := cp.LaunchCommand("ws", l, nil)
		if err1 != nil || err2 != nil || with != without || strings.Contains(with, "providers") {
			t.Errorf("claude %v: %q / %q %v %v", l, with, without, err1, err2)
		}
		rw, err1 := cc.ResumeCommand("11111111-2222-4333-8444-555555555555", "ws", l)
		ro, err2 := cp.ResumeCommand("11111111-2222-4333-8444-555555555555", "ws", l)
		if err1 != nil || err2 != nil || rw != ro {
			t.Errorf("claude resume %v: %q / %q", l, rw, ro)
		}
	}
	xc, xp := codexCfg(t, st, true), codexCfg(t, st, false)
	xp.LauncherScript = xc.LauncherScript
	for _, l := range []*agents.Launch{nil, choose("gpt-5.4", "")} {
		with, err1 := xc.LauncherLine("ws", "", l)
		without, err2 := xp.LauncherLine("ws", "", l)
		if err1 != nil || err2 != nil || with != without || strings.Contains(with, "XAI_API_KEY") {
			t.Errorf("codex line %v: %q / %q", l, with, without)
		}
		aw, _, err1 := xc.RuntimeArgs("ws", "", l)
		ao, _, err2 := xp.RuntimeArgs("ws", "", l)
		if err1 != nil || err2 != nil || strings.Join(aw, "\x00") != strings.Join(ao, "\x00") {
			t.Errorf("codex args %v: %q / %q", l, aw, ao)
		}
		if strings.Contains(strings.Join(aw, " "), "model_provider") || strings.Contains(strings.Join(aw, " "), "XAI_API_KEY") {
			t.Errorf("codex args carry a profile: %q", aw)
		}
	}
}

func TestClaudeProfileLaunchUsesProfileSettings(t *testing.T) {
	st := newStore(t)
	if _, err := st.Set("glm", fakeKey); err != nil {
		t.Fatal(err)
	}
	c := claudeCfg(t, st, true)
	p, _ := Find("glm")
	line, err := c.LaunchCommand("ws", choose(p.Models[0].ID, "glm"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "--settings") || !strings.Contains(line, "glm."+"") || strings.Contains(line, fakeKey) {
		t.Fatalf("%q", line)
	}
	if strings.Contains(line, c.HooksPath+"'") && !strings.Contains(line, "settings.json") {
		t.Fatalf("hooks file used as is: %q", line)
	}
	if _, err := c.LaunchCommand("ws", choose("not-a-model", "glm"), nil); err == nil {
		t.Fatal("an unknown model of the profile")
	}
	none := claudeCfg(t, st, false)
	if _, err := none.LaunchCommand("ws", choose(p.Models[0].ID, "glm"), nil); err != claude.ErrNoProfiles {
		t.Fatalf("%v", err)
	}
	eff := "high"
	l := choose(p.Models[0].ID, "glm")
	l.Effort = &eff
	if _, err := c.LaunchCommand("ws", l, nil); err == nil {
		t.Fatal("a profile takes no effort")
	}
}

func TestCodexProfileLaunchLine(t *testing.T) {
	st := newStore(t)
	if _, err := st.Set("xai", fakeKey); err != nil {
		t.Fatal(err)
	}
	p, _ := Find("xai")
	c := codexCfg(t, st, true)
	l := choose(p.Models[0].ID, "xai")
	line, err := c.LauncherLine("ws", "", l)
	if err != nil {
		t.Fatal(err)
	}
	want := p.KeyEnv + `="$(cat '` + st.KeyFile("xai") + `')" node `
	if !strings.HasPrefix(line, want) || !strings.Contains(line, "'xai/"+p.Models[0].ID+"'") || strings.Contains(line, fakeKey) {
		t.Fatalf("%q", line)
	}
	args, _, err := c.RuntimeArgs("ws", "", l)
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(args, " ")
	for _, w := range []string{"model_provider=\"xai\"", "model_providers.xai.base_url=", "env_key=\"" + p.KeyEnv + "\"", `wire_api="responses"`, p.KeyEnv} {
		if !strings.Contains(all, w) {
			t.Errorf("no %s in %q", w, all)
		}
	}
	if strings.Contains(all, fakeKey) {
		t.Fatal("the key is in the arguments")
	}
	if _, err := codexCfg(t, st, false).LauncherLine("ws", "", l); err != codex.ErrNoProfiles {
		t.Fatalf("%v", err)
	}
	if _, err := c.LauncherLine("ws", "", choose(p.Models[0].ID, "kimi")); err == nil {
		t.Fatal("kimi does not run through Codex")
	}
}

func TestLauncherModelRoundTrip(t *testing.T) {
	for _, c := range []struct{ profile, model string }{{"", "gpt-5.4"}, {"xai", "grok-build-0.1"}, {"", "a/b"}} {
		m := c.model
		pr, md := agents.SplitLauncherModel(agents.LauncherModel(&agents.Launch{Model: &m, Profile: c.profile}))
		if c.profile != "" && (pr != c.profile || md != c.model) {
			t.Errorf("%v: %q %q", c, pr, md)
		}
	}
	if pr, md := agents.SplitLauncherModel("gpt-5.4"); pr != "" || md != "gpt-5.4" {
		t.Fatal(pr, md)
	}
}

func TestLauncherLookup(t *testing.T) {
	st := newStore(t)
	l := Launcher{Store: st}
	via, conf, models, ok := l.Lookup("kimi")
	if !ok || conf || via != "claude" || len(models) == 0 {
		t.Fatal(via, conf, models, ok)
	}
	_, _ = st.Set("kimi", fakeKey)
	if _, conf, _, _ = l.Lookup("kimi"); !conf {
		t.Fatal("configured")
	}
	if _, _, _, ok := l.Lookup("nope"); ok {
		t.Fatal("unknown")
	}
	if !l.OwnsModel(models[0]) || l.OwnsModel("opus") || l.OwnsModel("gpt-5.4") {
		t.Fatal("owns")
	}
}
