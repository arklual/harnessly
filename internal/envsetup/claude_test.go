package envsetup

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func pluginItem(id, plugin string) hubproto.EnvItem {
	return hubproto.EnvItem{ItemID: id, Kind: hubproto.EnvKindPlugin, Name: plugin,
		Source: json.RawMessage(`{"marketplace":"anthropics/claude-plugins-official","plugin":"` + plugin + `"}`)}
}

func TestPluginIsInstalledOnceWithItsMarketplace(t *testing.T) {
	f := newFixture(t)
	plugins := filepath.Join(f.home, ".claude", "plugins")
	_ = os.MkdirAll(plugins, 0o700)
	_ = os.WriteFile(filepath.Join(plugins, "installed_plugins.json"), []byte(`{"version":2,"plugins":{"context7@claude-plugins-official":[{}]}}`), 0o600)
	f.setList(pluginItem("p1", "superpowers@claude-plugins-official"), pluginItem("p2", "context7@claude-plugins-official"))
	f.r.Tick(context.Background())
	run := f.onlyRun()
	if s := f.step(run, "p1"); s.Status != hubproto.EnvOK {
		t.Fatalf("p1 = %+v", s)
	}
	if s := f.step(run, "p2"); s.Status != hubproto.EnvSkipped {
		t.Fatalf("p2 in place = %+v", s)
	}
	var got []string
	for _, c := range f.calls {
		if c.Name == "claude" {
			got = append(got, strings.Join(c.Args, " "))
		}
	}
	want := "plugin marketplace add anthropics/claude-plugins-official,plugin install superpowers@claude-plugins-official"
	if strings.Join(got, ",") != want {
		t.Fatalf("claude ran %v", got)
	}
}

func TestPluginWithoutClaudeOrWithAnErrorFailsAlone(t *testing.T) {
	f := newFixture(t)
	f.handle = func(c Cmd) reply {
		if c.Name == "claude" {
			return reply{err: exec.ErrNotFound}
		}
		return reply{}
	}
	f.setList(pluginItem("p1", "superpowers@claude-plugins-official"),
		hubproto.EnvItem{ItemID: "c1", Kind: hubproto.EnvKindClaudeSettings, Name: "Настройки", Source: json.RawMessage(`{"claudeMd":"правило"}`)})
	f.r.Tick(context.Background())
	run := f.onlyRun()
	if s := f.step(run, "p1"); s.Status != hubproto.EnvError || !strings.Contains(s.Message, "нет Claude Code") {
		t.Fatalf("p1 = %+v", s)
	}
	if s := f.step(run, "c1"); s.Status != hubproto.EnvOK {
		t.Fatalf("the other item = %+v", s)
	}
}

