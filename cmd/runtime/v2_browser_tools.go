package main

import (
	"errors"
	"net/http"

	browsertools "github.com/ly-solutions/harnessly/internal/agentbrowser/tools"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
)

func (c *core) browserTool(d deps) func(*http.Request, string, string, *jsvalue.Object) *jsvalue.Object {
	callers := &callerCache{panes: c.cfg.Tmux, convOf: func(session string) string {
		if v := c.v2.Load(); v != nil {
			return v.rt.ConvOfSession(session)
		}
		return ""
	}}
	return func(r *http.Request, id, name string, args *jsvalue.Object) *jsvalue.Object {
		if err := browserCaller(callers, r, id); err != nil {
			return mcpserver.TextResult(err.Error(), true)
		}
		if _, walled := d.CLI.AgentOf(r); walled {
			return mcpserver.TextResult("Browser tools are unavailable to walled chats", true)
		}
		v := c.v2.Load()
		if v == nil {
			return mcpserver.TextResult("Conversations are starting; retry in 2 seconds", true)
		}
		rec, ok := v.rt.Record(id)
		if !ok {
			return mcpserver.TextResult("Cannot identify this chat", true)
		}
		t := browsertools.Tools{S: d.BrowserTools, Caller: id, Cwd: rec.Cwd}
		return t.Call(r.Context(), name, args)
	}
}

// The CLI token authenticates the machine, not the chat named in a JSON body.
func browserCaller(callers *callerCache, r *http.Request, claimed string) error {
	actual, why := callers.of(r)
	if actual == "" {
		return errors.New("Cannot identify this chat by its connection: " + why)
	}
	if actual != claimed {
		return errors.New("The requested session does not match the chat of this connection")
	}
	return nil
}
