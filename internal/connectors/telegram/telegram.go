// Package telegram is the connector «Telegram bot» (docs/connectors.md): the person's own bot, made with BotFather,
// as a channel between harnessly and them. Only the official Bot API with the bot's token: nothing here reads the
// person's own chats through their account.
//
//   - Connect: the token (kept in the safe), checked with getMe. Then the page shows a link t.me/<bot>?start=<code>
//     with a one-time code; the person writes to the bot and the connection binds their private chat. From then on the
//     bot reads and answers only that chat (and the groups the person added it to); everyone else is ignored.
//   - Source: a long poll of getUpdates (a Watcher, no webhook to the outside), the cursor is the update offset. A
//     message of the person to the bot, and a message in a group that mentions or answers the bot (privacy mode stays
//     on), is a chat.message event without its text; the text is read on the machine (`message`, `messages`).
//   - Reads: the last messages the bot got, the groups it is in.
//   - Actions: `notify` writes to the person's own chat (Direct: no card, limited per hour, without a sound in the
//     person's quiet hours), with buttons whose press comes back as an event and into the chat that wrote; `send`
//     writes to a group, after the person's yes on the card.
//   - Relay: every proposed action outward of any connection is shown in the bound chat too, with «Send / Don't send»:
//     the same args as the card, a one-time button, the proposal's own time to live, the bound chat only. The text of
//     a letter is shown there only when the person chose so on connect; by default who and the subject.
package telegram

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/connectors"
	"github.com/ly-solutions/harnessly/internal/notifybots"
)

// ID names the connector.
const ID = "telegram-bot"

// EventMessage is the one event: a message to the bot, a button pressed under its message.
const EventMessage = "chat.message"

const (
	// codeFor is how long a pairing code works.
	codeFor = 10 * time.Minute
	// recentMax messages the bot got are kept on the machine for the reads.
	recentMax = 200
	// notifyPerHour is how many pushes to the person one connection makes in an hour.
	notifyPerHour = 12
	notifyMax     = 3500
	buttonsMax    = 4
	buttonMax     = 32
	// pollWait is under the runtime client's 15 s wait for an answer's headers.
	pollWait = 10 * time.Second
	// shortMax is how much of an argument the short card shows.
	shortMax = 200
	fullMax  = 3000
	textMax  = 3800
)

// API is the Bot API's address; tests put a fake server here.
var API = notifybots.DefaultAPI

// pause is the wait after a failed poll.
var pause = 3 * time.Second

// Hooks are what the runtime gives the connector beyond the contract. Every field may be nil.
type Hooks struct {
	// ToChat puts the person's text into a chat of harnessly, as their own message.
	ToChat func(ctx context.Context, conv, text string) error
	// Quiet says whether t is in the person's quiet hours: a push then comes without a sound.
	Quiet func(t time.Time) bool
	// Open is a link that opens the chat conv in harnessly, "" when none can be made (then no button is shown).
	Open func(conv string) string
	// Taken says whether the bot of this Telegram id already serves this machine's notifications or asks: Telegram
	// gives one bot's updates to one reader, so the same bot cannot be both.
	Taken func(botID int64) bool
	// Russian says the person reads Russian.
	Russian func() bool
}

var hooks atomic.Pointer[Hooks]

// SetHooks gives the connector the runtime's hooks.
func SetHooks(h Hooks) { hooks.Store(&h) }

func hooked() Hooks {
	if h := hooks.Load(); h != nil {
		return *h
	}
	return Hooks{}
}

func word(t connectors.Text) string {
	if h := hooked(); h.Russian != nil && h.Russian() && t.Ru != "" {
		return t.Ru
	}
	return t.En
}

type botConn struct{}

func init() { connectors.Register(botConn{}) }

// Inbound choices: what a message of the person to the bot becomes besides the event.
const (
	inboundEvent = "event"
	inboundChat  = "chat"
)