func TestClaudeSettingsKeepThePersonsOwn(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.home, ".claude")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"effortLevel":"high","env":{"BASH_MAX_TIMEOUT_MS":"1","MY_OWN":"x"},"theme":"dark"}`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Моё\n\nне трогать\n"), 0o644)
	src := `{"always":{"env":{"CLAUDE_CODE_SUBAGENT_MODEL":"sonnet","BASH_MAX_TIMEOUT_MS":"600000"},"subagentPromptCacheTtl":"1h"},
		"ifAbsent":{"effortLevel":"medium","cleanupPeriodDays":365,"env":{"BASH_DEFAULT_TIMEOUT_MS":"120000"}},"claudeMd":"## Экономия\n\n- сам"}`
	item := hubproto.EnvItem{ItemID: "c1", Kind: hubproto.EnvKindClaudeSettings, Name: "Настройки", Source: json.RawMessage(src)}
	f.setList(item)
	f.r.Tick(context.Background())
	if s := f.step(f.onlyRun(), "c1"); s.Status != hubproto.EnvOK {
		t.Fatalf("c1 = %+v", s)
	}
	var got struct {
		EffortLevel            string            `json:"effortLevel"`
		CleanupPeriodDays      int               `json:"cleanupPeriodDays"`
		SubagentPromptCacheTtl string            `json:"subagentPromptCacheTtl"`
		Theme                  string            `json:"theme"`
		Env                    map[string]string `json:"env"`
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.EffortLevel != "high" || got.CleanupPeriodDays != 365 || got.SubagentPromptCacheTtl != "1h" || got.Theme != "dark" ||
		got.Env["MY_OWN"] != "x" || got.Env["BASH_MAX_TIMEOUT_MS"] != "600000" || got.Env["BASH_DEFAULT_TIMEOUT_MS"] != "120000" ||
		got.Env["CLAUDE_CODE_SUBAGENT_MODEL"] != "sonnet" {
		t.Fatalf("settings = %s", raw)
	}
	md, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if !strings.HasPrefix(string(md), "# Моё\n\nне трогать\n\n<!-- harnessly:c1 -->\n## Экономия") {
		t.Fatalf("CLAUDE.md = %q", md)
	}

	// The block is rewritten in place, the rest stays; a second run with the same text changes nothing.
	_ = os.WriteFile(filepath.Join(dir, "CLAUDE.md"), append(md, []byte("\n# Ниже моё\n")...), 0o644)
	item.Source = json.RawMessage(`{"claudeMd":"## Экономия\n\n- по-новому"}`)
	f.setList(item)
	f.request("update-0001", hubproto.RunUpdate)
	f.r.Tick(context.Background())
	md2, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if strings.Count(string(md2), "<!-- harnessly:c1 -->") != 1 || !strings.Contains(string(md2), "по-новому") || strings.Contains(string(md2), "- сам") ||
		!strings.HasSuffix(string(md2), "# Ниже моё\n") {
		t.Fatalf("CLAUDE.md again = %q", md2)
	}
	f.request("update-0002", hubproto.RunUpdate)
	f.r.Tick(context.Background())
	if s := f.step(f.runFile("update-0002"), "c1"); s.Status != hubproto.EnvSkipped {
		t.Fatalf("nothing to do = %+v", s)
	}
}

func TestClaudeSettingsRefuseWhatTheListDoesNotSet(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.home, ".claude")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`not json`), 0o600)
	f.setList(
		hubproto.EnvItem{ItemID: "c1", Kind: hubproto.EnvKindClaudeSettings, Name: "Плохой файл", Source: json.RawMessage(`{"always":{"model":"sonnet"}}`)},
	)
	f.r.Tick(context.Background())
	if s := f.step(f.onlyRun(), "c1"); s.Status != hubproto.EnvError || !strings.Contains(s.Message, "не трогаю") {
		t.Fatalf("broken settings.json = %+v", s)
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, "settings.json")); string(raw) != "not json" {
		t.Fatalf("the file was changed: %q", raw)
	}
	_ = os.Remove(filepath.Join(dir, "settings.json"))
	for _, src := range []string{`{"always":{"hooks":{}}}`, `{"always":{"env":{"ANTHROPIC_BASE_URL":"https://x"}}}`} {
		if _, err := mergeSettings(map[string]json.RawMessage{}, parse(t, src)["always"], nil); err == nil {
			t.Errorf("%s was let through", src)
		}
	}
}

func parse(t *testing.T, s string) map[string]map[string]json.RawMessage {
	t.Helper()
	var m map[string]map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCheckRunOfClaudeKindsWritesNothing(t *testing.T) {
	f := newFixture(t)
	items := []hubproto.EnvItem{pluginItem("p1", "superpowers@claude-plugins-official"),
		{ItemID: "c1", Kind: hubproto.EnvKindClaudeSettings, Name: "Н", Source: json.RawMessage(`{"claudeMd":"x"}`)}}
	f.setList(items...)
	_ = f.r.writeState(hubproto.EnvSetupState{Hash: hubproto.EnvHash(items), Items: []hubproto.EnvApplied{}, Once: map[string]string{}})
	f.request("check-0001", hubproto.RunCheck)
	f.r.Tick(context.Background())
	run := f.runFile("check-0001")
	if f.step(run, "p1").Status != hubproto.EnvPending || f.step(run, "c1").Status != hubproto.EnvPending {
		t.Fatalf("%+v", run.Steps)
	}
	if _, err := os.Stat(filepath.Join(f.home, ".claude", "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatal("a check wrote CLAUDE.md")
	}
	for _, c := range f.calls {
		if c.Name == "claude" {
			t.Fatalf("a check ran claude %v", c.Args)
		}
	}
}
