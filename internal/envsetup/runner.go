package envsetup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// held is environment.json as the agent writes it (internal/agent/envkeeper.go); only what setup reads.
type held struct {
	Hash  string             `json:"hash"`
	Items []hubproto.EnvItem `json:"items"`
}

// Run looks at the link folder until ctx ends: a list the setup has not worked on gets an apply run, a request of the
// hub gets its run. One run goes at a time; the next waits. It returns when ctx ends (a run in progress is stopped).
func (r *Runner) Run(ctx context.Context) {
	r.Recover()
	poll := r.Poll
	if poll <= 0 {
		poll = defaultPoll
	}
	t := time.NewTicker(poll)
	defer t.Stop()
	// The heartbeat goes on while a run works: a run of minutes (an apply after a reboot) left it stale, the agent
	// stopped offering the setup and called the hub again, twice per run, and every call cut the page's streams.
	go r.beatLoop(ctx, poll)
	for {
		r.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Recover ends what a crash left running: a run file that still says running is an error, «прервано перезапуском». The
// list is worked on again by the next Tick (its hash was never written), and the ledger tells what need not be redone.
func (r *Runner) Recover() {
	for _, run := range r.runs() {
		if run.Status != hubproto.EnvRunning {
			continue
		}
		for i := range run.Steps {
			if run.Steps[i].Status == hubproto.EnvRunning {
				run.Steps[i].Status, run.Steps[i].Message = hubproto.EnvError, "прервано перезапуском"
			}
		}
		run.Status, run.FinishedAt = hubproto.EnvError, r.now().UnixMilli()
		if len(run.Steps) == 0 {
			run.Steps = []hubproto.EnvStep{{ItemID: "run", Kind: hubproto.EnvKindScript, Name: "Запуск", Status: hubproto.EnvError, Message: "прервано перезапуском"}}
		}
		if err := r.writeRun(run); err != nil {
			r.logf("warn", "envsetup: interrupted run not marked", "run", run.RunID, "error", err.Error())
		}
	}
}

// Tick does what is due once: a request of the hub, else the apply of a list not worked on yet.
func (r *Runner) Tick(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	r.pruneRuns()
	r.beat()
	if !r.allowed() {
		// The agent has not said the policy allows the setup (or says it does not): nothing runs, and a request that is
		// there is answered with an error rather than kept for the day the policy opens.
		r.refuseRequests("настройка машины выключена на этой машине (политика setup)", false)
		return
	}
	r.refuseRequests("машина не взялась за запуск вовремя", true)
	if req, path, ok := r.nextRequest(); ok {
		r.runRequest(ctx, req, path)
		return
	}
	h, ok := r.readHeld()
	if !ok {
		return
	}
	st := r.readState()
	if st.Hash == h.Hash && !r.secretArrived(h.Hash) && !r.retryDue(st) && !st.Moved && !r.headsMoved(ctx, &st, h.Items) {
		return
	}
	items := setupItems(h.Items)
	if len(items) == 0 {
		// Nothing for the machine in this list: say so, so that the agent stops calling the setup items pending.
		st.Hash, st.Items, st.At = h.Hash, []hubproto.EnvApplied{}, r.now().UnixMilli()
		r.keepLedger(&st, items)
		if err := r.writeState(st); err != nil {
			r.logf("warn", "envsetup: state not written", "error", err.Error())
		}
		return
	}
	id := "apply-" + short(h.Hash) + "-" + fmt.Sprint(r.now().UnixMilli())
	r.execute(ctx, hubproto.EnvRunRequest{RunID: id, Mode: hubproto.RunApply, At: r.now().UnixMilli()}, h.Hash, items, "")
}

// retryDue: the list was worked on, something in it is not done (an error, or waiting), and the last work was half an
// hour ago. A machine where everything is done is left alone.
func (r *Runner) retryDue(st hubproto.EnvSetupState) bool {
	if st.At == 0 || r.now().Sub(time.UnixMilli(st.At)) < retryEvery {
		return false
	}
	for _, it := range st.Items {
		if it.Status != hubproto.EnvOK {
			return true
		}
	}
	return false
}

// beatLoop keeps the heartbeat fresh until ctx ends, whatever Tick is busy with.
func (r *Runner) beatLoop(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.beat()
		}
	}
}

// beat writes the runtime's heartbeat (hubproto.EnvRunnerFile) every 30 seconds.
func (r *Runner) beat() {
	r.beatMu.Lock()
	defer r.beatMu.Unlock()
	now := r.now()
	if !r.lastBeat.IsZero() && now.Sub(r.lastBeat) < hubproto.EnvRunnerBeatEvery {
		return
	}
	r.lastBeat = now
	if err := writeJSON(filepath.Join(r.LinkDir, hubproto.EnvRunnerFile), hubproto.EnvRunnerBeat{PID: os.Getpid(), Version: r.Version, At: now.UnixMilli()}); err != nil {
		r.logf("warn", "envsetup: heartbeat not written", "error", err.Error())
	}
}

