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

	"github.com/ly-solutions/harnessly/internal/cdp"
)

const LogLimit = 4 << 20

type Config struct{ ProfileDir, Socket, Chrome, Mode string }

// SocketPath keeps room for the terminating NUL on both Linux and Darwin.
func SocketPath(runDir, profile string) string {
	p := filepath.Join(runDir, profile+".sock")
	if len(p) < 100 {
		return p
	}
	sum := sha256.Sum256([]byte(p))
	return filepath.Join("/tmp", fmt.Sprintf("agent-browser-%d-%x", os.Getuid(), sum[:12]), "host.sock")
}
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

func Main(args []string) error {
	fs := flag.NewFlagSet("browser-host", flag.ContinueOnError)
	var cfg Config
	fs.StringVar(&cfg.ProfileDir, "profile", "", "profile directory")
	fs.StringVar(&cfg.Socket, "socket", "", "private relay socket")
	fs.StringVar(&cfg.Chrome, "chrome", "", "Chrome executable")
	fs.StringVar(&cfg.Mode, "mode", "auto", "auto, xvfb or headless")
	if err := fs.Parse(args); err != nil {
		return err
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
	cmd := exec.Command(executable, "browser-host", "--profile", cfg.ProfileDir, "--socket", cfg.Socket, "--chrome", cfg.Chrome, "--mode", cfg.Mode)
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

func Run(ctx context.Context, cfg Config) error {
	if cfg.ProfileDir == "" || cfg.Socket == "" || cfg.Chrome == "" {
		return errors.New("browser-host requires profile, socket and chrome")
	}
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
		x := exec.Command(xvfb, "-displayfd", "3", "-screen", "0", "3840x2160x24", "-nolisten", "tcp", "-terminate")
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
	args := []string{"--remote-debugging-pipe", "--user-data-dir=" + filepath.Join(cfg.ProfileDir, "chrome"), "--no-first-run", "--no-default-browser-check", "--password-store=basic", "--use-mock-keychain", "--disable-backgrounding-occluded-windows", "--disable-renderer-backgrounding", "--disable-background-timer-throttling", "--disable-dev-shm-usage"}
	if display == "" {
		args = append(args, "--headless=new")
	}
	args = append(args, strings.Fields(os.Getenv("RUNTIME_BROWSER_CHROME_ARGS"))...)
	args = append(args, "--no-startup-window")
	chrome := exec.Command(cfg.Chrome, args...)
	chrome.ExtraFiles = []*os.File{toChrome, fromChrome}
	chrome.Stdout = log
	chrome.Stderr = log
	chrome.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	chrome.Env = os.Environ()
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
	defer stream.Close()
	r := &relay{chrome: stream}
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
	go func() { readDone <- r.read(ctx) }()
	go func() {
		for {
			conn, e := listener.Accept()
			if e != nil {
				return
			}
			r.replace(conn)
		}
	}()
	select {
	case err = <-chromeDone:
		return err
	case <-readDone:
	case <-ctx.Done():
	}
	// Give Chrome a chance to flush persistent cookies before terminating its process group.
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	_ = stream.Write(closeCtx, []byte(`{"id":0,"method":"Browser.close"}`))
	cancel()
	select {
	case <-chromeDone:
	case <-time.After(3 * time.Second):
		_ = syscall.Kill(-chrome.Process.Pid, syscall.SIGKILL)
		<-chromeDone
	}
	return nil
}
