package evalrun

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ErrBusy is what Start returns while another run is going: a machine runs one at a time.
var ErrBusy = errors.New("evalrun: another run is going on this machine")

const (
	checkTimeout = 10 * time.Minute
	maxWallS     = 4 * 3600
	timeFormat   = "2006-01-02T15:04:05.000Z"
)

var (
	baseRe  = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
	repoRe  = regexp.MustCompile(`^[\w.-]+$`)
	modelRe = regexp.MustCompile(`^[\w.:\[\]-]+$`)
	nameRe  = regexp.MustCompile(`[^\w.-]`)
)

// Runner runs the hub's golden tasks on this machine, one at a time.
type Runner struct {
	// Home is the person's home: their logins and harness are read from here, and hidden from the sandbox.
	Home string
	// Dir is where runs live: <Dir>/runs/<id>, and while a run goes <Dir>/work/<id> and <Dir>/home/<id>.
	Dir string
	// Workspace holds the repositories the tasks are cut from: <Workspace>/<repo>.
	Workspace string
	// BwrapPath is the bubblewrap binary; empty means the machine cannot sandbox and runs are skipped.
	BwrapPath string
	// ToolDirs are extra directories of tools (node, pnpm, go ...) that are put on the sandbox's PATH and bound read-only.
	ToolDirs []string

	mu     sync.Mutex
	active map[string]*activeRun
}

type activeRun struct {
	cancel  context.CancelFunc
	stopped atomic.Bool
}

// New is a Runner for the person who owns the home. Dir is usually <data dir>/eval.
func New(home, dir, workspace string, toolDirs []string) *Runner {
	r := &Runner{Home: home, Dir: dir, Workspace: workspace, ToolDirs: toolDirs, active: map[string]*activeRun{}}
	if runtime.GOOS == "linux" {
		if p, err := exec.LookPath("bwrap"); err == nil {
			r.BwrapPath = p
		}
	}
	return r
}

func (r *Runner) runDir(id string) string  { return filepath.Join(r.Dir, "runs", id) }
func (r *Runner) workDir(id string) string { return filepath.Join(r.Dir, "work", id) }
func (r *Runner) homeDir(id string) string { return filepath.Join(r.Dir, "home", id) }

// validate says what is wrong with a request, or "" when the runner can take it.
func validate(q *Request) string {
	switch {
	case !RunID.MatchString(q.RunID):
		return "bad run id"
	case !baseRe.MatchString(q.Task.Base):
		return "bad base commit"
	case !repoRe.MatchString(q.Task.Repo) || q.Task.Repo == "." || q.Task.Repo == "..":
		return "bad repository name"
	case q.Task.Prompt == "":
		return "empty prompt"
	case !modelRe.MatchString(q.Arm.Model):
		return "bad model"
	case q.Arm.Agent != "" && q.Arm.Agent != "claude" && q.Arm.Agent != "codex":
		return "unknown agent"
	}
	if q.Task.Limits.WallS < 0 || q.Task.Limits.WallS > maxWallS || q.Task.Limits.USD < 0 {
		return "limits out of range"
	}
	return ""
}

