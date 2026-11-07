package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	browsertools "github.com/ly-solutions/harnessly/internal/agentbrowser/tools"
	"github.com/ly-solutions/harnessly/internal/browseract"
	"github.com/ly-solutions/harnessly/internal/coreapi/layoutapi"
	"github.com/ly-solutions/harnessly/internal/delegation"
	"github.com/ly-solutions/harnessly/internal/delivery"
	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/fileplace"
	"github.com/ly-solutions/harnessly/internal/imagegen"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
	"github.com/ly-solutions/harnessly/internal/paneauto"
	"github.com/ly-solutions/harnessly/internal/panel"
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

// cliDelegate is `harnessly tab create -t claude-code|codex-cli --prompt …` run in the terminal of a chat: the work goes
// to a chat nested under that chat, as delegate_task makes it, not to a loose tab of the workspace. The chat
// is the one that runs in the terminal the command names (the CLI sends its tmux session, as peers send does); a command
// from a terminal that is no chat's gets layoutapi.ErrNoParent and the tab is made the old way.
func (c *core) cliDelegate(ctx context.Context, req layoutapi.DelegateRequest) (layoutapi.DelegateResult, error) {
	v := c.v2.Load()
	if v == nil || v.delegation == nil || v.rt == nil {
		return layoutapi.DelegateResult{}, &layoutapi.ErrNoParent{Reason: "the machine's chats are not running"}
	}
	parent := v.rt.ConvOfSession(req.Session)
	if parent == "" {
		return layoutapi.DelegateResult{}, &layoutapi.ErrNoParent{Reason: "this terminal is not the agent of a chat (a plain terminal tab), so there is no chat to nest the new one under"}
	}
	rec, err := v.delegation.Delegate(ctx, parent, delegation.Task{Provider: wire.Provider(req.Provider), Model: req.Model,
		Effort: wire.Effort(req.Effort), Prompt: req.Prompt, Title: req.Title, Notify: req.Notify, Test: req.Test})
	if err != nil {
		var refusal *delegation.Refusal
		if errors.As(err, &refusal) {
			return layoutapi.DelegateResult{}, &layoutapi.DelegateRefused{Msg: refusal.Msg}
		}
		return layoutapi.DelegateResult{}, err
	}
	res := layoutapi.DelegateResult{ChatID: rec.ID, ParentID: parent, Provider: string(rec.Provider)}
	if rec.Model != nil {
		res.Model = rec.Model.ID
	}
	return res, nil
}

// chatTab is the layout tab a chat runs in, "" before it has one: the CLI's tab status, result, send and close name a
// chat it started by the chat's id.
func (c *core) chatTab(chat string) string {
	v := c.v2.Load()
	if v == nil || v.rt == nil || c.cfg.Tmux == nil {
		return ""
	}
	tab, err := legacyTabOf(v.rt.Record, c.cfg.Tmux.ParseSessionName, chat)
	if err != nil {
		return ""
	}
	return tab.TabID
}

// serveDelegation gives the agents' MCP server the delegation tools and wakes the parents until done.
func (v *v2Live) serveDelegation(done <-chan struct{}) {
	if v.delegation == nil {
		return
	}
	access := workspaceAccess(v.d)
	callers := &callerCache{panes: v.c.cfg.Tmux, convOf: v.rt.ConvOfSession}
	tools := func(r *http.Request) mcpserver.Toolset {
		id, why := callers.of(r)
		t := delegation.Tools{S: v.delegation, Caller: id, Why: why}
		hand := delivery.Tools{Access: access}
		pics := imagegen.Tools{S: imageJobs(v.d), Caller: id, Why: why, Allowed: func(p string) bool { return saveFolderOK(access, p, v.d.WorkspaceRoot, sharedProjects) }, Readable: pictureReadable(v.d, access)}
		if id != "" {
			hand.Handed = func(info delivery.Info) { v.openDelivered(id, info) }
		}
		if rec, ok := v.rt.Record(id); ok {
			t.Provider = rec.Provider
			hand.Cwd = rec.Cwd
			pics.Cwd, pics.Codex = rec.Cwd, rec.Provider == wire.ProviderCodex
		}
		place := fileplace.Tools{Store: v.selections, Access: access, Chat: id, Why: why, Show: v.showFilePlace}
		if rec, ok := v.rt.Record(id); ok {
			place.Cwd = rec.Cwd
		}
		browse := browseract.Tools{Hub: v.browser, Chat: id, Why: why}
		// The agent of a chat with apps stays in its wall: a chat it started or wrote to would run outside.
		if _, walled := v.d.CLI.AgentOf(r); walled {
			return toolsets{hand, pics, place, browse}
		}
		// Only a chat that works in a video project carries the video's tools.
		video := design.VideoTools{L: designAPI(v.d).L}
		if rec, ok := v.rt.Record(id); ok {
			video.Project = video.L.VideoProjectOf(rec.Cwd)
		}
		browser := browsertools.Tools{S: v.d.BrowserTools, Caller: id, Why: why}
		if rec, ok := v.rt.Record(id); ok {
			browser.Cwd = rec.Cwd
		}
		return toolsets{t, hand, pics, video, place, browser, browse}
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

// openDelivered opens a file the agent handed over in its conversation's pane, once: the request is kept with the
// conversation (the page applies it when it is there), as the agent's `open` is.
func (v *v2Live) openDelivered(conv string, info delivery.Info) {
	c := v.c
	if !info.Opens() || c.cfg.Tmux == nil || c.intents == nil || v.rt == nil {
		return
	}
	tab, err := legacyTabOf(v.rt.Record, c.cfg.Tmux.ParseSessionName, conv)
	if err != nil || tab.TabID == "" {
		return
	}
	if paneauto.OpenOnce(c.intents, tab.TabID, "files", info.Path) && c.sync != nil {
		c.sync.Broadcast(jsvalue.NewObject("type", "pane-intents", "tabId", tab.TabID))
	}
}

// showFilePlace opens a file in the pane of the chat's tab at a place, as the agent asked.
func (v *v2Live) showFilePlace(conv, path, place string) error {
	c := v.c
	if c.cfg.Tmux == nil || c.intents == nil || v.rt == nil {
		return errNoPane
	}
	tab, err := legacyTabOf(v.rt.Record, c.cfg.Tmux.ParseSessionName, conv)
	if err != nil || tab.TabID == "" {
		return errNoPane
	}
	c.intents.Add(tab.TabID, panel.Command{Kind: "open", Tab: "files", Target: path, Place: place}.JSON())
	if c.sync != nil {
		c.sync.Broadcast(jsvalue.NewObject("type", "pane-intents", "tabId", tab.TabID))
	}
	return nil
}

// toolsets are several groups of tools served as one.
type toolsets []mcpserver.Toolset

var _ mcpserver.Toolset = toolsets(nil)

func (ts toolsets) Instructions() string {
	parts := make([]string, 0, len(ts))
	for _, t := range ts {
		parts = append(parts, t.Instructions())
	}
	return strings.Join(parts, "\n\n")
}

func (ts toolsets) Tools() []any {
	var out []any
	for _, t := range ts {
		out = append(out, t.Tools()...)
	}
	return out
}

func (ts toolsets) Has(name string) bool {
	for _, t := range ts {
		if t.Has(name) {
			return true
		}
	}
	return false
}

func (ts toolsets) Call(ctx context.Context, name string, args *jsvalue.Object) *jsvalue.Object {
	for _, t := range ts {
		if t.Has(name) {
			return t.Call(ctx, name, args)
		}
	}
	return mcpserver.TextResult("no tool "+name, true)
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
