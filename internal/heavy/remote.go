package heavy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

// A heavy go test, go vet, result-dropping go build or node check (node.go) can run on another machine: with HARNESSLY_HEAVY_REMOTE=<ssh destination> the launcher
// copies the git working tree it is called in to that machine (rsync, without .git and node_modules), runs the call
// there over ssh, streams its output and returns its exit code. Go calls that write nothing back go there: go test
// with the flags of go test alone (no -c, -o, profiles, -fuzz, -args or a test's own flag such as -update) and go vet;
// a node build's output is copied back into the tree after it (node.go).
// When the copy or the connection fails before the call starts, it runs here in a slot, after one line saying why.
const (
	envRemote     = "HARNESSLY_HEAVY_REMOTE"
	envRemoteRoot = "HARNESSLY_HEAVY_REMOTE_ROOT"
	// defaultRemoteRoot is under the remote user's home; the tree lands at <root>/<its path here>.
	defaultRemoteRoot = ".cache/harnessly-remote"
)

// Exit codes of the remote wrapper that mean the call did not start there; 255 is ssh's own failure.
const (
	exitNoDeps = 96
	exitNoDir  = 97
	exitNoTool = 98
	exitSSH    = 255
)

// remoteWrapper runs on the remote machine as `sh -c remoteWrapper sh DIR DEPS TOOL ARGS…`. With DEPS (the folder of
// a node call's lockfile) it first installs node_modules there, once per lockfile and package.json, one install at a
// time; a lockfile that did not install is not tried again until it changes. The tool gets a session of its
// own; when ssh's stdin closes (the launcher here was stopped, or the connection dropped) the whole session is stopped,
// so nothing is left running there. A harnessly on the remote PATH runs the call in its own slots.
const remoteWrapper = `d=$1; deps=$2; shift 2
case $d in /*) ;; *) d=$PWD/$d ;; esac
case $deps in ''|/*) ;; *) deps=$PWD/$deps ;; esac
cd "$d" 2>/dev/null || exit 97
PATH=$PATH:/usr/local/go/bin
if [ -n "$deps" ]; then
	cd "$deps" 2>/dev/null || exit 97
	export COREPACK_ENABLE_DOWNLOAD_PROMPT=0 npm_config_update_notifier=false
	if [ -f pnpm-lock.yaml ]; then l=pnpm-lock.yaml; i="pnpm install --frozen-lockfile --prefer-offline --config.confirmModulesPurge=false"
	else l=package-lock.json; i="npm ci --prefer-offline --no-audit --no-fund --loglevel=error"; fi
	s=$(cat package.json "$l" 2>/dev/null | sha256sum | cut -c1-16)
	mkdir -p "$HOME/.cache"
	exec 4>"$HOME/.cache/harnessly-deps.lock"
	flock 4
	if [ "$(cat node_modules/.harnessly-deps 2>/dev/null)" != "$s" ]; then
		[ "$(cat node_modules/.harnessly-deps-failed 2>/dev/null)" = "$s" ] && exit 96
		echo "harnessly: installing node_modules on $(hostname) from $l (once per lockfile)" >&2
		if ! CI=1 $i </dev/null >"$HOME/.cache/harnessly-deps.log" 2>&1; then
			tail -n 5 "$HOME/.cache/harnessly-deps.log" >&2
			mkdir -p node_modules && echo "$s" >node_modules/.harnessly-deps-failed
			exit 96
		fi
		echo "$s" >node_modules/.harnessly-deps
	fi
	exec 4>&-
	PATH=$PWD/node_modules/.bin:$PATH
	cd "$d" || exit 97
	PATH=$PWD/node_modules/.bin:$PATH
fi
if command -v harnessly >/dev/null 2>&1; then set -- harnessly heavy "$@"
elif ! command -v "$1" >/dev/null 2>&1; then exit 98; fi
exec 3<&0
setsid "$@" </dev/null 3<&- & p=$!
( while read -r _; do :; done <&3; kill -TERM -$p 2>/dev/null || kill -TERM $p ) >/dev/null 2>&1 & w=$!
exec 3<&-
wait $p; c=$?
kill $w 2>/dev/null
exit $c`

var sshOptions = []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=4"}

