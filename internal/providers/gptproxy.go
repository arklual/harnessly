package providers

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsonlog"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// GPTProxy is the switch of the gpt profile and the CLIProxyAPI it keeps running while it is on. The chain of a GPT
// chat is Claude Code → the proxy (127.0.0.1, a port of its own, a key of its own) at Anthropic's /v1/messages → the
// proxy's Codex upstream, set up as an API key with a base URL that is the runtime's GPT relay → the relay, which puts
// on the Codex CLI's ChatGPT token and account and sends it to chatgpt.com. So the proxy never holds the person's
// sign-in, and the Codex CLI stays the only one that renews it (CodexLogin).
//
// The proxy is a child of the runtime: started when the switch goes on (downloaded the first time, InstallProxy),
// watched and started again after a crash with a growing pause, stopped when the switch goes off or the runtime ends.
// Its files are under Dir, a folder of 0700: the config (written on every start), the key Claude Code sends, the port,
// the log and the binary.
type GPTProxy struct {
	Dir string
	// OnFile keeps the switch across restarts: it exists while the switch is on.
	OnFile string
	// OffFile keeps the person's «off»; with it set, the switch is on until it exists (switchOn).
	OffFile string
	// Login is the Codex CLI's sign-in the requests go up on.
	Login *CodexLogin
	// RelayBase is the runtime's GPT relay as the proxy reaches it (http://127.0.0.1:<port>/api/cli/gpt/v1).
	RelayBase string
	// Install puts the proxy's binary in place and gives its path; nil is InstallProxy.
	Install func(ctx context.Context, dir string) (string, error)
	// Command is the proxy's command for a config file; nil runs the binary with it and the built-in model lists.
	Command func(bin, config string) *exec.Cmd
	// FastTier says whether the ChatGPT backend has the fast tier for a model (Codex's model cache); nil: for none.
	FastTier func(model string) bool
	// ExtRelayBase is the runtime's relay of the OpenAI-compatible providers as the proxy reaches it
	// (http://127.0.0.1:<port>/api/cli/ext): the upstream of provider <id> is ExtRelayBase/<id>, and the relay puts the
	// provider's key from the safe on each request, so the proxy's config holds no key of a provider. "" runs none.
	ExtRelayBase string
	// FastPane says whether the chat in a tmux pane (its $TMUX_PANE, on the server of that socket name, "" for any) is
	// in fast mode; nil: none is. It is read on each request of the relay.
	FastPane func(pane, socket string) bool

	// mu orders the switch's changes; a turn on holds it until the proxy answers.
	mu sync.Mutex
	// secret is the relay's key, the proxy's «API key» of its Codex upstream: new on each start of the runtime.
	secretOnce sync.Once
	secret     string
	sup        *proxySupervisor
	healthy    atomic.Bool
	onChange   atomic.Pointer[func()]
	// ext are the OpenAI-compatible providers Claude Code chats run on through the proxy (SetExt): while there is one,
	// the proxy runs whether the gpt switch is on or not, once a chat asked for it (ExtEndpoint).
	ext atomic.Pointer[[]ExtUpstream]
}

// ExtUpstream is an OpenAI-compatible provider of the proxy: Claude Code asks for a model by its Alias, the proxy
// sends the provider its Name at /chat/completions, through the runtime's relay of the provider.
type ExtUpstream struct {
	// ID is the provider's id: the path of its relay.
	ID     string
	Models []ExtModel
}

// ExtModel is a model of an ExtUpstream.
type ExtModel struct {
	Name, Alias string
	Efforts     []wire.Effort
}

// ExtRelayPath is where the runtime mounts the relay of the OpenAI-compatible providers.
const ExtRelayPath = "/api/cli/ext"

// SetExt sets the OpenAI-compatible providers of the proxy. A proxy that runs takes them at once (its config is written
// again, and CLIProxyAPI reads it again); one that runs only for them stops when there are none left.
func (g *GPTProxy) SetExt(list []ExtUpstream) {
	list = slices.Clone(list)
	g.ext.Store(&list)
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sup == nil {
		return
	}
	if len(list) == 0 && !g.On() {
		g.stopLocked()
		return
	}
	g.rewriteLocked()
}

