package env

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
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
// personal switches (a machine serves a team, not one person). Sorted as the hash is taken. setup says whether the
// machine has the feature environment.setup: it gets the scripts, with their place in the run (Order), and the
// accesses; a machine without it gets neither, so that the list and its hash are exactly what an older agent
// computes from what it received.
func MachineItems(w *world, spaceID, teamID string, setup bool) []hubproto.EnvItem {
	person := Person{OrgID: w.org.ID, SpaceID: spaceID}
	if teamID != "" {
		person.Teams = []string{teamID}
	}
	eff := Resolve(w.resolverItems(), resolverEntries(w.entries), person)
	order := scriptOrder(eff)
	out := []hubproto.EnvItem{}
	for _, e := range eff {
		it := hubproto.EnvItem{ItemID: e.ID, Kind: e.Kind, Name: e.Name, Layer: e.Layer, Pinned: e.Pinned,
			Source: hubproto.CanonicalJSON(json.RawMessage(e.Source))}
		if setup {
			it.Order = order[e.ID]
		} else if e.Kind == hubproto.EnvKindScript || e.Kind == hubproto.EnvKindAccess {
			continue
		}
		out = append(out, it)
	}
	hubproto.SortEnvItems(out)
	return out
}

// scriptOrder numbers the scripts of a list 1, 2, …: the organization's first, then the tenant's, then the team's,
// each layer in the order of its entries (position), ties by name.
func scriptOrder(eff []Effective) map[string]int {
	var scripts []Effective
	for _, e := range eff {
		if e.Kind == hubproto.EnvKindScript {
			scripts = append(scripts, e)
		}
	}
	slices.SortFunc(scripts, func(a, b Effective) int {
		return cmp.Or(cmp.Compare(LayerRank(a.Layer), LayerRank(b.Layer)), cmp.Compare(a.Position, b.Position),
			cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
	})
	out := make(map[string]int, len(scripts))
	for i, e := range scripts {
		out[e.ID] = i + 1
	}
	return out
}

// PushDeps are what the pusher is built from.
type PushDeps struct {
	DB       *store.DB
	Reg      Caller
	Features FeatureGate // nil: off for everybody, nothing is sent
	Log      *slog.Logger
	Now      func() time.Time
	// Publisher tells the people watching an organization that a run changed (env.run.changed); nil: nobody.
	Publisher Publisher
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
	pub   Publisher

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	kick   chan struct{}

	mu   sync.Mutex
	held map[string]string // machine with the environment feature → the hash it holds
	// setup is whether a machine, at its last hello, had the feature environment.setup; a machine absent has not said yet.
	setup map[string]bool
	// newRuns are the times a machine started runs of its own, for the hourly bound.
	newRuns map[string][]time.Time
	run     sync.Mutex // one pass at a time
}

// NewPusher starts the pusher.
func NewPusher(d PushDeps) *Pusher {
	p := &Pusher{db: d.DB, reg: d.Reg, feat: d.Features, log: d.Log, now: d.Now, every: d.Every, pub: d.Publisher, kick: make(chan struct{}, 1), held: map[string]string{}, setup: map[string]bool{}, newRuns: map[string][]time.Time{}}
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
	p.setup[machineID] = slices.Contains(hello.Features, hubproto.FeatureEnvSetup)
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
	sweep := time.NewTicker(sweepEvery)
	defer sweep.Stop()
	p.Sweep(p.ctx)
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-sweep.C:
			p.Sweep(p.ctx)
			continue
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
	p.mu.Lock()
	held, ok := p.held[machineID]
	setup := p.setup[machineID]
	p.mu.Unlock()
	items := MachineItems(w, sp.ID, m.TeamID, setup)
	hash := hubproto.EnvHash(items)
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

// SetupOf says whether a machine has the feature environment.setup. known is false until the machine has said hello to
// this hub process: its features are not kept anywhere else.
func (p *Pusher) SetupOf(machineID string) (setup, known bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	setup, known = p.setup[machineID]
	return setup, known
}

const (
	// sweepEvery is how often the runs are looked at: the old ones removed, the stuck ones ended.
	sweepEvery = 5 * time.Minute
	// stuckAfter is how long a run may say «running» without a word from its machine.
	stuckAfter = 30 * time.Minute
)

// Sweep ends the runs that stayed «running» with no word from the machine for stuckAfter (the machine went away, or its
// runtime died) and removes the runs older than hubproto.EnvRunKeepDays.
func (p *Pusher) Sweep(ctx context.Context) {
	p.endStuckRuns(ctx)
	cut := p.now().AddDate(0, 0, -hubproto.EnvRunKeepDays)
	n, err := p.db.S().DeleteEnvRunsBefore(ctx, cut)
	if err != nil {
		if ctx.Err() == nil {
			p.log.Warn("old environment runs not removed", "error", err)
		}
		return
	}
	if n > 0 {
		p.log.Info("old environment runs removed", "runs", n)
	}
}

func (p *Pusher) endStuckRuns(ctx context.Context) {
	st := p.db.S()
	stuck, err := st.StuckEnvRuns(ctx, p.now().Add(-stuckAfter))
	if err != nil {
		if ctx.Err() == nil {
			p.log.Warn("stuck environment runs not read", "error", err)
		}
		return
	}
	for _, r := range stuck {
		var steps []hubproto.EnvStep
		if json.Unmarshal([]byte(r.Steps), &steps) != nil {
			steps = nil
		}
		for i := range steps {
			if steps[i].Status == hubproto.EnvRunning {
				steps[i].Status, steps[i].Message = hubproto.EnvError, "машина не ответила"
			}
		}
		steps = append(steps, hubproto.EnvStep{Name: "Запуск", Status: hubproto.EnvError, Message: "машина не ответила"})
		raw, err := json.Marshal(steps)
		if err != nil {
			continue
		}
		now := p.now()
		r.Status, r.FinishedAt, r.UpdatedAt, r.Steps = hubproto.EnvError, now, now, string(raw)
		if _, err := st.UpsertEnvRun(ctx, r); err != nil {
			p.log.Warn("stuck environment run not ended", "run", r.ID, "error", err)
			continue
		}
		if p.pub != nil {
			p.pub.Publish(r.OrgID, "env.run.changed", map[string]any{"runId": r.ID, "status": hubproto.EnvError, "machineId": r.MachineID})
		}
	}
}

// The most steps and bytes of steps one stored run may have: a run is a few kilobytes of statuses and at most
// hubproto.EnvLogMax of log per step, and the hub does not keep what a misbehaving machine makes up.
const (
	maxRunSteps = 200
	maxRunBytes = 512 << 10
	// maxNewRunsPerHour bounds the run ids one machine may invent (apply runs it starts itself).
	maxNewRunsPerHour = 30
)

// allowNewRun says whether a machine may start one more run of its own now.
func (p *Pusher) allowNewRun(machineID string) bool {
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	recent := slices.DeleteFunc(p.newRuns[machineID], func(t time.Time) bool { return now.Sub(t) >= time.Hour })
	if len(recent) >= maxNewRunsPerHour {
		p.newRuns[machineID] = recent
		return false
	}
	p.newRuns[machineID] = append(recent, now)
	return true
}

// fitSteps marshals the steps within maxRunBytes by emptying the logs from the oldest step on; false when even the
// steps without logs do not fit.
func fitSteps(steps []hubproto.EnvStep) ([]byte, bool) {
	for i := 0; ; i++ {
		raw, err := json.Marshal(steps)
		if err != nil {
			return nil, false
		}
		if len(raw) <= maxRunBytes {
			return raw, true
		}
		for i < len(steps) && steps[i].Log == "" {
			i++
		}
		if i >= len(steps) {
			return nil, false
		}
		steps[i].Log = ""
	}
}

// Run keeps a run a machine reported (the env.run event): the same run id replaces the earlier frame. The run belongs
// to the machine's organization and owner; a run id that another machine already used is refused.
func (p *Pusher) Run(machineID string, run hubproto.EnvRun) {
	if err := p.saveRun(machineID, run); err != nil {
		p.log.Warn("environment run not saved", "machine", machineID, "run", run.RunID, "error", err)
	}
}

func (p *Pusher) saveRun(machineID string, run hubproto.EnvRun) error {
	if !hubproto.EnvRunIDRe.MatchString(run.RunID) || !hubproto.ValidRunMode(run.Mode) {
		return fmt.Errorf("unusable run id or mode")
	}
	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	defer cancel()
	st := p.db.S()
	m, err := st.MachineByID(ctx, machineID)
	if err != nil {
		return err
	}
	sp, err := st.SpaceByID(ctx, m.SpaceID)
	if err != nil {
		return err
	}
	if sp.OrgID == "" {
		return nil // no organization: nobody lists the runs
	}
	// A run the hub asked for (update, check, sandbox) is a row the hub inserted for this machine; only a run the machine
	// started itself (apply) may be new.
	prev, err := st.EnvRunByID(ctx, sp.OrgID, run.RunID)
	switch {
	case err == nil && prev.MachineID != machineID:
		return fmt.Errorf("run id of another machine")
	case store.IsNotFound(err) && run.Mode != hubproto.RunApply:
		return fmt.Errorf("a %s run the hub did not ask this machine for", run.Mode)
	case err != nil && !store.IsNotFound(err):
		return err
	case store.IsNotFound(err) && !p.allowNewRun(machineID):
		return fmt.Errorf("more than %d runs of its own in an hour", maxNewRunsPerHour)
	}
	if len(run.Steps) > maxRunSteps {
		run.Steps = run.Steps[:maxRunSteps]
	}
	for i := range run.Steps {
		if l := run.Steps[i].Log; len(l) > hubproto.EnvLogMax {
			run.Steps[i].Log = l[len(l)-hubproto.EnvLogMax:]
		}
	}
	if run.Steps == nil {
		run.Steps = []hubproto.EnvStep{}
	}
	raw, fits := fitSteps(run.Steps)
	if !fits {
		return fmt.Errorf("run is too big")
	}
	row := store.EnvRun{ID: run.RunID, OrgID: sp.OrgID, MachineID: machineID, UserID: m.CreatedBy, SpaceID: m.SpaceID, TeamID: m.TeamID,
		Mode: run.Mode, Status: run.Status, Hash: run.Hash, ByName: run.By, Steps: string(raw), UpdatedAt: p.now()}
	row.StartedAt = time.UnixMilli(run.StartedAt)
	if run.StartedAt <= 0 {
		row.StartedAt = p.now()
	}
	if run.FinishedAt > 0 {
		row.FinishedAt = time.UnixMilli(run.FinishedAt)
	}
	if ok, err := st.UpsertEnvRun(ctx, row); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("run id held by another machine or organization")
	}
	if p.pub != nil {
		p.pub.Publish(sp.OrgID, "env.run.changed", map[string]any{"runId": run.RunID, "status": run.Status, "machineId": machineID})
	}
	return nil
}
