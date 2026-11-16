package telegram_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/connectors"
	"github.com/ly-solutions/harnessly/internal/connectors/telegram"
)

const (
	goodToken = "123456789:AAbbccddeeffgghhiijjkkllmmnnooppqq"
	owner     = int64(42)
	stranger  = int64(99)
)

// fakeTG is the Bot API: it serves getMe, hands out the updates the test queues and records what the bot sends.
type fakeTG struct {
	mu      sync.Mutex
	updates []map[string]any
	nextID  int64
	msgID   int64
	// replay serves every update again whatever the offset (a server that lost the confirmations).
	replay bool
	sent   []map[string]any
	edits  []map[string]any
	acks   []map[string]any
	leaves []map[string]any
}

func (f *fakeTG) push(u map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	u["update_id"] = f.nextID
	f.updates = append(f.updates, u)
}

func (f *fakeTG) message(chat, from int64, kind, text string) {
	f.mu.Lock()
	f.msgID++
	id := f.msgID
	f.mu.Unlock()
	f.push(map[string]any{"message": map[string]any{"message_id": id, "date": time.Now().Unix(), "text": text,
		"from": map[string]any{"id": from, "first_name": "U"},
		"chat": map[string]any{"id": chat, "type": kind, "title": "Team"}}})
}

func (f *fakeTG) press(from, chat int64, data string) {
	f.push(map[string]any{"callback_query": map[string]any{"id": "cb" + time.Now().Format("150405.000000000"), "data": data,
		"from":    map[string]any{"id": from, "first_name": "X"},
		"message": map[string]any{"message_id": 1, "date": 0, "chat": map[string]any{"id": chat, "type": "private"}}}})
}

func (f *fakeTG) sentTo(chat int64) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []map[string]any
	for _, s := range f.sent {
		if int64(s["chat_id"].(float64)) == chat {
			out = append(out, s)
		}
	}
	return out
}

func (f *fakeTG) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/bot")
	tok, method, _ := strings.Cut(rest, "/")
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	ok := func(result any) { _ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result}) }
	if tok != goodToken {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": 401, "description": "Unauthorized"})
		return
	}
	switch method {
	case "getMe":
		ok(map[string]any{"id": 1, "is_bot": true, "first_name": "Bot", "username": "my_bot"})
	case "getUpdates":
		offset, _ := body["offset"].(float64)
		deadline := time.Now().Add(40 * time.Millisecond)
		for {
			f.mu.Lock()
			var out []map[string]any
			for _, u := range f.updates {
				if f.replay || u["update_id"].(int64) >= int64(offset) {
					out = append(out, u)
				}
			}
			f.mu.Unlock()
			if len(out) > 0 || time.Now().After(deadline) {
				ok(out)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	case "sendMessage":
		f.mu.Lock()
		f.sent = append(f.sent, body)
		f.msgID++
		id := f.msgID
		f.mu.Unlock()
		ok(map[string]any{"message_id": id})
	case "editMessageText":
		f.mu.Lock()
		f.edits = append(f.edits, body)
		f.mu.Unlock()
		ok(true)
	case "answerCallbackQuery":
		f.mu.Lock()
		f.acks = append(f.acks, body)
		f.mu.Unlock()
		ok(true)
	case "leaveChat":
		f.mu.Lock()
		f.leaves = append(f.leaves, body)
		f.mu.Unlock()
		ok(true)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

type memSafe struct {
	mu sync.Mutex
	m  map[string]string
}

func (s *memSafe) Put(n, v string) error { s.mu.Lock(); defer s.mu.Unlock(); s.m[n] = v; return nil }
func (s *memSafe) Reveal(n string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[n]
	if !ok {
		return "", errors.New("no secret")
	}
	return v, nil
}
func (s *memSafe) Remove(n string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, n)
	return nil
}

// hub is a destination that takes everything and keeps it.
type hub struct {
	mu  sync.Mutex
	got []connectors.Envelope
}

func (h *hub) Name() string                   { return "hub" }
func (h *hub) Wants(connectors.Envelope) bool { return true }
func (h *hub) Deliver(_ context.Context, batch []connectors.Envelope) ([]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var fps []string
	for _, e := range batch {
		h.got = append(h.got, e)
		fps = append(fps, e.Fingerprint)
	}
	return fps, nil
}

func (h *hub) events() []connectors.Envelope {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]connectors.Envelope(nil), h.got...)
}

// chats records what the connector put into chats of harnessly.
type chats struct {
	mu  sync.Mutex
	got []string
}

func (c *chats) add(conv, text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, conv+"|"+text)
}

func (c *chats) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.got...)
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("never: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type world struct {
	tg    *fakeTG
	safe  *memSafe
	dir   string
	chats *chats
}

