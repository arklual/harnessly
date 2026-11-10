package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/egress"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jsonlog"
	"github.com/ly-solutions/harnessly/internal/mcpconn"
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
	// Branding is what the app sets for the `branding` slot; the page shows it only with HARNESSLY_UI_SLOTS on.
	Branding *apps.Branding `json:"branding,omitempty"`
	// MCPServers are the names of the servers in the package's .mcp.json: a card's tool is the app's own only when it
	// comes from one of them.
	MCPServers []string `json:"mcpServers"`
	// InstructionsBytes is what the app's instructions weigh in the prompt; the page shows it as tokens (bytes / 4).
	InstructionsBytes int `json:"instructionsBytes"`
	// Command is the slash command that turns the app on in a chat (activation.on "command:/x"); empty when none.
	Command string `json:"command,omitempty"`
	// Always: the app is on in every new chat of the machine (activation.on "always"; a team's app is not).
	Always bool `json:"always,omitempty"`
	// PluginParts are the hooks, agents and other parts of a Claude Code plugin a trusted package carries (the others
	// may not): the install's permission request and the app's «О приложении» list them.
	PluginParts []string `json:"pluginParts"`
	// Sinopia is what the app puts into the document editor (panels, selection items, bindings, checks); the page shows
	// them only in a document the app is on in.
	Sinopia *apps.Sinopia `json:"sinopia,omitempty"`
	Dev     bool          `json:"dev,omitempty"`
	// Commands are the app's chat.commands for the «/» palette of the page, each with its description.
	Commands []apps.ChatCommand `json:"commands"`
	// The app's own parts of the chat's interface: lines of an empty chat, buttons at the field, links it shows a card for.
	Starters []apps.Starter     `json:"starters,omitempty"`
	Buttons  []apps.ChatButton  `json:"buttons,omitempty"`
	Links    []apps.LinkCard    `json:"links,omitempty"`
	Mentions *apps.ChatMentions `json:"mentions,omitempty"`
	// Setup: the app has settings or secrets, so «О приложении» draws their form. Waiting are the required secrets that
	// have no value yet: the app is «ждёт секрет».
	Setup   bool     `json:"setup"`
	Waiting []string `json:"waiting"`
	// AppState is ready, waiting, failed or disabled, with the reason the page says in words.
	apps.AppState
	// WaitingConnections are the connections (catalog ids) the app's servers name that the person has not made or not
	// signed in to: the app is «ждёт вход», its «О приложении» offers «Подключить». Its chat does not fail; those servers
	// are left out of the launch.
	WaitingConnections []string `json:"waitingConnections"`
	// Team: the app is the team's, installed for everybody by an owner; Channel is the channel an install follows.
	Team bool `json:"team,omitempty"`
	// Env: the app comes with the organization's environment; Locked: it cannot be turned off; Muted: the person
	// turned it off.
	Env         bool   `json:"env,omitempty"`
	Locked      bool   `json:"locked,omitempty"`
	Muted       bool   `json:"muted,omitempty"`
	Channel     string `json:"channel,omitempty"`
	Dir         string `json:"dir,omitempty"`
	InstalledAt int64  `json:"installedAt"`
	UpdatedAt   int64  `json:"updatedAt"`
	// Access is everything the app reaches, from the most dangerous, in the person's words.
	Access []apps.Access `json:"access"`
	// Granted are the keys of Access the app is given now: each of them can be taken back alone.
	Granted []string `json:"granted"`
	// Consent: the install waits for the person to agree to all of Access (a first install nobody clicked through).
	Consent bool `json:"consent,omitempty"`
	// PermissionsOff: the apps' permissions are off on this machine, so the page shows no rights of the app.
	PermissionsOff bool `json:"permissionsOff,omitempty"`
	// AskedIn is the chat whose agent installed what waits for consent: its card shows there.
	AskedIn string `json:"askedIn,omitempty"`
	// Pending is a newer version that reaches more and waits for the person's word; its Access is only what it adds.
	Pending *pendingView `json:"pending,omitempty"`
	// Off: the person turned the app off with its switch.
	Off bool `json:"off,omitempty"`
	// Source is where it was installed from: a folder, an address, a catalog.
	Source string `json:"source,omitempty"`
}

// pendingView is a version waiting for consent as the install screen shows it.
type pendingView struct {
	Version string        `json:"version"`
	Access  []apps.Access `json:"access"`
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
	// The slots: the name, icon, place and objects of a view that fills one.
	Label   string   `json:"label,omitempty"`
	Icon    string   `json:"icon,omitempty"`
	Order   int      `json:"order,omitempty"`
	Objects []string `json:"objects,omitempty"`
}

