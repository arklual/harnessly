package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The machine setup of the environment list (docs/hub/environment.md, «Machine setup») is done by the runtime, which runs
// as the person and holds the safe; this agent is sandboxed and runs nothing of the hub's choosing. It is the courier:
// the list is in environment.json (already), a run the hub asks for goes to environment-requests/<runId>.json, and what
// the runtime writes back (environment-setup.json, environment-runs/<runId>.json) is told to the hub.

const (
	envSetupPoll     = 2 * time.Second
	envRunsResend    = 24 * time.Hour
	envRunItemsMax   = 500
	envRunRequestMax = 4 << 20
)

// readSetupState is the runtime's account of the setup; the zero value when there is none.
func (k *envKeeper) readSetupState() hubproto.EnvSetupState {
	var st hubproto.EnvSetupState
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(k.path), hubproto.EnvSetupFile))
	if err != nil || json.Unmarshal(raw, &st) != nil {
		return hubproto.EnvSetupState{}
	}
	return st
}

// setupStatuses are the outcomes of the setup items for the list whose hash is hash: the runtime's when it worked on
// that very list, else everything waits.
func (k *envKeeper) setupStatuses(hash string) map[string]hubproto.EnvApplied {
	st := k.readSetupState()
	if st.Hash == "" || st.Hash != hash {
		return nil
	}
	out := map[string]hubproto.EnvApplied{}
	for _, a := range st.Items {
		out[a.ItemID] = a
	}
	return out
}

// setupFileSig is what changed when the runtime wrote its account.
func (k *envKeeper) setupFileSig() string {
	info, err := os.Stat(filepath.Join(filepath.Dir(k.path), hubproto.EnvSetupFile))
	if err != nil {
		return ""
	}
	return info.ModTime().String() + "/" + strconv.FormatInt(info.Size(), 10)
}

// runnerAlive says the runtime's setup is running: its heartbeat in the link folder is fresh.
func (h *hub) runnerAlive() bool {
	raw, err := os.ReadFile(filepath.Join(h.dir, hubproto.EnvRunnerFile))
	var b hubproto.EnvRunnerBeat
	if err != nil || json.Unmarshal(raw, &b) != nil {
		return false
	}
	return time.Since(time.UnixMilli(b.At)) < hubproto.EnvRunnerStale
}

// setupOffered is what `hello` says: the policy allows the setup and a runtime is there to do it.
func (h *hub) setupOffered() bool { return h.setupAllowed() && h.runnerAlive() }

// setupAllowed says whether this machine may be set up by the list. An own link follows the policy's `setup`; an observe
// link (the machine is run by its own cluster) is off unless the operator opted in on the machine with AGENT_HUB_SETUP=on.
// The kill switch turns it off either way.
func (h *hub) setupAllowed() bool {
	if h.policy.Disabled() {
		return false
	}
	if h.observe {
		return os.Getenv("AGENT_HUB_SETUP") == "on"
	}
	return h.policy.Current().Setup
}

// writeSetupAllow tells the runtime the decision, when it changed since the last time. It answers whether it did.
func (h *hub) writeSetupAllow() bool {
	// Decided under the lock that writes it: two callers (a policy change, the watcher) cannot leave the older decision last.
	h.allowMu.Lock()
	defer h.allowMu.Unlock()
	allowed := h.setupAllowed()
	if h.allowKnown && h.allowWritten == allowed {
		return false
	}
	if err := writeFileAtomic(filepath.Join(h.dir, hubproto.EnvAllowFile), mustMarshal(hubproto.EnvAllow{Setup: allowed, At: time.Now().UnixMilli()})); err != nil {
		Log("warn", "the setup permission was not written; the runtime sets nothing up until it is", F{"error", err.Error()})
		return false
	}
	h.allowKnown, h.allowWritten = true, allowed
	return true
}

// setupChanged: whether the machine may be set up, or how the runtime did, changed. The items' outcomes are worked out
// again and the hub told; when what `hello` offers changed too, the link calls again so the hub hears the new features
// (it sends the setup kinds only to a machine that offers them).
func (h *hub) setupChanged() {
	if h.environment.Reapply() {
		h.emit(hubproto.EventEnvState, "state", h.environment.State())
	}
	h.mu.Lock()
	said := h.setupSaid
	h.mu.Unlock()
	if h.setupOffered() != said {
		select {
		case h.relink <- struct{}{}:
		default:
		}
	}
}

