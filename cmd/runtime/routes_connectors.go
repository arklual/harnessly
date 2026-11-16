package main

// Connectors (internal/connectors, docs/connectors.md): mail, calendars, folders, chat bots, feeds and webhooks the person
// connects in Settings → Connections. The page lists the catalog and the connections, connects, checks and removes
// them, and answers the agents' proposed actions outward on the card of their chat. An agent reads through the CLI
// routes and proposes actions there; it can never approve one: the answer is a page route with the person's session.
//
//	GET    <platform>/connectors                          {catalog, connections}
//	POST   <platform>/connectors                          {connector, name, values} → {connection}
//	POST   <platform>/connectors/discover                 {connector, field, values} → {choices} (nothing is kept)
//	POST   <platform>/connectors/<id>/check               → {connection}
//	POST   <platform>/connectors/<id>/reissue             → {connection} with its new generated secrets in shown, once
//	PATCH  <platform>/connectors/<id>                     {name} → {connection}
//	DELETE <platform>/connectors/<id>
//	GET    <platform>/connector-actions[?rev=N]           {proposals, rev}: with rev, waits up to 25 s for a change
//	POST   <platform>/connector-actions/<id>/<approve|decline>  → {proposal}
//	GET    <platform>/connector-sends                     {sent} (the journal of sends, newest first)
//	GET    /api/cli/connectors                            {catalog, connections}
//	POST   /api/cli/connectors/read                       {connection, tool, args} → {text}
//	POST   <platform>/connector-webhook                    {connection, path, header, body}: a request the hub passed on
//	                                                      from a webhook's address (the machine agent) → {status}
//	POST   /api/cli/connectors/act                        {connection, action, args, why, wait, id} → {proposal}

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/asks"
	"github.com/ly-solutions/harnessly/internal/connections"
	"github.com/ly-solutions/harnessly/internal/connectors"
	_ "github.com/ly-solutions/harnessly/internal/connectors/caldav"
	_ "github.com/ly-solutions/harnessly/internal/connectors/folder"
	_ "github.com/ly-solutions/harnessly/internal/connectors/imap"
	_ "github.com/ly-solutions/harnessly/internal/connectors/rss"
	"github.com/ly-solutions/harnessly/internal/connectors/rules"
	"github.com/ly-solutions/harnessly/internal/connectors/telegram"
	"github.com/ly-solutions/harnessly/internal/connectors/webhook"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/notifybots"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
	"github.com/ly-solutions/harnessly/internal/wire"
)

const (
	// connectorActWaitMax is how long one CLI request waits for the person's answer; the agent asks again with the id.
	connectorActWaitMax = 50 * time.Second
	connectorCallMax    = 45 * time.Second
)

var (
	connectorsMu  sync.Mutex
	connectorsAll = map[string]*connectors.Service{}
)

// connectorsFor is the one connectors service of a runtime; its sources start with the first call.
func connectorsFor(d deps) *connectors.Service {
	connectorsMu.Lock()
	defer connectorsMu.Unlock()
	if s := connectorsAll[d.DataDir]; s != nil {
		return s
	}
	sec := &connSecrets{store: safeFor(d).store, titleOf: func(name string) string {
		return "connector " + strings.TrimPrefix(name, "connector-")
	}}
	webhook.Base = func() string { return webhookBase(d.DataDir) }
	s := connectors.New(filepath.Join(d.DataDir, "connectors"), sec, connections.NewHTTP(nil).Client)
	header := os.Getenv("RUNTIME_AGENT_TOKEN_HEADER")
	s.Start(context.Background(),
		connectors.Hub(func(ctx context.Context, source, fingerprint string, event json.RawMessage) error {
			return agent.SendAutomationEvent(ctx, d.DataDir, header, source, fingerprint, event)
		}),
		connectors.Inbox(func() (string, string, bool) { return inboxIngest(d) }, nil),
		rulesFor(d, s).Destination(),
	)
	connectorsAll[d.DataDir] = s
	return s
}

// webhookBase is the public address of this machine's webhooks: its hub's automations hook with the machine's id. The
// hub passes what comes there over the machine's link (connectors.webhook) and keeps nothing; "" without a hub.
func webhookBase(dataDir string) string {
	for _, file := range machineFiles(dataDir) {
		m, err := agent.LoadMachine(file)
		if err != nil || m.HubURL == "" || m.MachineID == "" {
			continue
		}
		return strings.TrimRight(m.HubURL, "/") + "/api/platform/automations/hook/m/" + url.PathEscape(m.MachineID)
	}
	return ""
}

