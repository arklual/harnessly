package main

import (
	"context"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/asks"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/notifybots"
	"github.com/ly-solutions/harnessly/internal/push"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// An agent asks a person (internal/asks). Who may call what:
//
//	GET <platform>/asks, GET <platform>/asks/<id>, POST <platform>/asks/<id>/answer|cancel,
//	GET|PUT <platform>/asks-settings           the person's page: a page session
//	POST /api/cli/asks, GET /api/cli/asks, GET /api/cli/asks/people,
//	GET /api/cli/asks/<id>[/wait], POST /api/cli/asks/<id>/cancel
//	                                           an agent of this machine: the cli-token (harnessly ask)

// askWaitMax is the longest one wait request holds; the CLI asks again until the ask closes.
const askWaitMax = 50 * time.Second

// askOfOutput reads the ask a background `harnessly ask` waits on from its output file; set once the routes are made.
var askOfOutput func(path string) (asks.Ask, bool)

// askLine is the CLI's first line: "asked ask_<hex> …".
var askLine = regexp.MustCompile(`\basked (ask_[0-9a-f]{12})\b`)

// describeAsk turns a background `harnessly ask` into what it is: a wait for a person's answer.
func describeAsk(task *wire.BackgroundTask) {
	if askOfOutput == nil || task.OutputPath == nil {
		return
	}
	a, ok := askOfOutput(*task.OutputPath)
	if !ok {
		return
	}
	who := locale.Pick("the chat", "чата")
	if a.To != nil {
		who = a.To.Label()
	}
	task.Title = locale.Pick("Waiting for an answer from "+who+" in «"+a.ChatTitle+"»",
		"Ждёт ответа от "+who+" в «"+a.ChatTitle+"»")
	last := a.Question
	task.Last = &last
}

func readAskOutput(svc *asks.Service) func(string) (asks.Ask, bool) {
	return func(path string) (asks.Ask, bool) {
		f, err := os.Open(path)
		if err != nil {
			return asks.Ask{}, false
		}
		defer f.Close()
		head := make([]byte, 512)
		n, _ := f.Read(head)
		m := askLine.FindSubmatch(head[:n])
		if m == nil {
			return asks.Ask{}, false
		}
		a, err := svc.Get(string(m[1]))
		return a, err == nil
	}
}

func newAsks(d deps) *asks.Service {
	return &asks.Service{
		Store:    asks.Open(d.DataDir),
		Settings: asks.OpenSettings(d.DataDir),
		Bots:     notifybots.Open(d.DataDir),
		Telegram: &notifybots.Client{Base: notifyTelegramAPI()},
	}
}

// askPusher sends an ask addressed to the machine's owner to their browsers too.
func askPusher(d deps, svc *asks.Service) func(before *asks.Ask, after asks.Ask) {
	h := hookConfigFromEnv(d.ConfigFile)
	if h.PushSubject == "" || h.PushTagPrefix == "" {
		return nil
	}
	sender := newSharedPusher(d.DataDir, h)
	return func(before *asks.Ask, after asks.Ask) {
		if before != nil || after.To == nil {
			return
		}
		set, err := svc.Settings.Get()
		if err != nil || !set.BrowserNotify {
			return
		}
		mine := false
		for _, c := range set.Contacts {
			if c.Me && ((c.Username != "" && strings.EqualFold(c.Username, after.To.Username)) || (c.TelegramID != 0 && c.TelegramID == after.To.TelegramID)) {
				mine = true
			}
		}
		if !mine {
			return
		}
		title := locale.Pick("An agent asks you", "Агент спрашивает вас")
		if after.Kind == asks.KindApproval {
			title = locale.Pick("An agent needs your approval", "Агенту нужно ваше согласование")
		}
		msg := push.Message{Kind: "question", Title: title, Body: push.Clip(after.Question, 180), ConversationTitle: after.Agent,
			TabID: after.Conversation, Tag: sender.TagPrefix() + "ask-" + after.ID, At: time.Now().UnixMilli()}
		go func() {
			for _, sub := range push.SelectTargets(sender.Store.Subscriptions(), sender.Store.Visible()) {
				_, _ = sender.Send(sub, msg)
			}
		}()
	}
}

func askRefusal(w http.ResponseWriter, err error) {
	r := asks.RefusalOf(err)
	status := http.StatusBadRequest
	switch r.Reason {
	case asks.ReasonNotFound:
		status = http.StatusNotFound
	case asks.ReasonClosed:
		status = http.StatusConflict
	case asks.ReasonRateLimited:
		status = http.StatusTooManyRequests
	case notifybots.ReasonUnreachable:
		status = http.StatusBadGateway
	case asks.ReasonFailed:
		status = http.StatusInternalServerError
	}
	body := map[string]string{"error": r.Reason}
	if r.Detail != "" {
		body["detail"] = r.Detail
	}
	notifyJSON(w, status, body)
}

// askBot is a bot as the settings page offers it.
type askBot struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Chat  string `json:"chat"`
	Bound bool   `json:"bound"`
}

func askBots(svc *asks.Service) []askBot {
	bots, err := svc.Bots.List()
	out := []askBot{}
	if err != nil {
		return out
	}
	for _, b := range bots {
		v := askBot{ID: b.ID, Name: b.Name, Bound: b.Chat != nil}
		if b.Chat != nil {
			v.Chat = b.Chat.Title
		}
		out = append(out, v)
	}
	return out
}

