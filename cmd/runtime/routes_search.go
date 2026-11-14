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
)

// The search's answer to a question in words (⌘K). The page finds the sources itself (chats, documents, designs,
// settings, the connected systems) and sends the best of them; the model picked for «Answers in the search» answers
// from them and says which it used.
//
//	POST <platform>/search/answer   {query, sources: [{kind, title, text}]}
//	                                -> {text, cited: [<index of a source>]}
//	                                   409 {error: "no-model", message}: no model is picked for the feature
//	                                   502 {error: <bgmodel reason>, message}: the provider failed
//	                                the person's page: a page session, same origin
//
// With `Accept: text/event-stream` the answer comes as it is written (server-sent events):
//
//	event: delta   data: {"text": "<a piece, may hold a [2] mark>"}   (as many as the model sends)
//	event: done    data: {"text": "<the whole answer>", "cited": [<index>]}
//	event: error   data: {"error": <bgmodel reason>, "message"}        (a failure after the stream began)
//
// A bad request, no session and no model are still the plain JSON above, before the stream starts. With no model
// picked, the first provider of the registry that holds a key and lists a model answers (bgmodel.Runner.Usable).
//
// The sources are the person's own, already on their page; nothing is kept and no log line carries them.

const searchAnswerBodyMax = 64 << 10

func searchRoutes(d deps) []gate.Route {
	runner := bgRunnerFor(d)
	base := platformAPI + "/search/answer"
	answer := func(w http.ResponseWriter, r *http.Request) {
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
			Query   string                 `json:"query"`
			Sources []bgmodel.SearchSource `json:"sources"`
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, searchAnswerBodyMax))
		if err != nil || json.Unmarshal(raw, &b) != nil || strings.TrimSpace(b.Query) == "" || len(b.Sources) == 0 {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		noModel := func() {
			secretJSON(w, http.StatusConflict, map[string]string{"error": "no-model",
				"message": locale.Pick("No model is picked for answers in the search.", "Для ответов в поиске не выбрана модель.")})
		}
		failMessage := func() string {
			message := locale.Pick("The model did not answer.", "Модель не ответила.")
			if last, ok := runner.Last()[bgmodel.FeatureSearchAnswer]; ok && last.Message != "" {
				message = last.Message
			}
			return message
		}
		if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			if !runner.Usable(bgmodel.FeatureSearchAnswer) {
				noModel()
				return
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
			text, cited, used, err := runner.SearchAnswerStream(r.Context(), b.Query, b.Sources, func(piece string) {
				event("delta", map[string]string{"text": piece})
			})
			switch {
			case !used && !started:
				noModel()
			case err != nil:
				event("error", map[string]string{"error": bgmodel.ReasonOf(err), "message": failMessage()})
			default:
				event("done", map[string]any{"text": text, "cited": cited})
			}
			return
		}
		text, cited, used, err := runner.SearchAnswer(r.Context(), b.Query, b.Sources)
		if !used {
			noModel()
			return
		}
		if err != nil {
			secretJSON(w, http.StatusBadGateway, map[string]string{"error": bgmodel.ReasonOf(err), "message": failMessage()})
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"text": text, "cited": cited})
	}
	return []gate.Route{{Match: func(r *http.Request) bool { return pathIs(r, base) }, Handler: http.HandlerFunc(answer)}}
}
