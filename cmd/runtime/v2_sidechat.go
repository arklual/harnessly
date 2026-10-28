package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/sessions"
	"github.com/ly-solutions/harnessly/internal/sidechat"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// v2SideChat is the Legacy side chat (internal/sidechat) asked of a v2 conversation: a fork of the conversation's Claude
// session answers one question, never saved. The conversation is the tab: its session and folder are the agent's own,
// never the caller's, and one question runs per conversation.
type v2SideChat struct {
	record func(conv string) (wire.Conversation, bool)
	// cwdOf is the working folder of a tmux session, "" when it is not known.
	cwdOf func(session string) string
	home  string
	// runtimeFile is the machine's runtime config naming the Claude binary and login.
	runtimeFile string
	// envDir holds the machine agent's env files, whose variables the CLI gets ("" when there is none).
	envDir    string
	ask       func(ctx context.Context, run sidechat.Run, onEvent func(sidechat.Event))
	heartbeat time.Duration
}

const sideChatBodyMax = 1 << 20

var v2SideChatRefusals = map[string]struct {
	status int
	text   locale.Text
}{
	"unsupported": {http.StatusUnprocessableEntity, locale.T("Side chat is only available for Claude for now", "Side chat пока только для Claude")},
	"no-session":  {http.StatusConflict, locale.T("The agent has not started a session yet: send it something and try again", "Агент ещё не начал сессию: напишите ему что-нибудь и попробуйте снова")},
	"busy":        {http.StatusConflict, locale.T("The previous question is still running", "Предыдущий вопрос ещё в работе")},
	"bad-request": {http.StatusBadRequest, locale.T("Invalid request", "Неверный запрос")},
}

func (c *v2SideChat) projectDir(cwd string) string {
	return filepath.Join(c.home, ".claude", "projects", sessions.ProjectName(cwd))
}

func (c *v2SideChat) refuse(w http.ResponseWriter, code, text string) {
	r := v2SideChatRefusals[code]
	if text == "" {
		text = r.text.String()
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(r.status)
	_, _ = w.Write(append(jsvalue.Stringify(jsvalue.NewObject("error", text, "code", code), ""), '\n'))
}

// session is the conversation's Claude session id and folder; the code is why there is none.
func (c *v2SideChat) session(rec wire.Conversation) (id, cwd, code string) {
	if rec.Provider != wire.ProviderClaude {
		return "", "", "unsupported"
	}
	id = rec.AgentSessionID
	if id == "" && rec.TranscriptPath != "" {
		id = strings.TrimSuffix(filepath.Base(rec.TranscriptPath), ".jsonl")
	}
	cwd = ""
	if rec.Session != "" && c.cwdOf != nil {
		cwd = c.cwdOf(rec.Session)
	}
	if cwd == "" {
		cwd = rec.Cwd
	}
	if cwd == "" || !agents.IsUUID(id) {
		return "", "", "no-session"
	}
	if st, err := os.Stat(filepath.Join(c.projectDir(cwd), id+".jsonl")); err != nil || !st.Mode().IsRegular() {
		return "", "", "no-session"
	}
	return id, cwd, ""
}

// Serve answers POST /api/v2/conversations/{id}/side-chat.
func (c *v2SideChat) Serve(w http.ResponseWriter, r *http.Request, conv string) error {
	rec, ok := c.record(conv)
	if !ok {
		return convo.ErrNotFound
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		c.refuse(w, "bad-request", "application/json is required")
		return nil
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, sideChatBodyMax))
	if err != nil {
		c.refuse(w, "bad-request", "")
		return nil
	}
	body, err := jsvalue.Parse(raw)
	o, isObj := body.(*jsvalue.Object)
	if err != nil || !isObj {
		c.refuse(w, "bad-request", "")
		return nil
	}
	// The Legacy request names the tab; here the conversation is the tab, whatever the body says.
	o.Set("tabId", conv)
	req, msg := sidechat.ParseRequest(o)
	if msg != "" {
		c.refuse(w, "bad-request", msg)
		return nil
	}
	sessionID, cwd, code := c.session(rec)
	if code != "" {
		c.refuse(w, code, "")
		return nil
	}
	release := sidechat.Claim(conv)
	if release == nil {
		c.refuse(w, "busy", "")
		return nil
	}
	defer release()
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
	// The stream ends with the page: closing it stops the question.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	events := make(chan string, 64)
	ask := c.ask
	if ask == nil {
		ask = sidechat.Ask
	}
	go func() {
		defer close(events)
		ask(ctx, sidechat.Run{SessionID: sessionID, Cwd: cwd, Prompt: sidechat.BuildPrompt(req), Model: req.Model,
			Runtime: sidechat.ReadRuntime(c.runtimeFile), BaseEnv: withAgentEnv(os.Environ(), c.envDir, agents.Claude), ProjectDir: c.projectDir(cwd), Log: coreLog},
			func(e sidechat.Event) { events <- sidechat.Format(e) })
	}()
	beat := c.heartbeat
	if beat <= 0 {
		beat = 15 * time.Second
	}
	tick := time.NewTicker(beat)
	defer tick.Stop()
	for {
		select {
		case frame, ok := <-events:
			if !ok {
				return nil
			}
			_, _ = w.Write([]byte(frame))
			flush()
		case <-tick.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flush()
		}
	}
}
