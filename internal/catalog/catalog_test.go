package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// The expectations below are the previous runtime's texts, which were in Russian.
	os.Setenv("RUNTIME_LOCALE", "ru")
	os.Exit(m.Run())
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func encode(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func TestParseFrontmatter(t *testing.T) {
	cases := []struct {
		in   string
		data map[string]string
		body string
	}{
		{"no head\nline", map[string]string{}, "no head\nline"},
		{"---\nname: x\ndescription: \"say \\\"hi\\\"\"\n---\nbody", map[string]string{"name": "x", "description": `say "hi"`}, "body"},
		{"\uFEFF---\r\ndescription: 'it''s'\r\n---\r\n# h\r\ntext", map[string]string{"description": "it's"}, "# h\ntext"},
		{"---\ndescription: |\n  one\n\n  two\nname: n\n---\n", map[string]string{"description": "one\n\ntwo", "name": "n"}, ""},
		{"---\ndescription: >-\n  one\n  two\n---", map[string]string{"description": "one two"}, ""},
		{"---\nkey:value\nempty:\n  nested: 1\ndescription: a\rb\n", map[string]string{}, ""},
		{"---\ndescription: first\ndescription:\n", map[string]string{"description": "first"}, ""},
	}
	for _, c := range cases {
		data, body := parseFrontmatter(c.in)
		if !reflect.DeepEqual(data, c.data) || body != c.body {
			t.Errorf("%q: %q %q, want %q %q", c.in, data, body, c.data, c.body)
		}
	}
	if got := firstBodyLine("\n# title\n\n  the line \nnext"); got != "the line" {
		t.Errorf("firstBodyLine: %q", got)
	}
}

func TestTextRules(t *testing.T) {
	long := strings.Repeat("я", 300)
	if got := clipDescription(long); utf16Len(got) != 240 || !strings.HasSuffix(got, "…") {
		t.Errorf("clip: %d", utf16Len(got))
	}
	if got := clipDescription("  a \n\t b\u00a0c  "); got != "a b c" {
		t.Errorf("collapse: %q", got)
	}
	if got := decodeUTF8([]byte("ok\xe2\x82")); got != "ok\uFFFD" {
		t.Errorf("cut character: %q", got)
	}
	if got := decodeUTF8([]byte("a\xffb")); got != "a\uFFFDb" {
		t.Errorf("bad byte: %q", got)
	}
	for _, c := range []struct {
		root, target string
		in           bool
	}{{"/a", "/a", true}, {"/a", "/a/b", true}, {"/a", "/ab", false}, {"/a", "/a/..b", false}, {"/a/b", "/a", false}} {
		if isInside(c.root, c.target) != c.in {
			t.Errorf("isInside(%s, %s) != %v", c.root, c.target, c.in)
		}
	}
	// The previous runtime's order (localeCompare, en-US).
	names := []string{"Zeta", "x2", "foo-bar", "README", "x10", "é", "alpha", "foo_bar", "ab", "commit", "x1", "a.b", "e", "f"}
	want := []string{"a.b", "ab", "alpha", "commit", "e", "é", "f", "foo_bar", "foo-bar", "README", "x1", "x10", "x2", "Zeta"}
	col := newCollator()
	sort.SliceStable(names, func(i, j int) bool { return col.CompareString(names[i], names[j]) < 0 })
	if !slices.Equal(names, want) {
		t.Errorf("order %q", names)
	}
}

func TestQuickPrompts(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "q.json")
	names := func() []string {
		var out []string
		for _, q := range quickPrompts(file) {
			out = append(out, q["prompt"].(string)+"="+map[bool]string{true: "on", false: "off"}[truthy(q["enabled"])])
		}
		return out
	}
	if got := names(); !slices.Equal(got, []string{"/commit-commands:commit=on", "/simplify=on"}) {
		t.Errorf("missing file: %q", got)
	}
	write(t, file, `{"custom":[{"id":"c","name":"Go","prompt":"/go","enabled":true}],"disabledBuiltinIds":["builtin-simplify"]}`)
	if got := names(); !slices.Equal(got, []string{"/commit-commands:commit=on", "/simplify=off", "/go=on"}) {
		t.Errorf("file: %q", got)
	}
	write(t, file, `[{"id":"builtin-commit","enabled":false},{"id":"x","prompt":"/x","enabled":true}]`)
	if got := names(); !slices.Equal(got, []string{"/commit-commands:commit=off", "/simplify=on", "/x=on"}) {
		t.Errorf("flat array: %q", got)
	}
	write(t, file, `{"custom":{"a":1},"disabledBuiltinIds":["builtin-commit"]}`)
	if got := names(); !slices.Equal(got, []string{"/commit-commands:commit=on", "/simplify=on"}) {
		t.Errorf("broken shape: %q", got)
	}
}

