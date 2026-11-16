// Package imap is the mail connector (docs/connectors.md, «Mail»): any mailbox with IMAP and SMTP over TLS and an app
// password — Yandex, Mail.ru, iCloud, Fastmail, Gmail, a server of one's own. Its source watches INBOX with IDLE (or
// polls where IDLE is missing) on one connection and emits mail.received, mail.replied when the person answers from any
// client, and mail.unanswered when a letter from a person (not a mailing) waits for an answer N days. The agent lists,
// searches and reads letters, saves an attachment into the chat's folder, follows a thread and keeps a draft; sending,
// replying, marking read and archiving run only after the person's yes on the card.
package imap

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"time"

	goimap "github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/charset"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

// ID names the connector.
const ID = "imap"

// Event types.
const (
	EventReceived   = "mail.received"
	EventReplied    = "mail.replied"
	EventUnanswered = "mail.unanswered"
)

const (
	defaultUnansweredDays = 3
	maxUnansweredDays     = 60
	dialTimeout           = 20 * time.Second
	commandTimeout        = 60 * time.Second
)

func init() {
	// Headers and envelopes in koi8-r, windows-1251 and the rest are decoded, not dropped.
	goimap.CharsetReader = charset.Reader
	connectors.Register(mailConn{})
}

type mailConn struct{}

// manual shows the server fields only where the servers are not preset.
var manual = &connectors.When{Field: "provider", Values: []string{"auto", "other"}}

