package apps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/journal"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
)

// AppMCP is the part of the harnessly-app server that proxies the remote MCP servers of the apps turned on in a chat
// after its agent started. Such an agent has no plugin of the app, so the host lists the servers' tools under
// names of its own (ProxyToolName: the app, the server, the tool) and runs a call from the machine with the host's
// credentials for the server, the way a view's hl.tools.call does. The list follows the apps on in the chat: the
// server's GET stream tells the agent when it changes (v2_applive.go), so an app turned off takes its tools along.
type AppMCP struct {
	// Apps are the apps whose servers are proxied: on in the chat, live, and not of the agent's launch.
	Apps func() []string
	// Package is an installed app's package (Registry.Package).
	Package func(app string) (*Package, error)
	// ListServer and CallServer reach one server of an app with the host's credentials for it (a connection, a secret, a token).
	ListServer func(ctx context.Context, app, server string) ([]RemoteTool, error)
	CallServer func(ctx context.Context, app, server, tool string, args map[string]any) (json.RawMessage, error)
	// Cache keeps the servers' lists between the agent's tools/list; nil keeps nothing.
	Cache *RemoteTools
	// Failed is told of a server whose list could not be had, and of a failed call (the journal, the app's log).
	Failed func(ctx context.Context, app, what string, err error)
}

var _ mcpserver.Toolset = AppMCP{}

// listWait is how long a tools/list of the agent waits for the apps' servers; a slower one is left out of that list.
const listWait = 15 * time.Second

func (AppMCP) Instructions() string { return "" }

// proxied is a tool the host lists: whose it is and what the server calls it.
type proxied struct {
	app, server, tool string
	def               any
}

func (a AppMCP) offered(ctx context.Context) []proxied {
	if a.Apps == nil || a.Package == nil || a.ListServer == nil {
		return nil
	}
	type job struct {
		app, version, name, server string
	}
	var jobs []job
	for _, app := range a.Apps() {
		pkg, err := a.Package(app)
		if err != nil || pkg.Manifest == nil {
			continue
		}
		for _, server := range ProxiedServers(pkg) {
			jobs = append(jobs, job{app: app, version: pkg.Manifest.Version, name: pkg.Manifest.Name, server: server})
		}
	}
	if len(jobs) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, listWait)
	defer cancel()
	lists := make([][]RemoteTool, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lists[i] = a.list(ctx, j.app, j.version, j.server)
		}()
	}
	wg.Wait()
	var out []proxied
	seen := map[string]bool{}
	for i, j := range jobs {
		for _, t := range lists[i] {
			name := ProxyToolName(j.app, j.server, t.Name)
			if t.Name == "" || seen[name] || (HostTools{}).Has(name) {
				continue
			}
			seen[name] = true
			out = append(out, proxied{app: j.app, server: j.server, tool: t.Name, def: toolDef(name, j.name, t)})
		}
	}
	return out
}

func (a AppMCP) list(ctx context.Context, app, version, server string) []RemoteTool {
	key := app + "\x00" + version + "\x00" + server
	if tools, ok := a.Cache.get(key); ok {
		return tools
	}
	tools, err := a.ListServer(ctx, app, server)
	if err != nil {
		a.Cache.put(key, nil, false)
		if a.Failed != nil {
			a.Failed(ctx, app, "tools/list "+server, err)
		}
		return nil
	}
	a.Cache.put(key, tools, true)
	return tools
}

// toolDef is the definition the agent reads: the server's own, under the host's name, its description saying whose.
func toolDef(name, appName string, t RemoteTool) any {
	desc := t.Description
	if appName != "" {
		desc = strings.TrimSpace("[" + appName + "] " + desc)
	}
	def := jsvalue.NewObject("name", name, "description", desc)
	schema := any(jsvalue.NewObject("type", "object"))
	if len(t.InputSchema) > 0 {
		if v, err := jsvalue.Parse(t.InputSchema); err == nil {
			if o, ok := v.(*jsvalue.Object); ok {
				schema = o
			}
		}
	}
	def.Set("inputSchema", schema)
	if len(t.Annotations) > 0 {
		if v, err := jsvalue.Parse(t.Annotations); err == nil {
			if o, ok := v.(*jsvalue.Object); ok {
				def.Set("annotations", o)
			}
		}
	}
	return def
}

