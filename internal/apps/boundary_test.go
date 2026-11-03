package apps

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents"
)

// The test binary is the wall's helper too: bwrap starts `<test binary> sandbox-exec -- <command>`, as it starts the
// runtime's own subcommand in a launch.
func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == "sandbox-exec" && os.Args[2] == "--" {
		err := ForegroundExec(os.Args[3:])
		fmt.Fprintln(os.Stderr, "sandbox-exec:", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// wallOrSkip is a Boundary of this machine; the test is skipped only where bwrap or the namespaces it needs are not
// there, and says why.
func wallOrSkip(t *testing.T) *Boundary {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the wall is a Linux mount namespace")
	}
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("no bwrap on this machine")
	}
	if out, err := exec.Command(bwrap, "--dev-bind", "/", "/", "--", "true").CombinedOutput(); err != nil {
		t.Skipf("bwrap cannot make a namespace here: %s", strings.TrimSpace(string(out)))
	}
	return &Boundary{Bwrap: bwrap, Helper: []string{os.Args[0], "sandbox-exec"}}
}

// A real child under the wall, as a launch line runs it: every way of writing into the registry and the packages that
// passes the hook (a shell redirect, `cd` and a relative path, a link to the folder, a script it writes and runs,
// another interpreter) fails in the kernel; the chat's own folder stays writable, and the runtime outside still writes
// the registry.
func TestBoundaryKeepsTheAgentOutOfTheApps(t *testing.T) {
	b := wallOrSkip(t)
	data := t.TempDir()
	root := filepath.Join(data, "apps")
	pkg := filepath.Join(root, "packages", "acme.review", "1.0.0")
	source := filepath.Join(t.TempDir(), "author-source")
	work := filepath.Join(t.TempDir(), "work")
	for _, d := range []string{pkg, source, work} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(data, "apps-link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if err := b.Ready(root); err != nil {
		t.Fatalf("probe: %v", err)
	}
	words := b.Words([]string{link, root, pkg, source})
	if strings.Count(words, agents.ShellQuote("--ro-bind")+" "+agents.ShellQuote(root)) != 1 || strings.Count(words, agents.ShellQuote("--ro-bind")+" "+agents.ShellQuote(source)) != 1 || strings.Contains(words, agents.ShellQuote("--ro-bind")+" "+agents.ShellQuote(pkg)) {
		t.Fatalf("the package is inside the registry, the link leads to it: one bind each for the registry and the source: %s", words)
	}

	script := filepath.Join(work, "escape.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho x > \"$1/from-script\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	tries := []struct {
		name, cmd string
		allowed   bool
	}{
		{"a redirect into the registry", `echo x > ` + agents.ShellQuote(root+"/registry.json"), false},
		{"cd and a relative path", `cd ` + agents.ShellQuote(root) + ` && echo x > y`, false},
		{"a subshell into a package", `(echo x > ` + agents.ShellQuote(pkg+"/app.json") + `)`, false},
		{"the link to the registry", `echo x > ` + agents.ShellQuote(link+"/via-link"), false},
		{"a new folder in a package", `mkdir ` + agents.ShellQuote(pkg+"/skills"), false},
		{"a script it wrote", agents.ShellQuote(script) + ` ` + agents.ShellQuote(root), false},
		{"the author's source folder", `echo x > ` + agents.ShellQuote(source+"/manifest.json"), false},
		{"a rename out of the registry", `mv ` + agents.ShellQuote(pkg) + ` ` + agents.ShellQuote(work+"/stolen"), false},
		{"a rename of the registry's parent", `mv ` + agents.ShellQuote(data) + ` ` + agents.ShellQuote(data+"-moved"), false},
		{"a rename of the source's parent", `mv ` + agents.ShellQuote(filepath.Dir(source)) + ` ` + agents.ShellQuote(filepath.Dir(source)+"-moved"), false},
		{"removing the registry's parent", `rm -rf ` + agents.ShellQuote(data), false},
		{"the chat's own folder", `echo x > ./own.txt && cat own.txt`, true},
	}
	if py, err := exec.LookPath("python3"); err == nil {
		tries = append(tries, struct {
			name, cmd string
			allowed   bool
		}{"python", agents.ShellQuote(py) + ` -c 'open("` + root + `/py.json","w").write("x")'`, false})
	}
	for _, try := range tries {
		t.Run(try.name, func(t *testing.T) {
			// As the launch line has it: the wall's words, then the program.
			line := words + " /bin/sh -c " + agents.ShellQuote(try.cmd)
			cmd := exec.Command("/bin/sh", "-c", line)
			cmd.Dir = work
			out, err := cmd.CombinedOutput()
			t.Logf("%s → %v: %s", try.cmd, err, strings.TrimSpace(string(out)))
			if try.allowed {
				if err != nil || strings.TrimSpace(string(out)) != "x" {
					t.Fatalf("the chat's folder must stay writable: %v %s", err, out)
				}
				return
			}
			if err == nil {
				t.Fatal("the write went through")
			}
			text := strings.ToLower(string(out))
			if !strings.Contains(text, "read-only file system") && !strings.Contains(text, "permission denied") && !strings.Contains(text, "device or resource busy") {
				t.Fatalf("refused, but not by the kernel's read-only bind: %s", out)
			}
		})
	}
	for _, f := range []string{root + "/registry.json", root + "/y", pkg + "/app.json", root + "/via-link", pkg + "/skills", root + "/from-script", source + "/manifest.json", root + "/py.json"} {
		if _, err := os.Lstat(f); err == nil {
			t.Errorf("%s was made from inside the wall", f)
		}
	}
	if _, err := os.Stat(pkg); err != nil {
		t.Errorf("the package was moved out: %v", err)
	}
	// The runtime is outside: its writes go through.
	if err := os.WriteFile(filepath.Join(root, "registry.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("the runtime's own write: %v", err)
	}
}

// Another process of the person's, outside the wall, sees the machine as it is: its /proc/<pid>/root leads there. The
// kernel lets a process of the same user follow it unless a security module stops it (here AppArmor's profile of an
// unprivileged bwrap does). The test says which this machine is; it is a known gap (docs/apps.md), not a pass.
func TestBoundaryOtherProcessRoot(t *testing.T) {
	b := wallOrSkip(t)
	root := filepath.Join(t.TempDir(), "apps")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	target := fmt.Sprintf("/proc/%d/root%s/through-proc", os.Getpid(), root)
	line := b.Words([]string{root}) + " /bin/sh -c " + agents.ShellQuote("echo x > "+agents.ShellQuote(target))
	out, err := exec.Command("/bin/sh", "-c", line).CombinedOutput()
	if _, serr := os.Stat(filepath.Join(root, "through-proc")); serr == nil {
		t.Logf("GAP on this machine: a write through /proc/<pid>/root of an outside process went through (%v)", err)
		return
	}
	t.Logf("this machine refuses /proc/<pid>/root of an outside process: %s", strings.TrimSpace(string(out)))
}

func TestBoundaryProbeAndLaunch(t *testing.T) {
	data := t.TempDir()
	reg := &Registry{Root: filepath.Join(data, "apps")}
	pkg := filepath.Join(t.TempDir(), "review")
	if err := Scaffold(pkg, "acme.review", "Ревью", "chat"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reg.Install(pkg, InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(t.TempDir(), "hooks.json")
	if err := os.WriteFile(base, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// No bwrap: the chat starts without the wall and the state says why.
	none := &Boundary{Helper: []string{"/rt", "sandbox-exec"}}
	got, err := (&Launcher{Registry: reg, Boundary: none}).Claude([]string{"acme.review"}, base)
	if err != nil || got.Wrap != "" {
		t.Fatalf("%q %v", got.Wrap, err)
	}
	if probed, err := none.State(); !probed || !errors.Is(err, ErrNoBwrap) {
		t.Fatalf("%v %v", probed, err)
	}

	// A bwrap that starts but leaves the folder writable is no wall: the probe sees the file and removes it.
	leaky := &Boundary{Bwrap: "/usr/bin/bwrap", Helper: []string{"/rt", "sandbox-exec"}, Run: func(_ string, args ...string) ([]byte, []byte, error) {
		probe := args[len(args)-1]
		return []byte("writable\n"), nil, os.WriteFile(probe, nil, 0o600)
	}}
	if err := leaky.Ready(reg.Root); err == nil || !strings.Contains(err.Error(), "went through") {
		t.Fatalf("leaky: %v", err)
	}
	if _, err := os.Stat(filepath.Join(reg.Root, BoundaryProbeFile)); err == nil {
		t.Fatal("the probe's file stays")
	}

	// A wall that holds: the launch gets its words, the registry and the dev source read-only, the helper last; a
	// passed probe is not run again.
	runs := 0
	good := &Boundary{Bwrap: "/usr/bin/bwrap", Helper: []string{"/rt", "sandbox-exec"}, Run: func(string, ...string) ([]byte, []byte, error) {
		runs++
		return []byte("refused\n"), nil, nil
	}}
	l := &Launcher{Registry: reg, Boundary: good}
	for range 2 {
		if got, err = l.Claude([]string{"acme.review"}, base); err != nil {
			t.Fatal(err)
		}
	}
	realRoot, _ := filepath.EvalSymlinks(reg.Root)
	realPkg, _ := filepath.EvalSymlinks(pkg)
	want := "'/usr/bin/bwrap' '--dev-bind' '/' '/' "
	if !strings.HasPrefix(got.Wrap, want) || !strings.Contains(got.Wrap, "'--ro-bind' '"+realRoot+"' '"+realRoot+"'") ||
		!strings.Contains(got.Wrap, "'--ro-bind' '"+realPkg+"' '"+realPkg+"'") || !strings.HasSuffix(got.Wrap, "'--' '/rt' 'sandbox-exec' '--'") {
		t.Fatalf("wrap: %s", got.Wrap)
	}
	if runs != 1 {
		t.Fatalf("probes: %d", runs)
	}
}

// The person's session bus and user systemd would run a command outside the wall: inside it the bus socket is no
// socket, the user manager's folder is empty, and the bus address is not in the environment.
func TestBoundaryMasksTheSessionBus(t *testing.T) {
	b := wallOrSkip(t)
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	root := filepath.Join(t.TempDir(), "apps")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	check := `if [ -S "$1/bus" ]; then echo socket; fi; if [ -n "$(ls -A "$1/systemd" 2>/dev/null)" ]; then echo systemd; fi; echo "bus=$DBUS_SESSION_BUS_ADDRESS"`
	line := "DBUS_SESSION_BUS_ADDRESS=unix:path=/x " + b.Words([]string{root}) + " /bin/sh -c " + agents.ShellQuote(check) + " sh " + agents.ShellQuote(dir)
	out, err := exec.Command("/bin/sh", "-c", line).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "bus=" {
		t.Fatalf("the bus is reachable inside the wall: %q", got)
	}
}

// A folder the person owns but cannot write today can be made writable, and then its entries renamed: the folder under it
// is pinned all the same.
func TestPinnedAncestorsTakeOwnedFolders(t *testing.T) {
	top := t.TempDir()
	locked := filepath.Join(top, "locked")
	reg := filepath.Join(locked, "data", "apps")
	if err := os.MkdirAll(reg, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	got := pinnedAncestors([]string{reg})
	if !slices.Contains(got, filepath.Join(locked, "data")) {
		t.Fatalf("the folder under an owned read-only one is not pinned: %v", got)
	}
}

// The bus the environment names is covered too, not only the one in the runtime folder.
func TestBusMaskCoversTheNamedBus(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "other-bus")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+sock+",guid=x")
	got := busMask()
	if !slices.Contains(got, sock) {
		t.Fatalf("the named bus is not covered: %v", got)
	}
}

// A bus named through a link, or with its path escaped as D-Bus escapes it, is covered at the socket itself.
func TestBusMaskFollowsLinksAndEscapes(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "real bus")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	link := filepath.Join(dir, "link")
	if err := os.Symlink(sock, link); err != nil {
		t.Fatal(err)
	}
	for _, addr := range []string{"unix:path=" + link, "unix:path=" + strings.ReplaceAll(sock, " ", "%20")} {
		t.Setenv("DBUS_SESSION_BUS_ADDRESS", addr)
		if got := busMask(); !slices.Contains(got, sock) {
			t.Fatalf("%s: the socket is not covered: %v", addr, got)
		}
	}
}

// The person's credentials read as empty inside the wall; the runtime outside still reads them.
func TestBoundaryHidesThePersonsToken(t *testing.T) {
	b := wallOrSkip(t)
	data := t.TempDir()
	root := filepath.Join(data, "apps")
	token := filepath.Join(data, "cli-token")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(token, []byte("person-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	line := b.Words([]string{root}, token, filepath.Join(data, "missing")) + " /bin/sh -c " + agents.ShellQuote(`cat "$1"; echo end`) + " sh " + agents.ShellQuote(token)
	out, err := exec.Command("/bin/sh", "-c", line).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	// Inside, the file reads as empty or not at all (AppArmor's bwrap profile refuses /dev/null bound over a file).
	if got := string(out); strings.Contains(got, "person-token") || !strings.HasSuffix(strings.TrimSpace(got), "end") {
		t.Fatalf("the person's token is readable inside the wall: %q", got)
	}
	if raw, _ := os.ReadFile(token); string(raw) != "person-token\n" {
		t.Fatalf("the token outside changed: %q", raw)
	}
}

// The tmux server would run a window outside the wall: inside, its socket folder is empty and TMUX is unset.
func TestBoundaryKeepsTmuxOut(t *testing.T) {
	b := wallOrSkip(t)
	base := t.TempDir()
	dir := filepath.Join(base, fmt.Sprintf("tmux-%d", os.Getuid()))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "default")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	t.Setenv("TMUX_TMPDIR", base)
	t.Setenv("TMUX", sock+",1,0")
	root := filepath.Join(t.TempDir(), "apps")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	check := `if [ -S "$1" ]; then echo socket; fi; echo "tmux=$TMUX"`
	line := b.Words([]string{root}) + " /bin/sh -c " + agents.ShellQuote(check) + " sh " + agents.ShellQuote(sock)
	out, err := exec.Command("/bin/sh", "-c", line).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "tmux=" {
		t.Fatalf("tmux is reachable inside the wall: %q", got)
	}
}

// The tmux folder that is not there when the launch line is written is made, 0700, and masked: a server started later
// does not put its socket in reach of the agent.
func TestTmuxMaskMakesTheMissingFolder(t *testing.T) {
	base := t.TempDir()
	t.Setenv("TMUX_TMPDIR", base)
	t.Setenv("TMUX", "")
	dir := filepath.Join(base, fmt.Sprintf("tmux-%d", os.Getuid()))
	args := tmuxMask()
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("%v %v", info, err)
	}
	real, _ := filepath.EvalSymlinks(dir)
	if !slices.Contains(args, real) {
		t.Fatalf("not masked: %v", args)
	}
}

// A live wall with the data folder and the start-up files sealed, in a home folder of the test's.
func sealedLine(t *testing.T, b *Boundary, data, home, check string, args ...string) (string, error) {
	t.Helper()
	root := filepath.Join(data, "apps")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	line := b.SealedWords([]string{root}, &Seal{Data: data, Home: home}) + " /bin/sh -c " + agents.ShellQuote(check) + " sh"
	for _, a := range args {
		line += " " + agents.ShellQuote(a)
	}
	out, err := exec.Command("/bin/sh", "-c", line).CombinedOutput()
	return string(out), err
}

// Inside the wall the data folder holds what the agent's launch needs and nothing else: no way to read the person's
// session key or the safe, and no way to change what runs outside.
func TestSealShutsTheDataFolder(t *testing.T) {
	b := wallOrSkip(t)
	data, home := t.TempDir(), t.TempDir()
	for rel, body := range map[string]string{
		"config.json": "authSecret", "agent.json": "machine-token", "secrets/github": "safe-value", "tmux.conf": "run evil",
		"status-hook.sh": "#!/bin/sh\n", "port": "1234\n", "hooks.json": "{}", "workspaces/w/claude-prompt.md": "prompt", "workspaces/w/layout.json": "layout",
		"design/p/index.html": "site",
	} {
		p := filepath.Join(data, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	check := `d="$1"
for f in config.json agent.json secrets/github tmux.conf workspaces/w/layout.json; do [ -e "$d/$f" ] && echo "SEEN $f"; done
for f in port status-hook.sh hooks.json workspaces/w/claude-prompt.md; do [ -r "$d/$f" ] || echo "MISSING $f"; (echo x >> "$d/$f") 2>/dev/null && echo "WROTE $f"; done
(echo x >> "$d/design/p/index.html") 2>/dev/null || echo "DESIGN READ-ONLY"
(echo evil > "$d/tmux.conf") 2>/dev/null
mv "$d" "$d.moved" 2>/dev/null && echo MOVED
echo end`
	out, err := sealedLine(t, b, data, home, check, data)
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if strings.TrimSpace(out) != "end" {
		t.Fatalf("the seal leaks: %s", out)
	}
	if raw, _ := os.ReadFile(filepath.Join(data, "tmux.conf")); string(raw) != "run evil" {
		t.Fatalf("a file of the data folder changed from inside: %q", raw)
	}
}

// A start-up file the agent writes runs outside the wall later, and so does one it creates where there was none.
func TestSealKeepsStartupFilesReadOnly(t *testing.T) {
	b := wallOrSkip(t)
	data, home := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".profile"), []byte("# theirs\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "known_hosts"), []byte("h\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	check := `h="$1"
for f in .bashrc .zshrc .zshenv .profile .bash_profile .gitconfig .tmux.conf .ssh/authorized_keys .ssh/rc .config/systemd/user/x.service .claude/settings.json .claude/hooks/x .codex/config.toml; do
  (echo evil >> "$h/$f") 2>/dev/null && echo "WROTE $f"
done
(echo host >> "$h/.ssh/known_hosts") 2>/dev/null || echo "KNOWN_HOSTS READ-ONLY"
(mkdir -p "$h/projects" && echo x > "$h/projects/ok") 2>/dev/null || echo "HOME SHUT"
echo end`
	out, err := sealedLine(t, b, data, home, check, home)
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if strings.TrimSpace(out) != "end" {
		t.Fatalf("the seal leaks: %s", out)
	}
	raw, _ := os.ReadFile(filepath.Join(home, ".bash_profile"))
	if !strings.Contains(string(raw), ".profile") || strings.Contains(string(raw), "evil") {
		t.Fatalf("a login bash no longer reads the file it read: %q", raw)
	}
	if raw, _ := os.ReadFile(filepath.Join(home, ".claude", "settings.json")); strings.TrimSpace(string(raw)) != "{}" {
		t.Fatalf("settings: %q", raw)
	}
}

// sandbox-exec's Landlock domain and seccomp filter: no way to a process outside, by /proc or a signal, and no typing into
// the terminal.
func TestHardenCutsTheAgentOffFromOutside(t *testing.T) {
	b := wallOrSkip(t)
	if landlockABI() < 1 {
		t.Skip("the kernel has Landlock off")
	}
	outside := exec.Command("sleep", "30")
	if err := outside.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = outside.Process.Kill(); _ = outside.Wait() }()
	root := t.TempDir()
	pid := strconv.Itoa(outside.Process.Pid)
	check := `p="$1"
ls "/proc/$p/root/" >/dev/null 2>&1 && echo "PROC ROOT"
cat "/proc/$p/environ" >/dev/null 2>&1 && echo "PROC ENVIRON"
sleep 5 & inner=$!
kill -0 "$inner" 2>/dev/null || echo "OWN CHILD SIGNAL REFUSED"
kill "$inner" 2>/dev/null
echo end`
	if landlockABI() >= landlockScopesABI {
		check = `kill -0 "$1" 2>/dev/null && echo "SIGNAL OUT"` + "\n" + check
	}
	line := b.Words([]string{root}) + " /bin/sh -c " + agents.ShellQuote(check) + " sh " + pid
	out, err := exec.Command("/bin/sh", "-c", line).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if strings.TrimSpace(string(out)) != "end" {
		t.Fatalf("the agent reaches outside: %s", out)
	}
}

func TestSSHConfigMask(t *testing.T) {
	dir := t.TempDir()
	if got := sshConfigMask(dir); !slices.Equal(got, []string{"--tmpfs", dir}) {
		t.Fatalf("got %v", got)
	}
	if got := sshConfigMask(filepath.Join(dir, "none")); got != nil {
		t.Fatalf("a missing folder: %v", got)
	}
}

// HRN-621. A launch line waiting in its file runs outside the wall: an agent must not reach it.
func TestSealHidesWaitingLaunchLines(t *testing.T) {
	if slices.Contains(dataReadonly, "launch-lines") || slices.Contains(dataWritable, "launch-lines") {
		t.Fatal("launch-lines is on the seal's list")
	}
}
