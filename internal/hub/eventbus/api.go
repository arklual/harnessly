package eventbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The API of the journal, under <platform api>/events (docs/hub/event-bus.md, «API» and «Уровни»).
//
// A request names its level with ?scope=:
//
//	me             the person's own events (the default)
//	space:<id>     and the team events of a space they are a member of
//	org:<id>       and the team events of every space of an organization they administer
//	all            every event of the installation, the deployment's own too: a hub admin only
//
// A person's own events are in every level; another person's correspondence (VisPerson) is in none. The same levels
// serve a session (the hub's page) and a personal token with the scope «events» (an app through the SDK): a token
// reads exactly what its person may.

var (
	errNoSub   = httpx.New(http.StatusNotFound, "not_found", "Подписка не найдена")
	errScope   = httpx.New(http.StatusForbidden, "scope", "Этот уровень журнала вам не открыт")
	errTokenSc = httpx.New(http.StatusForbidden, "scope", "Токену не разрешено читать журнал: подключите приложение с доступом к событиям")
)

// streamPoll is how often a live stream looks at the journal: a write on another replica reaches it within this.
const streamPoll = time.Second

// streamFor bounds one live connection; the page reconnects with Last-Event-ID and loses nothing.
const streamFor = 30 * time.Minute

// Mount adds the routes. prefix is the platform API prefix ("/api/platform").
func (b *Bus) Mount(mux *http.ServeMux, prefix string, a *auth.Service) {
	p := strings.TrimRight(prefix, "/") + "/events"
	guard := func(fn func(w http.ResponseWriter, r *http.Request, pr *auth.Principal) error) http.Handler {
		return httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
			pr, err := a.Authenticate(r)
			if err != nil {
				return err
			}
			if pr == nil {
				// An app through the SDK: a personal token with the scope «events».
				if pr, err = a.TokenPrincipal(r); err != nil {
					return err
				}
				if pr == nil {
					return httpx.ErrUnauthenticated
				}
				if !pr.Can(auth.ScopeEvents) {
					return errTokenSc
				}
			}
			return fn(w, r, pr)
		})
	}
	mux.Handle("GET "+p, guard(b.listEvents))
	mux.Handle("GET "+p+"/stream", guard(b.stream))
	mux.Handle("GET "+p+"/scopes", guard(b.scopes))
	mux.Handle("GET "+p+"/triggers", guard(b.listTriggers))
	mux.Handle("PUT "+p+"/triggers", guard(b.putTrigger))
	mux.Handle("GET "+p+"/subscriptions", guard(b.listSubs))
	mux.Handle("POST "+p+"/subscriptions/{id}/replay", guard(b.replay))
	mux.Handle("POST "+p+"/subscriptions/{id}/pause", guard(b.pause(true)))
	mux.Handle("POST "+p+"/subscriptions/{id}/resume", guard(b.pause(false)))
}

// Level is one level of the journal a person may read.
type Level struct {
	Scope string `json:"scope"` // what ?scope= takes
	Kind  string `json:"kind"`  // me | space | org | all
	Name  string `json:"name"`
}

// levels lists what the person may read: their own, each space they are in, each organization they administer, and
// the whole installation for a hub admin.
func (b *Bus) levels(ctx context.Context, pr *auth.Principal) ([]Level, error) {
	st := b.db.S()
	out := []Level{{Scope: "me", Kind: "me", Name: pr.User.Name}}
	spaces, err := st.SpacesByUser(ctx, pr.User.ID)
	if err != nil {
		return nil, err
	}
	for _, sp := range spaces {
		out = append(out, Level{Scope: "space:" + sp.ID, Kind: "space", Name: sp.Name})
	}
	orgs, err := b.adminOrgs(ctx, pr)
	if err != nil {
		return nil, err
	}
	for _, o := range orgs {
		out = append(out, Level{Scope: "org:" + o.ID, Kind: "org", Name: o.Name})
	}
	if pr.Admin {
		out = append(out, Level{Scope: "all", Kind: "all"})
	}
	return out, nil
}

