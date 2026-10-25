package main

import (
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/delegation"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
	"github.com/ly-solutions/harnessly/internal/proc"
	"github.com/ly-solutions/harnessly/internal/providers"
	"github.com/ly-solutions/harnessly/internal/tmux"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Delegation (internal/delegation): the agents' MCP server gets the tools to start, read, message and wait for other
// agents' chats once the conversations run. Which chat calls is found by the connection, not taken from the call: the
// MCP request comes over loopback from the agent's own process, so the client's end of the socket is held by a process
// below the pane of exactly one conversation. Nothing the agent sends can name another chat as itself.

// delegationSweep is how often the delegated chats are looked at for the parents that asked to be woken.
const delegationSweep = 2 * time.Second

func newDelegation(v *v2Live, settings wire.RuntimeSettingsStore) *delegation.Service {
	return &delegation.Service{
		Convs: v.rt,
		Settings: func() wire.DelegationSettings {
			if settings == nil {
				return wire.DefaultDelegation
			}
			return settings.RuntimeSettings().DelegationOf()
		},
		Models: v.machine.modelsOf,
		ProfileModels: func(profile string) []string {
			_, _, models, _ := providers.Launcher{Store: v.profiles}.Lookup(profile)
			return models
		},
		Self: peerSelf(os.Getenv),
		Book: &delegation.Book{Path: filepath.Join(v.d.DataDir, "v2", "delegations.json")},
		Log: func(msg string, kv ...any) {
			fs := make([]agent.F, 0, len(kv)/2)
			for i := 0; i+1 < len(kv); i += 2 {
				k, _ := kv[i].(string)
				fs = append(fs, agent.F{K: k, V: kv[i+1]})
			}
			agent.Log("warn", msg, fs...)
		},
	}
}

// serveDelegation gives the agents' MCP server the delegation tools and wakes the parents until done.
func (v *v2Live) serveDelegation(done <-chan struct{}) {
	if v.delegation == nil {
		return
	}
	callers := &callerCache{panes: v.c.cfg.Tmux, convOf: v.rt.ConvOfSession}
	tools := func(r *http.Request) mcpserver.Toolset {
		id, why := callers.of(r)
		t := delegation.Tools{S: v.delegation, Caller: id, Why: why}
		if rec, ok := v.rt.Record(id); ok {
			t.Provider = rec.Provider
		}
		return t
	}
	set := previewOf(v.d)
	set.agentTools.Store(&tools)
	defer set.agentTools.CompareAndSwap(&tools, nil)
	tick := time.NewTicker(delegationSweep)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return
		case <-tick.C:
			v.delegation.Sweep()
		}
	}
}

// callerCache finds the conversation whose agent made a request, by the connection. Each call reads which socket the
// connection is again; what is kept is the conversation found for that socket (its inode) and the process below the
// chat's pane that held it, checked to still hold it on every call. A later connection over the same addresses, a
// socket of its own, is looked up anew.
type callerCache struct {
	panes  *tmux.Tmux
	convOf func(session string) string

	mu   sync.Mutex
	seen map[string]cachedCaller // by socket inode
}

type cachedCaller struct {
	conv string
	pid  int // the process below the conversation's pane that holds the socket
	at   time.Time
}

// callerKept is how long a socket is taken to stay with the conversation it was found for (an agent's MCP client
// keeps its connection open between calls); after it a fresh look costs a few reads.
const callerKept = 30 * time.Second

func (c *callerCache) of(r *http.Request) (string, string) {
	if c.panes == nil {
		return "", "this machine runs no terminals, so no chat's agent can be told by its connection"
	}
	inode, why := socketOf(r)
	if inode == "" {
		return "", why
	}
	c.mu.Lock()
	hit, ok := c.seen[inode]
	c.mu.Unlock()
	if ok && time.Since(hit.at) < callerKept && proc.HoldsSocket(hit.pid, inode) {
		return hit.conv, ""
	}
	conv, pid, why := c.find(inode)
	if conv == "" {
		return "", why
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen == nil || len(c.seen) > 256 {
		c.seen = map[string]cachedCaller{}
	}
	c.seen[inode] = cachedCaller{conv: conv, pid: pid, at: time.Now()}
	return conv, ""
}

// socketOf is the inode of the client's end of the request's loopback connection, matched on both whole addresses.
func socketOf(r *http.Request) (string, string) {
	client, err := netip.ParseAddrPort(r.RemoteAddr)
	local, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	server, _ := local.(*net.TCPAddr)
	if err != nil || server == nil || !client.Addr().IsLoopback() {
		return "", "the call did not come over this machine's loopback"
	}
	inode, ok := proc.SocketInode(client, server.AddrPort())
	if !ok {
		return "", "the call's connection is not one socket of this machine"
	}
	return inode, ""
}

// find is the conversation below whose pane a process holds the socket, and that process.
func (c *callerCache) find(inode string) (string, int, string) {
	panes, ok := c.panes.ReadAllPanes()
	if !ok {
		return "", 0, "tmux did not answer"
	}
	for session, p := range panes {
		conv := c.convOf(session)
		if conv == "" || p.PID <= 0 {
			continue
		}
		for _, pid := range proc.Descendants(p.PID) {
			if proc.HoldsSocket(pid, inode) {
				return conv, pid, ""
			}
		}
	}
	return "", 0, "the call comes from a process outside the machine's chats"
}
