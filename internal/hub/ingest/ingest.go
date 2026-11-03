// Package ingest pulls the machines' spend facts into the hub (docs/hub/usage.md): on connect and on usage.available
// the hub reads a machine's outbox from its cursor, writes the batch in one transaction (calls priced by the price list,
// sessions with who started them, account windows and balances, the hourly sums, the cursor) and acks it.
package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/usage"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Stream is the name of the outbox in ingest_cursors.
const Stream = "usage"

// skewLimit is how far ahead of the hub a machine's clock may be before the hub shifts its batch.
const skewLimit = 5 * time.Minute

// Caller asks a machine's agent for an op (link.Registry).
type Caller interface {
	DoInto(ctx context.Context, machineID, op string, params, out any, timeout time.Duration) error
}

// Deps are what the service is built from.
type Deps struct {
	DB  *store.DB
	Reg Caller
	Log *slog.Logger
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Timeout of one usage.read or usage.ack; 0 means 30 s.
	Timeout time.Duration
}

// Service pulls the outboxes. At most one pull runs per machine; a trigger during a pull makes it go round once more.
type Service struct {
	db      *store.DB
	reg     Caller
	log     *slog.Logger
	now     func() time.Time
	timeout time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.Mutex
	running map[string]bool // machine → a pull is running
	again   map[string]bool // machine → triggered while running
}

func New(d Deps) *Service {
	s := &Service{db: d.DB, reg: d.Reg, log: d.Log, now: d.Now, timeout: d.Timeout, running: map[string]bool{}, again: map[string]bool{}}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.timeout == 0 {
		s.timeout = 30 * time.Second
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	return s
}

// Close stops the pulls and waits for them.
func (s *Service) Close() {
	s.cancel()
	s.wg.Wait()
}

// Connected is called once a machine said hello: an agent that keeps the outbox is read at once.
func (s *Service) Connected(machineID string, features []string) {
	if slices.Contains(features, hubproto.FeatureUsage) {
		s.Trigger(machineID)
	}
}

// Trigger starts a pull of the machine's outbox, or asks the running one to go round once more.
func (s *Service) Trigger(machineID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return
	}
	if s.running[machineID] {
		s.again[machineID] = true
		return
	}
	s.running[machineID] = true
	s.wg.Add(1)
	go s.loop(machineID)
}

func (s *Service) loop(machineID string) {
	defer s.wg.Done()
	for {
		if err := s.Pull(s.ctx, machineID); err != nil && s.ctx.Err() == nil {
			s.log.Warn("usage not pulled", "machine", machineID, "error", err)
		}
		s.mu.Lock()
		if !s.again[machineID] || s.ctx.Err() != nil {
			delete(s.running, machineID)
			delete(s.again, machineID)
			s.mu.Unlock()
			return
		}
		delete(s.again, machineID)
		s.mu.Unlock()
	}
}

// Pull reads the machine's outbox until it is empty: each batch is stored in one transaction, then acked.
func (s *Service) Pull(ctx context.Context, machineID string) error {
	for range 1000 { // a bound: 1000 batches of 1000 records is far beyond any backlog
		more, err := s.pullOne(ctx, machineID)
		if err != nil || !more {
			return err
		}
	}
	return nil
}

func (s *Service) pullOne(ctx context.Context, machineID string) (bool, error) {
	st := s.db.S()
	m, err := st.MachineByID(ctx, machineID)
	if err != nil {
		return false, err
	}
	if m.SpaceID == "" || m.RevokedAt != nil {
		return false, nil // a local machine nobody claimed has no organization to count its spend in
	}
	cur, err := st.CursorOf(ctx, machineID, Stream)
	if err != nil {
		return false, err
	}
	var res hubproto.UsageReadResult
	if err := s.reg.DoInto(ctx, machineID, hubproto.OpUsageRead, hubproto.UsageReadParams{Epoch: cur.Epoch, After: cur.Seq, Max: hubproto.UsageReadMax}, &res, s.timeout); err != nil {
		return false, err
	}
	if res.Epoch == "" {
		return false, errors.New("usage.read answered without an epoch")
	}
	newEpoch := res.Epoch != cur.Epoch
	after := cur.Seq
	if newEpoch {
		after = 0
	}
	if !newEpoch && res.Last <= cur.Seq && len(res.Records) == 0 {
		return false, nil
	}
	received := s.now().UTC()
	if err := s.db.Tx(ctx, func(tx store.Store) error {
		if newEpoch && cur.Epoch != "" {
			if err := tx.ResetSessionSeqs(ctx, machineID); err != nil {
				return err
			}
		}
		if err := s.store(ctx, tx, m, after, res.Records, received); err != nil {
			return err
		}
		return tx.SetCursor(ctx, store.Cursor{MachineID: machineID, Stream: Stream, Epoch: res.Epoch, Seq: max(res.Last, after)})
	}); err != nil {
		return false, err
	}
	if err := s.reg.DoInto(ctx, machineID, hubproto.OpUsageAck, hubproto.UsageAckParams{Epoch: res.Epoch, Seq: max(res.Last, after)}, nil, s.timeout); err != nil {
		// Stored but not acked: the agent keeps the records and sends them again; calls are deduplicated, sessions and
		// measurements are replaced only by newer ones, so a second time adds nothing.
		return false, fmt.Errorf("usage.ack: %w", err)
	}
	at := store.Ms(s.now())
	_ = st.SetCursor(ctx, store.Cursor{MachineID: machineID, Stream: Stream, Epoch: res.Epoch, Seq: max(res.Last, after), AckedAt: &at})
	return res.More, nil
}

