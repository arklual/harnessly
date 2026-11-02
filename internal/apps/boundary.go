package apps

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/ly-solutions/harnessly/internal/agents"
)

// The operating system's wall between the agent of a chat with apps and the apps (HRN-527). The guard hook and Claude's
// deny rules judge the agent's tool calls; a shell the agent bends (a script, `cd` and a relative path, python) passes
// them, because the agent runs as the person. So the agent's process tree runs in a mount namespace of its own, made by
// bubblewrap: the whole machine bound as it is (`--dev-bind / /`), and over it the registry and every installed
// package and source folder bound read-only. A write there fails in the kernel (EROFS) whatever program makes it; the
// runtime, outside the namespace, still writes them. bwrap's child is the runtime's own `sandbox-exec`, which takes
// the terminal's foreground for the agent's CLI before it becomes it (ForegroundExec): the pane's foreground program
// stays the CLI, and Ctrl-C reaches the agent, not bwrap.
//
// A machine where the wall cannot be made (no bwrap, user namespaces refused) starts the chat without it and says so:
// a warning in the runtime's log at every such launch, the machine check `apps_boundary`, and a line in the app's
// «О приложении». It does not refuse the chat: the hook and the deny rules still hold there, and a chat that cannot
// start loses the person's work for a wall the person cannot put up from the page.

const landlockScopesABI = 6

// BoundaryProbeFile is the file the probe tries to create in the protected folder: its being there after a probe means
// the wall did not hold.
const BoundaryProbeFile = ".boundary-probe"

// Boundary makes the wall: the bwrap it runs and the helper bwrap starts the CLI through.
type Boundary struct {
	// Bwrap is bubblewrap's path; "" when the machine has none.
	Bwrap string
	// Helper is the command line of ForegroundExec without its `--` (the runtime binary and `sandbox-exec`).
	Helper []string
	// Run runs a probe; nil runs it as a process of this machine. Gaps uses it too.
	Run func(name string, args ...string) (stdout, stderr []byte, err error)

	mu     sync.Mutex
	probed bool
	err    error
}

// ErrNoBwrap: the machine has no bubblewrap.
var ErrNoBwrap = errors.New("bubblewrap (bwrap) is not installed")

// Ready says whether the wall can be made, probing under dir (a protected folder that exists) until a probe passes:
// one that passed is kept for the process, one that failed is tried again at the next launch, so a package or a
// setting put right on the machine takes without a restart.
func (b *Boundary) Ready(dir string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.probed && b.err == nil {
		return nil
	}
	b.probed, b.err = true, b.probe(dir)
	return b.err
}

// State is the last probe's result: probed false before the first one.
func (b *Boundary) State() (probed bool, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.probed, b.err
}

// probe runs a shell in the wall that tries to create a file in dir: it must say "refused", and the file must not be
// there after it. (The attempt is a subshell's: a failed redirect of `:` ends a POSIX shell.)
func (b *Boundary) probe(dir string) error {
	if b.Bwrap == "" {
		return ErrNoBwrap
	}
	if len(b.Helper) == 0 {
		return errors.New("no sandbox-exec helper")
	}
	run := b.Run
	if run == nil {
		run = runProbe
	}
	probe := filepath.Join(dir, BoundaryProbeFile)
	args := append(b.args([]string{dir}), "/bin/sh", "-c", `if (: > "$1") 2>/dev/null; then echo writable; else echo refused; fi`, "sh", probe)
	stdout, stderr, err := run(b.Bwrap, args...)
	if _, serr := os.Lstat(probe); serr == nil {
		_ = os.Remove(probe)
		return fmt.Errorf("a write into %s went through the read-only bind", dir)
	}
	if err != nil {
		return fmt.Errorf("bwrap: %s", firstLine(stderr, err))
	}
	if got := strings.TrimSpace(string(stdout)); got != "refused" {
		return fmt.Errorf("the probe in the namespace said %q", got)
	}
	return nil
}

// ErrSelfSSH: the person's own login over ssh from this machine works, and such a shell is outside the wall.
var ErrSelfSSH = errors.New("ssh from this machine to itself logs in, and a shell got that way is outside the wall")

// SelfSSHFix refuses logins that come from the machine itself. sshd has no way to refuse a user in a Match block, so it
// takes away every way of proving who one is; logins from elsewhere are as they were.
const SelfSSHFix = `printf 'Match Address 127.0.0.0/8,::1,%s\n  PubkeyAuthentication no\n  PasswordAuthentication no\n  KbdInteractiveAuthentication no\n  HostbasedAuthentication no\n' "$(hostname -I | tr ' ' ',' | sed 's/,*$//')" | sudo tee /etc/ssh/sshd_config.d/10-no-self-login.conf && sudo systemctl reload ssh`