func newWorld(t *testing.T) *world {
	t.Helper()
	tg := &fakeTG{}
	srv := httptest.NewServer(tg)
	t.Cleanup(srv.Close)
	telegram.API = srv.URL
	w := &world{tg: tg, safe: &memSafe{m: map[string]string{}}, dir: t.TempDir(), chats: &chats{}}
	telegram.SetHooks(telegram.Hooks{ToChat: func(_ context.Context, conv, text string) error {
		w.chats.add(conv, text)
		return nil
	}, Open: func(conv string) string {
		return "https://hub.example.com/agents/?c=" + url.QueryEscape("agent:m1:"+conv)
	}})
	return w
}

func (w *world) start(t *testing.T) (*connectors.Service, *hub, context.CancelFunc) {
	t.Helper()
	s := connectors.New(w.dir, w.safe, nil)
	h := &hub{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s.Start(ctx, h)
	return s, h, cancel
}

// bind connects the bot and binds the owner's chat with the code of the pairing link.
func bind(t *testing.T, w *world, s *connectors.Service, values map[string]string) connectors.Conn {
	t.Helper()
	values["token"] = goodToken
	conn, err := s.Connect(context.Background(), telegram.ID, "", values)
	if err != nil {
		t.Fatal(err)
	}
	if conn.Account != "@my_bot" {
		t.Fatalf("account %q", conn.Account)
	}
	views, err := s.Views(context.Background())
	if err != nil || len(views) == 0 || views[0].Next == nil {
		t.Fatalf("no pairing step: %+v %v", views, err)
	}
	u, err := url.Parse(views[0].Next.URL)
	if err != nil || u.Host != "t.me" || u.Path != "/my_bot" || u.Query().Get("start") == "" {
		t.Fatalf("pairing link %q", views[0].Next.URL)
	}
	code := u.Query().Get("start")
	// Someone else's message and a wrong code bind nothing.
	w.tg.message(stranger, stranger, "private", "hello")
	w.tg.message(owner, owner, "private", "/start deadbeef")
	w.tg.message(owner, owner, "private", "/start "+code)
	eventually(t, "the confirmation in the owner's chat", func() bool { return len(w.tg.sentTo(owner)) == 1 })
	if text, _ := w.tg.sentTo(owner)[0]["text"].(string); !strings.Contains(text, "bound to harnessly") {
		t.Fatalf("the confirmation: %q", text)
	}
	views, _ = s.Views(context.Background())
	if views[0].Next != nil {
		t.Fatalf("still a pairing step after the binding: %+v", views[0].Next)
	}
	return conn
}

func TestCheckRefusesAWrongToken(t *testing.T) {
	w := newWorld(t)
	s, _, _ := w.start(t)
	if _, err := s.Connect(context.Background(), telegram.ID, "", map[string]string{"token": "nope"}); err == nil {
		t.Fatal("a malformed token connected")
	}
	_, err := s.Connect(context.Background(), telegram.ID, "", map[string]string{"token": "123456789:ZZbbccddeeffgghhiijjkkllmmnnooppqq"})
	if err == nil || !strings.Contains(connectors.Say(err), "does not know") {
		t.Fatalf("an unknown token: %v", err)
	}
	if list, _ := s.List(); len(list) != 0 {
		t.Fatalf("kept after a failed check: %+v", list)
	}

	// The bot of the machine's notifications cannot be the connector's too: a refusal in words, nothing kept.
	telegram.SetHooks(telegram.Hooks{Taken: func(id int64) bool { return id == 1 }})
	_, err = s.Connect(context.Background(), telegram.ID, "", map[string]string{"token": goodToken})
	if err == nil || !strings.Contains(connectors.Say(err), "already used for notifications") {
		t.Fatalf("a bot of the notifications: %v", err)
	}
	if list, _ := s.List(); len(list) != 0 {
		t.Fatalf("kept a bot of the notifications: %+v", list)
	}
}

func TestBindingAndOnlyTheOwnersChat(t *testing.T) {
	w := newWorld(t)
	s, h, _ := w.start(t)
	conn := bind(t, w, s, map[string]string{"inbound": "chat", "chat": "conv1"})
	if n := len(w.tg.sentTo(stranger)); n != 0 {
		t.Fatalf("the bot wrote to a stranger: %d", n)
	}

	// A stranger, even with a /start, is ignored; the owner's message is an event and goes into the chat.
	w.tg.message(stranger, stranger, "private", "/start anything")
	w.tg.message(stranger, stranger, "private", "do something")
	w.tg.message(owner, owner, "private", "remind me about the contract")
	eventually(t, "the owner's event", func() bool { return len(h.events()) == 1 })
	time.Sleep(100 * time.Millisecond)
	evs := h.events()
	if len(evs) != 1 || evs[0].Type != telegram.EventMessage || !evs[0].Mine || evs[0].Connection != conn.ID {
		t.Fatalf("events: %+v", evs)
	}
	raw, _ := json.Marshal(evs[0])
	if strings.Contains(string(raw), "contract") {
		t.Fatalf("the envelope carries the text: %s", raw)
	}
	got := w.chats.all()
	if len(got) != 1 || got[0] != "conv1|[Telegram] remind me about the contract" {
		t.Fatalf("into the chat: %q", got)
	}
	if n := len(w.tg.sentTo(stranger)); n != 0 {
		t.Fatalf("the bot answered a stranger: %d", n)
	}

	// The agent reads the text on the machine, marked as data.
	text, err := s.Read(context.Background(), conn.ID, "message", map[string]string{"id": strings.TrimPrefix(evs[0].Ref, "message id=")})
	if err != nil || !strings.Contains(text, "remind me about the contract") || !strings.Contains(text, "untrusted-data") {
		t.Fatalf("read: %q %v", text, err)
	}
	all, _ := s.Read(context.Background(), conn.ID, "messages", nil)
	if strings.Contains(all, "do something") {
		t.Fatalf("a stranger's message was kept: %q", all)
	}
}

func TestGroupAddedByAStrangerIsLeft(t *testing.T) {
	w := newWorld(t)
	s, h, _ := w.start(t)
	conn := bind(t, w, s, map[string]string{})
	added := func(chat, by int64) {
		w.tg.push(map[string]any{"my_chat_member": map[string]any{"chat": map[string]any{"id": chat, "type": "supergroup", "title": "G"},
			"from": map[string]any{"id": by}, "new_chat_member": map[string]any{"status": "member"}}})
	}
	added(-100, stranger)
	added(-200, owner)
	w.tg.message(-100, stranger, "supergroup", "@my_bot hi")
	w.tg.message(-200, stranger, "supergroup", "@my_bot what about friday")
	eventually(t, "the group's event", func() bool { return len(h.events()) == 1 })
	eventually(t, "the bot leaves the stranger's group", func() bool {
		w.tg.mu.Lock()
		defer w.tg.mu.Unlock()
		return len(w.tg.leaves) == 1 && w.tg.leaves[0]["chat_id"].(float64) == -100
	})
	if e := h.events()[0]; e.Mine || e.Facts["chat"] != "group" {
		t.Fatalf("group event: %+v", e)
	}
	// Writing to the group waits for the person's yes.
	p, err := s.Propose(conn.ID, "send", map[string]string{"chat": "-200", "text": "Friday works"}, "", connectors.By{Conv: "c1"})
	if err != nil || p.State != connectors.Waiting {
		t.Fatalf("send to a group: %+v %v", p, err)
	}
	if n := len(w.tg.sentTo(-200)); n != 0 {
		t.Fatalf("sent before the yes: %d", n)
	}
	if _, err := s.Answer(context.Background(), p.ID, true); err != nil {
		t.Fatal(err)
	}
	if n := len(w.tg.sentTo(-200)); n != 1 {
		t.Fatalf("sent after the yes: %d", n)
	}
}

// outward is a connector with an action outward, to be confirmed in Telegram.
type outward struct{}

var (
	actsMu sync.Mutex
	acts   []map[string]string
)

func (outward) Spec() connectors.Spec {
	return connectors.Spec{ID: "tg-test-mail", Kind: connectors.Mail, Title: connectors.T("Mail", "Почта"),
		Actions: []connectors.Action{{Name: "send", Summary: "send", Title: connectors.T("Send the letter", "Отправить письмо"),
			Args: []connectors.Arg{{Name: "to", Required: true}, {Name: "subject"}, {Name: "text", Required: true}}}}}
}
func (outward) Check(context.Context, connectors.Instance) (connectors.Account, error) {
	return connectors.Account{Label: "me@example.com"}, nil
}
func (outward) Act(_ context.Context, _ connectors.Instance, _ string, args map[string]string) (connectors.Result, error) {
	actsMu.Lock()
	defer actsMu.Unlock()
	acts = append(acts, args)
	return connectors.Result{Text: "sent"}, nil
}

func init() { connectors.Register(outward{}) }

func TestApprovalInTelegramRunsOnce(t *testing.T) {
	w := newWorld(t)
	s, _, _ := w.start(t)
	bind(t, w, s, map[string]string{})
	mail, err := s.Connect(context.Background(), "tg-test-mail", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	actsMu.Lock()
	acts = nil
	actsMu.Unlock()
	p, err := s.Propose(mail.ID, "send", map[string]string{"to": "ann@example.com", "subject": "Contract", "text": "the secret body of the letter"},
		"Ann asked", connectors.By{Conv: "c7"})
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the card in Telegram", func() bool { return len(w.tg.sentTo(owner)) == 2 })
	card := w.tg.sentTo(owner)[1]
	text := card["text"].(string)
	if !strings.Contains(text, "ann@example.com") || !strings.Contains(text, "Contract") || strings.Contains(text, "secret body") {
		t.Fatalf("the short card: %q", text)
	}
	var yes string
	for _, row := range card["reply_markup"].(map[string]any)["inline_keyboard"].([]any) {
		for _, b := range row.([]any) {
			if d, _ := b.(map[string]any)["callback_data"].(string); strings.HasSuffix(d, ":y") {
				yes = d
			}
		}
	}
	if yes == "" {
		t.Fatalf("no yes button: %v", card["reply_markup"])
	}
	if raw, _ := json.Marshal(card["reply_markup"]); !strings.Contains(string(raw), "agent%3Am1%3Ac7") {
		t.Fatalf("no button to the chat that asked: %s", raw)
	}

	// A stranger's press, and the owner's press from another chat, do nothing; the owner's press runs it once.
	w.tg.press(stranger, stranger, yes)
	w.tg.press(owner, stranger, yes)
	w.tg.press(owner, owner, yes)
	w.tg.press(owner, owner, yes)
	eventually(t, "the action", func() bool {
		got, _ := s.Wait(context.Background(), p.ID, 0)
		return got.State == connectors.Done
	})
	eventually(t, "every press answered", func() bool {
		w.tg.mu.Lock()
		defer w.tg.mu.Unlock()
		return len(w.tg.acks) == 4
	})
	actsMu.Lock()
	n := len(acts)
	actsMu.Unlock()
	if n != 1 {
		t.Fatalf("the action ran %d times", n)
	}
	sends, _ := s.Journal(10)
	if len(sends) != 1 || !strings.HasPrefix(sends[0].ApprovedBy, "relay:") {
		t.Fatalf("journal: %+v", sends)
	}

	// A push to the owner needs no card; a press of its button comes back to the chat that wrote it.
	push, err := s.Propose(bindingID(t, s), "notify", map[string]string{"text": "Ann wrote 3 days ago. Answer with a draft?", "buttons": "Yes|Later"},
		"", connectors.By{Conv: "c9"})
	if err != nil || push.State != connectors.Done {
		t.Fatalf("notify: %+v %v", push, err)
	}
	msg := w.tg.sentTo(owner)[2]
	first := msg["reply_markup"].(map[string]any)["inline_keyboard"].([]any)[0].([]any)[0].(map[string]any)["callback_data"].(string)
	w.tg.press(owner, owner, first)
	w.tg.press(owner, owner, first)
	eventually(t, "the press in the chat", func() bool { return len(w.chats.all()) == 1 })
	time.Sleep(100 * time.Millisecond)
	if got := w.chats.all(); len(got) != 1 || !strings.HasPrefix(got[0], "c9|") || !strings.Contains(got[0], "Yes") {
		t.Fatalf("press into the chat: %q", got)
	}
}

func bindingID(t *testing.T, s *connectors.Service) string {
	t.Helper()
	list, _ := s.List()
	for _, c := range list {
		if c.Connector == telegram.ID {
			return c.ID
		}
	}
	t.Fatal("no telegram connection")
	return ""
}

func TestRestartDoublesNothing(t *testing.T) {
	w := newWorld(t)
	s, h, stop := w.start(t)
	bind(t, w, s, map[string]string{"inbound": "chat", "chat": "conv1"})
	w.tg.message(owner, owner, "private", "first")
	eventually(t, "the event", func() bool { return len(h.events()) == 1 })
	eventually(t, "the chat", func() bool { return len(w.chats.all()) == 1 })
	stop()
	time.Sleep(100 * time.Millisecond)

	// The server forgot the confirmations: every update comes again, the binding's /start too.
	w.tg.mu.Lock()
	w.tg.replay = true
	w.tg.mu.Unlock()
	_, h2, _ := w.start(t)
	time.Sleep(400 * time.Millisecond)
	w.tg.mu.Lock()
	w.tg.replay = false
	w.tg.mu.Unlock()
	w.tg.message(owner, owner, "private", "second")
	eventually(t, "the new event", func() bool { return len(h2.events()) == 1 })
	time.Sleep(100 * time.Millisecond)
	if evs := h2.events(); len(evs) != 1 {
		t.Fatalf("after the restart: %d events", len(evs))
	}
	if got := w.chats.all(); len(got) != 2 || got[1] != "conv1|[Telegram] second" {
		t.Fatalf("into the chat after the restart: %q", got)
	}
	if n := len(w.tg.sentTo(owner)); n != 1 {
		t.Fatalf("the confirmation was sent %d times", n)
	}
}
