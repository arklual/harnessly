package env

import (
	"context"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/store/storetest"
)

// The version an app's row shows is the newest a machine named, and «обновлено» is when a machine put it on — a later
// run that found it already there does not move it.
func TestAppVersionComesFromTheMachinesRuns(t *testing.T) {
	ctx := context.Background()
	db := storetest.Open(t)
	st := db.S()
	now := time.Date(2026, 11, 15, 6, 0, 0, 0, time.UTC)
	put := func(id string, ago time.Duration, steps string) {
		t.Helper()
		at := now.Add(-ago)
		if _, err := st.UpsertEnvRun(ctx, store.EnvRun{ID: id, OrgID: "org1", MachineID: "m", Mode: "apply", Status: "ok",
			StartedAt: at, FinishedAt: at.Add(time.Second), Steps: steps, UpdatedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	if v, at := appVersionOf(ctx, st, "org1", "app-boards", now); v != nil || at != nil {
		t.Fatalf("no runs, no version: %v %v", v, at)
	}
	put("run-old-000001", 48*time.Hour, `[{"itemId":"app-boards","kind":"app","name":"Доски","status":"ok","message":"поставлено: harnessly.boards","ms":1,"version":"0.4.0"}]`)
	put("run-new-000002", 2*time.Hour, `[{"itemId":"app-boards","kind":"app","name":"Доски","status":"ok","message":"поставлено: harnessly.boards","ms":1,"version":"0.5.0"}]`)
	put("run-same-00003", time.Hour, `[{"itemId":"app-boards","kind":"app","name":"Доски","status":"skipped","message":"уже стоит","ms":1,"version":"0.5.0"}]`)
	v, at := appVersionOf(ctx, st, "org1", "app-boards", now)
	if v == nil || *v != "0.5.0" || at == nil || *at != now.Add(-2*time.Hour+time.Second).Format(time.RFC3339) {
		t.Fatalf("version %v at %v", show(v), show(at))
	}
	if v, _ := appVersionOf(ctx, st, "org1", "app-docs", now); v != nil {
		t.Fatalf("another app: %v", *v)
	}
}

func show(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}
