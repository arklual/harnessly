// Package cliupdate keeps the agent CLIs of the machine (Claude Code, Codex) up to date in the background.
// OpenAI's backend gives Codex the model list of its client version, so a stale CLI hides new models everywhere; a copy
// left early on PATH (an old `npm i -g` into ~/.local) shadows a newer one the machine has.
//
// A run, for each CLI: it finds every copy along the login shell's PATH, points the first one at the newest when a
// newer copy stands later (only a link of the person's own folder is changed), asks npm for the latest version, and
// when the person's own copy is older it installs the new one beside the old (Codex into
// ~/.local/share/harnessly/agent-cli/codex/<version>, Claude Code through its own `claude update`, which does the
// same) and switches the link. Running chats keep the files they started with. A copy the machine's admin keeps (a
// folder the account cannot write) is left to the admin and reported.
package cliupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/preflight"
	"github.com/ly-solutions/harnessly/internal/updatecheck"
)

// CLI is an agent CLI the updater keeps: its command and its npm package (where its latest version is read).
type CLI struct {
	Name    string
	Package string
	Label   string
}

// CLIs the updater keeps.
var CLIs = []CLI{
	{Name: "claude", Package: "@anthropic-ai/claude-code", Label: "Claude Code"},
	{Name: "codex", Package: "@openai/codex", Label: "Codex"},
}

// Timing of the runs.
const (
	// Every is how often a run starts by itself.
	Every = 6 * time.Hour
	// PokeAfter is how old the last run must be for a page open to start another.
	PokeAfter = 30 * time.Minute
	// FirstAfter is how long after the start the first run waits, so it does not slow the start.
	FirstAfter = 2 * time.Minute
	// keepVersions is how many side-by-side installs of Codex stay (the current one and the one before).
	keepVersions = 2
	listTimeout  = time.Minute
	viewTimeout  = 45 * time.Second
	setupTimeout = 10 * time.Minute
)

// Copy is one copy of a CLI along PATH: the PATH entry, where it really is, the version it prints.
type Copy struct {
	Path    string `json:"path"`
	Real    string `json:"real"`
	Version string `json:"version"`
}

// States of a CLI (Status.State).
const (
	StateOK       = "ok"       // the newest known version runs
	StateUpdating = "updating" // a new version is being installed
	StateUpdated  = "updated"  // the last run installed a new version or pointed PATH at the newer copy
	StateManaged  = "managed"  // a newer version exists, the copy is the admin's
	StateShadowed = "shadowed" // an older copy shadows a newer one and its entry cannot be changed here
	StateFailed   = "failed"   // the last update failed (Detail says why); the old version keeps running
	StateMissing  = "missing"  // not installed
)

// Status of a CLI as the machine checks show it.
type Status struct {
	CLI     string `json:"cli"`
	Label   string `json:"label"`
	Path    string `json:"path,omitempty"`
	Real    string `json:"real,omitempty"`
	Version string `json:"version,omitempty"`
	Latest  string `json:"latest,omitempty"`
	State   string `json:"state"`
	Detail  string `json:"detail,omitempty"`
	// From is the version before the last update, To the one after it, At when it happened.
	From      string    `json:"from,omitempty"`
	To        string    `json:"to,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitzero"`
	CheckedAt time.Time `json:"checkedAt,omitzero"`
	// Fixable: a run now would change something (an update of the person's copy, a link to repoint).
	Fixable bool `json:"fixable,omitempty"`
}

// Deps are what the updater needs of the runtime.
type Deps struct {
	// Shell runs scripts in the person's login shell (preflight.Checker).
	Shell agents.LoginRunner
	// EnvDir holds the env files (the proxy) the CLIs' scripts source first.
	EnvDir string
	Home   string
	// Changed is told after a CLI changed (a new version, a repointed link): the runtime drops what it cached of the
	// CLIs and refreshes the model lists.
	Changed func(cli string)
	// Log hears what the updater did.
	Log func(format string, a ...any)
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Updater runs the checks, one at a time.
type Updater struct {
	d Deps

	mu      sync.Mutex
	status  map[string]Status
	running bool
	last    time.Time
	done    chan struct{}
}

// New is an updater; nothing runs until Loop, Poke or Run.
func New(d Deps) *Updater {
	return &Updater{d: d, status: map[string]Status{}}
}

func (u *Updater) now() time.Time {
	if u.d.Now != nil {
		return u.d.Now()
	}
	return time.Now()
}

func (u *Updater) log(format string, a ...any) {
	if u.d.Log != nil {
		u.d.Log(format, a...)
	}
}

// Loop runs a check FirstAfter the start and then Every, until ctx ends.
func (u *Updater) Loop(ctx context.Context) {
	t := time.NewTimer(FirstAfter)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		u.Run(ctx)
		t.Reset(Every)
	}
}