func (botConn) Spec() connectors.Spec {
	return connectors.Spec{
		ID:    ID,
		Kind:  connectors.Chat,
		Title: connectors.T("Telegram bot", "Telegram-бот"),
		Summary: connectors.T("Your own bot from @BotFather: harnessly writes to you there, you answer and confirm sends",
			"Ваш бот от @BotFather: harnessly пишет вам туда, вы отвечаете и подтверждаете отправки"),
		Fields: []connectors.Field{
			{ID: "token", Type: connectors.FieldPassword, Secret: true, Label: connectors.T("Bot token", "Токен бота"),
				Hint: connectors.T("@BotFather → /newbot gives it. Use a bot of its own, not the one of notifications",
					"Его выдаёт @BotFather → /newbot. Нужен отдельный бот, не тот, что для уведомлений"),
				Placeholder: "123456789:AA…"},
			{ID: "inbound", Type: connectors.FieldChoice, Default: inboundEvent,
				Label: connectors.T("Your messages to the bot", "Ваши сообщения боту"),
				Hint: connectors.T("An event goes to the hub's rules either way: a rule can start a new agent chat",
					"Событие уходит в правила хаба в любом случае: правило может начать новый чат агента"),
				Choices: []connectors.Choice{
					{Value: inboundEvent, Label: connectors.T("Only an event", "Только событие")},
					{Value: inboundChat, Label: connectors.T("Into a chat of harnessly", "В чат harnessly")},
				}},
			{ID: "chat", Type: connectors.FieldText, Optional: true, Label: connectors.T("Chat id in harnessly", "Id чата в harnessly"),
				Hint: connectors.T("For «into a chat». A reply to a bot's message always goes to the chat that wrote it",
					"Для «в чат harnessly». Ответ на сообщение бота всегда уходит в тот чат, что его написал")},
			{ID: "letters", Type: connectors.FieldChoice, Default: "short",
				Label: connectors.T("Sends to confirm in Telegram", "Отправки на подтверждение в Telegram"),
				Choices: []connectors.Choice{
					{Value: "short", Label: connectors.T("To whom and the subject", "Кому и тема")},
					{Value: "full", Label: connectors.T("The whole text", "Весь текст")},
				}},
		},
		Events: []string{EventMessage},
		Tools: []connectors.Tool{
			{Name: "messages", Summary: "the last messages the bot got: yours to it, and those of groups that mention or answer it",
				Args: []connectors.Arg{{Name: "chat", Summary: "\"me\", or a group's id or title; all when empty"},
					{Name: "limit", Summary: "how many, 20 by default"}}},
			{Name: "message", Summary: "one message the bot got, by the id of its event",
				Args: []connectors.Arg{{Name: "id", Required: true}}},
			{Name: "groups", Summary: "the groups you added the bot to"},
		},
		Actions: []connectors.Action{
			{Name: "notify", Direct: true, Summary: "write to the person in their own Telegram chat, at once and without a card; " +
				"buttons=\"Yes|Later\" adds buttons whose press comes back to this chat",
				Title: connectors.T("Write to me in Telegram", "Написать мне в Telegram"),
				Args: []connectors.Arg{{Name: "text", Required: true, Summary: "short: what happened and what you need"},
					{Name: "buttons", Summary: "up to 4 answers split by |"}}},
			{Name: "send", Summary: "write to a group the bot is in, after the person's yes",
				Title: connectors.T("Write to the Telegram group", "Написать в группу Telegram"),
				Args: []connectors.Arg{{Name: "chat", Required: true, Summary: "the group's id or title (read groups)"},
					{Name: "text", Required: true}}},
		},
	}
}

// ---- the connection's record -----------------------------------------------------------------------------------

type group struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

type message struct {
	ID        string    `json:"id"`
	Chat      int64     `json:"chat"`
	ChatTitle string    `json:"chatTitle,omitempty"`
	From      string    `json:"from"`
	Mine      bool      `json:"mine,omitempty"`
	Forwarded string    `json:"forwarded,omitempty"`
	Text      string    `json:"text"`
	At        time.Time `json:"at"`
}

// offer is a proposed action shown in the bound chat, by its one-time button code.
type offer struct {
	Proposal string    `json:"proposal"`
	Message  int64     `json:"message"`
	Title    string    `json:"title"`
	Until    time.Time `json:"until"`
}

// notice is a push with buttons, by its button code.
type notice struct {
	Conv    string    `json:"conv,omitempty"`
	HTML    string    `json:"html"`
	Buttons []string  `json:"buttons"`
	Message int64     `json:"message"`
	At      time.Time `json:"at"`
	Pressed string    `json:"pressed,omitempty"`
}

type memo struct {
	Username  string    `json:"username,omitempty"`
	Code      string    `json:"code,omitempty"`
	CodeUntil time.Time `json:"codeUntil,omitzero"`
	// Chat and User are the person's bound private chat and their Telegram id.
	Chat    int64              `json:"chat,omitempty"`
	User    int64              `json:"user,omitempty"`
	Who     string             `json:"who,omitempty"`
	Groups  []group            `json:"groups,omitempty"`
	Recent  []message          `json:"recent,omitempty"`
	Offers  map[string]offer   `json:"offers,omitempty"`
	Notices map[string]*notice `json:"notices,omitempty"`
	// Replies maps a bot's message id in the bound chat to the harnessly chat that wrote it.
	Replies map[string]string `json:"replies,omitempty"`
	// Handled is the last update whose effects (a binding, a press, a message into a chat) are done.
	Handled int64       `json:"handled,omitempty"`
	Pushes  []time.Time `json:"pushes,omitempty"`
}

var (
	locksMu sync.Mutex
	locks   = map[string]*sync.Mutex{}
)

// lock serializes the record of one connection between its source, its reads, its actions and the relay.
func lock(id string) func() {
	locksMu.Lock()
	m := locks[id]
	if m == nil {
		m = &sync.Mutex{}
		locks[id] = m
	}
	locksMu.Unlock()
	m.Lock()
	return m.Unlock
}