func (g *GPTProxy) extList() []ExtUpstream {
	if p := g.ext.Load(); p != nil && g.ExtRelayBase != "" {
		return *p
	}
	return nil
}

// ExtReady says whether a chat may start on the OpenAI-compatible provider of that id: the proxy has it among its
// upstreams (it is started when the chat asks for it).
func (g *GPTProxy) ExtReady(id string) bool {
	return slices.ContainsFunc(g.extList(), func(u ExtUpstream) bool { return u.ID == id && len(u.Models) > 0 })
}

// ExtEndpoint is where a Claude Code chat on an OpenAI-compatible provider sends its requests and the file of the key
// it sends: the proxy, started (downloaded the first time) when it does not run.
func (g *GPTProxy) ExtEndpoint(ctx context.Context) (string, string, error) {
	if len(g.extList()) == 0 {
		return "", "", ErrSwitchOff
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.startLocked(ctx, false); err != nil {
		return "", "", err
	}
	port, ok := g.savedPort()
	if !ok {
		return "", "", ErrSwitchOff
	}
	return "http://127.0.0.1:" + strconv.Itoa(port), g.keyFile(), nil
}

// rewriteLocked writes the config of a running proxy again: the upstreams changed. CLIProxyAPI watches its config and
// takes the change without a restart, so the chats that stream through it go on. The file is written in place: its
// watch is of the file, and a file renamed over it is not seen (CLIProxyAPI 8.x).
func (g *GPTProxy) rewriteLocked() {
	port, ok := g.savedPort()
	key, err := g.clientKey()
	if !ok || err != nil {
		return
	}
	body := g.config(port, key).YAML()
	if have, err := os.ReadFile(g.configFile()); err == nil && string(have) == string(body) {
		return
	}
	if err := os.WriteFile(g.configFile(), body, 0o600); err != nil {
		jsonlog.Log("warn", "providers: the proxy's config was not written", jsonlog.F{K: "error", V: err.Error()})
	}
}

// RefreshModels gives a running proxy the latest models of Codex's cache. CLIProxyAPI watches its config, so existing
// chats can switch to a newly offered model without restarting either the proxy or the chat.
func (g *GPTProxy) RefreshModels() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sup != nil {
		g.rewriteLocked()
	}
}

// config is the proxy's config: the Codex upstream while the gpt switch is on, and the OpenAI-compatible providers.
func (g *GPTProxy) config(port int, key string) ProxyConfig {
	cfg := ProxyConfig{Port: port, AuthDir: filepath.Join(g.Dir, "auth"), ClientKey: key, RelayKey: g.Secret(), Ext: g.extList(), ExtBase: g.ExtRelayBase}
	if g.On() {
		cfg.RelayBase = g.RelayBase
	}
	return cfg
}

// Notify sets what is told when the proxy comes up or goes down by itself, so a machine can show the profile offered
// or not.
func (g *GPTProxy) Notify(f func()) { g.onChange.Store(&f) }

func (g *GPTProxy) changed() {
	if f := g.onChange.Load(); f != nil && *f != nil {
		(*f)()
	}
}

// GPTRelayPath is where the runtime mounts the GPT relay.
const GPTRelayPath = "/api/cli/gpt/v1"

// gptUpstream is the ChatGPT backend the relay sends to.
const gptUpstream = "https://chatgpt.com/backend-api/codex"

// The waits of the proxy: how long a start may take to answer, and the pauses between restarts after a crash.
const (
	proxyStartWait  = 20 * time.Second
	proxyBackoffMin = time.Second
	proxyBackoffMax = time.Minute
	// proxySteady is how long a proxy has to run before a crash starts the pauses from the shortest again.
	proxySteady   = 2 * time.Minute
	proxyStopWait = 3 * time.Second
	// proxyLogMax is the size past which the log is moved to <log>.1 at a start.
	proxyLogMax = 4 << 20
)

var _ Switch = (*GPTProxy)(nil)

func (g *GPTProxy) configFile() string { return filepath.Join(g.Dir, "config.yaml") }
func (g *GPTProxy) keyFile() string    { return filepath.Join(g.Dir, "client.key") }
func (g *GPTProxy) portFile() string   { return filepath.Join(g.Dir, "port") }
func (g *GPTProxy) pidFile() string    { return filepath.Join(g.Dir, "pid") }

