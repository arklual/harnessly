package machines

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/provider"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/safego"
)

// The operations on a machine (docs/hub/machines-resources.md, «Operations»): one unfinished operation per machine,
// stored in machine_ops, run by the hub (a restart of the runner is a call to the agent, a resize or a reboot of the
// server is a call to the provider followed by asking how it went) and told to the organization as machine.op.

const (
	// providerWatchMax is how long the hub asks a provider about one operation before it gives up.
	providerWatchMax = 30 * time.Minute
	// providerWatchErrors is how many answers in a row may fail before the operation is given up.
	providerWatchErrors = 8
	// staleCall is how long an operation may stand without ever reaching the provider (the hub died between the two).
	staleCall = 5 * time.Minute
	// staleWait is how long a restart may wait for the chats of a machine before it is written off.
	staleWait = 24 * time.Hour
	// staleProvider is how long an operation at the provider may stand without a word.
	staleProvider = time.Hour
	// pruneEvery is how often old load buckets are deleted.
	pruneEvery = 10 * time.Minute
	pruneBatch = 2000
)

var errOpBusy = httpx.New(http.StatusConflict, "op_busy", "С этой машиной уже идёт операция")

// provider kinds are the ones the provider carries out; the rest are the agent's.
func providerKind(kind string) bool {
	return kind == store.OpResize || kind == store.OpReboot || kind == store.OpPowerOff || kind == store.OpPowerOn
}

// newOp stores an unfinished operation. A machine has one: a second one is refused.
func (s *Service) newOp(ctx context.Context, m store.Machine, kind string, params map[string]any, by string) (store.MachineOp, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return store.MachineOp{}, err
	}
	s.res.opMu.Lock()
	defer s.res.opMu.Unlock()
	st := s.db.S()
	open, err := st.OpsByMachine(ctx, m.ID, true, 1)
	if err != nil {
		return store.MachineOp{}, err
	}
	if len(open) > 0 {
		return store.MachineOp{}, errOpBusy.With("opId", open[0].ID).With("kind", open[0].Kind)
	}
	now := s.now().UTC()
	op := store.MachineOp{ID: store.RandomHex(16), MachineID: m.ID, Kind: kind, Status: store.OpPending, Params: string(raw), CreatedBy: by, CreatedAt: now, UpdatedAt: now}
	if err := st.CreateOp(ctx, &op); err != nil {
		return store.MachineOp{}, err
	}
	s.publishOp(ctx, m, op)
	return op, nil
}

type callFn func(ctx context.Context, pm provider.Machine) (provider.OpRef, error)

// providerOp stores an operation and asks the provider to carry it out. The provider's answer to the call itself (a
// refusal, a timeout) is the answer to the request; how the work goes is watched in the background.
func (s *Service) providerOp(ctx context.Context, m store.Machine, kind string, params map[string]any, by string, call callFn, after func(context.Context)) (store.MachineOp, error) {
	op, err := s.newOp(ctx, m, kind, params, by)
	if err != nil {
		return store.MachineOp{}, err
	}
	return s.runProvider(ctx, m, op, call, after)
}

// runProvider makes the provider call of a stored operation.
func (s *Service) runProvider(ctx context.Context, m store.Machine, op store.MachineOp, call callFn, after func(context.Context)) (store.MachineOp, error) {
	cctx, cancel := context.WithTimeout(provider.WithIdempotencyKey(context.WithoutCancel(ctx), op.ID), providerTimeout)
	ref, err := call(cctx, s.pmachine(ctx, m))
	cancel()
	if err != nil {
		mapped := mapProviderErr(err)
		s.finishOp(ctx, m, op.ID, store.OpFailed, mapped.Error())
		if fresh, ferr := s.db.S().OpByID(ctx, op.ID); ferr == nil {
			op = fresh
		}
		return op, mapped
	}
	s.setOp(ctx, m, op.ID, store.OpRunning, ref.ID, "")
	fresh, err := s.db.S().OpByID(ctx, op.ID)
	if err != nil {
		return op, err
	}
	safego.Go("machines watch", func() { s.watch(m, fresh, ref, after) })
	return fresh, nil
}

