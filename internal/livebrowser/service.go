package livebrowser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/egress"
	"github.com/ly-solutions/harnessly/internal/preview"
)

// Config is how the service runs the browser.
type Config struct {
	// DataDir is the runtime's data directory: the browser's profile (cookies, sign-ins) lives in its live-browser/.
	// One runtime is one person on one machine, so the profile is theirs alone.
	DataDir string
	// Binary is the browser to run; empty looks for google-chrome, chromium and their variants on PATH.
	Binary string
	// ExtraArgs are added to the browser's command line (RUNTIME_LIVE_BROWSER_ARGS).
	ExtraArgs []string
	// MaxTabs bounds the pages open at once; a tab past it is refused.
	MaxTabs int
	// IdleAfter closes the browser once no tab has been open for this long.
	IdleAfter time.Duration
	// Nice lowers the browser's priority (0 keeps it).
	Nice int
	// Local are the machine's own addresses (preview.SelfAddresses); Lookup resolves names (nil for the system's).
	// Together they decide what the browser may load: see Refusal.
	Local  map[string]bool
	Lookup preview.Lookup
	// Log takes the service's notes.
	Log func(level, msg string, kv ...any)
}

const (
	defaultMaxTabs   = 4
	defaultIdleAfter = 3 * time.Minute
	startTimeout     = 20 * time.Second
	// A host's verdict is kept this long: a page asks for the same hosts many times a second.
	verdictFor = time.Minute
)

var chromeNames = []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"}

// Service owns the one browser of the runtime: started on the first tab, closed when idle or when it dies.
type Service struct {
	cfg Config

	mu     sync.Mutex
	cmd    *exec.Cmd
	exited chan struct{}
	conn   *conn
	tabs   map[string]*Tab
	idle   *time.Timer
	starts int
	lookup func(string) (string, error)
	// resolve is resolveHost, except in tests, which load pages from the machine.
	resolve func(host string) (addr, why string)
	egress  *egress.Proxy
	// windows are the pages a tab's page opened, by target id, while they are adopted (watchTargets).
	windows map[string]*Tab

	vmu      sync.Mutex
	verdicts map[string]verdict
}

// verdict is where a name pointed and whether it may be reached, kept for verdictFor.
type verdict struct {
	addr string
	why  string
	at   time.Time
}

// New is a service with the defaults filled in.
func New(cfg Config) *Service {
	if cfg.MaxTabs <= 0 {
		cfg.MaxTabs = defaultMaxTabs
	}
	if cfg.IdleAfter <= 0 {
		cfg.IdleAfter = defaultIdleAfter
	}
	if cfg.Log == nil {
		cfg.Log = func(string, string, ...any) {}
	}
	if cfg.Lookup == nil {
		cfg.Lookup = systemLookup
	}
	s := &Service{cfg: cfg, tabs: map[string]*Tab{}, lookup: exec.LookPath, verdicts: map[string]verdict{}, windows: map[string]*Tab{}}
	s.resolve = s.resolveHost
	return s
}

func systemLookup(host string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(addrs))
	for i, a := range addrs {
		out[i] = a.IP.String()
	}
	return out, nil
}

// Refusal says why an address may not open in the live browser: "" when it may. The browser runs on the machine, so
// its servers and the private networks behind it stay with the proxy, which knows what is allowed there. The rule
// holds for every connection the browser makes, not only the address typed: see egress.
func (s *Service) Refusal(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "Give an http(s) address."
	}
	if u.User != nil {
		return "An address must not carry a user name or password."
	}
	_, why := s.resolve(u.Hostname())
	return why
}

// resolveHost is where a name points (the one address the browser connects to) or why it is refused.
func (s *Service) resolveHost(host string) (addr, why string) {
	host = strings.ToLower(host)
	s.vmu.Lock()
	v, ok := s.verdicts[host]
	s.vmu.Unlock()
	if ok && time.Since(v.at) < verdictFor {
		return v.addr, v.why
	}
	r := preview.ResolvePublic(host, s.cfg.Lookup, s.cfg.Local)
	switch r.Error {
	case "":
		addr = r.Address
	case "not-found":
		why = "The address's name does not resolve."
	default:
		why = "The address leads into the machine or a private network: the pane opens such servers through the proxy."
	}
	s.vmu.Lock()
	if len(s.verdicts) > 4096 {
		s.verdicts = map[string]verdict{}
	}
	s.verdicts[host] = verdict{addr: addr, why: why, at: time.Now()}
	s.vmu.Unlock()
	return addr, why
}

// Available says whether a browser binary is on this machine.
func (s *Service) Available() bool {
	_, err := s.binary()
	return err == nil
}

func (s *Service) binary() (string, error) {
	if s.cfg.Binary != "" {
		return s.lookup(s.cfg.Binary)
	}
	for _, name := range chromeNames {
		if path, err := s.lookup(name); err == nil {
			return path, nil
		}
	}
	return "", errors.New("no Chrome or Chromium on this machine")
}