// Gaps says what the machine leaves open with the wall up: a login to itself, and a kernel without what sandbox-exec asks
// of it (harden_linux.go). nil when there is nothing to say.
func (b *Boundary) Gaps() error {
	run := b.Run
	if run == nil {
		run = runProbe
	}
	if path, err := exec.LookPath("ssh"); err == nil {
		if _, _, err := run(path, "-n", "-o", "BatchMode=yes", "-o", "ConnectTimeout=3", "-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null", "-o", "LogLevel=QUIET", "127.0.0.1", "true"); err == nil {
			return ErrSelfSSH
		}
	}
	switch abi := landlockABI(); {
	case abi < 1:
		return errors.New("the kernel has Landlock off: boot it with lsm=landlock,… so that the agent cannot reach a process outside the wall")
	case abi < landlockScopesABI:
		return fmt.Errorf("the kernel's Landlock is version %d: signals and abstract sockets out of the wall need 6 (Linux 6.12)", abi)
	}
	return nil
}

func runProbe(name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.Command(name, args...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return out.Bytes(), errOut.Bytes(), err
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		return nil, nil, fmt.Errorf("the probe did not end in 10s (%v)", <-done)
	}
}

func firstLine(stderr []byte, err error) string {
	if line, _, _ := strings.Cut(strings.TrimSpace(string(stderr)), "\n"); line != "" {
		return strings.TrimPrefix(line, "bwrap: ")
	}
	return err.Error()
}

// args are bwrap's arguments up to and including the helper's `--`: the machine as it is, the protected folders
// read-only.
func (b *Boundary) args(protected []string, hidden ...string) []string {
	return b.sealedArgs(protected, nil, hidden...)
}

func (b *Boundary) sealedArgs(protected []string, seal *Seal, hidden ...string) []string {
	args := []string{"--dev-bind", "/", "/"}
	bound := BoundDirs(protected)
	// A read-only bind keeps its folder, not the path to it: renaming an ancestor (`mv ~/.harnessly ~/x`) would carry
	// the bind away and leave the path free for a registry of the agent's making, which the runtime outside then reads.
	// A mount point cannot be renamed or removed (EBUSY), so every ancestor the agent could rename (its parent is
	// writable) is bound onto itself, writable as before, ahead of the read-only binds beneath it.
	for _, a := range pinnedAncestors(bound) {
		args = append(args, "--bind", a, a)
	}
	// Ahead of the read-only binds: the data folder's tmpfs would hide them.
	args = append(args, seal.dataArgs(bound)...)
	for _, dir := range bound {
		args = append(args, "--ro-bind", dir, dir)
	}
	args = append(args, seal.homeArgs(bound)...)
	args = append(args, busMask()...)
	args = append(args, tmuxMask()...)
	args = append(args, socketMask()...)
	// The person's own credentials (the runtime's cli-token, the key agent tokens are signed with, the MCP config that
	// carries the cli-token) read as empty inside: the agent has a token of its own launch, which the routes that
	// change the apps refuse, and must not be able to pass for the person.
	for _, f := range hidden {
		if info, err := os.Lstat(f); err == nil && info.Mode().IsRegular() {
			args = append(args, "--ro-bind", "/dev/null", f)
		}
	}
	args = append(args, "--")
	args = append(args, b.Helper...)
	return append(args, "--")
}

// Words are the wall as shell words that go in front of the CLI in a launch line; hidden files read as empty inside.
func (b *Boundary) Words(protected []string, hidden ...string) string {
	return b.SealedWords(protected, nil, hidden...)
}

// SealedWords are Words with the data folder and the person's start-up files sealed (seal.go).
func (b *Boundary) SealedWords(protected []string, seal *Seal, hidden ...string) string {
	words := []string{agents.ShellQuote(b.Bwrap)}
	for _, a := range b.sealedArgs(protected, seal, hidden...) {
		words = append(words, agents.ShellQuote(a))
	}
	return strings.Join(words, " ")
}

// BoundDirs are the folders the wall binds read-only: each protected folder as it really is (the written form leads
// there, so one bind covers both), only those that exist (bwrap cannot bind a missing one; a missing package folder has
// nothing to write into, and the hook still refuses one being made), and none inside another one bound already.
func BoundDirs(protected []string) []string {
	var real []string
	for _, dir := range protected {
		r, err := realPath(dir)
		if err != nil {
			continue
		}
		if info, err := os.Stat(r); err != nil || !info.IsDir() {
			continue
		}
		if !slices.Contains(real, r) {
			real = append(real, r)
		}
	}
	slices.Sort(real)
	var out []string
	for _, d := range real {
		if !slices.ContainsFunc(out, func(parent string) bool { return within(parent, d) }) {
			out = append(out, d)
		}
	}
	return out
}