// Poke starts a run in the background when the last one is older than PokeAfter (a page was opened); it never waits.
func (u *Updater) Poke(ctx context.Context) bool {
	u.mu.Lock()
	due := !u.running && u.now().Sub(u.last) >= PokeAfter
	u.mu.Unlock()
	if due {
		go u.Run(context.WithoutCancel(ctx))
	}
	return due
}

// Status is what the last run saw, in the order of CLIs; a CLI never looked at is left out.
func (u *Updater) Status() []Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	var out []Status
	for _, c := range CLIs {
		if s, ok := u.status[c.Name]; ok {
			out = append(out, s)
		}
	}
	return out
}

func (u *Updater) set(s Status) {
	u.mu.Lock()
	if old, ok := u.status[s.CLI]; ok && s.UpdatedAt.IsZero() {
		s.From, s.To, s.UpdatedAt = old.From, old.To, old.UpdatedAt
	}
	u.status[s.CLI] = s
	u.mu.Unlock()
}

// Run checks and updates every CLI now; a run already going is waited for instead of starting another.
func (u *Updater) Run(ctx context.Context) {
	u.mu.Lock()
	if u.running {
		done := u.done
		u.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
		}
		return
	}
	u.running, u.done = true, make(chan struct{})
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		u.running, u.last = false, u.now()
		close(u.done)
		u.mu.Unlock()
	}()
	for _, c := range CLIs {
		if ctx.Err() != nil {
			return
		}
		u.runOne(ctx, c)
	}
}

func (u *Updater) runOne(ctx context.Context, c CLI) {
	st := Status{CLI: c.Name, Label: c.Label, CheckedAt: u.now()}
	copies, err := u.copies(ctx, c.Name)
	if err != nil {
		st.State, st.Detail = StateFailed, err.Error()
		u.set(st)
		return
	}
	if len(copies) == 0 {
		st.State = StateMissing
		u.set(st)
		return
	}
	active := copies[0]
	changed := false
	if newer, ok := Shadowing(copies); ok {
		if err := relink(active.Path, newer.Path); err != nil {
			st.State = StateShadowed
			st.Detail = fmt.Sprintf("%s %s перекрывает более новую %s %s: %v", active.Path, active.Version, newer.Path, newer.Version, err)
			st.Path, st.Real, st.Version = active.Path, active.Real, active.Version
			u.set(st)
			return
		}
		u.log("cliupdate: %s %s pointed at %s %s", active.Path, active.Version, newer.Path, newer.Version)
		st.From, st.To, st.UpdatedAt = active.Version, newer.Version, u.now()
		active = Copy{Path: active.Path, Real: newer.Real, Version: newer.Version}
		changed = true
	}
	st.Path, st.Real, st.Version, st.State = active.Path, active.Real, active.Version, StateOK
	if changed {
		st.State = StateUpdated
	}
	latest, err := u.latest(ctx, c)
	if err != nil {
		u.log("cliupdate: latest %s: %v", c.Package, err)
	}
	st.Latest = latest
	if latest != "" && Newer(latest, active.Version) {
		if !u.own(active) {
			st.State = StateManaged
			st.Detail = "есть " + latest + "; эту копию (" + active.Real + ") обновляет администратор машины"
		} else {
			st.State = StateUpdating
			u.set(st)
			ver, err := u.install(ctx, c, active, latest)
			if err != nil {
				st.State, st.Detail = StateFailed, err.Error()
				u.log("cliupdate: %s %s → %s: %v", c.Name, active.Version, latest, err)
			} else {
				u.log("cliupdate: %s %s → %s", c.Name, active.Version, ver)
				st.From, st.To, st.UpdatedAt = active.Version, ver, u.now()
				st.Version, st.State, st.Detail = ver, StateUpdated, ""
				if now, err := u.copies(ctx, c.Name); err == nil && len(now) > 0 {
					st.Real = now[0].Real
				}
				changed = true
			}
		}
	}
	u.set(st)
	if changed && u.d.Changed != nil {
		u.d.Changed(c.Name)
	}
}