// inboxIngest is where the person's inbox takes signals: the installed app whose start-page view is a day feed signed
// in by the machine (feedSecret, docs/apps/day-feed.md) takes them at /api/ingest of the feed's origin, with the same
// token. ok is false while no such app is installed or its secret has no value.
func inboxIngest(d deps) (string, string, bool) {
	list, err := apps.Open(d.DataDir).List()
	if err != nil {
		return "", "", false
	}
	for _, a := range list {
		if a.Off || a.Muted || a.NeedsConsent() {
			continue
		}
		m := a.LiveManifest()
		for _, v := range m.Views {
			if v.Feed == "" || v.FeedSecret == "" {
				continue
			}
			feed, ok := m.FeedURL(v.Feed)
			if !ok {
				continue
			}
			u, err := url.Parse(feed)
			if err != nil {
				continue
			}
			token := revealApp(safeFor(d).store.Local, a.ID, []string{v.FeedSecret})[v.FeedSecret]
			if token == "" {
				continue
			}
			return u.Scheme + "://" + u.Host + "/api/ingest", token, true
		}
	}
	return "", "", false
}

// connectorRefusal answers an error of the service: words for the person and the agent, never a stack.
func connectorRefusal(w http.ResponseWriter, err error) {
	var said *connectors.Said
	switch {
	case errors.As(err, &said):
		secretJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "said", "message": said.Message})
	case errors.Is(err, connectors.ErrUnknown):
		secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
	case errors.Is(err, connectors.ErrUnknownTool):
		secretJSON(w, http.StatusNotFound, map[string]string{"error": "unknown-tool"})
	case errors.Is(err, connectors.ErrBadArgs):
		secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-args"})
	case errors.Is(err, connectors.ErrNotWaiting):
		secretJSON(w, http.StatusConflict, map[string]string{"error": "not-waiting"})
	default:
		secretJSON(w, http.StatusBadGateway, map[string]string{"error": "failed", "message": connectors.Say(err)})
	}
}