func load(in connectors.Instance) (memo, error) {
	var m memo
	if in.Load == nil {
		return m, nil
	}
	err := in.Load(&m)
	return m, err
}

func keep(in connectors.Instance, m memo) error {
	if in.Store == nil {
		return nil
	}
	// The record stays small: the oldest go first.
	if len(m.Recent) > recentMax {
		m.Recent = m.Recent[len(m.Recent)-recentMax:]
	}
	now := time.Now()
	for k, o := range m.Offers {
		if now.After(o.Until.Add(time.Hour)) {
			delete(m.Offers, k)
		}
	}
	for k, n := range m.Notices {
		if now.Sub(n.At) > 7*24*time.Hour {
			delete(m.Notices, k)
		}
	}
	if len(m.Replies) > 500 {
		m.Replies = map[string]string{}
	}
	return in.Store(m)
}

func client(in connectors.Instance) *notifybots.Client {
	return &notifybots.Client{Base: API, HTTP: in.HTTP}
}

func token(in connectors.Instance) (string, error) {
	t, err := in.Secret("token")
	if err != nil {
		return "", err
	}
	return notifybots.CleanToken(t), nil
}

func code() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

var tokenRe = regexp.MustCompile(`^\d{5,16}:[A-Za-z0-9_-]{30,64}$`)

// said turns a Telegram failure into words for the person.
func said(err error) error {
	switch notifybots.ReasonOf(err) {
	case notifybots.ReasonInvalidToken:
		return connectors.Sayf("Telegram does not know this token: copy it again from @BotFather")
	case notifybots.ReasonWebhook:
		return connectors.Sayf("The bot has a webhook: remove it (deleteWebhook) or use another bot")
	case notifybots.ReasonUnreachable:
		return connectors.Sayf("Telegram does not answer from this machine")
	case notifybots.ReasonChatGone:
		return connectors.Sayf("The bot cannot write there: it was blocked or removed from the chat")
	case notifybots.ReasonRateLimited:
		return connectors.Sayf("Telegram asked to wait: try again later")
	}
	return err
}

// ---- connect -----------------------------------------------------------------------------------------------------

func (botConn) Check(ctx context.Context, in connectors.Instance) (connectors.Account, error) {
	tok, err := token(in)
	if err != nil {
		return connectors.Account{}, err
	}
	if !tokenRe.MatchString(tok) {
		return connectors.Account{}, connectors.Sayf("This is not a bot token: it looks like 123456789:AA…")
	}
	if in.Config["inbound"] == inboundChat && strings.TrimSpace(in.Config["chat"]) == "" {
		return connectors.Account{}, connectors.Sayf("Name the chat of harnessly the messages go to")
	}
	me, err := client(in).GetMe(ctx, tok)
	if err != nil {
		return connectors.Account{}, said(err)
	}
	if !me.IsBot || me.Username == "" {
		return connectors.Account{}, connectors.Sayf("This token is not a bot's")
	}
	if h := hooked(); h.Taken != nil && h.Taken(me.ID) {
		return connectors.Account{}, connectors.Sayf("%s", word(connectors.T(
			"This bot is already used for notifications: make a separate one in @BotFather",
			"Этот бот уже используется для уведомлений — заведите отдельного в @BotFather")))
	}
	unlock := lock(in.ID)
	defer unlock()
	m, err := load(in)
	if err != nil {
		return connectors.Account{}, err
	}
	if m.Username != me.Username {
		m.Username = me.Username
		if err := keep(in, m); err != nil {
			return connectors.Account{}, err
		}
	}
	return connectors.Account{Label: "@" + me.Username}, nil
}

// Next is the pairing link while no chat is bound: a fresh one-time code when the last one ran out.
func (botConn) Next(_ context.Context, in connectors.Instance) (connectors.Step, bool) {
	unlock := lock(in.ID)
	defer unlock()
	m, err := load(in)
	if err != nil || m.Chat != 0 || m.Username == "" {
		return connectors.Step{}, false
	}
	if m.Code == "" || time.Now().After(m.CodeUntil) {
		m.Code, m.CodeUntil = code(), time.Now().Add(codeFor)
		if keep(in, m) != nil {
			return connectors.Step{}, false
		}
	}
	return connectors.Step{
		Text: connectors.T("Open the link in Telegram, press Start, then «Check» here: the bot will write to you only",
			"Откройте ссылку в Telegram, нажмите «Старт», затем «Проверить» здесь — бот будет писать только вам"),
		URL: "https://t.me/" + m.Username + "?start=" + m.Code,
	}, true
}

// ---- the source --------------------------------------------------------------------------------------------------

type cursor struct {
	Offset int64 `json:"offset"`
}

type user struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

