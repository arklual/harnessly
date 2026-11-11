// Package heavy keeps the heavy checks agents run (go build and test, vitest, playwright, tsc, eslint) from taking a
// shared machine away from the people working on it. harnessly puts a small launcher under each of those names on the
// agents' PATH; it runs `harnessly heavy <tool> ARGS…`, and this package runs the real tool:
//
//   - a light call (go env, go list, tsc --version) runs at once, as if there were no launcher;
//   - a heavy call first takes one of the machine's shared slots (a flock on <dir>/slot<n>, for every user of the
//     machine), waits for one up to a limit, and runs under nice and ionice with fewer threads (GOMAXPROCS, -p);
//   - when no slot frees up in time it does not run: it says so in words an agent acts on and exits with 75.
//
// The number of slots follows the cores: 1 up to 4, 2 up to 16, one per 8 above. A call inside a slot already held
// ($HARNESSLY_HEAVY_SLOT, e.g. go test running go build) runs at once. Every heavy call is one line of <dir>/events, which
// `harnessly heavy stats` counts.
package heavy

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ExitBusy is the exit code of a heavy call that found no free slot in time (EX_TEMPFAIL).
const ExitBusy = 75

// Tools are the names harnessly puts a launcher under.
var Tools = []string{"go", "vitest", "playwright", "tsc", "eslint", "npx", "npm", "pnpm"}

// Marker is in every launcher harnessly writes; a file carrying it is never taken for the real tool.
const Marker = "harnessly heavy"

const (
	envSlot  = "HARNESSLY_HEAVY_SLOT"
	envDir   = "HARNESSLY_HEAVY_DIR"
	envSlots = "HARNESSLY_HEAVY_SLOTS"
	envWait  = "HARNESSLY_HEAVY_WAIT"
	envOff   = "HARNESSLY_HEAVY"

	defaultWait = 15 * time.Minute
	maxEvents   = 1 << 20
)

// Env is what a run depends on outside its arguments; tests replace the parts.
type Env struct {
	Getenv  func(string) string
	Environ func() []string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	// CPUs is the number of cores; 0 is runtime.NumCPU.
	CPUs int
	// Poll is how often a waiting call tries the slots again; 0 is 2 s.
	Poll time.Duration
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// Exec replaces the process with the tool for a light call; nil is syscall.Exec.
	Exec func(path string, argv, env []string) error
	// Getwd is the folder the call runs in; nil is os.Getwd.
	Getwd func() (string, error)
}

// OSEnv is the process's own environment.
func OSEnv() Env {
	return Env{Getenv: os.Getenv, Environ: os.Environ, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
}

func (e *Env) fill() {
	if e.Getenv == nil {
		e.Getenv = os.Getenv
	}
	if e.Environ == nil {
		e.Environ = os.Environ
	}
	if e.Stdout == nil {
		e.Stdout = io.Discard
	}
	if e.Stderr == nil {
		e.Stderr = io.Discard
	}
	if e.CPUs <= 0 {
		e.CPUs = runtime.NumCPU()
	}
	if e.Poll <= 0 {
		e.Poll = 2 * time.Second
	}
	if e.Now == nil {
		e.Now = time.Now
	}
	if e.Exec == nil {
		e.Exec = syscall.Exec
	}
	if e.Getwd == nil {
		e.Getwd = os.Getwd
	}
}

// Slots is how many heavy checks a machine with this many cores runs at once.
func Slots(cpus int) int {
	switch {
	case cpus <= 4:
		return 1
	case cpus <= 16:
		return 2
	}
	return cpus / 8
}

// Dir is the folder of the slots: $HARNESSLY_HEAVY_DIR, else /var/lib/harnessly-heavy when the machine has one, else
// /tmp/harnessly-heavy. It is the same for every user of the machine.
func Dir(getenv func(string) string) string {
	if v := strings.TrimSpace(getenv(envDir)); v != "" {
		return v
	}
	if st, err := os.Stat("/var/lib/harnessly-heavy"); err == nil && st.IsDir() {
		return "/var/lib/harnessly-heavy"
	}
	return filepath.Join(os.TempDir(), "harnessly-heavy")
}

// Run is `harnessly heavy [--skip DIR] TOOL ARGS…` and `harnessly heavy stats`; it returns the exit code.
func Run(args []string, env Env) int {
	env.fill()
	var skip []string
	for len(args) > 0 {
		switch {
		case args[0] == "--skip" && len(args) > 1:
			skip = append(skip, args[1])
			args = args[2:]
			continue
		case strings.HasPrefix(args[0], "--skip="):
			skip = append(skip, strings.TrimPrefix(args[0], "--skip="))
			args = args[1:]
			continue
		}
		break
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		fmt.Fprint(env.Stdout, HelpText)
		return 0
	}
	if args[0] == "stats" {
		return statsCmd(args[1:], env)
	}
	tool, targs := args[0], args[1:]
	real, err := lookPath(tool, env.Getenv("PATH"), skip)
	if err != nil {
		fmt.Fprintf(env.Stderr, "harnessly: %s is not installed on this machine (no %s on PATH besides harnessly's launcher)\n", tool, tool)
		return 127
	}
	argv := append([]string{tool}, targs...)
	base := withoutDirs(env.Environ(), skip)
	if !Heavy(tool, targs) || env.Getenv(envSlot) != "" || isOff(env.Getenv(envOff)) {
		if err := env.Exec(real, argv, base); err != nil {
			fmt.Fprintf(env.Stderr, "harnessly: cannot run %s: %v\n", real, err)
			return 126
		}
		return 0
	}
	return runHeavy(tool, real, argv, base, env)
}

func isOff(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "off", "0", "false", "no", "none":
		return true
	}
	return false
}