// Newer: version a is newer than b. A version that does not parse is never newer, and anything is newer than none.
func Newer(a, b string) bool {
	va, ok := updatecheck.Parse(a)
	if !ok {
		return false
	}
	vb, ok := updatecheck.Parse(b)
	if !ok {
		return true
	}
	return updatecheck.Compare(va, vb) > 0
}

// Newest is the copy with the highest version, the earliest on PATH among equals; false with no copy that has one.
func Newest(copies []Copy) (Copy, bool) {
	best, found := Copy{}, false
	for _, c := range copies {
		if _, ok := updatecheck.Parse(c.Version); !ok {
			continue
		}
		if !found || Newer(c.Version, best.Version) {
			best, found = c, true
		}
	}
	return best, found
}

// Shadowing is the newer copy that the first copy on PATH shadows; false when the first one is the newest.
func Shadowing(copies []Copy) (Copy, bool) {
	if len(copies) < 2 {
		return Copy{}, false
	}
	newest, ok := Newest(copies)
	if !ok || newest.Real == copies[0].Real || !Newer(newest.Version, copies[0].Version) {
		return Copy{}, false
	}
	return newest, true
}

// ParseCopies reads the lines of the listing script: PATH entry, real path and the first line of `--version`, by tabs.
// A real path seen before is the same copy and is left out.
func ParseCopies(out string) []Copy {
	var copies []Copy
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(strings.TrimRight(line, "\r"), "\t", 3)
		if len(f) < 2 || !filepath.IsAbs(f[0]) {
			continue
		}
		c := Copy{Path: f[0], Real: f[1]}
		if c.Real == "" {
			c.Real = c.Path
		}
		if len(f) == 3 {
			if v := preflight.Version(f[2]); v != nil {
				c.Version = *v
			}
		}
		if slices.ContainsFunc(copies, func(x Copy) bool { return x.Real == c.Real }) {
			continue
		}
		copies = append(copies, c)
	}
	return copies
}

func (u *Updater) prefix(name string) string { return agents.EnvSourcePrefix(u.d.EnvDir, name) }

func (u *Updater) login(ctx context.Context, timeout time.Duration, script string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := u.d.Shell.RunLogin(ctx, script)
	if err != nil {
		return "", err
	}
	if res.Exit != 0 {
		return res.Stdout, fmt.Errorf("exit %d: %s", res.Exit, lastLines(res.Stdout))
	}
	return res.Stdout, nil
}

// copies lists the CLI's copies along the login shell's PATH, the first being the one a chat starts.
func (u *Updater) copies(ctx context.Context, name string) ([]Copy, error) {
	out, err := u.login(ctx, listTimeout, u.prefix(name)+listScript(name))
	if err != nil {
		return nil, err
	}
	return ParseCopies(out), nil
}

// listScript lists the copies on PATH, then the machine's own installs of /opt/*/bin that are not on it (the copy a
// machine timer keeps fresh): a copy of the person's that shadows a newer one
// there is pointed at that folder's stable link, and follows it from then on.
func listScript(name string) string {
	return `IFS=:
seen=0
one() {
  f="$1/` + name + `"
  [ -x "$f" ] && [ ! -d "$f" ] || return 0
  r=$(readlink -f "$f" 2>/dev/null || printf '%s' "$f")
  v=$("$f" --version 2>/dev/null </dev/null | head -n 1)
  printf '%s\t%s\t%s\n' "$f" "$r" "$v"
  seen=1
}
for d in $PATH; do one "$d"; done
[ "$seen" = 1 ] || exit 0
unset IFS
for d in /opt/*/bin; do
  case ":$PATH:" in *":$d:"*) continue ;; esac
  one "$d"
done
exit 0`
}

// latest is the npm registry's latest version of the CLI's package; "" when npm is not there.
func (u *Updater) latest(ctx context.Context, c CLI) (string, error) {
	out, err := u.login(ctx, viewTimeout, u.prefix(c.Name)+"command -v npm >/dev/null 2>&1 || exit 0\nnpm view "+
		agents.ShellQuote(c.Package)+" version --silent --loglevel=error 2>&1")
	if err != nil {
		return "", err
	}
	if v := preflight.Version(out); v != nil {
		return *v, nil
	}
	return "", nil
}