func (u *user) name() string {
	if u == nil {
		return ""
	}
	n := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if u.Username != "" {
		if n == "" {
			return "@" + u.Username
		}
		n += " (@" + u.Username + ")"
	}
	return clip(n, 80)
}

type chat struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

type tgMessage struct {
	MessageID int64  `json:"message_id"`
	Date      int64  `json:"date"`
	Text      string `json:"text"`
	Caption   string `json:"caption"`
	From      *user  `json:"from"`
	Chat      chat   `json:"chat"`
	// ForwardOrigin is set on a message forwarded from someone else.
	ForwardOrigin *struct {
		Type       string `json:"type"`
		SenderUser *user  `json:"sender_user"`
		SenderName string `json:"sender_user_name"`
		Chat       *chat  `json:"chat"`
	} `json:"forward_origin"`
	ReplyTo *struct {
		MessageID int64 `json:"message_id"`
		From      *user `json:"from"`
	} `json:"reply_to_message"`
	MigrateTo int64 `json:"migrate_to_chat_id"`
}

type callback struct {
	ID      string     `json:"id"`
	From    user       `json:"from"`
	Data    string     `json:"data"`
	Message *tgMessage `json:"message"`
}

type memberUpdate struct {
	Chat          chat `json:"chat"`
	From          user `json:"from"`
	NewChatMember struct {
		Status string `json:"status"`
	} `json:"new_chat_member"`
}

type update struct {
	UpdateID     int64         `json:"update_id"`
	Message      *tgMessage    `json:"message"`
	Callback     *callback     `json:"callback_query"`
	MyChatMember *memberUpdate `json:"my_chat_member"`
}

// Watch long-polls the bot's updates. Each update's effects (a binding, a press, a message into a chat) are done once:
// the record keeps the last handled update, so an update read again after a crash only gives its event again, which
// the runtime drops as already delivered.
func (b botConn) Watch(ctx context.Context, in connectors.Instance, from connectors.Cursor, emit connectors.Emit) error {
	var cur cursor
	if from != "" {
		_ = json.Unmarshal([]byte(from), &cur)
	}
	tg := client(in)
	for ctx.Err() == nil {
		tok, err := token(in)
		if err != nil {
			return err
		}
		var updates []update
		err = tg.Call(ctx, tok, "getUpdates", map[string]any{"offset": cur.Offset, "timeout": int(pollWait / time.Second),
			"allowed_updates": []string{"message", "callback_query", "my_chat_member"}}, &updates)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			switch notifybots.ReasonOf(err) {
			case notifybots.ReasonInvalidToken, notifybots.ReasonWebhook:
				return said(err)
			}
			// A network hiccup, Telegram's 429, another reader of the same bot: later.
			if !sleep(ctx, pause) {
				return nil
			}
			continue
		}
		if len(updates) == 0 {
			continue
		}
		var events []connectors.Event
		next := cur
		for _, u := range updates {
			if u.UpdateID >= next.Offset {
				next.Offset = u.UpdateID + 1
			}
			events = append(events, b.handle(ctx, in, tok, u)...)
		}
		raw, _ := json.Marshal(next)
		if err := emit(events, connectors.Cursor(raw)); err != nil {
			return err
		}
		cur = next
	}
	return nil
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// handle is one update: its event, if it is one, and its effects, once.
func (b botConn) handle(ctx context.Context, in connectors.Instance, tok string, u update) []connectors.Event {
	unlock := lock(in.ID)
	m, err := load(in)
	if err != nil {
		unlock()
		return nil
	}
	fresh := u.UpdateID > m.Handled
	var events []connectors.Event
	var after func()
	switch {
	case u.MyChatMember != nil:
		if fresh {
			after = b.member(ctx, in, tok, &m, u.MyChatMember)
		}
	case u.Callback != nil:
		var ev *connectors.Event
		ev, after = b.press(ctx, in, tok, &m, u.Callback, fresh)
		if ev != nil {
			events = append(events, *ev)
		}
	case u.Message != nil:
		var ev *connectors.Event
		ev, after = b.message(ctx, in, tok, &m, u.Message, fresh)
		if ev != nil {
			events = append(events, *ev)
		}
	}
	if fresh {
		m.Handled = u.UpdateID
		_ = keep(in, m)
	}
	unlock()
	// What talks to the runtime (the answer of a proposal runs an action, which may take this lock) goes after it.
	if after != nil {
		after()
	}
	return events
}

func (botConn) member(ctx context.Context, in connectors.Instance, tok string, m *memo, mu *memberUpdate) func() {
	if mu.Chat.Type != "group" && mu.Chat.Type != "supergroup" {
		return nil
	}
	m.Groups = slices.DeleteFunc(m.Groups, func(g group) bool { return g.ID == mu.Chat.ID })
	switch mu.NewChatMember.Status {
	case "member", "administrator":
		if m.User == 0 || mu.From.ID != m.User {
			// Added by someone who is not the person: the bot does not stay.
			id := mu.Chat.ID
			return func() { _ = client(in).Call(ctx, tok, "leaveChat", map[string]any{"chat_id": id}, nil) }
		}
		m.Groups = append(m.Groups, group{ID: mu.Chat.ID, Title: clip(oneLine(mu.Chat.Title), 64)})
	}
	return nil
}

