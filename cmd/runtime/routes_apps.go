package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jsonlog"
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
	// InstructionsBytes is what the app's instructions weigh in the prompt; the page shows it as tokens (bytes / 4).
	InstructionsBytes int `json:"instructionsBytes"`
	// Command is the slash command that turns the app on in a chat (activation.on "command:/x"); empty when none.
	Command string `json:"command,omitempty"`
	// Always: the app is on in every new Claude chat of the machine (activation.on "always"; a team's app is not).
	Always bool `json:"always,omitempty"`
	// PluginParts are the hooks, agents and other parts of a Claude Code plugin a trusted package carries (the others
	// may not): the install's permission request and the app's «О приложении» list them.
	PluginParts []string `json:"pluginParts"`
	// Sinopia is what the app puts into the document editor (panels, selection items, bindings, checks); the page shows
	// them only in a document the app is on in.
	Sinopia *apps.Sinopia `json:"sinopia,omitempty"`
	Dev     bool          `json:"dev,omitempty"`
	// Setup: the app has settings or secrets, so «О приложении» draws their form. Waiting are the required secrets that
	// have no value yet: the app is «ждёт секрет».
	Setup   bool     `json:"setup"`
	Waiting []string `json:"waiting"`
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
		v.Command = m.Command()
		v.Always = !a.Team && m.HasTarget(apps.TargetChat) && m.ActivatesOn(apps.OnAlways)
		if sin, err := m.SinopiaOf(); err == nil && (len(sin.Panels) > 0 || len(sin.Selection) > 0 || sin.Bindings != nil || len(sin.Checks) > 0) {
			v.Sinopia = &sin
		}
	}
	if v.Permissions == nil {
		v.Permissions = []string{}
	}
	v.Waiting = []string{}
	if m := a.Manifest; m != nil {
		v.Setup = len(m.Settings) > 0 || len(m.Secrets) > 0
	}
	v.MCPServers = []string{}
	v.PluginParts = []string{}
	if pkg, _ := apps.CachedPackage(a.Dir); pkg != nil {
		for name := range pkg.MCP {
			v.MCPServers = append(v.MCPServers, name)
		}
		slices.Sort(v.MCPServers)
		v.PluginParts = append(v.PluginParts, pkg.PluginParts...)
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
	v.InstructionsBytes = apps.InstructionsBytes(a)
	if withDir {
		v.Dir = a.Dir
	}
	return v
}

func appRoutes(d deps, c *core) []gate.Route {
	reg := apps.Open(d.DataDir)
	// What the apps' MCP calls may reach besides the public internet: the deployment's internal hosts and root
	// certificates. A bad CA file is logged and the calls keep the system's roots.
	network, err := apps.NetworkFromEnv(os.Getenv)
	if err != nil {
		jsonlog.Log("warn", "apps: the network of the apps' calls", jsonlog.F{K: "error", V: err.Error()})
	}
	apps.SetNetwork(network)
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
	installShareGate(d, reg)
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
			v := viewOfApp(a, withDir)
			if a.Manifest != nil && len(a.Manifest.Secrets) > 0 {
				store := safeFor(d).store
				v.Waiting = a.Manifest.WaitingFor(func(name string) bool { return store.HasApp(a.ID, name) })
			}
			out = append(out, v)
		}
		return out, nil
	}
	viewServer := apps.NewViewServer(reg)
	widgets := apps.OpenWidgets(d.DataDir)
	viewServer.Widgets = widgets
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
		case errors.Is(err, apps.ErrCommandTaken):
			secretJSON(w, http.StatusConflict, map[string]any{"error": "command-taken", "message": err.Error()})
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
	// context: the answer of an app's agent.context tool for the chat of a tab (harnessly hook app-context). The app must
	// be on in the chat and name the tool in its context; the call is the one a view would make, so the same wall applies.
	appContext := func(w http.ResponseWriter, r *http.Request) {
		if !cliOK(w, r, http.MethodPost) {
			return
		}
		var b struct {
			Session string `json:"session"`
			App     string `json:"app"`
			ID      string `json:"id"`
		}
		if !readSecretBody(r, &b) || b.App == "" || b.ID == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		rec, ok := chats.record(chats.ofSession(b.Session))
		if !ok || !slices.Contains(rec.Apps, b.App) {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "no-chat"})
			return
		}
		pkg, err := reg.Package(b.App)
		if err != nil {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "no-app"})
			return
		}
		for _, item := range apps.ContextItems(pkg.Manifest) {
			if item.ID != b.ID {
				continue
			}
			server, name, err := pkg.CanCallTool(item.Tool)
			if err != nil {
				break
			}
			result, err := apps.CallTool(r.Context(), pkg.MCP[server], name, nil)
			if err != nil {
				secretJSON(w, http.StatusBadGateway, map[string]string{"error": "tool-failed"})
				return
			}
			secretJSON(w, http.StatusOK, map[string]string{"text": apps.ContextText(result)})
			return
		}
		secretJSON(w, http.StatusNotFound, map[string]string{"error": "no-context"})
	}

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
		// An app over the chat's budget is still on: the answer says so, and the person decides what to take off.
		answer := map[string]any{"conversation": conv, "apps": next}
		if b.On {
			total := 0
			for _, id := range next {
				if a, err := reg.Get(id); err == nil {
					total += apps.InstructionsBytes(a)
				}
			}
			if warning := apps.ChatBudgetWarning(total); warning != "" {
				answer["warnings"] = []string{warning}
			}
		}
		secretJSON(w, http.StatusOK, answer)
	}

	routes := append(appBridgeRoutes(d, reg, storage, viewServer, chats), appAskRoutes(d, appAsks)...)
	routes = append(routes, appSetupRoutes(d, reg, storage)...)
	routes = append(routes, appMCPProxyRoute(d, reg))
	routes = append(routes, sinopiaRoutes(d, reg, viewServer)...)
	routes = append(routes, kindRoutes(d, reg)...)
	routes = append(routes, appWidgetRoutes(d, reg, widgets, chats)...)
	routes = append(routes, helpRoutes(d)...)
	routes = append(routes, appPublicRoutes(d, reg, version)...)
	routes = append(routes, appShareRoutes(d, reg, guests, version)...)
	routes = append(routes, []gate.Route{
		{Match: is(platformAPI + "/installed-apps"), Handler: http.HandlerFunc(page)},
		{Match: is("/agents" + platformAPI + "/installed-apps"), Handler: http.HandlerFunc(page)},
		{Match: is("/api/cli/apps"), Handler: http.HandlerFunc(cliList)},
		{Match: is("/api/cli/apps/install"), Handler: http.HandlerFunc(install)},
		{Match: is("/api/cli/apps/remove"), Handler: http.HandlerFunc(remove)},
		{Match: is("/api/cli/apps/enable"), Handler: http.HandlerFunc(enable)},
		{Match: is("/api/cli/apps/context"), Handler: http.HandlerFunc(appContext)},
	}...)
	return refuseAgentAppChanges(d.CLI, routes)
}