// batch is what one transaction needs to know about the machine.
type batch struct {
	tx      store.Store
	m       store.Machine
	space   store.Space
	orgKey  string
	book    *usage.Book
	shift   int64 // added to every time of the batch
	now     int64
	users   map[string]string // email → user id ("" unknown)
	hours   []int64
	changed []string // session keys whose attribution may have moved
	catalog []store.CatalogItem
	loaded  bool // catalog was read
}

func (s *Service) store(ctx context.Context, tx store.Store, m store.Machine, after int64, records []hubproto.UsageRecord, received time.Time) error {
	sp, err := tx.SpaceByID(ctx, m.SpaceID)
	if err != nil {
		return err
	}
	prices, err := tx.Prices(ctx)
	if err != nil {
		return err
	}
	b := &batch{tx: tx, m: m, space: sp, orgKey: store.OrgKey(sp), book: usage.NewBook(prices), now: store.Ms(received), users: map[string]string{}}
	// A machine whose clock runs ahead writes records «from the future»; the batch is moved back by the difference. A
	// clock behind cannot be told from a backlog written earlier and is left as it is.
	var newest int64
	for _, r := range records {
		newest = max(newest, r.At)
	}
	if d := newest - b.now; d > skewLimit.Milliseconds() {
		b.shift = -d
	}
	last := after
	for _, r := range records {
		if r.Seq <= last {
			continue // out of order or already stored: the outbox's seq only grows
		}
		last = r.Seq
		if err := b.record(ctx, r); err != nil {
			return fmt.Errorf("record %d (%s): %w", r.Seq, r.Kind, err)
		}
	}
	if len(b.changed) > 0 {
		hs, err := tx.CallHours(ctx, b.orgKey, b.changed)
		if err != nil {
			return err
		}
		b.hours = append(b.hours, hs...)
	}
	return tx.Rollup(ctx, b.orgKey, b.hours)
}

func (b *batch) record(ctx context.Context, r hubproto.UsageRecord) error {
	switch r.Kind {
	case hubproto.UsageCall:
		if r.Call != nil {
			return b.call(ctx, *r.Call)
		}
	case hubproto.UsageSession:
		if r.Session != nil {
			return b.session(ctx, r.Seq, r.At, *r.Session)
		}
	case hubproto.UsageTool:
		if r.Tool != nil {
			return b.tool(ctx, r.At, *r.Tool)
		}
	case hubproto.UsageWindow:
		if r.Window != nil {
			return b.window(ctx, *r.Window)
		}
	case hubproto.UsageBalance:
		if r.Balance != nil {
			return b.balance(ctx, *r.Balance)
		}
	}
	return nil // a kind this hub does not know yet, from a newer agent: skipped, not an error
}

