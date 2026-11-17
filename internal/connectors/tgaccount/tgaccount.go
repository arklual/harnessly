// Package tgaccount is the connector «Telegram account» (docs/connectors.md): the person's own Telegram account through
// MTProto (github.com/gotd/td), so that the agent reads their chats and, after their yes on the card, writes as them.
// The Telegram bot connector (package telegram) stays the channel between harnessly and the person; this one is the
// person's own account.
//
//   - Connect: api_id and api_hash of the person's own app (my.telegram.org → API development tools), the hash in the
//     safe, checked by asking Telegram for a sign-in code. Then the page shows a QR code (Step.Image) that the person
//     scans in Telegram on the phone (Settings → Devices → Link Desktop Device); a cloud password, when the account has
//     one, is an optional secret field. The session is a file of the connection's folder on the machine (0600), gone
//     with the connection; removing the connection signs this device out of Telegram (Forgetter).
//   - Source: the account's updates (gotd's updates manager, its state kept beside the session, so a restart catches up
//     with getDifference). A message in a private chat, a mention or a reply in a group (or every message but channels,
//     by the connection's choice) is a chat.message event without its text; the text is read on the machine.
//   - Reads: chats (the dialogs with unread counts), messages (a chat's history), message (one, an event's ref), search,
//     me. Everything read comes marked as data, not instructions.
//   - Actions: send writes as the person, after their yes on the card; note writes to the person's own Saved Messages
//     at once (Direct: it reaches only them), limited per hour.
package tgaccount

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"rsc.io/qr"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

// ID names the connector.
const ID = "telegram-account"

// EventMessage is the one event: a message in one of the person's chats.
const EventMessage = "chat.message"

// What the connection turns into events.
const (
	eventsDirect = "direct"
	eventsAll    = "all"
	eventsNone   = "none"
)

const (
	// loginIdle is how long the sign-in waits for the page to ask for its code again before it stops.
	loginIdle = 2 * time.Minute
	// retryAfter is how long a problem of the sign-in stays shown before a new code is made.
	retryAfter = 20 * time.Second
	// notesPerHour is how many notes to the person's Saved Messages one connection writes in an hour.
	notesPerHour = 20
	textMax      = 4096
	limitMax     = 100
)

// pause is the wait of a source before the account is signed in, and after a lost session.
var pause = 3 * time.Second

// Recheck, when set, checks a connection again once its sign-in is done, so that its account label becomes the
// person's name. The runtime sets it.
var recheck atomic.Pointer[func(id string)]

// SetRecheck sets what runs when a connection signed in.
func SetRecheck(f func(id string)) { recheck.Store(&f) }

type accountConn struct{}

func init() { connectors.Register(accountConn{}) }

