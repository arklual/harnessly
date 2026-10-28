package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/preflight"
	"github.com/ly-solutions/harnessly/internal/selfupdate"
	"github.com/ly-solutions/harnessly/internal/voice/engine"
)

// Settings → General → «Проверка машины»: what may quietly not work on this machine, and how to put it right.
//
//	GET  <platform>/machine/checks       {checks: [{id, ok, title, detail, fix_command, fixable}]}
//	POST <platform>/machine/checks/fix   {id}: the fix of that check, then the checks again
//
// Every check and every fix is in the table below; the page names a check by its id and nothing else, so it can make
// the runtime run no command of its own. A fix the button may do needs no root and touches only the account's own
// files: a line in the shell's profile that puts ~/.local/bin on PATH, and the hub's update of this machine's programs.
// The rest is a command for the person to run (fix_command). The only addresses asked are the public download hosts
// of the voice model and server, and only when the model is still to be downloaded.

type machineCheck struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Title string `json:"title"`
	// Detail is what was seen (a version, a path, the error): the page's own text says what the check is about.
	Detail     string `json:"detail"`
	FixCommand string `json:"fix_command"`
	// Fixable: POST …/fix with this id does it.
	Fixable bool `json:"fixable"`
}

// machineProbe is what the checks look at; a test puts its own in.
type machineProbe struct {
	GOOS    string
	Home    string
	DataDir string
	// BinDir is the folder of the runtime's binary.
	BinDir string
	Tools  func() preflight.Result
	// ShellPath is the PATH of the person's login shell.
	ShellPath func() string
	// Shell is the person's login shell.
	Shell string
	// HasDecoder: ffmpeg is there.
	HasDecoder func() bool
	// Voice is the runtime's own recognition when it is the one this machine uses or may use; nil otherwise.
	Voice func() *engine.Engine
	// Update keeps a hub machine's programs; nil for another runtime.
	Update *selfupdate.Updater
	// Free is the room left on the disk of a path, in bytes.
	Free func(path string) (uint64, bool)
	// Reach is nil when an address answers.
	Reach func(ctx context.Context, url string) error
	// GitEmail is the account's git user.email.
	GitEmail func() string
	// LookPath finds a program on the runtime's PATH.
	LookPath func(string) (string, error)
	// Exists says a file is there.
	Exists func(string) bool
	User   string
	// Invalidate drops what the tool check keeps, after a fix.
	Invalidate func()
}

// pkgInstall is the command that installs a system package here.
func (p machineProbe) pkgInstall(pkg string) string {
	if p.GOOS == "darwin" {
		return "brew install " + pkg
	}
	for _, m := range []struct{ bin, cmd string }{
		{"apt-get", "sudo apt-get install -y "}, {"dnf", "sudo dnf install -y "}, {"yum", "sudo yum install -y "},
		{"pacman", "sudo pacman -S --needed "}, {"zypper", "sudo zypper install -y "}, {"apk", "sudo apk add "},
	} {
		if _, err := p.LookPath(m.bin); err == nil {
			return m.cmd + pkg
		}
	}
	return "sudo apt-get install -y " + pkg
}

// localBin is ~/.local/bin, where install.sh puts the programs of an install of the person's own account and where
// Claude Code's installer puts claude.
func (p machineProbe) localBin() string { return filepath.Join(p.Home, ".local", "bin") }