func connectorRoutes(d deps, c *core) []gate.Route {
	svc := connectorsFor(d)
	rulesCore.Store(c)
	ruleEngine := rulesFor(d, svc)
	telegram.SetHooks(telegramHooks(d, c))
	callers := &callerCache{panes: c.cfg.Tmux, convOf: func(session string) string {
		id, _ := convOf(c, session)
		return id
	}}
	byOf := func(r *http.Request) connectors.By {
		conv, _ := callers.of(r)
		if conv == "" {
			return connectors.By{}
		}
		return connectors.By{Conv: conv, Title: secretIdentity(c, conv).Title}
	}
	base, actionsBase := platformAPI+"/connectors", platformAPI+"/connector-actions"
	sendsBase, cliBase := platformAPI+"/connector-sends", "/api/cli/connectors"
	under := func(r *http.Request, p string) (string, bool) {
		for _, prefix := range []string{p + "/", "/agents" + p + "/"} {
			if rest, ok := strings.CutPrefix(r.URL.Path, prefix); ok && rest != "" {
				return rest, true
			}
		}
		return "", false
	}
	pageOK := func(w http.ResponseWriter, r *http.Request) bool {
		if sameorigin.Refuse(w, r) {
			return false
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return false
		}
		return true
	}
	read := func(w http.ResponseWriter, r *http.Request, out any) bool {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		if err != nil || json.Unmarshal(raw, out) != nil {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return false
		}
		return true
	}
	notAllowed := func(w http.ResponseWriter) {
		secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
	}
	// listing is the catalog and the connections; the page's also has the step each waits for (a pairing link that is
	// the person's alone, never an agent's).
	listing := func(w http.ResponseWriter, r *http.Request, page bool) {
		list, err := svc.List()
		if page {
			list, err = svc.Views(r.Context())
		}
		if err != nil {
			connectorRefusal(w, err)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"catalog": connectors.Catalog(), "connections": list,
			"ruled": rules.Connectors()})
	}

	page := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), connectorCallMax)
		defer cancel()
		rest, sub := under(r, base)
		if !sub {
			switch r.Method {
			case http.MethodGet:
				listing(w, r, true)
			case http.MethodPost:
				var b struct {
					Connector string            `json:"connector"`
					Name      string            `json:"name"`
					Values    map[string]string `json:"values"`
				}
				if !read(w, r, &b) {
					return
				}
				conn, err := svc.Connect(ctx, b.Connector, b.Name, b.Values)
				if err != nil {
					connectorRefusal(w, err)
					return
				}
				secretJSON(w, http.StatusOK, map[string]any{"connection": conn})
			default:
				notAllowed(w)
			}
			return
		}
		id, verb, _ := strings.Cut(rest, "/")
		switch {
		case id == "discover" && verb == "" && r.Method == http.MethodPost:
			var b struct {
				Connector string            `json:"connector"`
				Field     string            `json:"field"`
				Values    map[string]string `json:"values"`
			}
			if !read(w, r, &b) {
				return
			}
			choices, err := svc.Discover(ctx, b.Connector, b.Field, b.Values)
			if err != nil {
				connectorRefusal(w, err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]any{"choices": choices})
		case verb == "rules":
			connectorRulesRoute(w, r, svc, ruleEngine, id, read)
		case verb == "check" && r.Method == http.MethodPost:
			conn, err := svc.Check(ctx, id)
			if err != nil {
				connectorRefusal(w, err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]any{"connection": conn})
		case verb == "reissue" && r.Method == http.MethodPost:
			conn, err := svc.Reissue(id)
			if err != nil {
				connectorRefusal(w, err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]any{"connection": conn})
		case verb == "" && r.Method == http.MethodPatch:
			var b struct {
				Name string `json:"name"`
			}
			if !read(w, r, &b) {
				return
			}
			conn, err := svc.Rename(id, b.Name)
			if err != nil {
				connectorRefusal(w, err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]any{"connection": conn})
		case verb == "" && r.Method == http.MethodDelete:
			if err := svc.Remove(id); err != nil {
				connectorRefusal(w, err)
				return
			}
			ruleEngine.Forget(id)
			secretJSON(w, http.StatusOK, map[string]bool{"ok": true})
		default:
			notAllowed(w)
		}
	}

	actions := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r) {
			return
		}
		rest, sub := under(r, actionsBase)
		if !sub {
			if r.Method != http.MethodGet {
				notAllowed(w)
				return
			}
			// ?rev=N waits until the revision moves from N (at most 25 s): the chat's card follows without polling.
			if v := r.URL.Query().Get("rev"); v != "" {
				after, _ := strconv.ParseUint(v, 10, 64)
				svc.WaitRev(r.Context(), after, 25*time.Second)
			}
			secretJSON(w, http.StatusOK, map[string]any{"proposals": svc.Proposals(), "rev": strconv.FormatUint(svc.Rev(), 10)})
			return
		}
		id, verb, _ := strings.Cut(rest, "/")
		if r.Method != http.MethodPost || (verb != "approve" && verb != "decline") {
			notAllowed(w)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), connectorCallMax)
		defer cancel()
		p, err := svc.Answer(ctx, id, verb == "approve")
		if err != nil {
			connectorRefusal(w, err)
			return
		}
		agent.Log("info", "connectors: the person answered an action", agent.F{K: "proposal", V: id}, agent.F{K: "state", V: p.State})
		secretJSON(w, http.StatusOK, map[string]any{"proposal": p})
	}

	sends := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r) {
			return
		}
		if r.Method != http.MethodGet {
			notAllowed(w)
			return
		}
		list, err := svc.Journal(200)
		if err != nil {
			connectorRefusal(w, err)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"sent": list})
	}

	// incoming is a request the hub passed on from a webhook's address, through the machine agent (its session): the
	// answer is always 200 with the status the hub gives the sender, so the agent hands it back as it is. The body and
	// the path's secret are never logged.
	incoming := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			notAllowed(w)
			return
		}
		var b struct {
			Connection string            `json:"connection"`
			Path       string            `json:"path"`
			Header     map[string]string `json:"header"`
			Body       []byte            `json:"body"`
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 2*webhook.MaxBody))
		if err != nil || json.Unmarshal(raw, &b) != nil {
			secretJSON(w, http.StatusOK, map[string]int{"status": http.StatusBadRequest})
			return
		}
		if len(b.Body) > webhook.MaxBody {
			secretJSON(w, http.StatusOK, map[string]int{"status": http.StatusRequestEntityTooLarge})
			return
		}
		h := http.Header{}
		for k, v := range b.Header {
			h.Set(k, v)
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		n, err := svc.Receive(ctx, b.Connection, connectors.Incoming{Header: h, Body: b.Body, Path: b.Path})
		status := http.StatusAccepted
		switch {
		case err == nil:
		case errors.Is(err, connectors.ErrDuplicate):
			status = http.StatusOK
		case errors.Is(err, connectors.ErrUnauthorized), errors.Is(err, connectors.ErrUnknown):
			// An unknown connection answers as a wrong secret: the address says nothing of what exists.
			status = http.StatusUnauthorized
		case errors.Is(err, connectors.ErrBadArgs):
			status = http.StatusBadRequest
		default:
			status = http.StatusServiceUnavailable
			agent.Log("warn", "connectors: a webhook request not kept", agent.F{K: "connection", V: b.Connection}, agent.F{K: "error", V: connectors.Say(err)})
		}
		agent.Log("info", "connectors: a webhook request", agent.F{K: "connection", V: b.Connection}, agent.F{K: "status", V: status}, agent.F{K: "events", V: n})
		secretJSON(w, http.StatusOK, map[string]int{"status": status})
	}

	cli := func(w http.ResponseWriter, r *http.Request) {
		if d.CLI == nil || !d.CLI.OK(r) {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
			return
		}
		rest, _ := under(r, cliBase)
		switch {
		case rest == "" && r.Method == http.MethodGet:
			listing(w, r, false)
		case rest == "read" && r.Method == http.MethodPost:
			var b struct {
				Connection string            `json:"connection"`
				Tool       string            `json:"tool"`
				Args       map[string]string `json:"args"`
			}
			if !read(w, r, &b) {
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), connectorCallMax)
			defer cancel()
			text, err := svc.Read(ctx, b.Connection, b.Tool, b.Args)
			if err != nil {
				connectorRefusal(w, err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]string{"text": text})
		case rest == "act" && r.Method == http.MethodPost:
			var b struct {
				Connection string            `json:"connection"`
				Action     string            `json:"action"`
				Args       map[string]string `json:"args"`
				Why        string            `json:"why"`
				Wait       float64           `json:"wait"`
				// ID is the proposal a repeated request keeps waiting on.
				ID string `json:"id"`
			}
			if !read(w, r, &b) {
				return
			}
			wait := max(min(time.Duration(b.Wait*float64(time.Second)), connectorActWaitMax), 0)
			if b.ID == "" {
				p, err := svc.Propose(b.Connection, b.Action, b.Args, b.Why, byOf(r))
				if err != nil {
					connectorRefusal(w, err)
					return
				}
				agent.Log("info", "connectors: an agent proposed an action", agent.F{K: "proposal", V: p.ID}, agent.F{K: "action", V: p.Action}, agent.F{K: "conv", V: p.Conv})
				b.ID = p.ID
			}
			p, err := svc.Wait(r.Context(), b.ID, wait)
			if err != nil {
				connectorRefusal(w, err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]any{"proposal": p})
		default:
			notAllowed(w)
		}
	}

	return []gate.Route{
		{Match: func(r *http.Request) bool { _, ok := under(r, base); return pathIs(r, base) || ok }, Handler: http.HandlerFunc(page)},
		{Match: func(r *http.Request) bool { _, ok := under(r, actionsBase); return pathIs(r, actionsBase) || ok }, Handler: http.HandlerFunc(actions)},
		{Match: func(r *http.Request) bool { return pathIs(r, sendsBase) }, Handler: http.HandlerFunc(sends)},
		{Match: func(r *http.Request) bool { return pathIs(r, platformAPI+"/connector-webhook") }, Handler: http.HandlerFunc(incoming)},
		{Match: func(r *http.Request) bool { _, ok := under(r, cliBase); return pathIs(r, cliBase) || ok }, Handler: http.HandlerFunc(cli)},
	}
}

