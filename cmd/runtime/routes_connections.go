package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/connections"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
	"github.com/ly-solutions/harnessly/internal/secrets"
	"github.com/ly-solutions/harnessly/internal/services"
)

// Connections (internal/connections): the person's trackers, wikis and code hosts, connected in Settings →
// Connections. Who may call what:
//
//	GET|POST <platform>/connections, POST <platform>/connections/detect, GET <platform>/connections/resolve,
//	PATCH|DELETE <platform>/connections/<id>, POST <platform>/connections/<id>/check,
//	GET <platform>/connections/<id>/projects|issues|issue|mrs|pages|page
//	                                     the person's page: a page session, same origin
//	GET /api/cli/connections, /api/cli/connections/resolve, /api/cli/connections/<id>/projects|issues|issue|mrs|pages|page
//	                                     an agent of this machine: the cli-token (harnessly connections, issues, …);
//	                                     reads only
//
// Credentials come in only by the page's connect and are kept in the safe as conn-<id>; no answer and no log line
// carries one (a log line names ids, kinds and reasons).

const (
	// connCallMax bounds one route's calls to an instance.
	connCallMax = 30 * time.Second
	// connCredFresh is how long opened credentials are reused before the safe is asked again (each ask records a use).
	connCredFresh = 30 * time.Second
	// The deployment's tracker and GitLab, listed as connections.
	envTrackerID = "env-tracker"
	envGitLabID  = "env-gitlab"
)

var (
	safesMu  sync.Mutex
	safesFor = map[*gate.CLIToken]*safe{}
	connsMu  sync.Mutex
	connsFor = map[*gate.CLIToken]*connections.Service{}
)

// safeFor is the one safe of a runtime: the secrets routes and the connections share its store.
func safeFor(d deps) *safe {
	safesMu.Lock()
	defer safesMu.Unlock()
	if sf := safesFor[d.CLI]; sf != nil {
		return sf
	}
	sf := newSafe(d)
	safesFor[d.CLI] = sf
	return sf
}

// connSecrets keeps connections' credentials in the safe. A reveal is recorded as a use by "connection <name>".
type connSecrets struct {
	store secrets.Provider
	svc   *connections.Service
	// titleOf names the use of a secret in the safe's record; nil names it by the connection.
	titleOf func(name string) string

	mu    sync.Mutex
	cache map[string]connCred
}

type connCred struct {
	value string
	at    time.Time
}

func (a *connSecrets) Put(name, value string) error {
	a.forget(name)
	_, _, err := a.store.Put(name, value)
	return err
}

func (a *connSecrets) Remove(name string) error {
	a.forget(name)
	err := a.store.Remove(name)
	if err != nil && secrets.ReasonOf(err) == secrets.ReasonNotFound {
		return connections.ErrNotFound
	}
	return err
}

func (a *connSecrets) forget(name string) {
	a.mu.Lock()
	delete(a.cache, name)
	a.mu.Unlock()
}

func (a *connSecrets) Reveal(name string) (string, error) {
	a.mu.Lock()
	if c, ok := a.cache[name]; ok && time.Since(c.at) < connCredFresh {
		a.mu.Unlock()
		return c.value, nil
	}
	a.mu.Unlock()
	title := "connection " + strings.TrimPrefix(name, "conn-")
	if a.titleOf != nil {
		title = a.titleOf(name)
	} else if a.svc != nil {
		if c, err := a.svc.Get(strings.TrimPrefix(name, "conn-")); err == nil && c.Name != "" {
			title = "connection " + c.Name
		}
	}
	values, err := a.store.Reveal([]string{name}, secrets.Use{Title: title})
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	if a.cache == nil {
		a.cache = map[string]connCred{}
	}
	a.cache[name] = connCred{value: values[name], at: time.Now()}
	a.mu.Unlock()
	return values[name], nil
}

// loadConnAdapters registers the declared connection adapters once per process.
var loadConnAdapters sync.Once