func (mailConn) Spec() connectors.Spec {
	id := connectors.Arg{Name: "id", Summary: "the letter's Message-ID, as in an event or in list"}
	return connectors.Spec{
		ID:      ID,
		Kind:    connectors.Mail,
		Title:   connectors.T("Mail (IMAP)", "Почта (IMAP)"),
		Summary: connectors.T("Any mailbox with an app password: Yandex, Mail.ru, iCloud, Fastmail, Gmail, your own server", "Любой ящик с паролем приложения: Яндекс, Mail.ru, iCloud, Fastmail, Gmail, свой сервер"),
		Fields: []connectors.Field{
			{ID: "email", Type: connectors.FieldEmail, Label: connectors.T("Address", "Адрес"), Placeholder: "me@example.com"},
			{ID: "password", Type: connectors.FieldPassword, Secret: true,
				Label: connectors.T("App password", "Пароль приложения"),
				Hint: connectors.T(
					"Not the mailbox's own password: make an app password and turn mail clients (IMAP) on. Yandex: id.yandex.ru → Security → App passwords, and Mail settings → Mail clients. Mail.ru: Settings → Security → Passwords for external apps (a phone must be linked). iCloud: account.apple.com → App-specific passwords (needs two-factor). Gmail: myaccount.google.com/apppasswords (needs 2-Step Verification). Fastmail: Settings → Privacy & Security → App passwords.",
					"Не обычный пароль ящика: создайте пароль приложения и включите доступ почтовых программ (IMAP). Яндекс: id.yandex.ru → Безопасность → Пароли приложений, и Настройки почты → Почтовые программы. Mail.ru: Настройки → Безопасность → Пароли для внешних приложений (нужен привязанный телефон). iCloud: account.apple.com → Пароли приложений (нужна двухфакторная защита). Gmail: myaccount.google.com/apppasswords (нужна двухэтапная проверка). Fastmail: Settings → Privacy & Security → App passwords.")},
			{ID: "provider", Type: connectors.FieldChoice, Optional: true, Default: "auto",
				Label: connectors.T("Servers", "Серверы"),
				Hint:  connectors.T("By the address's domain when it is known; “Other” takes the servers below.", "По домену адреса, если он известен; «Другой» берёт серверы ниже."),
				Choices: []connectors.Choice{
					{Value: "auto", Label: connectors.T("By the address", "По адресу")},
					{Value: "yandex", Label: connectors.T("Yandex", "Яндекс")},
					{Value: "mailru", Label: connectors.T("Mail.ru", "Mail.ru")},
					{Value: "icloud", Label: connectors.T("iCloud", "iCloud")},
					{Value: "gmail", Label: connectors.T("Gmail", "Gmail")},
					{Value: "fastmail", Label: connectors.T("Fastmail", "Fastmail")},
					{Value: "other", Label: connectors.T("Other", "Другой")},
				}},
			{ID: "imapServer", Type: connectors.FieldText, Optional: true, Placeholder: "imap.example.com:993", When: manual,
				Label: connectors.T("IMAP server", "Сервер IMAP"),
				Hint:  connectors.T("When the domain does not say it itself. TLS only.", "Если домен не подсказал сам. Только TLS.")},
			{ID: "smtpServer", Type: connectors.FieldText, Optional: true, Placeholder: "smtp.example.com:465", When: manual,
				Label: connectors.T("SMTP server", "Сервер SMTP"),
				Hint:  connectors.T("465 is TLS, 587 is STARTTLS.", "465 — TLS, 587 — STARTTLS.")},
			{ID: "login", Type: connectors.FieldText, Optional: true,
				Label: connectors.T("Login, if not the address", "Логин, если не адрес")},
			{ID: "unansweredDays", Type: connectors.FieldNumber, Optional: true, Default: strconv.Itoa(defaultUnansweredDays),
				Label: connectors.T("“No answer” after, days", "«Без ответа» через, дней"),
				Hint:  connectors.T("A letter from a person, not a mailing, without your answer; 0 turns it off.", "Письмо от человека, не рассылка, без вашего ответа; 0 — выключить.")},
		},
		Events: []string{EventReceived, EventReplied, EventUnanswered},
		Tools: []connectors.Tool{
			{Name: "list", Summary: "letters of a folder, newest first, with filters", Args: []connectors.Arg{
				{Name: "folder", Summary: "INBOX by default; sent, drafts, archive or a folder's name"},
				{Name: "from", Summary: "sender's address or part of it"},
				{Name: "since", Summary: "a date YYYY-MM-DD or a span like 7d"},
				{Name: "before", Summary: "a date YYYY-MM-DD"},
				{Name: "unread", Summary: "true for unread only"},
				{Name: "query", Summary: "words in the subject or text"},
				{Name: "limit", Summary: "how many, 20 by default, at most 100"},
			}},
			{Name: "folders", Summary: "the mailbox's folders"},
			{Name: "message", Summary: "one letter: headers, text and the list of attachments", Args: []connectors.Arg{id,
				{Name: "folder", Summary: "where to look; INBOX and sent by default"}}},
			{Name: "attachment", Summary: "save an attachment of a letter into a folder (the chat's folder)", Args: []connectors.Arg{id,
				{Name: "index", Summary: "the attachment's number from message", Required: true},
				{Name: "dir", Summary: "absolute path of the folder to save into, normally the chat's folder (pwd)", Required: true},
				{Name: "folder", Summary: "where to look; INBOX and sent by default"}}},
			{Name: "thread", Summary: "the letters of a thread by References, in INBOX, sent and archive", Args: []connectors.Arg{id}},
			{Name: "draft", Summary: "save a draft into the Drafts folder; nothing is sent, so no card", Args: []connectors.Arg{
				{Name: "to", Summary: "addresses, comma separated"}, {Name: "cc"}, {Name: "subject"},
				{Name: "text", Summary: "the letter's text", Required: true},
				{Name: "reply", Summary: "Message-ID of the letter it answers, for In-Reply-To and References"}}},
		},
		Actions: []connectors.Action{
			{Name: "send", Summary: "send a new letter", Title: connectors.T("Send the letter", "Отправить письмо"),
				Args: []connectors.Arg{{Name: "to", Summary: "addresses, comma separated", Required: true}, {Name: "cc"}, {Name: "bcc"},
					{Name: "subject", Required: true}, {Name: "text", Required: true}}},
			{Name: "reply", Summary: "answer in a thread: give the recipients and the subject as they will go", Title: connectors.T("Send the reply", "Отправить ответ"),
				Args: []connectors.Arg{{Name: "id", Summary: "Message-ID of the letter answered", Required: true},
					{Name: "to", Summary: "addresses, comma separated", Required: true}, {Name: "cc"},
					{Name: "subject", Summary: "usually Re: and the subject", Required: true}, {Name: "text", Required: true}}},
			{Name: "mark-read", Summary: "mark letters read", Title: connectors.T("Mark as read", "Пометить прочитанным"),
				Args: []connectors.Arg{{Name: "id", Summary: "Message-IDs, comma separated", Required: true}}},
			{Name: "archive", Summary: "move letters from INBOX to the archive", Title: connectors.T("Move to the archive", "Переместить в архив"),
				Args: []connectors.Arg{{Name: "id", Summary: "Message-IDs, comma separated", Required: true}}},
		},
		Inbox: "mail",
	}
}

