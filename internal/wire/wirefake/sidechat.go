package wirefake

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/sidechat"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// SideChat answers a side question of a Claude conversation in a few deltas, with the frames of the real one; a Codex
// conversation is refused as unsupported (422). A question that says "busy" is refused as the previous one still
// running (409), one that says "fail" ends with an error frame, so a page can see each.
func (s *Service) SideChat(w http.ResponseWriter, r *http.Request, id string) error {
	s.mu.Lock()
	c, err := s.get(id)
	var provider wire.Provider
	if err == nil {
		provider = c.rec.Provider
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	refuse := func(status int, code, text string) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write(append(jsvalue.Stringify(jsvalue.NewObject("error", text, "code", code), ""), '\n'))
	}
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	body, perr := jsvalue.Parse(raw)
	o, ok := body.(*jsvalue.Object)
	if perr != nil || !ok {
		refuse(http.StatusBadRequest, "bad-request", "Invalid request")
		return nil
	}
	o.Set("tabId", id)
	req, msg := sidechat.ParseRequest(o)
	if msg != "" {
		refuse(http.StatusBadRequest, "bad-request", msg)
		return nil
	}
	switch {
	case provider != wire.ProviderClaude:
		refuse(http.StatusUnprocessableEntity, "unsupported", "Side chat is only available for Claude for now")
		return nil
	case strings.Contains(req.Question, "busy"):
		refuse(http.StatusConflict, "busy", "The previous question is still running")
		return nil
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-transform")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	send := func(e sidechat.Event) {
		_, _ = w.Write([]byte(sidechat.Format(e)))
		if flusher != nil {
			flusher.Flush()
		}
	}
	pause := func() bool {
		select {
		case <-time.After(s.opt.Step / 2):
			return true
		case <-r.Context().Done():
			return false
		}
	}
	words := strings.Fields("Это пробный ответ на вопрос: " + req.Question)
	text := ""
	for i, word := range words {
		if strings.Contains(req.Question, "fail") && i == len(words)/2 {
			send(sidechat.Event{Type: "error", Code: "failed", Message: "The fake side chat failed as asked"})
			return nil
		}
		part := word
		if i > 0 {
			part = " " + word
		}
		text += part
		send(sidechat.Event{Type: "delta", Text: part})
		if !pause() {
			return nil
		}
	}
	send(sidechat.Event{Type: "done", Text: text})
	return nil
}
