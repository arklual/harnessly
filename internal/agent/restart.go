package agent

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// runtime.restart: the hub asks the agent to restart the runtime (the page and the chats' bookkeeping; tmux and the agents
// in it live on). when:
//
//	now          at once
//	after_chats  when no chat has an agent at work; new chats may start meanwhile
//	drain        new chats are refused (routefilter) until no chat is at work, then the restart
//
// The answer is immediate ({state, chatsNow}); progress is the `restart` event. Only one restart is live at a time. A
// restart that waits can be taken back with runtime.restart.cancel. The runtime restarts by exiting (its service starts
// it again), so "done" is said when it answers again.

const (
	restartPoll    = 5 * time.Second
	restartSettle  = 3 * time.Second
	restartBackMax = 2 * time.Minute
)

// restarter holds the one live restart job.
type restarter struct {
	// Injected.
	chats    func() *int
	restart  func() error // asks the runtime to exit; returns once it said it will
	back     func() bool  // the runtime answers again
	emit     func(hubproto.RestartEvent)
	poll     time.Duration
	settle   time.Duration
	backMax  time.Duration
	draining atomic.Bool

	mu  sync.Mutex
	job *restartJob
	wg  sync.WaitGroup
}

type restartJob struct {
	id         string
	when       string
	cancel     context.CancelFunc
	restarting bool
}

// Draining says new chats are refused now.
func (r *restarter) Draining() bool { return r != nil && r.draining.Load() }

// Start begins a job. The same job id again is the same job (the hub retried). A different one while a job lives is busy.
func (r *restarter) Start(ctx context.Context, id, when string) (hubproto.RestartEvent, error) {
	switch when {
	case hubproto.RestartNow, hubproto.RestartAfterChats, hubproto.RestartDrain:
	default:
		return hubproto.RestartEvent{}, invalid("when must be now, after_chats or drain")
	}
	r.mu.Lock()
	if j := r.job; j != nil {
		r.mu.Unlock()
		if j.id == id {
			return r.snapshot(j), nil
		}
		return hubproto.RestartEvent{}, hubproto.Errf(hubproto.CodeBusy, "a restart is already under way (job %s)", j.id)
	}
	jctx, cancel := context.WithCancel(ctx)
	j := &restartJob{id: id, when: when, cancel: cancel}
	r.job = j
	r.mu.Unlock()
	if when == hubproto.RestartDrain {
		r.draining.Store(true)
	}
	first := r.snapshot(j)
	if when == hubproto.RestartNow {
		first.State = hubproto.RestartRestarting
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.run(jctx, j)
	}()
	return first, nil
}

func (r *restarter) snapshot(j *restartJob) hubproto.RestartEvent {
	st := hubproto.RestartWaiting
	r.mu.Lock()
	if j.restarting {
		st = hubproto.RestartRestarting
	}
	r.mu.Unlock()
	return hubproto.RestartEvent{JobID: j.id, State: st, ChatsNow: r.chats()}
}

// Cancel takes back a job that still waits.
func (r *restarter) Cancel(id string) (hubproto.RestartEvent, error) {
	chats := r.chats() // asked before the lock: it may take the runtime a while
	r.mu.Lock()
	j := r.job
	if j == nil || j.id != id {
		r.mu.Unlock()
		return hubproto.RestartEvent{}, hubproto.Errf(hubproto.CodeNotFound, "no restart %s is waiting", id)
	}
	if j.restarting {
		r.mu.Unlock()
		return hubproto.RestartEvent{}, hubproto.Errf(hubproto.CodeBusy, "the runtime is already restarting")
	}
	r.job = nil
	j.cancel() // under the lock: run checks the context under it before it starts restarting
	r.draining.Store(false)
	ev := hubproto.RestartEvent{JobID: id, State: hubproto.RestartCancelled, ChatsNow: chats}
	// Said under the lock, so that no progress of the job comes after it.
	r.emit(ev)
	r.mu.Unlock()
	return ev, nil
}

// Wait returns once the jobs' goroutines have.
func (r *restarter) Wait() { r.wg.Wait() }

// say reports progress of a job that is still the live one.
func (r *restarter) say(j *restartJob, ev hubproto.RestartEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.job == j {
		r.emit(ev)
	}
}

