package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func envItems() []hubproto.EnvItem {
	items := []hubproto.EnvItem{
		{ItemID: "skill1", Kind: "skill", Name: "Ревью кода", Layer: "org", Pinned: true, Source: json.RawMessage(`{"body":"Пиши коротко."}`)},
		{ItemID: "skill2", Kind: "skill", Name: "По ссылке", Layer: "tenant", Source: json.RawMessage(`{"ref":"https://example.org/skill"}`)},
		{ItemID: "mcp1", Kind: "mcp", Name: "Трекер", Layer: "tenant", Source: json.RawMessage(`{"transport":"http","url":"https://tracker.example.org/mcp","secretRefs":["TRACKER_TOKEN"]}`)},
		{ItemID: "repo1", Kind: "repo", Name: "Монорепо", Layer: "team", Source: json.RawMessage(`{"url":"https://git.example.org/acme/mono.git"}`)},
		{ItemID: "doc1", Kind: "doc", Name: "Регламент", Layer: "team", Source: json.RawMessage(`{"url":"https://docs.example.org/rules"}`)},
		{ItemID: "../evil", Kind: "skill", Name: "Плохой", Layer: "team", Source: json.RawMessage(`{"body":"x"}`)},
		{ItemID: "odd1", Kind: "widget", Name: "Странный", Layer: "team", Source: json.RawMessage(`{}`)},
	}
	hubproto.SortEnvItems(items)
	return items
}