// goTestFlags are go test's and go build's own flags that write nothing into the tree; any other flag stays here.
var goTestFlags = map[string]bool{
	"run": true, "skip": true, "v": true, "count": true, "timeout": true, "short": true, "failfast": true,
	"bench": true, "benchtime": true, "benchmem": true, "cpu": true, "parallel": true, "shuffle": true, "json": true,
	"list": true, "race": true, "msan": true, "asan": true, "cover": true, "covermode": true, "coverpkg": true,
	"vet": true, "tags": true, "p": true, "mod": true, "trimpath": true, "gcflags": true, "ldflags": true,
	"asmflags": true, "a": true, "x": true, "n": true, "buildvcs": true, "fullpath": true,
}

// goVetStays are go vet's flags that need this machine: another analysis tool or another folder.
var goVetStays = map[string]bool{"vettool": true, "C": true, "modfile": true, "overlay": true, "pgo": true}

// remoteFits tells whether a heavy call may run on the remote machine.
func remoteFits(tool string, args []string) bool {
	if tool != "go" || len(args) == 0 {
		return false
	}
	switch args[0] {
	case "test":
		for _, a := range args[1:] {
			if a == "-args" || a == "--args" {
				return false
			}
			if name, ok := flagName(a); ok && !goTestFlags[name] {
				return false
			}
		}
		return true
	case "vet":
		for _, a := range args[1:] {
			if name, ok := flagName(a); ok && goVetStays[name] {
				return false
			}
		}
		return true
	case "build":
		return buildDiscards(args[1:])
	}
	return false
}

// buildDiscards tells whether go build only checks that the code compiles: go drops what it built for several
// packages or for -o /dev/null, while one package without -o may leave its binary in the folder.
func buildDiscards(args []string) bool {
	discard, pkgs := false, 0
	for i := 0; i < len(args); i++ {
		a := args[i]
		if name, ok := flagName(a); ok {
			if name != "o" {
				if !goTestFlags[name] {
					return false
				}
				continue
			}
			out, set := strings.CutPrefix(strings.TrimLeft(a, "-"), "o=")
			if !set && i+1 < len(args) {
				i++
				out = args[i]
			}
			if out != os.DevNull {
				return false
			}
			discard = true
			continue
		}
		// Only paths that are surely packages count: a flag's value (-tags x) may stand between them.
		if a == "." || strings.HasPrefix(a, "./") || strings.HasPrefix(a, "../") || strings.Contains(a, "...") {
			if strings.Contains(a, "...") {
				discard = true
			}
			pkgs++
		}
	}
	return discard || pkgs > 1
}

// flagName is the name of a flag argument (-run=X, --count, -v): false for a package or a flag's value.
func flagName(a string) (string, bool) {
	if !strings.HasPrefix(a, "-") || a == "-" {
		return "", false
	}
	name := strings.TrimLeft(a, "-")
	if i := strings.IndexByte(name, '='); i >= 0 {
		name = name[:i]
	}
	name = strings.TrimPrefix(name, "test.")
	return name, name != ""
}