func mustMarshal(v any) []byte { raw, _ := json.Marshal(v); return raw }

// watchEnvSetup tells the hub when the runtime's account changes what an item's outcome is.
func (h *hub) watchEnvSetup(ctx context.Context) {
	last := h.environment.setupFileSig()
	h.writeSetupAllow()
	t := time.NewTicker(envSetupPoll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		changed := h.writeSetupAllow()
		if alive := h.runnerAlive(); alive != h.environment.aliveSeen {
			h.environment.aliveSeen, changed = alive, true
		}
		if sig := h.environment.setupFileSig(); sig != last || changed {
			last = sig
			h.setupChanged()
		}
		h.shipRuns(false)
	}
}

// shipRuns sends the runs the runtime has written or rewritten since they were last sent as env.run events. With all, it
// sends every run of the last 24 hours (a link just came up). It only looks at the files' times; what changed is read.
func (h *hub) shipRuns(all bool) {
	dir := filepath.Join(h.dir, hubproto.EnvRunsDir)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	h.runsMu.Lock()
	defer h.runsMu.Unlock()
	if h.runsSent == nil {
		h.runsSent = map[string]time.Time{}
	}
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) > envRunsResend {
			continue
		}
		if !all && h.runsSent[name].Equal(info.ModTime()) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		var run hubproto.EnvRun
		if err != nil || json.Unmarshal(raw, &run) != nil || run.RunID+".json" != name {
			continue // half-written or not ours; the next write is looked at again
		}
		h.runsSent[name] = info.ModTime()
		h.emit(hubproto.EventEnvRun, "run", run)
	}
}

// opEnvRun takes a run of the machine setup from the hub: the request goes to the runtime as a file, the answer is at once.
func (h *hub) opEnvRun(req map[string]json.RawMessage) (any, func(), error) {
	runID, _ := field[string](req, "runId")
	mode, _ := field[string](req, "mode")
	by, _ := field[string](req, "by")
	items, _ := field[[]hubproto.EnvItem](req, "items")
	switch {
	case !hubproto.EnvRunIDRe.MatchString(runID):
		return nil, nil, invalid("runId is not a plain id")
	case !hubproto.ValidRunMode(mode) || mode == hubproto.RunApply:
		return nil, nil, invalid("mode must be update, check or sandbox")
	case len(items) > envRunItemsMax:
		return nil, nil, invalid("too many items")
	case len(items) > 0 && mode != hubproto.RunSandbox:
		return nil, nil, invalid("items are only for a sandbox run")
	case len(by) > 200:
		return nil, nil, invalid("by is too long")
	}
	for _, it := range items {
		if !envItemID.MatchString(it.ItemID) {
			return nil, nil, invalid("an item id is not a plain name")
		}
		if len(it.Source) > envMaxSource {
			return nil, nil, invalid("an item's source is too large")
		}
	}
	path := filepath.Join(h.dir, hubproto.EnvRequestsDir, runID+".json")
	if _, err := os.Stat(path); err == nil {
		return nil, nil, hubproto.Errf(hubproto.CodeBusy, "a run %s waits to start", runID)
	}
	if _, err := os.Stat(filepath.Join(h.dir, hubproto.EnvRunsDir, runID+".json")); err == nil {
		return nil, nil, hubproto.Errf(hubproto.CodeBusy, "a run %s is known already", runID)
	}
	raw, err := json.Marshal(hubproto.EnvRunRequest{RunID: runID, Mode: mode, Items: items, By: by, At: time.Now().UnixMilli()})
	if err != nil || len(raw) > envRunRequestMax {
		return nil, nil, invalid("the request is too large")
	}
	if err := writeFileAtomic(path, raw); err != nil {
		h.audit.Record(hubproto.OpEnvRun, false, map[string]any{"mode": mode})
		return nil, nil, err
	}
	h.audit.Record(hubproto.OpEnvRun, true, map[string]any{"runId": runID, "mode": mode, "items": len(items)})
	return map[string]any{"runId": runID, "accepted": true}, nil, nil
}
