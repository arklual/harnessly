package store

import (
	"context"
	"testing"
	"time"
)

func TestEnvSQLite(t *testing.T)   { exerciseEnv(t, openTest(t)) }
func TestEnvPostgres(t *testing.T) { exerciseEnv(t, openPostgres(t)) }

func exerciseEnv(t *testing.T, db *DB) {
	ctx := context.Background()
	s := db.S()
	now := time.UnixMilli(1_700_000_000_000).UTC()
	alice := &User{Email: "alice@example.com"}
	if err := s.CreateUser(ctx, alice); err != nil {
		t.Fatal(err)
	}

	builtin, err := s.AddCatalogItem(ctx, CatalogItem{Kind: CatalogSkill, Name: "review", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	own, err := s.AddCatalogItem(ctx, CatalogItem{OrgID: "org1", Kind: CatalogMCP, Name: "tracker", Hint: "tasks", CreatedAt: now, CreatedBy: alice.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddCatalogItem(ctx, CatalogItem{OrgID: "org2", Kind: CatalogDoc, Name: "foreign", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	items, err := s.CatalogItems(ctx, "org1", false)
	if err != nil || len(items) != 2 || items[0].ID != own.ID || items[1].OrgID != "" {
		t.Fatalf("items %+v, %v", items, err)
	}
	if got, _ := s.CatalogItemByID(ctx, own.ID); got.Source != "{}" || got.CreatedBy != alice.ID {
		t.Fatalf("item %+v", got)
	}
	arch := now.Add(time.Hour)
	own.ArchivedAt = &arch
	if err := s.UpdateCatalogItem(ctx, own); err != nil {
		t.Fatal(err)
	}
	if items, _ = s.CatalogItems(ctx, "org1", false); len(items) != 1 {
		t.Fatalf("archived stays: %+v", items)
	}
	if items, _ = s.CatalogItems(ctx, "org1", true); len(items) != 2 {
		t.Fatalf("with archived: %+v", items)
	}

	e, ok, err := s.AddEnvEntry(ctx, EnvEntry{OrgID: "org1", Layer: EnvLayerOrg, ScopeID: "org1", ItemID: builtin.ID, Enabled: true, Pinned: true, UpdatedAt: now, UpdatedBy: alice.ID})
	if err != nil || !ok || e.Rev != 1 {
		t.Fatalf("entry %+v %v %v", e, ok, err)
	}
	if _, ok, err := s.AddEnvEntry(ctx, EnvEntry{OrgID: "org1", Layer: EnvLayerOrg, ScopeID: "org1", ItemID: builtin.ID, Enabled: true, UpdatedAt: now}); err != nil || ok {
		t.Fatalf("a second entry of the layer: %v %v", ok, err)
	}
	got, err := s.EnvEntryByKey(ctx, EnvLayerOrg, "org1", builtin.ID)
	if err != nil || got.ID != e.ID || !got.Enabled || !got.Pinned || got.Audience == "" {
		t.Fatalf("by key %+v, %v", got, err)
	}
	got.Enabled, got.Rev, got.Audience = false, 2, `{"mode":"off","teams":[]}`
	if err := s.UpdateEnvEntry(ctx, got); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.EnvEntryByID(ctx, e.ID); again.Enabled || again.Rev != 2 || again.Audience != got.Audience {
		t.Fatalf("updated %+v", again)
	}
	if all, err := s.EnvEntriesOfOrg(ctx, "org1"); err != nil || len(all) != 1 {
		t.Fatalf("entries %+v, %v", all, err)
	}
	if err := s.DeleteEnvEntry(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnvEntryByID(ctx, e.ID); !IsNotFound(err) {
		t.Fatalf("deleted entry: %v", err)
	}

	if err := s.SetEnvPersonOff(ctx, alice.ID, "sp1", builtin.ID, true, now); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnvPersonOff(ctx, alice.ID, "sp1", builtin.ID, true, now); err != nil {
		t.Fatalf("switching off twice: %v", err)
	}
	if off, _ := s.EnvPersonOffs(ctx, alice.ID, "sp1"); !off[builtin.ID] || len(off) != 1 {
		t.Fatalf("off %+v", off)
	}
	if n, _ := s.EnvOffCountByItem(ctx, "sp1"); n[builtin.ID] != 1 {
		t.Fatalf("off count %+v", n)
	}
	if err := s.SetEnvPersonOff(ctx, alice.ID, "sp1", builtin.ID, false, now); err != nil {
		t.Fatal(err)
	}
	if off, _ := s.EnvPersonOffs(ctx, alice.ID, "sp1"); len(off) != 0 {
		t.Fatalf("on again: %+v", off)
	}

	// What the hub sent and what the machine reported are kept apart.
	if _, err := s.EnvStateByMachine(ctx, "m1"); !IsNotFound(err) {
		t.Fatalf("no state yet: %v", err)
	}
	if err := s.SaveEnvSent(ctx, "m1", 3, "h3", now); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveEnvReported(ctx, "m1", 3, "h3", `[{"itemId":"a","status":"ok"}]`, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveEnvSent(ctx, "m1", 4, "h4", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	st, err := s.EnvStateByMachine(ctx, "m1")
	if err != nil || st.Rev != 3 || st.Hash != "h3" || st.SentRev != 4 || st.SentHash != "h4" || st.Applied == "" || !st.At.Equal(now.Add(time.Second)) {
		t.Fatalf("state %+v, %v", st, err)
	}

	// Owner and position are kept; a script entry carries its place.
	sc, err := s.AddCatalogItem(ctx, CatalogItem{OrgID: "org1", Kind: CatalogScript, Name: "setup", CreatedAt: now, OwnerLayer: EnvLayerTeam, OwnerScope: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.CatalogItemByID(ctx, sc.ID); got.OwnerLayer != EnvLayerTeam || got.OwnerScope != "t1" {
		t.Fatalf("owner %+v", got)
	}
	if got, _ := s.CatalogItemByID(ctx, builtin.ID); got.OwnerLayer != "" || got.OwnerScope != "" {
		t.Fatalf("no owner %+v", got)
	}
	pe, _, err := s.AddEnvEntry(ctx, EnvEntry{OrgID: "org1", Layer: EnvLayerTeam, ScopeID: "t1", ItemID: sc.ID, Enabled: true, UpdatedAt: now, Position: 3})
	if err != nil {
		t.Fatal(err)
	}
	pe.Position = 1
	if err := s.UpdateEnvEntry(ctx, pe); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.EnvEntryByID(ctx, pe.ID); got.Position != 1 {
		t.Fatalf("position %+v", got)
	}

	// Runs: a later frame of a run replaces the earlier one, the query filters, the sweep removes the old.
	run := EnvRun{ID: "run-aaaaaaaa", OrgID: "org1", MachineID: "m1", UserID: alice.ID, SpaceID: "sp1", Mode: "update", Status: "running",
		StartedAt: now, Steps: "[]", UpdatedAt: now}
	if ok, err := s.UpsertEnvRun(ctx, run); err != nil || !ok {
		t.Fatal(ok, err)
	}
	run.Status, run.FinishedAt, run.Steps, run.UserID = "error", now.Add(time.Minute), `[{"itemId":"a","log":"boom"}]`, "someone-else"
	if ok, err := s.UpsertEnvRun(ctx, run); err != nil || !ok {
		t.Fatal(ok, err)
	}
	ok2 := EnvRun{ID: "run-bbbbbbbb", OrgID: "org1", MachineID: "m2", Mode: "check", Status: "ok", StartedAt: now.Add(time.Hour), UpdatedAt: now}
	old := EnvRun{ID: "run-cccccccc", OrgID: "org1", MachineID: "m1", Mode: "apply", Status: "ok", StartedAt: now.Add(-48 * time.Hour), UpdatedAt: now}
	foreign := EnvRun{ID: "run-dddddddd", OrgID: "org2", MachineID: "m3", Mode: "apply", Status: "ok", StartedAt: now, UpdatedAt: now}
	for _, r := range []EnvRun{ok2, old, foreign} {
		if ok, err := s.UpsertEnvRun(ctx, r); err != nil || !ok {
			t.Fatal(ok, err)
		}
	}
	got1, err := s.EnvRunByID(ctx, "org1", run.ID)
	if err != nil || got1.Status != "error" || got1.UserID != alice.ID || got1.Steps != run.Steps || !got1.FinishedAt.Equal(run.FinishedAt) {
		t.Fatalf("run %+v, %v", got1, err)
	}
	if _, err := s.EnvRunByID(ctx, "org1", "nope"); !IsNotFound(err) {
		t.Fatalf("missing run: %v", err)
	}
	if _, err := s.EnvRunByID(ctx, "org2", run.ID); !IsNotFound(err) {
		t.Fatalf("run of another organization: %v", err)
	}
	// Another machine's frame with the same id changes nothing.
	hijack := run
	hijack.MachineID, hijack.Status, hijack.Steps = "m9", "ok", "[]"
	if ok, err := s.UpsertEnvRun(ctx, hijack); err != nil || ok {
		t.Fatalf("hijack: %v %v", ok, err)
	}
	if again, _ := s.EnvRunByID(ctx, "org1", run.ID); again.Status != "error" || again.MachineID != "m1" {
		t.Fatalf("hijacked %+v", again)
	}
	since := now.Add(-time.Hour)
	if rs, err := s.EnvRunsOf(ctx, EnvRunQuery{OrgID: "org1", Since: since}); err != nil || len(rs) != 2 || rs[0].ID != ok2.ID {
		t.Fatalf("runs %+v, %v", rs, err)
	}
	if rs, _ := s.EnvRunsOf(ctx, EnvRunQuery{OrgID: "org1", Since: since, ErrorsOnly: true}); len(rs) != 1 || rs[0].ID != run.ID {
		t.Fatalf("errors only %+v", rs)
	}
	if rs, _ := s.EnvRunsOf(ctx, EnvRunQuery{OrgID: "org1", Since: since, MachineIDs: []string{"m2"}}); len(rs) != 1 || rs[0].ID != ok2.ID {
		t.Fatalf("machine %+v", rs)
	}
	if rs, _ := s.EnvRunsOf(ctx, EnvRunQuery{OrgID: "org1", Since: since, MachineIDs: []string{}}); len(rs) != 0 {
		t.Fatalf("no machines %+v", rs)
	}
	if rs, _ := s.EnvRunsOf(ctx, EnvRunQuery{OrgID: "org1", Since: since, Limit: 1}); len(rs) != 1 {
		t.Fatalf("limit %+v", rs)
	}
	if n, err := s.DeleteEnvRunsBefore(ctx, now.Add(-24*time.Hour)); err != nil || n != 1 {
		t.Fatalf("sweep %d, %v", n, err)
	}
	if rs, _ := s.EnvRunsOf(ctx, EnvRunQuery{OrgID: "org1", Since: now.Add(-100 * time.Hour)}); len(rs) != 2 {
		t.Fatalf("after the sweep %+v", rs)
	}

	// A repeated fact of a session replaces its row; the read sums the sessions.
	u := ToolUsage{OrgKey: "org1", SpaceID: "sp1", UserID: alice.ID, MachineID: "m1", SessionKey: "s1", ToolKind: "mcp", ToolName: "tracker", Day: "2026-10-30", Calls: 2, Errors: 1, LastAt: 1700000000000}
	if err := s.UpsertToolUsage(ctx, u); err != nil {
		t.Fatal(err)
	}
	u.Calls, u.Errors, u.ItemID = 5, 1, own.ID
	if err := s.UpsertToolUsage(ctx, u); err != nil {
		t.Fatal(err)
	}
	u.SessionKey, u.Calls, u.Errors = "s2", 3, 0
	if err := s.UpsertToolUsage(ctx, u); err != nil {
		t.Fatal(err)
	}
	u.Day, u.SessionKey = "2026-10-20", "s1"
	if err := s.UpsertToolUsage(ctx, u); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ToolUsageOf(ctx, "org1", "2026-10-25")
	if err != nil || len(rows) != 1 || rows[0].Calls != 8 || rows[0].Errors != 1 || rows[0].ItemID != own.ID {
		t.Fatalf("usage %+v, %v", rows, err)
	}
	if rows, _ = s.ToolUsageOf(ctx, "other", "2026-10-25"); len(rows) != 0 {
		t.Fatalf("foreign usage %+v", rows)
	}
}

func TestItemForTool(t *testing.T) {
	gone := time.UnixMilli(1)
	items := []CatalogItem{
		{ID: "built", Kind: CatalogSkill, Name: "review"},
		{ID: "own", OrgID: "org1", Kind: CatalogSkill, Name: "Review"},
		{ID: "old", OrgID: "org1", Kind: CatalogSkill, Name: "deploy", ArchivedAt: &gone},
		{ID: "live", Kind: CatalogSkill, Name: "deploy"},
		{ID: "srv", OrgID: "org1", Kind: CatalogMCP, Name: "grafana"},
		{ID: "repo", OrgID: "org1", Kind: CatalogRepo, Name: "grafana"},
		{ID: "docs", OrgID: "org1", Kind: CatalogMCP, Name: "docs"},
	}
	for _, c := range []struct{ kind, name, want string }{
		{"skill", "review", "own"},             // the organization's beats the built-in, whatever the case
		{"skill", "acme:review", "own"},        // plugin-qualified
		{"skill", "deploy", "live"},            // live beats archived
		{"mcp", "grafana", "srv"},              // by kind
		{"mcp", "plugin_tools_grafana", "srv"}, // a plugin's server by its short name
		{"mcp", "claude_ai_Docs", "docs"},      // a connector's
		{"mcp", "plugin_tools", ""},            // a prefix and nothing after it
		{"skill", "grafana", ""},               // another kind's name is not a match
		{"bash", "Bash", ""},                   // no item
		{"other", "Read", ""},                  //
		{"skill", "unknown", ""},               //
		{"skill", "acme:", ""},                 // nothing after the colon
	} {
		if got := ItemForTool(items, c.kind, c.name); got != c.want {
			t.Errorf("%s %q = %q, want %q", c.kind, c.name, got, c.want)
		}
	}
}