// pinnedAncestors are the folders above the bound ones that the agent could rename: each one whose parent the person may
// write, or may make writable (owns), shallowest first, none of them a bound folder itself.
func pinnedAncestors(bound []string) []string {
	var out []string
	for _, dir := range bound {
		for a := filepath.Dir(dir); a != "/" && a != "."; a = filepath.Dir(a) {
			if slices.Contains(out, a) || slices.Contains(bound, a) {
				continue
			}
			if renamable(filepath.Dir(a)) {
				out = append(out, a)
			}
		}
	}
	slices.SortFunc(out, func(x, y string) int { return strings.Count(x, "/") - strings.Count(y, "/") })
	return out
}

// busMask keeps the agent from asking the person's session bus and user systemd to run something outside the wall
// (`systemd-run --user`, a D-Bus activation): the bus socket gets /dev/null bound over it, the user manager's private
// folder an empty tmpfs, and the bus address leaves the environment. The rest of the runtime folder (the ssh and gpg
// agents' sockets git uses) stays.
func busMask() []string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	out := []string{"--unsetenv", "DBUS_SESSION_BUS_ADDRESS"}
	// The bus where the environment says it is, besides the usual place (an abstract address has no path to cover; a
	// person's session bus on a machine is the socket in the runtime folder).
	buses := []string{filepath.Join(dir, "bus")}
	for _, part := range strings.Split(os.Getenv("DBUS_SESSION_BUS_ADDRESS"), ";") {
		for _, kv := range strings.Split(strings.TrimPrefix(part, "unix:"), ",") {
			// D-Bus escapes a value's bytes as %XX.
			p, ok := strings.CutPrefix(kv, "path=")
			if !ok {
				continue
			}
			if dec, err := url.PathUnescape(p); err == nil {
				p = dec
			}
			if filepath.IsAbs(p) && !slices.Contains(buses, p) {
				buses = append(buses, p)
			}
		}
	}
	// A link to the socket is covered where it leads: the socket itself gets the bind.
	var masked []string
	for _, bus := range buses {
		real, err := filepath.EvalSymlinks(bus)
		if err != nil {
			continue
		}
		if info, err := os.Stat(real); err == nil && info.Mode()&os.ModeSocket != 0 && !slices.Contains(masked, real) {
			masked = append(masked, real)
			out = append(out, "--ro-bind", "/dev/null", real)
		}
	}
	if info, err := os.Stat(filepath.Join(dir, "systemd")); err == nil && info.IsDir() {
		out = append(out, "--tmpfs", filepath.Join(dir, "systemd"))
	}
	return out
}

// renamable says whether a folder's entries can be renamed by the person: it is writable, or the person owns it and so
// can make it writable.
func renamable(dir string) bool {
	if unix.Access(dir, unix.W_OK) == nil {
		return true
	}
	var st unix.Stat_t
	return unix.Stat(dir, &st) == nil && int(st.Uid) == os.Geteuid()
}

// tmuxMask keeps the tmux server out of reach: a window or a command it runs (`tmux new-window '…'`) runs outside the
// wall. The folder of its sockets is an empty tmpfs inside and TMUX leaves the environment; the launch names the tab's
// session in HARNESSLY_SESSION before the wall, which is all the hooks and the CLI asked tmux for.
func tmuxMask() []string {
	out := []string{"--unsetenv", "TMUX", "--unsetenv", "TMUX_PANE"}
	var dirs []string
	base := os.Getenv("TMUX_TMPDIR")
	if base == "" {
		base = "/tmp"
	}
	dirs = append(dirs, filepath.Join(base, fmt.Sprintf("tmux-%d", os.Getuid())))
	if sock, _, _ := strings.Cut(os.Getenv("TMUX"), ","); filepath.IsAbs(sock) {
		if d := filepath.Dir(sock); !slices.Contains(dirs, d) {
			dirs = append(dirs, d)
		}
	}
	for _, d := range dirs {
		// A folder that is not there yet is made the way tmux makes it (0700, the person's), so a server started after
		// the launch line was written still has its socket under the mask; tmux takes a folder of that mode as its own.
		if _, err := os.Lstat(d); os.IsNotExist(err) {
			_ = os.MkdirAll(d, 0o700)
		}
		if real, err := filepath.EvalSymlinks(d); err == nil {
			if info, err := os.Stat(real); err == nil && info.IsDir() {
				out = append(out, "--tmpfs", real)
			}
		}
	}
	return out
}