// press is a button under a bot's message, from the person in their bound chat only.
func (botConn) press(ctx context.Context, in connectors.Instance, tok string, m *memo, q *callback, fresh bool) (*connectors.Event, func()) {
	tg := client(in)
	ack := func(text string) func() {
		return func() { _ = tg.AnswerCallback(ctx, tok, q.ID, text) }
	}
	if m.User == 0 || q.From.ID != m.User || q.Message == nil || q.Message.Chat.ID != m.Chat {
		return nil, ack("")
	}
	kind, rest, _ := strings.Cut(q.Data, ":")
	nonce, choice, _ := strings.Cut(rest, ":")
	switch kind {
	case "a":
		o, ok := m.Offers[nonce]
		if !ok || !fresh {
			return nil, ack(word(connectors.T("Already decided", "Уже решено")))
		}
		// One press only: the code is gone before the answer runs.
		delete(m.Offers, nonce)
		approve := choice == "y"
		chatID, msgID := m.Chat, o.Message
		return nil, func() {
			if in.Answer == nil {
				_ = tg.AnswerCallback(ctx, tok, q.ID, "")
				return
			}
			p, err := in.Answer(ctx, o.Proposal, approve)
			if err != nil {
				_ = tg.AnswerCallback(ctx, tok, q.ID, word(connectors.T("Already decided or out of time", "Уже решено или срок вышел")))
				_ = tg.EditHTML(ctx, tok, chatID, msgID, settled(o.Title, connectors.Expired))
				return
			}
			_ = tg.AnswerCallback(ctx, tok, q.ID, "")
			_ = tg.EditHTML(ctx, tok, chatID, msgID, settled(o.Title, p.State))
		}
	case "n":
		n := m.Notices[nonce]
		i, err := strconv.Atoi(choice)
		if n == nil || err != nil || i < 0 || i >= len(n.Buttons) {
			return nil, ack("")
		}
		label := n.Buttons[i]
		ev := &connectors.Event{Type: EventMessage, ID: "press:" + nonce, At: time.Now(), Actor: m.Who, Mine: true,
			Thread: thread(m.Chat), Ref: "message id=press:" + nonce,
			Facts: map[string]string{"chat": "private", "button": label, "notice": nonce}}
		if !fresh || n.Pressed != "" {
			return ev, ack("")
		}
		n.Pressed = label
		m.Recent = append(m.Recent, message{ID: "press:" + nonce, Chat: m.Chat, From: m.Who, Mine: true,
			Text: "[pressed «" + label + "»]", At: time.Now().UTC()})
		conv, chatID, msgID, body := n.Conv, m.Chat, n.Message, n.HTML
		return ev, func() {
			_ = tg.AnswerCallback(ctx, tok, q.ID, "")
			_ = tg.EditHTML(ctx, tok, chatID, msgID, body+"\n\n→ <b>"+html.EscapeString(label)+"</b>")
			if h := hooked(); h.ToChat != nil && conv != "" {
				_ = h.ToChat(ctx, conv, "[Telegram] I pressed «"+label+"» under your message there.")
			}
		}
	}
	return nil, ack("")
}

func thread(chat int64) string { return "tg:" + strconv.FormatInt(chat, 10) }