func (b *batch) call(ctx context.Context, c hubproto.UsageCallFact) error {
	if c.MsgKey == "" || c.Provider == "" {
		return nil
	}
	ts := c.Ts + b.shift
	pp := usage.PriceProvider(c.Provider, c.BaseURLHost)
	row := store.UsageCall{
		OrgKey: b.orgKey, Provider: c.Provider, MsgKey: c.MsgKey, SessionKey: c.SessionKey, MachineID: b.m.ID, Ts: ts,
		Model: c.Model, PriceProvider: pp, BaseURLHost: c.BaseURLHost, Input: c.Input, Output: c.Output, CacheRead: c.CacheRead,
		CacheWrite5m: c.CacheWrite5m, CacheWrite1h: c.CacheWrite1h, TsShifted: b.shift != 0, ReceivedAt: b.now,
	}
	cost, ver, unit, ok := b.book.Cost(pp, c.Model, ts, usage.Tokens{Input: c.Input, Output: c.Output, CacheRead: c.CacheRead, CacheWrite5m: c.CacheWrite5m, CacheWrite1h: c.CacheWrite1h})
	if ok && unit == "usd" { // money is kept in dollars; a price in credits is not money
		row.ListCostMicros, row.PriceVer = &cost, &ver
	}
	added, err := b.tx.AddCall(ctx, row)
	if err == nil && added {
		b.hours = append(b.hours, store.HourMs(ts))
	}
	return err
}

func (b *batch) session(ctx context.Context, seq, at int64, x hubproto.UsageSessionFact) error {
	if x.SessionKey == "" || x.Provider == "" {
		return nil
	}
	userID, attribution, err := b.person(ctx, x.Subject, x.Attribution)
	if err != nil {
		return err
	}
	teamID, err := b.team(ctx, userID)
	if err != nil {
		return err
	}
	row := store.AgentSession{
		OrgKey: b.orgKey, Provider: x.Provider, SessionKey: x.SessionKey, ParentKey: x.ParentKey, MachineID: b.m.ID, UserID: userID,
		SpaceID: b.space.ID, TeamID: teamID, Funding: store.FundingPersonal, Origin: oneOf(x.Origin, hubproto.OriginLive, hubproto.OriginEval, hubproto.OriginAutomation),
		Attribution: attribution, StartedAt: x.StartedAt + b.shift, Seq: seq, UpdatedAt: at + b.shift,
	}
	if x.EndedAt != nil {
		e := *x.EndedAt + b.shift
		row.EndedAt = &e
	}
	if x.AccountRef != "" {
		a, err := b.account(ctx, usage.PriceProvider(x.Provider, x.BaseURLHost), x.AccountRef, x.AuthMode, x.BaseURLHost, userID)
		if err != nil {
			return err
		}
		row.AccountID, row.Funding = a.ID, a.Funding
	}
	if len(x.Metrics) > 0 {
		raw, err := json.Marshal(x.Metrics)
		if err != nil {
			return err
		}
		row.Metrics = string(raw)
	}
	changed, err := b.tx.PutSession(ctx, row)
	if err == nil && changed {
		b.changed = append(b.changed, x.SessionKey)
		if len(x.MRKeys) > 0 {
			owner := x.ParentKey
			if owner == "" {
				owner = x.SessionKey
			}
			keys := make([]string, 0, len(x.MRKeys))
			for _, k := range x.MRKeys {
				if hubproto.IsMRKey(k) {
					keys = append(keys, k)
				}
			}
			err = b.tx.ReplaceSessionMRs(ctx, b.orgKey, x.Provider, x.SessionKey, owner, keys)
		}
	}
	return err
}

// tool keeps the calls of one tool of a session on a day. The session says who and which team (it was stored before, in
// this batch or an earlier one); a session the hub has not seen is the machine creator's, as a session's own guess is.
func (b *batch) tool(ctx context.Context, at int64, x hubproto.UsageToolFact) error {
	if x.SessionKey == "" || x.Provider == "" || x.ToolName == "" || x.Calls <= 0 {
		return nil
	}
	if _, err := time.Parse("2006-01-02", x.Day); err != nil {
		return nil
	}
	kind := oneOf(x.ToolKind, "other", "skill", "mcp", "bash")
	row := store.ToolUsage{
		OrgKey: b.orgKey, SpaceID: b.space.ID, TeamID: b.m.TeamID, UserID: b.m.CreatedBy, MachineID: b.m.ID, SessionKey: x.SessionKey,
		ToolKind: kind, ToolName: x.ToolName, Day: x.Day, Calls: x.Calls, Errors: min(max(x.Errors, 0), x.Calls), LastAt: at + b.shift,
	}
	switch s, err := b.tx.SessionByKey(ctx, b.orgKey, x.Provider, x.SessionKey); {
	case err == nil:
		row.UserID, row.TeamID = s.UserID, s.TeamID
	case !store.IsNotFound(err):
		return err
	default:
		team, err := b.team(ctx, row.UserID)
		if err != nil {
			return err
		}
		row.TeamID = team
	}
	if !b.loaded {
		items, err := b.tx.CatalogItems(ctx, b.orgKey, true)
		if err != nil {
			return err
		}
		b.catalog, b.loaded = items, true
	}
	row.ItemID = store.ItemForTool(b.catalog, kind, x.ToolName)
	return b.tx.UpsertToolUsage(ctx, row)
}