func TestEnvKeeperKeepsAppliesAndReports(t *testing.T) {
	dir := t.TempDir()
	k := newEnvKeeper(dir)
	if rev, hash := k.Held(); rev != 0 || hash != "" {
		t.Fatalf("held before any list: %d %q", rev, hash)
	}
	items := envItems()
	set := hubproto.EnvSet{Rev: 3, Hash: hubproto.EnvHash(items), Items: items}
	st, err := k.Set(set)
	if err != nil {
		t.Fatal(err)
	}
	if st.Rev != 3 || st.Hash != set.Hash || len(st.Applied) != len(items) {
		t.Fatalf("state = %+v", st)
	}
	by := map[string]hubproto.EnvApplied{}
	for _, a := range st.Applied {
		by[a.ItemID] = a
	}
	// Every valid kind is applied; the MCP server waits for its secret, the bad ones say why.
	for id, want := range map[string]string{"skill1": hubproto.EnvOK, "skill2": hubproto.EnvOK, "mcp1": hubproto.EnvPending, "repo1": hubproto.EnvPending,
		"doc1": hubproto.EnvOK, "../evil": hubproto.EnvError, "odd1": hubproto.EnvError} {
		if by[id].Status != want || (want != hubproto.EnvOK && by[id].Message == "") {
			t.Errorf("%s = %+v, want %s", id, by[id], want)
		}
	}
	if !strings.Contains(by["mcp1"].Message, "TRACKER_TOKEN") {
		t.Errorf("the waiting server does not name its secret: %q", by["mcp1"].Message)
	}
	plugin := filepath.Join(dir, "environment", "claude-plugin")
	// The skill with a body is on disk, in the agent's own folder; nothing outside it, nothing for the bad id.
	skill := filepath.Join(plugin, "skills", "skill1", "SKILL.md")
	if raw, err := os.ReadFile(skill); err != nil || !strings.Contains(string(raw), "Пиши коротко.") || !strings.Contains(string(raw), `description: "Ревью кода"`) {
		t.Errorf("skill file: %q %v", raw, err)
	}
	// A skill by a reference and a document are skills that say where the thing is; the repositories are one skill of them all.
	for id, want := range map[string]string{"skill2": "https://example.org/skill", "doc1": "https://docs.example.org/rules", reposSkillID: "https://git.example.org/acme/mono.git"} {
		if raw, err := os.ReadFile(filepath.Join(plugin, "skills", id, "SKILL.md")); err != nil || !strings.Contains(string(raw), want) {
			t.Errorf("%s: %q %v", id, raw, err)
		}
	}
	// The MCP server is in the plugin's .mcp.json, its secret by name only.
	var mcp struct {
		Servers map[string]map[string]any `json:"mcpServers"`
	}
	if raw, err := os.ReadFile(filepath.Join(plugin, ".mcp.json")); err != nil || json.Unmarshal(raw, &mcp) != nil {
		t.Fatalf(".mcp.json: %q %v", raw, err)
	}
	if len(mcp.Servers) != 1 {
		t.Fatalf("servers: %+v", mcp.Servers)
	}
	for _, srv := range mcp.Servers {
		h, _ := srv["headers"].(map[string]any)
		if srv["type"] != "http" || srv["url"] != "https://tracker.example.org/mcp" || h["Authorization"] != "Bearer ${TRACKER_TOKEN:-}" {
			t.Errorf("server: %+v", srv)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "environment", "evil")); !os.IsNotExist(err) {
		t.Errorf("a path escaped: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "environment", "claude-plugin", ".claude-plugin", "plugin.json")); err != nil {
		t.Errorf("plugin.json: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "environment.json")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("environment.json: %v %v", fi, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "environment.json.tmp")); !os.IsNotExist(err) {
		t.Errorf("the temporary file stayed: %v", err)
	}

	// A restart reads the list back, and the hash tells the hub what is held.
	k2 := newEnvKeeper(dir)
	if rev, hash := k2.Held(); rev != 3 || hash != set.Hash {
		t.Fatalf("after a restart: %d %q", rev, hash)
	}
	if got := k2.State(); len(got.Applied) != len(items) {
		t.Errorf("applied after a restart: %+v", got)
	}

	// The same list under a newer revision is not applied again, only the revision moves.
	if err := os.Remove(skill); err != nil {
		t.Fatal(err)
	}
	if st, err = k2.Set(hubproto.EnvSet{Rev: 4, Hash: set.Hash, Items: items}); err != nil || st.Rev != 4 {
		t.Fatalf("%+v %v", st, err)
	}
	if _, err := os.Stat(skill); !os.IsNotExist(err) {
		t.Errorf("an identical list was applied again")
	}

	// A new list replaces the old; a skill that left the list leaves the disk.
	next := []hubproto.EnvItem{{ItemID: "skill3", Kind: "skill", Name: "Другой", Layer: "org", Source: json.RawMessage(`{"body":"Другое."}`)}}
	if st, err = k2.Set(hubproto.EnvSet{Rev: 5, Hash: hubproto.EnvHash(next), Items: next}); err != nil || len(st.Applied) != 1 {
		t.Fatalf("%+v %v", st, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "environment", "claude-plugin", "skills", "skill1")); !os.IsNotExist(err) {
		t.Errorf("a skill that left the list stayed")
	}
	if _, err := os.Stat(filepath.Join(dir, "environment", "claude-plugin", "skills", "skill3", "SKILL.md")); err != nil {
		t.Errorf("the new skill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "environment", "claude-plugin", ".mcp.json")); !os.IsNotExist(err) {
		t.Errorf("the servers that left the list stayed: %v", err)
	}

	// An empty list leaves no plugin for the chats to load.
	if _, err = k2.Set(hubproto.EnvSet{Rev: 6, Hash: hubproto.EnvHash(nil), Items: nil}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "environment", "claude-plugin")); !os.IsNotExist(err) {
		t.Errorf("an empty list left the plugin: %v", err)
	}
}

// A secret that comes later turns the waiting server "ok", and the keeper says the outcome changed.
func TestEnvKeeperReappliesWhenASecretComes(t *testing.T) {
	k := newEnvKeeper(t.TempDir())
	have := map[string]bool{}
	k.hasSecret = func(n string) bool { return have[n] }
	items := []hubproto.EnvItem{
		{ItemID: "mcp1", Kind: "mcp", Name: "Трекер", Source: json.RawMessage(`{"transport":"stdio","command":"npx","args":["-y","tracker-mcp"],"secretRefs":["TRACKER_TOKEN"]}`)},
		{ItemID: "mcp2", Kind: "mcp", Name: "Two secrets", Source: json.RawMessage(`{"transport":"sse","url":"https://x.example.org","secretRefs":["A","B"]}`)},
	}
	st, err := k.Set(hubproto.EnvSet{Rev: 1, Hash: hubproto.EnvHash(items), Items: items})
	if err != nil || st.Applied[0].Status != hubproto.EnvPending || st.Applied[1].Status != hubproto.EnvPending {
		t.Fatalf("%+v %v", st, err)
	}
	if k.Reapply() {
		t.Error("nothing changed, yet the keeper says it did")
	}
	have["TRACKER_TOKEN"] = true
	if !k.Reapply() || k.State().Applied[0].Status != hubproto.EnvOK {
		t.Fatalf("after the secret: %+v", k.State())
	}
	raw, _ := os.ReadFile(filepath.Join(k.plugin, ".mcp.json"))
	if !strings.Contains(string(raw), `"TRACKER_TOKEN": "${TRACKER_TOKEN:-}"`) || !strings.Contains(string(raw), `"tracker-mcp"`) || strings.Contains(string(raw), "x.example.org") {
		t.Errorf(".mcp.json: %s", raw)
	}
}

func TestEnvKeeperRefusesAListWithAWrongHash(t *testing.T) {
	dir := t.TempDir()
	k := newEnvKeeper(dir)
	items := envItems()
	if _, err := k.Set(hubproto.EnvSet{Rev: 1, Hash: "deadbeef", Items: items}); err == nil {
		t.Fatal("a list with a wrong hash was taken")
	}
	if _, err := os.Stat(filepath.Join(dir, "environment.json")); !os.IsNotExist(err) {
		t.Errorf("a refused list was written: %v", err)
	}
	// A damaged file is ignored, not trusted.
	if err := os.WriteFile(filepath.Join(dir, "environment.json"), []byte(`{"rev":9,"hash":"x","items":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if rev, _ := newEnvKeeper(dir).Held(); rev != 0 {
		t.Errorf("a damaged file was trusted: %d", rev)
	}
}

func TestHubEnvSetOverTheLink(t *testing.T) {
	r := startHubAgent(t, rigOptions{})
	c := r.connect()
	hello := decode[hubproto.Hello](t, c.mustCall("hello", nil))
	if !contains(hello.Features, hubproto.FeatureEnvironment) || hello.EnvRev != 0 || hello.EnvHash != "" {
		t.Fatalf("%+v", hello)
	}
	items := envItems()
	st := decode[hubproto.EnvState](t, c.mustCall(hubproto.OpEnvSet, map[string]any{"rev": 2, "hash": hubproto.EnvHash(items), "items": items}))
	if st.Rev != 2 || len(st.Applied) != len(items) {
		t.Fatalf("%+v", st)
	}
	if hello = decode[hubproto.Hello](t, c.mustCall("hello", nil)); hello.EnvRev != 2 || hello.EnvHash != st.Hash {
		t.Fatalf("%+v", hello)
	}
	for name, p := range map[string]map[string]any{
		"no rev":    {"hash": st.Hash, "items": items},
		"bad hash":  {"rev": 3, "hash": "nope", "items": items},
		"bad shape": {"rev": 3, "hash": st.Hash, "items": "x"},
	} {
		if _, e := c.call(hubproto.OpEnvSet, p); e == "" {
			t.Errorf("%s: accepted", name)
		}
	}
	if a := r.audit(); !strings.Contains(a, `"op":"env.set"`) {
		t.Errorf("audit %s", a)
	}
}

// The organization's rule for apps (HRN-915) rides on env.set: the agent keeps it beside the list for the runtime, says
// the hash of both to the hub, and a change of the rule alone applies nothing again.
func TestEnvKeeperKeepsTheAppsRule(t *testing.T) {
	dir := t.TempDir()
	k := newEnvKeeper(dir)
	items := envItems()
	plain := hubproto.EnvHash(items)
	if _, err := k.Set(hubproto.EnvSet{Rev: 1, Hash: plain, Items: items}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Set(hubproto.EnvSet{Rev: 2, Hash: plain, Items: items, AppsConsent: hubproto.AppsConsentAuto}); err == nil {
		t.Fatal("the rule auto under the items' hash was taken")
	}
	auto := hubproto.EnvSetHash(plain, hubproto.AppsConsentAuto)
	st, err := k.Set(hubproto.EnvSet{Rev: 2, Hash: auto, Items: items, AppsConsent: hubproto.AppsConsentAuto})
	if err != nil {
		t.Fatal(err)
	}
	if rev, hash := k.Held(); st.Hash != auto || rev != 2 || hash != auto {
		t.Fatalf("held %d %q, state %q; want %q", rev, hash, st.Hash, auto)
	}
	var file struct {
		Hash        string `json:"hash"`
		AppsConsent string `json:"appsConsent"`
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "environment.json"))
	if err := json.Unmarshal(raw, &file); err != nil || file.Hash != plain || file.AppsConsent != hubproto.AppsConsentAuto {
		t.Fatalf("the file keeps the items' hash for the setup and the rule for the runtime: %s", raw)
	}
	// Read again after a restart, then turned off: the hash is the items' again.
	k2 := newEnvKeeper(dir)
	if _, hash := k2.Held(); hash != auto {
		t.Fatalf("after a restart the rule is held: %q", hash)
	}
	if st, err = k2.Set(hubproto.EnvSet{Rev: 3, Hash: plain, Items: items}); err != nil || st.Hash != plain {
		t.Fatalf("the rule turned off: %+v %v", st, err)
	}
	raw, _ = os.ReadFile(filepath.Join(dir, "environment.json"))
	if strings.Contains(string(raw), "appsConsent") {
		t.Fatalf("the rule ask is not kept in the file: %s", raw)
	}
}