// message is a message to the bot: the person's in their bound chat, a binding with the code, or one in a group.
func (b botConn) message(ctx context.Context, in connectors.Instance, tok string, m *memo, msg *tgMessage, fresh bool) (*connectors.Event, func()) {
	text := msg.Text
	if text == "" {
		text = msg.Caption
	}
	at := time.Unix(msg.Date, 0).UTC()
	id := strconv.FormatInt(msg.Chat.ID, 10) + ":" + strconv.FormatInt(msg.MessageID, 10)
	tg := client(in)

	// The binding: a private chat sends the live code.
	if m.Chat == 0 {
		if msg.Chat.Type != "private" || msg.From == nil || m.Code == "" || time.Now().After(m.CodeUntil) ||
			!notifybots.HasCode(text, m.Code) || !fresh {
			return nil, nil
		}
		m.Chat, m.User, m.Who, m.Code = msg.Chat.ID, msg.From.ID, msg.From.name(), ""
		chatID := m.Chat
		return nil, func() {
			_, _ = tg.SendHTML(ctx, tok, chatID, html.EscapeString(word(connectors.T(
				"Done, bound to harnessly: it will write to you here, and you can answer it here.",
				"Готово, привязано к harnessly: он будет писать вам сюда, и здесь же можно ему отвечать."))), nil, false)
		}
	}

	if msg.Chat.ID == m.Chat {
		if msg.From == nil || msg.From.ID != m.User || strings.HasPrefix(text, "/start") || text == "" {
			return nil, nil
		}
		rec := message{ID: id, Chat: msg.Chat.ID, From: m.Who, Mine: true, Text: text, At: at}
		facts := map[string]string{"chat": "private"}
		if o := msg.ForwardOrigin; o != nil {
			rec.Mine = false
			rec.Forwarded = forwardedFrom(o.SenderUser, o.SenderName, o.Chat)
			facts["forwarded"] = "true"
		}
		var conv string
		if msg.ReplyTo != nil {
			facts["reply"] = "true"
			conv = m.Replies[strconv.FormatInt(msg.ReplyTo.MessageID, 10)]
		}
		if conv == "" && in.Config["inbound"] == inboundChat {
			conv = strings.TrimSpace(in.Config["chat"])
		}
		ev := &connectors.Event{Type: EventMessage, ID: id, At: at, Actor: m.Who, Thread: thread(msg.Chat.ID),
			Ref: "message id=" + id, Mine: rec.Mine, Facts: facts}
		if !fresh {
			return ev, nil
		}
		m.Recent = append(m.Recent, rec)
		h := hooked()
		if conv == "" || h.ToChat == nil {
			return ev, nil
		}
		// The person's own words come as theirs; what they forwarded from someone else is data.
		body := "[Telegram] " + text
		if rec.Forwarded != "" {
			body = "[Telegram] I forwarded a message:\n" + connectors.Untrusted("telegram: forwarded from "+rec.Forwarded, text)
		}
		return ev, func() { _ = h.ToChat(ctx, conv, body) }
	}

	gi := slices.IndexFunc(m.Groups, func(g group) bool { return g.ID == msg.Chat.ID })
	if gi < 0 {
		// Not the person's chat, not a group they added the bot to: ignored.
		return nil, nil
	}
	if msg.MigrateTo != 0 {
		if fresh {
			m.Groups[gi].ID = msg.MigrateTo
		}
		return nil, nil
	}
	if text == "" {
		return nil, nil
	}
	from := msg.From.name()
	mine := msg.From != nil && msg.From.ID == m.User
	ev := &connectors.Event{Type: EventMessage, ID: id, At: at, Actor: from, Thread: thread(msg.Chat.ID),
		Ref: "message id=" + id, Mine: mine, Title: m.Groups[gi].Title,
		Facts: map[string]string{"chat": "group", "group": strconv.FormatInt(msg.Chat.ID, 10)}}
	if fresh {
		m.Recent = append(m.Recent, message{ID: id, Chat: msg.Chat.ID, ChatTitle: m.Groups[gi].Title, From: from, Mine: mine, Text: text, At: at})
	}
	return ev, nil
}

func forwardedFrom(u *user, name string, c *chat) string {
	switch {
	case u != nil:
		return u.name()
	case name != "":
		return clip(oneLine(name), 80)
	case c != nil:
		return clip(oneLine(c.Title), 80)
	}
	return "someone"
}

// ---- reads -------------------------------------------------------------------------------------------------------

func (botConn) Read(_ context.Context, in connectors.Instance, tool string, args map[string]string) (connectors.Result, error) {
	unlock := lock(in.ID)
	m, err := load(in)
	unlock()
	if err != nil {
		return connectors.Result{}, err
	}
	switch tool {
	case "groups":
		if len(m.Groups) == 0 {
			return connectors.Result{Text: "The bot is in no group you added it to."}, nil
		}
		var b strings.Builder
		for _, g := range m.Groups {
			fmt.Fprintf(&b, "%d %s\n", g.ID, g.Title)
		}
		return connectors.Result{Text: fmt.Sprintf("%d groups (id, title)", len(m.Groups)), Data: b.String(), From: "telegram: group titles"}, nil
	case "message":
		id := strings.TrimSpace(args["id"])
		for i := len(m.Recent) - 1; i >= 0; i-- {
			if m.Recent[i].ID == id {
				return connectors.Result{Text: "1 message", Data: line(m.Recent[i]), From: "telegram: " + source(m.Recent[i])}, nil
			}
		}
		return connectors.Result{}, connectors.ErrNotFound
	case "messages":
		limit := 20
		if n, err := strconv.Atoi(args["limit"]); err == nil && n > 0 {
			limit = min(n, recentMax)
		}
		want := strings.TrimSpace(args["chat"])
		var picked []message
		for i := len(m.Recent) - 1; i >= 0 && len(picked) < limit; i-- {
			r := m.Recent[i]
			switch {
			case want == "":
			case want == "me" && r.Chat == m.Chat:
			case want == strconv.FormatInt(r.Chat, 10) || (r.ChatTitle != "" && strings.EqualFold(want, r.ChatTitle)):
			default:
				continue
			}
			picked = append(picked, r)
		}
		if len(picked) == 0 {
			return connectors.Result{Text: "No messages."}, nil
		}
		slices.Reverse(picked)
		var b strings.Builder
		for _, r := range picked {
			b.WriteString(line(r) + "\n")
		}
		return connectors.Result{Text: fmt.Sprintf("%d messages, the oldest first", len(picked)), Data: b.String(), From: "telegram"}, nil
	}
	return connectors.Result{}, connectors.ErrUnknownTool
}