func (accountConn) Spec() connectors.Spec {
	chat := connectors.Arg{Name: "chat", Required: true,
		Summary: "@username, +phone, \"me\" (Saved Messages) or the id from chats"}
	return connectors.Spec{
		ID:    ID,
		Kind:  connectors.Chat,
		Title: connectors.T("Telegram account", "Telegram-аккаунт"),
		Summary: connectors.T("Your own account: the agent reads your chats and writes as you after your yes",
			"Ваш собственный аккаунт: агент читает ваши чаты и пишет от вашего имени после вашего «да»"),
		Fields: []connectors.Field{
			{ID: "apiId", Type: connectors.FieldNumber, Label: connectors.T("api_id", "api_id"),
				Hint: connectors.T("my.telegram.org → API development tools: create an app, it gives api_id and api_hash",
					"my.telegram.org → API development tools: создайте приложение, там api_id и api_hash"),
				Placeholder: "1234567"},
			{ID: "apiHash", Type: connectors.FieldPassword, Secret: true, Label: connectors.T("api_hash", "api_hash"),
				Placeholder: "0123456789abcdef0123456789abcdef"},
			{ID: "password", Type: connectors.FieldPassword, Secret: true, Optional: true,
				Label: connectors.T("Cloud password", "Облачный пароль"),
				Hint: connectors.T("Only if the account has two-step verification: Telegram asks it after the QR code",
					"Только если в аккаунте включена двухэтапная проверка: Telegram спросит его после QR-кода")},
			{ID: "events", Type: connectors.FieldChoice, Default: eventsDirect,
				Label: connectors.T("Events of new messages", "События о новых сообщениях"),
				Hint: connectors.T("They go to the hub's rules and the inbox, without the text",
					"Уходят в правила хаба и во «Входящие», без текста"),
				Choices: []connectors.Choice{
					{Value: eventsDirect, Label: connectors.T("Private chats and mentions", "Личные чаты и упоминания")},
					{Value: eventsAll, Label: connectors.T("Every chat but channels", "Все чаты, кроме каналов")},
					{Value: eventsNone, Label: connectors.T("None", "Никаких")},
				}},
		},
		Events: []string{EventMessage},
		Tools: []connectors.Tool{
			{Name: "chats", Summary: "the chats of the account, the latest first: id, title, unread count, last message date",
				Args: []connectors.Arg{{Name: "limit", Summary: "how many, 30 by default, at most 100"},
					{Name: "unread", Summary: "\"true\": only chats with unread messages"}}},
			{Name: "messages", Summary: "the last messages of a chat, the oldest first",
				Args: []connectors.Arg{chat, {Name: "limit", Summary: "how many, 20 by default, at most 100"},
					{Name: "before", Summary: "a message id: only messages older than it"}}},
			{Name: "message", Summary: "one message by its chat and id (an event's ref)",
				Args: []connectors.Arg{chat, {Name: "id", Required: true, Summary: "the message id"}}},
			{Name: "search", Summary: "messages with these words, in one chat or in all of them",
				Args: []connectors.Arg{{Name: "q", Required: true, Summary: "the words"},
					{Name: "chat", Summary: "a chat as in messages; all chats when empty"},
					{Name: "limit", Summary: "how many, 20 by default, at most 100"}}},
			{Name: "me", Summary: "who the connection signs in as"},
		},
		Actions: []connectors.Action{
			{Name: "send", Summary: "write a message as the person, after their yes",
				Title: connectors.T("Write in Telegram as you", "Написать в Telegram от вашего имени"),
				Args: []connectors.Arg{chat, {Name: "text", Required: true},
					{Name: "reply", Summary: "the id of the message to answer"}}},
			{Name: "note", Direct: true, Summary: "write to the person's own Saved Messages, at once and without a card",
				Title: connectors.T("Write to my Saved Messages", "Написать мне в «Избранное»"),
				Args:  []connectors.Arg{{Name: "text", Required: true}}},
		},
		Inbox: "telegram",
	}
}

// ---- the connection's record -----------------------------------------------------------------------------------

type memo struct {
	// Signed says the session file holds a signed-in account.
	Signed bool   `json:"signed,omitempty"`
	Self   int64  `json:"self,omitempty"`
	Who    string `json:"who,omitempty"`
	// Notes are the times of the last notes to Saved Messages, for the hourly limit.
	Notes []time.Time `json:"notes,omitempty"`
}

var (
	locksMu sync.Mutex
	locks   = map[string]*sync.Mutex{}
)

// lock serializes the record and the peer book of one connection.
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

func load(in connectors.Instance) memo {
	var m memo
	if in.Load != nil {
		_ = in.Load(&m)
	}
	return m
}

func keep(in connectors.Instance, m memo) error {
	if in.Store == nil {
		return nil
	}
	return in.Store(m)
}

func signed(in connectors.Instance) bool {
	if in.Dir == "" {
		return false
	}
	unlock := lock(in.ID)
	defer unlock()
	if _, err := os.Stat(sessionFile(in)); err != nil {
		return false
	}
	return load(in).Signed
}

// signOut forgets the session after Telegram refused it (ended from another device).
func signOut(in connectors.Instance) {
	unlock := lock(in.ID)
	defer unlock()
	m := load(in)
	m.Signed, m.Self, m.Who = false, 0, ""
	_ = keep(in, m)
	_ = os.Remove(sessionFile(in))
	_ = os.Remove(stateFile(in))
}

func sessionFile(in connectors.Instance) string { return filepath.Join(in.Dir, "session.json") }
func stateFile(in connectors.Instance) string   { return filepath.Join(in.Dir, "updates.json") }

// ---- the client --------------------------------------------------------------------------------------------------

var hashRe = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

// app reads the connection's api_id and api_hash.
func app(in connectors.Instance) (int, string, error) {
	id, err := strconv.Atoi(strings.TrimSpace(in.Config["apiId"]))
	if err != nil || id <= 0 {
		return 0, "", connectors.Sayf("api_id is a number from my.telegram.org")
	}
	hash := ""
	if in.Secret != nil {
		hash, _ = in.Secret("apiHash")
	}
	hash = strings.TrimSpace(hash)
	if !hashRe.MatchString(hash) {
		return 0, "", connectors.Sayf("api_hash is 32 hex characters from my.telegram.org")
	}
	return id, hash, nil
}

