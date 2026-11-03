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
		{ItemID: "odd1", Kind: "plugin", Name: "Странный", Layer: "team", Source: json.RawMessage(`{}`)},
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
	for id, want := range map[string]string{"skill1": hubproto.EnvPending, "skill2": hubproto.EnvPending, "mcp1": hubproto.EnvPending, "repo1": hubproto.EnvPending,
		"doc1": hubproto.EnvPending, "../evil": hubproto.EnvError, "odd1": hubproto.EnvError} {
		if by[id].Status != want || by[id].Message == "" {
			t.Errorf("%s = %+v, want %s with a message", id, by[id], want)
		}
	}
	// The skill with a body is on disk, in the agent's own folder; nothing outside it, nothing for the bad id.
	skill := filepath.Join(dir, "environment", "claude-plugin", "skills", "skill1", "SKILL.md")
	if raw, err := os.ReadFile(skill); err != nil || !strings.Contains(string(raw), "Пиши коротко.") {
		t.Errorf("skill file: %q %v", raw, err)
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
