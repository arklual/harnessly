package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/peers"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Messages between agents (internal/peers). Who may call what:
//
//	POST /api/peers/inbox, GET /api/peers/directory   another machine, through the fleet's gateway: a token signed
//	                                                   with this runtime's own secret (peers.VerifyInbox)
//	GET  /api/cli/peers, POST /api/cli/peers/send      an agent of this machine: the cli-token (harnessly peers)
//	GET|PUT <platform>/peers/settings, POST <platform>/peers/send   the person's page: a page session
//
// The person's page never takes the cli-token, and the agents' routes never take a page session alone.

// peerLocal is the service's view of this runtime's conversations.
type peerLocal struct{ c *core }

var errNoCore = errors.New("the conversations are not running")

func (p peerLocal) rt() *convo.Runtime {
	if p.c == nil {
		return nil
	}
	if v := p.c.v2.Load(); v != nil {
		return v.rt
	}
	return nil
}

func chatState(k wire.AgentStateKind) peers.ChatState {
	switch k {
	case wire.StateWorking, wire.StateStarting, wire.StateWaitingQuestion, wire.StateWaitingPermission:
		return peers.ChatBusy
	case wire.StateGone, wire.StateLaunchFailed:
		return peers.ChatGone
	}
	return peers.ChatIdle
}

func (p peerLocal) Chats(ctx context.Context) ([]peers.Chat, error) {
	rt := p.rt()
	if rt == nil {
		return nil, errNoCore
	}
	snap, err := rt.Conversations(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]peers.Chat, 0, len(snap.Conversations))
	for _, c := range snap.Conversations {
		out = append(out, peers.Chat{Conv: c.ID, Title: c.Title, State: chatState(c.State.K)})
	}
	return out, nil
}

func (p peerLocal) Deliver(ctx context.Context, conv, clientID, text string, steer bool) (peers.Refusal, string) {
	rt := p.rt()
	if rt == nil {
		return peers.RefuseFailed, string(wire.RefuseTimeout)
	}
	res := rt.DeliverPeer(ctx, conv, clientID, text, steer)
	if res.R != wire.ResultRefused {
		return "", ""
	}
	switch res.Code {
	case wire.RefuseNotFound:
		return peers.RefuseUnknownChat, ""
	case wire.RefuseAgentGone:
		return peers.RefuseAgentGone, ""
	case wire.RefuseTextTooLong:
		return peers.RefuseTooLong, ""
	}
	return peers.RefuseFailed, string(res.Code)
}

var fleetIDRe = regexp.MustCompile(`[^a-z0-9-]+`)

// peerSelf is this machine and its owner: RUNTIME_FLEET_ID (the id the fleet's gateway knows the machine by) and
// RUNTIME_MACHINE_NAME, else the host name; RUNTIME_OWNER_EMAIL and RUNTIME_OWNER_NAME.
func peerSelf(getenv func(string) string) peers.Sender {
	host, _ := os.Hostname()
	host = strings.TrimPrefix(host, getenv("RUNTIME_MACHINE_NAME_PREFIX"))
	id := strings.TrimSpace(getenv("RUNTIME_FLEET_ID"))
	if id == "" {
		id = strings.Trim(fleetIDRe.ReplaceAllString(strings.ToLower(host), "-"), "-")
	}
	if id == "" || id[0] < 'a' || id[0] > 'z' {
		id = "m-" + id
	}
	if len(id) > 64 {
		id = id[:64]
	}
	name := strings.TrimSpace(getenv("RUNTIME_MACHINE_NAME"))
	if name == "" {
		name = host
	}
	return peers.Sender{Person: strings.TrimSpace(getenv("RUNTIME_OWNER_EMAIL")), PersonName: strings.TrimSpace(getenv("RUNTIME_OWNER_NAME")),
		Machine: id, MachineName: name}
}

// newPeers is the runtime's service; the way to the other machines is RUNTIME_PEER_RELAY_URL (the gateway's relay),
// signed with the runtime's own secret: no secret of its own.
func newPeers(d deps, c *core) *peers.Service {
	self := peerSelf(os.Getenv)
	s := &peers.Service{Self: self, Local: peerLocal{c: c},
		Settings: &peers.Store{Path: filepath.Join(d.DataDir, "v2", "peers.json")},
		Audit:    &peers.Audit{Path: filepath.Join(d.DataDir, "v2", "peers-audit.json")}}
	if url := strings.TrimSpace(os.Getenv("RUNTIME_PEER_RELAY_URL")); url != "" && len(d.Secret) > 0 {
		s.Relay = &peers.HTTPRelay{URL: url, Secret: d.Secret, Self: self}
	}
	return s
}