func runHeavy(tool, real string, argv, base []string, env Env) int {
	dir := Dir(env.Getenv)
	if dest := strings.TrimSpace(env.Getenv(envRemote)); dest != "" {
		if code, started := runRemote(dest, argv, base, env); started {
			record(dir, env.Now(), tool, "remote", 0)
			return code
		}
	}
	n := Slots(env.CPUs)
	if v, err := strconv.Atoi(strings.TrimSpace(env.Getenv(envSlots))); err == nil && v > 0 {
		n = v
	}
	wait := defaultWait
	if v, err := time.ParseDuration(strings.TrimSpace(env.Getenv(envWait))); err == nil && v >= 0 {
		wait = v
	}

	start := env.Now()
	slot, lock, err := acquire(dir, n, wait, env, func() {
		fmt.Fprintf(env.Stderr, "harnessly: every slot for heavy checks on this machine (%d) is taken by other agents; waiting up to %s for one to free up. "+
			"If this command is stopped or times out while it waits, do not run it again: go on without this check, or leave it to CI.\n",
			n, roundWait(wait))
	})
	waited := env.Now().Sub(start)
	switch {
	case errors.Is(err, errBusy):
		record(dir, env.Now(), tool, "refused", waited)
		fmt.Fprintf(env.Stderr, "harnessly: the machine is busy: other agents' builds and tests hold every slot for heavy checks (%d), and people's chats "+
			"run on this machine too. Waited %s; %s did not run. Do not retry it now: go on without this check, or leave the full run to CI. "+
			"(exit %d)\n", n, roundWait(waited), strings.Join(argv, " "), ExitBusy)
		return ExitBusy
	case err != nil:
		// The slots cannot be kept here (a read-only folder): run as the launcher would without them, gently.
		slot = 0
	}
	if lock != nil {
		defer lock.Close()
	}
	outcome := "ran"
	if waited >= env.Poll {
		outcome = "waited"
	}
	if slot > 0 {
		record(dir, env.Now(), tool, outcome, waited)
	}

	cmdArgv := gentle(base, argv, real)
	cmd := exec.Command(cmdArgv[0], cmdArgv[1:]...)
	cmd.Args = cmdArgv
	cmd.Stdin, cmd.Stdout, cmd.Stderr = env.Stdin, env.Stdout, env.Stderr
	cmd.Env = childEnv(base, tool, slot)
	// A signal for the launcher goes on to the tool; the launcher stays until the tool ends, to free the slot. (Caught,
	// not ignored: an ignored signal would stay ignored in the tool.)
	fwd := make(chan os.Signal, 4)
	signal.Notify(fwd, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(fwd)
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(env.Stderr, "harnessly: cannot run %s: %v\n", real, err)
		return 126
	}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case s := <-fwd:
				_ = cmd.Process.Signal(s)
			case <-done:
				return
			}
		}
	}()
	err = cmd.Wait()
	close(done)
	return exitOf(cmd, err)
}