func (r *restarter) finish(j *restartJob) {
	r.mu.Lock()
	if r.job == j {
		r.job = nil
	}
	r.mu.Unlock()
	r.draining.Store(false)
}

func (r *restarter) run(ctx context.Context, j *restartJob) {
	if j.when != hubproto.RestartNow {
		last := -2
		r.say(j, hubproto.RestartEvent{JobID: j.id, State: hubproto.RestartWaiting, ChatsNow: r.chats()})
		t := time.NewTicker(r.poll)
		defer t.Stop()
		for {
			if n := r.chats(); n != nil && *n == 0 {
				break
			} else if n != nil && *n != last {
				last = *n
				r.say(j, hubproto.RestartEvent{JobID: j.id, State: hubproto.RestartWaiting, ChatsNow: n})
			}
			select {
			case <-ctx.Done():
				return // cancelled: Cancel said so
			case <-t.C:
			}
		}
	}
	// From here it is not taken back.
	r.mu.Lock()
	if ctx.Err() != nil {
		r.mu.Unlock()
		return
	}
	j.restarting = true
	r.mu.Unlock()
	r.say(j, hubproto.RestartEvent{JobID: j.id, State: hubproto.RestartRestarting})
	if err := r.restart(); err != nil {
		r.say(j, hubproto.RestartEvent{JobID: j.id, State: hubproto.RestartFailed, Error: err.Error()})
		r.finish(j)
		return
	}
	time.Sleep(r.settle)
	deadline := time.Now().Add(r.backMax)
	for time.Now().Before(deadline) {
		if r.back() {
			r.say(j, hubproto.RestartEvent{JobID: j.id, State: hubproto.RestartDone})
			r.finish(j)
			return
		}
		time.Sleep(r.poll / 2)
	}
	r.say(j, hubproto.RestartEvent{JobID: j.id, State: hubproto.RestartFailed, Error: "the runtime did not come back in time"})
	r.finish(j)
}

// newRestarter wires a restarter to this hub link and its runtime.
func (h *hub) newRestarter() *restarter {
	return &restarter{
		chats:   h.chatsNow,
		restart: func() error { _, err := h.a.callRuntime(h.a.cfg.RuntimeAPI+"/restart", map[string]any{}); return err },
		back: func() bool {
			_, err := h.a.callRuntime(h.a.cfg.RuntimeAPI+"/load", map[string]any{})
			return err == nil
		},
		emit: func(ev hubproto.RestartEvent) { h.emit(hubproto.EventRestart, "restart", ev) },
		poll: restartPoll, settle: restartSettle, backMax: restartBackMax,
	}
}

// diskPath is the folder whose filesystem the machine's disk numbers are about.
func diskPath(cfg Config) string {
	for _, p := range []string{cfg.WorkDir, cfg.Home, "/"} {
		if p != "" {
			return p
		}
	}
	return "/"
}

// opRestart serves runtime.restart and runtime.restart.cancel.
func (h *hub) opRestart(op string, req map[string]json.RawMessage) (any, func(), error) {
	if h.observe {
		return nil, nil, h.allow(op, false, "restarting the runner")
	}
	if h.a.cfg.RuntimePort == 0 {
		return nil, nil, hubproto.Errf(hubproto.CodeUnsupported, "this machine has no runtime to restart")
	}
	id, _ := field[string](req, "jobId")
	if !jobIDPattern.MatchString(id) {
		return nil, nil, invalid("jobId must be 32 lower-case hex characters")
	}
	if op == hubproto.OpRuntimeRestartCancel {
		ev, err := h.restarts.Cancel(id)
		h.audit.Record(op, err == nil, map[string]any{"jobId": id})
		if err != nil {
			return nil, nil, err
		}
		return ev, nil, nil
	}
	when, _ := field[string](req, "when")
	if when == "" {
		when = hubproto.RestartNow
	}
	ev, err := h.restarts.Start(h.ctx, id, when)
	h.audit.Record(op, err == nil, map[string]any{"jobId": id, "when": when})
	if err != nil {
		return nil, nil, err
	}
	return ev, nil, nil
}
