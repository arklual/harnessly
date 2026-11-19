package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/mcpconn"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// The person's connections for the views of an app: hl.connections.list / tools / call / api / request.
//
//	POST <platform>/apps/connections  {session, op, connection, tool, args, method, path, params, body, access, confirmed}
//
//	op list      → {connections: [...]}                 what the manifest names and where each stands
//	op tools     → {tools: [...]}                       the tools of a connection (name, description, schema, readOnly)
//	op call      → {result} | {needsConfirm}            a tool: the MCP result as the server gave it
//	op api       → {status, contentType, body} | {needsConfirm}   a request to an API connection
//	op request   → {granted, access} | {needsConsent}   ask for the person's agreement (the page shows the card)
//	op grant     → {granted, access}                    the page puts the person's yes into the agreements
//
// The person's page session only; a view reaches it through the bridge, never itself. What the runtime holds back:
//
//   - A view reaches only what the manifest names (an id or a group of the catalog) AND the person agreed to for this
//     app (connection-grants.json), at the level both allow: read, or all. An agreement the installed version no
//     longer declares ends with the declaration.
//   - read is a tool the server marks readOnlyHint and a GET (or a search the catalog lists); anything else is a
//     change: it needs the right connections.act, the level all, and the person's yes to this very call (confirmed,
//     which only the page sets, after its own dialog shows the tool and the arguments). Without it the answer is
//     needsConfirm and nothing is sent.
//   - The sign-in, the keys and the server's address stay in the connections' service. An answer and an error go
//     through mcpconn.Redact: a key a server echoes comes back as [key]. A refusal names the connection to sign in to
//     (auth_required: «ждёт вход»), never a header.
//   - 20 calls in 10 seconds per app and connection (rate_limited), 512 KiB of answer (quota_exceeded), 30 seconds.
const (
	appConnWindow    = 10 * time.Second
	appConnBurst     = 20
	appConnAnswerMax = 512 << 10
	appConnArgsMax   = 256 << 10
	appConnTimeout   = 30 * time.Second
)

// The statuses of a connection a view lists.
const (
	connReady        = "ready"
	connNeedsConsent = "needs_consent"
	connNotConnected = "not_connected"
	connNeedsSignIn  = "needs_signin"
	connOff          = "off"
	connUnavailable  = "unavailable"
)

// appConnInfo is a connection as a view sees it: nothing of its keys, address or headers.
type appConnInfo struct {
	ID       string `json:"id"`
	Slug     string `json:"slug,omitempty"`
	Name     string `json:"name"`
	Kind     string `json:"kind,omitempty"`
	Category string `json:"category,omitempty"`
	Status   string `json:"status"`
	// Declared is how far the manifest goes with it; Access how far the person agreed ("" for not at all).
	Declared string `json:"declared"`
	Access   string `json:"access"`
	Reason   string `json:"reason,omitempty"`
	Guide    string `json:"guide,omitempty"`
}

type connLimiter struct {
	mu sync.Mutex
	at map[string][]time.Time
}

func (l *connLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.at == nil {
		l.at = map[string][]time.Time{}
	}
	kept := l.at[key][:0]
	for _, t := range l.at[key] {
		if now.Sub(t) < appConnWindow {
			kept = append(kept, t)
		}
	}
	if len(kept) >= appConnBurst {
		l.at[key] = kept
		return false
	}
	l.at[key] = append(kept, now)
	return true
}

// appConns is the machine's side of hl.connections.
type appConns struct {
	ms  *mcpconn.Service
	reg *apps.Registry
	lim connLimiter
	now func() time.Time
}

func (c *appConns) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func refuseWith(code, message, detail string) error {
	return &apps.BridgeError{Code: code, Message: message, Detail: detail}
}

// pkgOf is the installed package of the app.
func (c *appConns) pkgOf(app string) (*apps.Package, error) {
	pkg, err := c.reg.Package(app)
	if err != nil {
		return nil, err
	}
	if pkg.Manifest == nil {
		return nil, apps.ErrNotInstalled
	}
	return pkg, nil
}

func statusOf(s mcpconn.Server, access string) string {
	switch {
	case !s.Enabled:
		return connOff
	case access == "":
		return connNeedsConsent
	case s.State == mcpconn.StateSignIn || s.State == mcpconn.StateDenied:
		return connNeedsSignIn
	case s.State == mcpconn.StateUnreachable || s.Problem != "":
		return connUnavailable
	}
	return connReady
}