// LogFile is the proxy's output, both streams.
func (g *GPTProxy) LogFile() string { return filepath.Join(g.Dir, "proxy.log") }

func (g *GPTProxy) On() bool { return switchOn(g.OnFile, g.OffFile) }

// signedIn says whether Codex is signed in by the subscription the requests go up on.
func (g *GPTProxy) signedIn() bool {
	_, ok := g.Login.Account()
	return ok
}

// Ready: the switch is on and Codex is signed in by the subscription. The proxy is started by the chat that needs it
// (Ensure), so a machine that never opens a GPT chat never downloads it.
func (g *GPTProxy) Ready() bool { return g.On() && g.signedIn() }

// Ensure starts the proxy (downloaded the first time) when the switch is on and it does not run: a chat is about to
// ask Endpoint for it.
func (g *GPTProxy) Ensure(ctx context.Context) error {
	if !g.Ready() {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.On() {
		return nil
	}
	return g.startLocked(ctx, false)
}

// Endpoint is the proxy's address and the file of its key, while the switch is on and the proxy has a port.
func (g *GPTProxy) Endpoint() (string, string, bool) {
	if !g.On() {
		return "", "", false
	}
	port, ok := g.savedPort()
	if !ok {
		return "", "", false
	}
	if _, err := os.Stat(g.keyFile()); err != nil {
		return "", "", false
	}
	return "http://127.0.0.1:" + strconv.Itoa(port), g.keyFile(), true
}

// Secret is the key the proxy sends the relay.
func (g *GPTProxy) Secret() string {
	g.secretOnce.Do(func() { g.secret = randomHex(32) })
	return g.secret
}

// SetOn turns the switch. On: Codex must be signed in by the subscription; the binary is installed, the proxy started,
// and the call returns once it answers (a *wire.ProfileSwitchError when it does not, and the switch stays off). Off
// stops the proxy.
func (g *GPTProxy) SetOn(ctx context.Context, on bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !on {
		if err := writeSwitch(g.OnFile, g.OffFile, false); err != nil {
			return err
		}
		g.offLocked()
		return nil
	}
	if !g.signedIn() {
		return &wire.ProfileSwitchError{Reason: wire.SwitchNoLogin, Message: "Codex is not signed in with a ChatGPT subscription"}
	}
	// The switch is on before the start: the config written at it has the Codex upstream.
	if err := writeSwitch(g.OnFile, g.OffFile, true); err != nil {
		return err
	}
	if err := g.startLocked(ctx, false); err != nil {
		_ = writeSwitch(g.OnFile, g.OffFile, false)
		g.offLocked()
		return err
	}
	return nil
}

// offLocked is the proxy after the gpt switch went off: stopped, unless the OpenAI-compatible providers keep it, which
// then runs on without the Codex upstream.
func (g *GPTProxy) offLocked() {
	if g.sup != nil && len(g.extList()) > 0 {
		g.rewriteLocked()
		return
	}
	g.stopLocked()
}

// Resume starts the proxy in the background when the switch is on and Codex is signed in: at the start of the runtime.
// A proxy that does not come up is tried again with the pauses of a crash.
func (g *GPTProxy) Resume() {
	if !g.Ready() {
		return
	}
	go func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if !g.On() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := g.startLocked(ctx, true); err != nil {
			jsonlog.Log("warn", "providers: the GPT proxy did not resume", jsonlog.F{K: "error", V: err.Error()})
		}
	}()
}

// Close stops the proxy and leaves the switch as it is: the runtime ends.
func (g *GPTProxy) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stopLocked()
}

func (g *GPTProxy) stopLocked() {
	if g.sup != nil {
		g.sup.stop()
		g.sup = nil
	}
	g.healthy.Store(false)
}

