package main

import (
	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/agentbrowser"
	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/paneauto"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// autoOpenPublisher is the hub as the conversations' actors publish to it, with the pane's watcher looking at the
// finished tool calls on the way (internal/paneauto): what the agent works on opens in the person's pane by itself.
type autoOpenPublisher struct {
	hub     *wire.Hub
	watch   *paneauto.Watcher
	browser *agentbrowser.ProfileAccess
}

var _ convo.Publisher = autoOpenPublisher{}

func (p autoOpenPublisher) Publish(ch wire.Channel, e wire.Event) int64 {
	if p.browser != nil {
		var err error
		switch ev := e.(type) {
		case wire.ConversationRemovedEv:
			err = p.browser.ConversationChanged(ev.ID, "", true)
		case wire.ConversationUpsertEv:
			project := ""
			if ev.Conv.ProjectID != nil {
				project = *ev.Conv.ProjectID
			}
			err = p.browser.ConversationChanged(ev.Conv.ID, project, false)
		}
		if err != nil {
			agent.Log("warn", "browser conversation cleanup failed", agent.F{K: "error", V: err.Error()})
		}
	}
	seq := p.hub.Publish(ch, e)
	if p.watch == nil {
		return seq
	}
	var entry wire.Entry
	switch ev := e.(type) {
	case wire.EntryUpdateEv:
		entry = ev.Entry
	case wire.StepEv:
		entry = ev.Entry
	default:
		return seq
	}
	if entry.T != wire.EntryTool || entry.OK == nil || !*entry.OK {
		return seq
	}
	if conv, ok := ch.ConvID(); ok {
		// Off the actor: the watcher reads the design library and writes the intents.
		go p.watch.Entry(conv, entry)
	}
	return seq
}

// newPaneWatcher is the watcher of a live v2 over the core's intents; nil without tmux (no Legacy tab, no pane).
func (v *v2Live) newPaneWatcher() *paneauto.Watcher {
	c := v.c
	if c.cfg.Tmux == nil || c.intents == nil {
		return nil
	}
	tmux := c.cfg.Tmux
	return paneauto.New(paneauto.Env{
		Targets:       panelTargets(),
		Browser:       !agentbrowser.ConfigFromEnv(v.d.DataDir).Off,
		BrowserServer: env("RUNTIME_MCP_SERVER", "harnessly"),
		DesignOf:      designAPI(v.d).L.DesignOfPath,
		Intents:       c.intents,
		TabOf: func(conv string) string {
			if v.rt == nil {
				return ""
			}
			tab, err := legacyTabOf(v.rt.Record, tmux.ParseSessionName, conv)
			if err != nil {
				return ""
			}
			return tab.TabID
		},
		Changed: func(tabID string) {
			if c.sync != nil {
				c.sync.Broadcast(jsvalue.NewObject("type", "pane-intents", "tabId", tabID))
			}
		},
	})
}
