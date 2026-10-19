package layoutapi

import (
	"errors"

	"harnessly/internal/agents"
	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
	"harnessly/internal/machine"
	"harnessly/internal/nodejson"
	"harnessly/internal/state"
)

// GET, POST /api/workspace
func (a *api) workspaces(c *call) {
	switch c.r.Method {
	case "GET":
		c.reply(200, a.d.Store.Workspaces())
	case "POST":
		directory, _ := c.field("directory")
		name, _ := c.field("name")
		resume, _ := c.field("resumeSessionId")
		panelType, _ := c.field("panelType")
		var p state.Provider
		resumeID := ""
		if jsvalue.Truthy(resume) {
			pt := panelType
			if pt == nil {
				pt = "claude-code"
			}
			if p = providerOf(pt); p == "" {
				c.fail(400, "Unknown panel type for resume")
				return
			}
			if resumeID = stringOr(resume); !agents.IsUUID(resumeID) {
				c.fail(400, "Invalid session ID format")
				return
			}
			if u := a.availability(panelOf(p)); u != nil {
				c.unavailable(u)
				return
			}
		}
		dir := stringOr(directory)
		if dir == "" {
			dir = a.d.Home
		}
		var launch *agents.Launch
		layoutType := ""
		if p != "" {
			launch = a.launchForAgentSession(resumeID)
			layoutType = panelOf(p)
		}
		ws, err := a.d.Store.CreateWorkspace(dir, stringOr(name), layoutType)
		var e *state.Error
		if errors.As(err, &e) {
			c.fail(e.Status, e.Message)
			return
		}
		if err != nil {
			c.fail(500, err.Error())
			return
		}
		var tab *jsvalue.Object
		if l := a.d.Store.Layout(ws.String("id")); l != nil {
			for _, pane := range state.Panes(l) {
				if ts := state.Tabs(pane); len(ts) > 0 {
					tab = ts[0]
					break
				}
			}
		}
		if p != "" && tab != nil {
			p.WriteSessionID(tab, resumeID)
			if err := a.d.Store.UpdateTabAgentSessionID(tab.String("sessionName"), p, resumeID); err != nil {
				c.fail(500, err.Error())
				return
			}
			if launch != nil {
				if _, err := a.d.Store.UpdateTabMeta(tab.String("sessionName"), jsvalue.NewObject("launch", launchJSON(launch))); err != nil {
					c.fail(500, err.Error())
					return
				}
			}
		}
		if tab != nil {
			a.register(ws.String("id"), tab)
		}
		if p != "" && tab != nil {
			a.resumeLater(p, resumeID, ws.String("id"), tab, launch)
		}
		c.reply(200, ws)
	default:
		c.methodNotAllowed("GET, POST")
	}
}

// DELETE, PATCH /api/workspace/<id>
func (a *api) workspace(c *call) {
	id := c.param("workspaceId")
	switch c.r.Method {
	case "DELETE":
		err := a.d.Store.DeleteWorkspace(id)
		if !c.refusal(err) {
			must(err)
			c.end(204)
		}
	case "PATCH":
		name, hasName := c.field("name")
		if groupID, has := c.field("groupId"); has {
			var next *string
			switch g := groupID.(type) {
			case nil:
			case string:
				next = &g
			default:
				c.fail(400, "Invalid groupId")
				return
			}
			err := a.d.Store.SetWorkspaceGroup(id, next)
			if c.refusal(err) {
				return
			}
			must(err)
			if !hasName {
				c.reply(200, jsvalue.NewObject("ok", true))
				return
			}
		}
		if hasName {
			s, isStr := name.(string)
			if !isStr || jsvalue.TrimJS(s) == "" {
				c.fail(400, "name field required")
				return
			}
			ws, err := a.d.Store.RenameWorkspace(id, jsvalue.TrimJS(s))
			if !c.refusal(err) {
				must(err)
				c.reply(200, ws)
			}
			return
		}
		c.fail(400, "name or groupId required")
	default:
		c.methodNotAllowed("DELETE, PATCH")
	}
}