func exitOf(cmd *exec.Cmd, err error) int {
	if err == nil {
		return 0
	}
	if cmd.ProcessState == nil {
		return 1
	}
	if st, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && st.Signaled() {
		return 128 + int(st.Signal())
	}
	if c := cmd.ProcessState.ExitCode(); c >= 0 {
		return c
	}
	return 1
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func roundWait(d time.Duration) string {
	if d >= time.Minute {
		return d.Round(time.Second).String()
	}
	return d.Round(100 * time.Millisecond).String()
}

// Heavy says whether the call takes a slot.
func Heavy(tool string, args []string) bool {
	if onlyInfo(args) {
		return false
	}
	switch tool {
	case "go":
		for i := 0; i < len(args); i++ {
			a := args[i]
			if a == "-C" {
				i++
				continue
			}
			if strings.HasPrefix(a, "-") {
				continue
			}
			switch a {
			case "build", "test", "vet", "install":
				return true
			}
			return false
		}
		return false
	case "vitest", "tsc", "eslint":
		return !watching(args)
	case "playwright":
		return len(args) > 0 && args[0] == "test"
	case "npm", "pnpm":
		return npmCall(tool, args)
	case "npx":
		for i := 0; i < len(args); i++ {
			a := args[i]
			if a == "-p" || a == "--package" || a == "-c" || a == "--call" {
				i++
				continue
			}
			if strings.HasPrefix(a, "-") {
				continue
			}
			name := a
			if at := strings.LastIndex(name, "@"); at > 0 {
				name = name[:at]
			}
			if name == "@playwright/test" {
				name = "playwright"
			}
			switch name {
			case "vitest", "playwright", "tsc", "eslint":
				return Heavy(name, args[i+1:])
			case "typescript":
				return true
			}
			return false
		}
	}
	return false
}

// onlyInfo is a call that asks for a version or the help.
// watching is a call that keeps running (vitest watch, tsc -w): it would hold a slot for good, so it takes none.
func watching(args []string) bool {
	for _, a := range args {
		switch a {
		case "--watch", "-w", "watch", "dev", "--watchAll":
			return true
		}
	}
	return false
}

// heavyScripts are the package scripts that take a slot under npm and pnpm: test, test:unit, lint, build:web…
var heavyScripts = map[string]bool{"test": true, "vitest": true, "playwright": true, "tsc": true, "eslint": true,
	"lint": true, "build": true, "check": true, "typecheck": true}

func heavyScript(name string) bool {
	base, _, _ := strings.Cut(name, ":")
	return heavyScripts[base]
}

// npmCall is `npm …` and `pnpm …`: a script of the list (npm test, npm run lint, pnpm build) or a tool of the list
// (npm exec vitest, pnpm dlx tsc, pnpm vitest).
func npmCall(tool string, args []string) bool {
	valued := map[string]bool{"--prefix": true, "-w": true, "--workspace": true, "-C": true, "--dir": true, "--filter": true,
		"-F": true, "--loglevel": true}
	var words []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			words = append(words, args[i+1:]...)
			break
		}
		if valued[a] {
			i++
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		words = append(words, args[i:]...)
		break
	}
	if len(words) == 0 {
		return false
	}
	rest := words[1:]
	switch words[0] {
	case "test", "t", "tst":
		return !watching(rest)
	case "run", "run-script", "rum", "urn":
		for i, a := range rest {
			if a == "--" || !strings.HasPrefix(a, "-") {
				if a == "--" {
					return false
				}
				return heavyScript(a) && !watching(rest[i+1:])
			}
		}
		return false
	case "exec", "x", "dlx":
		return Heavy("npx", rest)
	}
	// pnpm runs a script or a local tool by its bare name.
	return tool == "pnpm" && heavyScript(words[0]) && !watching(rest)
}

func onlyInfo(args []string) bool {
	if len(args) == 0 {
		return false
	}
	for _, a := range args {
		switch a {
		case "--version", "-v", "-V", "version", "--help", "-h", "help":
		default:
			return false
		}
	}
	return true
}

// acquire takes the first free slot of n, trying again every env.Poll until wait is over. busy is called once, when
// the first try finds them all taken. The slot's file stays open: closing it (or the process ending) frees the slot.
func acquire(dir string, n int, wait time.Duration, env Env, busy func()) (int, *os.File, error) {
	root, err := openDir(dir)
	if err != nil {
		return 0, nil, err
	}
	defer root.Close()
	deadline := env.Now().Add(wait)
	told := false
	for {
		usable := false
		for i := 1; i <= n; i++ {
			f, err := openShared(root, "slot"+strconv.Itoa(i), os.O_RDONLY)
			if err != nil {
				continue
			}
			usable = true
			if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
				return i, f, nil
			}
			f.Close()
		}
		if !usable {
			return 0, nil, fmt.Errorf("no slot file can be opened in %s", dir)
		}
		if !env.Now().Before(deadline) {
			return 0, nil, errBusy
		}
		if !told {
			told = true
			busy()
		}
		time.Sleep(min(env.Poll, max(deadline.Sub(env.Now()), time.Millisecond)))
	}
}