// onPath says a folder is on a PATH.
func onPath(dir, path string) bool {
	for _, d := range filepath.SplitList(path) {
		if filepath.Clean(d) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}

// profileFile is the start file of the person's shell that a PATH line goes into.
func (p machineProbe) profileFile() (file, line string) {
	switch filepath.Base(p.Shell) {
	case "zsh":
		return filepath.Join(p.Home, ".zshrc"), `export PATH="$HOME/.local/bin:$PATH"`
	case "bash":
		return filepath.Join(p.Home, ".bashrc"), `export PATH="$HOME/.local/bin:$PATH"`
	case "fish":
		return filepath.Join(p.Home, ".config", "fish", "conf.d", "harnessly-path.fish"), "fish_add_path -g $HOME/.local/bin"
	default:
		return filepath.Join(p.Home, ".profile"), `export PATH="$HOME/.local/bin:$PATH"`
	}
}

// pathFixCommand is the shell command of the PATH fix: one line appended to the shell's start file.
func (p machineProbe) pathFixCommand() string {
	file, line := p.profileFile()
	rel := "~/" + strings.TrimPrefix(file, p.Home+"/")
	if filepath.Base(p.Shell) == "fish" {
		return "fish_add_path -U $HOME/.local/bin"
	}
	return fmt.Sprintf("echo '%s' >> %s", line, rel)
}

// fixPath appends the PATH line to the shell's start file, unless the file names ~/.local/bin already.
func (p machineProbe) fixPath() error {
	file, line := p.profileFile()
	if raw, err := os.ReadFile(file); err == nil && strings.Contains(string(raw), ".local/bin") {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(f, "\n# harnessly: programs in ~/.local/bin (harnessly, claude)\n%s\n", line)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// fullProfile: install.sh installed this machine into the person's own account (services of the account).
func (p machineProbe) fullProfile() bool {
	return p.Exists(filepath.Join(p.Home, ".harnessly", "install-manifest"))
}

const (
	gib = 1 << 30
	mib = 1 << 20
)

func (p machineProbe) run(ctx context.Context) []machineCheck {
	var (
		tools   preflight.Result
		reached = map[string]error{}
		email   string
		wg      sync.WaitGroup
		mu      sync.Mutex
	)
	var eng *engine.Engine
	if p.Voice != nil {
		eng = p.Voice()
	}
	var needs engine.Needs
	var eligible error
	if eng != nil {
		eligible = eng.Eligible()
		needs = eng.Needs()
	}
	probe := func(url string) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := p.Reach(ctx, url)
			mu.Lock()
			reached[url] = err
			mu.Unlock()
		}()
	}
	if eng != nil && eligible == nil && !needs.Offline {
		if needs.Model != nil {
			probe(eng.ModelURL(*needs.Model))
		}
		if needs.Binary {
			probe(eng.BinaryURL())
		}
	}
	wg.Add(2)
	go func() { defer wg.Done(); tools = p.Tools() }()
	go func() { defer wg.Done(); email = p.GitEmail() }()
	wg.Wait()

	var out []machineCheck
	add := func(c machineCheck) { out = append(out, c) }

	// The agents' terminal and their work.
	tmux := machineCheck{ID: "tmux", OK: tools.Tmux.Installed && tools.Tmux.Compatible, Title: "tmux 2.9+"}
	if !tmux.OK {
		tmux.FixCommand = p.pkgInstall("tmux")
		if tools.Tmux.Installed && tools.Tmux.Version != nil {
			tmux.Detail = "tmux " + *tools.Tmux.Version
		}
	}
	add(tmux)
	gitOK := tools.Git.Installed
	git := machineCheck{ID: "git", OK: gitOK, Title: "git"}
	if !gitOK {
		git.FixCommand = p.pkgInstall("git")
	}
	add(git)
	agentCLI := machineCheck{ID: "agent_cli", OK: tools.Claude.Installed || tools.Codex.Installed, Title: "Claude Code / Codex"}
	if !agentCLI.OK {
		agentCLI.FixCommand = "curl -fsSL https://claude.ai/install.sh | bash"
		agentCLI.Detail = "npm install -g @openai/codex"
	}
	add(agentCLI)
	if gitOK {
		identity := machineCheck{ID: "git_identity", OK: email != "", Title: "git user.email"}
		if !identity.OK {
			identity.FixCommand = `git config --global user.name "Your Name" && git config --global user.email you@example.com`
		}
		add(identity)
	}

	// ~/.local/bin on the PATH of the person's shell: the harnessly command and claude are there.
	if dir := p.localBin(); p.Exists(filepath.Join(dir, "harnessly")) || p.Exists(filepath.Join(dir, "claude")) || p.BinDir == dir {
		path := machineCheck{ID: "local_bin_path", OK: onPath(dir, p.ShellPath()), Title: "~/.local/bin in PATH"}
		if !path.OK {
			path.FixCommand, path.Fixable = p.pathFixCommand(), true
			path.Detail, _ = p.profileFile()
		}
		add(path)
	}

	// The harnessly command beside the runtime (an install by an older hub has none).
	if p.Update != nil {
		cli := machineCheck{ID: "cli", OK: p.Exists(filepath.Join(p.BinDir, "harnessly")), Title: "harnessly"}
		if !cli.OK {
			cli.FixCommand = p.Update.Command()
			cli.Fixable = p.Update.Status().Mode == "auto"
		}
		add(cli)
	}

	// The services outlive the person's logout only with lingering (an install of the person's own account on Linux).
	if p.GOOS == "linux" && p.fullProfile() && p.User != "" {
		linger := machineCheck{ID: "linger", OK: p.Exists(filepath.Join("/var/lib/systemd/linger", p.User)), Title: "loginctl enable-linger"}
		if !linger.OK {
			linger.FixCommand = "sudo loginctl enable-linger " + p.User
		}
		add(linger)
	}

	// Voice input.
	ffmpeg := machineCheck{ID: "ffmpeg", OK: p.HasDecoder(), Title: "ffmpeg"}
	if !ffmpeg.OK {
		ffmpeg.FixCommand = p.pkgInstall("ffmpeg")
	}
	add(ffmpeg)
	if eng != nil && !eng.UsesRemote() {
		builtin := machineCheck{ID: "voice_engine", OK: eligible == nil, Title: "whisper-server"}
		if eligible != nil {
			builtin.Detail = eligible.Error()
			if p.GOOS == "darwin" {
				builtin.FixCommand = "brew install whisper-cpp"
			}
		}
		add(builtin)
		if eligible == nil {
			download := machineCheck{ID: "voice_download", OK: true, Title: "huggingface.co, github.com"}
			var failed []string
			for url, err := range reached {
				if err != nil {
					failed = append(failed, hostOf(url)+": "+err.Error())
				}
			}
			switch {
			case needs.Offline && (needs.Model != nil || needs.Binary):
				download.OK = false
				download.Detail = "RUNTIME_VOICE_DOWNLOAD=off"
			case len(failed) > 0:
				download.OK = false
				download.Detail = strings.Join(failed, "; ")
			}
			if !download.OK && needs.Model != nil {
				download.FixCommand = fmt.Sprintf("mkdir -p %s && curl -fL -o %s %s", filepath.Dir(needs.ModelPath),
					needs.ModelPath, eng.ModelURL(*needs.Model))
			}
			add(download)
		}
	}

	// Room on the disk of the data folder: 1 GiB, and the model that is still to come.
	if free, ok := p.Free(p.DataDir); ok {
		want := uint64(gib)
		if eng != nil && needs.Model != nil {
			want += uint64(needs.Model.Size)
		}
		disk := machineCheck{ID: "disk", OK: free >= want, Title: p.DataDir}
		if !disk.OK {
			disk.Detail = fmt.Sprintf("%d MB / %d MB", free/mib, want/mib)
			disk.FixCommand = "du -sh " + filepath.Join(p.DataDir, "*") + " | sort -h"
		}
		add(disk)
	}
	return out
}

// fix runs the fix of a check; only the checks marked fixable have one.
func (p machineProbe) fix(ctx context.Context, id string) error {
	switch id {
	case "local_bin_path":
		return p.fixPath()
	case "cli":
		if p.Update == nil || p.Update.Status().Mode != "auto" {
			return errNoFix
		}
		err := p.Update.Apply(ctx)
		if errors.Is(err, selfupdate.ErrBusy) {
			return nil
		}
		return err
	}
	return errNoFix
}

var errNoFix = errors.New("this check has no fix the runtime may make")

// --- the machine as it is ------------------------------------------------------------------------------------------

// reachTimeout bounds one look at a download host.
const reachTimeout = 6 * time.Second

// reachCache keeps a host's answer for ten minutes: the page asks often, the hosts are not asked as often.
var reachCache = struct {
	sync.Mutex
	m map[string]reachAnswer
}{m: map[string]reachAnswer{}}

type reachAnswer struct {
	err error
	at  time.Time
}

// reach asks an address for its headers (a redirect is followed, nothing is downloaded).
func reach(ctx context.Context, url string) error {
	reachCache.Lock()
	if a, ok := reachCache.m[url]; ok && time.Since(a.at) < 10*time.Minute {
		reachCache.Unlock()
		return a.err
	}
	reachCache.Unlock()
	ctx, cancel := context.WithTimeout(ctx, reachTimeout)
	defer cancel()
	err := func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			var uerr interface{ Unwrap() error }
			if errors.As(err, &uerr) && uerr.Unwrap() != nil {
				return uerr.Unwrap()
			}
			return err
		}
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			return errors.New(resp.Status)
		}
		return nil
	}()
	reachCache.Lock()
	reachCache.m[url] = reachAnswer{err, time.Now()}
	reachCache.Unlock()
	return err
}