// PATCH /api/workspace/active
func (a *api) workspaceActive(c *call) {
	if c.r.Method != "PATCH" {
		c.methodNotAllowed("PATCH")
		return
	}
	body, _ := c.body.(*jsvalue.Object)
	if body == nil {
		body = jsvalue.NewObject()
	}
	must(a.d.Store.UpdateActive(body))
	c.reply(200, jsvalue.NewObject("ok", true))
}

// PATCH /api/workspace/reorder
func (a *api) workspaceReorder(c *call) {
	if c.r.Method != "PATCH" {
		c.methodNotAllowed("PATCH")
		return
	}
	var items []state.ReorderItem
	if v, _ := c.field("items"); len(asArray(v)) > 0 {
		for _, it := range asArray(v) {
			o, isObj := it.(*jsvalue.Object)
			if it == nil {
				panic("reorder item is null")
			}
			item := state.ReorderItem{ID: "undefined"}
			if id, has := o.Get("id"); isObj && has {
				item.ID = jscompat.String(id)
			}
			if g, has := o.Get("groupId"); has {
				switch x := g.(type) {
				case nil:
					item.HasGroup = true
				case string:
					item.GroupID, item.HasGroup = &x, true
				}
			}
			items = append(items, item)
		}
	} else if v, _ := c.field("workspaceIds"); len(asArray(v)) > 0 {
		for _, id := range asArray(v) {
			items = append(items, state.ReorderItem{ID: jscompat.String(id)})
		}
	}
	if items == nil {
		c.fail(400, "items array required")
		return
	}
	err := a.d.Store.ReorderWorkspaces(items)
	if !c.refusal(err) {
		must(err)
		c.reply(200, jsvalue.NewObject("ok", true))
	}
}

func asArray(v any) []any { a, _ := v.([]any); return a }

// GET /api/workspace/validate
func (a *api) workspaceValidate(c *call) {
	if c.r.Method != "GET" {
		c.methodNotAllowed("GET")
		return
	}
	dir := c.q("directory")
	if !dir.Truthy() {
		c.fail(400, "directory parameter required")
		return
	}
	if dir.Array() {
		c.raw(200, nodejson.Marshal(machine.DirMissing()))
		return
	}
	c.raw(200, nodejson.Marshal(machine.ValidateDirectory(dir.Values[0])))
}

// POST /api/workspace/group
func (a *api) groupCreate(c *call) {
	if c.r.Method != "POST" {
		c.methodNotAllowed("POST")
		return
	}
	name, _ := c.str("name")
	g, err := a.d.Store.CreateGroup(name)
	must(err)
	c.reply(200, g)
}

// DELETE, PATCH /api/workspace/group/<id>
func (a *api) group(c *call) {
	id := c.param("groupId")
	switch c.r.Method {
	case "DELETE":
		err := a.d.Store.UngroupGroup(id)
		if !c.refusal(err) {
			must(err)
			c.end(204)
		}
	case "PATCH":
		name, hasName := c.field("name")
		if collapsed, has := c.field("collapsed"); has {
			b, isBool := collapsed.(bool)
			if !isBool {
				c.fail(400, "collapsed must be boolean")
				return
			}
			err := a.d.Store.SetGroupCollapsed(id, b)
			if c.refusal(err) {
				return
			}
			must(err)
			if !hasName {
				c.reply(200, jsvalue.NewObject("ok", true))
				return
			}
		}
		if hasName {
			s, isStr := name.(string)
			if !isStr || jsvalue.TrimJS(s) == "" {
				c.fail(400, "name required")
				return
			}
			g, err := a.d.Store.RenameGroup(id, jsvalue.TrimJS(s))
			if !c.refusal(err) {
				must(err)
				c.reply(200, g)
			}
			return
		}
		c.fail(400, "name or collapsed required")
	default:
		c.methodNotAllowed("DELETE, PATCH")
	}
}

// PATCH /api/workspace/group/reorder
func (a *api) groupReorder(c *call) {
	if c.r.Method != "PATCH" {
		c.methodNotAllowed("PATCH")
		return
	}
	v, _ := c.field("groupIds")
	ids, isArr := v.([]any)
	if !isArr {
		c.fail(400, "groupIds array required")
		return
	}
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = jscompat.String(id)
	}
	err := a.d.Store.ReorderGroups(strs)
	if !c.refusal(err) {
		must(err)
		c.reply(200, jsvalue.NewObject("ok", true))
	}
}
