package imap

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend"
	"github.com/emersion/go-imap/backend/memory"
	"github.com/emersion/go-imap/server"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

// A local IMAP server (go-imap's memory backend, user "username" / "password") and a local SMTP server, both TLS with
// a certificate of the test's own CA: no real mailbox is ever touched.

type fake struct {
	t        *testing.T
	be       *memory.Backend
	user     backend.User
	imapAddr string
	smtpAddr string
	mu       sync.Mutex
	sent     []sentMail
	messages func(folder string) []*memory.Message
}

type sentMail struct {
	from string
	to   []string
	data []byte
}

func selfSigned(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

func newFake(t *testing.T) *fake {
	t.Helper()
	cert, ca := selfSigned(t)
	prevTLS, prevSettle, prevIdle := tlsConfig, sentSettle, poolIdle
	tlsConfig = func(host string) *tls.Config {
		return &tls.Config{ServerName: host, RootCAs: ca, MinVersion: tls.VersionTLS12}
	}
	sentSettle = 0
	t.Cleanup(func() {
		tlsConfig, sentSettle, poolIdle = prevTLS, prevSettle, prevIdle
		poolMu.Lock()
		for id, p := range pool {
			p.mu.Lock()
			if p.c != nil {
				_ = p.c.Terminate()
			}
			p.mu.Unlock()
			delete(pool, id)
		}
		poolMu.Unlock()
	})
	srvTLS := &tls.Config{Certificates: []tls.Certificate{cert}}

	f := &fake{t: t, be: memory.New()}
	u, err := f.be.Login(nil, "username", "password")
	if err != nil {
		t.Fatal(err)
	}
	f.user = u
	for _, name := range []string{"Sent", "Drafts", "Archive"} {
		if err := u.CreateMailbox(name); err != nil {
			t.Fatal(err)
		}
	}
	inbox, _ := u.GetMailbox("INBOX")
	inbox.(*memory.Mailbox).Messages = nil
	f.messages = func(name string) []*memory.Message {
		memMu.Lock()
		defer memMu.Unlock()
		return append([]*memory.Message(nil), f.mailbox(name).Messages...)
	}

	il, err := tls.Listen("tcp", "127.0.0.1:0", srvTLS)
	if err != nil {
		t.Fatal(err)
	}
	is := server.New(lockedBackend{f.be})
	is.ErrorLog = discardLog{}
	go func() { _ = is.Serve(il) }()
	t.Cleanup(func() { _ = is.Close() })
	f.imapAddr = il.Addr().String()

	sl, err := tls.Listen("tcp", "127.0.0.1:0", srvTLS)
	if err != nil {
		t.Fatal(err)
	}
	ss := smtp.NewServer(smtpBackend{f})
	ss.Domain = "localhost"
	ss.ErrorLog = discardLog{}
	go func() { _ = ss.Serve(sl) }()
	t.Cleanup(func() { _ = ss.Close() })
	f.smtpAddr = sl.Addr().String()
	return f
}

type discardLog struct{}

func (discardLog) Printf(string, ...any) {}
func (discardLog) Println(...any)        {}

func (f *fake) instance(id string) connectors.Instance {
	return connectors.Instance{ID: id, Connector: ID, Name: "me",
		Config: map[string]string{"email": "me@example.com", "login": "username", "provider": "other",
			"imapServer": f.imapAddr, "smtpServer": f.smtpAddr},
		Secret: func(field string) (string, error) {
			if field == "password" {
				return "password", nil
			}
			return "", errors.New("no such secret")
		}}
}

func (f *fake) mailbox(name string) *memory.Mailbox {
	m, err := f.user.GetMailbox(name)
	if err != nil {
		f.t.Fatal(err)
	}
	return m.(*memory.Mailbox)
}

// deliver puts a raw letter into a folder, as the mail server does on arrival.
func (f *fake) deliver(folder, raw string) uint32 {
	memMu.Lock()
	defer memMu.Unlock()
	mb := f.mailbox(folder)
	if err := mb.CreateMessage(nil, time.Now(), literal([]byte(raw))); err != nil {
		f.t.Fatal(err)
	}
	return mb.Messages[len(mb.Messages)-1].Uid
}

func (f *fake) sentMails() []sentMail {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentMail(nil), f.sent...)
}

type smtpBackend struct{ f *fake }

func (b smtpBackend) NewSession(*smtp.Conn) (smtp.Session, error) { return &smtpSession{f: b.f}, nil }

type smtpSession struct {
	f      *fake
	authed bool
	from   string
	to     []string
}

func (s *smtpSession) AuthMechanisms() []string { return []string{sasl.Plain} }

func (s *smtpSession) Auth(string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(_, user, pass string) error {
		if user != "username" || pass != "password" {
			return errors.New("bad credentials")
		}
		s.authed = true
		return nil
	}), nil
}

func (s *smtpSession) Mail(from string, _ *smtp.MailOptions) error {
	if !s.authed {
		return smtp.ErrAuthRequired
	}
	s.from = from
	return nil
}

func (s *smtpSession) Rcpt(to string, _ *smtp.RcptOptions) error {
	s.to = append(s.to, to)
	return nil
}

