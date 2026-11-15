package realtime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Guard lets a request through or answers it itself.
type Guard func(w http.ResponseWriter, r *http.Request) bool

const bodyMax = 64 << 10

// Handler serves the voice routes under base (e.g. /api/platform/voice/live).
//
//	GET    <base>                       State
//	PUT    <base>/settings              {subscription, voice, model, talk, head, noBarge, wake?} -> State
//	PUT    <base>/settings/wake         {on, silence} -> State (only the wake field; the others stay as saved)
//	GET    <base>/wake/model            the wake phrase's speech model, tar.gz (fetched once, kept on the machine)
//	POST   <base>/calls                 {sdp, mode, voice, model, context} -> {id, sdp, route, model}
//	GET    <base>/calls/<id>/events     text/event-stream of Event, from Last-Event-ID or ?since=
//	GET    <base>/calls/<id>/audio      WebSocket of the call's sound over the machine (relay.go)
//	POST   <base>/calls/<id>/say        {text, task?, speak?, typed?}
//	DELETE <base>/calls/<id>
func (s *Service) Handler(base string, guard Guard) http.Handler {
	base = strings.TrimRight(base, "/")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest, ok := strings.CutPrefix(r.URL.Path, base)
		if !ok || rest != "" && !strings.HasPrefix(rest, "/") {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": CodeNoCall})
			return
		}
		if !guard(w, r) {
			return
		}
		p := strings.Split(strings.Trim(rest, "/"), "/")
		m := r.Method
		switch {
		case len(p) == 1 && p[0] == "" && m == http.MethodGet:
			writeJSON(w, http.StatusOK, s.State())
		case len(p) == 1 && p[0] == "settings" && m == http.MethodPut:
			var in Settings
			if !readJSON(w, r, &in) {
				return
			}
			if in.Wake == nil {
				in.Wake = s.Settings().Wake
			}
			if err := s.SaveSettings(in); err != nil {
				answerErr(w, err)
				return
			}
			writeJSON(w, http.StatusOK, s.State())
		case len(p) == 2 && p[0] == "settings" && p[1] == "wake" && m == http.MethodPut:
			var in Wake
			if !readJSON(w, r, &in) {
				return
			}
			if err := s.SaveWake(in); err != nil {
				answerErr(w, err)
				return
			}
			writeJSON(w, http.StatusOK, s.State())
		case len(p) == 2 && p[0] == "wake" && p[1] == "model" && (m == http.MethodGet || m == http.MethodHead):
			s.serveWakeModel(w, r)
		case len(p) == 1 && p[0] == "calls" && m == http.MethodPost:
			var in CallInput
			if !readJSON(w, r, &in) {
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			defer cancel()
			st, err := s.Start(ctx, in)
			if err != nil {
				answerErr(w, err)
				return
			}
			writeJSON(w, http.StatusOK, st)
		case len(p) == 2 && p[0] == "calls" && m == http.MethodDelete:
			s.Hang(p[1])
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		case len(p) == 3 && p[0] == "calls" && p[2] == "say" && m == http.MethodPost:
			c, err := s.Call(p[1])
			if err != nil {
				answerErr(w, err)
				return
			}
			var in struct {
				Text  string `json:"text"`
				Task  string `json:"task"`
				Speak *bool  `json:"speak"`
				// Typed: the person's own words, typed in place of said.
				Typed bool `json:"typed"`
			}
			if !readJSON(w, r, &in) {
				return
			}
			say := func() error { return c.Say(in.Text, in.Task, in.Speak == nil || *in.Speak) }
			if in.Typed {
				say = func() error { return c.Type(in.Text) }
			}
			if err := say(); err != nil {
				answerErr(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		case len(p) == 3 && p[0] == "calls" && p[2] == "audio" && m == http.MethodGet:
			c, err := s.Call(p[1])
			if err != nil {
				answerErr(w, err)
				return
			}
			serveAudio(w, r, c)
		case len(p) == 3 && p[0] == "calls" && p[2] == "events" && m == http.MethodGet:
			c, err := s.Call(p[1])
			if err != nil {
				answerErr(w, err)
				return
			}
			since, _ := strconv.Atoi(r.Header.Get("Last-Event-ID"))
			if q, err := strconv.Atoi(r.URL.Query().Get("since")); err == nil && q > since {
				since = q
			}
			serveEvents(w, r, c, since)
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": CodeNoCall})
		}
	})
}

func serveEvents(w http.ResponseWriter, r *http.Request, c *Call, since int) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	flush := func() {
		if flusher != nil {
			flusher.Flush()
		}
	}
	flush()
	for {
		evs, wake, done := c.Events(since)
		for _, e := range evs {
			raw, _ := json.Marshal(e)
			if _, err := io.WriteString(w, "id: "+strconv.Itoa(e.Seq)+"\nevent: "+e.Type+"\ndata: "+string(raw)+"\n\n"); err != nil {
				return
			}
			since = e.Seq
		}
		flush()
		if done {
			return
		}
		select {
		case <-wake:
		case <-r.Context().Done():
			return
		case <-time.After(15 * time.Second):
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			flush()
		}
	}
}

func readJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, bodyMax))
	if err := dec.Decode(out); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": CodeBadRequest})
		return false
	}
	return true
}

func answerErr(w http.ResponseWriter, err error) {
	if e, ok := ErrorOf(err); ok {
		writeJSON(w, e.Status, map[string]string{"error": e.Code, "detail": e.Detail})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": CodeUnreached})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