// Start takes a run and goes on with it in the background. Asking again for a run the machine already has is not an error,
// so a hub that did not hear the answer can ask again.
func (r *Runner) Start(q Request) error {
	if why := validate(&q); why != "" {
		return errors.New("evalrun: " + why)
	}
	if q.Task.Limits.WallS == 0 {
		q.Task.Limits.WallS = 1800
	}
	if q.Task.Limits.USD == 0 {
		q.Task.Limits.USD = 5
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.active[q.RunID]; ok {
		return nil
	}
	if _, err := os.Stat(filepath.Join(r.runDir(q.RunID), "state.json")); err == nil {
		return nil
	}
	if len(r.active) > 0 {
		return ErrBusy
	}
	if err := os.MkdirAll(r.runDir(q.RunID), 0o700); err != nil {
		return err
	}
	j := &job{r: r, q: q, started: time.Now(), ar: &activeRun{}}
	j.res = State{RunID: q.RunID, State: "preparing", StartedAt: j.started.UTC().Format(timeFormat), Checks: []CheckResult{}, Timings: map[string]int{}}
	if b, err := json.Marshal(q); err == nil {
		_ = os.WriteFile(filepath.Join(r.runDir(q.RunID), "request.json"), b, 0o600)
	}
	if err := j.save(""); err != nil {
		return err
	}
	var ctx context.Context
	ctx, j.ar.cancel = context.WithCancel(context.Background())
	r.active[q.RunID] = j.ar
	go func() {
		defer func() {
			if p := recover(); p != nil {
				j.finish(Failed, fmt.Sprint("panic: ", p))
			}
			j.ar.cancel()
			r.mu.Lock()
			delete(r.active, q.RunID)
			r.mu.Unlock()
		}()
		j.execute(ctx)
	}()
	return nil
}

// Poll reports the runs it knows among ids. A run that was going when the agent restarted is over: it is reported failed.
func (r *Runner) Poll(ids []string) map[string]State {
	out := map[string]State{}
	for _, id := range ids {
		if !RunID.MatchString(id) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(r.runDir(id), "state.json"))
		if err != nil {
			continue
		}
		var s State
		if json.Unmarshal(b, &s) != nil {
			continue
		}
		if !s.Terminal() {
			r.mu.Lock()
			_, going := r.active[id]
			r.mu.Unlock()
			if !going {
				s.State, s.Error = Failed, "агент перезапущен во время прогона"
				s.FinishedAt = time.Now().UTC().Format(timeFormat)
				r.writeState(id, s)
				r.cleanup(id, s.State)
			}
		}
		out[id] = s
	}
	return out
}

// Stop ends a run. A run the machine does not have is not an error.
func (r *Runner) Stop(id string) {
	if !RunID.MatchString(id) {
		return
	}
	r.mu.Lock()
	ar := r.active[id]
	r.mu.Unlock()
	if ar != nil {
		ar.stopped.Store(true)
		ar.cancel()
	}
}

func (r *Runner) writeState(id string, s State) {
	b, _ := json.MarshalIndent(s, "", " ")
	tmp := filepath.Join(r.runDir(id), "state.json.tmp")
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, filepath.Join(r.runDir(id), "state.json"))
	}
}

// cleanup removes the clone and the sandbox home of a run and logs the removal.
func (r *Runner) cleanup(id, state string) {
	for _, d := range []string{r.workDir(id), r.homeDir(id)} {
		removeAll(d)
	}
	f, err := os.OpenFile(filepath.Join(r.Dir, "cleanup.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		fmt.Fprintf(f, "%s run %s %s: removed clone and sandbox home\n", time.Now().UTC().Format(time.RFC3339), id, state)
		f.Close()
	}
}

// removeAll deletes a tree, making read-only folders writable first when it has to (a package store leaves some).
func removeAll(dir string) {
	if os.RemoveAll(dir) == nil {
		return
	}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o700)
		}
		return nil
	})
	_ = os.RemoveAll(dir)
}

// job is one run in progress.
type job struct {
	r       *Runner
	q       Request
	ar      *activeRun
	started time.Time
	res     State
	mu      sync.Mutex
}

func (j *job) save(state string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if state != "" {
		j.res.State = state
	}
	j.res.UpdatedAt = time.Now().UTC().Format(timeFormat)
	b, err := json.MarshalIndent(j.res, "", " ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(j.r.runDir(j.q.RunID), "state.json.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(j.r.runDir(j.q.RunID), "state.json"))
}

