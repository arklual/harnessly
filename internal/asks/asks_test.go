package asks

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/notifybots"
)

const testToken = "123456:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// fakeTelegram is the Bot API as far as asks call it: it records what is sent and hands out queued updates.
type fakeTelegram struct {
	mu       sync.Mutex
	sent     []map[string]any
	edits    []map[string]any
	answers  []map[string]any
	updates  []notifybots.FullUpdate
	nextID   int64
	sendFail string
}

func (f *fakeTelegram) push(u notifybots.FullUpdate) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u.UpdateID = int64(len(f.updates) + 1)
	f.updates = append(f.updates, u)
}

func (f *fakeTelegram) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	f.mu.Lock()
	defer f.mu.Unlock()
	ok := func(v any) { _ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": v}) }
	switch method {
	case "sendMessage":
		if f.sendFail != "" {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": 403, "description": f.sendFail})
			return
		}
		f.nextID++
		f.sent = append(f.sent, body)
		ok(map[string]any{"message_id": 100 + f.nextID})
	case "editMessageText":
		f.edits = append(f.edits, body)
		ok(true)
	case "answerCallbackQuery":
		f.answers = append(f.answers, body)
		ok(true)
	case "getUpdates":
		offset := int64(0)
		if o, has := body["offset"].(float64); has {
			offset = int64(o)
		}
		var out []notifybots.FullUpdate
		for _, u := range f.updates {
			if u.UpdateID >= offset {
				out = append(out, u)
			}
		}
		if out == nil {
			out = []notifybots.FullUpdate{}
		}
		ok(out)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

type rig struct {
	svc *Service
	tg  *fakeTelegram
	now time.Time
}

func newRig(t *testing.T) *rig {
	t.Helper()
	dir := t.TempDir()
	tg := &fakeTelegram{}
	srv := httptest.NewServer(tg)
	t.Cleanup(srv.Close)
	bots := notifybots.Open(dir)
	if err := bots.Add(notifybots.Bot{ID: "nb_1", Name: "Team", Username: "team_bot", TelegramID: 1, Token: testToken,
		Chat: &notifybots.Chat{ID: -100, Title: "Team chat", Type: "supergroup"}}); err != nil {
		t.Fatal(err)
	}
	r := &rig{tg: tg, now: time.Now()}
	r.svc = &Service{Store: Open(dir), Settings: OpenSettings(dir), Bots: bots, Telegram: &notifybots.Client{Base: srv.URL},
		PollWait: 50 * time.Millisecond, ErrPause: 20 * time.Millisecond}
	t.Cleanup(r.svc.Stop)
	set := Defaults()
	set.Contacts = []Contact{{Name: "Anna", Username: "anna_dev"}, {Name: "Lead", Username: "the_lead", CanApprove: true}}
	if _, err := r.svc.Settings.Put(set); err != nil {
		t.Fatal(err)
	}
	return r
}

func waitFinal(t *testing.T, s *Service, id string) Ask {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, err := s.Wait(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Status.Final() {
		t.Fatalf("still %s", a.Status)
	}
	return a
}

func eventually(t *testing.T, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); !ok(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("not in time")
		}
	}
}

func msg(from notifybots.User, chat int64, text string, replyTo int64) notifybots.FullUpdate {
	m := &notifybots.FullMessage{MessageID: 900, Date: time.Now().Unix(), Text: text, From: &from}
	m.Chat.ID, m.Chat.Type = chat, "supergroup"
	if replyTo != 0 {
		m.ReplyTo = &struct {
			MessageID int64            `json:"message_id"`
			From      *notifybots.User `json:"from"`
		}{MessageID: replyTo}
	}
	return notifybots.FullUpdate{Message: m}
}

var (
	anna   = notifybots.User{ID: 7, FirstName: "Anna", Username: "anna_dev"}
	lead   = notifybots.User{ID: 8, FirstName: "Lead", Username: "the_lead"}
	random = notifybots.User{ID: 9, FirstName: "Someone", Username: "someone"}
)

func TestQuestionAnsweredByReply(t *testing.T) {
	r := newRig(t)
	a, err := r.svc.Create(context.Background(), Request{Question: "Which **port**?", To: "Anna", Session: "s1", Agent: "Fix login"})
	if err != nil {
		t.Fatal(err)
	}
	sent := r.tg.sent[0]
	text := sent["text"].(string)
	if !strings.Contains(text, "@anna_dev") || !strings.Contains(text, "<b>port</b>") || !strings.Contains(text, "«Fix login»") {
		t.Fatalf("message: %s", text)
	}
	if sent["chat_id"].(float64) != -100 {
		t.Fatalf("chat: %v", sent["chat_id"])
	}
	// Someone else's reply is kept but does not answer a question addressed to Anna.
	r.tg.push(msg(random, -100, "8080?", a.MessageID))
	r.tg.push(msg(anna, -100, "3000", a.MessageID))
	got := waitFinal(t, r.svc, a.ID)
	if got.Status != StatusAnswered || got.Answer == nil || got.Answer.Text != "3000" || got.Answer.From.Username != "anna_dev" {
		t.Fatalf("answer: %+v", got)
	}
	if len(got.Replies) != 2 {
		t.Fatalf("replies: %+v", got.Replies)
	}
	// The message in the chat is closed with the outcome.
	eventually(t, func() bool { r.tg.mu.Lock(); defer r.tg.mu.Unlock(); return len(r.tg.edits) > 0 })
	r.tg.mu.Lock()
	defer r.tg.mu.Unlock()
	if len(r.tg.edits) != 1 || !strings.Contains(r.tg.edits[0]["text"].(string), "@anna_dev") {
		t.Fatalf("edits: %+v", r.tg.edits)
	}
}

func TestApprovalButtons(t *testing.T) {
	r := newRig(t)
	a, err := r.svc.Create(context.Background(), Request{Kind: KindApproval, Question: "RFC is up", Link: "https://example.com/rfc", To: "@anna_dev"})
	if err != nil {
		t.Fatal(err)
	}
	kb := r.tg.sent[0]["reply_markup"].(map[string]any)["inline_keyboard"].([]any)
	if len(kb) != 2 {
		t.Fatalf("keyboard: %+v", kb)
	}
	press := func(u notifybots.User, choice string) {
		r.tg.push(notifybots.FullUpdate{CallbackQuery: &notifybots.CallbackQuery{ID: "cb" + choice + u.Username, From: u, Data: "ha:" + a.ID + ":" + choice}})
	}
	press(random, "ok") // not addressed, not an approver: refused
	press(lead, "fix")  // an approver may decide
	got := waitFinal(t, r.svc, a.ID)
	if got.Decision != DecisionChanges || got.Answer.From.Username != "the_lead" {
		t.Fatalf("decision: %+v", got)
	}
	eventually(t, func() bool { r.tg.mu.Lock(); defer r.tg.mu.Unlock(); return len(r.tg.answers) >= 2 })
	r.tg.mu.Lock()
	defer r.tg.mu.Unlock()
	if len(r.tg.answers) < 2 || !strings.Contains(r.tg.answers[0]["text"].(string), "@anna_dev") {
		t.Fatalf("callback answers: %+v", r.tg.answers)
	}
}

func TestOptionsAndWebAnswer(t *testing.T) {
	r := newRig(t)
	a, err := r.svc.Create(context.Background(), Request{Question: "Deploy now?", Options: []string{"Yes", "Tomorrow"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.AnswerFromWeb(a.ID, "Nope", "", Person{Name: "me"}); RefusalOf(err).Reason != ReasonBadChoice {
		t.Fatalf("bad choice: %v", err)
	}
	got, err := r.svc.AnswerFromWeb(a.ID, "o1", "", Person{Name: "me"})
	if err != nil || got.Answer.Text != "Tomorrow" || got.Answer.Via != "web" {
		t.Fatalf("web answer: %+v %v", got, err)
	}
	if _, err := r.svc.AnswerFromWeb(a.ID, "o0", "", Person{}); RefusalOf(err).Reason != ReasonClosed {
		t.Fatalf("second answer: %v", err)
	}
}

func TestApprovalCommentIsNotApproval(t *testing.T) {
	r := newRig(t)
	a, _ := r.svc.Create(context.Background(), Request{Kind: KindApproval, Question: "Ship it?", To: "Anna"})
	// Anna writes without replying: the only ask waiting for her takes it.
	r.tg.push(msg(anna, -100, "looks fine but rename the flag", 0))
	got := waitFinal(t, r.svc, a.ID)
	if got.Decision != DecisionComment {
		t.Fatalf("decision: %q", got.Decision)
	}
}

func TestTimeoutAndCancel(t *testing.T) {
	r := newRig(t)
	now := time.Now()
	r.svc.Now = func() time.Time { return now }
	a, err := r.svc.Create(context.Background(), Request{Question: "Still there?", TimeoutSec: 60})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	got, _ := r.svc.Get(a.ID)
	if got.Status != StatusTimeout {
		t.Fatalf("status: %s", got.Status)
	}
	b, _ := r.svc.Create(context.Background(), Request{Question: "Another"})
	if got, err := r.svc.Cancel(b.ID); err != nil || got.Status != StatusCancelled {
		t.Fatalf("cancel: %+v %v", got, err)
	}
}

func TestRefusals(t *testing.T) {
	r := newRig(t)
	set, _ := r.svc.Settings.Get()
	set.OnlyContacts, set.PerHour = true, 2
	if _, err := r.svc.Settings.Put(set); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := r.svc.Create(ctx, Request{Question: "x", To: "@stranger"}); RefusalOf(err).Reason != ReasonUnknownTo {
		t.Fatalf("stranger: %v", err)
	}
	if _, err := r.svc.Create(ctx, Request{Question: " "}); RefusalOf(err).Reason != ReasonEmpty {
		t.Fatalf("empty: %v", err)
	}
	if _, err := r.svc.Create(ctx, Request{Question: "x", Bot: "nope"}); RefusalOf(err).Reason != ReasonBotNotFound {
		t.Fatalf("bot: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := r.svc.Create(ctx, Request{Question: "q", Session: "s"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.svc.Create(ctx, Request{Question: "q", Session: "s"}); RefusalOf(err).Reason != ReasonRateLimited {
		t.Fatalf("rate: %v", err)
	}
	set.Enabled = false
	_, _ = r.svc.Settings.Put(set)
	if _, err := r.svc.Create(ctx, Request{Question: "q", Session: "other"}); RefusalOf(err).Reason != ReasonDisabled {
		t.Fatalf("disabled: %v", err)
	}
	r.tg.sendFail = "Forbidden: bot was kicked from the supergroup chat"
	set.Enabled = true
	_, _ = r.svc.Settings.Put(set)
	if _, err := r.svc.Create(ctx, Request{Question: "q", Session: "third"}); RefusalOf(err).Reason != notifybots.ReasonChatGone {
		t.Fatalf("kicked: %v", err)
	}
}

func TestQuietHours(t *testing.T) {
	q := Quiet{Enabled: true, From: "22:00", To: "09:00"}
	at := func(h, m int) time.Time { return time.Date(2026, 1, 1, h, m, 0, 0, time.Local) }
	for _, c := range []struct {
		t    time.Time
		want bool
	}{{at(23, 0), true}, {at(3, 0), true}, {at(9, 0), false}, {at(12, 0), false}, {at(22, 0), true}} {
		if q.InQuiet(c.t) != c.want {
			t.Fatalf("%v: want %v", c.t, c.want)
		}
	}
}

func TestSettingsCheck(t *testing.T) {
	set := Defaults()
	set.Contacts = []Contact{{Name: "A", Username: "https://t.me/anna_dev"}}
	out, err := Check(set)
	if err != nil || out.Contacts[0].Username != "anna_dev" || out.Contacts[0].ID == "" {
		t.Fatalf("clean: %+v %v", out, err)
	}
	set.Contacts = []Contact{{Name: "A"}}
	if _, err := Check(set); err == nil {
		t.Fatal("a contact without a way to tag them")
	}
	set = Defaults()
	set.DefaultTimeoutMin = 0
	if _, err := Check(set); err == nil {
		t.Fatal("zero timeout")
	}
}