// watch asks the provider how an operation goes until it ends.
func (s *Service) watch(m store.Machine, op store.MachineOp, ref provider.OpRef, after func(context.Context)) {
	ctx := context.Background()
	started := s.now()
	fails := 0
	for {
		s.res.mu.Lock()
		every := s.res.pollEvery
		s.res.mu.Unlock()
		time.Sleep(every)
		cur, err := s.db.S().OpByID(ctx, op.ID)
		if err != nil || !cur.Active() {
			return
		}
		if s.now().Sub(started) > providerWatchMax {
			s.finishOp(ctx, m, op.ID, store.OpFailed, "провайдер не завершил операцию вовремя")
			return
		}
		pctx, cancel := context.WithTimeout(ctx, providerTimeout)
		st, err := s.prov.Op(pctx, ref)
		cancel()
		if err != nil {
			if errors.Is(err, provider.ErrNotFound) || errors.Is(err, provider.ErrUnsupported) {
				s.finishOp(ctx, m, op.ID, store.OpFailed, "провайдер не знает эту операцию")
				return
			}
			if fails++; fails >= providerWatchErrors {
				s.finishOp(ctx, m, op.ID, store.OpFailed, "провайдер не отвечает")
				return
			}
			continue
		}
		fails = 0
		switch st.State {
		case provider.StateDone:
			if s.finishOp(ctx, m, op.ID, store.OpDone, "") && after != nil {
				after(ctx)
			}
			return
		case provider.StateFailed:
			text := st.Error
			if text == "" {
				text = "провайдер не выполнил операцию"
			}
			s.finishOp(ctx, m, op.ID, store.OpFailed, clamp(text, 500))
			return
		}
	}
}

// setOp moves an unfinished operation along and tells the organization.
func (s *Service) setOp(ctx context.Context, m store.Machine, id, status, providerOpID, errText string) bool {
	changed, err := s.db.S().SetOp(ctx, id, status, providerOpID, errText, s.now().UTC())
	if err != nil {
		s.log.Warn("operation not saved", "op", id, "error", err)
		return false
	}
	if changed {
		if op, err := s.db.S().OpByID(ctx, id); err == nil {
			s.publishOp(ctx, m, op)
		}
	}
	return changed
}

// finishOp ends an operation; false when it had ended already.
func (s *Service) finishOp(ctx context.Context, m store.Machine, id, status, errText string) bool {
	s.res.mu.Lock()
	delete(s.res.waiters, id)
	delete(s.res.chats, id)
	s.res.mu.Unlock()
	changed := s.setOp(ctx, m, id, status, "", clamp(errText, 500))
	if changed {
		s.Publish(ctx, m)
	}
	return changed
}

// publishOp tells the machine's organization how an operation stands.
func (s *Service) publishOp(ctx context.Context, m store.Machine, op store.MachineOp) {
	s.emit(ctx, m, EventMachineOp, map[string]any{"machineId": m.ID, "op": s.opView(op)})
}

// emit sends a live event of a machine to its organization (Deps.Publisher), or to its space's stream without one.
func (s *Service) emit(ctx context.Context, m store.Machine, kind string, data any) {
	if m.SpaceID == "" {
		return
	}
	if s.pub != nil {
		if sp, err := s.db.S().SpaceByID(ctx, m.SpaceID); err == nil {
			s.pub.Publish(store.OrgKey(sp), kind, data)
		}
		return
	}
	s.events.PublishMachine(m.SpaceID, m.ID, kind, data, false)
}

// ---- restart ----

// restartRequest is the body of POST /api/machines/{id}/restart.
type restartRequest struct {
	What string `json:"what"`
	When string `json:"when"`
}