// runRemote runs the call on dest; started is false when it did not start there and should run here.
func runRemote(dest string, argv, base []string, env Env) (code int, started bool) {
	tool := argv[0]
	cwd, err := env.Getwd()
	if err != nil {
		return 0, false
	}
	top := gitTop(cwd)
	if top == "" {
		return 0, false
	}
	if home := env.Getenv("HOME"); top == "/" || (home != "" && filepath.Clean(home) == top) {
		return 0, false
	}
	rel, err := filepath.Rel(top, cwd)
	if err != nil {
		return 0, false
	}
	deps, back := "", false
	if tool == "go" {
		if !remoteFits(tool, argv[1:]) {
			return 0, false
		}
	} else {
		var ok bool
		if deps, back, ok = nodeFits(tool, argv[1:], cwd, top); !ok {
			return 0, false
		}
	}
	root := strings.TrimSpace(env.Getenv(envRemoteRoot))
	if root == "" {
		root = defaultRemoteRoot
	}
	there := filepath.Join(root, top)
	depsThere := ""
	if deps != "" {
		r, _ := filepath.Rel(top, deps)
		depsThere = filepath.Join(there, r)
	}
	fallback := func(why string) (int, bool) {
		fmt.Fprintf(env.Stderr, "harnessly: %s did not start on %s (%s); running it on this machine\n", strings.Join(argv, " "), dest, why)
		return 0, false
	}

	rsync, err := lookPath("rsync", env.Getenv("PATH"), nil)
	if err != nil {
		return fallback("no rsync here")
	}
	ssh, err := lookPath("ssh", env.Getenv("PATH"), nil)
	if err != nil {
		return fallback("no ssh here")
	}
	var msg bytes.Buffer
	copyArgs := []string{"-a", "--compress", "--timeout=30", "--exclude=/.git", "--exclude=node_modules",
		"-e", ssh + " " + strings.Join(sshOptions, " ")}
	sync := exec.Command(rsync, append(append([]string{"--delete", "--mkpath"}, copyArgs...), top+"/", dest+":"+there+"/")...)
	sync.Env, sync.Stderr = base, &msg
	if err := sync.Run(); err != nil {
		return fallback("copying the tree: " + firstLine(msg.String(), err))
	}

	line := []string{"env"}
	for _, kv := range base {
		if k, _, ok := strings.Cut(kv, "="); ok && remoteVar(k, kv) {
			line = append(line, shellQuote(kv))
		}
	}
	line = append(line, "sh", "-c", shellQuote(remoteWrapper), "sh", shellQuote(filepath.Join(there, rel)), shellQuote(depsThere))
	for _, a := range argv {
		line = append(line, shellQuote(a))
	}
	cmdArgv := append(append([]string{"ssh"}, sshOptions...), dest, strings.Join(line, " "))
	cmd := exec.Command(ssh)
	cmd.Args = cmdArgv
	cmd.Env = base
	// The remote call is stopped by closing this pipe, which its wrapper watches.
	stdin, keep, err := os.Pipe()
	if err != nil {
		return fallback(err.Error())
	}
	out := &counting{w: env.Stdout}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, out, env.Stderr
	stop := make(chan os.Signal, 4)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(stop)
	if err := cmd.Start(); err != nil {
		stdin.Close()
		keep.Close()
		return fallback(err.Error())
	}
	stdin.Close()
	done := make(chan struct{})
	stopped := make(chan bool, 1)
	go func() {
		select {
		case <-stop:
			keep.Close()
			stopped <- true
		case <-done:
			stopped <- false
		}
	}()
	err = cmd.Wait()
	close(done)
	interrupted := <-stopped
	if !interrupted {
		keep.Close()
	}
	code = exitOf(cmd, err)
	if !interrupted {
		switch code {
		case exitSSH:
			// ssh's own failure; a call that started has written to stdout (go test always does).
			if out.n == 0 {
				return fallback("ssh could not connect")
			}
		case exitNoDir:
			return fallback("the copied tree is not there")
		case exitNoDeps:
			return fallback("node_modules did not install there")
		case exitNoTool:
			return fallback(argv[0] + " is not installed there")
		}
	}
	if interrupted && code == 0 {
		code = 130
	}
	if back && !interrupted {
		// What the build wrote comes back; a file changed here meanwhile is newer and stays (--update), and
		// nothing here is deleted.
		msg.Reset()
		pull := exec.Command(rsync, append(append([]string{"--update"}, copyArgs...), dest+":"+there+"/", top+"/")...)
		pull.Env, pull.Stderr = base, &msg
		if err := pull.Run(); err != nil {
			fmt.Fprintf(env.Stderr, "harnessly: %s ran on %s, but its output did not come back: %s\n", strings.Join(argv, " "), dest, firstLine(msg.String(), err))
			if code == 0 {
				code = 1
			}
		}
	}
	return code, true
}

// counting passes the remote call's stdout through and counts it.
type counting struct {
	w io.Writer
	n int64
}

func (c *counting) Write(b []byte) (int, error) {
	n, err := c.w.Write(b)
	c.n += int64(n)
	return n, err
}

// remoteVar tells whether an environment variable goes to the remote call: Go's settings, without the paths of this
// machine and the thread limit made for it.
func remoteVar(k, kv string) bool {
	switch k {
	case "NODE_ENV", "TZ", "FORCE_COLOR", "NO_COLOR":
		return true
	case "NODE_OPTIONS":
		// Not when it names a file of this machine (--require, --import).
		return !strings.Contains(kv, "/")
	}
	if !strings.HasPrefix(k, "GO") && !strings.HasPrefix(k, "CGO_") {
		return false
	}
	switch k {
	case "GOPATH", "GOROOT", "GOCACHE", "GOMODCACHE", "GOTMPDIR", "GOBIN", "GOENV", "GOMAXPROCS", "GOTELEMETRYDIR":
		return false
	case "GOWORK":
		return kv == "GOWORK=off"
	}
	return true
}

// gitTop is the working tree that holds dir: the nearest folder with .git, or "" outside one.
func gitTop(dir string) string {
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		if d == filepath.Dir(d) {
			return ""
		}
	}
}

func firstLine(s string, err error) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if s == "" && err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return fmt.Sprintf("exit %d", ee.ExitCode())
		}
		return err.Error()
	}
	return s
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
