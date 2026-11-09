package host

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/preview"
	"github.com/ly-solutions/harnessly/internal/preview/ports"
)

// AllowedNavigation says whether a page may be opened at raw: an http(s) address without a user name, or about:blank.
// file:, chrome:, javascript:, data:, view-source: and the rest never become a page of the agent's Chrome; the relay
// refuses them for every client (Page.navigate, Target.createTarget), whatever the tool that asked.
func AllowedNavigation(raw string) bool {
	if raw == "about:blank" {
		return true
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Hostname() == "" {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

const (
	// A name's verdict is kept this long: a page asks for the same hosts many times a second.
	verdictFor = time.Minute
	// The dev servers of the machine are listed again after this long.
	devServersFor = 2 * time.Second
)

// gate is the rule the agent's Chrome is held to at the network (the egress proxy), for every connection a page makes:
// a public address, a host the deployment listed for a private network (RUNTIME_PREVIEW_PRIVATE_HOSTS), or a dev server
// of this machine on the loopback. The machine's other ports (the runtime's own, the machine agent's, databases of
// other users), its own public addresses and the private networks behind it are refused. A name is resolved once and
// the proxy connects to the address checked, so DNS rebinding changes nothing.
type gate struct {
	lookup  preview.Lookup
	local   map[string]bool
	private map[string]bool
	// ports lists the loopback ports a page may reach (dev) and every port something listens on (listening).
	ports func() (dev, listening map[int]bool)

	mu        sync.Mutex
	verdicts  map[string]verdict
	dev       map[int]bool
	listening map[int]bool
	portsAt   time.Time
}

type verdict struct {
	addr, why string
	at        time.Time
}

// newGate is the rule of the machine; inHost leaves the host's own ports (its egress) out of the dev servers.
func newGate(inHost bool) *gate {
	return &gate{
		local:    preview.SelfAddresses(os.Getenv("RUNTIME_PREVIEW_SELF_IPS")),
		private:  preview.PrivateHosts(strings.Split(os.Getenv("RUNTIME_PREVIEW_PRIVATE_HOSTS"), ",")),
		verdicts: map[string]verdict{},
		ports:    func() (map[int]bool, map[int]bool) { return machinePorts(inHost) },
	}
}

// runtimeGate is the same rule in the runtime, which checks an address before it asks the browser to open it.
var runtimeGate = sync.OnceValue(func() *gate { return newGate(false) })

// Refusal says why the agent's browser will not open raw, in the words of a failed navigation ("" when it may).
// The egress refuses the same at the network; checked before, the agent learns why instead of reading a 403 page.
func Refusal(raw string) string {
	if raw == "about:blank" {
		return ""
	}
	if !AllowedNavigation(raw) {
		return "URL refused: use http://, https:// or about:blank"
	}
	u, _ := url.Parse(strings.TrimSpace(raw))
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	_, why := runtimeGate().resolve(u.Hostname(), port)
	return why
}

// platformExecutables are the platform's programs as installed (ss shows only 15 letters of a name, see
// ports.DevServers): their ports are the runtime's, the machine agent's, the CLI's — never a dev server.
var platformExecutables = map[string]bool{"runtime": true, "harnessly-runtime": true, "agent": true,
	"harnessly-agent": true, "harnessly": true, "agentprobe": true}

// machinePorts are this user's listeners less the platform's own (dev) and every listener (listening): the runtime
// and this host run one executable (whatever it is called), a test binary aside — its servers are a test's dev servers.
func machinePorts(inHost bool) (dev, listening map[int]bool) {
	if runtime.GOOS != "linux" {
		dev = lsofDevServers(inHost)
		return dev, dev
	}
	out, err := ports.RunSS()
	if err != nil {
		return map[int]bool{}, map[int]bool{}
	}
	listening = map[int]bool{}
	for _, s := range ports.ParseSockets(out) {
		listening[s.Port] = true
	}
	self, _ := os.Executable()
	self = filepath.Clean(self)
	platform := func(pid int) bool {
		if inHost && pid == os.Getpid() {
			return true
		}
		exe, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
		if err != nil {
			// A process of this user that cannot be looked into is not offered.
			return true
		}
		exe = filepath.Clean(strings.TrimSuffix(exe, " (deleted)"))
		return platformExecutables[filepath.Base(exe)] || exe == self && !strings.HasSuffix(self, ".test")
	}
	return ports.DevServers(out, platform, reservedPorts()), listening
}

// reservedPorts are the ports the deployment keeps from the preview (RUNTIME_RESERVED_PORTS, the gate's): never a dev
// server, whoever listens there.
func reservedPorts() map[int]bool {
	hidden := map[int]bool{}
	for _, p := range strings.Split(os.Getenv("RUNTIME_RESERVED_PORTS"), ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil && n > 0 {
			hidden[n] = true
		}
	}
	if gate := os.Getenv("RUNTIME_GATE_LISTEN"); gate != "" {
		if i := strings.LastIndexByte(gate, ':'); i >= 0 {
			if n, err := strconv.Atoi(gate[i+1:]); err == nil && n > 0 {
				hidden[n] = true
			}
		}
	}
	return hidden
}

// loopback is the address to reach a loopback host at ("" when the host is not one): localhost and its subdomains
// by name, 127.0.0.0/8 and ::1 as written.
func loopback(host string) string {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return "localhost"
	}
	lit := preview.LiteralAddress(host)
	switch {
	case lit.Family == 4 && strings.HasPrefix(lit.Address, "127."):
		return lit.Address
	case lit.Family == 6 && lit.Address == "::1":
		return lit.Address
	}
	return ""
}

// devServer says whether a loopback port is a dev server of the machine, and why not when it is not.
func (g *gate) devServer(port string) (ok bool, why string) {
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return false, "Not a port: " + port + "."
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.dev == nil || time.Since(g.portsAt) >= devServersFor {
		g.dev, g.listening = g.ports()
		g.portsAt = time.Now()
	}
	if g.dev[n] {
		return true, ""
	}
	if !g.listening[n] {
		return false, "Navigation failed: net::ERR_CONNECTION_REFUSED (nothing listens on port " + port + " of this machine)"
	}
	return false, "URL refused: port " + port + " of this machine is not a dev server; the agent's browser opens only the machine's dev servers on localhost"
}

// resolve is where host:port may be reached (the one address to connect to) or why it may not.
func (g *gate) resolve(host, port string) (addr, why string) {
	if at := loopback(host); at != "" {
		if ok, why := g.devServer(port); !ok {
			return "", why
		}
		return at, ""
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	g.mu.Lock()
	v, ok := g.verdicts[host]
	g.mu.Unlock()
	if ok && time.Since(v.at) < verdictFor {
		return v.addr, v.why
	}
	var r preview.Resolved
	if g.private[host] {
		r = preview.ResolveListed(host, g.lookup, g.local)
	} else {
		r = preview.ResolvePublic(host, g.lookup, g.local)
	}
	switch r.Error {
	case "":
		addr = r.Address
	case "not-found":
		why = "Navigation failed: net::ERR_NAME_NOT_RESOLVED"
	default:
		why = "URL refused: the address leads into the machine or a private network, which the agent's browser does not open"
	}
	g.mu.Lock()
	if len(g.verdicts) > 4096 {
		g.verdicts = map[string]verdict{}
	}
	g.verdicts[host] = verdict{addr: addr, why: why, at: time.Now()}
	g.mu.Unlock()
	return addr, why
}

// lsofDevServers is machineDevServers where there is neither ss nor /proc (macOS): lsof names this user's listeners
// and their programs.
func lsofDevServers(inHost bool) map[int]bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// #nosec G204 -- fixed arguments.
	out, err := exec.CommandContext(ctx, "lsof", "-nP", "+c", "0", "-iTCP", "-sTCP:LISTEN", "-a",
		"-u", strconv.Itoa(os.Getuid()), "-F", "pcn").Output()
	if err != nil {
		return map[int]bool{}
	}
	self := 0
	if inHost {
		self = os.Getpid()
	}
	return parseLsof(string(out), self, executableName())
}

func executableName() string {
	self, _ := os.Executable()
	if strings.HasSuffix(self, ".test") {
		return ""
	}
	return filepath.Base(self)
}

// parseLsof reads lsof -F pcn: a p line opens a process, c names its program, each n is one of its listeners.
func parseLsof(out string, self int, own string) map[int]bool {
	devs := map[int]bool{}
	reserved := reservedPorts()
	pid, command := 0, ""
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
			command = ""
		case 'c':
			command = line[1:]
		case 'n':
			i := strings.LastIndexByte(line, ':')
			n, err := strconv.Atoi(line[i+1:])
			if i < 0 || err != nil || n < 1 || n > 65535 || reserved[n] || pid == self {
				continue
			}
			if platformExecutables[command] || command == own && own != "" {
				continue
			}
			devs[n] = true
		}
	}
	return devs
}
