package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/bgmodel"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
	"github.com/ly-solutions/harnessly/internal/sidewidget"
)

// The smart widgets of the chat's right column: a widget's manifest names a request, the page sends it with
// the data the widget shows (never a secret, a chat or the page), and a model answers in a few short lines. The page asks
// no more often than the widget's interval (5 minutes at least) and at once on the person's press; the machine holds its
// own floor on top, per widget and for the hour, so a page gone wrong cannot spend the person's subscription.
//
//	POST <platform>/side-widgets/smart  {key: "<widget>", prompt, data?}  -> {text, at}
//	  400 bad-request, 401, 409 {error: "no-model", message}, 429 {error: "too-often", retryAt}, 502 {error, message}

const (
	sideSmartBodyMax = 16 << 10
	// sideSmartFloor: one widget is answered at most this often, whatever the page asks.
	sideSmartFloor = 60 * time.Second
	// sideSmartHour: the machine answers the smart widgets at most this many times an hour, together.
	sideSmartHour = 60
)

type sideSmartGuard struct {
	mu   sync.Mutex
	last map[string]time.Time
	hour []time.Time
}

// take says whether a call for key may go now; when not, when it may.
func (g *sideSmartGuard) take(key string, now time.Time) (bool, time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.last == nil {
		g.last = map[string]time.Time{}
	}
	if at, ok := g.last[key]; ok && now.Sub(at) < sideSmartFloor {
		return false, at.Add(sideSmartFloor)
	}
	kept := g.hour[:0]
	for _, at := range g.hour {
		if now.Sub(at) < time.Hour {
			kept = append(kept, at)
		}
	}
	g.hour = kept
	if len(g.hour) >= sideSmartHour {
		return false, g.hour[0].Add(time.Hour)
	}
	g.last[key] = now
	g.hour = append(g.hour, now)
	return true, time.Time{}
}

func sideWidgetRoutes(d deps) []gate.Route {
	runner := bgRunnerFor(d)
	guard := &sideSmartGuard{}
	path := platformAPI + "/side-widgets/smart"
	handle := func(w http.ResponseWriter, r *http.Request) {
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
			Key    string `json:"key"`
			Prompt string `json:"prompt"`
			Data   string `json:"data"`
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, sideSmartBodyMax))
		if err != nil || json.Unmarshal(raw, &b) != nil || strings.TrimSpace(b.Prompt) == "" ||
			b.Key == "" || len(b.Key) > 200 {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		if !runner.Usable(bgmodel.FeatureWidget) {
			secretJSON(w, http.StatusConflict, map[string]string{"error": "no-model",
				"message": locale.Pick("No model can answer the smart widgets.", "Умным виджетам некому ответить: нет модели.")})
			return
		}
		now := time.Now()
		if ok, at := guard.take(b.Key, now); !ok {
			secretJSON(w, http.StatusTooManyRequests, map[string]any{"error": "too-often", "retryAt": at.UnixMilli()})
			return
		}
		text, _, err := runner.Widget(r.Context(), b.Prompt, b.Data)
		if err != nil {
			message := locale.Pick("The model did not answer.", "Модель не ответила.")
			if last, ok := runner.Last()[bgmodel.FeatureWidget]; ok && last.Message != "" {
				message = last.Message
			}
			secretJSON(w, http.StatusBadGateway, map[string]string{"error": bgmodel.ReasonOf(err), "message": message})
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"text": text, "at": now.UnixMilli()})
	}
	return []gate.Route{{Match: func(r *http.Request) bool { return pathIs(r, path) }, Handler: http.HandlerFunc(handle)}}
}

// The widgets an agent makes (internal/sidewidget): one service per data folder, for the CLI's route and the MCP tools.
var (
	sideWidgetsMu  sync.Mutex
	sideWidgetsFor = map[string]*sidewidget.Service{}
)

func sideWidgetService(d deps) *sidewidget.Service {
	sideWidgetsMu.Lock()
	defer sideWidgetsMu.Unlock()
	if s, ok := sideWidgetsFor[d.DataDir]; ok {
		return s
	}
	s := &sidewidget.Service{Reg: apps.Open(d.DataDir), Root: filepath.Join(d.DataDir, "side-widgets"),
		Host: env("AGENT_VERSION", agent.BuildVersion)}
	sideWidgetsFor[d.DataDir] = s
	return s
}

// sideWidgetCLIRoutes are `harnessly widget create|update|list|remove`:
//
//	GET  /api/cli/apps/side-widgets                          -> {widgets: [Info]}
//	POST /api/cli/apps/side-widgets {op: "create", spec}     -> {message}
//	POST /api/cli/apps/side-widgets {op: "update", name, patch}
//	POST /api/cli/apps/side-widgets {op: "remove", name}
//
// Under /api/cli/apps/, so the agent of a chat with apps reaches it too: it makes only a sandboxed view of the side
// target with no permission (`local.widget-<name>`), nothing else of the apps.
func sideWidgetCLIRoutes(d deps) []gate.Route {
	path := "/api/cli/apps/side-widgets"
	handle := func(w http.ResponseWriter, r *http.Request) {
		if d.CLI == nil || !d.CLI.OK(r) {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
			return
		}
		s := sideWidgetService(d)
		if r.Method == http.MethodGet {
			list, err := s.List()
			if err != nil {
				secretJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed", "message": err.Error()})
				return
			}
			secretJSON(w, http.StatusOK, map[string]any{"widgets": list})
			return
		}
		if r.Method != http.MethodPost {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		var b struct {
			Op    string           `json:"op"`
			Name  string           `json:"name"`
			Spec  sidewidget.Spec  `json:"spec"`
			Patch sidewidget.Patch `json:"patch"`
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil || json.Unmarshal(raw, &b) != nil {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		refuse := func(err error) {
			status := http.StatusUnprocessableEntity
			switch {
			case errors.Is(err, sidewidget.ErrNotFound):
				status = http.StatusNotFound
			case errors.Is(err, sidewidget.ErrExists):
				status = http.StatusConflict
			}
			secretJSON(w, status, map[string]string{"error": "refused", "message": err.Error()})
		}
		switch b.Op {
		case "create":
			if _, err := s.Create(b.Spec); err != nil {
				refuse(err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]string{"message": sidewidget.Created(b.Spec.Name, b.Spec.Title)})
		case "update":
			if _, err := s.Update(b.Name, b.Patch); err != nil {
				refuse(err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]string{"message": "widget " + b.Name + " changed"})
		case "remove":
			if err := s.Remove(b.Name); err != nil {
				refuse(err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]string{"message": "widget " + b.Name + " removed"})
		default:
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
		}
	}
	return []gate.Route{{Match: func(r *http.Request) bool { return pathIs(r, path) }, Handler: http.HandlerFunc(handle)}}
}
