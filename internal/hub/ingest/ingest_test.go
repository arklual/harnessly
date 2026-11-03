package ingest_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// outbox is the machine side of the protocol as the agent keeps it: records by seq in an epoch, dropped on ack.
type outbox struct {
	mu      sync.Mutex
	epoch   string
	seq     int64
	records []hubproto.UsageRecord
	acked   []hubproto.UsageAckParams
	reads   []hubproto.UsageReadParams
}

func (o *outbox) add(at int64, r hubproto.UsageRecord) int64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seq++
	r.Seq, r.At = o.seq, at
	o.records = append(o.records, r)
	return o.seq
}

// renew makes the outbox anew, as after the machine's data folder was wiped.
func (o *outbox) renew(epoch string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.epoch, o.seq, o.records = epoch, 0, nil
}

func (o *outbox) read(c hubtest.Call) (any, error) {
	var p hubproto.UsageReadParams
	decode(c, &p)
	o.mu.Lock()
	defer o.mu.Unlock()
	o.reads = append(o.reads, p)
	after := p.After
	if p.Epoch != o.epoch {
		after = 0
	}
	res := hubproto.UsageReadResult{Epoch: o.epoch, Last: after, Records: []hubproto.UsageRecord{}}
	for _, r := range o.records {
		if r.Seq <= after {
			continue
		}
		if len(res.Records) == p.Max {
			res.More = true
			break
		}
		res.Records = append(res.Records, r)
		res.Last = r.Seq
	}
	return res, nil
}

func (o *outbox) ack(c hubtest.Call) (any, error) {
	var p hubproto.UsageAckParams
	decode(c, &p)
	o.mu.Lock()
	defer o.mu.Unlock()
	o.acked = append(o.acked, p)
	if p.Epoch == o.epoch {
		kept := o.records[:0]
		for _, r := range o.records {
			if r.Seq > p.Seq {
				kept = append(kept, r)
			}
		}
		o.records = kept
	}
	return map[string]any{}, nil
}

func (o *outbox) lastAck() hubproto.UsageAckParams {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.acked) == 0 {
		return hubproto.UsageAckParams{}
	}
	return o.acked[len(o.acked)-1]
}

// usageMachine registers a machine whose agent keeps an outbox and connects it.
func usageMachine(t *testing.T, s *hubtest.Stack, p *hubtest.Person, spaceID string, o *outbox) *hubtest.Agent {
	t.Helper()
	a := s.NewAgent(nil)
	a.Hello.Features = append(append([]string{}, a.Hello.Features...), hubproto.FeatureUsage)
	a.Handle(hubproto.OpUsageRead, o.read)
	a.Handle(hubproto.OpUsageAck, o.ack)
	if r := a.Register(p.NewCode(spaceID, "dir"), hubproto.ProfileDir, hubproto.ViaCommand); r.Code != http.StatusCreated {
		t.Fatalf("register: %s", r)
	}
	a.Connect()
	return a
}

// decode reads a call's parameters, which travel at the top of the frame.
func decode(c hubtest.Call, v any) {
	b, _ := json.Marshal(c.Params)
	_ = json.Unmarshal(b, v)
}

func ptr[T any](v T) *T { return &v }

func call(session, msg, model string, ts, in, out int64) hubproto.UsageRecord {
	return hubproto.UsageRecord{Kind: hubproto.UsageCall, Call: &hubproto.UsageCallFact{
		Provider: "claude", MsgKey: msg, SessionKey: session, Ts: ts, Model: model, Input: in, Output: out,
	}}
}

func session(key, parent, subject, ref string, started int64) hubproto.UsageRecord {
	return hubproto.UsageRecord{Kind: hubproto.UsageSession, Session: &hubproto.UsageSessionFact{
		Provider: "claude", SessionKey: key, ParentKey: parent, StartedAt: started, Origin: hubproto.OriginLive, Subject: subject,
		AccountRef: ref, AuthMode: "subscription", Attribution: hubproto.AttributionExact, Metrics: map[string]float64{"prompts": 3},
	}}
}

