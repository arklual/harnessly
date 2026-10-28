package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/notifybots"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// The Telegram bots for notifications (internal/notifybots). Who may call what:
//
//	GET|POST <platform>/notify-bots, PUT|DELETE <platform>/notify-bots/<id>,
//	POST|DELETE <platform>/notify-bots/<id>/bind, POST <platform>/notify-bots/<id>/test
//	                                              the person's page: a page session (Settings → Notifications)
//	GET|PUT <platform>/notify-shared, POST|DELETE <platform>/notify-shared/link, POST <platform>/notify-shared/test
//	                                              the person's page: the shared bot
//	GET /api/cli/notify/bots, POST /api/cli/notify/send
//	                                              an agent of this machine: the cli-token (harnessly notify); a send
//	                                              without a bot goes through the shared bot
//
// No answer ever carries a token: the page gets whether one is set and its last characters, the agents nothing of it.
//
// The shared bot (internal/notifybots, shared.go) is the cloud's, reached through the machine agent, unless the runtime
// has a bot of its own installation (RUNTIME_NOTIFY_BOT_TOKEN, self-hosted). Bots of the person's own are linked only
// as RUNTIME_NOTIFY_OWN_BOTS says: auto (the default) while there is no shared bot, on always, off never.

const notifyBodyMax = 64 << 10

// notifyTelegramAPI is Telegram's address (RUNTIME_TELEGRAM_API_URL replaces it, for a proxy or a test).
func notifyTelegramAPI() string { return env("RUNTIME_TELEGRAM_API_URL", notifybots.DefaultAPI) }

// notifyOwnBotsMode is RUNTIME_NOTIFY_OWN_BOTS.
func notifyOwnBotsMode() string { return env("RUNTIME_NOTIFY_OWN_BOTS", notifybots.OwnBotsAuto) }

var (
	notifySharedMu sync.Mutex
	notifyShareds  = map[string]*notifybots.Shared{}
)

// notifySharedFor is the process's one shared bot of a data folder (the routes and the agents' launch share it, and a
// self-hosted bot's messages are read by one loop only).
func notifySharedFor(dataDir string) *notifybots.Shared {
	notifySharedMu.Lock()
	defer notifySharedMu.Unlock()
	if s := notifyShareds[dataDir]; s != nil {
		return s
	}
	s := &notifybots.Shared{CachePath: notifybots.SharedCachePath(dataDir)}
	if token := strings.TrimSpace(os.Getenv("RUNTIME_NOTIFY_BOT_TOKEN")); token != "" {
		local := &notifybots.Local{
			Token:    token,
			Telegram: &notifybots.Client{Base: notifyTelegramAPI()},
			Path:     filepath.Join(dataDir, "v2", notifybots.LocalFile),
			Linked: locale.Pick("✅ Telegram is connected to harnessly: agents will write here as your instruction in Settings says. /stop disconnects it.",
				"✅ Telegram подключён к harnessly: агенты будут писать сюда, как сказано в инструкции в Настройках. Отключить — /stop."),
			Stopped: locale.Pick("Disconnected: agents will not write here any more. Connect again in Settings → Notifications.",
				"Отключено: агенты сюда больше не пишут. Подключить снова — в Настройках → Уведомления."),
			Hello: locale.Pick("To connect this chat, press «Connect Telegram» in harnessly: Settings → Notifications.",
				"Чтобы подключить этот чат, нажмите «Подключить Telegram» в harnessly: Настройки → Уведомления."),
			Log: func(reason string) {
				agent.Log("warn", "notify: the shared bot's messages cannot be read", agent.F{K: "reason", V: reason})
			},
		}
		go local.Run(context.Background())
		s.Backend = local
	} else {
		header := os.Getenv("RUNTIME_AGENT_TOKEN_HEADER")
		s.Backend = &notifybots.Cloud{Call: func(route string, body any) (int, []byte, error) {
			status, raw, err := agent.CallLocal(nil, dataDir, header, route, body, 50*time.Second)
			if errors.Is(err, agent.ErrNoAgent) {
				return 0, nil, notifybots.ErrNoAgent
			}
			return status, raw, err
		}}
	}
	notifyShareds[dataDir] = s
	return s
}

