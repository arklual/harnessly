// Package host owns Chrome independently of the runtime's lifetime.
package host

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser/provision"
	"github.com/ly-solutions/harnessly/internal/cdp"
	"github.com/ly-solutions/harnessly/internal/egress"
)

// LogLimit caps the persistent host log in bytes.
const LogLimit = 4 << 20

// Config contains the paths and display mode for one profile host.
type Config struct {
	ProfileDir, Socket, Chrome, Mode string
	NoSandbox                        *bool
	// Idle closes the host once no runtime has been connected for this long (0: never). The runtime closes an idle
	// browser it is connected to itself; this is for a runtime that went away (an update, a crash) and left it behind.
	Idle time.Duration
	// MaxTabs bounds Chrome's renderer processes (--renderer-process-limit); 0 leaves Chrome's own bound.
	MaxTabs int
	// Nice lowers Chrome's and Xvfb's priority (0 keeps it).
	Nice int
	// DownloadLimit is the largest file a page may download into the profile's downloads/; 0 refuses downloads.
	DownloadLimit int64
}

// Build is the runtime's build (set by the runtime's main): a host answers it to Host.hello, and a runtime of another
// build replaces the host, so an update reaches the browser too.
var Build = "dev"

// BuildID is Build, with the executable's time for a dev build: every rebuild is another build.
var BuildID = sync.OnceValue(func() string {
	if Build != "dev" {
		return Build
	}
	if exe, err := os.Executable(); err == nil {
		if st, err := os.Stat(exe); err == nil {
			return Build + "+" + strconv.FormatInt(st.ModTime().UnixNano(), 36)
		}
	}
	return Build
})

// SocketPath keeps room for the terminating NUL on both Linux and Darwin.
func SocketPath(runDir, profile string) string {
	p := filepath.Join(runDir, profile+".sock")
	if len(p) < 100 {
		return p
	}
	sum := sha256.Sum256([]byte(p))
	return filepath.Join("/tmp", fmt.Sprintf("agent-browser-%d-%x", os.Getuid(), sum[:12]), "host.sock")
}

// PrivateDir creates an owner-only directory and rejects foreign ownership or symlinks.
func PrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	st, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("browser directory is not a directory")
	}
	if stat, ok := st.Sys().(*syscall.Stat_t); !ok || stat.Uid != uint32(os.Getuid()) {
		return errors.New("browser directory belongs to another user")
	}
	return os.Chmod(dir, 0700)
}

