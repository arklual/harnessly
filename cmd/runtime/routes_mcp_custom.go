package main

// The person's own services: the page's form that makes one, and the card an agent's connection_add asks
// the person on. The card is the connectors' card of the chat (<platform>/connector-actions): the same list, the same
// yes and no, so a new kind of "action" needs no new screen.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/connectors"
	"github.com/ly-solutions/harnessly/internal/mcpconn"
)

type hubRequestKey struct{}

// withHubCaller keeps the request of a hub call in its context: whose chat it is is found by its connection.
func withHubCaller(ctx context.Context, r *http.Request) context.Context {
	return context.WithValue(ctx, hubRequestKey{}, r)
}

// ownServiceConfirmer is how the hub asks the person: a proposal on the card of the chat whose agent called.
func ownServiceConfirmer(d deps, c *core) mcpconn.Confirmer {
	callers := &callerCache{panes: c.cfg.Tmux, convOf: func(session string) string {
		id, _ := convOf(c, session)
		return id
	}}
	return proposeConfirmer(connectorsFor(d), func(ctx context.Context) connectors.By {
		if r, _ := ctx.Value(hubRequestKey{}).(*http.Request); r != nil {
			if conv, _ := callers.of(r); conv != "" {
				return connectors.By{Conv: conv, Title: secretIdentity(c, conv).Title}
			}
		}
		return connectors.By{}
	})
}

// proposeConfirmer shows a hub request as a proposal on the card of the chat byOf names, and waits on it.
func proposeConfirmer(svc *connectors.Service, byOf func(context.Context) connectors.By) mcpconn.Confirmer {
	return func(ctx context.Context, req mcpconn.ConfirmRequest, wait time.Duration) (mcpconn.ConfirmAnswer, error) {
		id := req.ProposalID
		if id == "" {
			by := byOf(ctx)
			if by.Conv == "" {
				return mcpconn.ConfirmAnswer{}, errors.New("the machine cannot tell which chat this is, so the card has nowhere to stand: the person connects it in Settings → Connections")
			}
			pairs := make([]connectors.Pair, 0, len(req.Pairs))
			for _, p := range req.Pairs {
				pairs = append(pairs, connectors.Pair{Name: p.Name, Value: p.Value})
			}
			p := svc.ProposeRun(req.Name, connectors.T(req.Title, req.TitleRu), pairs, req.Why, by, req.Run)
			agent.Log("info", "mcp: an agent proposed its person's own service", agent.F{K: "proposal", V: p.ID}, agent.F{K: "conv", V: by.Conv})
			id = p.ID
		}
		p, err := svc.Wait(ctx, id, wait)
		if err != nil {
			return mcpconn.ConfirmAnswer{}, err
		}
		return mcpconn.ConfirmAnswer{ID: p.ID, State: p.State, Result: p.Result}, nil
	}
}

// mcpCustomRoute: POST <platform>/mcp/custom {input, values} makes (or, with input.id of an existing one, changes) a
// service of the person's own from the page's form. The person is the one typing, so there is no card; the keys they
// typed go to the safe and nowhere else.
func mcpCustomRoute(w http.ResponseWriter, r *http.Request, ms *mcpconn.Service, ctxOf func(*http.Request) (context.Context, context.CancelFunc)) {
	if r.Method != http.MethodPost {
		connJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	var b struct {
		Input  mcpconn.CustomInput `json:"input"`
		Values map[string]string   `json:"values"`
	}
	if !readSecretBody(r, &b) {
		connJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
		return
	}
	ctx, cancel := ctxOf(r)
	defer cancel()
	srv, start, err := ms.SaveCustom(ctx, b.Input, b.Values)
	if err != nil {
		agent.Log("info", "mcp: own service refused", agent.F{K: "reason", V: mcpconn.Reason(err)})
		extra := map[string]any{}
		if errors.Is(err, mcpconn.ErrBadRequest) {
			extra["message"] = strings.TrimPrefix(err.Error(), mcpconn.ErrBadRequest.Error()+": ")
		}
		mcpRefusal(w, err, extra)
		return
	}
	agent.Log("info", "mcp: own service made", agent.F{K: "id", V: srv.ID}, agent.F{K: "kind", V: b.Input.Kind}, agent.F{K: "state", V: string(srv.State)})
	connJSON(w, http.StatusOK, map[string]any{"server": srv, "authStart": start})
}
