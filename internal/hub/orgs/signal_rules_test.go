package orgs_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/analytics"
	"github.com/ly-solutions/harnessly/internal/hub/features"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// liveOn switches the new interface and the stream of the organization on, as an operator does.
func (v veraCase) liveOn(t *testing.T) {
	t.Helper()
	for _, f := range []string{features.UIHX, features.OrgEvents} {
		if err := v.s.Hub.Features.Set(context.Background(), v.org, f, true, ""); err != nil {
			t.Fatal(err)
		}
	}
}

// check is one run of the signal checker for the organization.
func (v veraCase) check(t *testing.T) {
	t.Helper()
	if err := v.s.Hub.Analytics.CheckOrg(context.Background(), v.org); err != nil {
		t.Fatalf("check: %v", err)
	}
}

// rawSignals are the signals as the endpoint writes them, to look at the action's fields.
func (v veraCase) rawSignals(t *testing.T, p *hubtest.Person, query string) []map[string]any {
	t.Helper()
	r := p.Do("GET", v.url("/signals"+query), nil)
	if r.Code != http.StatusOK {
		t.Fatalf("signals: %s", r)
	}
	var out struct{ Signals []map[string]any }
	r.JSON(&out)
	return out.Signals
}

func rawFind(sigs []map[string]any, rule, subject string) map[string]any {
	for _, s := range sigs {
		if s["rule"] == rule && s["subject"].(map[string]any)["id"] == subject {
			return s
		}
	}
	return nil
}

func rawAction(t *testing.T, s map[string]any) map[string]any {
	t.Helper()
	if s == nil {
		t.Fatal("no signal")
	}
	a, _ := s["action"].(map[string]any)
	if a == nil {
		t.Fatalf("no action in %v", s)
	}
	return a
}

func (v veraCase) countRule(t *testing.T, rule string) (open int) {
	t.Helper()
	ctx := context.Background()
	rows, err := v.s.DB.S().OpenSignals(ctx, v.org)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range rows {
		if g.Rule == rule {
			open++
		}
	}
	return open
}