var errBusy = errors.New("no free slot")

// openDir opens the slots' folder, making it writable by every user of the machine, as /tmp is. Every file of it is
// then opened through the returned root, so the folder checked is the folder used, and nothing leads out of it.
//
// A folder that is there already must be a real folder (not a link), owned by root or by this user, or sticky and open
// to all like /tmp: anything else may be another user's trap, and the slots are not used.
func openDir(dir string) (*os.Root, error) {
	if err := os.Mkdir(dir, 0o700); err == nil {
		// Ours and closed to others until it is opened to all through its own descriptor, never by path.
		root, err := os.OpenRoot(dir)
		if err != nil {
			return nil, err
		}
		d, err := root.Open(".")
		if err == nil {
			err = d.Chmod(os.ModeSticky | 0o777)
			d.Close()
		}
		if err != nil {
			root.Close()
			return nil, err
		}
		return root, nil
	} else if !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	st, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", dir)
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		owner := int(sys.Uid)
		if owner != 0 && owner != os.Getuid() && (st.Mode()&os.ModeSticky == 0 || st.Mode().Perm() != 0o777) {
			return nil, fmt.Errorf("%s belongs to another user and is not a sticky folder open to all", dir)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	// The folder opened is the one checked, not a link put in its place since.
	if now, err := root.Stat("."); err != nil || !os.SameFile(st, now) {
		root.Close()
		return nil, fmt.Errorf("%s changed while it was opened", dir)
	}
	return root, nil
}

// openShared opens a file of the shared folder, making it (readable and writable by all) when it is not there. An
// existing file is opened without O_CREAT: in a sticky folder the kernel refuses O_CREAT on another user's file
// (fs.protected_regular).
func openShared(root *os.Root, name string, flag int) (*os.File, error) {
	f, err := openRegular(root, name, flag)
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		return f, err
	}
	nf, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o666)
	if err == nil {
		_ = nf.Chmod(0o666)
		nf.Close()
	} else if !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	return openRegular(root, name, flag)
}

// openRegular opens a plain file of the folder: never a link, a device or a fifo, and never a second name of a file
// that lives elsewhere (a hard link).
func openRegular(root *os.Root, name string, flag int) (*os.File, error) {
	f, err := root.OpenFile(name, flag|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	ok := err == nil && st.Mode().IsRegular()
	if sys, isSys := st.Sys().(*syscall.Stat_t); ok && isSys && uint64(sys.Nlink) != 1 {
		ok = false
	}
	if !ok {
		f.Close()
		return nil, fmt.Errorf("%s is not a plain file of its own", name)
	}
	return f, nil
}

// ownFile says whether this user owns the open file.
func ownFile(f *os.File) bool {
	st, err := f.Stat()
	if err != nil {
		return false
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	return ok && int(sys.Uid) == os.Getuid()
}

// gentle puts the tool under nice and ionice when the machine has them.
func gentle(env, argv []string, real string) []string {
	path := ""
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			path = strings.TrimPrefix(kv, "PATH=")
		}
	}
	out := []string{}
	if p, err := lookPath("nice", path, nil); err == nil {
		out = append(out, p, "-n", "10")
	}
	if p, err := lookPath("ionice", path, nil); err == nil {
		out = append(out, p, "-c3")
	}
	out = append(out, real)
	return append(out, argv[1:]...)
}

// childEnv marks the slot as held and, for go, asks for fewer threads and parallel packages.
func childEnv(base []string, tool string, slot int) []string {
	env := setEnv(base, envSlot, strconv.Itoa(max(slot, 1)))
	if tool != "go" {
		return env
	}
	env = setEnv(env, "GOMAXPROCS", "2")
	flags := getEnv(env, "GOFLAGS")
	if !hasParallelFlag(flags) {
		env = setEnv(env, "GOFLAGS", strings.TrimSpace(flags+" -p=2"))
	}
	return env
}

func hasParallelFlag(flags string) bool {
	for _, f := range strings.Fields(flags) {
		if f == "-p" || strings.HasPrefix(f, "-p=") {
			return true
		}
	}
	return false
}

func getEnv(env []string, name string) string {
	for _, kv := range env {
		if strings.HasPrefix(kv, name+"=") {
			return strings.TrimPrefix(kv, name+"=")
		}
	}
	return ""
}