// startLocked installs the binary, writes the config and starts the supervisor; it returns once the proxy answers.
// keep: a first start that fails is not given up but retried (Resume).
func (g *GPTProxy) startLocked(ctx context.Context, keep bool) error {
	if g.sup != nil && g.healthy.Load() {
		// Running already: what it relays may have changed.
		g.rewriteLocked()
		return nil
	}
	g.stopLocked()
	if err := os.MkdirAll(g.Dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(g.Dir, 0o700); err != nil {
		return err
	}
	install := g.Install
	if install == nil {
		install = func(ctx context.Context, dir string) (string, error) { return InstallProxy(ctx, dir, nil) }
	}
	bin, err := install(ctx, g.Dir)
	if err != nil {
		return &wire.ProfileSwitchError{Reason: wire.SwitchDownload, Message: "CLIProxyAPI could not be installed: " + err.Error()}
	}
	key, err := g.clientKey()
	if err != nil {
		return err
	}
	g.killStale(key)
	sup := &proxySupervisor{g: g, bin: bin, key: key, done: make(chan struct{}), quit: make(chan struct{})}
	first := make(chan error, 1)
	go sup.run(first, keep)
	g.sup = sup
	select {
	case err := <-first:
		if err != nil && !keep {
			g.stopLocked()
			return &wire.ProfileSwitchError{Reason: wire.SwitchStart, Message: err.Error()}
		}
		return err
	case <-ctx.Done():
		if !keep {
			g.stopLocked()
		}
		return &wire.ProfileSwitchError{Reason: wire.SwitchStart, Message: "CLIProxyAPI did not answer in time"}
	}
}

// clientKey is the key Claude Code sends the proxy: made once and kept, so a chat's key file stays right across the
// proxy's restarts.
func (g *GPTProxy) clientKey() (string, error) {
	if raw, err := os.ReadFile(g.keyFile()); err == nil && len(strings.TrimSpace(string(raw))) >= 32 {
		return strings.TrimSpace(string(raw)), nil
	}
	key := "hrn-" + randomHex(24)
	// No newline: the file is read by `cat` into a header.
	return key, writePrivate(g.keyFile(), []byte(key))
}

func (g *GPTProxy) savedPort() (int, bool) {
	raw, err := os.ReadFile(g.portFile())
	if err != nil {
		return 0, false
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	return port, err == nil && port > 0 && port < 65536
}

// port is the saved port while it is free, else a new free one, saved: a port that stays makes the address in the
// settings of running chats stay right.
func (g *GPTProxy) port() (int, error) {
	if port, ok := g.savedPort(); ok && portFree(port) {
		return port, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port, writePrivate(g.portFile(), []byte(strconv.Itoa(port)))
}

func portFree(port int) bool {
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// killStale ends a proxy a previous runtime left running (one that died without stopping it): the process of the
// saved pid, if it still answers on the saved port with this proxy's key, so a pid taken since is not touched.
func (g *GPTProxy) killStale(key string) {
	raw, err := os.ReadFile(g.pidFile())
	if err != nil {
		return
	}
	_ = os.Remove(g.pidFile())
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	port, ok := g.savedPort()
	if err != nil || pid <= 1 || !ok || !proxyAnswers(context.Background(), port, key) {
		return
	}
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Signal(syscall.SIGTERM)
		deadline := time.Now().Add(proxyStopWait)
		for time.Now().Before(deadline) && !portFree(port) {
			time.Sleep(100 * time.Millisecond)
		}
		_ = p.Signal(syscall.SIGKILL)
	}
}

// proxyAnswers says whether the proxy on port lists its models to the key: it is up, and it is ours.
func proxyAnswers(ctx context.Context, port int, key string) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(port)+"/v1/models", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := (&http.Client{Transport: &http.Transport{Proxy: nil}}).Do(req)
	if err != nil {
		return false
	}
	_ = res.Body.Close()
	return res.StatusCode == http.StatusOK
}

// proxySupervisor runs one proxy after another until it is stopped.
type proxySupervisor struct {
	g        *GPTProxy
	bin, key string
	quit     chan struct{}
	done     chan struct{}
	once     sync.Once
}

func (s *proxySupervisor) stop() {
	s.once.Do(func() { close(s.quit) })
	<-s.done
}

// run starts the proxy, tells first how its first start went, and starts it again after each crash. Without keep a
// first start that fails ends the supervisor.
func (s *proxySupervisor) run(first chan<- error, keep bool) {
	defer close(s.done)
	g := s.g
	pause := proxyBackoffMin
	for attempt := 0; ; attempt++ {
		started := time.Now()
		tell := func(error) {}
		if attempt == 0 {
			tell = func(err error) { first <- err }
		}
		up, err := s.runOnce(tell)
		if !up && attempt == 0 {
			tell(err)
			if !keep {
				return
			}
		}
		if err != nil {
			jsonlog.Log("warn", "providers: the GPT proxy stopped", jsonlog.F{K: "error", V: err.Error()})
		}
		if g.healthy.Swap(false) {
			g.changed()
		}
		if time.Since(started) > proxySteady {
			pause = proxyBackoffMin
		}
		select {
		case <-s.quit:
			return
		case <-time.After(pause):
		}
		pause = min(pause*2, proxyBackoffMax)
	}
}

// runOnce runs the proxy once: until it exits, or until it is stopped (nil). It tells up once the proxy answers; up is
// false, and up not told, for a start that failed.
func (s *proxySupervisor) runOnce(tell func(error)) (up bool, err error) {
	g := s.g
	port, err := g.port()
	if err != nil {
		return false, err
	}
	cfg := g.config(port, s.key)
	if err := os.MkdirAll(cfg.AuthDir, 0o700); err != nil {
		return false, err
	}
	if err := writePrivate(g.configFile(), cfg.YAML()); err != nil {
		return false, err
	}
	logf, err := openProxyLog(g.LogFile())
	if err != nil {
		return false, err
	}
	defer logf.Close()
	var cmd *exec.Cmd
	if g.Command != nil {
		cmd = g.Command(s.bin, g.configFile())
	} else {
		cmd = exec.Command(s.bin, "-config", g.configFile(), "-local-model")
	}
	cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = g.Dir, nil, logf, logf
	cmd.SysProcAttr = proxyProcAttr()
	if err := cmd.Start(); err != nil {
		return false, err
	}
	_ = writePrivate(g.pidFile(), []byte(strconv.Itoa(cmd.Process.Pid)))
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	defer os.Remove(g.pidFile())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	answers := make(chan bool, 1)
	go func() {
		deadline := time.Now().Add(proxyStartWait)
		for time.Now().Before(deadline) {
			if proxyAnswers(ctx, port, s.key) {
				answers <- true
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(150 * time.Millisecond):
			}
		}
		answers <- false
	}()
	select {
	case ok := <-answers:
		if !ok {
			terminate(cmd, exited)
			return false, errors.New("CLIProxyAPI did not answer on its port; see " + g.LogFile())
		}
	case err := <-exited:
		return false, fmt.Errorf("CLIProxyAPI exited at its start (%v); see %s", err, g.LogFile())
	case <-s.quit:
		terminate(cmd, exited)
		return false, errors.New("CLIProxyAPI was stopped before it answered")
	}
	g.healthy.Store(true)
	tell(nil)
	g.changed()
	select {
	case err := <-exited:
		return true, fmt.Errorf("CLIProxyAPI exited (%v)", err)
	case <-s.quit:
		terminate(cmd, exited)
		return true, nil
	}
}

// terminate asks the proxy to stop, and kills it if it has not after proxyStopWait.
func terminate(cmd *exec.Cmd, exited <-chan error) {
	_ = cmd.Process.Signal(syscall.SIGTERM)
	// The exit status of a proxy asked to stop says nothing the supervisor acts on.
	select {
	case err := <-exited:
		_ = err
	case <-time.After(proxyStopWait):
		_ = cmd.Process.Kill()
		err := <-exited
		_ = err
	}
}

func openProxyLog(path string) (*os.File, error) {
	if fi, err := os.Stat(path); err == nil && fi.Size() > proxyLogMax {
		_ = os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
}

// ProxyConfig is what the proxy's config says: where it listens and with which key, and its upstreams: the runtime's
// GPT relay with the gpt preset's models (RelayBase, "" for none), and the runtime's relay of each OpenAI-compatible
// provider (ExtBase/<id>). Both relays take RelayKey and put the real credential on; the config holds none.
type ProxyConfig struct {
	Port                int
	AuthDir             string
	ClientKey, RelayKey string
	RelayBase           string
	ExtBase             string
	Ext                 []ExtUpstream
}

// YAML is the proxy's config file, in CLIProxyAPI's flat layout (its v8 template still reads it). Everything the proxy
// does besides relaying is off: the management API and its panel, usage statistics, the image tool, the retries of
// another credential (there is none). The upstream goes direct: the relay is on this machine.
func (c ProxyConfig) YAML() []byte {
	q := strconv.Quote
	var b strings.Builder
	b.WriteString("# Written by harnessly at every start of the proxy (internal/providers/gptproxy.go); edits are lost.\n")
	fmt.Fprintf(&b, "host: %s\nport: %d\nauth-dir: %s\n", q("127.0.0.1"), c.Port, q(c.AuthDir))
	fmt.Fprintf(&b, "api-keys:\n  - %s\n", q(c.ClientKey))
	b.WriteString("remote-management:\n  allow-remote: false\n  secret-key: \"\"\n  disable-control-panel: true\n")
	b.WriteString("logging-to-file: false\nusage-statistics-enabled: false\nrequest-retry: 0\ndisable-image-generation: true\n")
	levels := func(indent string, efforts []wire.Effort) {
		if len(efforts) == 0 {
			return
		}
		out := make([]string, len(efforts))
		for i, e := range efforts {
			out[i] = q(string(e))
		}
		fmt.Fprintf(&b, "%sthinking:\n%s  levels: [%s]\n", indent, indent, strings.Join(out, ", "))
	}
	if c.RelayBase != "" {
		b.WriteString("codex-api-key:\n")
		fmt.Fprintf(&b, "  - api-key: %s\n    base-url: %s\n    proxy-url: %s\n    models:\n", q(c.RelayKey), q(c.RelayBase), q("direct"))
		p, _ := Find("gpt")
		for _, m := range p.Models {
			fmt.Fprintf(&b, "      - name: %s\n        alias: %s\n", q(m.ID), q(m.ID))
			levels("        ", m.Efforts)
		}
	}
	if c.ExtBase != "" && len(c.Ext) > 0 {
		b.WriteString("openai-compatibility:\n")
		for _, u := range c.Ext {
			fmt.Fprintf(&b, "  - name: %s\n    base-url: %s\n    api-key-entries:\n      - api-key: %s\n        proxy-url: %s\n    models:\n",
				q("ext-"+u.ID), q(strings.TrimRight(c.ExtBase, "/")+"/"+u.ID), q(c.RelayKey), q("direct"))
			for _, m := range u.Models {
				fmt.Fprintf(&b, "      - name: %s\n        alias: %s\n", q(m.Name), q(m.Alias))
				levels("        ", m.Efforts)
			}
		}
	}
	return []byte(b.String())
}

// gptForwarded are the headers of the proxy's request that go up with it: those the Codex CLI sends the backend.
var gptForwarded = []string{"Content-Type", "Accept", "User-Agent", "Originator", "Version", "Session_id", "Session-Id",
	"X-Client-Request-Id", "X-Codex-Turn-Metadata", "X-Codex-Turn-State", "X-Codex-Window-Id", "Thread-Id",
	"X-Codex-Beta-Features", "X-Openai-Internal-Codex-Responses-Lite", "Content-Encoding"}

// Relay is the GPT relay mounted on GPTRelayPath: only the proxy, with the secret of this runtime, may use it, and each
// request goes up to the ChatGPT backend with the Codex CLI's token and account.
func (g *GPTProxy) Relay() *Relay {
	return &Relay{
		Upstream: gptUpstream, Prefix: GPTRelayPath, Plain: true, Forward: gptForwarded, Prepare: g.prepare,
		Credential: g.Login.Token,
		Extra: func(context.Context) http.Header {
			if id := g.Login.AccountID(); id != "" {
				return http.Header{"Chatgpt-Account-Id": {id}}
			}
			return nil
		},
		Allow: func(r *http.Request) bool {
			got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			return ok && subtle.ConstantTimeCompare([]byte(got), []byte(g.Secret())) == 1
		},
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// writePrivate writes a file of 0600 whole, under a temporary name first.
func writePrivate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