func (c *appConns) infoOf(pkg *apps.Package, app string, s mcpconn.Server) appConnInfo {
	access := c.reg.EffectiveConnectionAccess(app, s.CatalogID)
	out := appConnInfo{ID: s.CatalogID, Slug: s.Slug, Name: s.Name, Kind: mcpconn.AppKindOf(s), Category: mcpconn.KindOf(s),
		Status: statusOf(s, access), Declared: pkg.DeclaredAccess(s.CatalogID), Access: access, Reason: pkg.ConnectionReason(s.CatalogID)}
	if out.Kind == mcpconn.AppKindAPI && out.Status == connReady {
		out.Guide = s.Guide
	}
	return out
}

// list is what the manifest names, each as the person has it: a connection of the person's, or, for a catalog id with
// none (and a group with none), a line that says it is not connected.
func (c *appConns) list(app string) ([]appConnInfo, error) {
	pkg, err := c.pkgOf(app)
	if err != nil {
		return nil, err
	}
	servers, err := c.ms.List()
	if err != nil {
		return nil, refuseWith(apps.CodeHostUnavailable, apps.ToolRefused, err.Error())
	}
	out := []appConnInfo{}
	seen := map[string]bool{}
	for _, n := range pkg.ConnectionNeeds() {
		matched := false
		for _, s := range servers {
			if s.Owner != "" || s.CatalogID == "" {
				continue
			}
			if n.ID != "" && s.CatalogID == n.ID || n.Category != "" && mcpconn.KindOf(s) == n.Category {
				matched = true
				if !seen[s.ID] {
					seen[s.ID] = true
					out = append(out, c.infoOf(pkg, app, s))
				}
			}
		}
		if !matched {
			info := appConnInfo{ID: n.ID, Category: n.Category, Status: connNotConnected, Declared: n.Level(), Reason: n.Reason}
			if n.ID != "" {
				info.Name, info.Category = mcpconn.CatalogName(n.ID), mcpconn.CatalogKind(n.ID)
				info.Declared = pkg.DeclaredAccess(n.ID)
				// The person may agree before they connect: the agreement is of the app and the catalog entry.
				info.Access = c.reg.EffectiveConnectionAccess(app, n.ID)
			}
			out = append(out, info)
		}
	}
	return out, nil
}

// target is the connection a call names, with what the app may do with it.
type target struct {
	srv      mcpconn.Server
	id       string
	declared string
	access   string
}

// resolve finds the person's connection a view names (its catalog id, or its slug from list) and checks the manifest
// names it. An id the manifest names that the person has not connected is auth_required, naming it: «ждёт вход». One
// the manifest does not name is refused alike whether the person has it or not.
func (c *appConns) resolve(pkg *apps.Package, app, ref string) (target, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" || len(ref) > 120 {
		return target{}, refuseWith(apps.CodeInvalidParams, "connection is the catalog id or the slug that hl.connections.list gives", "")
	}
	servers, err := c.ms.List()
	if err != nil {
		return target{}, refuseWith(apps.CodeHostUnavailable, apps.ToolRefused, err.Error())
	}
	var found *mcpconn.Server
	for _, byCatalog := range []bool{false, true} {
		for i, s := range servers {
			if s.Owner != "" || s.CatalogID == "" {
				continue
			}
			if !byCatalog && s.Slug == ref || byCatalog && s.CatalogID == ref {
				// Of several connections made from one catalog entry, the one that works now.
				if found == nil || found.State != mcpconn.StateOK && s.State == mcpconn.StateOK {
					found = &servers[i]
				}
			}
		}
		if found != nil {
			break
		}
	}
	notNamed := refuseWith(apps.CodePermissionDenied, "the app's manifest names no connection "+clipDetail(ref)+" (its `connections`)", "")
	if found == nil {
		if pkg.DeclaredAccess(ref) == "" {
			return target{}, notNamed
		}
		return target{}, &apps.BridgeError{Code: apps.CodeAuthRequired, Message: apps.AuthRequiredMessage, SignIn: &apps.SignIn{Connection: ref},
			Detail: "the person has no connection " + ref}
	}
	declared := pkg.DeclaredAccess(found.CatalogID)
	if declared == "" {
		return target{}, notNamed
	}
	return target{srv: *found, id: found.CatalogID, declared: declared, access: c.reg.EffectiveConnectionAccess(app, found.CatalogID)}, nil
}

