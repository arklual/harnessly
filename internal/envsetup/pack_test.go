package envsetup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const packManifest = `{"schema":1,"id":"acme","name":"Acme","version":"1.0.0",
 "tools":[{"name":"acme-cli","path":"tools/cli.sh"}],
 "hooks":[{"id":"guard","event":"PreToolUse","matcher":"Bash","command":"python3 ${PACK}/hooks/guard.py"}],
 "mcp":"mcp.json","skills":"skills",
 "safe":{"secrets":[{"name":"acme-token","why":"доступ к Acme"},{"name":"acme-extra","why":"по желанию","optional":true}],"never":["acme-token"]}}`

func writePackRepo(t *testing.T, dir, manifest string) {
	t.Helper()
	files := map[string]string{
		"packs/acme/harnessly/pack.json":  manifest,
		"packs/acme/tools/cli.sh":         "#!/bin/sh\necho acme\n",
		"packs/acme/hooks/guard.py":       "print('ok')\n",
		"packs/acme/mcp.json":             `{"mcpServers":{"acme":{"command":"acme-cli"}}}`,
		"packs/acme/skills/acme/SKILL.md": "---\nname: acme\ndescription: acme\n---\n",
	}
	for p, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func packFixture(t *testing.T, manifest *string) *fixture {
	f := newFixture(t)
	f.handle = func(c Cmd) reply {
		if c.Name == "git" && len(c.Args) > 0 && c.Args[0] == "clone" {
			writePackRepo(t, c.Args[len(c.Args)-1], *manifest)
		}
		if c.Name == "git" && len(c.Args) > 3 && c.Args[0] == "-C" && c.Args[2] == "pull" {
			writePackRepo(t, c.Args[1], *manifest)
		}
		return reply{}
	}
	return f
}

var packItem = hubproto.EnvItem{ItemID: "p1", Kind: hubproto.EnvKindPack, Name: "Acme", Layer: "org",
	Source: json.RawMessage(`{"repoUrl":"https://example.com/acme/env.git","branch":"main","path":"packs/acme"}`)}

func TestAPackIsInstalledAsAPluginAndAsksForItsSecrets(t *testing.T) {
	manifest := packManifest
	f := packFixture(t, &manifest)
	f.setList(packItem)
	f.r.Tick(context.Background())
	s := f.step(f.onlyRun(), "p1")
	// The required secret is not in the safe: the pack stays installed, the step waits for the person.
	if s.Status != hubproto.EnvPending || !strings.Contains(s.Message, "acme 1.0.0") || !strings.Contains(s.Message, "acme-token") {
		t.Fatalf("%+v", s)
	}
	if len(f.safe.asked) != 1 || !strings.HasPrefix(f.safe.asked[0], "acme-token|") {
		t.Fatalf("asked: %v", f.safe.asked)
	}
	dest := filepath.Join(f.r.LinkDir, "environment", "packs", "acme")
	var pj map[string]string
	raw, err := os.ReadFile(filepath.Join(dest, ".claude-plugin", "plugin.json"))
	if err != nil || json.Unmarshal(raw, &pj) != nil || pj["name"] != "pack-acme" || pj["version"] != "1.0.0" {
		t.Fatalf("plugin.json: %s %v", raw, err)
	}
	fi, err := os.Stat(filepath.Join(dest, "bin", "acme-cli"))
	if err != nil || fi.Mode().Perm()&0o100 == 0 {
		t.Fatalf("bin: %v %v", fi, err)
	}
	hooks, _ := os.ReadFile(filepath.Join(dest, "hooks", "hooks.json"))
	if !strings.Contains(string(hooks), "${CLAUDE_PLUGIN_ROOT}/hooks/guard.py") || strings.Contains(string(hooks), "${PACK}") {
		t.Fatalf("hooks.json: %s", hooks)
	}
	for _, p := range []string{".mcp.json", "skills/acme/SKILL.md", hubproto.PackSafeFile, hubproto.PackMarkFile} {
		if _, err := os.Stat(filepath.Join(dest, filepath.FromSlash(p))); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
	}

	// The secret arrives: the same pack is «уже стоит», nothing is written again.
	f.safe.put("acme-token", "x")
	before, _ := os.Stat(filepath.Join(dest, hubproto.PackMarkFile))
	f.request("again-run-01", hubproto.RunUpdate)
	f.r.Tick(context.Background())
	s = f.step(f.runFile("again-run-01"), "p1")
	if s.Status != hubproto.EnvSkipped || s.Message != "уже стоит: acme 1.0.0" {
		t.Fatalf("again: %+v", s)
	}
	after, _ := os.Stat(filepath.Join(dest, hubproto.PackMarkFile))
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("the pack was written again")
	}

	// A new version of the pack in the repository replaces the plugin.
	manifest = strings.Replace(packManifest, `"version":"1.0.0"`, `"version":"1.1.0"`, 1)
	f.request("newer-run-01", hubproto.RunUpdate)
	f.r.Tick(context.Background())
	s = f.step(f.runFile("newer-run-01"), "p1")
	if s.Status != hubproto.EnvOK || s.Message != "поставлен: acme 1.1.0" {
		t.Fatalf("newer: %+v", s)
	}
	raw, _ = os.ReadFile(filepath.Join(dest, ".claude-plugin", "plugin.json"))
	if !strings.Contains(string(raw), "1.1.0") {
		t.Fatalf("plugin.json: %s", raw)
	}

	// The item leaves the list: the pack is taken off after a full run; a folder that is not a pack stays.
	other := filepath.Join(f.r.LinkDir, "environment", "packs", "mine")
	_ = os.MkdirAll(other, 0o755)
	f.setList()
	f.request("empty-run-01", hubproto.RunUpdate)
	f.r.Tick(context.Background())
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("the pack stayed: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("a foreign folder was removed: %v", err)
	}
}

func TestAPackWithABadManifestFailsAndInstallsNothing(t *testing.T) {
	manifest := strings.Replace(packManifest, `"path":"tools/cli.sh"`, `"path":"tools/missing.sh"`, 1)
	f := packFixture(t, &manifest)
	f.setList(packItem)
	f.r.Tick(context.Background())
	s := f.step(f.onlyRun(), "p1")
	if s.Status != hubproto.EnvError || !strings.Contains(s.Message, "tools/missing.sh") {
		t.Fatalf("%+v", s)
	}
	if _, err := os.Stat(filepath.Join(f.r.LinkDir, "environment", "packs", "acme")); !os.IsNotExist(err) {
		t.Fatalf("installed: %v", err)
	}
}

func TestTwoPacksWithOneIDAreRefused(t *testing.T) {
	manifest := packManifest
	f := packFixture(t, &manifest)
	second := packItem
	second.ItemID = "p2"
	f.setList(packItem, second)
	f.r.Tick(context.Background())
	run := f.onlyRun()
	ok, bad := 0, 0
	for _, id := range []string{"p1", "p2"} {
		if s := f.step(run, id); s.Status == hubproto.EnvError {
			bad++
		} else {
			ok++
		}
	}
	if ok != 1 || bad != 1 {
		t.Fatalf("ok=%d bad=%d", ok, bad)
	}
}