// uiSlotsOn says whether the interface's slots are on (HARNESSLY_UI_SLOTS=on). Off, the page lists no slot view and no
// branding, and the runtime refuses a view session for a slot, so an installed app that names slots does nothing.
func uiSlotsOn() bool { return strings.EqualFold(os.Getenv("HARNESSLY_UI_SLOTS"), "on") }

// withoutSlots takes the slot views and the branding out of the apps a page is given when the slots are off.
func withoutSlots(list []appView) []appView {
	for i := range list {
		list[i].Branding = nil
		list[i].Targets = slices.DeleteFunc(slices.Clone(list[i].Targets), apps.IsSlot)
		views := list[i].Views[:0:0]
		for _, v := range list[i].Views {
			if v.Targets = slices.DeleteFunc(slices.Clone(v.Targets), apps.IsSlot); len(v.Targets) > 0 {
				views = append(views, v)
			}
		}
		list[i].Views = views
	}
	return list
}

func viewOfApp(a apps.Installed, withDir bool) appView {
	v := appView{ID: a.ID, Version: a.Version, Dev: a.Dev, Team: a.Team, Env: a.Env, Locked: a.Locked, Muted: a.Muted, Channel: a.Channel, InstalledAt: a.InstalledAt, UpdatedAt: a.UpdatedAt}
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
	v.WaitingConnections = []string{}
	if m := a.Manifest; m != nil {
		v.Setup = len(m.Settings) > 0 || len(m.Secrets) > 0
	}
	v.MCPServers = []string{}
	v.PluginParts = []string{}
	if pkg, _ := apps.CachedPackage(a.Dir); pkg != nil {
		// A server through the person's connection is set up in «Настройка» too: the person allows it there.
		v.Setup = v.Setup || len(pkg.Connections()) > 0
		for name := range pkg.MCP {
			v.MCPServers = append(v.MCPServers, name)
		}
		slices.Sort(v.MCPServers)
		v.PluginParts = append(v.PluginParts, pkg.PluginParts...)
	}
	v.Commands = a.Manifest.ChatCommands(a.Dir)
	if m := a.Manifest; m != nil && m.Chat != nil {
		v.Starters, v.Buttons, v.Links, v.Mentions = m.Chat.Starters, m.Chat.Buttons, m.Chat.Links, m.Chat.Mentions
	}
	v.Views = []appViewEntry{}
	if m := a.Manifest; m != nil {
		v.Branding = m.Branding
		for _, mv := range m.Views {
			e := appViewEntry{ID: mv.ID, Title: mv.Title, Targets: mv.Targets, Height: mv.Height, Label: mv.Label, Icon: mv.Icon, Order: mv.Order, Objects: mv.Objects}
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
	// What the install reaches now: a dev install's folder as it is, where its consent is read from too.
	if apps.PermissionsOn() {
		v.Access = a.AccessNow()
		v.Granted = a.GrantedKeys()
	} else {
		v.PermissionsOff = true
	}
	v.Consent, v.Off, v.Source = a.NeedsConsent(), a.Off, a.Source
	if a.WaitsConsent() {
		v.AskedIn = a.AskedIn
	}
	if p := a.Pending; p != nil && apps.PermissionsOn() {
		next, _ := apps.CachedPackage(p.Dir)
		granted := apps.AccessKeys(v.Access)
		if a.Grant != nil {
			granted = a.Grant.Keys
		}
		v.Pending = &pendingView{Version: p.Version, Access: apps.NewAccess(apps.AccessOf(p.Manifest, next), granted)}
	}
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
	// The hub carries the deployment's policy (internal hosts, roots, proxy); what this machine's environment names wins.
	// The hub's part is kept in the data folder, so a restart without the hub has it (internal/egress).
	apps.SetNetwork(network)
	egress.Keep(context.Background(), os.Getenv("RUNTIME_HUB_URL"), d.DataDir, network, func(msg string, err error) {
		jsonlog.Log("warn", "apps: "+msg, jsonlog.F{K: "error", V: err.Error()})
	})
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
	liveAppsOf.init(reg, c)
	return append(appRouteList(d, reg, version, chats), chatRoutes(d, chats)...)
}

// decisionRate is how many decisions a launch may journal in a minute.
const decisionRate = 30

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
		installed, err := reg.ListAll()
		if err != nil {
			return nil, err
		}
		safe := reg.SafeMode()
		out := make([]appView, 0, len(installed))
		for _, a := range installed {
			// A team app turned off is found in the team's catalog; the environment's has no other place, so it stays
			// here, marked muted, for the person to turn it on again.
			if a.Muted && !a.Env {
				continue
			}
			v := viewOfApp(a, withDir)
			if a.Manifest != nil && len(a.Manifest.Secrets) > 0 {
				store := safeFor(d).store.Local // an app's secrets stay in the file
				v.Waiting = a.Manifest.WaitingFor(func(name string) bool { return store.HasApp(a.ID, name) })
			}
			v.WaitingConnections = waitingConnections(mcpFor(d), reg, a)
			_, readErr := os.Stat(filepath.Join(a.Dir, apps.ManifestPath))
			v.AppState = apps.StateIn(a, safe, version, readErr, v.Waiting, v.WaitingConnections)
			out = append(out, v)
		}
		return out, nil
	}
	viewServer := apps.NewViewServer(reg)
	appViewSessions.Store(viewServer.Sessions)
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
		slots := uiSlotsOn()
		if !slots {
			views = withoutSlots(views)
		}
		// The views' loopback port starts with the first page that lists apps; the page opens a preview slot on it.
		port, err := viewServer.EnsureServer()
		if err != nil {
			failed(w, err)
			return
		}
		appViewPort.Store(int64(port))
		secretJSON(w, http.StatusOK, map[string]any{"apps": views, "viewPort": port, "agentWalled": agentWalled(reg.Root), "slots": slots, "safeMode": reg.SafeMode(), "permissionsOff": !apps.PermissionsOn()})
	}

	// safeMode turns every app but the environment's pinned ones off for the chats that start from now. The
	// person's page only: an agent cannot turn it off to get its apps back.
	safeMode := func(w http.ResponseWriter, r *http.Request) {
		if sameorigin.Refuse(w, r) {
			return
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}
		if r.Method != http.MethodPost {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		var b struct {
			On bool `json:"on"`
		}
		if !readSecretBody(r, &b) {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		if err := reg.SetSafeMode(b.On); err != nil {
			failed(w, err)
			return
		}
		agent.Log("info", "apps: safe mode", agent.F{K: "on", V: b.On})
		secretJSON(w, http.StatusOK, map[string]any{"safeMode": b.On})
	}

	// appSignIn is POST <platform>/apps/signin {app, server}: the «Войти» of an app whose server signs in by OAuth
	// (auth: oauth). It finds or makes the connection the sign-in goes through and answers its id; the page then runs
	// the connections' own sign-in (POST <platform>/mcp/<id>/signin) in a top-level popup. The token stays in the safe.
	appSignIn := func(w http.ResponseWriter, r *http.Request) {
		if sameorigin.Refuse(w, r) {
			return
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}
		if r.Method != http.MethodPost {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		var b struct {
			App    string `json:"app"`
			Server string `json:"server"`
		}
		if !readSecretBody(r, &b) {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		pkg, err := reg.Package(b.App)
		if err != nil {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
			return
		}
		decl, ok := pkg.MCP[b.Server]
		if !ok || decl.Auth == nil || decl.Auth.Type != apps.AuthOAuth {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "not-oauth", "message": "the server does not sign in by OAuth"})
			return
		}
		name := b.App
		if pkg.Manifest != nil && pkg.Manifest.Name != "" {
			name = pkg.Manifest.Name
		}
		conn, err := appSignInConnection(r.Context(), mcpFor(d), b.App, b.Server, name, decl)
		if err != nil {
			agent.Log("warn", "apps: sign-in to an app's server", agent.F{K: "app", V: b.App}, agent.F{K: "error", V: err.Error()})
			secretJSON(w, http.StatusBadGateway, map[string]string{"error": mcpconn.Reason(err)})
			return
		}
		_ = reg.Journal(b.App, apps.JournalEntry{What: "signin.start", Class: apps.ClassHigh, OK: true, Detail: b.Server})
		secretJSON(w, http.StatusOK, map[string]string{"connection": conn.ID})
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
			// Archive is a .hlapp to install instead of a folder (absolute path); Sig the path of its signature, none:
			// <archive>.sig when that file is there.
			Archive string `json:"archive"`
			Sig     string `json:"sig"`
			Dev     bool   `json:"dev"`
			// Session is the CLI's tab: the chat whose agent installs, where the consent card shows.
			Session string `json:"session"`
		}
		if !readSecretBody(r, &b) || !strings.HasPrefix(b.Dir, "/") && !strings.HasPrefix(b.Archive, "/") {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		// The CLI is the person's shell and every agent's: what the app reaches waits for a click in the page.
		askedIn := ""
		if b.Session != "" && chats.ofSession != nil {
			askedIn = chats.ofSession(b.Session)
		}
		opt := apps.InstallOptions{Dev: b.Dev, HostVersion: version, Consent: apps.ConsentAsk, AskedIn: askedIn}
		var got apps.Installed
		var problems apps.Problems
		var err error
		if b.Archive != "" {
			var signed apps.Verified
			got, problems, signed, err = installHlapp(reg, b.Archive, b.Sig, opt)
			if err == nil && signed.Signed {
				_ = reg.Journal(got.ID, apps.JournalEntry{What: "install.signed", Class: apps.ClassHigh, OK: true, Detail: signed.Publisher})
			}
		} else {
			got, problems, err = reg.Install(b.Dir, opt)
		}
		if errors.Is(err, apps.ErrUnsigned) || errors.Is(err, apps.ErrUntrustedKey) || errors.Is(err, apps.ErrBadSignature) {
			agent.Log("warn", "apps: install refused by the signature rule", agent.F{K: "error", V: err.Error()})
			secretJSON(w, http.StatusForbidden, map[string]any{"error": "signature", "message": err.Error()})
			return
		}
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
		case errors.Is(err, apps.ErrSameVersionGrew):
			secretJSON(w, http.StatusConflict, map[string]any{"error": "same-version-grew", "message": err.Error()})
			return
		case errors.Is(err, apps.ErrHostVersion), errors.Is(err, apps.ErrPluginName):
			secretJSON(w, http.StatusConflict, map[string]any{"error": "host-version", "message": err.Error()})
			return
		case err != nil:
			failed(w, err)
			return
		}
		agent.Log("info", "apps: installed", agent.F{K: "id", V: got.ID}, agent.F{K: "version", V: got.Version}, agent.F{K: "dev", V: got.Dev})
		if got.WaitsConsent() {
			detail := got.Version
			if got.Pending != nil {
				detail = got.Pending.Version
			}
			_ = reg.Journal(got.ID, apps.JournalEntry{What: "consent.waits", Class: apps.ClassHigh, OK: true, Detail: detail})
		}
		warning := ""
		if b.Dev && b.Archive == "" && apps.SignaturesRequired() {
			warning = apps.DevUnsignedWarning
			_ = reg.Journal(got.ID, apps.JournalEntry{What: "install.dev-unsigned", Class: apps.ClassHigh, OK: true, Detail: "signature not checked: " + got.Version})
		}
		byPolicy := !got.WaitsConsent() && got.Grant != nil && got.Grant.Policy
		secretJSON(w, http.StatusOK, map[string]any{"app": viewOfApp(got, true), "problems": problems, "waitsConsent": got.WaitsConsent(), "consentPolicy": byPolicy, "warning": warning})
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
		} else if errors.Is(err, apps.ErrTeamApp) || errors.Is(err, apps.ErrEnvApp) {
			shareRefused(w, err)
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

	// decision: the guard hook's line for the journal of a chat's decisions (internal/apps decisions.go). Only the agent of
	// a launch with apps writes it (the hook runs under that token behind the wall); the person's token is refused. The
	// launch is the token's, and so is its chat (the launch's grant): the body's tab must be that chat's, the app one the
	// launch was granted and on in the chat; at most decisionRate lines a minute.
	decisionsDir := filepath.Join(reg.Root, "decisions")
	var decMu sync.Mutex
	decTimes := map[string][]time.Time{}
	decision := func(w http.ResponseWriter, r *http.Request) {
		if !cliOK(w, r, http.MethodPost) {
			return
		}
		launch, byAgent := d.CLI.AgentOf(r)
		if !byAgent || launch == "" {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
			return
		}
		var b struct {
			Session      string `json:"session"`
			AgentSession string `json:"agentSession"`
			App          string `json:"app"`
			Tool         string `json:"tool"`
			Rule         string `json:"rule"`
			Decision     string `json:"decision"`
			Reason       string `json:"reason"`
		}
		if !readSecretBody(r, &b) || b.App == "" || b.Tool == "" || b.Rule == "" || len(b.Tool) > 200 || len(b.Rule) > 200 ||
			(b.Decision != "deny" && b.Decision != "ask") {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		// The chat is the launch's own, from its grant (written when the launch was made, out of the agent's reach); the
		// body can only name that chat's tab.
		granted, conv := reg.Grant(launch)
		rec, ok := chats.record(conv)
		if conv == "" || !ok || chats.ofSession(b.Session) != conv || !slices.Contains(rec.Apps, b.App) || !slices.Contains(granted, b.App) {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "not-on"})
			return
		}
		// The rule is one the app wrote, or a system rule of the guard (Bash(...), Edit(...)).
		if !strings.HasPrefix(b.Rule, "Bash(") && !strings.HasPrefix(b.Rule, "Edit(") {
			pkg, err := reg.Package(b.App)
			if err != nil || pkg.Manifest.Agent == nil || pkg.Manifest.Agent.Tools == nil {
				secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-rule"})
				return
			}
			t := pkg.Manifest.Agent.Tools
			if !slices.Contains(t.Deny, b.Rule) && !slices.Contains(t.Confirm, b.Rule) && !slices.Contains(t.Allow, b.Rule) {
				secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-rule"})
				return
			}
		}
		now := time.Now()
		decMu.Lock()
		recent := slices.DeleteFunc(decTimes[launch], func(t time.Time) bool { return now.Sub(t) >= time.Minute })
		if len(recent) >= decisionRate {
			decTimes[launch] = recent
			decMu.Unlock()
			secretJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate"})
			return
		}
		decTimes[launch] = append(recent, now)
		decMu.Unlock()
		e := apps.DecisionEntry{At: now.UnixMilli(), Session: b.AgentSession, App: b.App, Tool: b.Tool, Rule: b.Rule,
			Decision: b.Decision, Reason: b.Reason}
		if err := apps.AppendDecision(decisionsDir, conv, e); err != nil {
			agent.Log("warn", "apps: a decision not journaled", agent.F{K: "error", V: err.Error()})
			secretJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed"})
			return
		}
		secretJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}

	// decisions: the last guard decisions of an app in a chat, for its «О приложении» section: the page's GET.
	decisions := func(w http.ResponseWriter, r *http.Request) {
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
		app, conv := r.URL.Query().Get("app"), r.URL.Query().Get("conversation")
		if app == "" || conv == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		list, err := apps.ReadDecisions(decisionsDir, conv, app, 50)
		if err != nil {
			failed(w, err)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"decisions": list})
	}

	// connect is the person's agreement that an app uses their connection (internal/apps/connection_grants.go); the agent's
	// token never reaches it (agentRefusedAppRoutes).
	connect := func(w http.ResponseWriter, r *http.Request) {
		if !cliOK(w, r, http.MethodPost) {
			return
		}
		var b struct {
			ID         string `json:"id"`
			Connection string `json:"connection"`
			On         bool   `json:"on"`
		}
		if !readSecretBody(r, &b) || b.ID == "" || b.Connection == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		var err error
		if b.On {
			err = reg.GrantConnection(b.ID, b.Connection)
		} else {
			err = reg.RevokeConnection(b.ID, b.Connection)
		}
		if err != nil {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"id": b.ID, "connection": b.Connection, "on": b.On})
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
		// An app that waits for the person's consent cannot be turned on in a chat: the answer says what it waits for and
		// where the person gives the word.
		if a, err := reg.Find(b.ID); b.On && err == nil && a.NeedsConsent() {
			secretJSON(w, http.StatusConflict, map[string]any{"error": "consent", "access": apps.AccessCodes(a.WaitingAccess()), "where": apps.ConsentWhere(b.ID)})
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

	// explain: why an app is on or off, layer by layer — the machine, the team, its setup, how it turns on, the
	// chat of the calling tab and the wall. A read: the agent of a chat with apps may ask it too.
	explain := func(w http.ResponseWriter, r *http.Request) {
		if !cliOK(w, r, http.MethodGet) {
			return
		}
		id := r.URL.Query().Get("id")
		if id == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		in := apps.ExplainInput{ID: id, HostVersion: version}
		// Every install, a dev one that waits for consent and one turned off too: explain says why it is off.
		if a, err := reg.Find(id); err == nil {
			in.App = &a
			in.ViewLog, _ = reg.ViewLog(id, apps.ViewLogMax)
			if a.Manifest != nil && len(a.Manifest.Secrets) > 0 {
				store := safeFor(d).store.Local // an app's secrets stay in the file
				in.Waiting = a.Manifest.WaitingFor(func(name string) bool { return store.HasApp(a.ID, name) })
			}
		}
		if session := r.URL.Query().Get("session"); session != "" {
			if rec, ok := chats.record(chats.ofSession(session)); ok {
				in.Chat = &apps.ExplainChat{Provider: string(rec.Provider), Apps: rec.Apps}
				for _, on := range rec.Apps {
					if a, err := reg.Get(on); err == nil {
						in.ChatBytes += apps.InstructionsBytes(a)
					}
				}
			}
		}
		if runtime.GOOS == "linux" {
			in.Wall = true
			if in.WallErr = appBoundary().Ready(reg.Root); in.WallErr == nil {
				in.WallErr = appBoundary().Gaps()
			}
		}
		secretJSON(w, http.StatusOK, map[string]any{"id": id, "lines": apps.Explain(in)})
	}

	routes := append(appBridgeRoutes(d, reg, storage, viewServer, chats), appAskRoutes(d, appAsks)...)
	routes = append(routes, appPageRoutes(d, reg, appPages, chats)...)
	routes = append(routes, appSetupRoutes(d, reg, storage)...)
	routes = append(routes, appMCPProxyRoute(d, reg))
	routes = append(routes, sinopiaRoutes(d, reg, viewServer)...)
	routes = append(routes, kindRoutes(d, reg)...)
	routes = append(routes, appWidgetRoutes(d, reg, widgets, chats)...)
	routes = append(routes, helpRoutes(d)...)
	routes = append(routes, appPublicRoutes(d, reg, version)...)
	routes = append(routes, appShareRoutes(d, reg, guests, version)...)
	routes = append(routes, appConsentRoutes(d, reg)...)
	routes = append(routes, appViewLogRoutes(d, reg)...)
	routes = append(routes, appMentionRoutes(d, reg, chats)...)
	routes = append(routes, appHookRoutes(d, reg, chats)...)
	routes = append(routes, appLiveRoutes(d, reg, chats)...)
	routes = append(routes, []gate.Route{
		{Match: is(platformAPI + "/installed-apps"), Handler: http.HandlerFunc(page)},
		{Match: is("/agents" + platformAPI + "/installed-apps"), Handler: http.HandlerFunc(page)},
		{Match: is(platformAPI + "/apps/safe-mode"), Handler: http.HandlerFunc(safeMode)},
		{Match: is(platformAPI + "/apps/signin"), Handler: http.HandlerFunc(appSignIn)},
		{Match: is("/agents" + platformAPI + "/apps/safe-mode"), Handler: http.HandlerFunc(safeMode)},
		{Match: is("/api/cli/apps"), Handler: http.HandlerFunc(cliList)},
		{Match: is("/api/cli/apps/install"), Handler: http.HandlerFunc(install)},
		{Match: is("/api/cli/apps/remove"), Handler: http.HandlerFunc(remove)},
		{Match: is("/api/cli/apps/enable"), Handler: http.HandlerFunc(enable)},
		{Match: is("/api/cli/apps/connect"), Handler: http.HandlerFunc(connect)},
		{Match: is("/api/cli/apps/context"), Handler: http.HandlerFunc(appContext)},
		{Match: is("/api/cli/apps/decision"), Handler: http.HandlerFunc(decision)},
		{Match: is(platformAPI + "/apps/decisions"), Handler: http.HandlerFunc(decisions)},
		{Match: is("/agents" + platformAPI + "/apps/decisions"), Handler: http.HandlerFunc(decisions)},
		{Match: is("/api/cli/apps/explain"), Handler: http.HandlerFunc(explain)},
	}...)
	return refuseAgentAppChanges(d.CLI, routes)
}

// waitingConnections are the connections the servers of the package in dir name that the person cannot use now.
func waitingConnections(ms *mcpconn.Service, reg *apps.Registry, a apps.Installed) []string {
	out := []string{}
	pkg, _ := apps.CachedPackage(a.Dir)
	if pkg == nil {
		return out
	}
	out = append(out, waitingConnectionsOf(ms, pkg.Connections())...)
	for _, id := range pkg.Connections() {
		if !reg.ConnectionGranted(a.ID, id) && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// waitingConnectionsOf are those of the catalog ids the person has not made or not signed in to.
func waitingConnectionsOf(ms *mcpconn.Service, ids []string) []string {
	out := []string{}
	for _, id := range ids {
		if _, ok := ms.ByCatalog(id); !ok {
			out = append(out, id)
		}
	}
	return out
}