// usable refuses a connection the person has not agreed to for this app, or that cannot be used now: off, waiting for a
// sign-in. These are the refusals a view shows as «нужно согласие» and «ждёт вход».
func (t target) usable() error {
	signIn := &apps.SignIn{Connection: t.id}
	switch {
	case t.access == "":
		return refuseWith(apps.CodePermissionDenied, "the person has not agreed to "+t.srv.Name+" for this app: ask with hl.connections.request", "")
	case !t.srv.Enabled:
		return &apps.BridgeError{Code: apps.CodeAuthRequired, Message: apps.AuthRequiredMessage, SignIn: signIn, Detail: "the connection is turned off"}
	case t.srv.State == mcpconn.StateSignIn:
		return &apps.BridgeError{Code: apps.CodeAuthRequired, Message: apps.AuthRequiredMessage, SignIn: signIn, Detail: "the connection waits for a sign-in"}
	}
	return nil
}

// upstream turns an error of the connections' service into the refusal a view reads: a sign-in to make names the
// connection; nothing of the server's own words goes on (they may carry a token).
func upstream(err error, t target) error {
	signIn := &apps.SignIn{Connection: t.id}
	detail := "connection " + t.id + ": " + err.Error()
	switch {
	case errors.Is(err, mcpconn.ErrSignInExpired):
		return &apps.BridgeError{Code: apps.CodeAuthExpired, Message: apps.AuthExpiredMessage, SignIn: signIn, Detail: detail}
	case errors.Is(err, mcpconn.ErrSignInRequired), errors.Is(err, mcpconn.ErrNotFound):
		return &apps.BridgeError{Code: apps.CodeAuthRequired, Message: apps.AuthRequiredMessage, SignIn: signIn, Detail: detail}
	case errors.Is(err, mcpconn.ErrDenied), errors.Is(err, mcpconn.ErrOAuthFailed):
		return &apps.BridgeError{Code: apps.CodeAuthDenied, Message: apps.AuthDeniedMessage, Detail: detail}
	case errors.Is(err, mcpconn.ErrNoRuntime):
		return refuseWith(apps.CodeHostUnavailable, apps.ToolRefused, detail)
	case errors.Is(err, mcpconn.ErrBadRequest):
		return refuseWith(apps.CodeInvalidParams, "the request does not fit the connection (a path outside its API, a method it does not take)", detail)
	}
	return refuseWith(apps.CodeUpstreamError, "the service did not answer", detail)
}

func (c *appConns) throttle(app string, t target) error {
	if !c.lim.allow(app+"/"+t.id, c.clock()) {
		return refuseWith(apps.CodeRateLimited, fmt.Sprintf("%d calls in %d seconds to one connection", appConnBurst, int(appConnWindow.Seconds())), "")
	}
	return nil
}

// A change a view asked for: the page asks the person about exactly this, then repeats the call with confirmed.
type connConfirm struct {
	Connection string `json:"connection"`
	Name       string `json:"name"`
	// What is "tool <name>" or "<METHOD> <path>"; Detail the arguments or the body, as the person reads them.
	What   string `json:"what"`
	Detail string `json:"detail,omitempty"`
}

// writes holds back a change: the right, the level and the person's yes to this call. A refusal says which is missing.
func (c *appConns) writes(m *apps.Manifest, t target, what, detail string, confirmed bool) (*connConfirm, error) {
	switch {
	case !m.Allows("connections.act"):
		return nil, refuseWith(apps.CodePermissionDenied, what+" may change something: the app has no permission connections.act", "")
	case t.access != apps.AccessAll:
		return nil, refuseWith(apps.CodePermissionDenied, what+" may change something: the person agreed to "+t.srv.Name+" for reading only", "")
	case !confirmed:
		return &connConfirm{Connection: t.id, Name: t.srv.Name, What: what, Detail: clipDetail2(detail, 1500)}, nil
	}
	return nil, nil
}

