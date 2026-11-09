package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// Sharing apps and their versions (internal/apps: channels.go, team.go, guest.go). Who may call what:
//
//	the cli-token (an agent of this machine, the person's shell: harnessly app …)
//	  POST /api/cli/apps/publish|promote|rollback|follow|sync|share|unshare, GET /api/cli/apps/channels|shares|team
//	  POST /api/cli/apps/team/{set,publish,install,uninstall,rollback}
//	the person's page session
//	  GET  <platform>/apps/team           the team and its catalog
//	  POST <platform>/apps/team/install|uninstall|mute   install for the team (an owner only), turn a team app off for oneself
//	the machine agent (the platform's relay)
//	  POST <platform>/apps/team/sync      the platform's snapshot of the team
//	  POST <platform>/apps/team/snapshot  this machine's snapshot, for the platform to send the team's other machines
//	nobody's session: the guest routes of a standalone app (GuestMatch); the link's token is the authority.

// shareRefused turns the registry's refusals into the status and the code the CLI and the page read.
func shareRefused(w http.ResponseWriter, err error) {
	code, status := "", 0
	for _, r := range []struct {
		err    error
		code   string
		status int
	}{
		{apps.ErrNoTeam, "no-team", http.StatusConflict},
		{apps.ErrNotOwner, "not-owner", http.StatusForbidden},
		{apps.ErrTeamApp, "team-app", http.StatusConflict},
		{apps.ErrEnvApp, "env-app", http.StatusConflict},
		{apps.ErrEnvLocked, "env-locked", http.StatusConflict},
		{apps.ErrTrustedTeam, "trusted", http.StatusUnprocessableEntity},
		{apps.ErrNotInCatalog, "not-in-catalog", http.StatusNotFound},
		{apps.ErrNotInstalled, "not-found", http.StatusNotFound},
		{apps.ErrNoVersion, "no-version", http.StatusNotFound},
		{apps.ErrNoChannel, "no-channel", http.StatusNotFound},
		{apps.ErrNoPrevious, "no-previous", http.StatusConflict},
		{apps.ErrVersionExists, "version-exists", http.StatusConflict},
		{apps.ErrChannelVersion, "channel-version", http.StatusUnprocessableEntity},
		{apps.ErrEvalGate, "eval-gate", http.StatusUnprocessableEntity},
		{apps.ErrHostVersion, "host-version", http.StatusConflict},
		{apps.ErrPluginName, "host-version", http.StatusConflict},
		{apps.ErrKindTaken, "kind-taken", http.StatusConflict},
		{apps.ErrCommandTaken, "command-taken", http.StatusConflict},
		{apps.ErrStaleTeam, "stale", http.StatusConflict},
		{apps.ErrGuestTarget, "not-app-view", http.StatusUnprocessableEntity},
		{apps.ErrGuestGone, "not-found", http.StatusNotFound},
		{apps.ErrGuestLimit, "too-many", http.StatusConflict},
		{apps.ErrArchive, "archive", http.StatusUnprocessableEntity},
		{apps.ErrUnsigned, "signature", http.StatusForbidden},
		{apps.ErrUntrustedKey, "signature", http.StatusForbidden},
		{apps.ErrBadSignature, "signature", http.StatusForbidden},
	} {
		if errors.Is(err, r.err) {
			code, status = r.code, r.status
			break
		}
	}
	if status == 0 {
		agent.Log("warn", "apps: share: "+err.Error())
		secretJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed"})
		return
	}
	secretJSON(w, status, map[string]string{"error": code, "message": err.Error()})
}

