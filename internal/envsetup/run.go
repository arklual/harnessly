package envsetup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/secretmask"
)

// runSlot is held by the run that goes.
var runSlot = make(chan struct{}, 1)

// execution is one run: the steps, their order and what is known along the way.
type execution struct {
	r       *Runner
	ctx     context.Context
	run     hubproto.EnvRun
	items   []hubproto.EnvItem // the steps' items, index for index
	srcs    []source
	srcErrs []error

	modifies bool // apply, update and sandbox change things; check does not
	sandbox  bool
	home     string // the home the run works in: the real one; in a sandbox the folder made for it, or under bwrap the real path bound to it
	tmpHome  string
	wrapped  bool // the programs of a sandbox run go through bubblewrap
	// logPrefix opens the log of every step of a sandbox run: which kind of sandbox it was.
	logPrefix string

	state  hubproto.EnvSetupState // the ledger, from the file; saved by apply and update
	logs   []*tailBuf
	values map[string]string // secrets revealed so far, by name
	// blocked is the name of the script that stopped the later ones.
	blocked string
	// waits are the secrets of the safe the run needs from the person.
	waits []string
	// repoDirs are the folders of the list's repositories that are in place, by item id.
	repoDirs map[string]string
	// heads are the commits the clones of repositories outside the list stand on after this run, by item id.
	heads map[string]string
	seq   int
	// denied: the agent took the permission back during the run.
	denied bool
}

// execute does a run to its end. path is the request file to remove once the run has begun ("" for a run of our own).
func (r *Runner) execute(ctx context.Context, req hubproto.EnvRunRequest, hash string, items []hubproto.EnvItem, path string) {
	// One run at a time in the whole runtime (all the link folders): setup is heavy and the person's chats come first.
	select {
	case runSlot <- struct{}{}:
		defer func() { <-runSlot }()
	case <-ctx.Done():
		return
	}
	ex := &execution{r: r, ctx: ctx, items: items, modifies: req.Mode != hubproto.RunCheck, sandbox: req.Mode == hubproto.RunSandbox,
		home: r.Home, state: r.readState(), values: map[string]string{}, repoDirs: map[string]string{}, heads: map[string]string{}}
	ex.run = hubproto.EnvRun{RunID: req.RunID, Mode: req.Mode, Hash: hash, By: req.By, Status: hubproto.EnvRunning, StartedAt: r.now().UnixMilli()}
	for _, it := range items {
		var src source
		var err error
		if len(bytes.TrimSpace(it.Source)) > 0 {
			err = json.Unmarshal(it.Source, &src) // a source that is not an object is an error of its step, said there
		}
		ex.srcErrs = append(ex.srcErrs, err)
		ex.srcs = append(ex.srcs, src)
		ex.run.Steps = append(ex.run.Steps, hubproto.EnvStep{ItemID: it.ItemID, Kind: it.Kind, Name: it.Name, Status: hubproto.EnvPending})
		ex.logs = append(ex.logs, &tailBuf{})
	}
	ex.save()
	if path != "" {
		_ = os.Remove(path) // the run is on disk now; the request is not needed
	}
	if ex.sandbox {
		if !ex.prepareSandbox() {
			ex.finish()
			return
		}
		defer ex.removeSandbox()
	}
	ex.stepsAll()
	ex.finish()
	if ex.modifies && !ex.sandbox && ctx.Err() == nil {
		ex.saveState()
		ex.pruneApps()
	}
	if req.Mode != hubproto.RunCheck && !ex.sandbox {
		r.waited, r.waitedHash = ex.waits, hash
	}
}

func (ex *execution) stepsAll() {
	// Access and repositories, then the scripts in order.
	scriptFrom := len(ex.items)
	for i, it := range ex.items {
		if it.Kind == hubproto.EnvKindScript {
			scriptFrom = i
			break
		}
	}
	for i := 0; i < scriptFrom && ex.ctx.Err() == nil; i++ {
		ex.step(i)
	}
	for i := scriptFrom; i < len(ex.items) && ex.ctx.Err() == nil; i++ {
		ex.step(i)
	}
	// What failed before the scripts may be mended by them (a script installs the CLI): once more.
	if ex.modifies {
		for i := 0; i < scriptFrom && ex.ctx.Err() == nil; i++ {
			if ex.run.Steps[i].Status == hubproto.EnvError {
				ex.logs[i].WriteString("\n— ещё раз после скриптов —\n")
				ex.step(i)
			}
		}
	}
}

