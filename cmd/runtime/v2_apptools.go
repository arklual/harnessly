package main

import (
	"context"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/apps"
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
			Conv: conv, Registry: reg, Storage: storage, Asks: appAsks, Widgets: widgets,
			Enabled: func() []string {
				rec, _ := v.rt.Record(conv)
				return rec.Apps
			},
			Open: func(target string) error { return v.openAppTab(conv, target) },
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
	set := previewOf(v.d)
	set.appTools.Store(&tools)
	defer set.appTools.CompareAndSwap(&tools, nil)
	<-done
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

// noAppTools lists the tools and refuses every call of an agent the runtime could not place in a chat, with the reason.
type noAppTools struct{ why string }

var _ mcpserver.Toolset = noAppTools{}

func (noAppTools) Instructions() string { return apps.HostTools{}.Instructions() }
func (noAppTools) Tools() []any         { return apps.HostTools{}.Tools() }
func (noAppTools) Has(name string) bool { return apps.HostTools{}.Has(name) }
func (n noAppTools) Call(context.Context, string, *jsvalue.Object) *jsvalue.Object {
	return mcpserver.TextResult("refused: "+n.why, true)
}