// own: the copy is the person's to change, under their home and in a folder they can write.
func (u *Updater) own(c Copy) bool {
	if u.d.Home == "" {
		return false
	}
	home := filepath.Clean(u.d.Home) + string(filepath.Separator)
	if !strings.HasPrefix(c.Real, home) || !strings.HasPrefix(c.Path, home) {
		return false
	}
	return writable(filepath.Dir(c.Real)) && writable(filepath.Dir(c.Path))
}

// Dir is where Codex's side-by-side installs go.
func Dir(home string) string { return filepath.Join(home, ".local", "share", "harnessly", "agent-cli") }

// install puts the version beside the running one and points the PATH entry at it; the version that runs after it.
func (u *Updater) install(ctx context.Context, c CLI, active Copy, version string) (string, error) {
	switch c.Name {
	case "codex":
		root := filepath.Join(Dir(u.d.Home), c.Name)
		dir := filepath.Join(root, version)
		bin := filepath.Join(dir, "node_modules", ".bin", c.Name)
		script := `set -e
command -v npm >/dev/null 2>&1 || { echo "нужен npm"; exit 127; }
root=` + agents.ShellQuote(root) + `; v=` + agents.ShellQuote(version) + `
mkdir -p "$root"; rm -rf "$root/$v.tmp"
npm install --prefix "$root/$v.tmp" --no-save --no-fund --no-audit --loglevel=error ` + agents.ShellQuote(c.Package+"@"+version) + ` 2>&1
"$root/$v.tmp/node_modules/.bin/codex" --version 2>&1 | head -n 1
rm -rf "$root/$v"; mv "$root/$v.tmp" "$root/$v"`
		out, err := u.login(ctx, setupTimeout, u.prefix(c.Name)+script)
		if err != nil {
			_ = os.RemoveAll(dir + ".tmp")
			return "", errors.New("npm install: " + err.Error())
		}
		got := ""
		if v := preflight.Version(out); v != nil {
			got = *v
		}
		if got != version {
			return "", fmt.Errorf("установленный Codex напечатал версию %q вместо %s", got, version)
		}
		if err := relink(active.Path, bin); err != nil {
			return "", err
		}
		prune(root, version, active.Real)
		return got, nil
	case "claude":
		// Claude Code's own updater installs beside the running version and switches its link.
		if _, err := u.login(ctx, setupTimeout, u.prefix(c.Name)+agents.ShellQuote(active.Path)+" update 2>&1 </dev/null"); err != nil {
			return "", errors.New("claude update: " + err.Error())
		}
		now, err := u.copies(ctx, c.Name)
		if err != nil || len(now) == 0 {
			return "", errors.New("claude не найден после обновления")
		}
		if !Newer(now[0].Version, active.Version) {
			return "", fmt.Errorf("claude update оставил версию %s", now[0].Version)
		}
		return now[0].Version, nil
	}
	return "", fmt.Errorf("no updater for %s", c.Name)
}

// relink points the link at path to target, atomically: a chat that starts meanwhile gets one or the other. Only a
// symbolic link is replaced, never a program.
func relink(path, target string) error {
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if st.Mode()&os.ModeSymlink == 0 {
		return errors.New(path + " не ссылка: замените её ссылкой на новую версию")
	}
	tmp := path + ".hl-new"
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// prune removes the side-by-side installs older than the keepVersions newest, never the current or the one that ran.
func prune(root, current, previous string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	var versions []string
	for _, e := range entries {
		if _, ok := updatecheck.Parse(e.Name()); ok && e.IsDir() {
			versions = append(versions, e.Name())
		}
	}
	slices.SortFunc(versions, func(a, b string) int {
		va, _ := updatecheck.Parse(a)
		vb, _ := updatecheck.Parse(b)
		return updatecheck.Compare(vb, va)
	})
	for i, v := range versions {
		dir := filepath.Join(root, v)
		if i < keepVersions || v == current || strings.HasPrefix(previous, dir+string(filepath.Separator)) {
			continue
		}
		_ = os.RemoveAll(dir)
	}
}

func lastLines(out string) string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	s := strings.Join(lines, " / ")
	if r := []rune(s); len(r) > 300 {
		s = "…" + string(r[len(r)-300:])
	}
	return s
}