// Check signs in to IMAP and to SMTP, sends nothing.
func (mailConn) Check(ctx context.Context, in connectors.Instance) (connectors.Account, error) {
	cfg, err := configOf(ctx, in)
	if err != nil {
		return connectors.Account{}, err
	}
	c, err := dialIMAP(ctx, cfg)
	if err != nil {
		return connectors.Account{}, err
	}
	_ = c.Logout()
	s, err := dialSMTP(ctx, cfg)
	if err != nil {
		return connectors.Account{}, err
	}
	_ = s.Quit()
	return connectors.Account{Label: cfg.email}, nil
}

// config is a connection's resolved values: the servers, the login and the password of the moment.
type config struct {
	email, login, password string
	provider               string
	imap, smtp             endpoint
	unansweredDays         int
}

type endpoint struct {
	host string
	port int
	// startTLS is SMTP on 587: plain greeting, then STARTTLS before anything else. Everything else is implicit TLS.
	startTLS bool
}

func (s endpoint) addr() string { return net.JoinHostPort(s.host, strconv.Itoa(s.port)) }

func configOf(ctx context.Context, in connectors.Instance) (config, error) {
	cfg := config{email: strings.TrimSpace(in.Config["email"]), login: strings.TrimSpace(in.Config["login"])}
	addr, err := mail.ParseAddress(cfg.email)
	if err != nil {
		return cfg, connectors.Sayf("The address %q is not an e-mail address", cfg.email)
	}
	cfg.email = addr.Address
	if cfg.login == "" {
		cfg.login = cfg.email
	}
	cfg.unansweredDays = defaultUnansweredDays
	if v := strings.TrimSpace(in.Config["unansweredDays"]); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return cfg, connectors.Sayf("“No answer” after: a number of days, 0 to turn it off")
		}
		cfg.unansweredDays = min(n, maxUnansweredDays)
	}
	if in.Secret == nil {
		return cfg, connectors.Sayf("The app password is missing")
	}
	pw, err := in.Secret("password")
	if err != nil || pw == "" {
		return cfg, connectors.Sayf("The app password is missing")
	}
	cfg.password = strings.ReplaceAll(pw, " ", "") // app passwords are shown in groups: "abcd efgh ijkl mnop"
	cfg.provider = in.Config["provider"]
	cfg.imap, cfg.smtp, err = servers(ctx, in, cfg.email, cfg.provider)
	return cfg, err
}

// presets are the servers of the mailboxes most people have; the address's domain picks one with provider "auto".
var presets = map[string][2]endpoint{
	"yandex":   {{host: "imap.yandex.ru", port: 993}, {host: "smtp.yandex.ru", port: 465}},
	"mailru":   {{host: "imap.mail.ru", port: 993}, {host: "smtp.mail.ru", port: 465}},
	"icloud":   {{host: "imap.mail.me.com", port: 993}, {host: "smtp.mail.me.com", port: 587, startTLS: true}},
	"gmail":    {{host: "imap.gmail.com", port: 993}, {host: "smtp.gmail.com", port: 465}},
	"fastmail": {{host: "imap.fastmail.com", port: 993}, {host: "smtp.fastmail.com", port: 465}},
}