func appShareRoutes(d deps, reg *apps.Registry, guests *apps.Guests, version string) []gate.Route {
	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	now := time.Now

	// The three callers each get a guard that answers the refusal itself.
	cli := func(w http.ResponseWriter, r *http.Request, method string) bool {
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
	page := func(w http.ResponseWriter, r *http.Request, method string) bool {
		if sameorigin.Refuse(w, r) {
			return false
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return false
		}
		if r.Method != method {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return false
		}
		return true
	}
	platform := func(w http.ResponseWriter, r *http.Request) bool {
		if d.MachineAgent == nil || !d.MachineAgent(r) {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
			return false
		}
		if r.Method != http.MethodPost {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return false
		}
		return true
	}
	// who is the person's name in a catalog entry or a team install: the session's subject, else the shell's.
	who := func(r *http.Request) string {
		if d.Claims != nil {
			if sub, ok := d.Claims(r)["sub"].(string); ok && sub != "" && len(sub) <= 254 {
				return sub
			}
		}
		return "cli"
	}
	channelOf := func(w http.ResponseWriter, name string) (apps.Channel, bool) {
		ch, ok := apps.ParseChannel(name)
		if !ok {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request", "message": "channel is dev, beta or stable"})
		}
		return ch, ok
	}

	publish := func(w http.ResponseWriter, r *http.Request) {
		if !cli(w, r, http.MethodPost) {
			return
		}
		var b struct {
			Dir     string `json:"dir"`
			Channel string `json:"channel"`
			Team    bool   `json:"team"`
			// Archive is a .hlapp to publish instead of a folder, Sig its signature.
			Archive string `json:"archive"`
			Sig     string `json:"sig"`
		}
		// The agent of a chat with apps publishes to dev and beta only (as the guard hook has it), read from this one
		// decoding of the body: a body that is not exactly these fields is refused to it, never read as narrow.
		_, byAgent := d.CLI.AgentOf(r)
		if byAgent {
			if !readAgentBody(r, &b) {
				secretJSON(w, http.StatusForbidden, map[string]string{"error": errAgentCannotChangeApps})
				return
			}
		} else if !readSecretBody(r, &b) {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		if !strings.HasPrefix(b.Dir, "/") && !strings.HasPrefix(b.Archive, "/") {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		if byAgent && (b.Team || (b.Channel != string(apps.ChannelDev) && b.Channel != string(apps.ChannelBeta))) {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": errAgentCannotChangeApps})
			return
		}
		ch, ok := channelOf(w, b.Channel)
		if !ok {
			return
		}
		var pv apps.PublishedVersion
		var problems apps.Problems
		var err error
		if b.Archive != "" {
			pv, problems, err = publishHlapp(reg, b.Archive, b.Sig, ch)
			if errors.Is(err, apps.ErrUnsigned) || errors.Is(err, apps.ErrUntrustedKey) || errors.Is(err, apps.ErrBadSignature) {
				secretJSON(w, http.StatusForbidden, map[string]any{"error": "signature", "message": err.Error()})
				return
			}
		} else {
			pv, problems, err = reg.Publish(b.Dir, ch, apps.PublishOptions{})
		}
		if problems == nil {
			problems = apps.Problems{}
		}
		if errors.Is(err, apps.ErrInvalid) {
			secretJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "invalid", "problems": problems})
			return
		}
		if err != nil {
			shareRefused(w, err)
			return
		}
		out := map[string]any{"id": pv.ID, "version": pv.Version, "channel": pv.Channel, "problems": problems}
		if b.Team {
			e, err := reg.PublishToTeam(pv.ID, pv.Version, ch, who(r), version, now())
			if err != nil {
				// The version is published; only the catalog refused.
				out["teamError"] = err.Error()
			} else {
				out["catalog"] = e
			}
		}
		agent.Log("info", "apps: published", agent.F{K: "id", V: pv.ID}, agent.F{K: "version", V: pv.Version}, agent.F{K: "channel", V: string(ch)})
		secretJSON(w, http.StatusOK, out)
	}

	promote := func(w http.ResponseWriter, r *http.Request) {
		if !cli(w, r, http.MethodPost) {
			return
		}
		var b struct {
			ID        string  `json:"id"`
			Version   string  `json:"version"`
			Channel   string  `json:"channel"`
			Threshold float64 `json:"threshold"`
			Team      bool    `json:"team"`
		}
		if !readSecretBody(r, &b) || b.ID == "" || b.Version == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		ch, ok := channelOf(w, b.Channel)
		if !ok {
			return
		}
		pv, err := reg.Promote(b.ID, b.Version, ch, apps.PromoteOptions{EvalThreshold: b.Threshold})
		if err != nil {
			shareRefused(w, err)
			return
		}
		out := map[string]any{"id": pv.ID, "version": pv.Version, "channel": pv.Channel}
		if b.Team {
			e, err := reg.PublishToTeam(pv.ID, pv.Version, ch, who(r), version, now())
			if err != nil {
				out["teamError"] = err.Error()
			} else {
				out["catalog"] = e
			}
		}
		secretJSON(w, http.StatusOK, out)
	}

	rollback := func(w http.ResponseWriter, r *http.Request) {
		if !cli(w, r, http.MethodPost) {
			return
		}
		var b struct {
			ID      string `json:"id"`
			Channel string `json:"channel"`
			// Team moves the team's catalog entry back instead of the machine's channel.
			Team bool `json:"team"`
		}
		if !readSecretBody(r, &b) || b.ID == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		ch, ok := channelOf(w, b.Channel)
		if !ok {
			return
		}
		if b.Team {
			e, err := reg.RollbackCatalog(b.ID, ch, version, now())
			if err != nil {
				shareRefused(w, err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]any{"id": e.ID, "version": e.Version, "channel": e.Channel})
			return
		}
		pv, err := reg.Rollback(b.ID, ch)
		if err != nil {
			shareRefused(w, err)
			return
		}
		// The installs that follow the channel go back with it.
		moved, err := reg.SyncChannels(apps.SyncOptions{HostVersion: version})
		if err != nil {
			shareRefused(w, err)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"id": pv.ID, "version": pv.Version, "channel": pv.Channel, "moved": orEmptySync(moved)})
	}

	channels := func(w http.ResponseWriter, r *http.Request) {
		if !cli(w, r, http.MethodGet) {
			return
		}
		if id := r.URL.Query().Get("id"); id != "" {
			chs, err := reg.Channels(id)
			if err != nil {
				shareRefused(w, err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]any{"id": id, "channels": chs})
			return
		}
		all, err := reg.Published()
		if err != nil {
			shareRefused(w, err)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"apps": all})
	}

	follow := func(w http.ResponseWriter, r *http.Request) {
		if !cli(w, r, http.MethodPost) {
			return
		}
		var b struct {
			ID      string `json:"id"`
			Channel string `json:"channel"`
		}
		if !readSecretBody(r, &b) || b.ID == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		ch, ok := channelOf(w, b.Channel)
		if !ok {
			return
		}
		got, problems, err := reg.InstallChannel(b.ID, ch, apps.InstallOptions{HostVersion: version, Consent: apps.ConsentAsk})
		if problems == nil {
			problems = apps.Problems{}
		}
		if errors.Is(err, apps.ErrInvalid) {
			secretJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "invalid", "problems": problems})
			return
		}
		if err != nil {
			shareRefused(w, err)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"app": viewOfApp(got, true), "problems": problems})
	}

	sync := func(w http.ResponseWriter, r *http.Request) {
		if !cli(w, r, http.MethodPost) {
			return
		}
		var b struct {
			Confirm []string `json:"confirm"`
		}
		_ = readSecretBody(r, &b)
		moved, err := reg.SyncChannels(apps.SyncOptions{HostVersion: version, Confirm: b.Confirm})
		if err != nil {
			shareRefused(w, err)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"moved": orEmptySync(moved)})
	}

	// The team, from the shell: tell a machine without a platform which team it is in, publish to the catalog, install.
	teamCLI := func(w http.ResponseWriter, r *http.Request) {
		if !cli(w, r, http.MethodGet) {
			return
		}
		info, err := reg.Team()
		if err != nil {
			shareRefused(w, err)
			return
		}
		secretJSON(w, http.StatusOK, info)
	}
	teamSet := func(w http.ResponseWriter, r *http.Request) {
		if !cli(w, r, http.MethodPost) {
			return
		}
		var b struct {
			Space string `json:"space"`
			Role  string `json:"role"`
		}
		if !readSecretBody(r, &b) {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		if err := reg.SetTeam(b.Space, b.Role, now()); err != nil {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request", "message": err.Error()})
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"space": b.Space, "role": b.Role})
	}
	teamInstall := func(w http.ResponseWriter, r *http.Request, by string) {
		var b struct {
			ID      string `json:"id"`
			Channel string `json:"channel"`
		}
		if !readSecretBody(r, &b) || b.ID == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		ch, ok := channelOf(w, b.Channel)
		if !ok {
			return
		}
		ti, err := reg.InstallForTeam(b.ID, ch, apps.TeamInstallOptions{HostVersion: version, By: by, Now: now()})
		if err != nil {
			shareRefused(w, err)
			return
		}
		agent.Log("info", "apps: installed for the team", agent.F{K: "id", V: ti.ID}, agent.F{K: "version", V: ti.Version})
		secretJSON(w, http.StatusOK, map[string]any{"id": ti.ID, "version": ti.Version, "channel": ti.Channel})
	}
	teamUninstall := func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			ID string `json:"id"`
		}
		if !readSecretBody(r, &b) || b.ID == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		if err := reg.UninstallForTeam(b.ID, now()); err != nil {
			shareRefused(w, err)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"removed": b.ID})
	}
	teamPublishCLI := func(w http.ResponseWriter, r *http.Request) {
		if !cli(w, r, http.MethodPost) {
			return
		}
		var b struct {
			ID      string `json:"id"`
			Version string `json:"version"`
			Channel string `json:"channel"`
		}
		if !readSecretBody(r, &b) || b.ID == "" || b.Version == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		ch, ok := channelOf(w, b.Channel)
		if !ok {
			return
		}
		e, err := reg.PublishToTeam(b.ID, b.Version, ch, who(r), version, now())
		if err != nil {
			shareRefused(w, err)
			return
		}
		secretJSON(w, http.StatusOK, e)
	}

	// The page.
	teamPage := func(w http.ResponseWriter, r *http.Request) {
		if !page(w, r, http.MethodGet) {
			return
		}
		info, err := reg.Team()
		if err != nil {
			shareRefused(w, err)
			return
		}
		entries := make([]*apps.CatalogEntry, len(info.Catalog))
		for i := range info.Catalog {
			entries[i] = &info.Catalog[i].CatalogEntry
		}
		reg.Precheck(entries, version)
		secretJSON(w, http.StatusOK, info)
	}
	teamPageInstall := func(w http.ResponseWriter, r *http.Request) {
		if page(w, r, http.MethodPost) {
			teamInstall(w, r, who(r))
		}
	}
	teamPageUninstall := func(w http.ResponseWriter, r *http.Request) {
		if page(w, r, http.MethodPost) {
			teamUninstall(w, r)
		}
	}
	teamPageMute := func(w http.ResponseWriter, r *http.Request) {
		if !page(w, r, http.MethodPost) {
			return
		}
		var b struct {
			ID   string `json:"id"`
			Mute bool   `json:"mute"`
		}
		if !readSecretBody(r, &b) || b.ID == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		if err := reg.MuteTeamApp(b.ID, b.Mute); err != nil {
			shareRefused(w, err)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"id": b.ID, "muted": b.Mute})
	}

	// The platform.
	teamSync := func(w http.ResponseWriter, r *http.Request) {
		if !platform(w, r) {
			return
		}
		var s apps.TeamSnapshot
		if !readAppsBody(r, &s) {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		problems, err := reg.ApplyTeam(s, version, now())
		if err != nil {
			if errors.Is(err, apps.ErrStaleTeam) {
				shareRefused(w, err)
				return
			}
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request", "message": err.Error()})
			return
		}
		if problems == nil {
			problems = []string{}
		}
		secretJSON(w, http.StatusOK, map[string]any{"ok": true, "problems": problems})
	}
	teamSnapshot := func(w http.ResponseWriter, r *http.Request) {
		if !platform(w, r) {
			return
		}
		s, err := reg.Snapshot()
		if err != nil {
			shareRefused(w, err)
			return
		}
		secretJSON(w, http.StatusOK, s)
	}

	// Links of a standalone app.
	share := func(w http.ResponseWriter, r *http.Request) {
		if !cli(w, r, http.MethodPost) {
			return
		}
		var b struct {
			ID      string `json:"id"`
			View    string `json:"view"`
			TTLSecs int64  `json:"ttlSeconds"`
			Write   bool   `json:"write"`
		}
		if !readSecretBody(r, &b) || b.ID == "" || b.TTLSecs < 0 {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		token, link, err := guests.Create(b.ID, b.View, time.Duration(b.TTLSecs)*time.Second, b.Write)
		if err != nil {
			shareRefused(w, err)
			return
		}
		agent.Log("info", "apps: shared by link", agent.F{K: "id", V: link.App}, agent.F{K: "link", V: link.ID})
		secretJSON(w, http.StatusOK, map[string]any{"link": link, "path": apps.GuestPrefix + token + "/"})
	}
	shares := func(w http.ResponseWriter, r *http.Request) {
		if !cli(w, r, http.MethodGet) {
			return
		}
		list, err := guests.List(r.URL.Query().Get("id"))
		if err != nil {
			shareRefused(w, err)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"links": list})
	}
	unshare := func(w http.ResponseWriter, r *http.Request) {
		if !cli(w, r, http.MethodPost) {
			return
		}
		var b struct {
			ID string `json:"id"`
		}
		if !readSecretBody(r, &b) || b.ID == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		if err := guests.Revoke(b.ID); err != nil {
			shareRefused(w, err)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"revoked": b.ID})
	}

	return []gate.Route{
		{Match: apps.GuestMatch, Handler: guests},
		{Match: is("/api/cli/apps/publish"), Handler: http.HandlerFunc(publish)},
		{Match: is("/api/cli/apps/promote"), Handler: http.HandlerFunc(promote)},
		{Match: is("/api/cli/apps/rollback"), Handler: http.HandlerFunc(rollback)},
		{Match: is("/api/cli/apps/channels"), Handler: http.HandlerFunc(channels)},
		{Match: is("/api/cli/apps/follow"), Handler: http.HandlerFunc(follow)},
		{Match: is("/api/cli/apps/sync"), Handler: http.HandlerFunc(sync)},
		{Match: is("/api/cli/apps/team"), Handler: http.HandlerFunc(teamCLI)},
		{Match: is("/api/cli/apps/team/set"), Handler: http.HandlerFunc(teamSet)},
		{Match: is("/api/cli/apps/team/publish"), Handler: http.HandlerFunc(teamPublishCLI)},
		{Match: is("/api/cli/apps/team/install"), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cli(w, r, http.MethodPost) {
				teamInstall(w, r, who(r))
			}
		})},
		{Match: is("/api/cli/apps/team/uninstall"), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cli(w, r, http.MethodPost) {
				teamUninstall(w, r)
			}
		})},
		{Match: is("/api/cli/apps/share"), Handler: http.HandlerFunc(share)},
		{Match: is("/api/cli/apps/shares"), Handler: http.HandlerFunc(shares)},
		{Match: is("/api/cli/apps/unshare"), Handler: http.HandlerFunc(unshare)},
		{Match: is(platformAPI + "/apps/team"), Handler: http.HandlerFunc(teamPage)},
		{Match: is("/agents" + platformAPI + "/apps/team"), Handler: http.HandlerFunc(teamPage)},
		{Match: is(platformAPI + "/apps/team/install"), Handler: http.HandlerFunc(teamPageInstall)},
		{Match: is(platformAPI + "/apps/team/uninstall"), Handler: http.HandlerFunc(teamPageUninstall)},
		{Match: is(platformAPI + "/apps/team/mute"), Handler: http.HandlerFunc(teamPageMute)},
		{Match: is("/api/cli/apps/check"), Handler: http.HandlerFunc(checkRoute(d, reg, guests))},
		{Match: is(platformAPI + "/apps/team/sync"), Handler: http.HandlerFunc(teamSync)},
		{Match: is(platformAPI + "/apps/team/snapshot"), Handler: http.HandlerFunc(teamSnapshot)},
	}
}

