package store

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestSessionSumsSQLite(t *testing.T)   { exerciseSessionSums(t, openTest(t)) }
func TestSessionSumsPostgres(t *testing.T) { exerciseSessionSums(t, openPostgres(t)) }

// exerciseSessionSums: the sums kept in usage_session_sums are the sums of the calls, after every way they change — a
// call taken, a call priced and summed again, the organization summed anew — subagents and long calls included.
func exerciseSessionSums(t *testing.T, db *DB) {
	ctx := context.Background()
	s := db.S()
	const key = "space:sums"
	for _, x := range []AgentSession{
		{SessionKey: "top"}, {SessionKey: "sub", ParentKey: "top"}, {SessionKey: "other"},
	} {
		x.OrgKey, x.Provider, x.Funding, x.Origin, x.Attribution, x.StartedAt, x.Seq, x.UpdatedAt = key, "claude", FundingOrg, "live", "guessed", 1000, 1, 1000
		if _, err := s.PutSession(ctx, x); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	add := func(session, model string, input int64, list *int64) {
		n++
		c := UsageCall{OrgKey: key, Provider: "claude", MsgKey: fmt.Sprintf("m%d", n), SessionKey: session, Ts: 3600000 + int64(n), Model: model,
			Input: input, Output: 7, CacheRead: 100, CacheWrite5m: 3, CacheWrite1h: 2, ListCostMicros: list, ReceivedAt: 1}
		if ok, err := s.AddCall(ctx, c); err != nil || !ok {
			t.Fatalf("add %v %v", ok, err)
		}
	}
	add("top", "opus", 10, i64(100))
	add("top", "opus", 300000, i64(5000))
	add("sub", "opus", 20, i64(200))
	add("sub", "haiku", 260000, nil)
	add("other", "opus", 1, i64(1))
	refs := []SessionRef{{key, "claude", "top"}, {key, "claude", "other"}}
	// past sessionChunk the sums are picked from the whole organization's rows: the same answer
	many := slices.Clone(refs)
	for i := range sessionChunk {
		many = append(many, SessionRef{key, "claude", fmt.Sprintf("absent%d", i)})
	}
	same := func(when string) {
		t.Helper()
		for _, c := range []struct {
			refs []SessionRef
			over int64
		}{{refs, 0}, {refs, LongContextTokens}, {many, 0}, {many, LongContextTokens}} {
			refs, over := c.refs, c.over
			got, gotSubs, err := s.sessionSums(ctx, refs, over)
			if err != nil {
				t.Fatal(err)
			}
			want, wantSubs, err := s.sessionSumsFromCalls(ctx, refs, over)
			if err != nil {
				t.Fatal(err)
			}
			order := func(a, b SessionCallSum) int {
				return strings.Compare(a.SessionKey+"/"+a.Model, b.SessionKey+"/"+b.Model)
			}
			slices.SortFunc(got, order)
			slices.SortFunc(want, order)
			if fmt.Sprint(got) != fmt.Sprint(want) || fmt.Sprint(gotSubs) != fmt.Sprint(wantSubs) {
				t.Fatalf("%s, over %d:\n kept  %v %v\n calls %v %v", when, over, got, gotSubs, want, wantSubs)
			}
			if over == 0 && len(got) != 3 {
				t.Fatalf("%s: %v", when, got)
			}
		}
	}
	same("taken")
	// A price given later: the hour summed again sums its sessions again.
	if ok, err := s.PriceCall(ctx, key, "claude", "m4", 9999, 1); err != nil || !ok {
		t.Fatalf("price %v %v", ok, err)
	}
	if err := s.Rollup(ctx, key, []int64{3600000}); err != nil {
		t.Fatal(err)
	}
	same("priced")
	if err := db.Tx(ctx, func(s Store) error { return s.ResumSessions(ctx, key) }); err != nil {
		t.Fatal(err)
	}
	same("summed anew")
}
