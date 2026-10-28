package agentnet

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// ProxyURL is a proxy address the person gave: http, https or socks5 (socks5h is the same here), with a login or not.
type ProxyURL struct {
	Scheme  string // http, https, socks5
	Host    string
	Port    int
	User    string
	Pass    string
	HasAuth bool
}

const maxProxyURL = 2048

var defaultProxyPorts = map[string]int{"http": 80, "https": 443, "socks5": 1080}

// ParseProxy reads a proxy URL. The error is for the person: it says what is wrong with the address, never repeats a
// password.
func ParseProxy(raw string) (ProxyURL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxProxyURL || strings.ContainsFunc(raw, func(r rune) bool { return r < 0x21 || r == 0x7f }) {
		return ProxyURL{}, errors.New("the proxy address is empty or has spaces")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ProxyURL{}, errors.New("the proxy address is not a URL")
	}
	var p ProxyURL
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks5":
		p.Scheme = strings.ToLower(u.Scheme)
	case "socks5h":
		p.Scheme = "socks5"
	default:
		return ProxyURL{}, errors.New("the proxy address starts with http://, https://, socks5:// or socks5h://")
	}
	if u.Hostname() == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return ProxyURL{}, errors.New("the proxy address is scheme://[user:password@]host:port and nothing after it")
	}
	p.Host = strings.ToLower(u.Hostname())
	p.Port = defaultProxyPorts[p.Scheme]
	if ps := u.Port(); ps != "" {
		n, err := strconv.Atoi(ps)
		if err != nil || n < 1 || n > 65535 {
			return ProxyURL{}, errors.New("the proxy port is not a number from 1 to 65535")
		}
		p.Port = n
	}
	if u.User != nil {
		p.HasAuth = true
		p.User = u.User.Username()
		p.Pass, _ = u.User.Password()
	}
	return p, nil
}

// HostPort is "host:port", IPv6 in brackets.
func (p ProxyURL) HostPort() string { return net.JoinHostPort(p.Host, strconv.Itoa(p.Port)) }

// URL is the address with its login, for a client that takes it.
func (p ProxyURL) URL() *url.URL {
	u := &url.URL{Scheme: p.Scheme, Host: p.HostPort()}
	if p.HasAuth {
		u.User = url.UserPassword(p.User, p.Pass)
	}
	return u
}

// Entry is the proxy as the hub may see it: no login.
func (p ProxyURL) Entry(bridge string) *hubproto.ProxyEntry {
	e := &hubproto.ProxyEntry{Scheme: p.Scheme, Host: p.HostPort()}
	if bridge != "" {
		e.Bridge = &bridge
	}
	return e
}

// noProxy keeps the agent's own loopback traffic (the runtime's MCP server, hooks) off the proxy.
const noProxy = "localhost,127.0.0.1,::1"

// proxyVars are the variables a proxy writes into an env file.
var proxyVars = []string{"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "https_proxy", "http_proxy", "no_proxy"}

// ProxyEnv is the variables a launch gets for the proxy address `via` (the bridge's, for a SOCKS proxy).
func ProxyEnv(via string) map[string]string {
	return map[string]string{
		"HTTPS_PROXY": via, "HTTP_PROXY": via, "NO_PROXY": noProxy,
		"https_proxy": via, "http_proxy": via, "no_proxy": noProxy,
	}
}

// savedProxy is one scope in proxy.json: the address as given (with its login) and, for SOCKS, the bridge.
type savedProxy struct {
	URL         string `json:"url"`
	BridgePort  int    `json:"bridgePort,omitempty"`
	BridgeToken string `json:"bridgeToken,omitempty"`
}

// Proxies keeps the person's proxy settings in proxy.json next to the env files (0600), writes the variables into the
// env files the launches source, and runs the bridges of SOCKS proxies. Safe for concurrent use.
type Proxies struct {
	env  *EnvDir
	file string
	// startBridge is StartBridge; tests put a fake dialer behind it.
	startBridge func(ProxyURL, string, int) (*Bridge, error)

	mu      sync.Mutex
	saved   map[string]savedProxy
	bridges map[string]*Bridge
}

// NewProxies is the proxy settings of the env files in env.
func NewProxies(env *EnvDir) *Proxies {
	return &Proxies{env: env, file: filepath.Join(env.Dir, "proxy.json"), startBridge: StartBridge,
		saved: map[string]savedProxy{}, bridges: map[string]*Bridge{}}
}

// Start reads proxy.json and brings the bridges back up, on their old ports when those are free. A scope whose saved
// address no longer reads is dropped. The env files are written again when a bridge's port changed.
func (p *Proxies) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	raw, err := os.ReadFile(p.file)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var saved map[string]savedProxy
	if err := json.Unmarshal(raw, &saved); err != nil {
		return fmt.Errorf("%s is not readable: %w", p.file, err)
	}
	for scope, sp := range saved {
		if !ValidScope(scope) {
			continue
		}
		if _, err := p.apply(scope, sp); err != nil {
			delete(saved, scope)
		}
	}
	return p.persist()
}