func (a AppMCP) Tools() []any {
	tools := a.offered(context.Background())
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.def)
	}
	return out
}

func (a AppMCP) find(ctx context.Context, name string) (proxied, bool) {
	for _, t := range a.offered(ctx) {
		if ProxyToolName(t.app, t.server, t.tool) == name {
			return t, true
		}
	}
	return proxied{}, false
}

func (a AppMCP) Has(name string) bool {
	_, ok := a.find(context.Background(), name)
	return ok
}

func (a AppMCP) Call(ctx context.Context, name string, args *jsvalue.Object) *jsvalue.Object {
	// A call repeated after its answer was lost is not sent to the app's server a second time.
	if CallRepeats(ctx) {
		return refuse(LostCall)
	}
	t, ok := a.find(ctx, name)
	if !ok || a.CallServer == nil {
		return mcpserver.TextResult("refused: the tool "+name+" is not offered in this chat (its app is off, or its server did not list it)", true)
	}
	journal.SetApp(ctx, t.app)
	in := map[string]any{}
	if args != nil {
		_ = json.Unmarshal(jsvalue.Stringify(args, ""), &in)
	}
	raw, err := a.CallServer(ctx, t.app, t.server, t.tool, in)
	if err != nil {
		if a.Failed != nil {
			a.Failed(ctx, t.app, "tools/call "+t.server+"."+t.tool, err)
		}
		var be *BridgeError
		if errors.As(err, &be) {
			// The agent reads the code and the short word, and the host's hint when it has one: never the server's body.
			text := be.Code + ": " + be.Message
			if be.Hint != "" {
				text += " — " + be.Hint
			}
			return mcpserver.TextResult(text, true)
		}
		return mcpserver.TextResult("error: the app's server could not be reached", true)
	}
	v, err := jsvalue.Parse(raw)
	if o, ok := v.(*jsvalue.Object); err == nil && ok {
		return o
	}
	return mcpserver.TextResult("error: the app's server answered no tool result", true)
}

// ProxyToolName is the name the host lists a tool of an app's server under: <app>_<server>_<tool> in the letters MCP
// clients take ([A-Za-z0-9_-], at most 64), a longer one cut with a hash of the whole.
func ProxyToolName(app, server, tool string) string {
	full := PluginName(app) + "_" + server + "_" + tool
	var b strings.Builder
	for _, r := range full {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	name := b.String()
	if len(name) <= 64 {
		return name
	}
	sum := sha256.Sum256([]byte(full))
	return name[:55] + "_" + hex.EncodeToString(sum[:])[:8]
}

// RemoteTools keeps the tool lists of apps' servers for a while: a list for 5 minutes, a failure for 30 seconds.
type RemoteTools struct {
	mu   sync.Mutex
	kept map[string]remoteList
	now  func() time.Time
}

type remoteList struct {
	tools []RemoteTool
	until time.Time
}

func NewRemoteTools() *RemoteTools { return &RemoteTools{kept: map[string]remoteList{}, now: time.Now} }

func (c *RemoteTools) get(key string) ([]RemoteTool, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	l, ok := c.kept[key]
	if !ok || c.now().After(l.until) {
		return nil, false
	}
	return l.tools, true
}

func (c *RemoteTools) put(key string, tools []RemoteTool, ok bool) {
	if c == nil {
		return
	}
	keep := 5 * time.Minute
	if !ok {
		keep = 30 * time.Second
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.kept[key] = remoteList{tools: tools, until: c.now().Add(keep)}
}

// Forget drops the lists of an app's servers: its package changed, or it was turned on again.
func (c *RemoteTools) Forget(app string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.kept {
		if strings.HasPrefix(k, app+"\x00") {
			delete(c.kept, k)
		}
	}
}