// restartOn is the restart of a machine whose organization has the resources feature on: the runner or the whole
// machine, now or after the chats are done, with the progress kept as an operation.
func (s *Service) restartOn(w http.ResponseWriter, r *http.Request, p *auth.Principal, m store.Machine, req restartRequest) error {
	what, when := req.What, req.When
	if what == "vm" {
		what = "machine"
	}
	if what != "runner" && what != "machine" {
		return httpx.Invalid("what", "what — runner или machine")
	}
	if when == "" {
		when = hubproto.RestartNow
	}
	if !oneOf(when, hubproto.RestartNow, hubproto.RestartAfterChats, hubproto.RestartDrain) {
		return httpx.Invalid("when", "when — now, drain или after_chats")
	}
	if m.DisabledAt != nil {
		return errDisabledMachine
	}
	ctx := r.Context()
	var op store.MachineOp
	var err error
	if what == "runner" {
		op, err = s.restartRunner(ctx, m, p.User.ID, when)
	} else {
		op, err = s.restartMachine(ctx, m, p.User.ID, when)
	}
	if err != nil {
		return err
	}
	s.auth.Audit(ctx, p.User.ID, "machine_restart", r, m.ID+" "+what+" "+when)
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"jobId": op.ID, "op": s.opView(op)})
	return nil
}

func (s *Service) hasFeature(machineID, feature string) bool {
	s.res.mu.Lock()
	defer s.res.mu.Unlock()
	return slices.Contains(s.res.features[machineID], feature)
}

// askRestart asks the agent to restart its runtime as the job op.ID, and reads the state it answers with.
func (s *Service) askRestart(ctx context.Context, m store.Machine, op store.MachineOp, when string) (hubproto.RestartEvent, error) {
	var ans hubproto.RestartEvent
	err := s.reg.DoInto(ctx, m.ID, hubproto.OpRuntimeRestart, hubproto.RestartParams{JobID: op.ID, When: when}, &ans, callTimeout)
	if err != nil {
		var he *httpx.Error
		if errors.As(err, &he) && he.Code == "provider_off" {
			return ans, errDrainOld
		}
		return ans, err
	}
	return ans, nil
}

// applyRestart moves an operation along with what the agent says of its restart job.
func (s *Service) applyRestart(ctx context.Context, m store.Machine, op store.MachineOp, ev hubproto.RestartEvent) {
	if ev.ChatsNow != nil {
		s.res.mu.Lock()
		s.res.chats[op.ID] = *ev.ChatsNow
		s.res.mu.Unlock()
	}
	switch ev.State {
	case hubproto.RestartWaiting:
		// still pending: the operation waits for the chats; the number of chats it waits for goes with the event
		if op, err := s.db.S().OpByID(ctx, op.ID); err == nil && op.Active() {
			s.publishOp(ctx, m, op)
		}
	case hubproto.RestartRestarting:
		if op.Kind == store.OpRestartMachine {
			s.continueReboot(ctx, m, op)
			return
		}
		s.setOp(ctx, m, op.ID, store.OpRunning, "", "")
	case hubproto.RestartDone:
		if op.Kind == store.OpRestartMachine {
			s.continueReboot(ctx, m, op)
			return
		}
		s.finishOp(ctx, m, op.ID, store.OpDone, "")
	case hubproto.RestartFailed:
		s.finishOp(ctx, m, op.ID, store.OpFailed, ev.Error)
	case hubproto.RestartCancelled:
		s.finishOp(ctx, m, op.ID, store.OpCancelled, "")
	}
}

// restartRunner restarts the runner of the machine. The agent drains and waits itself; the operation follows its events.
func (s *Service) restartRunner(ctx context.Context, m store.Machine, by, when string) (store.MachineOp, error) {
	if when == hubproto.RestartDrain && !s.hasFeature(m.ID, hubproto.FeatureDrain) {
		return store.MachineOp{}, errDrainOld
	}
	kind := store.OpRestartRunner
	if when == hubproto.RestartDrain {
		kind = store.OpDrain
	}
	op, err := s.newOp(ctx, m, kind, map[string]any{"what": "runner", "when": when}, by)
	if err != nil {
		return store.MachineOp{}, err
	}
	ans, err := s.askRestart(ctx, m, op, when)
	if err != nil {
		s.finishOp(ctx, m, op.ID, store.OpFailed, httpErrText(err))
		return store.MachineOp{}, err
	}
	s.applyRestart(ctx, m, op, ans)
	return s.db.S().OpByID(ctx, op.ID)
}

