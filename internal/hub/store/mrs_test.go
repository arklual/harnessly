package store

import (
	"context"
	"encoding/json"
	"testing"
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