func TestWindow90DedupesResolvesAndRespectsSnooze(t *testing.T) {
	v := newVera(t)
	ctx := context.Background()
	st := v.s.DB.S()
	v.liveOn(t)

	// A second shared subscription with room: the load can move there.
	spare := "a2spare"
	if err := st.CreateAccount(ctx, &store.Account{ID: spare, OrgKey: v.org, ScopeKind: store.ScopeOrg, ScopeID: v.org, Provider: "anthropic",
		Kind: "subscription", AccountRef: "spare-ref", Label: "Claude Max №3", Funding: store.FundingOrg, MonthlyFeeMicros: 200e6}); err != nil {
		t.Fatal(err)
	}
	low := 30.0
	resets := v.s.Clock.Now().Add(2 * time.Hour).UnixMilli()
	put := func(id string, pct float64) {
		v.s.Clock.Advance(time.Minute) // a measurement older than the one kept is ignored
		if _, err := st.PutWindow(ctx, store.AccountWindow{AccountID: id, Scope: "all", Name: store.PeriodFiveHour, LengthSec: 5 * 3600, Unit: "pct",
			UsedPct: &pct, ResetsAt: &resets, MeasuredAt: v.s.Clock.Now().UnixMilli(), Source: "statusline", Confidence: "exact"}); err != nil {
			t.Fatal(err)
		}
	}
	put(spare, low)

	// Screens do not look for the rule: until the checker has run there is nothing.
	if rawFind(v.rawSignals(t, v.anna, ""), "window_90", v.max2) != nil {
		t.Fatal("a screen found a checker's rule")
	}
	v.check(t)
	sig := rawFind(v.rawSignals(t, v.anna, ""), "window_90", v.max2)
	if sig == nil || sig["severity"] != "warn" {
		t.Fatalf("signal = %v", sig)
	}
	assertSignalShapes(t, []map[string]any{sig})
	act := rawAction(t, sig)
	if act["kind"] != "move_account" || act["fromAccountId"] != v.max2 || act["toAccountId"] != spare || act["target"] != spare || act["label"] == "" {
		t.Errorf("action = %v", act)
	}
	if !strings.Contains(sig["detail"].(string), "Claude Max №3") {
		t.Errorf("detail = %v", sig["detail"])
	}
	// The member does not see the organization's account.
	if rawFind(v.rawSignals(t, v.vera, ""), "window_90", v.max2) != nil {
		t.Error("a member sees the organization's window")
	}

	// One row per rule and object, however often the checker runs; the first moment stays.
	first := sig["firstAt"]
	v.s.Clock.Advance(10 * time.Minute)
	put(v.max2, 92)
	v.check(t)
	v.check(t)
	if n := v.countRule(t, "window_90"); n != 1 {
		t.Errorf("%d open rows", n)
	}
	sig = rawFind(v.rawSignals(t, v.anna, ""), "window_90", v.max2)
	if sig == nil || sig["firstAt"] != first {
		t.Fatalf("again = %v", sig)
	}

	// Snoozed: the checker keeps finding it and leaves the snooze alone.
	if r := v.snooze(v.anna, sig["id"].(string), "2026-11-05T09:00:00Z"); r.Code != http.StatusOK {
		t.Fatalf("snooze: %s", r)
	}
	v.check(t)
	if rawFind(v.rawSignals(t, v.anna, ""), "window_90", v.max2) != nil {
		t.Error("a snoozed signal is shown")
	}
	sig = rawFind(v.rawSignals(t, v.anna, "?all=1"), "window_90", v.max2)
	if sig == nil || sig["snoozedUntil"] != "2026-11-05T09:00:00Z" {
		t.Errorf("snooze lost: %v", sig)
	}

	// Past the critical line the signal got worse: the snooze is lifted and the severity follows.
	put(v.max2, 99)
	v.check(t)
	if sig = rawFind(v.rawSignals(t, v.anna, ""), "window_90", v.max2); sig == nil || sig["severity"] != "critical" || sig["snoozedUntil"] != nil {
		t.Errorf("critical = %v", sig)
	}

	// The window drops: the signal is resolved, kept in the store, and a screen read does not bring it back.
	put(v.max2, 50)
	v.check(t)
	if rawFind(v.rawSignals(t, v.anna, ""), "window_90", v.max2) != nil {
		t.Error("signal survived the drop")
	}
	g, err := st.SignalByKey(ctx, v.org, "window_90", v.max2)
	if err != nil || g.ResolvedAt == nil {
		t.Errorf("stored = %+v (%v)", g, err)
	}

	// A window that stopped being measured says nothing about now.
	put(v.max2, 95)
	v.s.Clock.Advance(25 * time.Hour)
	v.check(t)
	if rawFind(v.rawSignals(t, v.anna, ""), "window_90", v.max2) != nil {
		t.Error("a stale window made a signal")
	}
}

func TestCheckerIsOnlyForOrganizationsWithTheInterface(t *testing.T) {
	v := newVera(t)
	ctx := context.Background()
	used, resets := 99.0, v.s.Clock.Now().Add(time.Hour).UnixMilli()
	if _, err := v.s.DB.S().PutWindow(ctx, store.AccountWindow{AccountID: v.max2, Scope: "all", Name: store.PeriodFiveHour, LengthSec: 5 * 3600, Unit: "pct",
		UsedPct: &used, ResetsAt: &resets, MeasuredAt: v.s.Clock.Now().UnixMilli(), Source: "statusline", Confidence: "exact"}); err != nil {
		t.Fatal(err)
	}
	// RunLive checks only the organizations whose flag is on; checkAll is its first step.
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { v.s.Hub.Analytics.RunLive(runCtx, nil); close(done) }()
	time.Sleep(300 * time.Millisecond)
	cancel()
	<-done
	if n := v.countRule(t, "window_90"); n != 0 {
		t.Errorf("checker ran for an organization with the flag off: %d", n)
	}
}