// newClient is a client of the connection's app; opts names the session and the handler of updates.
var newClient = func(in connectors.Instance, opts telegram.Options) (*telegram.Client, error) {
	id, hash, err := app(in)
	if err != nil {
		return nil, err
	}
	opts.Device = telegram.DeviceConfig{DeviceModel: "harnessly", SystemVersion: "harnessly", AppVersion: "1"}
	return telegram.NewClient(id, hash, opts), nil
}

var (
	livesMu sync.Mutex
	// lives are the clients of the running sources, by connection: reads and actions go through them.
	lives = map[string]*tg.Client{}
)

// withAPI runs f with a signed-in client of the connection: its source's, or one made for this call from a copy of
// the session (so that two clients never write the same file).
func withAPI(ctx context.Context, in connectors.Instance, f func(ctx context.Context, api *tg.Client) error) error {
	livesMu.Lock()
	api := lives[in.ID]
	livesMu.Unlock()
	if api != nil {
		return said(f(ctx, api))
	}
	if !signed(in) {
		return errNotSigned
	}
	raw, err := os.ReadFile(sessionFile(in))
	if err != nil {
		return errNotSigned
	}
	mem := &session.StorageMemory{}
	if err := mem.StoreSession(ctx, raw); err != nil {
		return err
	}
	c, err := newClient(in, telegram.Options{SessionStorage: mem, NoUpdates: true})
	if err != nil {
		return err
	}
	return said(c.Run(ctx, func(ctx context.Context) error { return f(ctx, c.API()) }))
}

var errNotSigned = connectors.Sayf("Not signed in to Telegram yet: scan the QR code in Settings → Connections")

// revoked says Telegram no longer knows the session.
func revoked(err error) bool {
	return tgerr.Is(err, "AUTH_KEY_UNREGISTERED", "SESSION_REVOKED", "SESSION_EXPIRED", "USER_DEACTIVATED",
		"USER_DEACTIVATED_BAN", "AUTH_KEY_INVALID")
}

// said turns Telegram's errors into words for the person and the agent.
func said(err error) error {
	if err == nil {
		return nil
	}
	var s *connectors.Said
	if errors.As(err, &s) || errors.Is(err, connectors.ErrNotFound) || errors.Is(err, connectors.ErrBadArgs) {
		return err
	}
	if d, ok := tgerr.AsFloodWait(err); ok {
		return connectors.Sayf("Telegram asks to wait %s before the next request", d.Round(time.Second))
	}
	if revoked(err) {
		return connectors.Sayf("Telegram ended this session: scan the QR code in Settings → Connections again")
	}
	if rpc, ok := tgerr.As(err); ok {
		return connectors.Sayf("Telegram refused: %s", rpc.Type)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return connectors.Sayf("Telegram did not answer in time")
	}
	return connectors.Sayf("Telegram: %v", err)
}

// ---- connect -----------------------------------------------------------------------------------------------------