// connectionsFor is the one connections service of a runtime.
func connectionsFor(d deps) *connections.Service {
	connsMu.Lock()
	defer connsMu.Unlock()
	if s := connsFor[d.CLI]; s != nil {
		return s
	}
	loadConnAdapters.Do(func() {
		// Only a declared file adds anything; a silent adapter is told on stderr and the built-in products go on.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, errs := connections.LoadAdapters(ctx, os.Getenv("RUNTIME_CONNECTION_ADAPTERS_FILE"))
		for _, err := range errs {
			fmt.Fprintf(os.Stderr, "runtime: %v\n", err)
		}
	})
	sec := &connSecrets{store: safeFor(d).store}
	s := connections.New(d.DataDir, sec, nil, envConnections(serviceConfig(d), time.Now().UTC())...)
	sec.svc = s
	connsFor[d.CLI] = s
	return s
}

// envConnections are the deployment's tracker (RUNTIME_TRACKER_URL, a YouTrack) and its own GitLab (the first of
// RUNTIME_GITLAB_HOSTS) as connections, with the tokens the tracker and GitLab routes read, taken on every use.
func envConnections(cfg services.Config, at time.Time) []connections.EnvConn {
	var out []connections.EnvConn
	tokenOf := func(read func(services.Config) string) func() connections.Credentials {
		return func() connections.Credentials {
			if t := read(cfg); t != "" {
				return connections.Credentials{connections.FieldToken: t}
			}
			return connections.Credentials{}
		}
	}
	if base := strings.TrimRight(strings.TrimSpace(cfg.TrackerURL), "/"); base != "" {
		if u, err := url.Parse(base); err == nil && u.Host != "" && u.User == nil {
			dep := connections.SelfHosted
			if connections.HostIs(u.Hostname(), "youtrack.cloud") || connections.HostIs(u.Hostname(), "myjetbrains.com") {
				dep = connections.Cloud
			}
			out = append(out, connections.EnvConn{
				Conn: connections.Conn{ID: envTrackerID, Kind: connections.YouTrack, Name: u.Host, BaseURL: base, Deployment: dep,
					Method: "pat", Capabilities: []connections.Capability{connections.Issues}, CreatedAt: at},
				Cred: tokenOf(services.TrackerToken),
			})
		}
	}
	if len(cfg.GitLabHosts) > 0 {
		if host := strings.TrimSpace(cfg.GitLabHosts[0]); host != "" && !strings.ContainsAny(host, "/@") {
			dep := connections.SelfHosted
			if strings.EqualFold(host, "gitlab.com") {
				dep = connections.Cloud
			}
			out = append(out, connections.EnvConn{
				Conn: connections.Conn{ID: envGitLabID, Kind: connections.GitLab, Name: host, BaseURL: "https://" + host, Deployment: dep,
					Method: "pat", Capabilities: []connections.Capability{connections.Issues, connections.Code}, CreatedAt: at},
				Cred: tokenOf(services.GitLabSelfToken),
			})
		}
	}
	return out
}

// connectionHosts are the hosts of the connections with issues or pages, for the page to know their links.
func connectionHosts(cs *connections.Service) []string {
	list, err := cs.List()
	if err != nil {
		return []string{}
	}
	out := []string{}
	for _, c := range list {
		if !c.Has(connections.Issues) && !c.Has(connections.Pages) {
			continue
		}
		u, err := url.Parse(c.BaseURL)
		if err != nil || u.Host == "" {
			continue
		}
		if h := strings.ToLower(u.Host); !slices.Contains(out, h) {
			out = append(out, h)
		}
	}
	return out
}

// hasOwnConnection says whether the person connected anything themselves.
func hasOwnConnection(cs *connections.Service) bool {
	list, err := cs.List()
	return err == nil && slices.ContainsFunc(list, func(c connections.Conn) bool { return !c.Env })
}

