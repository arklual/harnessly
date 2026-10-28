package mail

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/mail"
	"strings"
	"testing"
	"time"
)

// fakeSMTP is a minimal SMTP server on loopback without TLS; it records each message.
type fakeSMTP struct {
	ln       net.Listener
	messages chan received
}

type received struct {
	from, to, auth string
	data           string
}

func newFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{ln: ln, messages: make(chan received, 4)}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return f
}

func (f *fakeSMTP) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	say := func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
	say("220 fake ESMTP")
	var msg received
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		switch cmd := strings.ToUpper(line); {
		case strings.HasPrefix(cmd, "EHLO"):
			say("250-fake")
			say("250 AUTH PLAIN")
		case strings.HasPrefix(cmd, "AUTH PLAIN"):
			raw, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(line[len("AUTH PLAIN"):]))
			msg.auth = strings.ReplaceAll(string(raw), "\x00", "|")
			say("235 ok")
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			msg.from = line[len("MAIL FROM:"):]
			say("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			msg.to = line[len("RCPT TO:"):]
			say("250 ok")
		case cmd == "DATA":
			say("354 go")
			var buf bytes.Buffer
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				buf.WriteString(l)
			}
			msg.data = buf.String()
			say("250 queued")
			f.messages <- msg
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("250 ok")
		}
	}
}

func (f *fakeSMTP) url(user string) string {
	return "smtp://" + user + f.ln.Addr().String()
}

func bodyOf(t *testing.T, raw string) (header mail.Header, text string) {
	t.Helper()
	m, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(m.Body)
	dec, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(strings.ReplaceAll(string(b), "\r\n", ""), "\n", ""))
	if err != nil {
		t.Fatalf("body is not base64: %v", err)
	}
	return m.Header, string(dec)
}

func TestSMTPSendsCodeAndInvite(t *testing.T) {
	f := newFakeSMTP(t)
	s, err := NewSMTP(f.url("hubuser:s3cret@"), "harnessly <no-reply@hub.example.com>")
	if err != nil {
		t.Fatal(err)
	}
	if s.Mode() != "smtp" {
		t.Error(s.Mode())
	}
	if err := s.SendCode(context.Background(), "Ann@Example.com", "123456", 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	got := <-f.messages
	if got.auth != "|hubuser|s3cret" || !strings.Contains(got.from, "no-reply@hub.example.com") || !strings.Contains(got.to, "Ann@Example.com") {
		t.Errorf("envelope: %+v", got)
	}
	h, text := bodyOf(t, got.data)
	subject, _ := (&mime.WordDecoder{}).DecodeHeader(h.Get("Subject"))
	if subject != "Код для входа в harnessly: 123456" || !strings.Contains(text, "123456") || !strings.Contains(text, "10 мин") {
		t.Errorf("subject %q body %q", subject, text)
	}
	if h.Get("Content-Type") != "text/plain; charset=UTF-8" || h.Get("Auto-Submitted") == "" {
		t.Errorf("headers: %v", h)
	}

	sent, err := s.SendInvite(context.Background(), "bob@example.com", "Анна\r\nBcc: evil@example.com", "Команда «А»", "https://hub.example.com/invite/tok", time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC))
	if err != nil || !sent {
		t.Fatalf("invite: %v %v", sent, err)
	}
	got = <-f.messages
	h, text = bodyOf(t, got.data)
	if h.Get("Bcc") != "" || strings.Contains(got.data, "\r\nBcc:") {
		t.Errorf("header injection: %v", h)
	}
	if !strings.Contains(text, "https://hub.example.com/invite/tok") || !strings.Contains(text, "2 ноября 2026") || !strings.Contains(text, "«Команда «А»»") {
		t.Errorf("invite text %q", text)
	}
}

func TestSMTPRefusesBadRecipient(t *testing.T) {
	f := newFakeSMTP(t)
	s, _ := NewSMTP(f.url(""), "no-reply@hub.example.com")
	for _, to := range []string{"not an address", "a@example.com\r\nRCPT TO:<b@example.com>", ""} {
		if err := s.SendCode(context.Background(), to, "1", time.Minute); err == nil {
			t.Errorf("%q was accepted", to)
		}
	}
}

func TestSMTPNeedsSTARTTLSOffLoopback(t *testing.T) {
	f := newFakeSMTP(t)
	s, _ := NewSMTP(f.url(""), "no-reply@hub.example.com")
	s.insecureLocal = false // as if the server were remote: the fake does not offer STARTTLS
	err := s.SendCode(context.Background(), "a@example.com", "1", time.Minute)
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Errorf("credentials would have gone in clear: %v", err)
	}
}

func TestNewSMTPErrors(t *testing.T) {
	for _, u := range []string{"http://mail", "smtp://", "://x"} {
		if _, err := NewSMTP(u, "a@example.com"); err == nil {
			t.Errorf("%q accepted", u)
		}
	}
	if _, err := NewSMTP("smtp://mail:25", "not an address"); err == nil {
		t.Error("bad from accepted")
	}
	s, err := NewSMTP("smtps://u:p@mail.example.com", "a@example.com")
	if err != nil || s.addr != "mail.example.com:465" || !s.implicit {
		t.Errorf("smtps: %+v %v", s, err)
	}
	s, _ = NewSMTP("smtp://mail.example.com", "a@example.com")
	if s.addr != "mail.example.com:587" || s.insecureLocal {
		t.Errorf("smtp: %+v", s)
	}
}

func TestLogMarksDev(t *testing.T) {
	var buf bytes.Buffer
	l := Log{Logger: slog.New(slog.NewTextHandler(&buf, nil))}
	if l.Mode() != "log" {
		t.Error(l.Mode())
	}
	if err := l.SendCode(context.Background(), "a@example.com", "654321", 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	sent, err := l.SendInvite(context.Background(), "b@example.com", "Ann", "Acme", "https://h/invite/x", time.Now())
	if err != nil || sent {
		t.Errorf("invite: %v %v", sent, err)
	}
	out := buf.String()
	if !strings.Contains(out, "DEV email code") || !strings.Contains(out, "code=654321") || !strings.Contains(out, "DEV invitation") || !strings.Contains(out, "https://h/invite/x") {
		t.Errorf("log: %s", out)
	}
}