// restartMachine restarts the whole machine: its server is rebooted through the provider, at once or once the agent has
// drained its chats.
func (s *Service) restartMachine(ctx context.Context, m store.Machine, by, when string) (store.MachineOp, error) {
	if !provider.Supported(s.prov) {
		return store.MachineOp{}, errNotSupported
	}
	if err := s.requireManaged(ctx, m); err != nil {
		return store.MachineOp{}, err
	}
	if when == hubproto.RestartNow {
		return s.providerOp(ctx, m, store.OpRestartMachine, map[string]any{"what": "machine", "when": when}, by,
			func(ctx context.Context, pm provider.Machine) (provider.OpRef, error) { return s.prov.Reboot(ctx, pm) }, nil)
	}
	if !s.hasFeature(m.ID, hubproto.FeatureDrain) {
		return store.MachineOp{}, errDrainOld
	}
	op, err := s.newOp(ctx, m, store.OpRestartMachine, map[string]any{"what": "machine", "when": when}, by)
	if err != nil {
		return store.MachineOp{}, err
	}
	s.res.mu.Lock()
	s.res.waiters[op.ID] = true
	s.res.mu.Unlock()
	ans, err := s.askRestart(ctx, m, op, when)
	if err != nil {
		s.finishOp(ctx, m, op.ID, store.OpFailed, httpErrText(err))
		return store.MachineOp{}, err
	}
	s.applyRestart(ctx, m, op, ans)
	return s.db.S().OpByID(ctx, op.ID)
}

// continueReboot reboots the server once the agent has no chats left to wait for. It happens once per operation.
func (s *Service) continueReboot(ctx context.Context, m store.Machine, op store.MachineOp) {
	s.res.mu.Lock()
	waiting := s.res.waiters[op.ID]
	delete(s.res.waiters, op.ID)
	s.res.mu.Unlock()
	if !waiting {
		return
	}
	safego.Go("machines continueReboot", func() {
		bg := context.WithoutCancel(ctx)
		if _, err := s.runProvider(bg, m, op, func(ctx context.Context, pm provider.Machine) (provider.OpRef, error) { return s.prov.Reboot(ctx, pm) }, nil); err != nil {
			s.log.Warn("machine not rebooted after the drain", "machine", m.ID, "op", op.ID, "error", err)
		}
	})
}

func httpErrText(err error) string {
	var he *httpx.Error
	if errors.As(err, &he) {
		return clamp(he.Message, 500)
	}
	return "ошибка"
}

// onRestart is what the agent says of a restart job.
func (s *Service) onRestart(ctx context.Context, m store.Machine, ev hubproto.RestartEvent) {
	if ev.JobID == "" {
		return
	}
	op, err := s.db.S().OpByID(ctx, ev.JobID)
	if err != nil || op.MachineID != m.ID || !op.Active() {
		return
	}
	s.applyRestart(ctx, m, op, ev)
}

// ---- power ----

// powerOp switches the server off or on after the machine was disabled or enabled, when the provider manages it. It
// returns the operation, or nil when there was nothing to do (no feature, no provider, not managed, or a busy machine).
func (s *Service) powerOp(ctx context.Context, m store.Machine, by string, off bool) *store.MachineOp {
	if !s.on(ctx, m.SpaceID) || !provider.Supported(s.prov) || s.requireManaged(ctx, m) != nil {
		return nil
	}
	kind := store.OpPowerOn
	call := func(ctx context.Context, pm provider.Machine) (provider.OpRef, error) { return s.prov.PowerOn(ctx, pm) }
	if off {
		kind = store.OpPowerOff
		call = func(ctx context.Context, pm provider.Machine) (provider.OpRef, error) {
			return s.prov.PowerOff(ctx, pm)
		}
	}
	op, err := s.providerOp(ctx, m, kind, map[string]any{}, by, call, nil)
	if err != nil {
		s.log.Warn("server not switched", "machine", m.ID, "kind", kind, "error", err)
		if op.ID == "" {
			return nil
		}
	}
	return &op
}

// ---- after a restart of the hub, and the slow upkeep ----