func (j *job) finish(state, why string) {
	j.mu.Lock()
	j.res.Error = why
	j.res.FinishedAt = time.Now().UTC().Format(timeFormat)
	wall := int(time.Since(j.started).Seconds() + 0.5)
	j.res.WallS = &wall
	j.mu.Unlock()
	// Clean up before the terminal state is visible, so a poll that sees "done" never finds a clone still on disk.
	j.r.cleanup(j.q.RunID, state)
	_ = j.save(state)
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// claudeBinary is the newest Claude Code the person has installed.
func claudeBinary(home string) (bin, version string, err error) {
	dir := filepath.Join(home, ".local", "share", "claude", "versions")
	ents, _ := os.ReadDir(dir)
	var vs []string
	for _, e := range ents {
		if regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(e.Name()) {
			vs = append(vs, e.Name())
		}
	}
	sort.Slice(vs, func(a, b int) bool { return versionLess(vs[a], vs[b]) })
	if len(vs) > 0 {
		v := vs[len(vs)-1]
		return filepath.Join(dir, v), v, nil
	}
	p, lerr := exec.LookPath("claude")
	if lerr != nil {
		return "", "", errors.New("на машине нет установленного Claude Code")
	}
	if rp, e := filepath.EvalSymlinks(p); e == nil {
		p = rp
	}
	res := run(context.Background(), p, []string{"--version"}, execOpts{timeout: 10 * time.Second})
	v := "?"
	if f := strings.Fields(res.stdout); len(f) > 0 {
		v = f[0]
	}
	return p, v, nil
}

func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if x != y {
			return x < y
		}
	}
	return len(pa) < len(pb)
}

// toolPaths are the directories with the person's node and pnpm (volta) and the extra tool dirs the machine named. The
// pnpm package comes first: node's own bin dir carries a corepack `pnpm` that fails when run outside volta's shim.
func (r *Runner) toolPaths() []string {
	var out []string
	nodeDirs := filepath.Join(r.Home, ".volta", "tools", "image", "node")
	var nodeVersion string
	if ents, err := os.ReadDir(nodeDirs); err == nil {
		var vs []string
		for _, e := range ents {
			vs = append(vs, e.Name())
		}
		sort.Slice(vs, func(a, b int) bool { return versionLess(vs[a], vs[b]) })
		if len(vs) > 0 {
			nodeVersion = vs[len(vs)-1]
		}
	}
	cands := []string{filepath.Join(r.Home, ".volta", "tools", "image", "packages", "pnpm", "bin")}
	if nodeVersion != "" {
		cands = append(cands, filepath.Join(nodeDirs, nodeVersion, "bin"))
	}
	cands = append(cands, r.ToolDirs...)
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			out = append(out, c)
		}
	}
	return out
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// safeWrite writes a file into the clone for the graders, refusing to go through a symlink the agent may have left: this
// write runs outside the sandbox.
func safeWrite(root, rel, content string) error {
	clean := filepath.Clean(rel)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("путь %q выходит за репозиторий", rel)
	}
	cur := root
	parts := strings.Split(clean, string(filepath.Separator))
	for i, p := range parts {
		cur = filepath.Join(cur, p)
		st, err := os.Lstat(cur)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			if i < len(parts)-1 {
				if err := os.Mkdir(cur, 0o755); err != nil {
					return err
				}
			}
		case err != nil:
			return err
		case st.Mode()&os.ModeSymlink != 0:
			return fmt.Errorf("на пути %q лежит ссылка", rel)
		}
	}
	return os.WriteFile(cur, []byte(content), 0o644)
}

// fingerprint tells apart the setups of a harness: two runs with the same one are comparable.
func fingerprint(v any) string {
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])[:16]
}

