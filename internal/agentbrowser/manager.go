// Package agentbrowser owns persistent browser profiles and conversation tabs.
package agentbrowser

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser/host"
	"github.com/ly-solutions/harnessly/internal/agentbrowser/provision"
	"github.com/ly-solutions/harnessly/internal/cdp"
)

// DefaultProfile is used for conversations without an explicit profile binding.
const DefaultProfile = "common"

// ErrOff reports that the browser was disabled by configuration.
var ErrOff = errors.New("agent browser is disabled")

var profileID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// Config selects managed Chrome or an externally owned CDP endpoint.
type Config struct {
	// ProfileOf resolves a conversation on each InstanceFor call. Nil or an empty result
	// selects DefaultProfile. The callback must be safe for concurrent calls.
	ProfileOf     func(conv string) string
	LookupProfile func(id string) (Profile, bool)
	// NoSandbox, when set, is the explicit preference and overrides environment flags.
	NoSandbox func() bool
	// Idle is the inactivity timeout; zero disables automatic shutdown.
	Idle time.Duration
	// MaxHosts bounds the managed browsers running at once for this runtime (RUNTIME_BROWSER_MAX_HOSTS): starting one
	// more closes the longest idle, or is refused while every one is busy. Zero is no bound.
	MaxHosts int
	// MaxTabs bounds a browser's tabs and its renderer processes (RUNTIME_BROWSER_MAX_TABS); zero is no bound.
	MaxTabs int
	// Nice lowers the priority of Chrome and Xvfb (RUNTIME_BROWSER_NICE).
	Nice int
	// DownloadLimit is the largest file a page may download into its profile (RUNTIME_BROWSER_DOWNLOAD_LIMIT,
	// bytes); zero refuses downloads.
	DownloadLimit int64
	DataDir       string
	Executable    string
	Chrome        string
	Mode          string
	CDP           string
	Off           bool
}

// ConfigFromEnv reads browser settings without starting any processes.
func ConfigFromEnv(data string) Config {
	return Config{
		DataDir: data, Chrome: os.Getenv("RUNTIME_BROWSER_CHROME"), Mode: os.Getenv("RUNTIME_BROWSER_MODE"),
		CDP:           strings.TrimSpace(os.Getenv("RUNTIME_BROWSER_CDP")),
		Idle:          browserIdle(os.Getenv("RUNTIME_BROWSER_IDLE")),
		MaxHosts:      envCount("RUNTIME_BROWSER_MAX_HOSTS", DefaultMaxHosts),
		MaxTabs:       envCount("RUNTIME_BROWSER_MAX_TABS", DefaultMaxTabs),
		Nice:          envCount("RUNTIME_BROWSER_NICE", DefaultNice),
		DownloadLimit: downloadLimit(os.Getenv("RUNTIME_BROWSER_DOWNLOAD_LIMIT")),
		Off:           strings.EqualFold(strings.TrimSpace(os.Getenv("RUNTIME_BROWSER")), "off"),
	}
}

// FindChrome discovers an installed browser without downloading; Manager.Ready also provisions missing Chrome.
func FindChrome() (string, error) {
	cfg := provision.FromEnv("")
	path, _, err := provision.Find(cfg)
	if err == nil && path == "" {
		err = errors.New("agent browser needs Chrome or Chromium")
	}
	return path, err
}

// Manager reconnects independent browser profiles throughout the runtime lifetime.
type Manager struct {
	preparation  *browserPreparation
	probe        func(context.Context, string, string) (provision.Inspection, error)
	cfg          Config
	mu           sync.Mutex
	instances    map[string]*Instance
	profileLocks map[string]chan struct{}
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

// New creates a lazy manager; Instance starts the first connection.
func New(cfg Config) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{probe: provision.Probe, cfg: cfg, instances: make(map[string]*Instance), profileLocks: make(map[string]chan struct{}), ctx: ctx, cancel: cancel}
}

// Enabled reports whether browser access is configured.
func (m *Manager) Enabled() bool { return !m.cfg.Off }

// Close disconnects clients but deliberately leaves hosts and browsers running.
func (m *Manager) Close() { m.mu.Lock(); m.cancel(); m.mu.Unlock(); m.wg.Wait() }

// InstanceFor resolves the conversation's profile without coupling callers to profile storage.
func (m *Manager) InstanceFor(ctx context.Context, conv string) (*Instance, error) {
	return m.Instance(ctx, m.profileFor(conv))
}

func (m *Manager) profileFor(conv string) string {
	id := DefaultProfile
	if m.cfg.ProfileOf != nil {
		if resolved := m.cfg.ProfileOf(conv); resolved != "" {
			id = resolved
		}
	}
	return id
}

// Instance waits for the named profile to become usable, starting its host when necessary.
func (m *Manager) Instance(ctx context.Context, id string) (*Instance, error) {
	if m.cfg.Off {
		return nil, ErrOff
	}
	if !profileID.MatchString(id) {
		return nil, errors.New("invalid browser profile id")
	}
	unlock, err := m.lockProfile(ctx, id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	profile, err := m.lookupProfile(id)
	if err != nil {
		return nil, err
	}
	if profile.Kind != "attached" {
		if _, err := m.preparedChrome(ctx); err != nil {
			return nil, err
		}
	}
	m.mu.Lock()
	if m.ctx.Err() != nil {
		m.mu.Unlock()
		return nil, cdp.ErrClosed
	}
	i := m.instances[id]
	m.mu.Unlock()
	if i == nil {
		// Profile callbacks and registry reads run outside the manager lock.
		i = newInstance(m, id)
		m.mu.Lock()
		if m.ctx.Err() != nil {
			m.mu.Unlock()
			i.cancel()
			return nil, cdp.ErrClosed
		}
		m.instances[id] = i
		m.wg.Add(1)
		go func() { defer m.wg.Done(); i.run() }()
		m.mu.Unlock()
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := i.State()
		i.mu.Lock()
		closing := i.closing
		i.mu.Unlock()
		if closing {
			return nil, cdp.ErrClosed
		}
		if state == "up" {
			i.mu.Lock()
			conn := i.conn
			i.mu.Unlock()
			select {
			case <-conn.Done():
			case <-m.ctx.Done():
				return nil, cdp.ErrClosed
			default:
				i.touch()
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
	if i.profile.Kind == "attached" {
		return cdp.Dial(ctx, i.profile.CDP, i.event)
	}
	socket := i.manager.profileSocket(i.profile.ID)
	dial := func() (net.Conn, error) { return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", socket) }
	if conn, err := dial(); err == nil {
		stream := cdp.NewStream(conn)
		if build, e := hostBuild(conn, stream); e == nil && build == host.BuildID() {
			return cdp.New(stream, i.event), nil
		}
		// A host of another build (the runtime was updated) or one that does not answer: replace it, so the
		// browser runs with this build's rules. The host keeps the session cookies across.
		_ = stream.Close()
		if err = host.Stop(ctx, i.dir, socket); err != nil {
			return nil, fmt.Errorf("replace browser host: %w", err)
		}
	}
	if err := i.manager.makeRoom(ctx, i.profile.ID); err != nil {
		return nil, err
	}
	chrome, err := i.manager.preparedChrome(ctx)
	if err != nil {
		return nil, err
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
	if err = host.Start(executable, host.Config{ProfileDir: i.dir, Socket: socket,
		Chrome: chrome, Mode: mode, NoSandbox: sandboxPreference(cfg.NoSandbox),
		Idle: cfg.Idle, MaxTabs: cfg.MaxTabs, Nice: cfg.Nice, DownloadLimit: cfg.DownloadLimit}); err != nil {
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