func newNotifyBots(d deps) *notifybots.Service {
	return &notifybots.Service{
		Store:    notifybots.Open(d.DataDir),
		Telegram: &notifybots.Client{Base: notifyTelegramAPI()},
		Confirm: locale.Pick("✅ This chat is linked to harnessly: agents will write here as the instruction in Settings says.",
			"✅ Чат привязан к harnessly: агенты будут писать сюда, как сказано в инструкции в Настройках."),
	}
}

func notifyJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func readNotifyBody(r *http.Request, out any) bool {
	raw, err := io.ReadAll(io.LimitReader(r.Body, notifyBodyMax+1))
	if err != nil || len(raw) > notifyBodyMax {
		return false
	}
	return json.Unmarshal(raw, out) == nil
}

// notifyRefusal answers an error of the service as {error: <reason>, detail?} with a status that fits it.
func notifyRefusal(w http.ResponseWriter, err error) {
	r := notifybots.RefusalOf(err)
	status := http.StatusBadRequest
	switch r.Reason {
	case notifybots.ReasonNotFound, notifybots.ReasonNoShared:
		status = http.StatusNotFound
	case notifybots.ReasonDuplicate, notifybots.ReasonAmbiguous, notifybots.ReasonNotLinked:
		status = http.StatusConflict
	case notifybots.ReasonDisabled:
		status = http.StatusForbidden
	case notifybots.ReasonOffline:
		status = http.StatusServiceUnavailable
	case notifybots.ReasonRateLimited:
		status = http.StatusTooManyRequests
	case notifybots.ReasonUnreachable:
		status = http.StatusBadGateway
	case notifybots.ReasonFailed:
		status = http.StatusInternalServerError
	}
	body := map[string]string{"error": r.Reason}
	if r.Detail != "" {
		body["detail"] = r.Detail
	}
	notifyJSON(w, status, body)
}

// notifyTestText is the page's test message.
func notifyTestText(name string) string {
	return locale.Pick("👋 A test from harnessly: «"+name+"» works. Agents will write here as its instruction says.",
		"👋 Проверка из harnessly: бот «"+name+"» работает. Агенты будут писать сюда, как сказано в его инструкции.")
}

// notifySharedTestText is the page's test message through the shared bot.
func notifySharedTestText() string {
	return locale.Pick("👋 A test from harnessly: agents will write here as your instruction in Settings says.",
		"👋 Проверка из harnessly: агенты будут писать сюда, как сказано в инструкции в Настройках.")
}

// notifyStatusWait is how long the page waits for the cloud before it is shown what was seen last.
const notifyStatusWait = 8 * time.Second