func connJSON(w http.ResponseWriter, status int, v any) { secretJSON(w, status, v) }

// connStatus is the status of a reason: never 401 or 403, which the page reads as its own session.
func connStatus(reason string) int {
	switch reason {
	case "unknown-product":
		return http.StatusUnprocessableEntity
	case "denied":
		return http.StatusFailedDependency
	case "not-found":
		return http.StatusNotFound
	case "unsupported", "bad-request":
		return http.StatusBadRequest
	case "read-only":
		return http.StatusConflict
	}
	return http.StatusBadGateway
}

func connReason(err error) string {
	if errors.Is(err, connections.ErrReadOnly) {
		return "read-only"
	}
	return connections.Reason(err)
}

// connRefusal answers an error as {error: reason}, with extra fields when given.
func connRefusal(w http.ResponseWriter, err error, extra map[string]any) {
	reason := connReason(err)
	body := map[string]any{"error": reason}
	for k, v := range extra {
		body[k] = v
	}
	connJSON(w, connStatus(reason), body)
}

// queryOf reads the issue and page filters of a request.
func queryOf(r *http.Request) connections.Query {
	v := r.URL.Query()
	on := func(k string) bool { s := v.Get(k); return s == "1" || s == "true" }
	limit, _ := strconv.Atoi(v.Get("limit"))
	return connections.Query{Text: strings.TrimSpace(v.Get("q")), Mine: on("mine"), Open: on("open"),
		Project: strings.TrimSpace(v.Get("project")), Native: strings.TrimSpace(v.Get("native")), Limit: limit}
}

func connectionRoutes(d deps) []gate.Route {
	return connectionRoutesOf(d, connectionsFor(d))
}