func askRoutes(d deps, c *core) []gate.Route {
	svc := newAsks(d)
	svc.Changed = askPusher(d, svc)
	askOfOutput = readAskOutput(svc)
	svc.Start()

	base, settingsPath := platformAPI+"/asks", platformAPI+"/asks-settings"
	under := func(r *http.Request, p string) (string, bool) {
		for _, prefix := range []string{p + "/", "/agents" + p + "/"} {
			if rest, ok := strings.CutPrefix(r.URL.Path, prefix); ok && rest != "" {
				return rest, true
			}
		}
		return "", false
	}
	pageOK := func(w http.ResponseWriter, r *http.Request) bool {
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
	one := func(w http.ResponseWriter, a asks.Ask, err error) {
		if err != nil {
			askRefusal(w, err)
			return
		}
		notifyJSON(w, http.StatusOK, map[string]any{"ask": a.View()})
	}
	list := func(w http.ResponseWriter) {
		all, err := svc.List()
		if err != nil {
			askRefusal(w, err)
			return
		}
		notifyJSON(w, http.StatusOK, map[string]any{"asks": asks.Views(all)})
	}

	page := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r) {
			return
		}
		if pathIs(r, base) {
			if r.Method != http.MethodGet {
				notAllowed(w)
				return
			}
			list(w)
			return
		}
		rest, _ := under(r, base)
		id, action, _ := strings.Cut(rest, "/")
		switch {
		case action == "" && r.Method == http.MethodGet:
			a, err := svc.Get(id)
			one(w, a, err)
		case action == "answer" && r.Method == http.MethodPost:
			var b struct {
				Choice string `json:"choice"`
				Text   string `json:"text"`
			}
			if !readNotifyBody(r, &b) {
				badBody(w)
				return
			}
			a, err := svc.AnswerFromWeb(id, b.Choice, b.Text, asks.Person{Name: locale.Pick("you", "вы")})
			one(w, a, err)
		case action == "cancel" && r.Method == http.MethodPost:
			a, err := svc.Cancel(id)
			one(w, a, err)
		default:
			notAllowed(w)
		}
	}

	settings := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r) {
			return
		}
		switch r.Method {
		case http.MethodGet:
			set, err := svc.Settings.Get()
			if err != nil {
				askRefusal(w, err)
				return
			}
			notifyJSON(w, http.StatusOK, map[string]any{"settings": set, "bots": askBots(svc)})
		case http.MethodPut:
			var set asks.Settings
			if !readNotifyBody(r, &set) {
				badBody(w)
				return
			}
			out, err := svc.Settings.Put(set)
			if err != nil {
				askRefusal(w, err)
				return
			}
			notifyJSON(w, http.StatusOK, map[string]any{"settings": out, "bots": askBots(svc)})
		default:
			notAllowed(w)
		}
	}

	// The agent's side.
	cliCreate := func(w http.ResponseWriter, r *http.Request) {
		var req asks.Request
		if !readNotifyBody(r, &req) {
			badBody(w)
			return
		}
		if rt := (peerLocal{c: c}).rt(); rt != nil && req.Session != "" {
			req.Conversation = rt.ConvOfSession(req.Session)
			if rec, ok := rt.Record(req.Conversation); ok {
				req.Agent = rec.Title
				if rec.ProjectID != nil {
					req.Workspace = *rec.ProjectID
				}
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		a, err := svc.Create(ctx, req)
		one(w, a, err)
	}
	cli := func(w http.ResponseWriter, r *http.Request) {
		if !cliOK(w, r) {
			return
		}
		if pathIs(r, "/api/cli/asks") {
			switch r.Method {
			case http.MethodPost:
				cliCreate(w, r)
			case http.MethodGet:
				list(w)
			default:
				notAllowed(w)
			}
			return
		}
		rest, _ := under(r, "/api/cli/asks")
		id, action, _ := strings.Cut(rest, "/")
		switch {
		case id == "people" && action == "" && r.Method == http.MethodGet:
			set, err := svc.Settings.Get()
			if err != nil {
				askRefusal(w, err)
				return
			}
			notifyJSON(w, http.StatusOK, map[string]any{"enabled": set.Enabled, "onlyContacts": set.OnlyContacts,
				"defaultTimeoutMin": set.DefaultTimeoutMin, "maxTimeoutMin": set.MaxTimeoutMin,
				"contacts": set.AgentContacts(), "bots": askBots(svc)})
		case action == "" && r.Method == http.MethodGet:
			a, err := svc.Get(id)
			one(w, a, err)
		case action == "wait" && r.Method == http.MethodGet:
			hold := askWaitMax
			if s, err := strconv.Atoi(r.URL.Query().Get("timeout")); err == nil && s >= 0 && time.Duration(s)*time.Second < hold {
				hold = time.Duration(s) * time.Second
			}
			ctx, cancel := context.WithTimeout(r.Context(), hold)
			defer cancel()
			a, err := svc.Wait(ctx, id)
			if err == nil && a.Status.Final() {
				a, err = svc.MarkSeen(id)
			}
			one(w, a, err)
		case action == "cancel" && r.Method == http.MethodPost:
			a, err := svc.Cancel(id)
			one(w, a, err)
		default:
			notAllowed(w)
		}
	}

	return []gate.Route{
		{Match: func(r *http.Request) bool { _, ok := under(r, base); return pathIs(r, base) || ok }, Handler: http.HandlerFunc(page)},
		{Match: func(r *http.Request) bool { return pathIs(r, settingsPath) }, Handler: http.HandlerFunc(settings)},
		{Match: func(r *http.Request) bool {
			_, ok := under(r, "/api/cli/asks")
			return pathIs(r, "/api/cli/asks") || ok
		}, Handler: http.HandlerFunc(cli)},
	}
}
