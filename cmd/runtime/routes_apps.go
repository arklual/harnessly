package main

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The apps installed on this machine (internal/apps). Who may call what:
//
//	GET <platform>/installed-apps        the person's page: a page session
//	GET /api/cli/apps, POST /api/cli/apps/install|remove|enable
//	                                     an agent of this machine or the person's shell: the cli-token (harnessly app)
//
// An install reads a folder of this machine, so only the cli-token installs: a page cannot name a path.

// appView is an installed app as the page and the CLI see it.
type appView struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Icon        string         `json:"icon,omitempty"`
	Version     string         `json:"version"`
	Trust       string         `json:"trust"`
	Targets     []string       `json:"targets"`
	Permissions []string       `json:"permissions"`
	Views       []appViewEntry `json:"views"`
	// MCPServers are the names of the servers in the package's .mcp.json: a card's tool is the app's own only when it
	// comes from one of them.
	MCPServers []string `json:"mcpServers"`
	// Sinopia is what the app puts into the document editor (panels, selection items, bindings, checks); the page shows
	// them only in a document the app is on in.
	Sinopia *apps.Sinopia `json:"sinopia,omitempty"`
	Dev     bool          `json:"dev,omitempty"`
	// Team: the app is the team's, installed for everybody by an owner; Channel is the channel an install follows.
	Team        bool   `json:"team,omitempty"`
	Channel     string `json:"channel,omitempty"`
	Dir         string `json:"dir,omitempty"`
	InstalledAt int64  `json:"installedAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

// appViewEntry is a view of an app as the page opens it: Path is its entry on the view server (/<app>/<file>), URL the
// address of one the app's own server hosts.
type appViewEntry struct {
	ID      string   `json:"id"`
	Title   string   `json:"title,omitempty"`
	Targets []string `json:"targets"`
	Height  string   `json:"height,omitempty"`
	Path    string   `json:"path,omitempty"`
	URL     string   `json:"url,omitempty"`
}

func viewOfApp(a apps.Installed, withDir bool) appView {
	v := appView{ID: a.ID, Version: a.Version, Dev: a.Dev, Team: a.Team, Channel: a.Channel, InstalledAt: a.InstalledAt, UpdatedAt: a.UpdatedAt}
	if m := a.Manifest; m != nil {
		v.Name, v.Description, v.Icon, v.Trust = m.Name, m.Description, m.Icon, m.Trust
		v.Targets, v.Permissions = m.Targets, m.Permissions
		if sin, err := m.SinopiaOf(); err == nil && (len(sin.Panels) > 0 || len(sin.Selection) > 0 || sin.Bindings != nil || len(sin.Checks) > 0) {
			v.Sinopia = &sin
		}
	}
	if v.Permissions == nil {
		v.Permissions = []string{}
	}
	v.MCPServers = []string{}
	if pkg, _ := apps.LoadPackage(a.Dir); pkg != nil {
		for name := range pkg.MCP {
			v.MCPServers = append(v.MCPServers, name)
		}
		slices.Sort(v.MCPServers)
	}
	v.Views = []appViewEntry{}
	if m := a.Manifest; m != nil {
		for _, mv := range m.Views {
			e := appViewEntry{ID: mv.ID, Title: mv.Title, Targets: mv.Targets, Height: mv.Height}
			if strings.HasPrefix(mv.Entry, "https://") {
				// A view at an address of the app's own server is framed only when its host is listed in network.frames.
				if u, ok := m.FrameURL(mv.Entry); ok {
					e.URL = u
				}
			} else if p, err := m.ViewPath(mv.ID); err == nil {
				e.Path = p
			}
			v.Views = append(v.Views, e)
		}
	}
	if withDir {
		v.Dir = a.Dir
	}
	return v
}

func appRoutes(d deps, c *core) []gate.Route {
	reg := apps.Open(d.DataDir)
	version := env("AGENT_VERSION", agent.BuildVersion)
	chats := appChats{
		ofSession: func(session string) string { id, _ := convOf(c, session); return id },
		record: func(id string) (wire.Conversation, bool) {
			rt := (peerLocal{c: c}).rt()
			if rt == nil {
				return wire.Conversation{}, false
			}
			return rt.Record(id)
		},
		do: func(ctx context.Context, cmd wire.Command) wire.CmdResult {
			rt := (peerLocal{c: c}).rt()
			if rt == nil {
				return wire.Refused(wire.RefuseUnsupported)
			}
			return rt.Do(ctx, "cli", cmd)
		},
	}
	installLock(d, reg)
	return appRouteList(d, reg, version, chats)
}

// appChats is what the routes need of the conversations: the one a tmux session runs, a record, the command entry.
type appChats struct {
	ofSession func(session string) string
	record    func(id string) (wire.Conversation, bool)
	do        func(ctx context.Context, cmd wire.Command) wire.CmdResult
}

// appRouteList is appRoutes with its parts given: the registry, the host version and the conversations.
func appRouteList(d deps, reg *apps.Registry, version string, chats appChats) []gate.Route {
	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	cliOK := func(w http.ResponseWriter, r *http.Request, method string) bool {
		if d.CLI == nil || !d.CLI.OK(r) {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
			return false
		}
		if r.Method != method {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return false
		}
		return true
	}
	list := func(withDir bool) ([]appView, error) {
		installed, err := reg.List()
		if err != nil {
			return nil, err
		}
		out := make([]appView, 0, len(installed))
		for _, a := range installed {
			out = append(out, viewOfApp(a, withDir))
		}
		return out, nil
	}
	viewServer := apps.NewViewServer(reg)
	storage := apps.OpenStorage(d.DataDir)
	guests := apps.NewGuests(reg, storage)
	wireAppsEvents(d, reg, guests)
	failed := func(w http.ResponseWriter, err error) {
		agent.Log("warn", "apps: "+err.Error())
		secretJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed"})
	}

	page := func(w http.ResponseWriter, r *http.Request) {
		if sameorigin.Refuse(w, r) {
			return
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}
		if r.Method != http.MethodGet {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		views, err := list(false)
		if err != nil {
			failed(w, err)
			return
		}
		// The views' loopback port starts with the first page that lists apps; the page opens a preview slot on it.
		port, err := viewServer.EnsureServer()
		if err != nil {
			failed(w, err)
			return
		}
		appViewPort.Store(int64(port))
		secretJSON(w, http.StatusOK, map[string]any{"apps": views, "viewPort": port, "agentWalled": agentWalled(reg.Root)})
	}

	cliList := func(w http.ResponseWriter, r *http.Request) {
		if !cliOK(w, r, http.MethodGet) {
			return
		}
		views, err := list(true)
		if err != nil {
			failed(w, err)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"apps": views})
	}

	install := func(w http.ResponseWriter, r *http.Request) {
		if !cliOK(w, r, http.MethodPost) {
			return
		}
		var b struct {
			Dir string `json:"dir"`
			Dev bool   `json:"dev"`
		}
		if !readSecretBody(r, &b) || !strings.HasPrefix(b.Dir, "/") {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		got, problems, err := reg.Install(b.Dir, apps.InstallOptions{Dev: b.Dev, HostVersion: version})
		if problems == nil {
			problems = apps.Problems{}
		}
		switch {
		case errors.Is(err, apps.ErrInvalid):
			secretJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "invalid", "problems": problems})
			return
		case errors.Is(err, apps.ErrKindTaken):
			secretJSON(w, http.StatusConflict, map[string]any{"error": "kind-taken", "message": err.Error()})
			return
		case errors.Is(err, apps.ErrHostVersion), errors.Is(err, apps.ErrPluginName):
			secretJSON(w, http.StatusConflict, map[string]any{"error": "host-version", "message": err.Error()})
			return
		case err != nil:
			failed(w, err)
			return
		}
		agent.Log("info", "apps: installed", agent.F{K: "id", V: got.ID}, agent.F{K: "version", V: got.Version}, agent.F{K: "dev", V: got.Dev})
		secretJSON(w, http.StatusOK, map[string]any{"app": viewOfApp(got, true), "problems": problems})
	}

	remove := func(w http.ResponseWriter, r *http.Request) {
		if !cliOK(w, r, http.MethodPost) {
			return
		}
		var b struct {
			ID string `json:"id"`
		}
		if !readSecretBody(r, &b) || b.ID == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		if err := reg.Remove(b.ID); errors.Is(err, apps.ErrNotInstalled) {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
			return
		} else if err != nil {
			failed(w, err)
			return
		}
		// The links to the app's views end with it, and the cluster is told.
		if _, err := guests.RevokeApp(b.ID); err != nil {
			agent.Log("warn", "apps: links of a removed app not ended", agent.F{K: "id", V: b.ID}, agent.F{K: "error", V: err.Error()})
		}
		agent.Log("info", "apps: removed", agent.F{K: "id", V: b.ID})
		secretJSON(w, http.StatusOK, map[string]any{"removed": b.ID})
	}

	// enable turns an app on or off in the chat of the calling tab, through the same command the page sends; a tab
	// changes its own chat only. Turning an app off from the tab is refused in a chat that has apps (use-page): that is the
	// page's toggle; the agent of such a chat is also kept from running the command by its guard.
	enable := func(w http.ResponseWriter, r *http.Request) {
		if !cliOK(w, r, http.MethodPost) {
			return
		}
		var b struct {
			Session string `json:"session"`
			ID      string `json:"id"`
			On      bool   `json:"on"`
		}
		if !readSecretBody(r, &b) || b.ID == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		conv := chats.ofSession(b.Session)
		if conv == "" {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "no-chat"})
			return
		}
		rec, ok := chats.record(conv)
		if !ok {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "no-chat"})
			return
		}
		// A chat with apps is the person's to take them out of: the page's toggle does it. A tab (the agent's, or a
		// script it ran) may add one, never turn one off.
		if !b.On && len(rec.Apps) > 0 {
			secretJSON(w, http.StatusConflict, map[string]string{"error": "use-page"})
			return
		}
		next := slices.DeleteFunc(slices.Clone(rec.Apps), func(id string) bool { return id == b.ID })
		if b.On {
			next = append(next, b.ID)
		}
		if next == nil {
			next = []string{}
		}
		res := chats.do(r.Context(), wire.ConversationAppsCmd{OpID: "cli-apps-" + strconv.FormatInt(time.Now().UnixNano(), 36), ID: conv, Apps: next})
		if res.R != wire.ResultAccepted {
			status := http.StatusConflict
			if res.Code == wire.RefuseNotFound {
				status = http.StatusNotFound
			}
			secretJSON(w, status, map[string]string{"error": string(res.Code)})
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"conversation": conv, "apps": next})
	}

	routes := append(appBridgeRoutes(d, reg, storage, viewServer, chats), appAskRoutes(d, appAsks)...)
	routes = append(routes, sinopiaRoutes(d, reg, viewServer)...)
	routes = append(routes, kindRoutes(d, reg)...)
	routes = append(routes, appPublicRoutes(d, reg, version)...)
	routes = append(routes, appShareRoutes(d, reg, guests, version)...)
	routes = append(routes, []gate.Route{
		{Match: is(platformAPI + "/installed-apps"), Handler: http.HandlerFunc(page)},
		{Match: is("/agents" + platformAPI + "/installed-apps"), Handler: http.HandlerFunc(page)},
		{Match: is("/api/cli/apps"), Handler: http.HandlerFunc(cliList)},
		{Match: is("/api/cli/apps/install"), Handler: http.HandlerFunc(install)},
		{Match: is("/api/cli/apps/remove"), Handler: http.HandlerFunc(remove)},
		{Match: is("/api/cli/apps/enable"), Handler: http.HandlerFunc(enable)},
	}...)
	return refuseAgentAppChanges(d.CLI, routes)
}
