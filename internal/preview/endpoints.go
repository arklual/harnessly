package preview

import (
	"io"
	"net/http"

	"harnessly/internal/jscompat"
	"harnessly/internal/nodejson"
	"harnessly/internal/preview/devtools"
)

// The slot's own paths, answered by the runtime before any routing to a dev server: the bridge script, and the
// DevTools paths the page's bridge reads its conditions from and reports what it saw to.
const (
	BridgePath         = "/__tk/bridge.js"
	DevtoolsConfigPath = "/__tk/devtools/config"
	DevtoolsReportPath = "/__tk/devtools/report"
)

func isDevtoolsPath(p string) bool { return p == DevtoolsConfigPath || p == DevtoolsReportPath }

// bridgeConfig is what the bridge needs: the patterns, the conditions, and the target its slot addresses stand for.
type bridgeConfig struct {
	Blocked  []string          `json:"blocked"`
	Throttle devtools.Throttle `json:"throttle"`
	Target   *string           `json:"target"`
}

func bridgeConfigOf(s devtools.Settings, t *devtools.Target) bridgeConfig {
	c := bridgeConfig{Blocked: s.Blocked, Throttle: s.Throttle}
	if t != nil {
		o := t.TargetOrigin()
		c.Target = &o
	}
	return c
}

func answerJSON(w http.ResponseWriter, status int, body any) {
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Robots-Tag", "noindex, nofollow")
	w.WriteHeader(status)
	if body != nil {
		_, _ = w.Write(nodejson.Marshal(body))
	}
}

type errorBody struct {
	Error string `json:"error"`
}

// AnswerDevtools answers a slot's own DevTools paths for its current target.
func (p *Proxy) AnswerDevtools(w http.ResponseWriter, r *http.Request, path, slot string, target devtools.Target) {
	// The gateway lets the app's own origin drive a slot too; only the slot's page reports and reads this.
	if origins := r.Header.Values("Origin"); len(origins) > 0 && joinHeader(origins) != slot {
		answerJSON(w, 403, errorBody{"forbidden"})
		return
	}
	store := p.cfg.Store
	if path == DevtoolsConfigPath {
		if r.Method != http.MethodGet {
			answerJSON(w, 405, errorBody{"GET only"})
			return
		}
		t := target
		answerJSON(w, 200, bridgeConfigOf(store.Settings(slot), &t))
		return
	}
	if r.Method != http.MethodPost {
		answerJSON(w, 405, errorBody{"POST only"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, devtools.MaxReportBytes+1))
	if err != nil || len(body) > devtools.MaxReportBytes {
		_, _ = io.Copy(io.Discard, r.Body)
		answerJSON(w, 413, errorBody{"report too large"})
		return
	}
	value, ok := parseJSON(jscompat.DecodeUTF8(body))
	if !ok {
		answerJSON(w, 400, errorBody{"invalid report"})
		return
	}
	now := store.Now().UnixMilli()
	t := target
	report := devtools.ParseReport(value, slot, &t, now)
	if report == nil {
		answerJSON(w, 400, errorBody{"invalid report"})
		return
	}
	granted := store.TakeQuota(slot, len(report.Network)+len(report.Console), now)
	network := report.Network[:min(granted, len(report.Network))]
	logged := report.Console[:min(granted-len(network), len(report.Console))]
	for _, e := range network {
		store.RecordRequest(slot, e)
	}
	for _, e := range logged {
		store.RecordConsole(slot, e)
	}
	if len(network)+len(logged) < len(report.Network)+len(report.Console) {
		answerJSON(w, 429, nil)
		return
	}
	answerJSON(w, 204, nil)
}
