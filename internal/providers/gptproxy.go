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

func (g *GPTProxy) On() bool {
	_, err := os.Stat(g.OnFile)
	return err == nil
}

// Ready: the switch is on, Codex is signed in by the subscription, and the proxy answers or its binary is there to
// start it again.
func (g *GPTProxy) Ready() bool {
	if !g.On() {
		return false
	}
	if _, ok := g.Login.Account(); !ok {
		return false
	}
	if g.healthy.Load() {
		return true
	}
	_, err := os.Stat(ProxyBin(g.Dir))
	return err == nil
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
		g.stopLocked()
		if err := os.Remove(g.OnFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if _, ok := g.Login.Account(); !ok {
		return &wire.ProfileSwitchError{Reason: wire.SwitchNoLogin, Message: "Codex is not signed in with a ChatGPT subscription"}
	}
	if err := g.startLocked(ctx, false); err != nil {
		return err
	}
	if err := writePrivate(g.OnFile, nil); err != nil {
		g.stopLocked()
		return err
	}
	return nil
}

// Resume starts the proxy in the background when the switch was left on: at the start of the runtime. A proxy that
// does not come up is tried again with the pauses of a crash.
func (g *GPTProxy) Resume() {
	if !g.On() {
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
	cfg := ProxyConfig{Port: port, AuthDir: filepath.Join(g.Dir, "auth"), ClientKey: s.key, RelayKey: g.Secret(), RelayBase: g.RelayBase}
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

// ProxyConfig is what the proxy's config says: where it listens and with which key, and its one upstream, the
// runtime's relay with its key and the gpt preset's models.
type ProxyConfig struct {
	Port                int
	AuthDir             string
	ClientKey, RelayKey string
	RelayBase           string
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
	b.WriteString("codex-api-key:\n")
	fmt.Fprintf(&b, "  - api-key: %s\n    base-url: %s\n    proxy-url: %s\n    models:\n", q(c.RelayKey), q(c.RelayBase), q("direct"))
	p, _ := Find("gpt")
	for _, m := range p.Models {
		fmt.Fprintf(&b, "      - name: %s\n        alias: %s\n", q(m.ID), q(m.ID))
		if len(m.Efforts) > 0 {
			levels := make([]string, len(m.Efforts))
			for i, e := range m.Efforts {
				levels[i] = q(string(e))
			}
			fmt.Fprintf(&b, "        thinking:\n          levels: [%s]\n", strings.Join(levels, ", "))
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