func source(r message) string {
	if r.ChatTitle != "" {
		return "group " + r.ChatTitle
	}
	if r.Forwarded != "" {
		return "forwarded from " + r.Forwarded
	}
	return "the person's own chat"
}

func line(r message) string {
	where := "me"
	if r.ChatTitle != "" {
		where = r.ChatTitle
	}
	from := r.From
	if r.Forwarded != "" {
		from += ", forwarded from " + r.Forwarded
	}
	return fmt.Sprintf("[%s] %s %s (%s): %s", r.ID, r.At.Format(time.RFC3339), from, where, r.Text)
}

// ---- actions -----------------------------------------------------------------------------------------------------

func (botConn) Act(ctx context.Context, in connectors.Instance, action string, args map[string]string) (connectors.Result, error) {
	tok, err := token(in)
	if err != nil {
		return connectors.Result{}, err
	}
	text := strings.TrimSpace(args["text"])
	if text == "" {
		return connectors.Result{}, connectors.Sayf("text: required")
	}
	tg := client(in)
	unlock := lock(in.ID)
	defer unlock()
	m, err := load(in)
	if err != nil {
		return connectors.Result{}, err
	}
	if m.Chat == 0 {
		return connectors.Result{}, connectors.Sayf("The bot has no chat yet: the person binds it in Settings → Connections")
	}
	switch action {
	case "notify":
		if utf8.RuneCountInString(text) > notifyMax {
			return connectors.Result{}, connectors.Sayf("text: at most %d characters", notifyMax)
		}
		var buttons []string
		for _, b := range strings.Split(args["buttons"], "|") {
			if b = oneLine(b); b != "" {
				buttons = append(buttons, clip(b, buttonMax))
			}
		}
		if len(buttons) > buttonsMax {
			return connectors.Result{}, connectors.Sayf("buttons: at most %d", buttonsMax)
		}
		now := time.Now()
		m.Pushes = slices.DeleteFunc(m.Pushes, func(t time.Time) bool { return now.Sub(t) > time.Hour })
		if len(m.Pushes) >= notifyPerHour {
			return connectors.Result{}, connectors.Sayf("Already %d messages to the person this hour: wait, or put it together", notifyPerHour)
		}
		h := hooked()
		silent := h.Quiet != nil && h.Quiet(now)
		body := notifybots.ToHTML(text)
		nonce := code()
		var rows [][]notifybots.Button
		if len(buttons) > 0 {
			var row []notifybots.Button
			for i, b := range buttons {
				row = append(row, notifybots.Button{Text: b, Data: "n:" + nonce + ":" + strconv.Itoa(i)})
			}
			rows = append(rows, row)
		}
		if h.Open != nil && in.Conv != "" {
			if u := h.Open(in.Conv); u != "" {
				rows = append(rows, []notifybots.Button{{Text: word(connectors.T("Open in harnessly", "Открыть в harnessly")), URL: u}})
			}
		}
		id, err := tg.SendHTML(ctx, tok, m.Chat, body, rows, silent)
		if err != nil {
			return connectors.Result{}, said(err)
		}
		m.Pushes = append(m.Pushes, now)
		if len(buttons) > 0 {
			if m.Notices == nil {
				m.Notices = map[string]*notice{}
			}
			m.Notices[nonce] = &notice{Conv: in.Conv, HTML: body, Buttons: buttons, Message: id, At: now}
		}
		remember(&m, id, in.Conv)
		if err := keep(in, m); err != nil {
			return connectors.Result{}, err
		}
		out := "Sent to the person's Telegram."
		if silent {
			out += " Without a sound: their quiet hours."
		}
		if len(buttons) > 0 {
			out += " A press of a button comes back to this chat."
		}
		return connectors.Result{Text: out}, nil
	case "send":
		want := strings.TrimSpace(args["chat"])
		gi := slices.IndexFunc(m.Groups, func(g group) bool {
			return strconv.FormatInt(g.ID, 10) == want || strings.EqualFold(g.Title, want)
		})
		if gi < 0 {
			return connectors.Result{}, connectors.Sayf("The bot is not in a group %q the person added it to", want)
		}
		if utf8.RuneCountInString(text) > notifybots.MessageLimit-200 {
			return connectors.Result{}, connectors.Sayf("text: too long for one message")
		}
		if _, err := tg.SendHTML(ctx, tok, m.Groups[gi].ID, notifybots.ToHTML(text), nil, false); err != nil {
			return connectors.Result{}, said(err)
		}
		return connectors.Result{Text: "Sent to the group " + m.Groups[gi].Title + "."}, nil
	}
	return connectors.Result{}, connectors.ErrUnknownTool
}

