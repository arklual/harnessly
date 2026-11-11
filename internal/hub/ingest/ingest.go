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

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/metrics"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/usage"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/safego"
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
	// waiting is since when a machine has had spend the hub was told of and has not stored yet.
	waiting map[string]time.Time
	// failures counts the failed pulls of a machine in a row; retry is the pull armed after the last of them, with the
	// pause of the link's replication (hubproto.ReplRetryWait): a failed pull no longer waits for the next
	// usage.available, which a machine with no new spend never sends.
	failures  map[string]int
	retry     map[string]*time.Timer
	retryWait func(failures int) time.Duration
}

func New(d Deps) *Service {
	s := &Service{db: d.DB, reg: d.Reg, log: d.Log, now: d.Now, timeout: d.Timeout, running: map[string]bool{}, again: map[string]bool{}, waiting: map[string]time.Time{},
		failures: map[string]int{}, retry: map[string]*time.Timer{}, retryWait: hubproto.ReplRetryWait}
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
	s.mu.Lock()
	for id, t := range s.retry {
		t.Stop()
		delete(s.retry, id)
	}
	s.mu.Unlock()
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
	if _, ok := s.waiting[machineID]; !ok {
		s.waiting[machineID] = s.now()
	}
	if s.running[machineID] {
		s.again[machineID] = true
		return
	}
	s.running[machineID] = true
	s.wg.Add(1)
	safego.Go("ingest loop", func() { s.loop(machineID) })
}