// peerSettingsView is GET <platform>/peers/settings: everything the section shows.
type peerSettingsView struct {
	Self     peers.Sender   `json:"self"`
	Settings peers.Settings `json:"settings"`
	// Relay: this machine reaches the other machines (the gateway's relay is configured).
	Relay   bool           `json:"relay"`
	Senders []peers.Sender `json:"senders"`
	Audit   []peers.Record `json:"audit"`
}

const peerAuditShown = 50

type peerSendBody struct {
	// Session is the sending agent's tmux session (the CLI's own); From the sending chat (the page's).
	Session string `json:"session"`
	From    string `json:"from"`
	To      string `json:"to"`
	Text    string `json:"text"`
}

func peerJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func peerRoutes(d deps, c *core) []gate.Route {
	svc := newPeers(d, c)
	route := func(p string, h http.HandlerFunc) gate.Route {
		return gate.Route{Match: func(r *http.Request) bool { return pathIs(r, p) }, Handler: h}
	}
	cliOK := func(w http.ResponseWriter, r *http.Request) bool {
		if d.CLI == nil || !d.CLI.OK(r) {
			peerJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
			return false
		}
		return true
	}
	pageOK := func(w http.ResponseWriter, r *http.Request) bool {
		// The session cookie goes with a request of any site: a page of another origin must not send or rewrite rules.
		if sameorigin.Refuse(w, r) {
			return false
		}
		if !d.SessionOK(r) {
			peerJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return false
		}
		return true
	}
	send := func(w http.ResponseWriter, r *http.Request, by string) {
		var b peerSendBody
		if r.Method != http.MethodPost || !peers.ReadJSON(r, &b) {
			peerJSON(w, http.StatusBadRequest, map[string]string{"error": "POST {to, text}"})
			return
		}
		from := b.From
		if by == "agent" {
			from = ""
			if rt := (peerLocal{c: c}).rt(); rt != nil && b.Session != "" {
				from = rt.ConvOfSession(b.Session)
			}
		}
		peerJSON(w, http.StatusOK, svc.Send(r.Context(), from, b.To, b.Text, by))
	}
	return []gate.Route{
		route("/api/peers/inbox", svc.InboxHandler(d.Secret).ServeHTTP),
		route("/api/peers/directory", svc.DirectoryHandler(d.Secret).ServeHTTP),
		route("/api/cli/peers", func(w http.ResponseWriter, r *http.Request) {
			if !cliOK(w, r) {
				return
			}
			chats, errs := svc.Directory(r.Context(), svc.Self, r.URL.Query().Get("remote") != "0")
			if errs == nil {
				errs = []string{}
			}
			peerJSON(w, http.StatusOK, map[string]any{"self": svc.Self, "chats": chats, "errors": errs})
		}),
		route("/api/cli/peers/send", func(w http.ResponseWriter, r *http.Request) {
			if cliOK(w, r) {
				send(w, r, "agent")
			}
		}),
		route(platformAPI+"/peers/send", func(w http.ResponseWriter, r *http.Request) {
			if pageOK(w, r) {
				send(w, r, "person")
			}
		}),
		route(platformAPI+"/peers/settings", func(w http.ResponseWriter, r *http.Request) {
			if !pageOK(w, r) {
				return
			}
			switch r.Method {
			case http.MethodGet:
			case http.MethodPut:
				var next peers.Settings
				if !peers.ReadJSON(r, &next) {
					peerJSON(w, http.StatusBadRequest, map[string]string{"error": "bad settings"})
					return
				}
				if _, err := svc.Settings.Put(next); err != nil {
					status := http.StatusBadRequest
					if errors.Is(err, peers.ErrConflict) {
						status = http.StatusConflict
					}
					peerJSON(w, status, map[string]string{"error": err.Error()})
					return
				}
			default:
				peerJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
				return
			}
			peerJSON(w, http.StatusOK, peerSettingsView{Self: svc.Self, Settings: svc.Settings.Get(), Relay: svc.Relay != nil,
				Senders: svc.Audit.Senders(), Audit: svc.Audit.Recent(peerAuditShown)})
		}),
	}
}