func TestWorkspaceDirs(t *testing.T) {
	file := filepath.Join(t.TempDir(), "w.json")
	write(t, file, `{"workspaces":[{"directories":["/a","/b"]},{"directory":"/c"},5,{"directories":"/d"}]}`)
	if got := workspaceDirs(file); !slices.Equal(got, []string{"/a", "/b", "/c", "/d"}) {
		t.Errorf("dirs %q", got)
	}
	write(t, file, `{"workspaces":[{"directories":["/a"]},null]}`)
	if got := workspaceDirs(file); got != nil {
		t.Errorf("null entry: %q", got)
	}
}

func itemNames(items []Item) []string {
	var out []string
	for _, it := range items {
		if it.Source != "builtin" {
			out = append(out, it.Trigger+it.Name+"@"+it.Source)
		}
	}
	return out
}

func TestScanClaude(t *testing.T) {
	base := t.TempDir()
	claude := filepath.Join(base, "claude")
	root := filepath.Join(base, "ws")
	cwd := filepath.Join(root, "repo")
	write(t, filepath.Join(cwd, ".claude/commands/deploy.md"), "---\ndescription: Ship it\nargument-hint: <env>\n---\n")
	write(t, filepath.Join(cwd, ".claude/commands/sub/run.md"), "Run the thing\n")
	write(t, filepath.Join(cwd, ".claude/commands/hidden.md"), "---\nuser-invocable: false\n---\n")
	write(t, filepath.Join(root, ".claude/commands/deploy.md"), "shadowed by the nearer folder")
	write(t, filepath.Join(root, ".claude/commands/top.md"), "from the top")
	write(t, filepath.Join(claude, "commands/mine.md"), "mine")
	write(t, filepath.Join(claude, "commands/Compact.md"), "a built-in name wins")
	write(t, filepath.Join(claude, "skills/writer/SKILL.md"), "---\nname: write\ndescription: Writes\n---\n")
	write(t, filepath.Join(base, "outside.md"), "escapes")
	if err := os.Symlink(filepath.Join(base, "outside.md"), filepath.Join(claude, "commands/link.md")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(base, "plugins/tools/.claude-plugin/plugin.json"), `{"name":"tools"}`)
	write(t, filepath.Join(base, "plugins/tools/commands/fix.md"), "Fix")
	write(t, filepath.Join(base, "plugins/tools/skills/check/SKILL.md"), "Check")
	write(t, filepath.Join(base, "plugins/off/commands/no.md"), "No")
	write(t, filepath.Join(claude, "plugins/installed_plugins.json"), `{"plugins":{
		"tools@m":[{"scope":"user","installPath":"`+filepath.Join(base, "plugins/tools")+`","lastUpdated":"2026-01-01"}],
		"off@m":[{"scope":"user","installPath":"`+filepath.Join(base, "plugins/off")+`"}]}}`)
	write(t, filepath.Join(claude, "settings.json"), `{"enabledPlugins":{"tools@m":true,"off@m":true}}`)
	write(t, filepath.Join(cwd, ".claude/settings.local.json"), `{"enabledPlugins":{"off@m":false}}`)

	prompts := []map[string]any{
		{"name": "Commit", "prompt": "/commit", "enabled": true},
		{"name": "Off", "prompt": "/off", "enabled": false},
		{"name": "Text", "prompt": "not a command", "enabled": true},
	}
	b := scanClaude(claude, cwd, root, prompts)
	want := []string{"/commit@quick", "/deploy@project", "/sub:run@project", "/top@project", "/mine@user", "/write@skill", "/tools:fix@plugin", "/tools:check@plugin"}
	if got := itemNames(b.items); !slices.Equal(got, want) {
		t.Errorf("items\n got %q\nwant %q", got, want)
	}
	for _, it := range b.items {
		switch it.Name {
		case "deploy":
			if it.Description != "Ship it" || it.ArgumentHint == nil || *it.ArgumentHint != "<env>" || it.ID != "project::deploy" {
				t.Errorf("deploy: %+v", it)
			}
		case "tools:fix":
			if it.SourceLabel != "плагин · tools" || it.Plugin == nil || *it.Plugin != "tools" || it.ID != "plugin:tools:tools:fix" {
				t.Errorf("plugin item: %+v", it)
			}
		}
	}
	if len(b.warnings) != 0 {
		t.Errorf("warnings %q", b.warnings)
	}
	write(t, filepath.Join(claude, "plugins/installed_plugins.json"), "{broken")
	if b := scanClaude(claude, cwd, root, []map[string]any{{"prompt": 5}}); !slices.Equal(b.warnings, []string{quickPromptsWarning.String(), claudePluginsWarning.String()}) {
		t.Errorf("warnings %q", b.warnings)
	}
}