func (s *Service) loop(machineID string) {
	defer s.wg.Done()
	for {
		err := s.Pull(s.ctx, machineID)
		if err != nil && s.ctx.Err() == nil {
			metrics.UsagePullErrors.Inc()
			s.log.Warn("usage not pulled", "machine", machineID, "error", err)
		}
		s.mu.Lock()
		s.pulled(machineID, err)
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

// pulled keeps the machine's wait after a pull; s.mu is held. A pull that emptied the outbox ends the wait, and a
// trigger that came during it starts a new one. A machine that went away ends it too: its spend is out of the hub's
// reach until it dials again, which triggers a pull. Any other failure leaves the wait running.
func (s *Service) pulled(machineID string, err error) {
	var he *httpx.Error
	offline := errors.As(err, &he) && he.Code == "machine_offline"
	switch {
	case err == nil && s.again[machineID]:
		s.waiting[machineID] = s.now()
	case err == nil, offline:
		delete(s.waiting, machineID)
	}
	if err == nil || offline || s.ctx.Err() != nil {
		// Stored, or out of reach until the machine dials again, which triggers a pull of its own.
		delete(s.failures, machineID)
		return
	}
	s.failures[machineID]++
	if s.retry[machineID] != nil {
		return
	}
	s.retry[machineID] = time.AfterFunc(s.retryWait(s.failures[machineID]), func() {
		s.mu.Lock()
		delete(s.retry, machineID)
		s.mu.Unlock()
		s.Trigger(machineID)
	})
}

// Lag is how long the spend the hub knows of and has not stored has waited, the longest wait among the machines, and
// how many machines wait.
func (s *Service) Lag() (oldest time.Duration, machines int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for _, since := range s.waiting {
		if d := now.Sub(since); d > oldest {
			oldest = d
		}
	}
	return oldest, len(s.waiting)
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
	var res readResult
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
	records := res.decode()
	var aside int
	apply := func(careful bool) error {
		aside = 0
		return s.db.Tx(ctx, func(tx store.Store) error {
			if newEpoch && cur.Epoch != "" {
				if err := tx.ResetSessionSeqs(ctx, machineID); err != nil {
					return err
				}
			}
			n, err := s.store(ctx, tx, m, res.Epoch, after, records, received, careful)
			if err != nil {
				return err
			}
			aside = n
			return tx.SetCursor(ctx, store.Cursor{MachineID: machineID, Stream: Stream, Epoch: res.Epoch, Seq: max(res.Last, after)})
		})
	}
	err = apply(false)
	if err != nil && ctx.Err() == nil && !store.Retriable(err) {
		// One record the hub cannot store must not hold the machine's whole outbox: the batch is stored again record
		// by record, and the one that fails is set aside with the reason.
		s.log.Warn("usage batch not stored, storing it record by record", "machine", machineID, "error", err)
		err = apply(true)
	}
	if err != nil {
		return false, err
	}
	if aside > 0 {
		metrics.UsageQuarantined.Add(int64(aside))
		s.log.Warn("usage records set aside", "machine", machineID, "records", aside)
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

// readResult is hubproto.UsageReadResult with each record kept as the machine sent it, so a record this hub cannot
// read is kept whole.
type readResult struct {
	Epoch   string            `json:"epoch"`
	Records []json.RawMessage `json:"records"`
	Last    int64             `json:"last"`
	More    bool              `json:"more"`
}

// rawRecord is a record with its text; bad says why it could not be read at all.
type rawRecord struct {
	hubproto.UsageRecord
	raw json.RawMessage
	bad string
}

func (r readResult) decode() []rawRecord {
	out := make([]rawRecord, len(r.Records))
	for i, raw := range r.Records {
		out[i].raw = raw
		if err := json.Unmarshal(raw, &out[i].UsageRecord); err != nil {
			out[i].bad = "not a usage record: " + err.Error()
		}
	}
	return out
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

// store writes a batch and returns how many of its records were set aside instead (the quarantine): the ones of a kind
// this hub does not know, the ones that are not what their kind says and, when careful, the ones whose storing failed.
// Without careful a record that fails fails the batch.
func (s *Service) store(ctx context.Context, tx store.Store, m store.Machine, epoch string, after int64, records []rawRecord, received time.Time, careful bool) (int, error) {
	sp, err := tx.SpaceByID(ctx, m.SpaceID)
	if err != nil {
		return 0, err
	}
	prices, err := tx.Prices(ctx)
	if err != nil {
		return 0, err
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
	aside := 0
	quarantine := func(r rawRecord, reason string) error {
		added, err := tx.QuarantineUsage(ctx, store.QuarantinedUsage{MachineID: m.ID, Epoch: epoch, Seq: r.Seq, Kind: r.Kind, Reason: reason, Record: string(r.raw), CreatedAt: b.now})
		if added {
			aside++
		}
		return err
	}
	const savepoint = "usage_record"
	last := after
	for _, r := range records {
		if r.bad != "" {
			if err := quarantine(r, r.bad); err != nil {
				return 0, err
			}
			continue
		}
		if r.Seq <= last {
			continue // out of order or already stored: the outbox's seq only grows
		}
		last = r.Seq
		if careful {
			if _, err := tx.Q().Exec(ctx, "SAVEPOINT "+savepoint); err != nil {
				return 0, err
			}
		}
		known, err := b.record(ctx, r.UsageRecord)
		switch {
		case err != nil && (!careful || ctx.Err() != nil || store.Retriable(err)):
			return 0, fmt.Errorf("record %d (%s): %w", r.Seq, r.Kind, err)
		case err != nil:
			// What the record wrote before it failed is undone; the rest of the batch stays.
			if _, rerr := tx.Q().Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint); rerr != nil {
				return 0, rerr
			}
			if qerr := quarantine(r, "not stored: "+err.Error()); qerr != nil {
				return 0, qerr
			}
		case !known:
			if qerr := quarantine(r, "a kind this hub does not read, or a record without what its kind carries"); qerr != nil {
				return 0, qerr
			}
		}
		if careful {
			if _, err := tx.Q().Exec(ctx, "RELEASE SAVEPOINT "+savepoint); err != nil {
				return 0, err
			}
		}
	}
	if len(b.changed) > 0 {
		hs, err := tx.CallHours(ctx, b.orgKey, b.changed)
		if err != nil {
			return 0, err
		}
		b.hours = append(b.hours, hs...)
	}
	return aside, tx.Rollup(ctx, b.orgKey, b.hours)
}

// record stores one record. known is false for a record this hub cannot read: a kind it does not know yet (a newer
// agent), or a kind without its fact.
func (b *batch) record(ctx context.Context, r hubproto.UsageRecord) (known bool, err error) {
	switch r.Kind {
	case hubproto.UsageCall:
		if r.Call != nil {
			return true, b.call(ctx, *r.Call)
		}
	case hubproto.UsageSession:
		if r.Session != nil {
			return true, b.session(ctx, r.Seq, r.At, *r.Session)
		}
	case hubproto.UsageTool:
		if r.Tool != nil {
			return true, b.tool(ctx, r.At, *r.Tool)
		}
	case hubproto.UsageWindow:
		if r.Window != nil {
			return true, b.window(ctx, *r.Window)
		}
	case hubproto.UsageBalance:
		if r.Balance != nil {
			return true, b.balance(ctx, *r.Balance)
		}
	}
	return false, nil
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