func TestIngestEndToEnd(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_USAGE_INGEST", "on"))
	ctx := context.Background()
	ann := s.SignIn("ann@example.com")
	bob := s.SignIn("bob@example.com")
	spaceID, _ := ann.NewSpace("Acme")
	ann.Join(spaceID, bob)
	sp, err := s.DB.S().SpaceByID(ctx, spaceID)
	if err != nil {
		t.Fatal(err)
	}
	orgKey := store.OrgKey(sp)

	now := s.Clock.Now().UnixMilli()
	hour := store.HourMs(now - 2*3600_000)
	t0 := hour + 60_000
	o := &outbox{epoch: "e1"}
	// Bob's session on Ann's machine, a subagent of it, a call of a model with no price, the same call twice.
	o.add(now, session("s1", "", "bob@example.com", "claude:acc-1", t0))
	o.add(now, session("s1-sub", "s1", "bob@example.com", "claude:acc-1", t0))
	o.add(now, call("s1", "m1", "claude-opus-4-6", t0+1000, 1_000_000, 100_000))
	o.add(now, call("s1-sub", "m2", "claude-sonnet-4-6", t0+2000, 1_000_000, 0))
	o.add(now, call("s1", "m3", "some-unknown-model", t0+3000, 500, 50))
	o.add(now, call("s1", "m1", "claude-opus-4-6", t0+1000, 1_000_000, 100_000))
	// A session nobody can be found for: the machine's creator, guessed.
	o.add(now, session("s2", "", "stranger@example.com", "", t0))
	o.add(now, hubproto.UsageRecord{Kind: hubproto.UsageWindow, Window: &hubproto.UsageWindowFact{
		Provider: "claude", AccountRef: "claude:acc-1", Name: "five_hour", Scope: "all", LengthSec: 18000, Unit: "pct",
		UsedPct: ptr(42.0), ResetsAt: ptr(now + 3600_000), MeasuredAt: now, Source: "oauth-usage", Confidence: "undocumented",
	}})
	o.add(now, hubproto.UsageRecord{Kind: hubproto.UsageBalance, Balance: &hubproto.UsageBalanceFact{
		Provider: "claude", AccountRef: "key:sha256:0123456789abcdef", Currency: "USD", Available: ptr(12.5), MeasuredAt: now, Source: "console",
	}})
	o.add(now, hubproto.UsageRecord{Kind: "something-new"})
	a := usageMachine(t, s, ann, spaceID, o)

	s.Eventually("the outbox acked", func() bool { return o.lastAck().Seq == 10 })

	// The hub listens for usage.available.
	var events []string
	_ = json.Unmarshal(a.Calls("link.subscribe")[0].Params["events"], &events)
	if !strings.Contains(strings.Join(events, ","), hubproto.EventUsageAvailable) {
		t.Errorf("subscribed events = %v", events)
	}

	st := s.DB.S()
	tot, err := st.SumCalls(ctx, orgKey, hour, hour+3600_000, "")
	if err != nil {
		t.Fatal(err)
	}
	// opus-4-6: 1M in × $5 + 0.1M out × $25 = $7.5; sonnet-4-6: 1M × $3 = $3; the unknown model has no price.
	if tot.Calls != 3 || tot.Unpriced != 1 || tot.ListCostMicros != 10_500_000 {
		t.Errorf("totals = %+v", tot)
	}

	s1, err := st.SessionByKey(ctx, orgKey, "claude", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if s1.UserID != bob.ID || s1.Attribution != "exact" || s1.SpaceID != spaceID || s1.AccountID == "" || s1.Funding != "personal" || s1.Metrics != `{"prompts":3}` {
		t.Errorf("s1 = %+v", s1)
	}
	s2, err := st.SessionByKey(ctx, orgKey, "claude", "s2")
	if err != nil {
		t.Fatal(err)
	}
	if s2.UserID != ann.ID || s2.Attribution != "guessed" || s2.AccountID != "" {
		t.Errorf("s2 = %+v", s2)
	}

	acc, err := st.AccountByRef(ctx, orgKey, "anthropic", "claude:acc-1")
	if err != nil {
		t.Fatal(err)
	}
	if !acc.Discovered || acc.Kind != "subscription" || acc.OwnerUserID != bob.ID || acc.ID != s1.AccountID {
		t.Errorf("account = %+v", acc)
	}
	ws, err := st.Windows(ctx, acc.ID)
	if err != nil || len(ws) != 1 || *ws[0].UsedPct != 42 || ws[0].MachineID != a.MachineID {
		t.Errorf("windows = %+v, %v", ws, err)
	}
	key, err := st.AccountByRef(ctx, orgKey, "anthropic", "key:sha256:0123456789abcdef")
	if err != nil || key.Kind != "api_key" {
		t.Fatalf("key account = %+v, %v", key, err)
	}
	if bs, err := st.Balances(ctx, key.ID); err != nil || len(bs) != 1 || *bs[0].Available != 12.5 {
		t.Errorf("balances = %+v, %v", bs, err)
	}

	// The hourly sums: the subagent's call is money of its parent, Bob, on the subscription (paid 0: the fee is split
	// when the screens read).
	rows, err := st.RollupRows(ctx, orgKey, hour, hour+3600_000)
	if err != nil {
		t.Fatal(err)
	}
	var calls, unpriced, list, paid int64
	for _, r := range rows {
		if r.UserID != bob.ID || r.AccountID != acc.ID || r.SpaceID != spaceID {
			t.Errorf("rollup row = %+v", r)
		}
		calls, unpriced, list, paid = calls+r.Calls, unpriced+r.Unpriced, list+r.ListCostMicros, paid+r.PaidCostMicros
	}
	if calls != 3 || unpriced != 1 || list != 10_500_000 || paid != 0 {
		t.Errorf("rollup = %d calls, %d unpriced, list %d, paid %d", calls, unpriced, list, paid)
	}

	// New records and usage.available: the hub reads from its cursor, not from the beginning.
	reads := len(o.reads)
	o.add(now, call("s1", "m4", "claude-opus-4-6", t0+4000, 0, 40_000))
	a.Emit(map[string]any{"event": hubproto.EventUsageAvailable, "epoch": "e1", "seq": 11})
	s.Eventually("the new record acked", func() bool { return o.lastAck().Seq == 11 })
	o.mu.Lock()
	if p := o.reads[reads]; p.Epoch != "e1" || p.After != 10 {
		t.Errorf("read after usage.available = %+v", p)
	}
	o.mu.Unlock()
	if tot, _ := st.SumCalls(ctx, orgKey, hour, hour+3600_000, ""); tot.Calls != 4 || tot.ListCostMicros != 11_500_000 {
		t.Errorf("after the second batch = %+v", tot)
	}

	// The data folder wiped: a new epoch from seq 1. The session's newer record wins although its seq is lower, the
	// calls seen before add nothing.
	o.renew("e2")
	o.add(now+1000, session("s1", "", "ann@example.com", "claude:acc-1", t0))
	o.add(now+1000, call("s1", "m1", "claude-opus-4-6", t0+1000, 1_000_000, 100_000))
	a.Emit(map[string]any{"event": hubproto.EventUsageAvailable, "epoch": "e2", "seq": 2})
	s.Eventually("the new epoch acked", func() bool { a := o.lastAck(); return a.Epoch == "e2" && a.Seq == 2 })
	if s1, _ := st.SessionByKey(ctx, orgKey, "claude", "s1"); s1.UserID != ann.ID {
		t.Errorf("s1 after the new epoch = %+v", s1)
	}
	if tot, _ := st.SumCalls(ctx, orgKey, hour, hour+3600_000, ""); tot.Calls != 4 {
		t.Errorf("after the new epoch = %+v", tot)
	}
	// The sums moved with the session: Ann's now, the subagent's call with it.
	rows, _ = st.RollupRows(ctx, orgKey, hour, hour+3600_000)
	for _, r := range rows {
		if r.UserID != ann.ID {
			t.Errorf("rollup row after the session moved = %+v", r)
		}
	}
	if cur, _ := st.CursorOf(ctx, a.MachineID, "usage"); cur.Epoch != "e2" || cur.Seq != 2 || cur.AckedAt == nil {
		t.Errorf("cursor = %+v", cur)
	}
}

// A machine whose clock runs ahead has its batch moved back to the hub's time, and the rows say so.
func TestIngestShiftsAClockAhead(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_USAGE_INGEST", "on"))
	ctx := context.Background()
	ann := s.SignIn("ann@example.com")
	spaceID, _ := ann.NewSpace("Acme")
	sp, _ := s.DB.S().SpaceByID(ctx, spaceID)

	now := s.Clock.Now().UnixMilli()
	ahead := int64(3 * 3600_000)
	o := &outbox{epoch: "e1"}
	o.add(now+ahead, call("s1", "m1", "claude-opus-4-6", now+ahead-1000, 10, 10))
	usageMachine(t, s, ann, spaceID, o)
	s.Eventually("acked", func() bool { return o.lastAck().Seq == 1 })

	var ts int64
	var shifted int
	if err := s.DB.QueryRow(ctx, `SELECT ts, ts_shifted FROM usage_calls WHERE msg_key = 'm1'`).Scan(&ts, &shifted); err != nil {
		t.Fatal(err)
	}
	if ts != now-1000 || shifted != 1 {
		t.Errorf("ts = %d (want %d), shifted = %d", ts, now-1000, shifted)
	}
	h := store.HourMs(now - 1000)
	if rows, _ := s.DB.S().RollupRows(ctx, store.OrgKey(sp), h, h+3600_000); len(rows) != 1 || rows[0].Calls != 1 {
		t.Errorf("rollup rows = %+v", rows)
	}
}

// With HUB_USAGE_INGEST off the hub neither subscribes to usage.available nor reads any outbox.
func TestIngestOff(t *testing.T) {
	s := hubtest.New(t)
	ann := s.SignIn("ann@example.com")
	spaceID, _ := ann.NewSpace("Acme")
	o := &outbox{epoch: "e1"}
	o.add(s.Clock.Now().UnixMilli(), call("s1", "m1", "claude-opus-4-6", s.Clock.Now().UnixMilli(), 10, 10))
	a := usageMachine(t, s, ann, spaceID, o)
	s.Eventually("subscribed", func() bool { return len(a.Calls("link.subscribe")) > 0 })
	a.Emit(map[string]any{"event": hubproto.EventUsageAvailable, "epoch": "e1", "seq": 1})
	s.Eventually("connect routine done", func() bool { return len(a.Calls("proxy.get")) > 0 })
	time.Sleep(200 * time.Millisecond)
	if n := len(a.Calls(hubproto.OpUsageRead)); n != 0 {
		t.Errorf("usage.read called %d times", n)
	}
	var events []string
	_ = json.Unmarshal(a.Calls("link.subscribe")[0].Params["events"], &events)
	if strings.Contains(strings.Join(events, ","), "usage") {
		t.Errorf("subscribed events = %v", events)
	}
	var n int
	if err := s.DB.QueryRow(context.Background(), `SELECT COUNT(*) FROM prices`).Scan(&n); err != nil || n != 0 {
		t.Errorf("prices = %d, %v", n, err)
	}
}

func toolFact(session, day, kind, name string, calls, errs int64) hubproto.UsageRecord {
	return hubproto.UsageRecord{Kind: hubproto.UsageTool, Tool: &hubproto.UsageToolFact{
		Provider: "claude", SessionKey: session, Day: day, ToolKind: kind, ToolName: name, Calls: calls, Errors: errs,
	}}
}

// Tool facts are attributed to the person and team of their session, matched to the catalog by name, and a later fact of
// the same session, day and tool replaces the earlier.
func TestIngestToolFacts(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_USAGE_INGEST", "on"))
	ctx := context.Background()
	ann := s.SignIn("ann@example.com")
	bob := s.SignIn("bob@example.com")
	spaceID, _ := ann.NewSpace("Acme")
	ann.Join(spaceID, bob)
	sp, err := s.DB.S().SpaceByID(ctx, spaceID)
	if err != nil {
		t.Fatal(err)
	}
	orgKey := store.OrgKey(sp)
	st := s.DB.S()
	skill, err := st.AddCatalogItem(ctx, store.CatalogItem{OrgID: orgKey, Kind: store.CatalogSkill, Name: "Review", CreatedAt: s.Clock.Now()})
	if err != nil {
		t.Fatal(err)
	}

	now := s.Clock.Now().UnixMilli()
	day := s.Clock.Now().UTC().Format("2006-01-02")
	o := &outbox{epoch: "e1"}
	o.add(now, session("s1", "", "bob@example.com", "", now-60_000))
	o.add(now, toolFact("s1", day, "skill", "review", 2, 0))
	o.add(now, toolFact("s1", day, "skill", "review", 3, 1)) // the same day again: replaces
	o.add(now, toolFact("s1", day, "skill", "acme:review", 1, 0))
	o.add(now, toolFact("s1", day, "mcp", "grafana", 4, 9)) // errors cannot exceed calls
	o.add(now, toolFact("s1", day, "bash", "Bash", 7, 0))
	o.add(now, toolFact("s1", day, "weird", "x", 1, 0))              // an unknown kind is "other"
	o.add(now, toolFact("s1", "not-a-day", "skill", "review", 1, 0)) // skipped
	o.add(now, toolFact("s1", day, "skill", "review-none", 0, 0))    // no calls: skipped
	o.add(now, toolFact("nobody", day, "skill", "review", 5, 0))     // a session the hub has no record of: the machine's creator
	a := usageMachine(t, s, ann, spaceID, o)
	s.Eventually("the outbox acked", func() bool { return o.lastAck().Seq == 10 })

	rows, err := st.ToolUsageOf(ctx, orgKey, "2000-01-01")
	if err != nil {
		t.Fatal(err)
	}
	type key struct{ kind, name, user string }
	got := map[key]store.ToolUsageRow{}
	for _, r := range rows {
		got[key{r.ToolKind, r.ToolName, r.UserID}] = r
	}
	if len(rows) != 6 {
		t.Fatalf("%d rows: %+v", len(rows), rows)
	}
	s1, err := st.SessionByKey(ctx, orgKey, "claude", "s1")
	if err != nil {
		t.Fatal(err)
	}
	r := got[key{"skill", "review", bob.ID}]
	if r.Calls != 3 || r.Errors != 1 || r.ItemID != skill.ID || r.TeamID != s1.TeamID || r.SpaceID != spaceID || r.Day != day || r.LastAt != now {
		t.Errorf("skill row = %+v", r)
	}
	if r := got[key{"skill", "acme:review", bob.ID}]; r.Calls != 1 || r.ItemID != skill.ID {
		t.Errorf("qualified skill row = %+v", r)
	}
	if r := got[key{"mcp", "grafana", bob.ID}]; r.Calls != 4 || r.Errors != 4 || r.ItemID != "" {
		t.Errorf("mcp row = %+v", r)
	}
	if r := got[key{"bash", "Bash", bob.ID}]; r.Calls != 7 || r.ItemID != "" {
		t.Errorf("bash row = %+v", r)
	}
	if r := got[key{"other", "x", bob.ID}]; r.Calls != 1 {
		t.Errorf("other row = %+v", r)
	}
	if r := got[key{"skill", "review", ann.ID}]; r.Calls != 5 || r.ItemID != skill.ID {
		t.Errorf("row of a session the hub does not know = %+v", r)
	}

	// An old hub's behaviour is the same code path as "a kind this hub does not know": a tool record without a body is
	// skipped, not an error.
	o.add(now, hubproto.UsageRecord{Kind: hubproto.UsageTool})
	a.Emit(map[string]any{"event": hubproto.EventUsageAvailable, "epoch": "e1", "seq": 11})
	s.Eventually("the empty record acked", func() bool { return o.lastAck().Seq == 11 })
}

// A session's merge request keys are kept per session, a subagent's count for its parent, and a newer record of the
// session replaces the set.
func TestIngestMRKeys(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_USAGE_INGEST", "on"))
	ctx := context.Background()
	ann := s.SignIn("ann@example.com")
	bob := s.SignIn("bob@example.com")
	spaceID, _ := ann.NewSpace("Acme")
	ann.Join(spaceID, bob)
	sp, err := s.DB.S().SpaceByID(ctx, spaceID)
	if err != nil {
		t.Fatal(err)
	}
	orgKey := store.OrgKey(sp)
	now := s.Clock.Now().UnixMilli()
	main := session("m1", "", "bob@example.com", "", now-60_000)
	main.Session.MRKeys = []string{"h/a/!1", "h/a/!2", "https://not-a-key"}
	sub := session("c1", "m1", "bob@example.com", "", now-50_000)
	sub.Session.MRKeys = []string{"h/a/!3"}
	o := &outbox{epoch: "e1"}
	o.add(now, main)
	o.add(now, sub)
	usageMachine(t, s, ann, spaceID, o)
	s.Eventually("the outbox acked", func() bool { return o.lastAck().Seq == 2 })

	count := func(where string, args ...any) int {
		var n int
		args = append([]any{orgKey}, args...)
		if err := s.DB.S().Q().QueryRow(ctx, `SELECT COUNT(*) FROM agent_session_mrs WHERE org_key = ? AND `+where, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count("owner_key = ?", "m1"); n != 3 {
		t.Errorf("m1 owns %d keys, want 3 (its own two and the subagent's)", n)
	}
	if n := count("session_key = ?", "m1"); n != 2 {
		t.Errorf("m1 mentions %d keys, want 2 (the text that is not a key is dropped)", n)
	}
}
