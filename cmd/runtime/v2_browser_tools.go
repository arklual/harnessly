package main

import (
	"net/http"

	browsertools "github.com/ly-solutions/harnessly/internal/agentbrowser/tools"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
)

func (c *core) browserTool(d deps) func(*http.Request, string, string, *jsvalue.Object) *jsvalue.Object {
	return func(r *http.Request, id, name string, args *jsvalue.Object) *jsvalue.Object {
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