func (j *job) execute(ctx context.Context) {
	q, r := j.q, j.r
	agent := q.Arm.Agent
	if agent == "" {
		agent = "claude"
	}
	runDir, work, sbHome := r.runDir(q.RunID), r.workDir(q.RunID), r.homeDir(q.RunID)
	wsRoot := filepath.Join(work, "ws", "workspace")
	repo := filepath.Join(work, "repo")
	if q.Task.Layout == "workspace" {
		repo = filepath.Join(wsRoot, q.Task.Repo)
	}
	fail := func(format string, a ...any) { j.finish(Failed, fmt.Sprintf(format, a...)) }
	skip := func(why string) { j.finish(Skipped, why) }

	if r.BwrapPath == "" {
		skip("на машине нет песочницы bubblewrap (bwrap) — прогон невозможен")
		return
	}
	if agent == "codex" {
		if _, err := os.Stat(codexPackage(r.Home)); err != nil {
			skip("Codex на машине не установлен или не залогинен — нужен `codex login`")
			return
		}
	}
	// Credentials are checked before the clone and the install, so a run that cannot go does not spend them.
	wallMs := int64(q.Task.Limits.WallS)*1000 + 10*60_000
	if err := os.MkdirAll(sbHome, 0o700); err != nil {
		fail("не удалось создать домашнюю папку песочницы: %v", err)
		return
	}
	if agent == "codex" {
		if !writeCodexCredentials(r.Home, sbHome, q.Arm.Effort) {
			skip("Codex на машине не залогинен — нужен `codex login`")
			return
		}
	} else if ok, why, err := writeCredentials(r.Home, sbHome, time.Now().UnixMilli(), wallMs); err != nil {
		fail("вход по подписке: %v", err)
		return
	} else if !ok {
		skip(why)
		return
	}

	source := filepath.Join(r.Workspace, q.Task.Repo)
	if !exists(filepath.Join(source, ".git")) {
		fail("нет репозитория %s в рабочем пространстве", q.Task.Repo)
		return
	}
	if res := run(ctx, "git", []string{"-C", source, "cat-file", "-e", q.Task.Base + "^{commit}"}, execOpts{timeout: 30 * time.Second}); res.code != 0 {
		noProxy := append(os.Environ(), "HTTP_PROXY=", "HTTPS_PROXY=", "http_proxy=", "https_proxy=")
		run(ctx, "git", []string{"-C", source, "fetch", "--quiet", "origin"}, execOpts{timeout: 120 * time.Second, env: noProxy})
	}
	if err := os.MkdirAll(repo, 0o700); err != nil {
		fail("не удалось создать папку прогона: %v", err)
		return
	}
	arch := run(ctx, "bash", []string{"-o", "pipefail", "-c", `git -C "$1" archive "$2" | tar -x -C "$3"`, "bash", source, q.Task.Base, repo}, execOpts{timeout: 120 * time.Second})
	if arch.code != 0 {
		fail("не удалось достать базу %s: %s", head(q.Task.Base, 12), head(arch.stderr, 300))
		return
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "eval@example.com"}, {"config", "user.name", "eval"}, {"add", "-A"},
		{"-c", "core.hooksPath=/dev/null", "commit", "-q", "-m", "eval base"}} {
		if step := run(ctx, "git", append([]string{"-C", repo}, args...), execOpts{timeout: 120 * time.Second}); step.code != 0 {
			fail("git %s: %s", args[0], head(step.stderr, 300))
			return
		}
	}
	j.timing("cloneMs", int(time.Since(j.started).Milliseconds()))

	tools := r.toolPaths()
	hostPath := strings.Join(append(append([]string{}, tools...), "/usr/local/bin", "/usr/bin", "/bin"), ":")
	hostEnv := append(os.Environ(), "PATH="+hostPath, "CI=1")
	installMs := 0
	if exists(filepath.Join(repo, "pnpm-lock.yaml")) {
		in := run(ctx, "pnpm", []string{"install", "--frozen-lockfile", "--offline"}, execOpts{dir: repo, env: hostEnv, timeout: 10 * time.Minute})
		if in.code != 0 && ctx.Err() == nil {
			in = run(ctx, "pnpm", []string{"install", "--frozen-lockfile", "--prefer-offline"}, execOpts{dir: repo, env: hostEnv, timeout: 15 * time.Minute})
		}
		if in.code != 0 {
			fail("зависимости не поставились: %s", tail(in.stderr, 400))
			return
		}
		installMs += in.ms
	}
	if exists(filepath.Join(repo, "go.mod")) {
		installMs += run(ctx, "go", []string{"mod", "download"}, execOpts{dir: repo, env: hostEnv, timeout: 10 * time.Minute}).ms
	}
	if installMs > 0 {
		j.timing("installMs", installMs)
	}
	if ctx.Err() != nil {
		j.stopOrFail("")
		return
	}

	// The login again, now that the install is done: it must outlive the session.
	if agent == "claude" {
		if ok, why, err := writeCredentials(r.Home, sbHome, time.Now().UnixMilli(), wallMs); err != nil {
			fail("вход по подписке: %v", err)
			return
		} else if !ok {
			skip(why)
			return
		}
	}
	var claudeBin, claudeVersion string
	if agent == "claude" {
		var err error
		if claudeBin, claudeVersion, err = claudeBinary(r.Home); err != nil {
			fail("%v", err)
			return
		}
	}
	codexVersion := ""
	if agent == "codex" {
		codexVersion = "?"
		if b, err := os.ReadFile(filepath.Join(codexPackage(r.Home), "package.json")); err == nil {
			var p struct {
				Version string `json:"version"`
			}
			if json.Unmarshal(b, &p) == nil && p.Version != "" {
				codexVersion = p.Version
			}
		}
	}
	if q.Arm.Variant == "claudemd-index" {
		skip("вариант claudemd-index в хабе не поддерживается")
		return
	}
	var harness *personalHarness
	if q.Arm.Harness == "personal" && agent == "claude" {
		var err error
		if harness, err = writePersonal(r.Home, sbHome); err != nil {
			fail("гарнитура владельца: %v", err)
			return
		}
	}
	fp := map[string]any{"arm": q.Arm.Key, "harness": q.Arm.Harness, "agent": agent, "variant": q.Arm.Variant, "model": q.Arm.Model, "effort": q.Arm.Effort}
	if agent == "codex" {
		fp["version"] = codexVersion
	} else {
		fp["version"] = claudeVersion
	}
	if harness != nil {
		fp["plugins"], fp["settings"], fp["claudeMd"] = harness.plugins, harness.settings, harness.claudeMd
	}
	j.mu.Lock()
	j.res.Fingerprint = fingerprint(fp)
	j.mu.Unlock()

	danger := q.Task.Profile == "danger"
	perms := map[string]any{"allow": allowTools, "deny": denyTools}
	if danger {
		perms = map[string]any{"deny": dangerDeny}
	}
	profile := filepath.Join(runDir, "eval-profile.json")
	pb, _ := json.Marshal(map[string]any{"permissions": perms, "env": map[string]string{"HARNESSLY_EVAL": "1"}})
	_ = os.WriteFile(profile, pb, 0o600)
	_ = os.WriteFile(filepath.Join(runDir, "gate.py"), []byte(gatePy), 0o600)
	_ = os.WriteFile(filepath.Join(runDir, "mcp-empty.json"), []byte(`{"mcpServers":{}}`), 0o600)

	home := r.Home
	binds := []string{"--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--tmpfs", "/tmp", "--tmpfs", home}
	roDirs := []string{filepath.Join(home, ".volta"), filepath.Join(home, ".local", "share", "pnpm"), filepath.Join(home, "go", "pkg", "mod")}
	roDirs = append(roDirs, r.ToolDirs...)
	for _, d := range roDirs {
		if exists(d) {
			binds = append(binds, "--ro-bind", d, d)
		}
	}
	if harness != nil {
		plugins := filepath.Join(home, ".claude", "plugins")
		if exists(plugins) {
			binds = append(binds, "--ro-bind", plugins, plugins, "--ro-bind", plugins, filepath.Join(sbHome, ".claude", "plugins"))
		}
		for _, sub := range []string{"skills", "agents"} {
			if d := filepath.Join(home, ".claude", sub); exists(d) {
				binds = append(binds, "--ro-bind", d, filepath.Join(sbHome, ".claude", sub))
			}
		}
	}
	if agent == "codex" {
		binds = append(binds, "--ro-bind", codexPackage(home), codexPackage(home))
	}
	binds = append(binds, "--bind", work, work, "--bind", sbHome, sbHome, "--ro-bind", runDir, runDir)
	if claudeBin != "" {
		binds = append(binds, "--ro-bind", claudeBin, "/tmp/eval-bin/claude")
	}
	sandboxPath := strings.Join(append(append([]string{"/tmp/eval-bin"}, tools...), "/usr/local/bin", "/usr/bin", "/bin"), ":")
	sandbox := func(cmd []string, extraEnv ...string) []string {
		a := append([]string{}, binds...)
		a = append(a, "--unshare-pid", "--die-with-parent", "--new-session", "--chdir", repo, "--",
			"env", "-i", "HOME="+sbHome, "PATH="+sandboxPath, "LANG=C.UTF-8", "TERM=xterm-256color", "SHELL=/bin/bash",
			"HARNESSLY_EVAL=1", "DISABLE_AUTOUPDATER=1", "CI=1", "HARNESSLY_WORKSPACE="+wsRoot)
		if d := filepath.Join(home, "go", "pkg", "mod"); exists(d) {
			a = append(a, "GOMODCACHE="+d)
		}
		a = append(a, extraEnv...)
		return append(a, cmd...)
	}
	inSandbox := func(timeout time.Duration, cmd []string, extraEnv ...string) execResult {
		return run(ctx, r.BwrapPath, sandbox(cmd, extraEnv...), execOpts{timeout: timeout})
	}

	if q.Task.Setup != "" {
		s := inSandbox(120*time.Second, []string{"bash", "-o", "pipefail", "-c", q.Task.Setup})
		_ = os.WriteFile(filepath.Join(runDir, "setup.log"), []byte(tail(s.stdout, 10_000)+"\n"+tail(s.stderr, 10_000)), 0o600)
		if ctx.Err() != nil {
			j.stopOrFail("")
			return
		}
		if s.code != 0 {
			fail("подготовка задачи упала: %s", tail(s.stderr, 300))
			return
		}
		for _, args := range [][]string{{"add", "-A"}, {"-c", "core.hooksPath=/dev/null", "commit", "-q", "--allow-empty", "-m", "eval setup"}} {
			if step := run(ctx, "git", append([]string{"-C", repo}, args...), execOpts{timeout: 120 * time.Second}); step.code != 0 {
				fail("git %s после подготовки: %s", args[0], head(step.stderr, 300))
				return
			}
		}
	}

	sessionID := newUUID()
	j.mu.Lock()
	j.res.SessionID = sessionID
	j.mu.Unlock()
	var sessionCmd []string
	if agent == "codex" {
		sessionCmd = []string{"node", filepath.Join(codexPackage(home), "bin", "codex.js"), "exec", "--json", "--skip-git-repo-check",
			"--dangerously-bypass-approvals-and-sandbox", "-C", repo, "-m", q.Arm.Model, q.Task.Prompt}
	} else {
		mode := "acceptEdits"
		if danger {
			mode = "bypassPermissions"
		}
		sessionCmd = []string{"claude", "-p", q.Task.Prompt, "--model", q.Arm.Model, "--output-format", "json", "--session-id", sessionID,
			"--permission-mode", mode, "--settings", profile, "--max-budget-usd", strconv.FormatFloat(q.Task.Limits.USD, 'f', -1, 64)}
		if q.Arm.Variant == "inline" {
			sessionCmd = append(sessionCmd, "--disallowedTools", "Task", "Agent")
		}
		if q.Arm.Effort != nil && *q.Arm.Effort != "" {
			sessionCmd = append(sessionCmd, "--effort", *q.Arm.Effort)
		}
		if harness != nil {
			for _, d := range harness.pluginDirs {
				sessionCmd = append(sessionCmd, "--plugin-dir", d)
			}
		}
		if q.Arm.Harness == "repo" {
			sessionCmd = append(sessionCmd, "--setting-sources", "project,local", "--strict-mcp-config", "--mcp-config", filepath.Join(runDir, "mcp-empty.json"))
		}
	}
	_ = j.save("running")
	session := inSandbox(time.Duration(q.Task.Limits.WallS)*time.Second, sessionCmd)
	j.timing("sessionMs", session.ms)
	_ = os.WriteFile(filepath.Join(runDir, "session-stdout.json"), []byte(head(session.stdout, 200_000)), 0o600)
	if session.stderr != "" {
		_ = os.WriteFile(filepath.Join(runDir, "session-stderr.txt"), []byte(tail(session.stderr, 20_000)), 0o600)
	}
	var summary struct {
		Cost *float64 `json:"total_cost_usd"`
	}
	_ = json.Unmarshal([]byte(session.stdout), &summary)
	use := &Usage{USD: summary.Cost}
	if agent == "codex" {
		for _, line := range strings.Split(session.stdout, "\n") {
			var ev struct {
				Type  string `json:"type"`
				Usage *struct {
					In  int64 `json:"input_tokens"`
					Out int64 `json:"output_tokens"`
				} `json:"usage"`
			}
			if json.Unmarshal([]byte(line), &ev) != nil || ev.Type != "turn.completed" || ev.Usage == nil {
				continue
			}
			use.ModelCalls++
			use.TokensIn += ev.Usage.In
			use.TokensOut += ev.Usage.Out
		}
		_ = os.WriteFile(filepath.Join(runDir, "transcript.jsonl"), []byte(head(session.stdout, 20_000_000)), 0o600)
	}
	if transcript := findTranscript(sbHome, sessionID); transcript != "" {
		if t, err := readTranscript(transcript); err == nil {
			use.ModelCalls, use.TokensIn, use.TokensOut = t.calls, t.tokensIn, t.tokensOut
			fc, pc := t.firstCtx, t.peakCtx
			use.FirstCtx, use.PeakCtx = &fc, &pc
		}
		if b, err := os.ReadFile(transcript); err == nil {
			_ = os.WriteFile(filepath.Join(runDir, "transcript.jsonl"), b, 0o600)
		}
		subDir := filepath.Join(filepath.Dir(transcript), sessionID, "subagents")
		subs, _ := filepath.Glob(filepath.Join(subDir, "*.jsonl"))
		use.Subagents = len(subs)
		for _, f := range subs {
			if t, err := readTranscript(f); err == nil {
				use.TokensIn += t.tokensIn
				use.TokensOut += t.tokensOut
			}
			_ = os.MkdirAll(filepath.Join(runDir, "subagents"), 0o700)
			if b, err := os.ReadFile(f); err == nil {
				_ = os.WriteFile(filepath.Join(runDir, "subagents", filepath.Base(f)), b, 0o600)
			}
		}
	}
	j.mu.Lock()
	j.res.Usage = use
	j.mu.Unlock()
	if j.ar.stopped.Load() {
		j.finish(Stopped, "остановлен вручную")
		return
	}
	if session.timedOut {
		j.finish(Timeout, fmt.Sprintf("сессия не уложилась в %d с", q.Task.Limits.WallS))
		return
	}
	if ctx.Err() != nil {
		j.stopOrFail("")
		return
	}

	_ = j.save("grading")
	// What the agent left is read inside the sandbox too: git runs hooks and helpers named in the repository's own config.
	st := inSandbox(2*time.Minute, []string{"git", "status", "--porcelain", "-z", "--untracked-files=all"})
	changed := parsePorcelain(st.stdout)
	var forbidden []string
	for _, f := range changed {
		for _, g := range q.Task.Forbidden {
			if globToRe(g).MatchString(f) {
				forbidden = append(forbidden, f)
				break
			}
		}
	}
	shortstat := inSandbox(2*time.Minute, []string{"git", "diff", "--shortstat", "HEAD"})
	added, removed := parseShortstat(shortstat.stdout)
	if forbidden == nil {
		forbidden = []string{}
	}
	j.mu.Lock()
	j.res.Diff = &Diff{Files: len(changed), Added: added, Removed: removed, Forbidden: forbidden}
	j.mu.Unlock()
	for file, content := range q.Task.Hidden {
		if err := safeWrite(repo, file, content); err != nil {
			fail("скрытый файл: %v", err)
			return
		}
	}
	for _, c := range q.Task.Checks {
		res := inSandbox(checkTimeout, []string{"bash", "-o", "pipefail", "-c", c.Run},
			"EVAL_TRANSCRIPT="+filepath.Join(runDir, "transcript.jsonl"), "EVAL_GATE="+filepath.Join(runDir, "gate.py"), "EVAL_SOURCE="+repo)
		if ctx.Err() != nil {
			j.stopOrFail("")
			return
		}
		cr := CheckResult{Name: c.Name, Passed: res.code == 0 && !res.timedOut, Ms: res.ms}
		if res.code >= 0 {
			code := res.code
			cr.ExitCode = &code
		}
		j.mu.Lock()
		j.res.Checks = append(j.res.Checks, cr)
		j.mu.Unlock()
		_ = os.WriteFile(filepath.Join(runDir, "check-"+nameRe.ReplaceAllString(c.Name, "_")+".log"), []byte(tail(res.stdout, 20_000)+"\n"+tail(res.stderr, 20_000)), 0o600)
	}
	ok := len(forbidden) == 0 && len(q.Task.Checks) > 0
	for _, c := range j.res.Checks {
		ok = ok && c.Passed
	}
	j.mu.Lock()
	j.res.Passed = &ok
	j.mu.Unlock()
	j.finish(Done, "")
}