func TestOtherRulesAndTheirActions(t *testing.T) {
	v := newVera(t)
	ctx := context.Background()
	st := v.s.DB.S()
	v.liveOn(t)
	now := v.s.Clock.Now()

	// machine_offline and machine_idle: a machine of the second tenant not heard from for an hour, never used.
	seen := now.Add(-time.Hour)
	if err := st.CreateMachine(ctx, &store.Machine{ID: "m-off", SpaceID: v.otherTenant, Name: "Старая", KeyPub: "k", EncPub: "e",
		AgentVersion: "1.0.0", CreatedAt: seen.Add(-30 * 24 * time.Hour), LastSeenAt: &seen}); err != nil {
		t.Fatal(err)
	}
	// invite_pending: an invitation by email four days old; a link invitation is not one.
	for _, iv := range []store.Invite{
		{SpaceID: v.tenant, Kind: "email", Email: "dan@example.org", TokenHash: store.HashToken("t1"), Role: "member", ExpiresAt: now.Add(48 * time.Hour), CreatedBy: v.anna.ID},
		{SpaceID: v.tenant, Kind: "link", TokenHash: store.HashToken("t2"), Role: "member", ExpiresAt: now.Add(48 * time.Hour), CreatedBy: v.anna.ID},
	} {
		if err := st.CreateInvite(ctx, &iv); err != nil {
			t.Fatal(err)
		}
		if _, err := v.s.DB.Exec(ctx, `UPDATE invites SET created_at = ? WHERE id = ?`, now.Add(-4*24*time.Hour).UnixMilli(), iv.ID); err != nil {
			t.Fatal(err)
		}
	}
	// unknown_account: an account a machine used that nobody registered.
	if err := st.CreateAccount(ctx, &store.Account{ID: "a-unknown", OrgKey: v.org, ScopeKind: store.ScopeTenant, ScopeID: v.tenant, Provider: "anthropic",
		Kind: "subscription", AccountRef: "stranger", Funding: store.FundingPersonal, Discovered: true}); err != nil {
		t.Fatal(err)
	}
	// model_no_price: calls of a model the hub has no price for.
	put := func(user, key string, when time.Time, model string, list *int64, metrics string, ended bool) {
		var end *int64
		if ended {
			e := when.Add(10 * time.Minute).UnixMilli()
			end = &e
		}
		if _, err := st.PutSession(ctx, store.AgentSession{OrgKey: v.org, Provider: "claude", SessionKey: key, UserID: user, SpaceID: v.tenant, TeamID: v.team.ID,
			AccountID: v.key1, Funding: store.FundingOrg, Origin: "live", Attribution: "exact", StartedAt: when.UnixMilli(), EndedAt: end, Metrics: metrics,
			Seq: 1, UpdatedAt: when.UnixMilli()}); err != nil {
			t.Fatal(err)
		}
		if model == "" {
			return
		}
		if _, err := st.AddCall(ctx, store.UsageCall{OrgKey: v.org, Provider: "claude", MsgKey: key + "m", SessionKey: key, Ts: when.UnixMilli(), Model: model,
			Input: 10, ListCostMicros: list, ReceivedAt: when.UnixMilli()}); err != nil {
			t.Fatal(err)
		}
		if err := st.Rollup(ctx, v.org, []int64{store.HourMs(when.UnixMilli())}); err != nil {
			t.Fatal(err)
		}
	}
	put(v.vera.ID, "np1", now.Add(-2*time.Hour), "model-x-9", nil, "{}", true)
	put(v.vera.ID, "np2", now.Add(-3*time.Hour), "model-x-9", nil, "{}", true)

	// chat_waiting: a chat that has waited for its person for two hours, and one that finished.
	machine := v.anna.Machine(v.tenant, nil).MachineID
	waiting := `{"waitingSince":` + itoa(now.Add(-2*time.Hour).UnixMilli()) + `}`
	_, err := st.PutSession(ctx, store.AgentSession{OrgKey: v.org, Provider: "claude", SessionKey: "wait1", MachineID: machine, UserID: v.vera.ID, SpaceID: v.tenant,
		TeamID: v.team.ID, Funding: store.FundingOrg, Origin: "live", Attribution: "exact", StartedAt: now.Add(-3 * time.Hour).UnixMilli(), Metrics: waiting, Seq: 1,
		UpdatedAt: now.UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	done := now.Add(-time.Hour).UnixMilli()
	if _, err := st.PutSession(ctx, store.AgentSession{OrgKey: v.org, Provider: "claude", SessionKey: "wait2", MachineID: machine, UserID: v.vera.ID, SpaceID: v.tenant,
		TeamID: v.team.ID, Funding: store.FundingOrg, Origin: "live", Attribution: "exact", StartedAt: now.Add(-3 * time.Hour).UnixMilli(), EndedAt: &done, Metrics: waiting,
		Seq: 1, UpdatedAt: now.UnixMilli()}); err != nil {
		t.Fatal(err)
	}

	// tool_errors_up: five sessions a week for two weeks, the share of failed calls up from 1% to 20%.
	for i := 0; i < 6; i++ {
		put(v.vera.ID, "te-old"+itoa(int64(i)), now.Add(-time.Duration(8+i)*24*time.Hour), "", nil, `{"toolErrors":1,"toolCalls":100}`, true)
		put(v.vera.ID, "te-new"+itoa(int64(i)), now.Add(-time.Duration(2+i)*24*time.Hour).Add(-time.Hour), "", nil, `{"toolErrors":20,"toolCalls":100}`, true)
	}

	v.check(t)
	all := v.rawSignals(t, v.anna, "")

	off := rawFind(all, "machine_offline", "m-off")
	if off == nil || off["severity"] != "warn" || off["scope"].(map[string]any)["id"] != v.otherTenant {
		t.Fatalf("machine_offline = %v", off)
	}
	if a := rawAction(t, off); a["kind"] != "open_machine" || a["machineId"] != "m-off" || a["target"] != "m-off" {
		t.Errorf("offline action = %v", a)
	}
	if idle := rawFind(all, "machine_idle", "m-off"); idle == nil || idle["severity"] != "info" {
		t.Errorf("machine_idle = %v", idle)
	}

	var inviteID string
	ivs, _ := st.ActiveInvites(ctx, v.tenant, now)
	for _, iv := range ivs {
		if iv.Kind == "email" {
			inviteID = iv.ID
		}
	}
	inv := rawFind(all, "invite_pending", inviteID)
	if inv == nil || inv["severity"] != "info" {
		t.Fatalf("invite_pending = %v in %v", inv, all)
	}
	if a := rawAction(t, inv); a["kind"] != "resend_invite" || a["inviteId"] != inviteID || a["tenantId"] != v.tenant {
		t.Errorf("invite action = %v", a)
	}
	for _, s := range all {
		if s["rule"] == "invite_pending" && s["subject"].(map[string]any)["id"] != inviteID {
			t.Errorf("a link invitation made a signal: %v", s)
		}
	}

	unk := rawFind(all, "unknown_account", "a-unknown")
	if unk == nil || rawAction(t, unk)["kind"] != "register_account" || rawAction(t, unk)["accountId"] != "a-unknown" {
		t.Errorf("unknown_account = %v", unk)
	}

	np := rawFind(all, "model_no_price", "model-x-9")
	if np == nil || rawAction(t, np)["kind"] != "open_prices" || rawAction(t, np)["model"] != "model-x-9" || !strings.Contains(np["detail"].(string), "2 вызовов") {
		t.Errorf("model_no_price = %v", np)
	}

	te := rawFind(all, "tool_errors_up", v.org)
	if te == nil || rawAction(t, te)["kind"] != "open_kpi" || rawAction(t, te)["kpi"] != "tool_errors" {
		t.Errorf("tool_errors_up = %v", te)
	}

	cw := rawFind(all, "chat_waiting", v.vera.ID)
	if cw == nil || cw["severity"] != "info" || !strings.Contains(cw["title"].(string), "2 ч") {
		t.Fatalf("chat_waiting = %v", cw)
	}
	if a := rawAction(t, cw); a["kind"] != "open_chat" || a["machineId"] != machine || a["sessionKey"] != "wait1" || a["target"] != "wait1" {
		t.Errorf("chat action = %v", a)
	}
	n := 0
	for _, s := range all {
		if s["rule"] == "chat_waiting" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d chat_waiting signals; a finished chat waits for nobody", n)
	}

	// invoice_mismatch has no provider bills to compare with.
	if open := v.countRule(t, "invoice_mismatch"); open != 0 {
		t.Error("invoice_mismatch found without data")
	}

	assertSignalShapes(t, all)

	// The person the chat waits for sees it, and only the signals about herself; a member never sees the machine's.
	mine := v.rawSignals(t, v.vera, "")
	if rawFind(mine, "chat_waiting", v.vera.ID) == nil || rawFind(mine, "machine_offline", "m-off") != nil || rawFind(mine, "unknown_account", "a-unknown") != nil {
		t.Errorf("member's signals = %v", mine)
	}

	// Things come right and the signals go: the invitation is revoked, the chat answered, the account registered.
	if _, err := v.s.DB.Exec(ctx, `UPDATE invites SET revoked_at = ? WHERE id = ?`, now.UnixMilli(), inviteID); err != nil {
		t.Fatal(err)
	}
	end := now.UnixMilli()
	if _, err := st.PutSession(ctx, store.AgentSession{OrgKey: v.org, Provider: "claude", SessionKey: "wait1", MachineID: machine, UserID: v.vera.ID, SpaceID: v.tenant,
		TeamID: v.team.ID, Funding: store.FundingOrg, Origin: "live", Attribution: "exact", StartedAt: now.Add(-3 * time.Hour).UnixMilli(), EndedAt: &end, Metrics: waiting, Seq: 2,
		UpdatedAt: now.UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	v.check(t)
	after := v.rawSignals(t, v.anna, "")
	if rawFind(after, "invite_pending", inviteID) != nil || rawFind(after, "chat_waiting", v.vera.ID) != nil {
		t.Errorf("resolved signals remain: %v", after)
	}
	if rawFind(after, "machine_offline", "m-off") == nil {
		t.Error("an unrelated signal was resolved")
	}
}

// assertSignalShapes compares the signals the endpoint wrote with the web fixture of the contract: the same keys on the
// signal, its scope, its subject and, per action kind, on the action. Only the rules the fixture holds are compared.
func assertSignalShapes(t *testing.T, got []map[string]any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "app", "hub-api", "fixtures", "analytics", "signals-admin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fx struct{ Signals []map[string]any }
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	want := map[string]map[string]any{}
	for _, g := range fx.Signals {
		want[g["rule"].(string)] = g
	}
	compared := 0
	for _, g := range got {
		w := want[g["rule"].(string)]
		if w == nil {
			continue
		}
		compared++
		rule := g["rule"].(string)
		if keys(g) != keys(w) {
			t.Errorf("%s: keys %s, fixture %s", rule, keys(g), keys(w))
		}
		if g["action"] == nil { // a window with no spare account to move to has no action
			continue
		}
		for _, k := range []string{"scope", "subject", "action"} {
			gm, _ := g[k].(map[string]any)
			wm, _ := w[k].(map[string]any)
			if gm == nil || wm == nil || keys(gm) != keys(wm) {
				t.Errorf("%s.%s: keys %v, fixture %v", rule, k, gm, wm)
			}
		}
		if g["action"].(map[string]any)["kind"] != w["action"].(map[string]any)["kind"] {
			t.Errorf("%s: action %v, fixture %v", rule, g["action"], w["action"])
		}
	}
	if compared == 0 {
		t.Fatal("no signal was compared with the fixture")
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestSignalNewIsAnnouncedOnceAndOnlyToWhoMaySee(t *testing.T) {
	v := newVera(t)
	ctx := context.Background()
	st := v.s.DB.S()
	v.liveOn(t)
	now := v.s.Clock.Now()

	admin, code := openStream(t, v.anna, v.url("/events"), "")
	if code != http.StatusOK {
		t.Fatalf("stream: %d", code)
	}
	member, _ := openStream(t, v.vera, v.url("/events"), "")
	otherLead, _ := openStream(t, v.gleb, v.url("/events?scope=tenant:"+v.otherTenant), "")
	waitListeners(t, v.s.Hub.OrgEvents, v.org, 3)

	machine := v.anna.Machine(v.tenant, nil).MachineID
	if _, err := st.PutSession(ctx, store.AgentSession{OrgKey: v.org, Provider: "claude", SessionKey: "w", MachineID: machine, UserID: v.vera.ID, SpaceID: v.tenant,
		TeamID: v.team.ID, Funding: store.FundingOrg, Origin: "live", Attribution: "exact", StartedAt: now.Add(-3 * time.Hour).UnixMilli(),
		Metrics: `{"waitingSince":` + itoa(now.Add(-2*time.Hour).UnixMilli()) + `}`, Seq: 1, UpdatedAt: now.UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	v.check(t)

	// Every new signal is one event; the limits' own signals of this organization are among them.
	byRule := func(s *stream) map[string]analytics.Signal {
		out := map[string]analytics.Signal{}
		for {
			f, ok := s.next()
			if !ok {
				return out
			}
			var ev struct{ Signal analytics.Signal }
			if f.kind != "signal.new" || json.Unmarshal([]byte(f.data), &ev) != nil {
				t.Errorf("unexpected frame %+v", f)
			}
			out[ev.Signal.Rule] = ev.Signal
		}
	}
	got := byRule(admin)
	if cw, ok := got["chat_waiting"]; !ok || cw.Action == nil || cw.Action.Kind != "open_chat" {
		t.Errorf("admin got %+v", got)
	}
	if _, ok := got["over_80"]; !ok {
		t.Errorf("admin did not hear of the limits' signal: %+v", got)
	}
	// The person it waits for is told of hers (and of the limit she is near), but not of the tenant's others.
	mine := byRule(member)
	if _, ok := mine["chat_waiting"]; !ok {
		t.Errorf("member got %+v", mine)
	}
	if !otherLead.expectNone() {
		t.Error("the other tenant was told")
	}

	// The same finding again is not news.
	v.check(t)
	if !admin.expectNone() || !member.expectNone() {
		t.Error("a signal that was already open was announced again")
	}
}

func TestLimitRequestIsPublished(t *testing.T) {
	v := newVera(t)
	v.liveOn(t)
	lead, code := openStream(t, v.boris, v.url("/events?scope=tenant:"+v.tenant), "")
	if code != http.StatusOK {
		t.Fatalf("stream: %d", code)
	}
	other, _ := openStream(t, v.gleb, v.url("/events?scope=tenant:"+v.otherTenant), "")
	waitListeners(t, v.s.Hub.OrgEvents, v.org, 2)

	r := v.vera.Do("POST", "/api/me/limit-requests", map[string]any{"amountUsd": 150, "reason": "Релиз на этой неделе", "scope": scope("team", v.team.ID)})
	if r.Code != http.StatusCreated {
		t.Fatalf("request: %s", r)
	}
	var got []frame
	for {
		f, ok := lead.next()
		if !ok {
			break
		}
		got = append(got, f)
	}
	kinds := map[string]string{}
	for _, f := range got {
		kinds[f.kind] = f.data
	}
	var lr struct {
		SignalID, UserID, Name string
		Scope                  struct{ Kind, ID string }
	}
	if err := json.Unmarshal([]byte(kinds["limit.request"]), &lr); err != nil || lr.UserID != v.vera.ID || lr.Scope.Kind != "team" || lr.Scope.ID != v.team.ID || lr.SignalID == "" {
		t.Errorf("limit.request = %q (%v)", kinds["limit.request"], err)
	}
	if !strings.Contains(kinds["signal.new"], `"limit_request"`) {
		t.Errorf("signal.new = %q", kinds["signal.new"])
	}
	if !other.expectNone() {
		t.Error("the other tenant heard of the request")
	}
}

func TestUsageTickOnlyWhileSomebodyListens(t *testing.T) {
	v := newVera(t)
	ctx := context.Background()
	st := v.s.DB.S()
	v.liveOn(t)
	now := v.s.Clock.Now()

	// Today's spend, tokens and two chats going on.
	list := int64(3e6)
	if _, err := st.PutSession(ctx, store.AgentSession{OrgKey: v.org, Provider: "claude", SessionKey: "t1", UserID: v.vera.ID, SpaceID: v.tenant, TeamID: v.team.ID,
		AccountID: v.key1, Funding: store.FundingOrg, Origin: "live", Attribution: "exact", StartedAt: now.Add(-time.Hour).UnixMilli(), Metrics: "{}", Seq: 1,
		UpdatedAt: now.Add(-time.Minute).UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutSession(ctx, store.AgentSession{OrgKey: v.org, Provider: "claude", SessionKey: "t2", UserID: v.vera.ID, SpaceID: v.tenant, TeamID: v.team.ID,
		AccountID: v.key1, Funding: store.FundingOrg, Origin: "live", Attribution: "exact", StartedAt: now.Add(-time.Hour).UnixMilli(), Metrics: "{}", Seq: 1,
		UpdatedAt: now.Add(-2 * time.Minute).UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	stale := now.Add(-3 * time.Hour).UnixMilli() // not updated for hours: not going on
	if _, err := st.PutSession(ctx, store.AgentSession{OrgKey: v.org, Provider: "claude", SessionKey: "t3", UserID: v.boris.ID, SpaceID: v.tenant, TeamID: v.team.ID,
		AccountID: v.key1, Funding: store.FundingOrg, Origin: "live", Attribution: "exact", StartedAt: now.Add(-5 * time.Hour).UnixMilli(), Metrics: "{}", Seq: 1,
		UpdatedAt: stale}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddCall(ctx, store.UsageCall{OrgKey: v.org, Provider: "claude", MsgKey: "t1m", SessionKey: "t1", Ts: now.Add(-30 * time.Minute).UnixMilli(),
		Model: "claude-sonnet-4-6", Input: 1000, Output: 500, CacheRead: 200, ListCostMicros: &list, ReceivedAt: now.UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	if err := st.Rollup(ctx, v.org, []int64{store.HourMs(now.Add(-30 * time.Minute).UnixMilli())}); err != nil {
		t.Fatal(err)
	}

	bus := v.s.Hub.OrgEvents
	// Nobody listens: nothing is computed or kept.
	v.s.Hub.Analytics.Tick(ctx, bus.Active())

	admin, _ := openStream(t, v.anna, v.url("/events"), "")
	lead, _ := openStream(t, v.boris, v.url("/events?scope=tenant:"+v.tenant), "")
	member, _ := openStream(t, v.vera, v.url("/events"), "")
	waitListeners(t, bus, v.org, 3)
	if !admin.expectNone() {
		t.Fatal("a tick was published to nobody and replayed")
	}
	v.s.Hub.Analytics.Tick(ctx, bus.Active())

	var tick analytics.UsageTick
	f, ok := admin.next()
	if !ok || f.kind != "usage.tick" {
		t.Fatalf("admin got %+v (%v)", f, ok)
	}
	if err := json.Unmarshal([]byte(f.data), &tick); err != nil {
		t.Fatal(err)
	}
	if tick.SpendToday.USD != 3 || tick.TokensToday != 1700 || tick.ActiveNow != 1 || tick.ChatsNow != 2 {
		t.Errorf("admin tick = %+v", tick)
	}
	if !admin.expectNone() {
		t.Error("the tenant's numbers reached the organization's stream")
	}
	// The tenant's lead has the tenant's own; the member nothing of the money.
	f, ok = lead.next()
	if !ok || f.kind != "usage.tick" || json.Unmarshal([]byte(f.data), &tick) != nil || tick.ChatsNow != 2 || tick.SpendToday.USD != 3 {
		t.Errorf("lead got %+v (%v)", f, ok)
	}
	if !member.expectNone() {
		t.Error("a member got the numbers")
	}
}