// allowed is the agent's word (hubproto.EnvAllowFile); a missing or unreadable file is a no.
func (r *Runner) allowed() bool {
	raw, err := os.ReadFile(filepath.Join(r.LinkDir, hubproto.EnvAllowFile))
	var a hubproto.EnvAllow
	return err == nil && json.Unmarshal(raw, &a) == nil && a.Setup
}

func short(hash string) string {
	if len(hash) > 8 {
		return hash[:8]
	}
	return hash
}

// secretArrived says the secret a finished run waited for is in the safe now, so the list is worked on again.
func (r *Runner) secretArrived(hash string) bool {
	if len(r.waited) == 0 || r.waitedHash != hash {
		return false
	}
	for _, n := range r.waited {
		if r.Secrets != nil && r.Secrets.Has(n) {
			r.waited = nil
			return true
		}
	}
	return false
}

func (r *Runner) readHeld() (held, bool) {
	raw, err := os.ReadFile(filepath.Join(r.LinkDir, "environment.json"))
	if err != nil {
		return held{}, false
	}
	var h held
	if json.Unmarshal(raw, &h) != nil || h.Hash == "" || hubproto.EnvHash(h.Items) != h.Hash {
		return held{}, false
	}
	return h, true
}

// setupItems are the items the machine does something with, in the order of the steps: access, repositories, apps,
// Claude's settings and plugins, then the scripts by their place in the list (those without one, in the order they came, last).
func setupItems(all []hubproto.EnvItem) []hubproto.EnvItem {
	var access, repos, appItems, claude, scripts []hubproto.EnvItem
	for _, it := range all {
		switch it.Kind {
		case hubproto.EnvKindAccess:
			access = append(access, it)
		case hubproto.EnvKindRepo:
			repos = append(repos, it)
		case hubproto.EnvKindApp:
			appItems = append(appItems, it)
		case hubproto.EnvKindClaudeSettings, hubproto.EnvKindPlugin:
			claude = append(claude, it)
		case hubproto.EnvKindScript:
			scripts = append(scripts, it)
		}
	}
	sort.SliceStable(scripts, func(i, j int) bool {
		a, b := scripts[i].Order, scripts[j].Order
		if a == 0 || b == 0 {
			return a != 0 && b == 0
		}
		return a < b
	})
	return slices.Concat(access, repos, appItems, claude, scripts)
}

func (r *Runner) readState() hubproto.EnvSetupState {
	var st hubproto.EnvSetupState
	raw, err := os.ReadFile(filepath.Join(r.LinkDir, hubproto.EnvSetupFile))
	if err == nil && json.Unmarshal(raw, &st) != nil {
		st = hubproto.EnvSetupState{}
	}
	if st.Once == nil {
		st.Once = map[string]string{}
	}
	if st.Items == nil {
		st.Items = []hubproto.EnvApplied{}
	}
	return st
}

func (r *Runner) writeState(st hubproto.EnvSetupState) error {
	return writeJSON(filepath.Join(r.LinkDir, hubproto.EnvSetupFile), st)
}

// keepLedger drops the ledger of scripts that left the list.
func (r *Runner) keepLedger(st *hubproto.EnvSetupState, items []hubproto.EnvItem) {
	in := map[string]bool{}
	for _, it := range items {
		in[it.ItemID] = true
	}
	for id := range st.Once {
		if !in[id] {
			delete(st.Once, id)
		}
	}
}

func (r *Runner) runPath(id string) string {
	return filepath.Join(r.LinkDir, hubproto.EnvRunsDir, id+".json")
}

func (r *Runner) writeRun(run hubproto.EnvRun) error {
	if run.Steps == nil {
		run.Steps = []hubproto.EnvStep{}
	}
	return writeJSON(r.runPath(run.RunID), run)
}

// runs reads the run files that parse.
func (r *Runner) runs() []hubproto.EnvRun {
	ents, _ := os.ReadDir(filepath.Join(r.LinkDir, hubproto.EnvRunsDir))
	var out []hubproto.EnvRun
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(r.LinkDir, hubproto.EnvRunsDir, e.Name()))
		var run hubproto.EnvRun
		if err != nil || json.Unmarshal(raw, &run) != nil || run.RunID != strings.TrimSuffix(e.Name(), ".json") {
			continue
		}
		out = append(out, run)
	}
	return out
}