func orEmptySync(list []apps.SyncResult) []apps.SyncResult {
	if list == nil {
		return []apps.SyncResult{}
	}
	return list
}

// maxSnapshotBody bounds the platform's snapshot of the team: the catalog's archives travel in it (base64).
const maxSnapshotBody = 64 << 20

func readAppsBody(r *http.Request, out any) bool {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxSnapshotBody+1))
	if err != nil || len(raw) > maxSnapshotBody {
		return false
	}
	return json.Unmarshal(raw, out) == nil
}

// wireAppsEvents makes the registry and the guest links tell the cluster, through the machine agent, what the cluster
// keeps in step: that the team's catalog or installs changed, and which guest links live (by the hash of the token).
// The runtime also sends the full set of live links when it starts, for an agent that was not told of them.
func wireAppsEvents(d deps, reg *apps.Registry, guests *apps.Guests) {
	header := os.Getenv("RUNTIME_AGENT_TOKEN_HEADER")
	emit := func(ev agent.AppsEvent) { agent.EmitAppsEvent(d.DataDir, header, ev) }
	reg.OnTeamChange(func() { emit(agent.AppsEvent{Event: agent.AppsTeamChanged}) })
	reg.OnPublicChange(func() { emit(agent.AppsEvent{Event: agent.AppsPublicChanged}) })
	guests.OnCreate = func(l apps.GuestLink) {
		emit(agent.AppsEvent{Event: agent.AppsGuestCreate, ID: l.ID, Hash: l.Hash, At: l.ExpiresAt})
	}
	guests.OnRevoke = func(id string) { emit(agent.AppsEvent{Event: agent.AppsGuestRevoke, ID: id}) }
	go func() {
		live, err := guests.List("")
		if err != nil {
			return
		}
		sync := agent.AppsEvent{Event: agent.AppsGuestSync, Links: []agent.AppsGuest{}}
		for _, l := range live {
			sync.Links = append(sync.Links, agent.AppsGuest{ID: l.ID, Hash: l.Hash, ExpiresAt: l.ExpiresAt})
		}
		// The agent may still be starting: ask a while.
		for i := 0; i < 30; i++ {
			if agent.SendAppsEvent(d.DataDir, header, sync) == nil {
				return
			}
			time.Sleep(2 * time.Second)
		}
	}()
}