func setEnv(env []string, name, value string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, name+"=") {
			out = append(out, kv)
		}
	}
	return append(out, name+"="+value)
}

// withoutDirs drops the launchers' folders from PATH, so the tool and what it starts find the real tools.
func withoutDirs(env, dirs []string) []string {
	if len(dirs) == 0 {
		return env
	}
	path := getEnv(env, "PATH")
	var keep []string
	for _, d := range filepath.SplitList(path) {
		if !inDirs(d, dirs) {
			keep = append(keep, d)
		}
	}
	return setEnv(env, "PATH", strings.Join(keep, string(filepath.ListSeparator)))
}

func inDirs(d string, dirs []string) bool {
	if d == "" {
		return false
	}
	cd := filepath.Clean(d)
	for _, s := range dirs {
		if s != "" && filepath.Clean(s) == cd {
			return true
		}
	}
	return false
}

// lookPath finds the real tool on path: not in the skipped folders and not a harnessly launcher.
func lookPath(name, path string, skip []string) (string, error) {
	for _, d := range filepath.SplitList(path) {
		if d == "" || inDirs(d, skip) {
			continue
		}
		p := filepath.Join(d, name)
		st, err := os.Stat(p)
		if err != nil || st.IsDir() || st.Mode().Perm()&0o111 == 0 {
			continue
		}
		if isLauncher(p) {
			continue
		}
		return p, nil
	}
	return "", exec.ErrNotFound
}

func isLauncher(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	return strings.HasPrefix(string(head[:n]), "#!") && strings.Contains(string(head[:n]), Marker)
}

// record appends one heavy call to <dir>/events: unix time, tool, ran|waited|refused|remote, milliseconds waited. The file is
// started over past 1 MiB by its owner.
func record(dir string, now time.Time, tool, outcome string, waited time.Duration) {
	root, err := openDir(dir)
	if err != nil {
		return
	}
	defer root.Close()
	f, err := openShared(root, "events", os.O_WRONLY|os.O_APPEND)
	if err != nil {
		return
	}
	defer f.Close()
	// Only its owner starts the file over; the others append.
	if st, err := f.Stat(); err == nil && st.Size() > maxEvents && ownFile(f) {
		_ = f.Truncate(0)
	}
	fmt.Fprintf(f, "%d %s %s %d\n", now.Unix(), tool, outcome, waited.Milliseconds())
}

// HelpText is `harnessly heavy --help`.
const HelpText = `harnessly heavy — heavy checks share the machine

The machine is shared: other agents and people's chats run on it. harnessly puts launchers named go, vitest,
playwright, tsc, eslint, npx, npm and pnpm on your PATH. A light call (go env, npm install, tsc --version) runs at
once. A heavy one (go build/test/vet/install, vitest, playwright test, tsc, eslint, npx of those, npm test, npm run
lint/build/check) takes one of the machine's few slots, runs gently (nice, ionice, GOMAXPROCS=2, -p=2), and waits when they are all taken.

When it says the machine is busy (exit 75), do not run the command again: go on without the check, or leave
the full run to CI. Check in one batch at the end of a task, and only the package or test you changed.

Usage:
  harnessly heavy stats [--since 24h] [--json]   how many heavy calls ran, waited for a slot, were refused, ran remotely

Settings (environment): HARNESSLY_HEAVY=off runs everything at once; HARNESSLY_HEAVY_SLOTS, HARNESSLY_HEAVY_WAIT
(default 15m), HARNESSLY_HEAVY_DIR (default /var/lib/harnessly-heavy when it exists, else /tmp/harnessly-heavy).
HARNESSLY_HEAVY_REMOTE=<ssh destination> runs go test, go vet, a go build whose result go drops, the node checks
(tsc --noEmit, vitest run, eslint, npx/npm exec of those, a package script made only of them) and the node builds
(tsc that emits, vite build, next build, a script of them such as pnpm build) on that machine: the git working tree
is copied there with rsync (without .git and node_modules, under HARNESSLY_HEAVY_REMOTE_ROOT, default
~/.cache/harnessly-remote), node_modules is installed there from the tree's pnpm-lock.yaml or package-lock.json once
per lockfile, the output and exit code come back, and so do the files a node build wrote (a file changed here
meanwhile stays). Calls that write elsewhere or change sources (go test -c, -o, profiles, -fuzz, -args, a test's own
flag such as -update, go build -o, eslint --fix, vitest -u or --coverage) and playwright stay here; when the copy,
the connection or the install fails, the call runs here after one line saying so.
`