func (s *smtpSession) Data(r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.f.mu.Lock()
	s.f.sent = append(s.f.sent, sentMail{from: s.from, to: s.to, data: b})
	s.f.mu.Unlock()
	return nil
}

func (s *smtpSession) Reset()        { s.from, s.to = "", nil }
func (s *smtpSession) Logout() error { return nil }

// locked serialises go-imap's memory backend, which has no locks of its own: the watcher's connection, the reads'
// connection and the test's deliveries touch it at once.
var memMu sync.Mutex

type lockedBackend struct{ be *memory.Backend }

func (b lockedBackend) Login(ci *goimap.ConnInfo, user, pass string) (backend.User, error) {
	memMu.Lock()
	defer memMu.Unlock()
	u, err := b.be.Login(ci, user, pass)
	if err != nil {
		return nil, err
	}
	return lockedUser{u}, nil
}

type lockedUser struct{ u backend.User }

func (u lockedUser) Username() string { return u.u.Username() }
func (u lockedUser) ListMailboxes(sub bool) ([]backend.Mailbox, error) {
	memMu.Lock()
	defer memMu.Unlock()
	list, err := u.u.ListMailboxes(sub)
	out := make([]backend.Mailbox, len(list))
	for i, m := range list {
		out[i] = lockedMailbox{m}
	}
	return out, err
}
func (u lockedUser) GetMailbox(name string) (backend.Mailbox, error) {
	memMu.Lock()
	defer memMu.Unlock()
	m, err := u.u.GetMailbox(name)
	if err != nil {
		return nil, err
	}
	return lockedMailbox{m}, nil
}
func (u lockedUser) CreateMailbox(name string) error {
	memMu.Lock()
	defer memMu.Unlock()
	return u.u.CreateMailbox(name)
}
func (u lockedUser) DeleteMailbox(name string) error {
	memMu.Lock()
	defer memMu.Unlock()
	return u.u.DeleteMailbox(name)
}
func (u lockedUser) RenameMailbox(a, b string) error {
	memMu.Lock()
	defer memMu.Unlock()
	return u.u.RenameMailbox(a, b)
}
func (u lockedUser) Logout() error { return nil }

type lockedMailbox struct{ m backend.Mailbox }

func (m lockedMailbox) Name() string { return m.m.Name() }
func (m lockedMailbox) Info() (*goimap.MailboxInfo, error) {
	memMu.Lock()
	defer memMu.Unlock()
	return m.m.Info()
}
func (m lockedMailbox) Status(items []goimap.StatusItem) (*goimap.MailboxStatus, error) {
	memMu.Lock()
	defer memMu.Unlock()
	return m.m.Status(items)
}
func (m lockedMailbox) SetSubscribed(s bool) error {
	memMu.Lock()
	defer memMu.Unlock()
	return m.m.SetSubscribed(s)
}
func (m lockedMailbox) Check() error { return nil }
func (m lockedMailbox) ListMessages(uid bool, set *goimap.SeqSet, items []goimap.FetchItem, ch chan<- *goimap.Message) error {
	// collected under the lock, sent after it: the server reads ch while the lock is free
	inner := make(chan *goimap.Message, 1024)
	memMu.Lock()
	var got []*goimap.Message
	done := make(chan struct{})
	go func() {
		for x := range inner {
			got = append(got, x)
		}
		close(done)
	}()
	err := m.m.ListMessages(uid, set, items, inner)
	memMu.Unlock()
	<-done
	for _, x := range got {
		ch <- x
	}
	close(ch)
	return err
}
func (m lockedMailbox) SearchMessages(uid bool, c *goimap.SearchCriteria) ([]uint32, error) {
	memMu.Lock()
	defer memMu.Unlock()
	return m.m.SearchMessages(uid, c)
}
func (m lockedMailbox) CreateMessage(flags []string, date time.Time, body goimap.Literal) error {
	memMu.Lock()
	defer memMu.Unlock()
	return m.m.CreateMessage(flags, date, body)
}
func (m lockedMailbox) UpdateMessagesFlags(uid bool, set *goimap.SeqSet, op goimap.FlagsOp, flags []string) error {
	memMu.Lock()
	defer memMu.Unlock()
	return m.m.UpdateMessagesFlags(uid, set, op, flags)
}
func (m lockedMailbox) CopyMessages(uid bool, set *goimap.SeqSet, dest string) error {
	memMu.Lock()
	defer memMu.Unlock()
	return m.m.CopyMessages(uid, set, dest)
}
func (m lockedMailbox) Expunge() error {
	memMu.Lock()
	defer memMu.Unlock()
	return m.m.Expunge()
}

func (m lockedMailbox) MoveMessages(uid bool, set *goimap.SeqSet, dest string) error {
	memMu.Lock()
	defer memMu.Unlock()
	if err := m.m.CopyMessages(uid, set, dest); err != nil {
		return err
	}
	if err := m.m.UpdateMessagesFlags(uid, set, goimap.AddFlags, []string{goimap.DeletedFlag}); err != nil {
		return err
	}
	return m.m.Expunge()
}