func TestScanCodex(t *testing.T) {
	codex := t.TempDir()
	write(t, filepath.Join(codex, "prompts/review.md"), "---\ndescription: Review\n---\n")
	write(t, filepath.Join(codex, "prompts/nested/skip.md"), "skip")
	write(t, filepath.Join(codex, "skills/plan/SKILL.md"), "Plan")
	write(t, filepath.Join(codex, "skills/.system/creator/SKILL.md"), "---\nname: skill-creator\n---\nCreate")
	write(t, filepath.Join(codex, "plugins/cache/mk/kit/1.0/skills/old/SKILL.md"), "old")
	write(t, filepath.Join(codex, "plugins/cache/mk/kit/2.0/skills/lint/SKILL.md"), "Lint")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(codex, "plugins/cache/mk/kit/1.0"), old, old); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(codex, "config.toml"), "model = \"gpt-5\"\n[plugins.\"kit@mk\"]\nenabled = true\n[plugins.\"gone@mk\"]\nenabled = false\n")
	b := scanCodex(codex)
	want := []string{"/prompts:review@user", "$plan@skill", "$skill-creator@skill", "$kit:lint@plugin"}
	if got := itemNames(b.items); !slices.Equal(got, want) {
		t.Errorf("items\n got %q\nwant %q", got, want)
	}
	write(t, filepath.Join(codex, "config.toml"), "[plugins\n")
	if b := scanCodex(codex); !slices.Equal(b.warnings, []string{codexConfigWarning.String()}) {
		t.Errorf("warnings %q", b.warnings)
	}
}

func TestResolveCwdAndCache(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	extra := filepath.Join(base, "extra")
	for _, d := range []string{filepath.Join(root, "a"), filepath.Join(extra, "b"), filepath.Join(base, "else")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(base, "else"), filepath.Join(root, "esc")); err != nil {
		t.Fatal(err)
	}
	ws := filepath.Join(base, "workspaces.json")
	write(t, ws, `{"workspaces":[{"directories":["`+extra+`"]}]}`)
	c := New(Config{ClaudeDir: filepath.Join(base, "c"), CodexDir: filepath.Join(base, "x"), WorkspaceRoot: root,
		QuickPromptsFile: filepath.Join(base, "q.json"), WorkspacesFile: ws}, encode)
	for _, tc := range []struct{ in, dir, root, err string }{
		{"", root, root, ""},
		{filepath.Join(root, "a") + "/", filepath.Join(root, "a"), root, ""},
		{filepath.Join(extra, "b"), filepath.Join(extra, "b"), extra, ""},
		{filepath.Join(root, "esc"), "", "", ErrOutside.String()},
		{filepath.Join(root, "missing"), "", "", ErrNotFound.String()},
		{"relative/path", "", "", ErrOutside.String()},
	} {
		dir, r, e := c.ResolveCwd(tc.in)
		if dir != tc.dir || r != tc.root || e != tc.err {
			t.Errorf("%q: %q %q %q", tc.in, dir, r, e)
		}
	}
	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }
	first := c.Get("claude", root, root)
	now = now.Add(30 * time.Second)
	if again := c.Get("claude", root, root); again.Catalog.GeneratedAt != first.Catalog.GeneratedAt {
		t.Error("a catalog within a minute is rebuilt")
	}
	write(t, filepath.Join(root, ".claude/commands/fresh.md"), "Fresh")
	again := c.Get("claude", root, root)
	if again.Catalog.GeneratedAt == first.Catalog.GeneratedAt || again.Catalog.Counts.Project != 1 || again.Tag == first.Tag {
		t.Errorf("a new command did not rebuild the catalog: %+v %d %d %q %q", again.Catalog.Counts, again.Catalog.GeneratedAt, first.Catalog.GeneratedAt, again.Tag, first.Tag)
	}
	if first.Catalog.Counts.Builtin != len(claudeBuiltins) || first.Catalog.Counts.Quick != 2 {
		t.Errorf("counts %+v", first.Catalog.Counts)
	}
	var body map[string]any
	if json.Unmarshal(first.Body, &body) != nil || body["provider"] != "claude" || body["truncated"] != false {
		t.Errorf("body %s", first.Body)
	}
}

