// Package mail sends the hub's two kinds of mail, sign-in codes and invitations, over SMTP, or writes them to the log
// when no SMTP server is configured (a single-host installation).
package mail

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

// Mailer sends the hub's mail.
type Mailer interface {
	// Mode is "smtp" when mail leaves the hub and "log" when it only reaches the hub's log.
	Mode() string
	// SendCode mails a six-digit sign-in code.
	SendCode(ctx context.Context, to, code string, ttl time.Duration) error
	// SendInvite mails an invitation link. sent is false when nothing left the hub (the log mailer).
	SendInvite(ctx context.Context, to, inviter, space, link string, expires time.Time) (sent bool, err error)
}

// Log writes mail to the hub's log instead of sending it. Every line is marked DEV: it prints a sign-in code, which
// is only acceptable on a single host where the person reading the log is the person signing in. The hub does not
// build it for an https hub or the cloud edition unless the operator said HUB_MAIL_LOG=allow (config.MailLogRefused).
type Log struct{ Logger *slog.Logger }

func (Log) Mode() string { return "log" }

func (l Log) logger() *slog.Logger {
	if l.Logger != nil {
		return l.Logger
	}
	return slog.Default()
}

func (l Log) SendCode(_ context.Context, to, code string, ttl time.Duration) error {
	l.logger().Warn("DEV email code (no HUB_SMTP_URL is set, so the code is not mailed)", "to", to, "code", code, "validFor", ttl.String())
	return nil
}

func (l Log) SendInvite(_ context.Context, to, inviter, space, link string, expires time.Time) (bool, error) {
	l.logger().Warn("DEV invitation (no HUB_SMTP_URL is set, so it is not mailed)", "to", to, "inviter", inviter, "space", space, "link", link, "expires", expires.UTC().Format(time.RFC3339))
	return false, nil
}

// SMTP sends through an SMTP server: smtp://[user:pass@]host[:587] with STARTTLS, smtps://…:465 with implicit TLS.
type SMTP struct {
	addr     string
	host     string
	implicit bool
	user     string
	pass     string
	from     *mail.Address
	timeout  time.Duration
	// insecureLocal is set for a loopback server, where no STARTTLS is required (tests, a local relay).
	insecureLocal bool
	// tlsConfig is for tests; nil means the system roots.
	tlsConfig *tls.Config
}

// NewSMTP parses the URL and the From header ("harnessly <no-reply@example.com>").
func NewSMTP(rawURL, from string) (*SMTP, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "smtp" && u.Scheme != "smtps") || u.Hostname() == "" {
		return nil, errors.New("smtp url must be smtp://[user:pass@]host[:port] or smtps://[user:pass@]host[:port]")
	}
	addr, err := mail.ParseAddress(from)
	if err != nil {
		return nil, fmt.Errorf("mail from: %w", err)
	}
	s := &SMTP{host: u.Hostname(), implicit: u.Scheme == "smtps", from: addr, timeout: 20 * time.Second}
	port := u.Port()
	if port == "" {
		port = "587"
		if s.implicit {
			port = "465"
		}
	}
	s.addr = net.JoinHostPort(s.host, port)
	if u.User != nil {
		s.user = u.User.Username()
		s.pass, _ = u.User.Password()
	}
	if ip := net.ParseIP(s.host); s.host == "localhost" || (ip != nil && ip.IsLoopback()) {
		s.insecureLocal = true
	}
	return s, nil
}

func (*SMTP) Mode() string { return "smtp" }

func (s *SMTP) SendCode(ctx context.Context, to, code string, ttl time.Duration) error {
	minutes := int(ttl.Minutes())
	body := fmt.Sprintf("Ваш код для входа в harnessly: %s\r\n\r\nОн действует %d мин. Если вы не запрашивали код, просто проигнорируйте это письмо.\r\n", code, minutes)
	return s.send(ctx, to, "Код для входа в harnessly: "+code, body)
}

func (s *SMTP) SendInvite(ctx context.Context, to, inviter, space, link string, expires time.Time) (bool, error) {
	inviter, space = clean(inviter), clean(space)
	body := fmt.Sprintf("%s приглашает вас в пространство «%s» в harnessly.\r\n\r\nПринять приглашение: %s\r\n\r\nСсылка действует до %s и подходит один раз.\r\n",
		inviter, space, link, russianDate(expires))
	err := s.send(ctx, to, fmt.Sprintf("%s приглашает вас в «%s» в harnessly", inviter, space), body)
	return err == nil, err
}

// clean removes what could break a header or a line of text.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == 0 {
			return ' '
		}
		return r
	}, s)
}

func (s *SMTP) send(ctx context.Context, to, subject, body string) error {
	rcpt, err := mail.ParseAddress(to)
	if err != nil || strings.ContainsAny(to, "\r\n") {
		return fmt.Errorf("mail: bad recipient %q", to)
	}
	msg := s.message(rcpt.Address, subject, body)

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	cfg := s.tlsConfig
	if cfg == nil {
		cfg = &tls.Config{}
	} else {
		cfg = cfg.Clone()
	}
	cfg.ServerName = s.host
	if s.implicit {
		conn = tls.Client(conn, cfg)
	}
	c, err := smtp.NewClient(conn, s.host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("mail: %w", err)
	}
	defer c.Close()
	if !s.implicit {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(cfg); err != nil {
				return fmt.Errorf("mail: starttls: %w", err)
			}
		} else if !s.insecureLocal {
			return errors.New("mail: the server does not offer STARTTLS; use smtps:// or a server that does")
		}
	}
	if s.user != "" {
		if err := c.Auth(smtp.PlainAuth("", s.user, s.pass, s.host)); err != nil {
			return fmt.Errorf("mail: auth: %w", err)
		}
	}
	if err := c.Mail(s.from.Address); err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	if err := c.Rcpt(rcpt.Address); err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	return c.Quit()
}

func (s *SMTP) message(to, subject, body string) []byte {
	var id [12]byte
	_, _ = rand.Read(id[:])
	domain := "localhost"
	if _, d, ok := strings.Cut(s.from.Address, "@"); ok {
		domain = d
	}
	var b strings.Builder
	h := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	h("From", (&mail.Address{Name: s.from.Name, Address: s.from.Address}).String())
	h("To", to)
	h("Subject", encodeHeader(clean(subject)))
	h("Date", time.Now().UTC().Format(time.RFC1123Z))
	h("Message-ID", "<"+base64.RawURLEncoding.EncodeToString(id[:])+"@"+domain+">")
	h("MIME-Version", "1.0")
	h("Content-Type", "text/plain; charset=UTF-8")
	h("Content-Transfer-Encoding", "base64")
	h("Auto-Submitted", "auto-generated")
	b.WriteString("\r\n")
	enc := base64.StdEncoding.EncodeToString([]byte(body))
	for len(enc) > 76 {
		b.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc + "\r\n")
	return []byte(b.String())
}

// encodeHeader encodes a header value as RFC 2047 when it is not plain ASCII.
func encodeHeader(v string) string { return mime.BEncoding.Encode("UTF-8", v) }

var months = [...]string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}

func russianDate(t time.Time) string {
	t = t.UTC()
	return fmt.Sprintf("%d %s %d", t.Day(), months[t.Month()-1], t.Year())
}