func freeBytes(path string) (uint64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, false
	}
	return uint64(st.Bavail) * uint64(st.Bsize), true
}

func pathExists(p string) bool { _, err := os.Stat(p); return err == nil }

// gitEmail is git's user.email for the account, "" when it has none.
func gitEmail(path string) func() string {
	return func() string {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", "config", "--global", "--get", "user.email")
		cmd.Env = append(os.Environ(), "PATH="+path)
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
}

// ownVoice is the runtime's recognition engine where this machine has no service of the operator's.
func ownVoice(dataDir string) func() *engine.Engine {
	return func() *engine.Engine {
		if externalVoice() {
			return nil
		}
		return builtinVoice(dataDir).eng
	}
}

func newMachineProbe(d deps, self *selfupdate.Updater) machineProbe {
	tools := d.Tools
	if tools == nil {
		tools = preflight.New(d.Home)
	}
	binDir := ""
	if exe, err := os.Executable(); err == nil {
		if p, err := filepath.EvalSymlinks(exe); err == nil {
			exe = p
		}
		binDir = filepath.Dir(exe)
	}
	name := ""
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	shell := tools.LoginShell()
	dictation := newVoice(d.DataDir)
	return machineProbe{
		GOOS: runtime.GOOS, Home: d.Home, DataDir: d.DataDir, BinDir: binDir, User: name, Shell: shell,
		Tools: tools.Runtime, ShellPath: tools.ShellPath, HasDecoder: dictation.HasDecoder, Voice: ownVoice(d.DataDir),
		Update: self, Free: freeBytes, Reach: reach, GitEmail: func() string { return gitEmail(tools.ShellPath())() },
		LookPath: exec.LookPath, Exists: pathExists, Invalidate: tools.Invalidate,
	}
}

func machineCheckRoutes(d deps, self *selfupdate.Updater) []gate.Route {
	probe := newMachineProbe(d, self)
	answer := func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		peerJSON(w, http.StatusOK, map[string]any{"checks": probe.run(ctx)})
	}
	return []gate.Route{
		{
			Match: func(r *http.Request) bool { return pathIs(r, platformAPI+"/machine/checks") },
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					w.Header().Set("Allow", "GET")
					peerJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
					return
				}
				if !d.SessionOK(r) {
					peerJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
					return
				}
				answer(w, r)
			}),
		},
		{
			Match: func(r *http.Request) bool { return pathIs(r, platformAPI+"/machine/checks/fix") },
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					w.Header().Set("Allow", "POST")
					peerJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
					return
				}
				if !d.SessionOK(r) {
					peerJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
					return
				}
				var body struct {
					ID string `json:"id"`
				}
				raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<10))
				if err == nil {
					err = json.Unmarshal(raw, &body)
				}
				if err != nil {
					peerJSON(w, http.StatusBadRequest, map[string]string{"error": "id"})
					return
				}
				ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Minute)
				defer cancel()
				if err := probe.fix(ctx, body.ID); err != nil {
					status := http.StatusInternalServerError
					if errors.Is(err, errNoFix) {
						status = http.StatusBadRequest
					}
					peerJSON(w, status, map[string]string{"error": err.Error()})
					return
				}
				probe.Invalidate()
				answer(w, r)
			}),
		},
	}
}