func clipDetail2(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// guardAnswer keeps a key out of an answer and the answer within its size.
func (c *appConns) guardAnswer(t target, raw []byte) (json.RawMessage, error) {
	if len(raw) > appConnAnswerMax {
		return nil, refuseWith(apps.CodeQuotaExceeded, "the answer is larger than the 512 KiB an app gets: ask for less", "")
	}
	return c.ms.Redact(t.srv.ID, raw), nil
}

func (c *appConns) tools(ctx context.Context, app, ref string) ([]mcpconn.AppTool, error) {
	pkg, err := c.pkgOf(app)
	if err != nil {
		return nil, err
	}
	t, err := c.resolve(pkg, app, ref)
	if err != nil {
		return nil, err
	}
	if err := t.usable(); err != nil {
		return nil, err
	}
	if t.srv.Transport == mcpconn.API {
		return nil, refuseWith(apps.CodeInvalidParams, t.srv.Name+" is an API with no tools: use hl.connections.api", "")
	}
	if err := c.throttle(app, t); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, appConnTimeout)
	defer cancel()
	tools, err := c.ms.AppTools(ctx, t.srv.ID)
	if err != nil {
		return nil, upstream(err, t)
	}
	if tools == nil {
		tools = []mcpconn.AppTool{}
	}
	return tools, nil
}

// call runs a tool. The answer is the MCP result; a change that waits for the person's yes comes back as confirm.
func (c *appConns) call(ctx context.Context, app, ref, tool string, args map[string]any, confirmed bool) (json.RawMessage, *connConfirm, error) {
	pkg, err := c.pkgOf(app)
	if err != nil {
		return nil, nil, err
	}
	t, err := c.resolve(pkg, app, ref)
	if err != nil {
		return nil, nil, err
	}
	if err := t.usable(); err != nil {
		return nil, nil, err
	}
	if tool = strings.TrimSpace(tool); tool == "" || len(tool) > 200 {
		return nil, nil, refuseWith(apps.CodeInvalidParams, "tool is the name of one of hl.connections.tools", "")
	}
	if t.srv.Transport == mcpconn.API {
		return nil, nil, refuseWith(apps.CodeInvalidParams, t.srv.Name+" is an API with no tools: use hl.connections.api", "")
	}
	raw, err := json.Marshal(args)
	if err != nil || len(raw) > appConnArgsMax {
		return nil, nil, refuseWith(apps.CodeInvalidParams, "args are a JSON object of up to 256 KiB", "")
	}
	if err := c.throttle(app, t); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, appConnTimeout)
	defer cancel()
	tools, err := c.ms.AppTools(ctx, t.srv.ID)
	if err != nil {
		return nil, nil, upstream(err, t)
	}
	i := slices.IndexFunc(tools, func(s mcpconn.AppTool) bool { return s.Name == tool })
	if i < 0 {
		return nil, nil, refuseWith(apps.CodeInvalidParams, t.srv.Name+" has no tool "+clipDetail(tool), "")
	}
	class := apps.ClassHigh
	if !tools[i].ReadOnly {
		class = apps.ClassCritical
		ask, err := c.writes(pkg.Manifest, t, "tool "+tool, string(raw), confirmed)
		if err != nil || ask != nil {
			return nil, ask, err
		}
	}
	res, err := c.ms.CallTool(ctx, t.srv.ID, tool, raw)
	_ = c.reg.Journal(app, apps.JournalEntry{What: "connections.call", Class: class, OK: err == nil, Detail: t.id + " " + clipDetail(tool)})
	if err != nil {
		return nil, nil, upstream(err, t)
	}
	out, err := c.guardAnswer(t, res)
	return out, nil, err
}

var apiMethods = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"}

type apiAnswer struct {
	Status      int    `json:"status"`
	ContentType string `json:"contentType"`
	Body        string `json:"body"`
}