// adminOrgs are the organizations the person administers (all of them for a hub admin).
func (b *Bus) adminOrgs(ctx context.Context, pr *auth.Principal) ([]store.Org, error) {
	st := b.db.S()
	if pr.Admin {
		return st.AllOrgs(ctx)
	}
	grants, err := st.GrantsOfUser(ctx, pr.User.ID)
	if err != nil {
		return nil, err
	}
	var out []store.Org
	for _, g := range grants {
		if g.ScopeKind != store.ScopeOrg || g.Role != store.RoleAdmin {
			continue
		}
		o, err := st.OrgByID(ctx, g.ScopeID)
		if err != nil {
			if store.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}

// scopeQuery turns ?scope= into what the person may read, or refuses it.
func (b *Bus) scopeQuery(ctx context.Context, pr *auth.Principal, scope string) (Query, error) {
	q := Query{Owner: pr.User.ID}
	kind, id, _ := strings.Cut(scope, ":")
	switch kind {
	case "", "me":
		return q, nil
	case "all":
		if !pr.Admin {
			return q, errScope
		}
		q.All = true
		return q, nil
	case "deployment": // the shared /hook's events (owner ""): a hub admin only
		if !pr.Admin {
			return q, errScope
		}
		q.Owner = ""
		return q, nil
	case "space":
		role, err := b.db.S().Role(ctx, id, pr.User.ID)
		if err != nil {
			return q, err
		}
		if role == "" && !pr.Admin {
			return q, errScope
		}
		q.Spaces = []string{id}
		return q, nil
	case "org":
		orgs, err := b.adminOrgs(ctx, pr)
		if err != nil {
			return q, err
		}
		for _, o := range orgs {
			if o.ID == id {
				q.Org = id
				return q, nil
			}
		}
		return q, errScope
	}
	return q, httpx.New(http.StatusBadRequest, "invalid_request", "scope: me, space:<id>, org:<id> или all")
}

// filterOf reads ?type= and ?source= (comma lists; "chat.*" takes a domain).
func filterOf(r *http.Request) (Filter, error) {
	f := Filter{}
	if t := r.URL.Query().Get("type"); t != "" {
		f.Types = strings.Split(t, ",")
	}
	if s := r.URL.Query().Get("source"); s != "" {
		f.Sources = strings.Split(s, ",")
	}
	if err := f.valid(); err != nil {
		return f, httpx.New(http.StatusBadRequest, "invalid_request", err.Error())
	}
	return f, nil
}

func (b *Bus) scopes(w http.ResponseWriter, r *http.Request, pr *auth.Principal) error {
	ls, err := b.levels(r.Context(), pr)
	if err != nil {
		return err
	}
	head, err := b.Head(r.Context())
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"scopes": ls, "head": head})
	return nil
}

// listEvents is GET /events?scope=&after=&before=&type=&source=&limit=: a page of the journal. With before (and no
// after) it reads backwards — the newest page first, for a page that opens on the present and scrolls into the past.
func (b *Bus) listEvents(w http.ResponseWriter, r *http.Request, pr *auth.Principal) error {
	q, err := b.scopeQuery(r.Context(), pr, r.URL.Query().Get("scope"))
	if err != nil {
		return err
	}
	if q.Filter, err = filterOf(r); err != nil {
		return err
	}
	v := r.URL.Query()
	q.After, _ = strconv.ParseInt(v.Get("after"), 10, 64)
	q.Before, _ = strconv.ParseInt(v.Get("before"), 10, 64)
	q.Limit, _ = strconv.Atoi(v.Get("limit"))
	if v.Get("after") == "" && v.Get("before") == "" && v.Get("newest") == "1" {
		q.Before = -1
	}
	events, err := b.List(r.Context(), q)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"events": events})
	return nil
}

