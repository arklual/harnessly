// Package agentbrowser owns persistent browser profiles and conversation tabs.
package agentbrowser

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser/host"
	"github.com/ly-solutions/harnessly/internal/cdp"
)

const DefaultProfile = "common"

var ErrOff = errors.New("agent browser is disabled")
var profileID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type Config struct {
	DataDir    string
	Executable string
	Chrome     string
	Mode       string
	CDP        string
	Off        bool
}

func ConfigFromEnv(data string) Config {
	return Config{DataDir: data, Chrome: os.Getenv("RUNTIME_BROWSER_CHROME"), Mode: os.Getenv("RUNTIME_BROWSER_MODE"), CDP: strings.TrimSpace(os.Getenv("RUNTIME_BROWSER_CDP")), Off: strings.EqualFold(strings.TrimSpace(os.Getenv("RUNTIME_BROWSER")), "off")}
}

// FindChrome is the replacement seam for a future managed Chrome installer.
func FindChrome() (string, error) {
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if p, e := exec.LookPath(name); e == nil {
			return p, nil
		}
	}
	for _, p := range []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Chromium.app/Contents/MacOS/Chromium"} {
		if st, e := os.Stat(p); e == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("agent browser needs Chrome or Chromium")
}

type Manager struct {
	cfg       Config
	mu        sync.Mutex
	instances map[string]*Instance
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

func New(cfg Config) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{cfg: cfg, instances: make(map[string]*Instance), ctx: ctx, cancel: cancel}
}
func (m *Manager) Enabled() bool { return !m.cfg.Off }

// Close disconnects clients but deliberately leaves hosts and browsers running.
func (m *Manager) Close() { m.mu.Lock(); m.cancel(); m.mu.Unlock(); m.wg.Wait() }
func (m *Manager) Instance(ctx context.Context, id string) (*Instance, error) {
	if m.cfg.Off {
		return nil, ErrOff
	}
	if !profileID.MatchString(id) {
		return nil, errors.New("invalid browser profile id")
	}
	m.mu.Lock()
	if m.ctx.Err() != nil {
		m.mu.Unlock()
		return nil, cdp.ErrClosed
	}
	i := m.instances[id]
	if i == nil {
		i = newInstance(m, id)
		m.instances[id] = i
		m.wg.Add(1)
		go func() { defer m.wg.Done(); i.run() }()
	}
	m.mu.Unlock()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := i.State()
		if state == "up" {
			i.mu.Lock()
			conn := i.conn
			i.mu.Unlock()
			select {
			case <-conn.Done():
			case <-m.ctx.Done():
				return nil, cdp.ErrClosed
			default:
				return i, nil
			}
		}
		if state == "error" {
			return nil, errors.New(err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-m.ctx.Done():
			return nil, cdp.ErrClosed
		case <-ticker.C:
		}
	}
}
func (i *Instance) connect(ctx context.Context) (*cdp.Conn, error) {
	cfg := i.manager.cfg
	if cfg.CDP != "" {
		return cdp.Dial(ctx, cfg.CDP, i.event)
	}
	socket := host.SocketPath(filepath.Join(cfg.DataDir, "agent-browser", "run"), i.profile.ID)
	dial := func() (net.Conn, error) { return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", socket) }
	if conn, err := dial(); err == nil {
		return cdp.New(cdp.NewStream(conn), i.event), nil
	}
	chrome := cfg.Chrome
	var err error
	if chrome == "" {
		chrome, err = FindChrome()
		if err != nil {
			return nil, err
		}
	}
	executable := cfg.Executable
	if executable == "" {
		executable, err = os.Executable()
		if err != nil {
			return nil, err
		}
	}
	mode := cfg.Mode
	if mode == "" {
		mode = "auto"
	}
	if err = host.Start(executable, host.Config{ProfileDir: i.dir, Socket: socket, Chrome: chrome, Mode: mode}); err != nil {
		return nil, err
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if conn, e := dial(); e == nil {
			return cdp.New(cdp.NewStream(conn), i.event), nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("browser host startup: %w (see host.log)", ctx.Err())
		case <-ticker.C:
		}
	}
}