// pruneRuns removes run files older than hubproto.EnvRunKeepDays, at most once an hour.
func (r *Runner) pruneRuns() {
	now := r.now()
	if now.Sub(r.lastPrune) < pruneEvery {
		return
	}
	r.lastPrune = now
	dir := filepath.Join(r.LinkDir, hubproto.EnvRunsDir)
	ents, _ := os.ReadDir(dir)
	cut := now.Add(-hubproto.EnvRunKeepDays * 24 * time.Hour)
	for _, e := range ents {
		if info, err := e.Info(); err == nil && !e.IsDir() && info.ModTime().Before(cut) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

type requestFile struct {
	path string
	mod  time.Time
}

// requestFiles are the requests of the hub, the oldest first.
func (r *Runner) requestFiles() []requestFile {
	dir := filepath.Join(r.LinkDir, hubproto.EnvRequestsDir)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []requestFile
	for _, e := range ents {
		if info, err := e.Info(); err == nil && !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			out = append(out, requestFile{filepath.Join(dir, e.Name()), info.ModTime()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].mod.Before(out[j].mod) })
	return out
}

func readRequest(path string) (hubproto.EnvRunRequest, bool) {
	var req hubproto.EnvRunRequest
	raw, err := os.ReadFile(path)
	ok := err == nil && json.Unmarshal(raw, &req) == nil && req.RunID+".json" == filepath.Base(path) &&
		hubproto.EnvRunIDRe.MatchString(req.RunID) && hubproto.ValidRunMode(req.Mode)
	return req, ok
}

// refuse answers a request that will not be run with a run of its own that says so, and removes the request: the hub
// must hear about it, not wait. A file whose run id cannot be told is only removed and logged.
func (r *Runner) refuse(f requestFile, msg string) {
	req, ok := readRequest(f.path)
	if !ok {
		id := strings.TrimSuffix(filepath.Base(f.path), ".json")
		if !hubproto.EnvRunIDRe.MatchString(id) {
			r.logf("warn", "envsetup: request dropped, it cannot be read", "file", filepath.Base(f.path))
			_ = os.Remove(f.path)
			return
		}
		req = hubproto.EnvRunRequest{RunID: id, Mode: hubproto.RunUpdate}
		if !hubproto.ValidRunMode(req.Mode) {
			req.Mode = hubproto.RunUpdate
		}
		msg = "запрос не прочитан: " + msg
	}
	now := r.now().UnixMilli()
	run := hubproto.EnvRun{RunID: req.RunID, Mode: req.Mode, By: req.By, Status: hubproto.EnvError, StartedAt: now, FinishedAt: now,
		Steps: []hubproto.EnvStep{{ItemID: "run", Kind: hubproto.EnvKindScript, Name: "Запуск", Status: hubproto.EnvError, Message: msg}}}
	if h, ok := r.readHeld(); ok {
		run.Hash = h.Hash
	}
	if err := r.writeRun(run); err != nil {
		r.logf("warn", "envsetup: refused request not answered", "run", req.RunID, "error", err.Error())
		return
	}
	r.logf("warn", "envsetup: request refused", "run", req.RunID, "why", msg)
	_ = os.Remove(f.path)
}

// refuseRequests refuses the requests (all, or with onlyStale the ones that waited longer than requestTTL).
func (r *Runner) refuseRequests(msg string, onlyStale bool) {
	for _, f := range r.requestFiles() {
		if onlyStale && r.now().Sub(f.mod) <= requestTTL {
			continue
		}
		r.refuse(f, msg)
	}
}

// nextRequest is the oldest request of the hub that can be read. One that cannot is answered with an error run.
func (r *Runner) nextRequest() (hubproto.EnvRunRequest, string, bool) {
	for _, f := range r.requestFiles() {
		if req, ok := readRequest(f.path); ok {
			return req, f.path, true
		}
		r.refuse(f, "запрос повреждён")
	}
	return hubproto.EnvRunRequest{}, "", false
}

func (r *Runner) runRequest(ctx context.Context, req hubproto.EnvRunRequest, path string) {
	if req.Mode == hubproto.RunUpdate && r.OnUpdate != nil {
		r.OnUpdate()
	}
	items, hash := []hubproto.EnvItem(nil), ""
	if h, ok := r.readHeld(); ok {
		items, hash = h.Items, h.Hash
	}
	if req.Mode == hubproto.RunSandbox && len(req.Items) > 0 {
		items, hash = req.Items, hubproto.EnvHash(req.Items)
	}
	r.execute(ctx, req, hash, setupItems(items), path)
}

func writeJSON(path string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