func (ex *execution) step(i int) {
	st := &ex.run.Steps[i]
	// The agent's word is looked at again before every step: a policy that turned the setup off in the middle of a run
	// stops the steps that have not started.
	if !ex.r.allowed() {
		ex.denied = true
	}
	if ex.denied {
		st.Status, st.Message = hubproto.EnvPending, "настройка машины выключена на этой машине (политика setup)"
		ex.save()
		return
	}
	st.Status, st.Message, st.Exit = hubproto.EnvRunning, "", nil
	ex.save()
	start := ex.r.now()
	switch {
	case ex.srcErrs[i] != nil:
		ex.fail(i, "источник не разобран: "+ex.srcErrs[i].Error())
	case ex.items[i].Kind == hubproto.EnvKindAccess:
		ex.access(i)
	case ex.items[i].Kind == hubproto.EnvKindRepo:
		ex.repo(i)
	case ex.items[i].Kind == hubproto.EnvKindScript:
		ex.script(i)
	case ex.items[i].Kind == hubproto.EnvKindPlugin:
		ex.plugin(i)
	case ex.items[i].Kind == hubproto.EnvKindClaudeSettings:
		ex.claudeSettings(i)
	case ex.items[i].Kind == hubproto.EnvKindApp:
		ex.app(i)
	}
	st.Message = ex.mask(st.Message)
	st.Ms = ex.r.now().Sub(start).Milliseconds()
	st.Log = ex.logPrefix + ex.logs[i].String()
	ex.save()
}

func (ex *execution) fail(i int, msg string) {
	ex.run.Steps[i].Status, ex.run.Steps[i].Message = hubproto.EnvError, msg
}
func (ex *execution) pend(i int, msg string) {
	ex.run.Steps[i].Status, ex.run.Steps[i].Message = hubproto.EnvPending, msg
}
func (ex *execution) done(i int, status, msg string) {
	ex.run.Steps[i].Status, ex.run.Steps[i].Message = status, msg
}

// finish gives the run its status: an error anywhere is an error; else anything not done (waiting for a token, for a
// script to be mended) is pending.
func (ex *execution) finish() {
	status := hubproto.EnvOK
	for _, s := range ex.run.Steps {
		switch s.Status {
		case hubproto.EnvError:
			status = hubproto.EnvError
		case hubproto.EnvPending, hubproto.EnvRunning:
			if status != hubproto.EnvError {
				status = hubproto.EnvPending
			}
		}
	}
	if ex.ctx.Err() != nil {
		status = hubproto.EnvError
		for i := range ex.run.Steps {
			if ex.run.Steps[i].Status == hubproto.EnvRunning {
				ex.fail(i, "остановлено")
			}
		}
	}
	ex.run.Status, ex.run.FinishedAt = status, ex.r.now().UnixMilli()
	ex.save()
}

func (ex *execution) save() {
	if err := ex.r.writeRun(ex.run); err != nil {
		ex.r.logf("warn", "envsetup: run not written", "run", ex.run.RunID, "error", err.Error())
	}
}

// saveState writes what apply and update leave: the hash worked on, each item's status and the ledger.
func (ex *execution) saveState() {
	st := ex.state
	st.Hash, st.At = ex.run.Hash, ex.r.now().UnixMilli()
	st.Items = []hubproto.EnvApplied{}
	for _, s := range ex.run.Steps {
		st.Items = append(st.Items, hubproto.EnvApplied{ItemID: s.ItemID, Status: shown(s.Status), Message: s.Message})
	}
	ex.r.keepLedger(&st, ex.items)
	if ex.modifies && !ex.sandbox {
		st.Moved = false // the run for the moved branch is done
	}
	if len(ex.heads) > 0 && !ex.sandbox {
		if st.Heads == nil {
			st.Heads = map[string]string{}
		}
		for id, h := range ex.heads {
			st.Heads[id] = h
		}
	}
	if err := ex.r.writeState(st); err != nil {
		ex.r.logf("warn", "envsetup: state not written", "error", err.Error())
	}
}

// remember puts a script in the ledger at once, so a crash later in the run does not make it run again.
func (ex *execution) remember(id, hash string) {
	if ex.sandbox || !ex.modifies {
		return
	}
	ex.state.Once[id] = hash
	st := ex.r.readState()
	st.Once[id] = hash
	if err := ex.r.writeState(st); err != nil {
		ex.r.logf("warn", "envsetup: ledger not written", "error", err.Error())
	}
}

// --- running programs ---

// tailBuf keeps the end of a step's output, at most hubproto.EnvLogMax bytes.
type tailBuf struct {
	b         []byte
	truncated bool
	reserve   int // bytes of the limit the log's prefix takes
}

func (t *tailBuf) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > 2*hubproto.EnvLogMax {
		t.b = append([]byte(nil), t.b[len(t.b)-hubproto.EnvLogMax:]...)
		t.truncated = true
	}
	return len(p), nil
}

func (t *tailBuf) WriteString(s string) { _, _ = t.Write([]byte(s)) }

func (t *tailBuf) String() string {
	const mark = "…\n"
	limit := hubproto.EnvLogMax - t.reserve
	b := t.b
	if len(b) <= limit && !t.truncated {
		return string(b)
	}
	if len(b) > limit-len(mark) {
		b = b[len(b)-(limit-len(mark)):]
	}
	for len(b) > 0 && !utf8.RuneStart(b[0]) {
		b = b[1:]
	}
	return mark + string(b)
}