// Main parses the browser-host subcommand and handles termination signals.
func Main(args []string) error {
	fs := flag.NewFlagSet("browser-host", flag.ContinueOnError)
	var cfg Config
	fs.StringVar(&cfg.ProfileDir, "profile", "", "profile directory")
	fs.StringVar(&cfg.Socket, "socket", "", "private relay socket")
	fs.StringVar(&cfg.Chrome, "chrome", "", "Chrome executable")
	fs.StringVar(&cfg.Mode, "mode", "auto", "auto, xvfb or headless")
	sandbox := fs.String("sandbox", "inherit", "inherit, enabled or disabled")
	fs.DurationVar(&cfg.Idle, "idle", 0, "close after this long without a runtime (0: never)")
	fs.IntVar(&cfg.MaxTabs, "max-tabs", 0, "renderer process limit (0: Chrome's own)")
	fs.IntVar(&cfg.Nice, "nice", 0, "priority of Chrome and Xvfb")
	fs.Int64Var(&cfg.DownloadLimit, "download-limit", DefaultDownloadLimit, "largest download in bytes (0: none)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	switch *sandbox {
	case "enabled", "disabled":
		value := *sandbox == "disabled"
		cfg.NoSandbox = &value
	case "inherit":
	default:
		return errors.New("invalid sandbox preference")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	return Run(ctx, cfg)
}

// Start gives the host its own session; the runtime must only ever close its client socket.
func Start(executable string, cfg Config) error {
	if err := PrivateDir(cfg.ProfileDir); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(cfg.ProfileDir, "host.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if st, e := f.Stat(); e == nil && st.Size() > LogLimit {
		_ = f.Truncate(0)
	}
	cmd := exec.Command(executable, "browser-host",
		"--profile", cfg.ProfileDir, "--socket", cfg.Socket, "--chrome", cfg.Chrome, "--mode", cfg.Mode,
	)
	cmd.Args = append(cmd.Args, "--idle", cfg.Idle.String(), "--max-tabs", strconv.Itoa(cfg.MaxTabs),
		"--nice", strconv.Itoa(cfg.Nice), "--download-limit", strconv.FormatInt(cfg.DownloadLimit, 10))
	if cfg.NoSandbox != nil {
		value := "enabled"
		if *cfg.NoSandbox {
			value = "disabled"
		}
		cmd.Args = append(cmd.Args, "--sandbox", value)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout = f
	cmd.Stderr = f
	if err = cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

type cappedLog struct {
	mu sync.Mutex
	f  *os.File
}

func (l *cappedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	original := len(p)
	if len(p) > LogLimit {
		p = p[len(p)-LogLimit:]
	}
	st, err := l.f.Stat()
	if err != nil {
		return 0, err
	}
	if st.Size()+int64(len(p)) > LogLimit {
		if err = l.f.Truncate(0); err != nil {
			return 0, err
		}
	}
	_, err = l.f.Write(p)
	if err != nil {
		return 0, err
	}
	return original, nil
}

type pipe struct {
	io.ReadCloser
	io.WriteCloser
}

func (p pipe) Close() error { _ = p.ReadCloser.Close(); return p.WriteCloser.Close() }

// Run owns a profile lock, relay, display and Chrome until termination.
func Run(ctx context.Context, cfg Config) error {
	if cfg.ProfileDir == "" || cfg.Socket == "" || cfg.Chrome == "" {
		return errors.New("browser-host requires profile, socket and chrome")
	}
	lease, err := provision.Retain(cfg.Chrome)
	if err != nil {
		return err
	}
	defer lease.Close()
	if cfg.Mode == "" {
		cfg.Mode = "auto"
	}
	if cfg.Mode != "auto" && cfg.Mode != "headless" && cfg.Mode != "xvfb" {
		return errors.New("invalid browser mode")
	}
	if err := PrivateDir(cfg.ProfileDir); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(cfg.ProfileDir, "host.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("browser host already running: %w", err)
	}
	if err = PrivateDir(filepath.Dir(cfg.Socket)); err != nil {
		return err
	}
	// The lock, not a possibly stale socket, is the authority on profile ownership.
	if err = os.Remove(cfg.Socket); err != nil && !os.IsNotExist(err) {
		return err
	}
	listener, err := net.Listen("unix", cfg.Socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err = os.Chmod(cfg.Socket, 0600); err != nil {
		return err
	}
	defer os.Remove(cfg.Socket)
	logFile, err := os.OpenFile(filepath.Join(cfg.ProfileDir, "host.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	log := &cappedLog{f: logFile}
	display := ""
	xvfb, findErr := exec.LookPath("Xvfb")
	if cfg.Mode == "xvfb" && findErr != nil {
		return findErr
	}
	if cfg.Mode == "xvfb" || cfg.Mode == "auto" && findErr == nil {
		rd, wr, e := os.Pipe()
		if e != nil {
			return e
		}
		defer rd.Close()
		// Exit when Chrome disconnects even if the host was killed before it could clean up.
		x := niced(cfg.Nice, xvfb, "-displayfd", "3", "-screen", "0", "3840x2160x24", "-nolisten", "tcp", "-terminate")
		x.ExtraFiles = []*os.File{wr}
		x.Stdout = log
		x.Stderr = log
		x.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		e = x.Start()
		_ = wr.Close()
		if e != nil {
			return e
		}
		_ = os.WriteFile(filepath.Join(cfg.ProfileDir, "xvfb.pid"), []byte(strconv.Itoa(x.Process.Pid)), 0600)
		defer func() { _ = syscall.Kill(-x.Process.Pid, syscall.SIGKILL); _ = x.Wait() }()
		ready := make(chan string, 1)
		go func() { line, _ := bufio.NewReader(rd).ReadString('\n'); ready <- strings.TrimSpace(line) }()
		select {
		case n := <-ready:
			if _, e = strconv.Atoi(n); e != nil {
				return errors.New("Xvfb did not provide a display")
			}
			display = ":" + n
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
			return errors.New("Xvfb startup timed out")
		}
	}
	toChrome, writeChrome, err := os.Pipe()
	if err != nil {
		return err
	}
	defer toChrome.Close()
	defer writeChrome.Close()
	readChrome, fromChrome, err := os.Pipe()
	if err != nil {
		return err
	}
	defer readChrome.Close()
	defer fromChrome.Close()
	// Every connection of the browser goes through the egress, held to the gate: no internal address, no port of the
	// machine but its dev servers. Prompt injection in a page gets no further than the page itself could.
	proxy, err := egress.Listen(newGate(true).resolve)
	if err != nil {
		return err
	}
	defer proxy.Close()
	args := []string{
		"--remote-debugging-pipe", "--user-data-dir=" + filepath.Join(cfg.ProfileDir, "chrome"),
		"--no-first-run", "--no-default-browser-check", "--password-store=basic", "--use-mock-keychain",
		"--disable-backgrounding-occluded-windows", "--disable-renderer-backgrounding",
		"--disable-background-timer-throttling", "--disable-dev-shm-usage",
	}
	args = append(args, egress.ChromeArgs(proxy.Addr())...)
	if cfg.MaxTabs > 0 {
		args = append(args, "--renderer-process-limit="+strconv.Itoa(cfg.MaxTabs))
	}
	if display == "" {
		args = append(args, "--headless=new")
	}
	args = append(args, provision.SandboxArgs(strings.Fields(os.Getenv("RUNTIME_BROWSER_CHROME_ARGS")), cfg.NoSandbox)...)
	args = append(args, "--no-startup-window")
	chrome := niced(cfg.Nice, cfg.Chrome, args...)
	chrome.ExtraFiles = []*os.File{toChrome, fromChrome}
	chrome.Stdout = log
	chrome.Stderr = log
	chrome.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	chrome.Env = os.Environ()
	// Chrome puts SingletonSocket beneath TMPDIR, with its own generated suffix.
	// A short relay address alone cannot protect startup from a long caller TMPDIR.
	if len(os.TempDir()) > 50 {
		tempDir, e := os.MkdirTemp("/tmp", "agent-browser-")
		if e != nil {
			return e
		}
		defer os.RemoveAll(tempDir)
		chrome.Env = append(chrome.Env, "TMPDIR="+tempDir)
	}
	if display != "" {
		chrome.Env = append(chrome.Env, "DISPLAY="+display)
	}
	if err = chrome.Start(); err != nil {
		return err
	}
	_ = toChrome.Close()
	_ = fromChrome.Close()
	defer func() { _ = syscall.Kill(-chrome.Process.Pid, syscall.SIGKILL) }()
	// Diagnostic PIDs also let an operator stop an orphan after an uncatchable host death.
	_ = os.WriteFile(filepath.Join(cfg.ProfileDir, "host.pid"), []byte(strconv.Itoa(os.Getpid())), 0600)
	_ = os.WriteFile(filepath.Join(cfg.ProfileDir, "chrome.pid"), []byte(strconv.Itoa(chrome.Process.Pid)), 0600)
	chromeDone := make(chan error, 1)
	go func() { chromeDone <- chrome.Wait() }()
	stream := cdp.NewStream(pipe{readChrome, writeChrome})
	defer func() { _ = stream.Close() }()
	r := &relay{chrome: stream, build: BuildID(), alone: time.Now()}
	keeper := &sessionKeeper{dir: cfg.ProfileDir, relay: r}
	loads := &downloads{dir: filepath.Join(cfg.ProfileDir, "downloads"), limit: cfg.DownloadLimit, relay: r}
	r.event = loads.event
	r.beforeClose = func() {
		saveCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		keeper.save(saveCtx)
	}
	defer func() {
		_ = listener.Close()
		r.mu.Lock()
		r.closed = true
		if r.current != nil {
			r.current.close()
		}
		r.mu.Unlock()
	}()
	readDone := make(chan error, 1)
	// The reader outlives ctx: the cookies are read from the browser after the signal to stop.
	go func() { readDone <- r.read(context.Background()) }()
	setupCtx, setupCancel := context.WithTimeout(ctx, 10*time.Second)
	err = loads.setup(setupCtx)
	if err == nil {
		err = keeper.restore(setupCtx)
	}
	setupCancel()
	if err != nil {
		return fmt.Errorf("browser setup: %w", err)
	}
	idle := make(chan struct{})
	if cfg.Idle > 0 {
		go func() {
			tick := time.NewTicker(min(cfg.Idle/4+time.Second, 30*time.Second))
			defer tick.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case now := <-tick.C:
					if r.idle(now) >= cfg.Idle {
						close(idle)
						return
					}
				}
			}
		}()
	}
	go func() {
		for {
			conn, e := listener.Accept()
			if e != nil {
				return
			}
			r.replace(conn)
		}
	}()
	var relayErr error
	stopping := false
	select {
	case err = <-chromeDone:
		return err
	case relayErr = <-readDone:
	case <-ctx.Done():
		stopping = true
	case <-idle:
		stopping = true
	}
	if stopping {
		r.beforeClose()
	}
	// Give Chrome a chance to flush persistent cookies before terminating its process group.
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	_ = stream.Write(closeCtx, []byte(`{"id":0,"method":"Browser.close"}`))
	cancel()
	select {
	case err = <-chromeDone:
	case <-time.After(3 * time.Second):
		_ = syscall.Kill(-chrome.Process.Pid, syscall.SIGKILL)
		err = <-chromeDone
	}
	if stopping {
		return nil
	}
	return errors.Join(err, relayErr)
}

// niced runs name at a lower priority through nice, when there is one and the priority is lowered at all.
func niced(priority int, name string, args ...string) *exec.Cmd {
	if priority > 0 {
		if nice, err := exec.LookPath("nice"); err == nil {
			// #nosec G204 -- the browser or Xvfb and the host's fixed flags.
			return exec.Command(nice, append([]string{"-n", strconv.Itoa(priority), name}, args...)...)
		}
	}
	// #nosec G204 -- the browser or Xvfb and the host's fixed flags.
	return exec.Command(name, args...)
}