var domainPresets = map[string]string{
	"yandex.ru": "yandex", "ya.ru": "yandex", "yandex.com": "yandex", "yandex.by": "yandex", "yandex.kz": "yandex", "narod.ru": "yandex",
	"mail.ru": "mailru", "inbox.ru": "mailru", "list.ru": "mailru", "bk.ru": "mailru", "internet.ru": "mailru", "mail.ua": "mailru",
	"icloud.com": "icloud", "me.com": "icloud", "mac.com": "icloud",
	"gmail.com": "gmail", "googlemail.com": "gmail",
	"fastmail.com": "fastmail", "fastmail.fm": "fastmail",
}

func servers(ctx context.Context, in connectors.Instance, email, provider string) (endpoint, endpoint, error) {
	domain := strings.ToLower(email[strings.LastIndexByte(email, '@')+1:])
	if provider == "" || provider == "auto" {
		if p, ok := domainPresets[domain]; ok {
			provider = p
		}
	}
	if p, ok := presets[provider]; ok {
		return p[0], p[1], nil
	}
	imapS, smtpS := strings.TrimSpace(in.Config["imapServer"]), strings.TrimSpace(in.Config["smtpServer"])
	if imapS != "" && smtpS != "" {
		i, err := parseServer(imapS, 993)
		if err != nil {
			return endpoint{}, endpoint{}, err
		}
		s, err := parseServer(smtpS, 465)
		if err != nil {
			return endpoint{}, endpoint{}, err
		}
		s.startTLS = s.port == 587 || s.port == 25
		return i, s, nil
	}
	i, s, ok := discover(ctx, in, email, domain)
	if !ok {
		return endpoint{}, endpoint{}, connectors.Sayf("Could not find the servers of %s: choose “Other” and give the IMAP and SMTP servers", domain)
	}
	if imapS != "" {
		if v, err := parseServer(imapS, 993); err == nil {
			i = v
		}
	}
	if smtpS != "" {
		if v, err := parseServer(smtpS, 465); err == nil {
			v.startTLS = v.port == 587 || v.port == 25
			s = v
		}
	}
	return i, s, nil
}

func parseServer(s string, port int) (endpoint, error) {
	host, p, err := net.SplitHostPort(s)
	if err != nil {
		host, p = s, ""
	}
	host = strings.TrimSpace(host)
	if host == "" || strings.ContainsAny(host, "/ ") {
		return endpoint{}, connectors.Sayf("The server %q is not a host name", s)
	}
	if p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n <= 0 || n > 65535 {
			return endpoint{}, connectors.Sayf("The server %q has a wrong port", s)
		}
		port = n
	}
	return endpoint{host: host, port: port}, nil
}

// tlsConfig is the TLS of every connection; tests give it their own CA.
var tlsConfig = func(host string) *tls.Config {
	return &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
}

// dialIMAP connects with TLS and signs in. The client is closed when ctx ends.
func dialIMAP(ctx context.Context, cfg config) (*client.Client, error) {
	d := &net.Dialer{Timeout: dialTimeout}
	raw, err := d.DialContext(ctx, "tcp", cfg.imap.addr())
	if err != nil {
		return nil, connectors.Sayf("The IMAP server %s does not answer", cfg.imap.addr())
	}
	conn := tls.Client(raw, tlsConfig(cfg.imap.host))
	hctx, cancel := context.WithTimeout(ctx, dialTimeout)
	err = conn.HandshakeContext(hctx)
	cancel()
	if err != nil {
		_ = raw.Close()
		return nil, connectors.Sayf("The IMAP server %s does not speak TLS on this port", cfg.imap.addr())
	}
	c, err := client.New(conn)
	if err != nil {
		_ = conn.Close()
		return nil, connectors.Sayf("The IMAP server %s did not greet", cfg.imap.addr())
	}
	c.Timeout = commandTimeout
	c.ErrorLog = quiet // a closed connection is said by the error the caller gets; nothing goes to the runtime's log
	if err := c.Login(cfg.login, cfg.password); err != nil {
		_ = c.Terminate()
		return nil, loginErr(cfg, "IMAP", err)
	}
	return c, nil
}

