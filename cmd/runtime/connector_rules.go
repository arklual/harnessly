package main

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/asks"
	"github.com/ly-solutions/harnessly/internal/connectors"
	"github.com/ly-solutions/harnessly/internal/connectors/rules"
	"github.com/ly-solutions/harnessly/internal/coreapi/layoutapi"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// The default rules of the connectors (internal/connectors/rules, docs/connectors.md «Default rules»): one engine per
// runtime, a destination of the connectors' events next to the hub and the inbox. Its chats start through the core,
// which is known only once the routes are made: rulesCore is set then.
var (
	rulesMu   sync.Mutex
	rulesAll  = map[string]*rules.Engine{}
	rulesCore atomic.Pointer[core]
)

var errRulesNoCore = errors.New("the runtime is starting")

func rulesFor(d deps, svc *connectors.Service) *rules.Engine {
	rulesMu.Lock()
	defer rulesMu.Unlock()
	if e := rulesAll[d.DataDir]; e != nil {
		return e
	}
	e := rules.New(rules.Deps{
		Dir:   filepath.Join(d.DataDir, "connectors"),
		Conns: func() ([]connectors.Conn, error) { return svc.Views(context.Background()) },
		Start: func(_ context.Context, prompt, name string) (string, error) {
			c := rulesCore.Load()
			if c == nil || c.agents == nil {
				return "", errRulesNoCore
			}
			res := c.agents.Start(layoutapi.StartRequest{Provider: "claude", Prompt: prompt, TabName: name})
			if res.ConversationID == "" {
				return "", errors.New(strings.TrimSpace("start refused " + string(res.Body)))
			}
			return res.ConversationID, nil
		},
		Push: func(_ context.Context, text string) error {
			list, err := svc.Views(context.Background())
			if err != nil {
				return err
			}
			for _, c := range list {
				if c.Connector == "telegram-bot" && c.State == connectors.StateOK && c.Next == nil {
					p, err := svc.Propose(c.ID, "notify", map[string]string{"text": text}, "default rule", connectors.By{})
					if err != nil {
						return err
					}
					if p.State != connectors.Done {
						return errors.New(p.Result)
					}
					return nil
				}
			}
			return errors.New("no bound Telegram bot")
		},
		Quiet: func(t time.Time) bool {
			set, err := asks.OpenSettings(d.DataDir).Get()
			return err == nil && set.Quiet.InQuiet(t)
		},
		Russian: func() bool { return locale.Pick("en", "ru") == "ru" },
	})
	go e.Run(context.Background())
	rulesAll[d.DataDir] = e
	return e
}

// connectorRulesRoute answers <platform>/connectors/<id>/rules: GET the templates of the connection with the person's
// switches, PUT {rule, on?, keywords?} one switch. It is the page's (pageOK checked by the caller).
func connectorRulesRoute(w http.ResponseWriter, r *http.Request, svc *connectors.Service, e *rules.Engine, id string,
	read func(http.ResponseWriter, *http.Request, any) bool) {
	conn, err := svc.Get(id)
	if err != nil {
		connectorRefusal(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		var b struct {
			Rule     string    `json:"rule"`
			On       *bool     `json:"on"`
			Keywords *[]string `json:"keywords"`
		}
		if !read(w, r, &b) {
			return
		}
		var words []string
		if b.Keywords != nil {
			words = append([]string{}, *b.Keywords...)
		}
		if _, err := e.Set(conn, b.Rule, b.On, words); err != nil {
			connectorRefusal(w, err)
			return
		}
	default:
		secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	views, err := e.Views(conn)
	if err != nil {
		agent.Log("warn", "connector rules not read", agent.F{K: "error", V: err.Error()})
		connectorRefusal(w, err)
		return
	}
	secretJSON(w, http.StatusOK, map[string]any{"rules": views})
}