// Check makes sure Telegram takes the app (it gives a sign-in code to it), and says whom the session signs in as, once
// it does.
func (accountConn) Check(ctx context.Context, in connectors.Instance) (connectors.Account, error) {
	if _, _, err := app(in); err != nil {
		return connectors.Account{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if signed(in) {
		var label string
		err := withAPI(ctx, in, func(ctx context.Context, api *tg.Client) error {
			users, err := api.UsersGetUsers(ctx, []tg.InputUserClass{&tg.InputUserSelf{}})
			if err != nil {
				return err
			}
			if len(users) > 0 {
				if u, ok := users[0].(*tg.User); ok {
					label = userName(u)
				}
			}
			return nil
		})
		if err != nil {
			return connectors.Account{}, err
		}
		return connectors.Account{Label: label}, nil
	}
	c, err := newClient(in, telegram.Options{SessionStorage: &session.StorageMemory{}, NoUpdates: true})
	if err != nil {
		return connectors.Account{}, err
	}
	err = c.Run(ctx, func(ctx context.Context) error {
		_, err := c.QR().Export(ctx)
		return err
	})
	if tgerr.Is(err, "API_ID_INVALID", "API_ID_PUBLISHED_FLOOD") {
		return connectors.Account{}, connectors.Sayf("Telegram does not take this api_id and api_hash: copy them from my.telegram.org again")
	}
	if err != nil {
		return connectors.Account{}, said(err)
	}
	return connectors.Account{Label: "Telegram"}, nil
}

// login is the sign-in of one connection by a QR code, while the page shows it.
type login struct {
	mu      sync.Mutex
	image   string
	problem string
	failed  time.Time
	touched time.Time
	running bool
}

var (
	loginsMu sync.Mutex
	logins   = map[string]*login{}
)

// Next shows the QR code to scan while the connection is not signed in; asking it keeps the sign-in going.
func (accountConn) Next(_ context.Context, in connectors.Instance) (connectors.Step, bool) {
	if in.Dir == "" || signed(in) {
		return connectors.Step{}, false
	}
	loginsMu.Lock()
	l := logins[in.ID]
	if l == nil {
		l = &login{}
		logins[in.ID] = l
	}
	loginsMu.Unlock()

	l.mu.Lock()
	defer l.mu.Unlock()
	l.touched = time.Now()
	if !l.running && (l.problem == "" || time.Since(l.failed) > retryAfter) {
		l.running, l.problem, l.image = true, "", ""
		go signIn(in, l)
	}
	switch {
	case l.problem != "":
		return connectors.Step{Text: connectors.T(l.problem, l.problem)}, true
	case l.image == "":
		return connectors.Step{Text: connectors.T("Making the code to sign in…", "Готовлю код для входа…")}, true
	}
	return connectors.Step{
		Text: connectors.T("Scan the code from Telegram on your phone, not with the camera: Settings → Devices → "+
			"Link Desktop Device. The code changes every half a minute.",
			"Отсканируйте код из самого Telegram на телефоне, не камерой: Настройки → Устройства → "+
				"Подключить устройство. Код меняется каждые полминуты."),
		Image: l.image,
	}, true
}

var errNoPassword = errors.New("no cloud password")

// signIn runs the QR sign-in until it is done, it fails, or the page stops asking for the code.
func signIn(in connectors.Instance, l *login) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				l.mu.Lock()
				idle := time.Since(l.touched) > loginIdle
				l.mu.Unlock()
				if idle {
					cancel()
					return
				}
			}
		}
	}()
	err := qrSignIn(ctx, in, func(image string) {
		l.mu.Lock()
		l.image = image
		l.mu.Unlock()
	})
	l.mu.Lock()
	l.running = false
	if err != nil && ctx.Err() == nil {
		l.problem, l.failed = problemOf(err), time.Now()
	}
	l.mu.Unlock()
	if err == nil {
		loginsMu.Lock()
		delete(logins, in.ID)
		loginsMu.Unlock()
		if f := recheck.Load(); f != nil {
			(*f)(in.ID)
		}
	}
}

func problemOf(err error) string {
	switch {
	case errors.Is(err, errNoPassword):
		return "The account has a cloud password: remove this connection and connect again with the password filled in"
	case tgerr.Is(err, "PASSWORD_HASH_INVALID"):
		return "The cloud password did not fit: remove this connection and connect again with the right one"
	}
	return said(err).Error()
}

func qrSignIn(ctx context.Context, in connectors.Instance, show func(image string)) error {
	if err := os.MkdirAll(in.Dir, 0o700); err != nil {
		return err
	}
	d := tg.NewUpdateDispatcher()
	loggedIn := qrlogin.OnLoginToken(d)
	c, err := newClient(in, telegram.Options{SessionStorage: &session.FileStorage{Path: sessionFile(in)}, UpdateHandler: d})
	if err != nil {
		return err
	}
	id, hash, _ := app(in)
	return c.Run(ctx, func(ctx context.Context) error {
		q := qrlogin.NewQR(c.API(), id, hash, qrlogin.Options{Migrate: c.MigrateTo,
			Password: func(context.Context) (string, error) {
				p := ""
				if in.Secret != nil {
					p, _ = in.Secret("password")
				}
				if p == "" {
					return "", errNoPassword
				}
				return p, nil
			}})
		a, err := q.Auth(ctx, loggedIn, func(_ context.Context, t qrlogin.Token) error {
			image, err := qrImage(t)
			if err != nil {
				return err
			}
			show(image)
			return nil
		})
		if err != nil {
			return err
		}
		u, _ := a.User.(*tg.User)
		unlock := lock(in.ID)
		defer unlock()
		m := load(in)
		m.Signed = true
		if u != nil {
			m.Self, m.Who = u.ID, userName(u)
		}
		return keep(in, m)
	})
}

// qrImage is the code as a PNG data URL.
func qrImage(t qrlogin.Token) (string, error) {
	img, err := t.Image(qr.M)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes()), nil
}

// Forget signs this device out of Telegram when the connection is removed.
func (accountConn) Forget(ctx context.Context, in connectors.Instance) {
	defer func() {
		forgetBook(in.ID)
		loginsMu.Lock()
		delete(logins, in.ID)
		loginsMu.Unlock()
	}()
	if !signed(in) {
		return
	}
	_ = withAPI(ctx, in, func(ctx context.Context, api *tg.Client) error {
		_, err := api.AuthLogOut(ctx)
		return err
	})
}

