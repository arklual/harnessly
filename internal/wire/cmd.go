package wire

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

// The answers of opIds are kept OpsKeep, at most OpsMax of them (§3, §4.5).
const (
	OpsKeep = 24 * time.Hour
	OpsMax  = 5000
	// maxCmdBody is the largest command body: a send of MaxText UTF-16 units, escaped, with room to spare.
	maxCmdBody = 2 << 20
	// opsSaveEvery is how often, at most, the kept answers are written (§4.5: ≤ 5 times a second).
	opsSaveEvery = 200 * time.Millisecond
)

// OpLog makes the commands idempotent by opId: the same opId with the same body gets the same answer, also while the
// first one still runs (it waits for that one's answer), and with another body refused{conflict}. A refused{timeout}
// is not kept: the command may still finish, and a retry gets its answer or runs it again.
type OpLog struct {
	now  func() time.Time
	path string // "" keeps the answers in memory only

	mu     sync.Mutex
	m      map[string]*opEntry
	saving bool
}

type opEntry struct {
	FP   string    `json:"fp"`
	Res  CmdResult `json:"res"`
	At   int64     `json:"at"`
	done chan struct{}
}

// NewOpLog reads the answers kept in path ("" for none) and keeps the new ones there.
func NewOpLog(path string) *OpLog {
	o := &OpLog{now: time.Now, path: path, m: map[string]*opEntry{}}
	if path == "" {
		return o
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return o
	}
	var kept map[string]*opEntry
	if err := json.Unmarshal(raw, &kept); err != nil {
		jsonlog.Log("warn", "v2: kept command answers unreadable, starting empty", jsonlog.F{K: "file", V: path}, jsonlog.F{K: "error", V: err.Error()})
		return o
	}
	cutoff := o.now().Add(-OpsKeep).UnixMilli()
	for k, e := range kept {
		if e != nil && e.At >= cutoff {
			o.m[k] = e
		}
	}
	return o
}

// Fingerprint is what tells two bodies of one opId apart: the op and the command as it was read.
func Fingerprint(cmd Command) string {
	b, _ := json.Marshal(cmd)
	sum := sha256.Sum256(append([]byte(string(cmd.Op())+"\n"), b...))
	return hex.EncodeToString(sum[:])
}

// Run answers cmd once per opId within budget: run gets a context that ends with the budget, and when it has not
// answered by then the caller gets refused{timeout} while run goes on.
func (o *OpLog) Run(ctx context.Context, cmd Command, budget time.Duration, run func(context.Context) CmdResult) CmdResult {
	key, fp := cmd.OpKey(), Fingerprint(cmd)
	timer := time.NewTimer(budget)
	defer timer.Stop()
	o.mu.Lock()
	e := o.m[key]
	if e != nil && e.FP != fp {
		o.mu.Unlock()
		return Refused(RefuseConflict)
	}
	if e == nil {
		e = &opEntry{FP: fp, done: make(chan struct{})}
		o.m[key] = e
		o.mu.Unlock()
		go o.finish(key, e, budget, run)
	} else {
		o.mu.Unlock()
	}
	if e.done == nil {
		return e.Res
	}
	select {
	case <-e.done:
		return e.Res
	case <-timer.C:
		return Refused(RefuseTimeout)
	case <-ctx.Done():
		return Refused(RefuseTimeout)
	}
}

func (o *OpLog) finish(key string, e *opEntry, budget time.Duration, run func(context.Context) CmdResult) {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	res := run(ctx)
	o.mu.Lock()
	e.Res, e.At = res, o.now().UnixMilli()
	if res.R == ResultRefused && res.Code == RefuseTimeout {
		delete(o.m, key)
	} else {
		o.prune()
		o.scheduleSave()
	}
	o.mu.Unlock()
	close(e.done)
}

// prune drops the answers past OpsKeep and, past OpsMax, the oldest; under o.mu.
func (o *OpLog) prune() {
	cutoff := o.now().Add(-OpsKeep).UnixMilli()
	for k, e := range o.m {
		if e.At != 0 && e.At < cutoff {
			delete(o.m, k)
		}
	}
	if len(o.m) <= OpsMax {
		return
	}
	keys := make([]string, 0, len(o.m))
	for k, e := range o.m {
		if e.At != 0 {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return o.m[keys[i]].At < o.m[keys[j]].At })
	for _, k := range keys[:max(0, len(o.m)-OpsMax)] {
		delete(o.m, k)
	}
}

func (o *OpLog) scheduleSave() {
	if o.path == "" || o.saving {
		return
	}
	o.saving = true
	time.AfterFunc(opsSaveEvery, o.save)
}

// save writes the finished answers: tmp, fsync, rename.
func (o *OpLog) save() {
	o.mu.Lock()
	o.saving = false
	kept := make(map[string]*opEntry, len(o.m))
	for k, e := range o.m {
		if e.At != 0 {
			kept[k] = e
		}
	}
	raw, err := json.Marshal(kept)
	o.mu.Unlock()
	if err == nil {
		err = writeFileAtomic(o.path, raw)
	}
	if err != nil {
		jsonlog.Log("warn", "v2: command answers not kept", jsonlog.F{K: "file", V: o.path}, jsonlog.F{K: "error", V: err.Error()})
	}
}

func writeFileAtomic(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		_ = os.Remove(f.Name())
	}
	return err
}

// Cmd serves POST /api/v2/cmd/<op>. Every answer is a CmdResult with status 200, a refusal too: a status other than
// 2xx is the page's failed{http}. It comes within limits.cmdBudgetMs.
func (s *Server) Cmd(w http.ResponseWriter, r *http.Request, op Op) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	device := r.Header.Get(DeviceHeader)
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCmdBody))
	if err != nil {
		writeJSON(w, http.StatusOK, Refused(RefuseBadRequest))
		return
	}
	cmd, err := DecodeCommand(op, body)
	if err != nil || device == "" {
		writeJSON(w, http.StatusOK, Refused(RefuseBadRequest))
		return
	}
	budget := time.Duration(s.Limits.CmdBudgetMs) * time.Millisecond
	res := s.Ops.Run(r.Context(), cmd, budget, func(ctx context.Context) CmdResult {
		return s.Service.Do(ctx, device, cmd)
	})
	if err := res.Validate(); err != nil {
		jsonlog.Log("error", "v2: a command answered out of contract", jsonlog.F{K: "op", V: string(op)}, jsonlog.F{K: "error", V: err.Error()})
		res = Refused(RefuseInternal)
	}
	writeJSON(w, http.StatusOK, res)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := Marshal(v)
	if err != nil {
		status, b = http.StatusInternalServerError, []byte(`{"r":"refused","code":"internal"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// Clear forgets every kept answer, for a fake runtime that starts its fixtures again.
func (o *OpLog) Clear() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.m = map[string]*opEntry{}
	o.scheduleSave()
}