// apply makes a scope's proxy live: the bridge if it needs one, the env file, the in-memory state. p.mu is held.
func (p *Proxies) apply(scope string, sp savedProxy) (savedProxy, error) {
	pu, err := ParseProxy(sp.URL)
	if err != nil {
		return sp, err
	}
	via := pu.URL().String()
	if old := p.bridges[scope]; old != nil {
		old.Close()
		delete(p.bridges, scope)
	}
	if pu.Scheme == "socks5" {
		if sp.BridgeToken == "" {
			tok, err := randomToken()
			if err != nil {
				return sp, err
			}
			sp.BridgeToken = tok
		}
		b, err := p.startBridge(pu, sp.BridgeToken, sp.BridgePort)
		if err != nil && sp.BridgePort != 0 {
			b, err = p.startBridge(pu, sp.BridgeToken, 0)
		}
		if err != nil {
			return sp, fmt.Errorf("the SOCKS bridge did not start: %w", err)
		}
		p.bridges[scope] = b
		sp.BridgePort = b.Port()
		bridgeURL := url.URL{Scheme: "http", User: url.UserPassword(BridgeUser, sp.BridgeToken), Host: b.Addr()}
		via = bridgeURL.String()
	} else {
		sp.BridgePort, sp.BridgeToken = 0, ""
	}
	if err := p.env.Set(scope, ProxyEnv(via)); err != nil {
		return sp, err
	}
	p.saved[scope] = sp
	return sp, nil
}

func (p *Proxies) persist() error {
	if err := os.MkdirAll(p.env.Dir, 0o700); err != nil {
		return err
	}
	if len(p.saved) == 0 {
		if err := os.Remove(p.file); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	raw, err := json.MarshalIndent(p.saved, "", "  ")
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", p.file, os.Getpid())
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, p.file)
}

// Set makes rawURL the proxy of a scope (all, claude, codex).
func (p *Proxies) Set(scope, rawURL string) (hubproto.ProxySummary, error) {
	if !ValidScope(scope) {
		return hubproto.ProxySummary{}, fmt.Errorf("unknown scope %q", scope)
	}
	if _, err := ParseProxy(rawURL); err != nil {
		return hubproto.ProxySummary{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	prev := p.saved[scope]
	sp := savedProxy{URL: strings.TrimSpace(rawURL)}
	// A scope that stays on SOCKS keeps its bridge port and token, so running tabs keep working across the change.
	if old, err := ParseProxy(prev.URL); err == nil && old.Scheme == "socks5" {
		sp.BridgePort, sp.BridgeToken = prev.BridgePort, prev.BridgeToken
	}
	if _, err := p.apply(scope, sp); err != nil {
		return hubproto.ProxySummary{}, err
	}
	if err := p.persist(); err != nil {
		return hubproto.ProxySummary{}, err
	}
	return p.summary(), nil
}

// Clear removes a scope's proxy.
func (p *Proxies) Clear(scope string) (hubproto.ProxySummary, error) {
	if !ValidScope(scope) {
		return hubproto.ProxySummary{}, fmt.Errorf("unknown scope %q", scope)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if b := p.bridges[scope]; b != nil {
		b.Close()
		delete(p.bridges, scope)
	}
	delete(p.saved, scope)
	if err := p.env.Unset(scope, proxyVars...); err != nil {
		return hubproto.ProxySummary{}, err
	}
	if err := p.persist(); err != nil {
		return hubproto.ProxySummary{}, err
	}
	return p.summary(), nil
}

// Summary is the proxies as the hub may see them.
func (p *Proxies) Summary() hubproto.ProxySummary {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.summary()
}

func (p *Proxies) summary() hubproto.ProxySummary {
	entry := func(scope string) *hubproto.ProxyEntry {
		sp, ok := p.saved[scope]
		if !ok {
			return nil
		}
		pu, err := ParseProxy(sp.URL)
		if err != nil {
			return nil
		}
		bridge := ""
		if b := p.bridges[scope]; b != nil {
			bridge = b.Addr()
		}
		return pu.Entry(bridge)
	}
	return hubproto.ProxySummary{All: entry(ScopeAll), Claude: entry(ScopeClaude), Codex: entry(ScopeCodex)}
}

// Effective is the proxy the agent of provider runs behind: its own scope's, else the shared one. False: none.
func (p *Proxies) Effective(provider string) (ProxyURL, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, scope := range []string{provider, ScopeAll} {
		if sp, ok := p.saved[scope]; ok {
			if pu, err := ParseProxy(sp.URL); err == nil {
				return pu, true
			}
		}
	}
	return ProxyURL{}, false
}

// Close stops the bridges (the settings stay).
func (p *Proxies) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for scope, b := range p.bridges {
		b.Close()
		delete(p.bridges, scope)
	}
}

func randomToken() (string, error) {
	var b [18]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
