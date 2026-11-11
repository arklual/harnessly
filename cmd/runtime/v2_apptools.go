package main

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/journal"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
	"github.com/ly-solutions/harnessly/internal/panel"
)

// appAsks are the choices agents put to people through app.ask, which the page lists and answers
// (routes_apps_bridge.go).
var appAsks = apps.NewAsks()

// serveAppTools gives the harnessly-app MCP server its tools until done: the host's app.* tools for the chat of the
// calling agent, which know the apps on in that chat and no others.
func (v *v2Live) serveAppTools(done <-chan struct{}) {
	c := v.c
	reg := apps.Open(v.d.DataDir)
	storage := apps.OpenStorage(v.d.DataDir)
	widgets := apps.OpenWidgets(v.d.DataDir)
	callers := &callerCache{panes: c.cfg.Tmux, convOf: v.rt.ConvOfSession}
	tools := func(r *http.Request) mcpserver.Toolset {
		conv, why := callers.of(r)
		if conv == "" {
			return noAppTools{why: why}
		}
		return apps.HostTools{
			Conv: conv, Registry: reg, Storage: storage, Asks: appAsks, Pages: appPages, Widgets: widgets,
			Enabled: func() []string {
				rec, _ := v.rt.Record(conv)
				return rec.Apps
			},
			Open: func(target string) error { return v.openAppTab(conv, target) },
			// An app turned on after the agent started is used live when it can be (v2_applive.go).
			Live: func(app string) string {
				rec, _ := v.rt.Record(conv)
				return liveAppsOf.hostRefusal(rec.Session)(app)
			},
			Check: func(ctx context.Context, app, view string) (apps.LoadCheck, error) {
				return runAppCheck(ctx, v.d, reg, apps.NewGuests(reg, storage), app, view)
			},
			Sinopia: &apps.SinopiaHost{
				Docs: sinopiaDocs(v.d), State: appSinopia,
				Tab: func(conv string) string {
					tab, err := legacyTabOf(v.rt.Record, c.cfg.Tmux.ParseSessionName, conv)
					if err != nil {
						return ""
					}
					return tab.TabID
				},
				Package: func(id string) *apps.Package {
					pkg, err := reg.Package(id)
					if err != nil {
						return nil
					}
					return pkg
				},
			},
		}
	}
	remote := v.appMCPTools(reg, callers)
	set := previewOf(v.d)
	set.appTools.Store(&tools)
	defer set.appTools.CompareAndSwap(&tools, nil)
	set.appMCP.Store(&remote)
	defer set.appMCP.CompareAndSwap(&remote, nil)
	<-done
}

// appMCPTools are the harnessly-app server's tools of the apps' own remote MCP servers: for the apps on in the
// calling chat that its agent did not start with and can use as it runs, each server's tools under the app's name, called
// from the machine with the host's credentials for the server. What a server refused or failed goes to the journal and
// to the app's log.
func (v *v2Live) appMCPTools(reg *apps.Registry, callers *callerCache) func(*http.Request) mcpserver.Toolset {
	cache := apps.NewRemoteTools()
	return func(r *http.Request) mcpserver.Toolset {
		conv, _ := callers.of(r)
		if conv == "" {
			return nil
		}
		return appMCPFor(v.d, reg, cache, func() []string {
			rec, _ := v.rt.Record(conv)
			launched := liveAppsOf.startedWith(rec.Session)
			var out []string
			for _, id := range rec.Apps {
				if !slices.Contains(launched, id) && liveAppsOf.refusal(id, launched) == "" {
					out = append(out, id)
				}
			}
			return out
		})
	}
}

// appMCPFor is the proxy of the remote servers of the apps on: each reached with the host's credentials for it.
func appMCPFor(d deps, reg *apps.Registry, cache *apps.RemoteTools, on func() []string) apps.AppMCP {
	return apps.AppMCP{
		Package: reg.Package, Cache: cache, Apps: on,
		ListServer: func(ctx context.Context, app, server string) ([]apps.RemoteTool, error) {
			raw, err := appServerDo(ctx, d, reg, app, server, listOp)
			if err != nil {
				return nil, err
			}
			var tools []apps.RemoteTool
			return tools, json.Unmarshal(raw, &tools)
		},
		CallServer: func(ctx context.Context, app, server, tool string, args map[string]any) (json.RawMessage, error) {
			return appServerDo(ctx, d, reg, app, server, toolOp(tool, args))
		},
		Failed: func(ctx context.Context, app, what string, err error) {
			why := toolFailure(err)
			reg.LogEventOnce(app, "", apps.ViewLogMCP, clip(what, 120)+" — "+why)
			journal.Add(journal.Fill(ctx, journal.Entry{Level: journal.Error, Source: journal.SourceRuntime,
				Event: "mcp.proxy", App: app, Tool: clip(what, 120), Text: why}))
		},
	}
}

// openAppTab asks the pane of the conversation's tab to open an app's tab, as `harnessly open app:<id>` does.
func (v *v2Live) openAppTab(conv, target string) error {
	c := v.c
	if c.cfg.Tmux == nil || c.intents == nil || v.rt == nil {
		return errNoPane
	}
	tab, err := legacyTabOf(v.rt.Record, c.cfg.Tmux.ParseSessionName, conv)
	if err != nil || tab.TabID == "" {
		return errNoPane
	}
	c.intents.Add(tab.TabID, panel.Command{Kind: "open", Tab: "app", Target: target}.JSON())
	if c.sync != nil {
		c.sync.Broadcast(jsvalue.NewObject("type", "pane-intents", "tabId", tab.TabID))
	}
	return nil
}

type paneError string

func (e paneError) Error() string { return string(e) }

const errNoPane = paneError("the chat has no pane")

// noAppTools is the server of an agent the runtime could not place in a chat: it offers the one tool that is always
// there (an agent placed a moment later must not depend on hearing that the tools changed) and nothing else
// (every Claude Code chat has the server, HRN-975, and one with no app on costs that one tool), and refuses a call by
// name with the reason.
type noAppTools struct{ why string }

var _ mcpserver.Toolset = noAppTools{}

func (noAppTools) Instructions() string { return "" }
func (noAppTools) Tools() []any         { return []any{apps.PageCallTool()} }
func (noAppTools) Has(name string) bool { return apps.HostTools{}.Has(name) }
func (n noAppTools) Call(context.Context, string, *jsvalue.Object) *jsvalue.Object {
	return mcpserver.TextResult("refused: "+n.why, true)
}