// api makes a request to an API connection.
func (c *appConns) api(ctx context.Context, app, ref, method, path string, params map[string]any, body json.RawMessage, confirmed bool) (*apiAnswer, *connConfirm, error) {
	pkg, err := c.pkgOf(app)
	if err != nil {
		return nil, nil, err
	}
	t, err := c.resolve(pkg, app, ref)
	if err != nil {
		return nil, nil, err
	}
	if err := t.usable(); err != nil {
		return nil, nil, err
	}
	if t.srv.Transport != mcpconn.API {
		return nil, nil, refuseWith(apps.CodeInvalidParams, t.srv.Name+" is not an API: use hl.connections.tools and call", "")
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "GET"
	}
	if !slices.Contains(apiMethods, method) {
		return nil, nil, refuseWith(apps.CodeInvalidParams, "method is GET, HEAD, POST, PUT, PATCH or DELETE", "")
	}
	if path = strings.TrimSpace(path); path == "" || len(path) > 2048 {
		return nil, nil, refuseWith(apps.CodeInvalidParams, "path is under the API's base, e.g. /v1/items", "")
	}
	if len(body) > appConnArgsMax {
		return nil, nil, refuseWith(apps.CodeInvalidParams, "body is up to 256 KiB", "")
	}
	query := map[string]string{}
	for k, v := range params {
		query[k] = fmt.Sprint(v)
	}
	if err := c.throttle(app, t); err != nil {
		return nil, nil, err
	}
	class := apps.ClassHigh
	if !mcpconn.APIReads(t.id, method, path) {
		class = apps.ClassCritical
		ask, err := c.writes(pkg.Manifest, t, method+" "+path, string(body), confirmed)
		if err != nil || ask != nil {
			return nil, ask, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, appConnTimeout)
	defer cancel()
	ans, err := c.ms.CallAPI(ctx, t.srv.ID, mcpconn.APIRequest{Method: method, Path: path, Query: query, Body: body})
	_ = c.reg.Journal(app, apps.JournalEntry{What: "connections.api", Class: class, OK: err == nil, Detail: t.id + " " + method + " " + clipDetail(strings.SplitN(path, "?", 2)[0])})
	if err != nil {
		return nil, nil, upstream(err, t)
	}
	raw, err := c.guardAnswer(t, ans.Body)
	if err != nil {
		return nil, nil, err
	}
	return &apiAnswer{Status: ans.Status, ContentType: ans.ContentType, Body: string(raw)}, nil, nil
}

// consentCard is what the page asks the person: this app wants this connection, as far as it says.
type consentCard struct {
	Connection string `json:"connection"`
	Name       string `json:"name"`
	Access     string `json:"access"`
	Reason     string `json:"reason,omitempty"`
}

// request is a view asking for the person's agreement to a connection: already given as far as asked, or the card the
// page shows. A connection the person does not have is asked for first: auth_required.
func (c *appConns) request(app, ref, access string) (string, *consentCard, error) {
	pkg, err := c.pkgOf(app)
	if err != nil {
		return "", nil, err
	}
	t, err := c.resolve(pkg, app, ref)
	if err != nil {
		return "", nil, err
	}
	switch access {
	case "":
		access = t.declared
	case apps.AccessRead, apps.AccessAll:
		if access == apps.AccessAll && t.declared != apps.AccessAll {
			return "", nil, refuseWith(apps.CodePermissionDenied, "the manifest names "+t.srv.Name+" for reading only", "")
		}
	default:
		return "", nil, refuseWith(apps.CodeInvalidParams, "access is read or all", "")
	}
	if t.access == apps.AccessAll || t.access == access {
		return t.access, nil, nil
	}
	return "", &consentCard{Connection: t.id, Name: t.srv.Name, Access: access, Reason: pkg.ConnectionReason(t.id)}, nil
}

// grant puts the person's yes, given in the page's card, into the agreements.
func (c *appConns) grant(app, ref, access string) (string, error) {
	pkg, err := c.pkgOf(app)
	if err != nil {
		return "", err
	}
	t, err := c.resolve(pkg, app, ref)
	if err != nil {
		return "", err
	}
	if err := c.reg.GrantConnectionAccess(app, t.id, access); err != nil {
		return "", refuseWith(apps.CodePermissionDenied, err.Error(), "")
	}
	_ = c.reg.Journal(app, apps.JournalEntry{What: "connections.grant", Class: apps.ClassHigh, OK: true, Detail: t.id + " " + c.reg.ConnectionAccess(app, t.id)})
	return c.reg.ConnectionAccess(app, t.id), nil
}

type connBody struct {
	bridgeBody
	Connection string          `json:"connection"`
	Tool       string          `json:"tool"`
	Method     string          `json:"method"`
	Params     map[string]any  `json:"params"`
	Body       json.RawMessage `json:"body"`
	Access     string          `json:"access"`
}

// connectionOps are the bridge methods of the ops, for the app's right and the target.
var connectionOps = map[string]string{
	"list": "harnessly/connections.list", "tools": "harnessly/connections.tools", "call": "harnessly/connections.call",
	"api": "harnessly/connections.api", "request": "harnessly/connections.request", "grant": "harnessly/connections.request",
}

func appConnectionRoutes(d deps, reg *apps.Registry, storage *apps.Storage, views *apps.ViewServer, live appLive) []gate.Route {
	ms := mcpFor(d)
	conns := &appConns{ms: ms, reg: reg}
	watchConnections(ms, reg, storage)
	handler := func(w http.ResponseWriter, r *http.Request) {
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
		var b connBody
		if !readSecretBody(r, &b) {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		status, out := func() (int, any) {
			method, ok := connectionOps[b.Op]
			if !ok {
				return bridgeRefused(apps.ErrUnknownMethod)
			}
			sess, app, err := live(b.bridgeBody)
			if err != nil {
				return bridgeRefused(err)
			}
			if err := app.Manifest.Authorize(method, sess.Target, nil); err != nil {
				return bridgeRefused(err)
			}
			refused := func(err error) (int, any) { return bridgeRefused(err) }
			ctx := r.Context()
			switch b.Op {
			case "list":
				list, err := conns.list(sess.App)
				if err != nil {
					return refused(err)
				}
				return http.StatusOK, map[string]any{"connections": list}
			case "tools":
				tools, err := conns.tools(ctx, sess.App, b.Connection)
				if err != nil {
					return refused(err)
				}
				return http.StatusOK, map[string]any{"tools": tools}
			case "call":
				res, ask, err := conns.call(ctx, sess.App, b.Connection, b.Tool, b.Args, b.Confirmed)
				switch {
				case err != nil:
					return refused(err)
				case ask != nil:
					return http.StatusOK, map[string]any{"needsConfirm": ask}
				}
				return http.StatusOK, map[string]any{"result": res}
			case "api":
				ans, ask, err := conns.api(ctx, sess.App, b.Connection, b.Method, b.Path, b.Params, b.Body, b.Confirmed)
				switch {
				case err != nil:
					return refused(err)
				case ask != nil:
					return http.StatusOK, map[string]any{"needsConfirm": ask}
				}
				return http.StatusOK, ans
			case "request":
				level, card, err := conns.request(sess.App, b.Connection, b.Access)
				switch {
				case err != nil:
					return refused(err)
				case card != nil:
					return http.StatusOK, map[string]any{"granted": false, "needsConsent": card}
				}
				return http.StatusOK, map[string]any{"granted": true, "access": level}
			default: // grant: the page's own word after the person said yes in the card
				level, err := conns.grant(sess.App, b.Connection, b.Access)
				if err != nil {
					return refused(err)
				}
				storage.NotifyConnections(sess.App)
				return http.StatusOK, map[string]any{"granted": true, "access": level}
			}
		}()
		journalBridgeCall(r.Context(), views, b.bridgeBody, status, out)
		secretJSON(w, status, out)
	}
	return []gate.Route{
		{Match: func(r *http.Request) bool { return pathIs(r, platformAPI+"/apps/connections") }, Handler: http.HandlerFunc(handler)},
		{Match: func(r *http.Request) bool { return pathIs(r, "/agents"+platformAPI+"/apps/connections") }, Handler: http.HandlerFunc(handler)},
	}
}

var watching sync.Map // *mcpconn.Service → true: the one subscription of a service

// watchConnections tells the views of every app that names connections that they changed (a sign-in, a turn on or off,
// an agreement) — the event `connections` of the SDK. A burst of writes is told once.
func watchConnections(ms *mcpconn.Service, reg *apps.Registry, storage *apps.Storage) {
	if _, dup := watching.LoadOrStore(ms, true); dup {
		return
	}
	var mu sync.Mutex
	var timer *time.Timer
	ms.OnChange(func() {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			return
		}
		timer = time.AfterFunc(300*time.Millisecond, func() {
			mu.Lock()
			timer = nil
			mu.Unlock()
			list, err := reg.List()
			if err != nil {
				return
			}
			for _, a := range list {
				if a.Manifest != nil && len(a.Manifest.Connections) > 0 {
					storage.NotifyConnections(a.ID)
				}
			}
		})
	})
}