// ResumeOps picks up the operations that were unfinished when the hub went down: the ones at the provider are watched
// again, a restart that waited for chats is written off (the hub keeps no memory of what to do after the drain).
func (s *Service) ResumeOps(ctx context.Context) {
	if s.gate == nil {
		return
	}
	ops, err := s.db.S().OpenOps(ctx)
	if err != nil {
		s.log.Warn("open operations not read", "error", err)
		return
	}
	for _, op := range ops {
		m, err := s.db.S().MachineByID(ctx, op.MachineID)
		if err != nil || m.RevokedAt != nil {
			s.finishOp(ctx, store.Machine{ID: op.MachineID}, op.ID, store.OpFailed, "машины больше нет")
			continue
		}
		switch {
		case providerKind(op.Kind) && op.ProviderOpID != "":
			after := s.afterFor(m, op)
			safego.Go("machines watch", func() { s.watch(m, op, provider.OpRef{ID: op.ProviderOpID}, after) })
		case op.Kind == store.OpRestartMachine && op.ProviderOpID != "":
			safego.Go("machines watch", func() { s.watch(m, op, provider.OpRef{ID: op.ProviderOpID}, nil) })
		case op.Kind == store.OpRestartMachine, providerKind(op.Kind):
			s.finishOp(ctx, m, op.ID, store.OpFailed, "хаб перезапустился, пока операция ждала")
		}
	}
}

// afterFor is what runs once a resumed operation ended well.
func (s *Service) afterFor(m store.Machine, op store.MachineOp) func(context.Context) {
	if op.Kind != store.OpResize {
		return nil
	}
	var want sizeRequest
	if json.Unmarshal([]byte(op.Params), &want) != nil {
		return nil
	}
	return func(ctx context.Context) {
		s.afterResize(ctx, m, provider.Resources{CPU: want.CPU, RAMGB: want.RAMGB, DiskGB: want.DiskGB})
	}
}

// afterResize remembers the size the machine is on now: from the provider's plans when it names the current one, else
// the size that was asked for (its price is unknown until the provider says it).
func (s *Service) afterResize(ctx context.Context, m store.Machine, want provider.Resources) {
	pctx, cancel := context.WithTimeout(ctx, providerTimeout)
	plans, err := s.prov.Plans(pctx, s.pmachine(ctx, m))
	cancel()
	if err == nil {
		for _, pl := range plans {
			if pl.Current {
				s.rememberPlan(ctx, m, pl)
				return
			}
		}
	}
	cur, err := s.db.S().ResourcesByMachine(ctx, m.ID)
	if err != nil && !store.IsNotFound(err) {
		return
	}
	cur.MachineID, cur.Provider, cur.UpdatedAt = m.ID, s.prov.Name(), s.now().UTC()
	cur.CPU, cur.RAMGB, cur.DiskGB = &want.CPU, &want.RAMGB, &want.DiskGB
	cur.PriceAmount, cur.PriceCurrency, cur.PricePeriod, cur.Plan = nil, "", "", "{}"
	if err := s.db.S().PutResources(ctx, cur); err != nil {
		s.log.Warn("resources not saved", "machine", m.ID, "error", err)
	}
	s.Publish(ctx, m)
}

// Maintain writes off operations that stand without a word and deletes old load buckets. Call it every minute or so.
func (s *Service) Maintain(ctx context.Context) {
	if s.gate == nil {
		return
	}
	now := s.now()
	if ops, err := s.db.S().OpenOps(ctx); err == nil {
		for _, op := range ops {
			limit := staleWait
			switch {
			case providerKind(op.Kind) && op.ProviderOpID == "":
				limit = staleCall
			case providerKind(op.Kind) || op.ProviderOpID != "":
				limit = staleProvider
			}
			if now.Sub(op.UpdatedAt) < limit {
				continue
			}
			m, err := s.db.S().MachineByID(ctx, op.MachineID)
			if err != nil {
				m = store.Machine{ID: op.MachineID}
			}
			s.finishOp(ctx, m, op.ID, store.OpFailed, "операция не двигалась и снята")
		}
	}
	s.res.mu.Lock()
	due := now.Sub(s.res.lastPrune) >= pruneEvery
	if due {
		s.res.lastPrune = now
	}
	s.res.mu.Unlock()
	if !due {
		return
	}
	before := now.Add(-store.MetricsRetention).UnixMilli()
	for {
		n, err := s.db.S().PruneMetrics(ctx, before, pruneBatch)
		if err != nil {
			s.log.Warn("load buckets not pruned", "error", err)
			return
		}
		if n < pruneBatch || ctx.Err() != nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}