func TestLaunch(t *testing.T) {
	base := t.TempDir()
	cfg := LaunchConfig{ClaudeDir: filepath.Join(base, "c"), CodexDir: filepath.Join(base, "x"), RuntimeConfig: filepath.Join(base, "config.json"), DefaultsKey: "defaults"}
	got := cfg.Launch("claude")
	if len(got.Models) != 4 || got.Saved != nil || len(got.Warnings) != 0 || got.CliDefault.Model != nil {
		t.Errorf("empty claude: %+v", got)
	}
	write(t, filepath.Join(base, "c/settings.json"), `{"model":" opus ","effortLevel":"weird"}`)
	write(t, cfg.RuntimeConfig, `{"defaults":{"claude":{"model":"sonnet","effort":""}}}`)
	got = cfg.Launch("claude")
	if *got.CliDefault.Model != "opus" || got.CliDefault.Effort != nil || *got.Saved.Model != "sonnet" || got.Saved.Effort != nil {
		t.Errorf("claude: %+v", got)
	}
	write(t, filepath.Join(base, "c/settings.json"), `{`)
	if got = cfg.Launch("claude"); len(got.Warnings) != 1 {
		t.Errorf("broken settings: %q", got.Warnings)
	}

	got = cfg.Launch("codex")
	if !slices.Equal(got.Warnings, []string{"~/.codex/models_cache.json не найден: Codex создаёт его при первом запуске"}) || got.Models == nil {
		t.Errorf("empty codex: %+v", got)
	}
	write(t, filepath.Join(base, "x/models_cache.json"), `{"fetched_at":"2026-10-01T00:00:00Z","models":[
		{"slug":"gpt-5","display_name":"GPT-5","visibility":"list","default_reasoning_level":"high","supported_reasoning_levels":[{"effort":"low"},{"effort":"high"},{"effort":"bogus"}]},
		{"slug":"gpt-5","visibility":"list"},
		{"slug":"hidden","visibility":"hide"},
		{"slug":"Bad Slug","visibility":"list"}]}`)
	write(t, filepath.Join(base, "x/config.toml"), "model = \"gpt-5\"\nmodel_reasoning_effort = \"high\"\n")
	got = cfg.Launch("codex")
	want := []Model{{ID: "gpt-5", Label: "GPT-5", Hint: "gpt-5 · по умолчанию high", Efforts: []string{"low", "high"}, DefaultEffort: strPtr("high")}}
	if !reflect.DeepEqual(got.Models, want) || *got.FetchedAt != "2026-10-01T00:00:00Z" || *got.CliDefault.Effort != "high" || len(got.Warnings) != 0 {
		t.Errorf("codex: %+v", got)
	}
	write(t, filepath.Join(base, "x/models_cache.json"), `{"models":[]}`)
	write(t, filepath.Join(base, "x/config.toml"), "model = ")
	got = cfg.Launch("codex")
	if !slices.Equal(got.Warnings, []string{"В ~/.codex/models_cache.json нет доступных моделей", "~/.codex/config.toml не читается — значение «Авто» неизвестно"}) {
		t.Errorf("codex warnings: %q", got.Warnings)
	}
	raw, _ := json.Marshal(got)
	if !strings.Contains(string(raw), `"saved":null`) || !strings.HasPrefix(string(raw), `{"provider":"codex","source":"models-cache","fetchedAt":null,"models":[]`) {
		t.Errorf("shape %s", raw)
	}
}

func TestBuiltinLookup(t *testing.T) {
	if it, ok := Builtin("claude", "COST"); !ok || it.Name != "usage" || it.Execution != "menu" {
		t.Errorf("alias in another case: %+v %v", it, ok)
	}
	if it, ok := Builtin("claude", "logout"); !ok || it.Execution != "blocked" || it.Note == nil {
		t.Errorf("blocked: %+v", it)
	}
	if it, ok := Builtin("codex", "model"); !ok || it.Execution != "menu" {
		t.Errorf("codex model: %+v", it)
	}
	if _, ok := Builtin("claude", "tracker:task"); ok {
		t.Error("a plugin command is a built-in")
	}
	if _, ok := Builtin("gpt", "model"); ok {
		t.Error("an unknown CLI has built-ins")
	}
	if !IsCommandName("tracker:task") || IsCommandName("-x") || IsCommandName("") {
		t.Error("command names")
	}
}

func TestInvalidate(t *testing.T) {
	c := New(Config{}, func(v any) []byte { return []byte("{}") })
	c.cache["claude\x00/a"] = cached{}
	c.cache["codex\x00/a"] = cached{}
	c.Invalidate("claude")
	if _, ok := c.cache["claude\x00/a"]; ok || len(c.cache) != 1 {
		t.Errorf("cache %v", c.cache)
	}
}