func notifyBotRoutes(d deps) []gate.Route {
	svc := newNotifyBots(d)
	shared := notifySharedFor(d.DataDir)
	ownAllowed := func() bool { return notifybots.OwnBotsAllowed(notifyOwnBotsMode(), shared.Cached().Enabled) }
	base := platformAPI + "/notify-bots"
	sharedBase := platformAPI + "/notify-shared"
	under := func(r *http.Request, p string) (string, bool) {
		for _, prefix := range []string{p + "/", "/agents" + p + "/"} {
			if rest, ok := strings.CutPrefix(r.URL.Path, prefix); ok && rest != "" {
				return rest, true
			}
		}
		return "", false
	}
	pageOK := func(w http.ResponseWriter, r *http.Request) bool {
		// The session cookie goes with a request of any site: a page of another origin must not link or send.
		if sameorigin.Refuse(w, r) {
			return false
		}
		if !d.SessionOK(r) {
			notifyJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return false
		}
		return true
	}
	cliOK := func(w http.ResponseWriter, r *http.Request) bool {
		if d.CLI == nil || !d.CLI.OK(r) {
			notifyJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
			return false
		}
		return true
	}
	badBody := func(w http.ResponseWriter) {
		notifyJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
	}
	notAllowed := func(w http.ResponseWriter) {
		notifyJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
	}
	telegramCtx := func(r *http.Request) (context.Context, context.CancelFunc) {
		return context.WithTimeout(r.Context(), 20*time.Second)
	}

	list := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r) {
			return
		}
		switch r.Method {
		case http.MethodGet:
			views, err := svc.Views()
			if err != nil {
				notifyRefusal(w, err)
				return
			}
			notifyJSON(w, http.StatusOK, map[string]any{"bots": views})
		case http.MethodPost:
			if !ownAllowed() {
				notifyRefusal(w, &notifybots.Refusal{Reason: notifybots.ReasonDisabled})
				return
			}
			var b struct {
				Token       string `json:"token"`
				Name        string `json:"name"`
				Instruction string `json:"instruction"`
			}
			if !readNotifyBody(r, &b) {
				badBody(w)
				return
			}
			ctx, cancel := telegramCtx(r)
			defer cancel()
			v, err := svc.Add(ctx, b.Token, b.Name, b.Instruction)
			if err != nil {
				notifyRefusal(w, err)
				return
			}
			notifyJSON(w, http.StatusOK, map[string]any{"bot": v})
		default:
			notAllowed(w)
		}
	}

	one := func(w http.ResponseWriter, r *http.Request) {
		rest, _ := under(r, base)
		if !pageOK(w, r) {
			return
		}
		id, action, _ := strings.Cut(rest, "/")
		answer := func(v notifybots.View, err error) {
			if err != nil {
				notifyRefusal(w, err)
				return
			}
			notifyJSON(w, http.StatusOK, map[string]any{"bot": v})
		}
		switch {
		case action == "" && r.Method == http.MethodPut:
			var c notifybots.Change
			if !readNotifyBody(r, &c) {
				badBody(w)
				return
			}
			ctx, cancel := telegramCtx(r)
			defer cancel()
			answer(svc.Edit(ctx, id, c))
		case action == "" && r.Method == http.MethodDelete:
			if err := svc.Remove(id); err != nil {
				notifyRefusal(w, err)
				return
			}
			notifyJSON(w, http.StatusOK, map[string]any{"removed": id})
		case action == "bind" && r.Method == http.MethodPost:
			answer(svc.StartBinding(id))
		case action == "bind" && r.Method == http.MethodDelete:
			svc.StopBinding(id)
			answer(svc.ViewOf(id))
		case action == "test" && r.Method == http.MethodPost:
			b, err := svc.Store.Get(id)
			if err != nil {
				notifyRefusal(w, err)
				return
			}
			ctx, cancel := telegramCtx(r)
			defer cancel()
			if _, err := svc.Test(ctx, id, notifyTestText(b.Name)); err != nil {
				notifyRefusal(w, err)
				return
			}
			answer(svc.ViewOf(id))
		case action == "" || action == "bind" || action == "test":
			notAllowed(w)
		default:
			notifyJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
		}
	}

	// The shared bot for the page: its state, and whether bots of the person's own are offered next to it.
	sharedAnswer := func(w http.ResponseWriter, st notifybots.SharedState, problem string) {
		notifyJSON(w, http.StatusOK, map[string]any{"shared": st, "ownBots": notifybots.OwnBotsAllowed(notifyOwnBotsMode(), st.Enabled), "problem": problem})
	}
	sharedChange := func(w http.ResponseWriter, st notifybots.SharedState, err error) {
		if err != nil {
			notifyRefusal(w, err)
			return
		}
		sharedAnswer(w, st, "")
	}
	sharedState := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r) {
			return
		}
		switch r.Method {
		case http.MethodGet:
			ctx, cancel := context.WithTimeout(r.Context(), notifyStatusWait)
			defer cancel()
			st, err := shared.State(ctx)
			problem := ""
			if err != nil {
				problem = notifybots.RefusalOf(err).Reason
			}
			sharedAnswer(w, st, problem)
		case http.MethodPut:
			var b struct {
				Instruction string `json:"instruction"`
			}
			if !readNotifyBody(r, &b) {
				badBody(w)
				return
			}
			ctx, cancel := telegramCtx(r)
			defer cancel()
			st, err := shared.SetInstruction(ctx, b.Instruction)
			sharedChange(w, st, err)
		default:
			notAllowed(w)
		}
	}
	sharedAction := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r) {
			return
		}
		ctx, cancel := telegramCtx(r)
		defer cancel()
		action := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/agents"), sharedBase+"/")
		switch {
		case action == "link" && r.Method == http.MethodPost:
			st, err := shared.Link(ctx)
			sharedChange(w, st, err)
		case action == "link" && r.Method == http.MethodDelete:
			st, err := shared.Unlink(ctx)
			sharedChange(w, st, err)
		case action == "test" && r.Method == http.MethodPost:
			if _, err := shared.Send(ctx, notifySharedTestText(), false); err != nil {
				notifyRefusal(w, err)
				return
			}
			st, err := shared.State(ctx)
			sharedChange(w, st, err)
		case action == "link" || action == "test":
			notAllowed(w)
		default:
			notifyJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
		}
	}

	return []gate.Route{
		{Match: func(r *http.Request) bool { return pathIs(r, sharedBase) }, Handler: http.HandlerFunc(sharedState)},
		{Match: func(r *http.Request) bool { return pathIs(r, sharedBase+"/link") || pathIs(r, sharedBase+"/test") }, Handler: http.HandlerFunc(sharedAction)},
		{Match: func(r *http.Request) bool { return pathIs(r, base) }, Handler: http.HandlerFunc(list)},
		{Match: func(r *http.Request) bool { _, ok := under(r, base); return ok }, Handler: http.HandlerFunc(one)},
		{Match: func(r *http.Request) bool { return pathIs(r, "/api/cli/notify/bots") }, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cliOK(w, r) {
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), notifyStatusWait)
			defer cancel()
			st, _ := shared.State(ctx)
			bots := []notifybots.AgentBot{}
			if notifybots.OwnBotsAllowed(notifyOwnBotsMode(), st.Enabled) {
				own, err := svc.AgentBots()
				if err != nil {
					notifyRefusal(w, err)
					return
				}
				bots = own
			}
			view := map[string]any{"enabled": st.Enabled, "bot": st.Bot, "linked": st.Chat != nil, "chat": "", "instruction": st.Instruction}
			if st.Chat != nil {
				view["chat"] = st.Chat.Title
			}
			notifyJSON(w, http.StatusOK, map[string]any{"bots": bots, "shared": view})
		})},
		{Match: func(r *http.Request) bool { return pathIs(r, "/api/cli/notify/send") }, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cliOK(w, r) {
				return
			}
			var b struct {
				Bot    string `json:"bot"`
				Text   string `json:"text"`
				Silent bool   `json:"silent"`
			}
			if r.Method != http.MethodPost {
				notAllowed(w)
				return
			}
			if !readNotifyBody(r, &b) {
				badBody(w)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 50*time.Second)
			defer cancel()
			if strings.TrimSpace(b.Bot) == "" {
				sent, err := shared.Send(ctx, b.Text, b.Silent)
				if err != nil {
					if sent.Messages > 0 {
						r := notifybots.RefusalOf(err)
						notifyJSON(w, http.StatusBadGateway, map[string]any{"error": r.Reason, "detail": "sent " + strconv.Itoa(sent.Messages) + " parts before: " + r.Detail})
						return
					}
					notifyRefusal(w, err)
					return
				}
				notifyJSON(w, http.StatusOK, map[string]any{"bot": "", "chat": sent.Chat, "messages": sent.Messages})
				return
			}
			if !ownAllowed() {
				notifyRefusal(w, &notifybots.Refusal{Reason: notifybots.ReasonDisabled})
				return
			}
			sent, err := svc.Send(ctx, b.Bot, b.Text, b.Silent)
			if err != nil {
				if sent.Messages > 0 {
					r := notifybots.RefusalOf(err)
					notifyJSON(w, http.StatusBadGateway, map[string]any{"error": r.Reason, "detail": "sent " + strconv.Itoa(sent.Messages) + " parts before: " + r.Detail})
					return
				}
				notifyRefusal(w, err)
				return
			}
			notifyJSON(w, http.StatusOK, sent)
		})},
	}
}