// stopOrFail ends a run whose context ended: stopped when it was asked to stop, failed otherwise.
func (j *job) stopOrFail(why string) {
	if j.ar.stopped.Load() {
		j.finish(Stopped, "остановлен вручную")
		return
	}
	if why == "" {
		why = "прогон прерван"
	}
	j.finish(Failed, why)
}

func (j *job) timing(k string, ms int) {
	j.mu.Lock()
	j.res.Timings[k] = ms
	j.mu.Unlock()
}

func findTranscript(sbHome, sessionID string) string {
	m, _ := filepath.Glob(filepath.Join(sbHome, ".claude", "projects", "*", sessionID+".jsonl"))
	if len(m) == 0 {
		return ""
	}
	return m[len(m)-1]
}

// parsePorcelain lists the files of `git status --porcelain -z`; the origin of a rename is not a changed file of its own.
func parsePorcelain(out string) []string {
	var files []string
	parts := strings.Split(out, "\x00")
	for i := 0; i < len(parts); i++ {
		e := parts[i]
		if len(e) < 4 {
			continue
		}
		files = append(files, e[3:])
		if e[0] == 'R' || e[0] == 'C' || e[1] == 'R' || e[1] == 'C' {
			i++
		}
	}
	return files
}

var shortstatRe = regexp.MustCompile(`(\d+) files? changed(?:, (\d+) insertions?\(\+\))?(?:, (\d+) deletions?\(-\))?`)

func parseShortstat(s string) (added, removed int) {
	m := shortstatRe.FindStringSubmatch(s)
	if m == nil {
		return 0, 0
	}
	added, _ = strconv.Atoi(m[2])
	removed, _ = strconv.Atoi(m[3])
	return added, removed
}