func connectionRoutesOf(d deps, cs *connections.Service) []gate.Route {
	base, cliBase := platformAPI+"/connections", "/api/cli/connections"
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
			connJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return false
		}
		return true
	}
	cliOK := func(w http.ResponseWriter, r *http.Request) bool {
		if d.CLI == nil || !d.CLI.OK(r) {
			connJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
			return false
		}
		if r.Method != http.MethodGet {
			connJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return false
		}
		return true
	}
	notAllowed := func(w http.ResponseWriter) {
		connJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
	}
	badBody := func(w http.ResponseWriter) {
		connJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
	}
	ctxOf := func(r *http.Request) (context.Context, context.CancelFunc) {
		return context.WithTimeout(r.Context(), connCallMax)
	}

	list := func(w http.ResponseWriter) {
		conns, err := cs.List()
		if err != nil {
			agent.Log("warn", "connections: the list is unreadable")
			connRefusal(w, err, nil)
			return
		}
		if conns == nil {
			conns = []connections.Conn{}
		}
		kinds := []connections.Kind{}
		for _, p := range connections.Providers() {
			kinds = append(kinds, p.Kind())
		}
		connJSON(w, http.StatusOK, map[string]any{"connections": conns, "kinds": kinds})
	}

	resolve := func(w http.ResponseWriter, r *http.Request) {
		c, t, ok := cs.Resolve(r.URL.Query().Get("url"))
		if !ok {
			connJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
			return
		}
		ctx, cancel := ctxOf(r)
		defer cancel()
		out := map[string]any{"connection": c, "target": t}
		var err error
		switch t.Kind {
		case "issue":
			var src connections.IssueSource
			if src, err = cs.Issues(c); err == nil {
				out["issue"], err = src.Issue(ctx, t.Key)
			}
		case "page":
			var src connections.PageSource
			if src, err = cs.Pages(c); err == nil {
				out["page"], err = src.Page(ctx, t.Key)
			}
		}
		if err != nil {
			agent.Log("info", "connections: resolve failed", agent.F{K: "id", V: c.ID}, agent.F{K: "kind", V: string(c.Kind)},
				agent.F{K: "reason", V: connReason(err)})
			connRefusal(w, err, map[string]any{"connection": c, "target": t})
			return
		}
		connJSON(w, http.StatusOK, out)
	}

	// read answers the reads of one connection: projects, issues, issue, mrs, pages, page.
	read := func(w http.ResponseWriter, r *http.Request, id, what string) {
		c, err := cs.Get(id)
		if err != nil {
			connRefusal(w, err, nil)
			return
		}
		ctx, cancel := ctxOf(r)
		defer cancel()
		var out any
		key := "projects"
		switch what {
		case "projects", "issues", "issue":
			src, err := cs.Issues(c)
			if err != nil {
				connRefusal(w, err, nil)
				return
			}
			switch what {
			case "projects":
				out, err = src.Projects(ctx)
			case "issues":
				key = "issues"
				out, err = src.Search(ctx, queryOf(r))
			default:
				key = "issue"
				k := strings.TrimSpace(r.URL.Query().Get("key"))
				if k == "" {
					badBody(w)
					return
				}
				out, err = src.Issue(ctx, k)
			}
			if err != nil {
				agent.Log("info", "connections: read failed", agent.F{K: "id", V: c.ID}, agent.F{K: "kind", V: string(c.Kind)},
					agent.F{K: "what", V: what}, agent.F{K: "reason", V: connReason(err)})
				connRefusal(w, err, nil)
				return
			}
		case "mrs":
			src, err := cs.Code(c)
			if err == nil {
				key = "mrs"
				qu := queryOf(r)
				qu.State = r.URL.Query().Get("state")
				out, err = src.MergeRequests(ctx, qu)
			}
			if err != nil {
				agent.Log("info", "connections: read failed", agent.F{K: "id", V: c.ID}, agent.F{K: "kind", V: string(c.Kind)},
					agent.F{K: "what", V: what}, agent.F{K: "reason", V: connReason(err)})
				connRefusal(w, err, nil)
				return
			}
		case "pages", "page":
			src, err := cs.Pages(c)
			if err != nil {
				connRefusal(w, err, nil)
				return
			}
			if what == "pages" {
				key = "pages"
				out, err = src.SearchPages(ctx, queryOf(r))
			} else {
				key = "page"
				pid := strings.TrimSpace(r.URL.Query().Get("id"))
				if pid == "" {
					badBody(w)
					return
				}
				out, err = src.Page(ctx, pid)
			}
			if err != nil {
				agent.Log("info", "connections: read failed", agent.F{K: "id", V: c.ID}, agent.F{K: "kind", V: string(c.Kind)},
					agent.F{K: "what", V: what}, agent.F{K: "reason", V: connReason(err)})
				connRefusal(w, err, nil)
				return
			}
		default:
			connJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
			return
		}
		switch v := out.(type) {
		case []connections.Project:
			if v == nil {
				out = []connections.Project{}
			}
		case []connections.Issue:
			if v == nil {
				out = []connections.Issue{}
			}
		case []connections.Page:
			if v == nil {
				out = []connections.Page{}
			}
		case []connections.MergeRequest:
			if v == nil {
				out = []connections.MergeRequest{}
			}
		}
		connJSON(w, http.StatusOK, map[string]any{key: out})
	}

	page := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r) {
			return
		}
		rest, sub := "", pathIs(r, base)
		if !sub {
			rest, _ = under(r, base)
		}
		switch {
		case sub:
			switch r.Method {
			case http.MethodGet:
				list(w)
			case http.MethodPost:
				var in connections.ConnectInput
				if !readSecretBody(r, &in) {
					badBody(w)
					return
				}
				ctx, cancel := ctxOf(r)
				defer cancel()
				c, err := cs.Connect(ctx, in)
				if err != nil {
					agent.Log("info", "connections: connect refused", agent.F{K: "kind", V: string(in.Detection.Kind)},
						agent.F{K: "reason", V: connReason(err)})
					connRefusal(w, err, nil)
					return
				}
				agent.Log("info", "connections: connected", agent.F{K: "id", V: c.ID}, agent.F{K: "kind", V: string(c.Kind)})
				connJSON(w, http.StatusOK, map[string]any{"connection": c})
			default:
				notAllowed(w)
			}
			return
		case rest == "detect":
			if r.Method != http.MethodPost {
				notAllowed(w)
				return
			}
			var b struct {
				URL string `json:"url"`
			}
			if !readSecretBody(r, &b) || strings.TrimSpace(b.URL) == "" {
				badBody(w)
				return
			}
			ctx, cancel := ctxOf(r)
			defer cancel()
			det, err := cs.Detect(ctx, b.URL)
			if err != nil {
				agent.Log("info", "connections: detect failed", agent.F{K: "reason", V: connReason(err)})
				connRefusal(w, err, nil)
				return
			}
			agent.Log("info", "connections: detected", agent.F{K: "kind", V: string(det.Kind)}, agent.F{K: "deployment", V: string(det.Deployment)})
			connJSON(w, http.StatusOK, map[string]any{"detection": det})
			return
		case rest == "resolve":
			if r.Method != http.MethodGet {
				notAllowed(w)
				return
			}
			resolve(w, r)
			return
		}
		id, what, _ := strings.Cut(rest, "/")
		if what == "" {
			switch r.Method {
			case http.MethodPatch:
				var b struct {
					Name    *string `json:"name"`
					Primary *bool   `json:"primary"`
				}
				if !readSecretBody(r, &b) || b.Primary != nil && !*b.Primary {
					badBody(w)
					return
				}
				c, err := cs.Get(id)
				if err == nil && b.Name != nil {
					c, err = cs.Rename(id, *b.Name)
				}
				if err == nil && b.Primary != nil {
					c, err = cs.SetPrimary(id)
				}
				if err != nil {
					connRefusal(w, err, nil)
					return
				}
				agent.Log("info", "connections: changed", agent.F{K: "id", V: id}, agent.F{K: "primary", V: c.Primary})
				connJSON(w, http.StatusOK, map[string]any{"connection": c})
			case http.MethodDelete:
				if err := cs.Remove(id); err != nil {
					connRefusal(w, err, nil)
					return
				}
				agent.Log("info", "connections: removed", agent.F{K: "id", V: id})
				w.Header().Set("Cache-Control", "no-store")
				w.WriteHeader(http.StatusNoContent)
			default:
				notAllowed(w)
			}
			return
		}
		if what == "check" {
			if r.Method != http.MethodPost {
				notAllowed(w)
				return
			}
			ctx, cancel := ctxOf(r)
			defer cancel()
			c, err := cs.Check(ctx, id)
			if err != nil {
				connRefusal(w, err, nil)
				return
			}
			agent.Log("info", "connections: checked", agent.F{K: "id", V: id}, agent.F{K: "state", V: c.State})
			connJSON(w, http.StatusOK, map[string]any{"connection": c})
			return
		}
		if r.Method != http.MethodGet {
			notAllowed(w)
			return
		}
		read(w, r, id, what)
	}

	cli := func(w http.ResponseWriter, r *http.Request) {
		if !cliOK(w, r) {
			return
		}
		if pathIs(r, cliBase) {
			list(w)
			return
		}
		rest, _ := under(r, cliBase)
		if rest == "resolve" {
			resolve(w, r)
			return
		}
		id, what, _ := strings.Cut(rest, "/")
		if what == "" || what == "check" {
			connJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
			return
		}
		read(w, r, id, what)
	}

	return []gate.Route{
		{Match: func(r *http.Request) bool { _, ok := under(r, base); return pathIs(r, base) || ok }, Handler: http.HandlerFunc(page)},
		{Match: func(r *http.Request) bool { _, ok := under(r, cliBase); return pathIs(r, cliBase) || ok }, Handler: http.HandlerFunc(cli)},
	}
}