// stream is GET /events/stream?scope=&type=&source=: the journal live, as server-sent events. Each message is one
// event, its id the seq: a reconnect with Last-Event-ID (or ?after=) goes on from there and loses nothing. Without
// either it starts at the present. A comment every 15 s keeps proxies from closing a quiet stream.
func (b *Bus) stream(w http.ResponseWriter, r *http.Request, pr *auth.Principal) error {
	ctx := r.Context()
	q, err := b.scopeQuery(ctx, pr, r.URL.Query().Get("scope"))
	if err != nil {
		return err
	}
	if q.Filter, err = filterOf(r); err != nil {
		return err
	}
	from := r.Header.Get("Last-Event-ID")
	if from == "" {
		from = r.URL.Query().Get("after")
	}
	if n, err := strconv.ParseInt(strings.TrimSpace(from), 10, 64); err == nil {
		q.After = n
	} else if q.After, err = b.Head(ctx); err != nil {
		return err
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		return httpx.New(http.StatusInternalServerError, "no_stream", "Поток не поддерживается")
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "retry: 2000\n: from %d\n\n", q.After)
	fl.Flush()

	end := time.NewTimer(streamFor)
	defer end.Stop()
	tick := time.NewTicker(streamPoll)
	defer tick.Stop()
	quiet := time.Now()
	for {
		q.Limit = 200
		events, err := b.List(ctx, q)
		if err != nil {
			return nil // the connection is gone or the database is: the page reconnects
		}
		for _, e := range events {
			data, _ := json.Marshal(e)
			fmt.Fprintf(w, "id: %d\nevent: bus\ndata: %s\n\n", e.Seq, data)
		}
		if n := len(events); n > 0 {
			q.After = events[n-1].Seq
			fl.Flush()
			quiet = time.Now()
			if n == q.Limit {
				continue // a backlog: read on without waiting
			}
		} else if time.Since(quiet) > 15*time.Second {
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
			quiet = time.Now()
		}
		select {
		case <-ctx.Done():
			return nil
		case <-end.C:
			return nil
		case <-tick.C:
		}
	}
}

func (b *Bus) listSubs(w http.ResponseWriter, r *http.Request, pr *auth.Principal) error {
	who, err := subOwner(r, pr)
	if err != nil {
		return err
	}
	subs, err := b.Subscriptions(r.Context(), who)
	if err != nil {
		return err
	}
	head, err := b.Head(r.Context())
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"subscriptions": subs, "head": head})
	return nil
}

// subOwner is whose subscriptions a request manages: the person's, or the hub's own (?owner=hub) for a hub admin.
func subOwner(r *http.Request, pr *auth.Principal) (string, error) {
	if r.URL.Query().Get("owner") == "hub" {
		if !pr.Admin || pr.Token != nil {
			return "", errScope
		}
		return "", nil
	}
	return pr.User.ID, nil
}

// replay is POST /events/subscriptions/{id}/replay {after}: the subscription reads again after that seq.
func (b *Bus) replay(w http.ResponseWriter, r *http.Request, pr *auth.Principal) error {
	who, err := subOwner(r, pr)
	if err != nil {
		return err
	}
	var body struct {
		After int64 `json:"after"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		return err
	}
	if err := b.Rewind(r.Context(), who, r.PathValue("id"), body.After); err != nil {
		if errors.Is(err, ErrNoSubscription) {
			return errNoSub
		}
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (b *Bus) pause(paused bool) func(w http.ResponseWriter, r *http.Request, pr *auth.Principal) error {
	return func(w http.ResponseWriter, r *http.Request, pr *auth.Principal) error {
		who, err := subOwner(r, pr)
		if err != nil {
			return err
		}
		if err := b.SetPaused(r.Context(), who, r.PathValue("id"), paused); err != nil {
			if errors.Is(err, ErrNoSubscription) {
				return errNoSub
			}
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
}
