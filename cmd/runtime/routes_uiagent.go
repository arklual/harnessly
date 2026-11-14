package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/bgmodel"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
	"github.com/ly-solutions/harnessly/internal/settingsctl"
)

// The agent of the window: a request in words from the field of Settings, answered at once by a quick model
// with no chat and no terminal. The page sends the window's map (features/settings/agent-map: lines «- <name>: /<path>»)
// and, if it likes, more places of its own; the machine adds its catalog of settings. The answer is a stream of
// server-sent events:
//
//	POST <platform>/ui-agent   {query, page?, lang?: "en"|"ru", map?: "<the map>", places?: [{id, title, words?}]}
//	                           Accept: text/event-stream; a place of the map has its path for its id
//	event: action  data: {"kind": "go", "place": "<id: a path of the map>", "label"}    (at most one, first)
//	               data: {"kind": "confirm", "key", "value", "label", "title", "was"}  a change the person confirms;
//	                     the page makes it with PUT <platform>/settings-catalog/{key} {value}
//	event: delta   data: {"text": "<a piece of the answer>"}
//	event: done    data: {"text": "<the whole answer>"}
//	event: error   data: {"error": <bgmodel reason>, "message"}
//
// Before the stream: 400 bad-request, 401, 409 {error: "no-model", message} when no model can answer (none picked for
// «Window assistant» and no account with a key). Nothing is kept; no log line carries the request.

const uiAgentBodyMax = 64 << 10

func uiAgentRoutes(d deps) []gate.Route {
	runner := bgRunnerFor(d)
	base := platformAPI + "/ui-agent"
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
			Query  string            `json:"query"`
			Page   string            `json:"page"`
			Map    string            `json:"map"`
			Lang   string            `json:"lang"`
			Places []bgmodel.UIPlace `json:"places"`
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, uiAgentBodyMax))
		if err != nil || json.Unmarshal(raw, &b) != nil || strings.TrimSpace(b.Query) == "" {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		places := append(bgmodel.PlacesOfMap(b.Map), b.Places...)
		if !runner.Usable(bgmodel.FeatureUIAgent) {
			secretJSON(w, http.StatusConflict, map[string]string{"error": "no-model",
				"message": locale.Pick("No model is picked for the window assistant.", "Для помощника окна не выбрана модель.")})
			return
		}
		catalog := uiAgentSettings(settingsCtlOf(d.DataDir))
		titles := map[string][2]string{}
		for _, s := range catalog {
			titles[s.Key] = [2]string{s.Title, s.ValueLabel}
		}
		flusher, _ := w.(http.Flusher)
		started := false
		event := func(name string, v any) {
			if !started {
				h := w.Header()
				h.Set("Content-Type", "text/event-stream")
				h.Set("Cache-Control", "no-store")
				h.Set("X-Accel-Buffering", "no")
				w.WriteHeader(http.StatusOK)
				started = true
			}
			data, _ := json.Marshal(v)
			_, _ = io.WriteString(w, "event: "+name+"\ndata: "+string(data)+"\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
		onAction := func(a bgmodel.UIAction) {
			if a.Kind == bgmodel.UIActGo {
				event("action", map[string]string{"kind": "go", "place": a.Place, "label": a.Label})
				return
			}
			t := titles[a.Key]
			event("action", map[string]string{"kind": "confirm", "key": a.Key, "value": a.Value, "label": a.Label,
				"title": t[0], "was": t[1]})
		}
		text, _, _, err := runner.UIAgentStream(r.Context(), b.Query, b.Page, b.Lang, places, catalog, onAction, func(piece string) {
			event("delta", map[string]string{"text": piece})
		})
		if err != nil {
			message := locale.Pick("The model did not answer.", "Модель не ответила.")
			if last, ok := runner.Last()[bgmodel.FeatureUIAgent]; ok && last.Message != "" {
				message = last.Message
			}
			event("error", map[string]string{"error": bgmodel.ReasonOf(err), "message": message})
			return
		}
		event("done", map[string]string{"text": text})
	}
	return []gate.Route{{Match: func(r *http.Request) bool { return pathIs(r, base) }, Handler: http.HandlerFunc(handle)}}
}

// uiAgentSettings is the catalog as the agent is shown it; nil before the routes made one.
func uiAgentSettings(svc *settingsctl.Service) []bgmodel.UISetting {
	if svc == nil {
		return nil
	}
	list := svc.List()
	out := make([]bgmodel.UISetting, 0, len(list))
	for _, s := range list {
		opts := make([]bgmodel.UIOption, 0, len(s.Options))
		for _, o := range s.Options {
			opts = append(opts, bgmodel.UIOption{Value: o.Value, Label: o.Label})
		}
		out = append(out, bgmodel.UISetting{Key: s.Key, Section: s.Section, Title: s.Title, Kind: string(s.Kind),
			Options: opts, Min: s.Min, Max: s.Max, Value: s.Value, ValueLabel: s.ValueLabel})
	}
	return out
}