// ProfileDir is where the browser keeps the person's cookies and sign-ins.
func (s *Service) ProfileDir() string { return filepath.Join(s.cfg.DataDir, "live-browser", "profile") }

func (s *Service) args(profile, proxy string) []string {
	args := []string{
		"--headless=new", "--remote-debugging-pipe",
		"--user-data-dir=" + profile,
		"--no-first-run", "--no-default-browser-check", "--disable-session-crashed-bubble",
		"--disable-dev-shm-usage", "--disable-gpu", "--disable-background-networking",
		"--disable-component-update", "--disable-sync", "--disable-breakpad", "--no-pings",
		"--disable-features=Translate,MediaRouter,OptimizationHints",
		"--renderer-process-limit=" + strconv.Itoa(s.cfg.MaxTabs),
		"--window-size=1280,800", "--lang=ru-RU", "--hide-scrollbars=false",
		"--autoplay-policy=user-gesture-required", "--mute-audio", "--disable-extensions",
	}
	// Every connection goes through the egress, the loopback too (Chrome would skip the proxy for it otherwise);
	// nothing goes around it: no QUIC, no WebRTC past the proxy.
	args = append(args, egress.ChromeArgs(proxy)...)
	// The first window shows nothing (not chrome://newtab): the pages are made over the protocol.
	return append(append(args, s.cfg.ExtraArgs...), "about:blank")
}

// start launches the browser and connects to it over its pipe; the lock is held.
func (s *Service) startLocked(ctx context.Context) error {
	bin, err := s.binary()
	if err != nil {
		return err
	}
	profile := s.ProfileDir()
	if err := os.MkdirAll(profile, 0o700); err != nil {
		return err
	}
	// A lock of a browser that was killed keeps a new one from using the profile.
	_ = os.Remove(filepath.Join(profile, "SingletonLock"))
	eg, err := egress.Listen(func(host, _ string) (string, string) { return s.resolve(host) })
	if err != nil {
		return err
	}
	name, args := bin, s.args(profile, eg.Addr())
	if s.cfg.Nice > 0 {
		if nice, err := s.lookup("nice"); err == nil {
			name, args = nice, append([]string{"-n", strconv.Itoa(s.cfg.Nice), bin}, args...)
		}
	}
	// The browser reads the protocol on its fd 3 and writes it on fd 4 (--remote-debugging-pipe).
	theirIn, ourOut, err := os.Pipe()
	if err != nil {
		eg.Close()
		return err
	}
	ourIn, theirOut, err := os.Pipe()
	if err != nil {
		eg.Close()
		_ = theirIn.Close()
		_ = ourOut.Close()
		return err
	}
	// #nosec G204 -- the browser and its fixed flags; nothing of a request reaches the command line.
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "HOME="+s.cfg.DataDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.ExtraFiles = []*os.File{theirIn, theirOut}
	err = cmd.Start()
	_ = theirIn.Close()
	_ = theirOut.Close()
	if err != nil {
		eg.Close()
		_ = ourOut.Close()
		_ = ourIn.Close()
		return err
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	c := newConn(ourOut, ourIn)
	startCtx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	if err := c.Call(startCtx, "", "Browser.getVersion", nil, nil); err != nil {
		c.Close()
		killGroup(cmd, exited)
		eg.Close()
		select {
		case <-exited:
			return errors.New("the browser exited at start")
		default:
		}
		return fmt.Errorf("the browser did not answer at start: %w", err)
	}
	// A window a page opens goes to its opener's tab (see watchTargets).
	go s.watchTargets(c)
	if err := c.Call(startCtx, "", "Target.setDiscoverTargets", map[string]any{"discover": true}, nil); err != nil {
		c.Close()
		killGroup(cmd, exited)
		eg.Close()
		return fmt.Errorf("the browser's targets: %w", err)
	}
	s.cmd, s.exited, s.conn, s.egress = cmd, exited, c, eg
	s.starts++
	s.cfg.Log("info", "live browser started", "pid", cmd.Process.Pid, "binary", bin)
	go func() {
		select {
		case <-exited:
		case <-c.Done():
		}
		s.mu.Lock()
		var tabs []*Tab
		if s.cmd == cmd {
			s.cfg.Log("info", "live browser ended")
			tabs = s.dropLocked()
		}
		s.mu.Unlock()
		endAll(tabs)
	}()
	return nil
}

// watchTargets takes the browser's new pages. The tabs' own pages come from Open; every other page is a window a page
// opened (target=_blank, window.open, or a window of such a window) and is closed: nothing opens out of the pane's
// sight. One whose chain of openers leads to a tab is adopted by that tab, which goes to its address instead.
func (s *Service) watchTargets(c *conn) {
	events, stop := c.Listen("", 256)
	defer stop()
	for {
		select {
		case <-c.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if ev.Method != "Target.targetCreated" {
				continue
			}
			var p struct {
				TargetInfo struct {
					TargetID string `json:"targetId"`
					Type     string `json:"type"`
					URL      string `json:"url"`
					OpenerID string `json:"openerId"`
				} `json:"targetInfo"`
			}
			if json.Unmarshal(ev.Params, &p) != nil || p.TargetInfo.Type != "page" {
				continue
			}
			// Open holds the lock from the page's making to its listing: a tab's own page is listed by now.
			s.mu.Lock()
			if s.ownsLocked(p.TargetInfo.TargetID) {
				s.mu.Unlock()
				continue
			}
			owner := s.ownerLocked(p.TargetInfo.OpenerID)
			if owner != nil {
				s.windows[p.TargetInfo.TargetID] = owner
			}
			s.mu.Unlock()
			if owner != nil {
				go func(id, url string) {
					owner.adoptWindow(id, url)
					s.mu.Lock()
					delete(s.windows, id)
					s.mu.Unlock()
				}(p.TargetInfo.TargetID, p.TargetInfo.URL)
				continue
			}
			go func(id string) {
				ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
				defer cancel()
				_ = c.Call(ctx, "", "Target.closeTarget", map[string]any{"targetId": id}, nil)
			}(p.TargetInfo.TargetID)
		}
	}
}

func (s *Service) ownsLocked(targetID string) bool {
	for _, t := range s.tabs {
		if t.targetID == targetID {
			return true
		}
	}
	return false
}

// ownerLocked is the tab a window belongs to: its opener, or the tab its opener was adopted by.
func (s *Service) ownerLocked(openerID string) *Tab {
	if openerID == "" {
		return nil
	}
	for _, t := range s.tabs {
		if t.targetID == openerID {
			return t
		}
	}
	return s.windows[openerID]
}

// killGroup ends the browser and its helpers: a term, and a kill for what is still there after a moment. With exited
// it waits for the process to go, so its profile is quiet when the caller moves on.
func killGroup(cmd *exec.Cmd, exited <-chan struct{}) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	if exited == nil {
		go func() {
			time.Sleep(3 * time.Second)
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}()
		return
	}
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		select {
		case <-exited:
		case <-time.After(2 * time.Second):
		}
	}
}