// CanAct refuses an action before the account is signed in.
func (accountConn) CanAct(in connectors.Instance, _ string) error {
	if !signed(in) {
		return errNotSigned
	}
	return nil
}

// ---- the source --------------------------------------------------------------------------------------------------

// Watch waits for the sign-in, then follows the account's updates until ctx ends. A session ended from another device
// is forgotten, and the page shows a new code.
func (accountConn) Watch(ctx context.Context, in connectors.Instance, _ connectors.Cursor, emit connectors.Emit) error {
	for {
		for !signed(in) {
			if !sleep(ctx, pause) {
				return nil
			}
		}
		err := follow(ctx, in, emit)
		if ctx.Err() != nil {
			return nil
		}
		if revoked(err) {
			signOut(in)
			continue
		}
		return said(err)
	}
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

func follow(ctx context.Context, in connectors.Instance, emit connectors.Emit) error {
	mode := in.Config["events"]
	if mode == "" {
		mode = eventsDirect
	}
	var emitMu sync.Mutex
	var self atomic.Int64
	d := tg.NewUpdateDispatcher()
	handle := func(e tg.Entities, msg tg.MessageClass) error {
		m, ok := msg.(*tg.Message)
		if !ok {
			return nil
		}
		b := bookOf(in)
		b.learnEntities(e)
		ev, ok := eventOf(b, mode, self.Load(), m)
		if !ok {
			return nil
		}
		emitMu.Lock()
		defer emitMu.Unlock()
		return emit([]connectors.Event{ev}, connectors.Cursor(strconv.Itoa(m.Date)))
	}
	d.OnNewMessage(func(_ context.Context, e tg.Entities, u *tg.UpdateNewMessage) error { return handle(e, u.Message) })
	d.OnNewChannelMessage(func(_ context.Context, e tg.Entities, u *tg.UpdateNewChannelMessage) error {
		return handle(e, u.Message)
	})
	mgr := newManager(d, stateFile(in))
	c, err := newClient(in, telegram.Options{SessionStorage: &session.FileStorage{Path: sessionFile(in)}, UpdateHandler: mgr})
	if err != nil {
		return err
	}
	return c.Run(ctx, func(ctx context.Context) error {
		me, err := c.Self(ctx)
		if err != nil {
			return err
		}
		self.Store(me.ID)
		livesMu.Lock()
		lives[in.ID] = c.API()
		livesMu.Unlock()
		defer func() {
			livesMu.Lock()
			delete(lives, in.ID)
			livesMu.Unlock()
		}()
		return mgr.Run(ctx, c.API(), me.ID, managerAuth())
	})
}

// eventOf makes the event of a new message, or false when the connection's choice leaves it out.
func eventOf(b *book, mode string, self int64, m *tg.Message) (connectors.Event, bool) {
	if mode == eventsNone {
		return connectors.Event{}, false
	}
	chat := markedPeer(m.PeerID)
	if chat == 0 || chat == self {
		return connectors.Event{}, false
	}
	p, _ := b.get(chat)
	facts := map[string]string{}
	switch {
	case p.Kind == kindChannel && !p.Group:
		// A channel's post is news, not a message to the person.
		return connectors.Event{}, false
	case m.PeerID.TypeID() == tg.PeerUserTypeID:
		facts["chat"] = "private"
		if p.Bot {
			facts["bot"] = "true"
		}
	default:
		facts["chat"] = "group"
		if m.Out || (mode == eventsDirect && !m.Mentioned) {
			return connectors.Event{}, false
		}
	}
	if m.Mentioned {
		facts["mentioned"] = "true"
	}
	if _, ok := m.GetReplyTo(); ok {
		facts["reply"] = "true"
	}
	if kind := mediaKind(m); kind != "" {
		facts["media"] = kind
	}
	if _, ok := m.GetFwdFrom(); ok {
		facts["forwarded"] = "true"
	}
	id := strconv.FormatInt(chat, 10)
	ev := connectors.Event{
		Type:   EventMessage,
		ID:     id + ":" + strconv.Itoa(m.ID),
		At:     time.Unix(int64(m.Date), 0).UTC(),
		Actor:  b.senderName(m, self),
		Thread: "tg:" + id,
		Ref:    "message chat=" + id + " id=" + strconv.Itoa(m.ID),
		Mine:   m.Out,
		Facts:  facts,
	}
	if facts["chat"] == "group" {
		ev.Title = p.Title
	}
	if p.Username != "" && p.Kind == kindChannel {
		ev.URL = "https://t.me/" + p.Username + "/" + strconv.Itoa(m.ID)
	}
	return ev, true
}
