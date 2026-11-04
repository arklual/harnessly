// Package provision finds and installs a verified browser without weakening its sandbox.
package provision

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// Progress describes preparation independently of a profile's connection state.
type Progress struct {
	Percent int
	Hint    string
}

// Config captures environment settings once so concurrent profiles use the same installation.
type Config struct {
	DataDir, Chrome, URL, GOOS, GOARCH string
	archive                            *archive
	Download                           bool
	LookPath                           func(string) (string, error)
}

// FromEnv keeps download policy consistent for the lifetime of a manager.
func FromEnv(data string) Config {
	return Config{DataDir: data, Chrome: os.Getenv("RUNTIME_BROWSER_CHROME"), URL: os.Getenv("RUNTIME_BROWSER_CHROME_URL"),
		Download: !strings.EqualFold(strings.TrimSpace(os.Getenv("RUNTIME_BROWSER_DOWNLOAD")), "off"),
		GOOS:     runtime.GOOS, GOARCH: runtime.GOARCH, LookPath: exec.LookPath}
}

func executable(path string) bool {
	s, err := os.Stat(path)
	return err == nil && s.Mode().IsRegular() && s.Mode().Perm()&0111 != 0
}

func isSnap(path string) bool {
	resolved, _ := filepath.EvalSymlinks(path)
	if strings.HasPrefix(path, "/snap/") || strings.HasPrefix(resolved, "/snap/") || resolved == "/usr/bin/snap" {
		return true
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	header, _ := io.ReadAll(io.LimitReader(f, 4096))
	return bytesHasSnapWrapper(string(header))
}

func bytesHasSnapWrapper(header string) bool {
	return strings.HasPrefix(header, "#!") && (strings.Contains(header, "/snap/bin/chromium") ||
		strings.Contains(header, "exec snap run chromium"))
}

// Find never downloads or starts Chrome. Snap candidates are returned separately for a pipe probe.
func Find(cfg Config) (path, snap string, err error) {
	if cfg.Chrome != "" {
		p, e := cfg.LookPath(cfg.Chrome)
		if e != nil {
			return "", "", fmt.Errorf("RUNTIME_BROWSER_CHROME: %w", e)
		}
		return p, "", nil
	}
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if p, e := cfg.LookPath(name); e == nil {
			if isSnap(p) {
				snap = p
			} else {
				return p, snap, nil
			}
		}
	}
	if cfg.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		for _, root := range []string{"/Applications", filepath.Join(home, "Applications")} {
			for _, app := range []string{"Google Chrome", "Chromium"} {
				p := filepath.Join(root, app+".app", "Contents", "MacOS", app)
				if executable(p) {
					return p, snap, nil
				}
			}
		}
	}
	if cfg.DataDir != "" {
		if a, e := platform(cfg); e == nil {
			p := filepath.Join(cfg.DataDir, "agent-browser", "chrome", Version, a.binary)
			if executable(p) {
				return p, snap, nil
			}
		}
	}
	if snap == "" {
		if p, e := cfg.LookPath("/snap/bin/chromium"); e == nil {
			snap = p
		}
	}
	return "", snap, nil
}

// DownloadSupport lets machine checks explain unsupported or offline installs without starting a download.
func DownloadSupport(cfg Config) error {
	message := ""
	if !cfg.Download {
		message = "Chrome not found and RUNTIME_BROWSER_DOWNLOAD=off"
	} else if _, err := platform(cfg); err != nil {
		message = err.Error()
	}
	if message != "" {
		return &Diagnostic{Message: message, Command: installCommand(osRelease(), cfg.GOARCH)}
	}
	return nil
}

// Ensure serializes installations across profiles and runtime processes. Interrupted transfers restart from zero.
func Ensure(ctx context.Context, cfg Config, progress func(Progress)) (string, error) {
	if progress == nil {
		progress = func(Progress) {}
	}
	path, snap, err := Find(cfg)
	if err != nil || path != "" {
		return path, err
	}
	a, platformErr := platform(cfg)
	if !cfg.Download || platformErr != nil {
		if snap != "" {
			progress(Progress{0, "Checking snap Chromium with the private debugging pipe"})
			if _, err = Probe(ctx, snap, cfg.DataDir); err == nil {
				return snap, nil
			}
		}
		if platformErr != nil {
			return "", platformErr
		}
		return "", fmt.Errorf("browser download disabled by RUNTIME_BROWSER_DOWNLOAD=off; "+
			"install system Chrome/Chromium or set RUNTIME_BROWSER_CHROME (snap probe: %v)", err)
	}
	root := filepath.Join(cfg.DataDir, "agent-browser", "chrome")
	if err = os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	lock, err := os.OpenFile(filepath.Join(root, "download.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	progress(Progress{0, "Preparing Chrome; waiting for the shared download lock"})
	for {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return "", err
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	path = filepath.Join(root, Version, a.binary)
	if executable(path) {
		return path, nil
	}
	stale, _ := filepath.Glob(filepath.Join(root, ".download-*"))
	for _, p := range stale {
		if err = os.RemoveAll(p); err != nil {
			return "", err
		}
	}
	if err = download(ctx, cfg, a, root, progress); err != nil {
		// Snap is a last resort because confinement may reject our data directory or pipe descriptors.
		if snap != "" {
			if _, probeErr := Probe(ctx, snap, cfg.DataDir); probeErr == nil {
				return snap, nil
			}
		}
		return "", err
	}
	return path, nil
}