// dropLocked forgets the browser and returns its tabs, which the caller ends once the lock is released (a tab's end
// tells the service, which takes the lock).
func (s *Service) dropLocked() []*Tab {
	if s.conn != nil {
		s.conn.Close()
	}
	if s.cmd != nil {
		killGroup(s.cmd, s.exited)
	}
	tabs := make([]*Tab, 0, len(s.tabs))
	for _, t := range s.tabs {
		tabs = append(tabs, t)
	}
	s.tabs = map[string]*Tab{}
	s.windows = map[string]*Tab{}
	if s.egress != nil {
		s.egress.Close()
		s.egress = nil
	}
	s.cmd, s.exited, s.conn = nil, nil, nil
	if s.idle != nil {
		s.idle.Stop()
		s.idle = nil
	}
	return tabs
}

func endAll(tabs []*Tab) {
	for _, t := range tabs {
		t.gone()
	}
}

// Close stops the browser.
func (s *Service) Close() {
	s.mu.Lock()
	tabs := s.dropLocked()
	s.mu.Unlock()
	endAll(tabs)
}

// Open gives a tab for the id (the pane's tab), starting the browser when it is not running. A tab that already exists
// is returned as it is: a page survives a reload of the pane. Past MaxTabs, the request is refused.
func (s *Service) Open(ctx context.Context, id string, width, height int, scale float64) (*Tab, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.tabs[id]; t != nil {
		t.rejoin()
		return t, nil
	}
	if len(s.tabs) >= s.cfg.MaxTabs {
		return nil, fmt.Errorf("%d pages are open already: close one", s.cfg.MaxTabs)
	}
	if s.conn == nil {
		if err := s.startLocked(ctx); err != nil {
			return nil, err
		}
	}
	if s.idle != nil {
		s.idle.Stop()
		s.idle = nil
	}
	t, err := newTab(ctx, s.conn, id, width, height, scale, s.Refusal)
	if err != nil {
		return nil, err
	}
	t.onClose = func() { s.forget(id, t) }
	s.tabs[id] = t
	return t, nil
}

// forget drops a tab and starts the idle count when it was the last.
func (s *Service) forget(id string, t *Tab) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tabs[id] != t {
		return
	}
	delete(s.tabs, id)
	if len(s.tabs) == 0 && s.conn != nil && s.idle == nil {
		s.idle = time.AfterFunc(s.cfg.IdleAfter, func() {
			s.mu.Lock()
			var tabs []*Tab
			if len(s.tabs) == 0 && s.conn != nil {
				s.cfg.Log("info", "live browser idle: closing")
				tabs = s.dropLocked()
			}
			s.mu.Unlock()
			endAll(tabs)
		})
	}
}

// has is the tab of the id, nil for none.
func (s *Service) has(id string) *Tab {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tabs[id]
}

// Tabs is how many pages are open.
func (s *Service) Tabs() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.tabs)
}

// Running says whether the browser is up.
func (s *Service) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn != nil
}

// EnvArgs reads a flag list from the environment: words split on spaces.
func EnvArgs(value string) []string { return strings.Fields(value) }
