package env

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Caller sends an operation to a machine over its link (link.Registry).
type Caller interface {
	DoInto(ctx context.Context, machineID, op string, params, out any, timeout time.Duration) error
	Online() []string
}

// MachineItems is the list a machine of a tenant holds: what the layers put in for the machine's team, without the
// personal switches (a machine serves a team, not one person). Sorted as the hash is taken.
func MachineItems(w *world, spaceID, teamID string) []hubproto.EnvItem {
	person := Person{OrgID: w.org.ID, SpaceID: spaceID}
	if teamID != "" {
		person.Teams = []string{teamID}
	}
	out := []hubproto.EnvItem{}
	for _, e := range Resolve(w.resolverItems(), resolverEntries(w.entries), person) {
		out = append(out, hubproto.EnvItem{ItemID: e.ID, Kind: e.Kind, Name: e.Name, Layer: e.Layer, Pinned: e.Pinned,
			Source: hubproto.CanonicalJSON(json.RawMessage(e.Source))})
	}
	hubproto.SortEnvItems(out)
	return out
}

// PushDeps are what the pusher is built from.
type PushDeps struct {
	DB       *store.DB
	Reg      Caller
	Features FeatureGate // nil: off for everybody, nothing is sent
	Log      *slog.Logger
	Now      func() time.Time
	// Every is how often a machine that does not hold the list is told again; 0 means 60 s.
	Every time.Duration
}

// Pusher keeps the environment lists of the machines current: it computes each machine's list, sends env.set when the
// list changed, and sends it again every pass while the machine reports another hash.
type Pusher struct {
	db    *store.DB
	reg   Caller
	feat  FeatureGate
	log   *slog.Logger
	now   func() time.Time
	every time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	kick   chan struct{}

	mu   sync.Mutex
	held map[string]string // machine with the environment feature → the hash it holds
	run  sync.Mutex        // one pass at a time
}

// NewPusher starts the pusher.
func NewPusher(d PushDeps) *Pusher {
	p := &Pusher{db: d.DB, reg: d.Reg, feat: d.Features, log: d.Log, now: d.Now, every: d.Every, kick: make(chan struct{}, 1), held: map[string]string{}}
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

// Connected is called after a machine's hello: an agent that keeps the environment gets its list now if it is behind.
func (p *Pusher) Connected(machineID string, hello hubproto.Hello) {
	p.mu.Lock()
	if slices.Contains(hello.Features, hubproto.FeatureEnvironment) {
		p.held[machineID] = hello.EnvHash
	} else {
		delete(p.held, machineID)
	}
	_, ok := p.held[machineID]
	p.mu.Unlock()
	if ok {
		p.Kick()
	}
}

// Reported is called with what an agent says it holds (the env.state event): it is kept and the machine is looked at
// again, in case it holds an older list.
func (p *Pusher) Reported(machineID string, st hubproto.EnvState) {
	p.mu.Lock()
	_, ok := p.held[machineID]
	if ok {
		p.held[machineID] = st.Hash
	}
	p.mu.Unlock()
	if err := p.save(machineID, st); err != nil {
		p.log.Warn("environment state not saved", "machine", machineID, "error", err)
	}
}

func (p *Pusher) save(machineID string, st hubproto.EnvState) error {
	applied := st.Applied
	if applied == nil {
		applied = []hubproto.EnvApplied{}
	}
	raw, err := json.Marshal(applied)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	defer cancel()
	return p.db.S().SaveEnvReported(ctx, machineID, st.Rev, st.Hash, string(raw), p.now())
}

// Kick asks for a pass soon (an entry changed, a machine came).
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
			p.log.Warn("environment not pushed", "error", err)
		}
	}
}

// Push checks every online machine that keeps the environment and sends the lists that differ from what it holds.
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
	worlds := map[string]*world{} // by organization; nil: nothing to send there
	for _, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := p.pushOne(ctx, st, id, worlds); err != nil {
			p.log.Warn("environment not pushed", "machine", id, "error", err)
		}
	}
	return nil
}

func (p *Pusher) world(ctx context.Context, st store.Store, spaceOrg string, worlds map[string]*world) (*world, error) {
	if w, ok := worlds[spaceOrg]; ok {
		return w, nil
	}
	var w *world
	if p.feat != nil && p.feat.On(ctx, spaceOrg, FeatureEnv) {
		o, err := st.OrgByID(ctx, spaceOrg)
		if err != nil {
			return nil, err
		}
		if w, err = loadWorld(ctx, st, o); err != nil {
			return nil, err
		}
	}
	worlds[spaceOrg] = w
	return w, nil
}

func (p *Pusher) pushOne(ctx context.Context, st store.Store, machineID string, worlds map[string]*world) error {
	m, err := st.MachineByID(ctx, machineID)
	if err != nil {
		return err
	}
	if m.RevokedAt != nil {
		return nil
	}
	sp, err := st.SpaceByID(ctx, m.SpaceID)
	if err != nil {
		return err
	}
	if sp.OrgID == "" {
		return nil
	}
	w, err := p.world(ctx, st, sp.OrgID, worlds)
	if err != nil || w == nil {
		return err
	}
	items := MachineItems(w, sp.ID, m.TeamID)
	hash := hubproto.EnvHash(items)

	p.mu.Lock()
	held, ok := p.held[machineID]
	p.mu.Unlock()
	if !ok || held == hash {
		return nil
	}
	last, err := st.EnvStateByMachine(ctx, machineID)
	if err != nil && !store.IsNotFound(err) {
		return err
	}
	if last.SentRev == 0 && held == "" && len(items) == 0 {
		return nil // nothing was ever put in, and the machine holds nothing
	}
	rev := last.SentRev
	if last.SentHash != hash || rev == 0 {
		rev++
	}
	var res hubproto.EnvState
	if err := p.reg.DoInto(ctx, machineID, hubproto.OpEnvSet, hubproto.EnvSet{Rev: rev, Hash: hash, Items: items}, &res, 30*time.Second); err != nil {
		return err
	}
	if err := st.SaveEnvSent(ctx, machineID, rev, hash, p.now()); err != nil {
		return err
	}
	p.mu.Lock()
	if _, still := p.held[machineID]; still {
		p.held[machineID] = res.Hash
	}
	p.mu.Unlock()
	return p.save(machineID, res)
}