func (ex *execution) secretValues() []string {
	out := make([]string, 0, len(ex.values))
	for _, v := range ex.values {
		out = append(out, v)
	}
	return out
}

// runLogged runs c with its output in step i's log (secrets masked) and says how it ended.
func (ex *execution) runLogged(i int, c Cmd, shownCmd string) (int, error) {
	mk := secretmask.New(ex.logs[i], ex.secretValues())
	_, _ = mk.Write([]byte("$ " + shownCmd + "\n"))
	c.Out = mk
	exit, err := ex.exec(c)
	mk.Flush()
	switch {
	case errors.Is(err, ErrTimeout):
		fmt.Fprintf(ex.logs[i], "\n(не закончилось за %s, остановлено)\n", c.Timeout)
	case err != nil && !errors.Is(err, exec.ErrNotFound):
		fmt.Fprintf(ex.logs[i], "\n(%v)\n", err)
	}
	return exit, err
}

// quiet runs c and gives its output; nothing of it goes to a log.
func (ex *execution) quiet(c Cmd) (string, int, error) {
	var b bytes.Buffer
	c.Out = &b
	exit, err := ex.exec(c)
	return b.String(), exit, err
}

func (ex *execution) exec(c Cmd) (int, error) {
	if ex.wrapped {
		c = ex.wrap(c)
	}
	return ex.r.exec(ex.ctx, c)
}

// baseEnv is what every program gets: little, and nothing of the runtime's own secrets.
func (ex *execution) baseEnv() []string {
	g := ex.r.getenv
	lang := g("LANG")
	if lang == "" {
		lang = "C.UTF-8"
	}
	shell := g("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	user := g("USER")
	if user == "" {
		user = filepath.Base(ex.r.Home)
	}
	path := g("PATH")
	if path == "" {
		path = "/usr/local/bin:/usr/bin:/bin"
	}
	env := []string{"PATH=" + path, "HOME=" + ex.home, "USER=" + user, "LANG=" + lang, "SHELL=" + shell,
		"GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=https:ssh", "NO_COLOR=1"}
	if ex.sandbox {
		// A fresh machine: every place a tool looks for its settings is inside the sandbox's home.
		env = append(env, "XDG_CONFIG_HOME="+ex.home+"/.config", "XDG_DATA_HOME="+ex.home+"/.local/share",
			"XDG_CACHE_HOME="+ex.home+"/.cache", "XDG_STATE_HOME="+ex.home+"/.local/state", "GIT_CONFIG_GLOBAL="+ex.home+"/.gitconfig")
	}
	return env
}

func (ex *execution) envWith(names []string) []string {
	env := ex.baseEnv()
	for _, n := range names {
		if v, ok := ex.values[n]; ok {
			env = append(env, hubproto.SecretVar(n)+"="+v)
		}
	}
	return env
}

// reveal fetches the values of names that are not fetched yet.
func (ex *execution) reveal(names []string) error {
	var need []string
	for _, n := range names {
		if _, ok := ex.values[n]; !ok {
			need = append(need, n)
		}
	}
	if len(need) == 0 {
		return nil
	}
	vals, err := ex.r.Secrets.Reveal(need)
	if err != nil {
		return err
	}
	for _, n := range need {
		ex.values[n] = vals[n]
	}
	return nil
}

// needSecret asks the person for a secret that is not in the safe.
func (ex *execution) needSecret(name, why string) {
	if !contains(ex.waits, name) {
		ex.waits = append(ex.waits, name)
	}
	if err := ex.r.Secrets.Ask(name, why); err != nil {
		ex.r.logf("warn", "envsetup: ask not opened", "name", name, "error", err.Error())
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// hostPath is where a path of the run's home is on this machine: the same, except in a sandbox, where the home is bound
// to a folder of the sandbox.
func (ex *execution) hostPath(p string) string {
	if !ex.wrapped {
		return p
	}
	if rel, err := filepath.Rel(ex.r.Home, p); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.Join(ex.tmpHome, rel)
	}
	return p
}

func hashOf(raw json.RawMessage) string {
	sum := sha256.Sum256(hubproto.CanonicalJSON(raw))
	return hex.EncodeToString(sum[:])
}

var plainName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)

// mask replaces the values of the secrets this run has revealed in s.
func (ex *execution) mask(s string) string {
	if s == "" || len(ex.values) == 0 {
		return s
	}
	var b bytes.Buffer
	mk := secretmask.New(&b, ex.secretValues())
	_, _ = mk.Write([]byte(s))
	mk.Flush()
	return b.String()
}

// lowDisk says why there is too little room on the disk of path for a clone or a sandbox, "" when there is enough.
func (ex *execution) lowDisk(path string) string {
	free := freeBytes
	if ex.r.Free != nil {
		free = ex.r.Free
	}
	n, err := free(path)
	if err != nil {
		return "" // not knowing is not a reason to refuse
	}
	if n < minFree {
		return fmt.Sprintf("мало места на диске: %.1f ГБ", float64(n)/(1<<30))
	}
	return ""
}