// remember ties a bot's message to the harnessly chat that wrote it: a reply to it goes back there.
func remember(m *memo, message int64, conv string) {
	if conv == "" || message == 0 {
		return
	}
	if m.Replies == nil {
		m.Replies = map[string]string{}
	}
	m.Replies[strconv.FormatInt(message, 10)] = conv
}

// ---- the relay of proposals --------------------------------------------------------------------------------------

// shortArgs are the arguments the short card shows as they are: who, where, the subject.
var shortArgs = map[string]bool{"to": true, "cc": true, "bcc": true, "subject": true, "chat": true, "title": true,
	"start": true, "end": true, "when": true, "attendees": true, "name": true, "url": true}

// Offer shows a proposal of any connection in the bound chat, with one-time «Send / Don't send».
func (botConn) Offer(ctx context.Context, in connectors.Instance, p connectors.Proposal) error {
	tok, err := token(in)
	if err != nil {
		return err
	}
	unlock := lock(in.ID)
	defer unlock()
	m, err := load(in)
	if err != nil || m.Chat == 0 {
		return err
	}
	full := in.Config["letters"] == "full"
	title := word(p.Title)
	var b strings.Builder
	b.WriteString("<b>" + html.EscapeString(title) + "</b>\n<i>" + html.EscapeString(oneLine(p.Name)) + "</i>")
	if p.Why != "" {
		b.WriteString("\n" + html.EscapeString(clip(p.Why, shortMax)))
	}
	b.WriteString("\n")
	room := fullMax
	for _, a := range p.Args {
		name := html.EscapeString(a.Name)
		switch {
		case full && room > 0:
			v := clip(a.Value, room)
			room -= utf8.RuneCountInString(v)
			b.WriteString("\n<b>" + name + ":</b> " + html.EscapeString(v))
		case shortArgs[a.Name]:
			b.WriteString("\n<b>" + name + ":</b> " + html.EscapeString(clip(oneLine(a.Value), shortMax)))
		default:
			b.WriteString("\n<b>" + name + ":</b> " + html.EscapeString(fmt.Sprintf(word(connectors.T(
				"%d characters, in full in harnessly", "%d символов, полностью — в harnessly")), utf8.RuneCountInString(a.Value))))
		}
	}
	body := b.String()
	if utf8.RuneCountInString(body) > textMax {
		body = clip(body, textMax)
	}
	nonce := code()
	rows := [][]notifybots.Button{{
		{Text: word(connectors.T("Send", "Отправить")), Data: "a:" + nonce + ":y"},
		{Text: word(connectors.T("Don't send", "Не отправлять")), Data: "a:" + nonce + ":n"},
	}}
	if h := hooked(); h.Open != nil && p.Conv != "" {
		if u := h.Open(p.Conv); u != "" {
			rows = append(rows, []notifybots.Button{{Text: word(connectors.T("Open in harnessly", "Открыть в harnessly")), URL: u}})
		}
	}
	id, err := client(in).SendHTML(ctx, tok, m.Chat, body, rows, false)
	if err != nil {
		return said(err)
	}
	if m.Offers == nil {
		m.Offers = map[string]offer{}
	}
	m.Offers[nonce] = offer{Proposal: p.ID, Message: id, Title: title, Until: p.CreatedAt.Add(30 * time.Minute)}
	remember(&m, id, p.Conv)
	return keep(in, m)
}

// Settle closes the shown proposal once it was answered anywhere: its buttons are gone.
func (botConn) Settle(ctx context.Context, in connectors.Instance, p connectors.Proposal) error {
	tok, err := token(in)
	if err != nil {
		return err
	}
	unlock := lock(in.ID)
	m, err := load(in)
	if err != nil {
		unlock()
		return err
	}
	var found *offer
	for k, o := range m.Offers {
		if o.Proposal == p.ID {
			delete(m.Offers, k)
			found = &o
			break
		}
	}
	if found != nil {
		err = keep(in, m)
	}
	chatID := m.Chat
	unlock()
	if found == nil || err != nil {
		return err
	}
	return client(in).EditHTML(ctx, tok, chatID, found.Message, settled(found.Title, p.State))
}

func settled(title, state string) string {
	var w connectors.Text
	switch state {
	case connectors.Done:
		w = connectors.T("sent", "отправлено")
	case connectors.Failed:
		w = connectors.T("failed: see harnessly", "не вышло — подробности в harnessly")
	case connectors.Declined:
		w = connectors.T("not sent", "не отправлено")
	default:
		w = connectors.T("out of time", "срок вышел")
	}
	return "<b>" + html.EscapeString(title) + "</b> — " + html.EscapeString(word(w))
}

// ---- small helpers -----------------------------------------------------------------------------------------------

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