func (b *batch) window(ctx context.Context, w hubproto.UsageWindowFact) error {
	if w.AccountRef == "" || w.Name == "" {
		return nil
	}
	a, err := b.account(ctx, usage.PriceProvider(w.Provider, ""), w.AccountRef, "", "", b.m.CreatedBy)
	if err != nil {
		return err
	}
	row := store.AccountWindow{
		AccountID: a.ID, Scope: def(w.Scope, "all"), Name: w.Name, LengthSec: w.LengthSec, Unit: def(w.Unit, "pct"), UsedPct: w.UsedPct,
		Used: w.Used, Limit: w.Limit, MeasuredAt: w.MeasuredAt + b.shift, Source: w.Source, Confidence: def(w.Confidence, "official"), MachineID: b.m.ID,
	}
	if w.ResetsAt != nil {
		r := *w.ResetsAt + b.shift
		row.ResetsAt = &r
	}
	_, err = b.tx.PutWindow(ctx, row)
	return err
}

func (b *batch) balance(ctx context.Context, x hubproto.UsageBalanceFact) error {
	if x.AccountRef == "" || x.Currency == "" {
		return nil
	}
	a, err := b.account(ctx, usage.PriceProvider(x.Provider, ""), x.AccountRef, "", "", b.m.CreatedBy)
	if err != nil {
		return err
	}
	return b.tx.PutBalance(ctx, store.AccountBalance{
		AccountID: a.ID, Currency: x.Currency, Available: x.Available, SpendCap: x.SpendCap, SpentInPeriod: x.SpentInPeriod,
		MeasuredAt: x.MeasuredAt + b.shift, Source: x.Source,
	})
}

// person is who started a session: the hub user of the subject's email, else the machine's creator, a guess.
func (b *batch) person(ctx context.Context, subject, attribution string) (string, string, error) {
	email := strings.ToLower(strings.TrimSpace(subject))
	if email != "" {
		id, ok := b.users[email]
		if !ok {
			u, err := b.tx.UserByEmail(ctx, email)
			switch {
			case err == nil:
				id = u.ID
			case !store.IsNotFound(err):
				return "", "", err
			}
			b.users[email] = id
		}
		if id != "" {
			return id, oneOf(attribution, hubproto.AttributionExact, hubproto.AttributionGuessed), nil
		}
	}
	return b.m.CreatedBy, hubproto.AttributionGuessed, nil
}

// team is the session's team (analytics.md): the machine's team when the person is in it, otherwise the person's
// primary team in the machine's tenant, otherwise their first team there; "" when they are in none.
func (b *batch) team(ctx context.Context, userID string) (string, error) {
	if userID == "" {
		return "", nil
	}
	mine, err := b.tx.TeamsOfUser(ctx, userID)
	if err != nil {
		return "", err
	}
	var first, primary string
	for _, tm := range mine {
		if tm.TeamID == b.m.TeamID {
			return tm.TeamID, nil
		}
		t, err := b.tx.TeamByID(ctx, tm.TeamID)
		if err != nil {
			if store.IsNotFound(err) {
				continue
			}
			return "", err
		}
		if t.SpaceID != b.space.ID {
			continue
		}
		if first == "" {
			first = t.ID
		}
		if tm.IsPrimary && primary == "" {
			primary = t.ID
		}
	}
	return def(primary, first), nil
}

// account is the account of a ref in the organization; an unknown one is made «discovered» and personal, owned by the
// person it was seen with, until an admin registers it.
func (b *batch) account(ctx context.Context, provider, ref, authMode, baseURLHost, ownerID string) (store.Account, error) {
	kind := "subscription"
	if strings.HasPrefix(ref, "key:") || authMode == "api_key" {
		kind = "api_key"
	}
	return b.tx.EnsureAccount(ctx, store.Account{
		OrgKey: b.orgKey, ScopeKind: store.ScopeTenant, ScopeID: b.space.ID, Provider: provider, Kind: kind, AccountRef: ref,
		BaseURLHost: baseURLHost, Funding: store.FundingPersonal, OwnerUserID: ownerID, Discovered: true, CreatedAt: time.UnixMilli(b.now).UTC(),
	})
}

func oneOf(v string, allowed ...string) string {
	if slices.Contains(allowed, v) {
		return v
	}
	return allowed[0]
}

func def(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
