package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestMergedMRsSQLite(t *testing.T)   { exerciseMergedMRs(t, openTest(t)) }
func TestMergedMRsPostgres(t *testing.T) { exerciseMergedMRs(t, openPostgres(t)) }

func exerciseMergedMRs(t *testing.T, db *DB) {
	ctx := context.Background()
	s := db.S()
	u := &User{Email: "mr@example.com"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	const org = "org1"
	put := func(key, parent string, start int64, metrics string) {
		t.Helper()
		if _, err := s.PutSession(ctx, AgentSession{OrgKey: org, Provider: "claude", SessionKey: key, ParentKey: parent, UserID: u.ID,
			Funding: FundingPersonal, Origin: "live", Attribution: "x", StartedAt: start, Metrics: metrics, Seq: 1, UpdatedAt: start}); err != nil {
			t.Fatal(err)
		}
	}
	put("early", "", 1000, `{"mrs":2,"mrsMerged":9}`)
	put("late", "", 5000, `{"mrs":1}`)
	put("sub", "late", 5100, `{}`)
	put("none", "", 6000, `{"mrsMerged":4}`)

	read := func() map[string]map[string]float64 {
		t.Helper()
		got, err := s.UserSessions(ctx, SessionFilter{UserID: u.ID, From: 0, To: 100000})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]map[string]float64{}
		for _, x := range got {
			m := map[string]float64{}
			_ = json.Unmarshal([]byte(x.Metrics), &m)
			out[x.SessionKey] = m
		}
		return out
	}
	// Without merged requests what the machines sent stays.
	if m := read(); m["early"]["mrsMerged"] != 9 || m["none"]["mrsMerged"] != 4 {
		t.Fatalf("untouched: %v", m)
	}

	if err := s.ReplaceSessionMRs(ctx, org, "claude", "early", "early", []string{"h/a/!1", "h/a/!2"}); err != nil {
		t.Fatal(err)
	}
	// The later session and its subagent mention !1 and !3.
	if err := s.ReplaceSessionMRs(ctx, org, "claude", "late", "late", []string{"h/a/!1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceSessionMRs(ctx, org, "claude", "sub", "late", []string{"h/a/!3"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMergedMRs(ctx, []MergedMR{
		{Key: "h/a/!1", MergedAt: 9000, CreatedAt: 1500, Title: "one"},
		{Key: "h/a/!3", MergedAt: 9100, CreatedAt: 5500},
		{Key: "h/a/!4", MergedAt: 9200, CreatedAt: 100},
	}); err != nil {
		t.Fatal(err)
	}
	if v, err := s.NewestMergedAt(ctx); err != nil || v != 9200 {
		t.Fatalf("newest %d %v", v, err)
	}
	m := read()
	// !1 counts for the earlier session only; !2 is not merged; !3 is the later session's through its subagent.
	if m["early"]["mrsMerged"] != 1 || m["early"]["firstMrMs"] != 500 {
		t.Errorf("early %v", m["early"])
	}
	if m["late"]["mrsMerged"] != 1 || m["late"]["firstMrMs"] != 500 {
		t.Errorf("late %v", m["late"])
	}
	// A request made before the session began is no «time to MR», and a machine's own count is replaced.
	if m["none"]["mrsMerged"] != 0 {
		t.Errorf("none %v", m["none"])
	}
	if _, ok := m["none"]["firstMrMs"]; ok {
		t.Errorf("none has firstMrMs %v", m["none"])
	}
	if m["late"]["mrs"] != 1 {
		t.Errorf("other metrics kept: %v", m["late"])
	}
	// Replacing the keys of a session drops the old ones; the upsert replaces a row.
	if err := s.ReplaceSessionMRs(ctx, org, "claude", "early", "early", []string{"h/a/!2"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMergedMRs(ctx, []MergedMR{{Key: "h/a/!2", MergedAt: 9300, CreatedAt: 900}}); err != nil {
		t.Fatal(err)
	}
	m = read()
	// !2 was created before the session started (900 < 1000): counted, but it gives no firstMrMs.
	if m["early"]["mrsMerged"] != 1 {
		t.Errorf("early count %v", m["early"])
	}
	if _, ok := m["early"]["firstMrMs"]; ok {
		t.Errorf("early after replace %v", m["early"])
	}
	if m["late"]["mrsMerged"] != 2 {
		t.Errorf("late now owns !1: %v", m["late"])
	}
}

func TestMRSurvivalSQLite(t *testing.T)   { exerciseMRSurvival(t, openTest(t)) }
func TestMRSurvivalPostgres(t *testing.T) { exerciseMRSurvival(t, openPostgres(t)) }

// A merged request survives when SurvivalDays have passed without a revert of it; younger ones are not counted at all.
func exerciseMRSurvival(t *testing.T, db *DB) {
	ctx := context.Background()
	s := db.S()
	u := &User{Email: "surv@example.com"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	const org = "org1"
	day := int64(24 * 3600 * 1000)
	now := time.Now().UnixMilli()
	if _, err := s.PutSession(ctx, AgentSession{OrgKey: org, Provider: "claude", SessionKey: "s", UserID: u.ID, Funding: FundingPersonal,
		Origin: "live", Attribution: "x", StartedAt: now - 60*day, Metrics: `{}`, Seq: 1, UpdatedAt: now - 60*day}); err != nil {
		t.Fatal(err)
	}
	keys := []string{"h/a/!1", "h/a/!2", "h/a/!3", "h/a/!4"}
	if err := s.ReplaceSessionMRs(ctx, org, "claude", "s", "s", keys); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMergedMRs(ctx, []MergedMR{
		{Key: "h/a/!1", MergedAt: now - 40*day, CreatedAt: now - 41*day, Title: "keep", Project: "a"},
		{Key: "h/a/!2", MergedAt: now - 40*day, CreatedAt: now - 41*day, Title: "undone", Project: "a"},
		{Key: "h/a/!3", MergedAt: now - 3*day, CreatedAt: now - 4*day, Title: "fresh", Project: "a"},
		// Reverts: one inside the 14 days of !2; one of !1 after them; one of another project's title.
		{Key: "h/a/!5", MergedAt: now - 30*day, CreatedAt: now - 31*day, Title: `Revert "undone"`, Project: "a"},
		{Key: "h/a/!6", MergedAt: now - 10*day, CreatedAt: now - 11*day, Title: `Revert "keep"`, Project: "a"},
		{Key: "h/a/!7", MergedAt: now - 39*day, CreatedAt: now - 39*day, Title: `Revert "keep"`, Project: "b"},
		{Key: "h/a/!4", MergedAt: now - 20*day, CreatedAt: now - 21*day, Title: "other", Project: "a"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.UserSessions(ctx, SessionFilter{UserID: u.ID, From: 0, To: now + day})
	if err != nil || len(got) != 1 {
		t.Fatalf("sessions %v %v", got, err)
	}
	m := map[string]float64{}
	_ = json.Unmarshal([]byte(got[0].Metrics), &m)
	// Mature: !1 (revert came after 14 days), !2 (reverted in 10 days), !4. !3 is too young. Survived: !1 and !4.
	if m["mrsMerged"] != 4 || m["mrsMature"] != 3 || m["mrsSurvived"] != 2 {
		t.Errorf("survival %v", m)
	}
}
