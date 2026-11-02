package limits

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// DefaultTZ is the time zone of an organization that has none (an implicit one).
const DefaultTZ = "Europe/Moscow"

// Location is the time zone an organization cuts its months in.
func Location(ctx context.Context, st store.Store, orgKey string) (*time.Location, error) {
	tz := DefaultTZ
	if orgKey != "" && !strings.HasPrefix(orgKey, "space:") {
		o, err := st.OrgByID(ctx, orgKey)
		if err != nil && !store.IsNotFound(err) {
			return nil, err
		}
		if o.Timezone != "" {
			tz = o.Timezone
		}
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC, nil
	}
	return loc, nil
}

// Caller sends an operation to a machine over its link (link.Registry).
type Caller interface {
	DoInto(ctx context.Context, machineID, op string, params, out any, timeout time.Duration) error
	Online() []string
}

// PushDeps are what the pusher is built from.
type PushDeps struct {
	DB   *store.DB
	Reg  Caller
	Mode string // hubproto.EnforceWarn or EnforceStop
	Log  *slog.Logger
	Now  func() time.Time
	// Every is how often every machine's list is checked; 0 means 60 s (the contract's bound).
	Every time.Duration
}

// Pusher keeps the budget lists of the machines current: it computes each machine's list, and sends budget.set when the
// list changed or the machine holds an older version than the hub sent.
type Pusher struct {
	db    *store.DB
	reg   Caller
	mode  string
	log   *slog.Logger
	now   func() time.Time
	every time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	kick   chan struct{}

	mu   sync.Mutex
	held map[string]int64 // machine with the budget feature → the version it holds
	run  sync.Mutex       // one pass at a time
}

// NewPusher starts the pusher.
func NewPusher(d PushDeps) *Pusher {
	p := &Pusher{db: d.DB, reg: d.Reg, mode: d.Mode, log: d.Log, now: d.Now, every: d.Every, kick: make(chan struct{}, 1), held: map[string]int64{}}
	if p.log == nil {
		p.log = slog.Default()
	}
	if p.now == nil {
		p.now = time.Now
	}
	if p.every == 0 {
		p.every = 60 * time.Second
	}
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.wg.Add(1)
	go p.loop()
	return p
}

// Close stops the pusher and waits for the pass running.
func (p *Pusher) Close() {
	p.cancel()
	p.wg.Wait()
}

// Connected is called after a machine's hello: an agent that applies budgets gets its list now if it is behind.
func (p *Pusher) Connected(machineID string, hello hubproto.Hello) {
	if !slices.Contains(hello.Features, hubproto.FeatureBudget) {
		p.mu.Lock()
		delete(p.held, machineID)
		p.mu.Unlock()
		return
	}
	p.mu.Lock()
	p.held[machineID] = hello.BudgetVersion
	p.mu.Unlock()
	p.Kick()
}

// Kick asks for a pass soon (a rule changed, a machine came).
func (p *Pusher) Kick() {
	select {
	case p.kick <- struct{}{}:
	default:
	}
}

func (p *Pusher) loop() {
	defer p.wg.Done()
	t := time.NewTicker(p.every)
	defer t.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-t.C:
		case <-p.kick:
		}
		if err := p.Push(p.ctx); err != nil && p.ctx.Err() == nil {
			p.log.Warn("budgets not pushed", "error", err)
		}
	}
}

// Push checks every online machine that applies budgets and sends the lists that changed.
func (p *Pusher) Push(ctx context.Context) error {
	p.run.Lock()
	defer p.run.Unlock()
	p.mu.Lock()
	var ids []string
	for _, id := range p.reg.Online() {
		if _, ok := p.held[id]; ok {
			ids = append(ids, id)
		}
	}
	p.mu.Unlock()
	if len(ids) == 0 {
		return nil
	}
	st := p.db.S()
	now := p.now()
	books := map[string]*Book{}
	for _, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := p.pushOne(ctx, st, id, now, books); err != nil {
			p.log.Warn("budget not pushed", "machine", id, "error", err)
		}
	}
	return nil
}

func (p *Pusher) pushOne(ctx context.Context, st store.Store, machineID string, now time.Time, books map[string]*Book) error {
	m, err := st.MachineByID(ctx, machineID)
	if err != nil {
		return err
	}
	sp, err := st.SpaceByID(ctx, m.SpaceID)
	if err != nil {
		return err
	}
	key := store.OrgKey(sp)
	b := books[key]
	if b == nil {
		loc, err := Location(ctx, st, key)
		if err != nil {
			return err
		}
		if b, err = Evaluate(ctx, st, key, loc, now); err != nil {
			return err
		}
		books[key] = b
	}
	entries, err := b.MachineEntries(ctx, st, m.SpaceID)
	if err != nil {
		return err
	}
	sum := sha256.New()
	_ = json.NewEncoder(sum).Encode(struct {
		Mode    string
		Entries []hubproto.BudgetEntry
	}{p.mode, entries})
	hash := hex.EncodeToString(sum.Sum(nil))

	last, err := st.MachineBudgetOf(ctx, machineID)
	if err != nil && !store.IsNotFound(err) {
		return err
	}
	p.mu.Lock()
	held, ok := p.held[machineID]
	p.mu.Unlock()
	if !ok || (last.Hash == hash && held >= last.Version && last.Version > 0) {
		return nil
	}
	set := hubproto.BudgetSet{Version: max(now.UnixMilli(), last.Version+1), Mode: p.mode, IssuedAt: now.UnixMilli(), Entries: entries}
	var res hubproto.BudgetSetResult
	if err := p.reg.DoInto(ctx, machineID, hubproto.OpBudgetSet, set, &res, 30*time.Second); err != nil {
		return err
	}
	p.mu.Lock()
	if _, still := p.held[machineID]; still {
		p.held[machineID] = max(res.Version, set.Version)
	}
	p.mu.Unlock()
	return st.SetMachineBudget(ctx, store.MachineBudget{MachineID: machineID, Version: set.Version, Hash: hash, SyncedAt: now})
}

// MachineEntries is the list a machine of the space holds: every limit of its people that is not ok.
func (b *Book) MachineEntries(ctx context.Context, st store.Store, spaceID string) ([]hubproto.BudgetEntry, error) {
	entries := []hubproto.BudgetEntry{}
	if len(b.States) == 0 {
		return entries, nil
	}
	members, err := st.Members(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(members, func(x, y store.Member) int { return cmp.Compare(x.Email, y.Email) })
	byID := map[string]State{}
	for _, s := range b.States {
		byID[s.Rule.ID] = s
	}
	for _, mb := range members {
		if mb.Email == "" {
			continue
		}
		person, err := b.PersonOf(ctx, st, mb.UserID)
		if err != nil {
			return nil, err
		}
		for _, e := range b.Effective(person) {
			if e.Status == OK {
				continue
			}
			x := hubproto.BudgetEntry{User: mb.Email, ModelPattern: e.ModelPattern, Status: e.Status, Pct: e.Pct, Reason: e.Reason, RuleID: e.Binding,
				Forbid: byID[e.Binding].Rule.Forbid}
			if e.AccountID != "" {
				a, ok := b.Accounts[e.AccountID]
				if !ok || a.AccountRef == "" {
					continue // the machine cannot tell this account from the others
				}
				x.Account = a.AccountRef
			}
			if e.ResetsAt != nil && !x.Forbid {
				x.ResetsAt = e.ResetsAt.UnixMilli()
			}
			entries = append(entries, x)
		}
	}
	return entries, nil
}