// telegramChatLink is the address of a chat of this machine on its hub, the same the hub gives its chats
// (<hub>/agents/?c=agent:<machine>:<chat>); nil-safe: "" when the machine is not a hub's (no button then).
func telegramChatLink(d deps) func(conv string) string {
	return func(conv string) string {
		if conv == "" {
			return ""
		}
		for _, file := range machineFiles(d.DataDir) {
			m, err := agent.LoadMachine(file)
			if err != nil || m.MachineID == "" || m.HubURL == "" {
				continue
			}
			return strings.TrimRight(m.HubURL, "/") + "/agents/?c=" + url.QueryEscape("agent:"+m.MachineID+":"+conv)
		}
		return ""
	}
}

// telegramHooks are what the Telegram bot connector needs of the runtime: the person's message into a chat (as theirs,
// from the device "telegram"), the quiet hours of the asks' settings, a link back to harnessly, the person's language.
func telegramHooks(d deps, c *core) telegram.Hooks {
	return telegram.Hooks{
		ToChat: func(ctx context.Context, conv, text string) error {
			rt := (peerLocal{c: c}).rt()
			if rt == nil {
				return errNoCore
			}
			raw := make([]byte, 8)
			_, _ = rand.Read(raw)
			id := "tg-" + hex.EncodeToString(raw)
			res := rt.Do(ctx, "telegram", wire.SendCmd{OpID: id, ClientID: id, ConversationID: conv, Text: text,
				AttachmentIDs: []string{}, Mode: wire.ModeQueue, Kind: wire.KindPrompt})
			if res.R == wire.ResultRefused {
				return errors.New("the chat did not take the message")
			}
			return nil
		},
		Quiet: func(t time.Time) bool {
			set, err := asks.OpenSettings(d.DataDir).Get()
			return err == nil && set.Quiet.InQuiet(t)
		},
		Open: telegramChatLink(d),
		Taken: func(botID int64) bool {
			bots, err := notifybots.Open(d.DataDir).List()
			if err != nil {
				return false
			}
			for _, b := range bots {
				if b.TelegramID == botID {
					return true
				}
			}
			return false
		},
		Russian: func() bool { return locale.Pick("en", "ru") == "ru" },
	}
}