func dialSMTP(ctx context.Context, cfg config) (*smtp.Client, error) {
	d := &net.Dialer{Timeout: dialTimeout}
	raw, err := d.DialContext(ctx, "tcp", cfg.smtp.addr())
	if err != nil {
		return nil, connectors.Sayf("The SMTP server %s does not answer", cfg.smtp.addr())
	}
	_ = raw.SetDeadline(time.Now().Add(2 * commandTimeout))
	var c *smtp.Client
	if cfg.smtp.startTLS {
		c, err = smtp.NewClientStartTLS(raw, tlsConfig(cfg.smtp.host))
	} else {
		conn := tls.Client(raw, tlsConfig(cfg.smtp.host))
		if err = conn.HandshakeContext(ctx); err == nil {
			c = smtp.NewClient(conn)
		}
	}
	if err != nil {
		_ = raw.Close()
		return nil, connectors.Sayf("The SMTP server %s does not speak TLS on this port", cfg.smtp.addr())
	}
	if err := c.Auth(sasl.NewPlainClient("", cfg.login, cfg.password)); err != nil {
		_ = c.Close()
		return nil, loginErr(cfg, "SMTP", err)
	}
	return c, nil
}

// loginErr says why a mailbox did not let in, in words: the endpoint's own words when it gives them (no secret is in
// them: the password is never sent back), and what usually helps.
func loginErr(cfg config, proto string, err error) error {
	said := strings.TrimSpace(err.Error())
	if len(said) > 200 {
		said = said[:200]
	}
	hint := "check the app password (the mailbox's own password does not work there) and that mail clients (IMAP) are on in the mailbox's settings"
	switch cfg.provider {
	case "yandex", "mailru":
	default:
		if p := domainPresets[strings.ToLower(cfg.email[strings.LastIndexByte(cfg.email, '@')+1:])]; p != "yandex" && p != "mailru" {
			return connectors.Sayf("The mailbox did not let in over %s (%s): %s", proto, said, hint)
		}
	}
	return connectors.Sayf("The mailbox did not let in over %s (%s): %s; if both are right, the mailbox may need a paid plan for mail clients", proto, said, hint)
}

// pool keeps one connection per mailbox for reads and actions, besides the watcher's, and closes it after a quiet
// while: a series of reads does not sign in again and again, and a mailbox never sees more than two connections.
type pooled struct {
	mu    sync.Mutex
	c     *client.Client
	key   string
	timer *time.Timer
}

var (
	poolMu   sync.Mutex
	pool     = map[string]*pooled{}
	poolIdle = 2 * time.Minute
)

// withClient runs fn on the connection's read connection, signing in when needed; one call at a time per mailbox.
func withClient(ctx context.Context, in connectors.Instance, fn func(c *client.Client, cfg config) error) error {
	cfg, err := configOf(ctx, in)
	if err != nil {
		return err
	}
	poolMu.Lock()
	p := pool[in.ID]
	if p == nil {
		p = &pooled{}
		pool[in.ID] = p
	}
	poolMu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	key := cfg.login + "\x00" + cfg.imap.addr() + "\x00" + cfg.password
	if p.c != nil && (p.key != key || p.c.State() == goimap.LogoutState) {
		_ = p.c.Terminate()
		p.c = nil
	}
	if p.c != nil && p.c.Noop() != nil {
		_ = p.c.Terminate()
		p.c = nil
	}
	if p.c == nil {
		c, err := dialIMAP(ctx, cfg)
		if err != nil {
			return err
		}
		p.c, p.key = c, key
	}
	c := p.c
	stop := context.AfterFunc(ctx, func() { _ = c.Terminate() })
	err = fn(c, cfg)
	if !stop() || errors.Is(err, client.ErrNotLoggedIn) {
		_ = c.Terminate()
		p.c = nil
	}
	if p.timer != nil {
		p.timer.Stop()
	}
	p.timer = time.AfterFunc(poolIdle, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.c == c {
			_ = c.Logout()
			p.c = nil
		}
	})
	return err
}

var quiet = log.New(io.Discard, "", 0)

func netErr(err error) error {
	if err == nil {
		return nil
	}
	var said *connectors.Said
	if errors.As(err, &said) || errors.Is(err, connectors.ErrBadArgs) || errors.Is(err, connectors.ErrNotFound) {
		return err
	}
	return fmt.Errorf("mailbox: %w", err)
}
