package imap

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap"
	"golang.org/x/text/encoding/charmap"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

func letterRaw(id, from, subject, extra, body string) string {
	return "From: " + from + "\r\nTo: me@example.com\r\nSubject: " + subject + "\r\nDate: Mon, 02 Nov 2026 10:00:00 +0000\r\n" +
		"Message-ID: <" + id + ">\r\n" + extra + "Content-Type: text/plain; charset=utf-8\r\n\r\n" + body + "\r\n"
}

func TestSpecRegistered(t *testing.T) {
	c, ok := connectors.Lookup(ID)
	if !ok {
		t.Fatal("imap is not registered")
	}
	sp := c.Spec()
	if sp.Kind != connectors.Mail || sp.Inbox != "mail" || len(sp.Actions) != 4 {
		t.Fatalf("spec: %+v", sp)
	}
}

func TestCheckSignsInToBothAndSaysWhyNot(t *testing.T) {
	f := newFake(t)
	in := f.instance("cx1")
	acc, err := mailConn{}.Check(context.Background(), in)
	if err != nil || acc.Label != "me@example.com" {
		t.Fatalf("check: %v %+v", err, acc)
	}
	if len(f.sentMails()) != 0 {
		t.Fatal("check sent a letter")
	}
	in.Secret = func(string) (string, error) { return "wrong", nil }
	_, err = mailConn{}.Check(context.Background(), in)
	var said *connectors.Said
	if !errors.As(err, &said) || !strings.Contains(said.Message, "did not let in") || strings.Contains(said.Message, "wrong") {
		t.Fatalf("wrong password: %v", err)
	}
	in.Config["imapServer"] = "127.0.0.1:1"
	if _, err := (mailConn{}).Check(context.Background(), in); !errors.As(err, &said) || !strings.Contains(said.Message, "does not answer") {
		t.Fatalf("no server: %v", err)
	}
}

func TestServersByPresetDomainAndManual(t *testing.T) {
	in := connectors.Instance{Config: map[string]string{}}
	i, s, err := servers(context.Background(), in, "ann@yandex.ru", "auto")
	if err != nil || i.host != "imap.yandex.ru" || s.port != 465 {
		t.Fatalf("yandex: %+v %+v %v", i, s, err)
	}
	i, s, _ = servers(context.Background(), in, "ann@example.org", "icloud")
	if i.host != "imap.mail.me.com" || !s.startTLS {
		t.Fatalf("icloud: %+v %+v", i, s)
	}
	in.Config = map[string]string{"imapServer": "mx.example.org", "smtpServer": "mx.example.org:587"}
	i, s, _ = servers(context.Background(), in, "ann@example.org", "other")
	if i.addr() != "mx.example.org:993" || !s.startTLS || s.port != 587 {
		t.Fatalf("manual: %+v %+v", i, s)
	}
}

func TestDiscoverByAutoconfigThenSRV(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`<clientConfig version="1.1"><emailProvider id="example.net">
<incomingServer type="pop3"><hostname>pop.example.net</hostname><port>995</port><socketType>SSL</socketType></incomingServer>
<incomingServer type="imap"><hostname>imap.example.net</hostname><port>143</port><socketType>plain</socketType></incomingServer>
<incomingServer type="imap"><hostname>imap.example.net</hostname><port>993</port><socketType>SSL</socketType></incomingServer>
<outgoingServer type="smtp"><hostname>smtp.example.net</hostname><port>587</port><socketType>STARTTLS</socketType></outgoingServer>
</emailProvider></clientConfig>`))
	}))
	defer ts.Close()
	prevURLs, prevSRV := autoconfigURLs, lookupSRV
	defer func() { autoconfigURLs, lookupSRV = prevURLs, prevSRV }()
	autoconfigURLs = func(domain, _ string) []string {
		if domain == "example.net" {
			return []string{ts.URL}
		}
		return []string{ts.URL + "/missing"}
	}
	in := connectors.Instance{HTTP: ts.Client()}
	i, s, ok := discover(context.Background(), in, "a@example.net", "example.net")
	if !ok || i.addr() != "imap.example.net:993" || s.host != "smtp.example.net" || !s.startTLS {
		t.Fatalf("autoconfig: %+v %+v %v", i, s, ok)
	}
	lookupSRV = func(_ context.Context, service, _, _ string) (string, []*net.SRV, error) {
		switch service {
		case "imaps":
			return "", []*net.SRV{{Target: "imap.example.edu.", Port: 993}}, nil
		case "submissions":
			return "", []*net.SRV{{Target: "smtp.example.edu.", Port: 465}}, nil
		}
		return "", nil, errors.New("none")
	}
	ts2 := httptest.NewServer(http.NotFoundHandler())
	defer ts2.Close()
	in.HTTP = ts2.Client()
	i, s, ok = discover(context.Background(), in, "a@example.edu", "example.edu")
	if !ok || i.host != "imap.example.edu" || s.host != "smtp.example.edu" || s.startTLS {
		t.Fatalf("srv: %+v %+v %v", i, s, ok)
	}
	// A record naming a stranger's host does not get the password.
	if _, _, ok := discover(context.Background(), in, "a@example.com", "example.com"); ok {
		t.Fatal("srv for example.com pointed at example.edu and was followed")
	}
	if !trusted("imap.gmail.com", "example.org") || !trusted("p12-imap.mail.me.com", "example.org") || trusted("imap.example.net", "example.org") {
		t.Fatal("trusted")
	}
}

type recorder struct {
	mu      sync.Mutex
	batches [][]connectors.Event
	cur     connectors.Cursor
	got     chan struct{}
}

func newRecorder() *recorder { return &recorder{got: make(chan struct{}, 100)} }

func (r *recorder) emit(events []connectors.Event, next connectors.Cursor) error {
	r.mu.Lock()
	r.batches = append(r.batches, events)
	r.cur = next
	r.mu.Unlock()
	r.got <- struct{}{}
	return nil
}

func (r *recorder) events() []connectors.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []connectors.Event
	for _, b := range r.batches {
		out = append(out, b...)
	}
	return out
}

func (r *recorder) cursor() connectors.Cursor {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cur
}

func (r *recorder) waitFor(t *testing.T, ok func([]connectors.Event) bool) []connectors.Event {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		if ev := r.events(); ok(ev) {
			return ev
		}
		select {
		case <-r.got:
		case <-deadline:
			t.Fatalf("timed out; events: %+v", r.events())
		}
	}
}

func fastWatch(t *testing.T) {
	debugf = func(string, ...any) {}
	if os.Getenv("IMAP_DEBUG") != "" {
		debugw = os.Stderr
	}
	prevB := backoffMin
	backoffMin = 50 * time.Millisecond
	t.Cleanup(func() { debugf, debugw, backoffMin = nil, nil, prevB })
	prev, prevPoll := recheck, pollEvery
	recheck, pollEvery = 150*time.Millisecond, 100*time.Millisecond
	t.Cleanup(func() { recheck, pollEvery = prev, prevPoll })
}

func run(t *testing.T, in connectors.Instance, from connectors.Cursor, r *recorder) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- mailConn{}.Watch(ctx, in, from, r.emit) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("watch: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Error("watch did not stop")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func TestWatchLearnsThenEmitsOnceAndKnowsPeopleFromMailings(t *testing.T) {
	fastWatch(t)
	f := newFake(t)
	in := f.instance("cx2")
	f.deliver("INBOX", letterRaw("old@example.org", "Ann <ann@example.org>", "Old", "", "old"))

	r := newRecorder()
	stop := run(t, in, "", r)
	r.waitFor(t, func([]connectors.Event) bool { return r.cursor() != "" })
	if len(r.events()) != 0 {
		t.Fatalf("the first run emitted %+v", r.events())
	}
	f.deliver("INBOX", letterRaw("p1@example.org", "Ann <ann@example.org>", "=?koi8-r?B?"+base64.StdEncoding.EncodeToString(koi8("Договор"))+"?=", "", "please"))
	f.deliver("INBOX", letterRaw("n1@example.org", "News <news@example.org>", "Digest", "List-Unsubscribe: <https://example.org/u>\r\n", "news"))
	f.deliver("INBOX", letterRaw("r1@example.org", "Robot <no-reply@example.org>", "Your code", "", "123"))
	ev := r.waitFor(t, func(e []connectors.Event) bool { return len(e) >= 3 })
	stop()

	if ev[0].Type != EventReceived || ev[0].ID != "p1@example.org" || ev[0].Title != "Договор" || ev[0].Facts["person"] != "true" ||
		ev[0].Ref != "message id=p1@example.org" || ev[0].Thread != "p1@example.org" || ev[0].Facts["unread"] != "true" {
		t.Fatalf("person: %+v", ev[0])
	}
	if ev[1].Facts["person"] != "false" || ev[1].Facts["list"] != "true" || ev[2].Facts["person"] != "false" {
		t.Fatalf("mailing and robot: %+v %+v", ev[1], ev[2])
	}
	cur, _ := readCursor(r.cursor())
	if len(cur.Wait) != 1 || cur.Wait[0].ID != "p1@example.org" {
		t.Fatalf("only the person's letter waits: %+v", cur.Wait)
	}

	// After a restart from the kept cursor: nothing again.
	r2 := newRecorder()
	stop = run(t, in, r.cursor(), r2)
	time.Sleep(500 * time.Millisecond)
	stop()
	if n := len(r2.events()); n != 0 {
		t.Fatalf("a restart emitted %d events: %+v", n, r2.events())
	}
}

func TestWatchAnswerEndsTheWaitAndSilenceIsUnanswered(t *testing.T) {
	fastWatch(t)
	f := newFake(t)
	in := f.instance("cx3")
	r := newRecorder()
	stop := run(t, in, "", r)
	r.waitFor(t, func([]connectors.Event) bool { return r.cursor() != "" })
	f.deliver("INBOX", letterRaw("q1@example.org", "Ann <ann@example.org>", "Question one", "", "?"))
	f.deliver("INBOX", letterRaw("q2@example.org", "Bob <bob@example.org>", "Question two", "", "?"))
	r.waitFor(t, func(e []connectors.Event) bool { return len(e) >= 2 })
	// The person answers Ann from their phone: the letter shows up in Sent.
	f.deliver("Sent", "From: me@example.com\r\nTo: ann@example.org\r\nSubject: Re: Question one\r\nMessage-ID: <a1@example.com>\r\n"+
		"In-Reply-To: <q1@example.org>\r\nReferences: <q1@example.org>\r\n\r\nyes\r\n")
	ev := r.waitFor(t, func(e []connectors.Event) bool { return len(e) >= 3 })
	if ev[2].Type != EventReplied || !ev[2].Mine || ev[2].Thread != "q1@example.org" {
		t.Fatalf("replied: %+v", ev[2])
	}
	stop()
	// Four days later Bob still waits.
	prev := now
	now = func() time.Time { return time.Now().Add(4 * 24 * time.Hour) }
	defer func() { now = prev }()
	r2 := newRecorder()
	stop = run(t, in, r.cursor(), r2)
	got := r2.waitFor(t, func(e []connectors.Event) bool { return len(e) >= 1 })
	stop()
	if len(got) != 1 || got[0].Type != EventUnanswered || got[0].Thread != "q2@example.org" || got[0].Ref != "message id=q2@example.org" || got[0].Facts["days"] != "3" {
		t.Fatalf("unanswered: %+v", got)
	}
	cur, _ := readCursor(r2.cursor())
	if len(cur.Wait) != 0 {
		t.Fatalf("waits left: %+v", cur.Wait)
	}
}

func koi8(s string) []byte {
	b, _ := charmap.KOI8R.NewEncoder().Bytes([]byte(s))
	return b
}

func cp1251(s string) []byte {
	b, _ := charmap.Windows1251.NewEncoder().Bytes([]byte(s))
	return b
}

func TestParseCharsetsNestedMultipartAndBrokenLetters(t *testing.T) {
	raw := "From: =?windows-1251?B?" + base64.StdEncoding.EncodeToString(cp1251("Иван")) + "?= <ivan@example.org>\r\n" +
		"Subject: =?koi8-r?B?" + base64.StdEncoding.EncodeToString(koi8("Счёт за ноябрь")) + "?=\r\nMessage-ID: <m1@example.org>\r\n" +
		"Content-Type: multipart/mixed; boundary=outer\r\n\r\n" +
		"--outer\r\nContent-Type: multipart/alternative; boundary=inner\r\n\r\n" +
		"--inner\r\nContent-Type: text/plain; charset=koi8-r\r\nContent-Transfer-Encoding: 8bit\r\n\r\n" + string(koi8("Добрый день, счёт во вложении.")) + "\r\n" +
		"--inner\r\nContent-Type: text/html; charset=windows-1251\r\n\r\n<p>" + string(cp1251("Добрый день")) + "</p>\r\n" +
		"--inner--\r\n" +
		"--outer\r\nContent-Type: application/pdf; name=\"=?windows-1251?B?" + base64.StdEncoding.EncodeToString(cp1251("счёт.pdf")) + "?=\"\r\n" +
		"Content-Disposition: attachment\r\nContent-Transfer-Encoding: base64\r\n\r\n" + base64.StdEncoding.EncodeToString([]byte("%PDF-1.4 fake")) + "\r\n" +
		"--outer\r\nContent-Type: text/plain; charset=x-unknown-charset\r\nContent-Disposition: attachment; filename=\"../../etc/passwd\"\r\n\r\nroot\r\n" +
		"--outer--\r\n"
	l, err := parseLetter(strings.NewReader(raw), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if l.Subject != "Счёт за ноябрь" || !strings.Contains(l.From, "Иван") || l.Text != "Добрый день, счёт во вложении." || l.HTML {
		t.Fatalf("letter: %q %q %q", l.Subject, l.From, l.Text)
	}
	if len(l.Parts) != 2 || l.Parts[0].Name != "счёт.pdf" || l.Parts[1].Name != "passwd" {
		t.Fatalf("parts: %+v", l.Parts)
	}
	var buf bytes.Buffer
	if _, err := parseLetter(strings.NewReader(raw), 1, &buf); err != nil || buf.String() != "%PDF-1.4 fake" {
		t.Fatalf("attachment: %v %q", err, buf.String())
	}

	html := "Subject: x\r\nContent-Type: text/html; charset=windows-1251\r\n\r\n<html><head><style>p{}</style></head><body><p>" +
		string(cp1251("Привет")) + "</p><script>alert(1)</script><div>&amp; ещё</div></body></html>"
	l, _ = parseLetter(strings.NewReader(html), 0, nil)
	if !l.HTML || !strings.Contains(l.Text, "Привет") || strings.Contains(l.Text, "alert") || strings.Contains(l.Text, "<") {
		t.Fatalf("html: %q", l.Text)
	}

	cutOff := "Subject: cut\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/plain\r\n\r\nbeginning of"
	l, err = parseLetter(strings.NewReader(cutOff), 0, nil)
	if err != nil || !strings.Contains(l.Text, "beginning") {
		t.Fatalf("cut: %v %+v", err, l)
	}

	if _, err := parseLetter(strings.NewReader("\x00\x01not a letter at all"), 0, nil); err == nil {
		// a header-less blob is read as a letter without headers; it must not panic
		t.Log("blob read as a letter")
	}

	deep := "Subject: deep\r\n"
	for i := range 40 {
		deep += "Content-Type: multipart/mixed; boundary=b" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + "\r\n\r\n--b" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + "\r\n"
	}
	deep += "Content-Type: text/plain\r\n\r\nbottom\r\n"
	if l, err := parseLetter(strings.NewReader(deep), 0, nil); err != nil || !l.Broken {
		t.Fatalf("deep: %v %+v", err, l)
	}
}

func TestReadsListMessageAttachmentThreadAndDraft(t *testing.T) {
	f := newFake(t)
	in := f.instance("cx4")
	ctx := context.Background()
	f.deliver("INBOX", letterRaw("t1@example.org", "Ann <ann@example.org>", "Plan", "", "first"))
	f.deliver("Sent", "From: me@example.com\r\nTo: ann@example.org\r\nSubject: Re: Plan\r\nMessage-ID: <t2@example.com>\r\n"+
		"In-Reply-To: <t1@example.org>\r\nReferences: <t1@example.org>\r\nDate: Mon, 02 Nov 2026 11:00:00 +0000\r\n\r\nok\r\n")
	f.deliver("INBOX", "From: Ann <ann@example.org>\r\nTo: me@example.com\r\nSubject: Re: Plan\r\nMessage-ID: <t3@example.org>\r\n"+
		"In-Reply-To: <t2@example.com>\r\nReferences: <t1@example.org> <t2@example.com>\r\nDate: Mon, 02 Nov 2026 12:00:00 +0000\r\n"+
		"Content-Type: multipart/mixed; boundary=z\r\n\r\n--z\r\nContent-Type: text/plain; charset=koi8-r\r\n\r\n"+string(koi8("Смотри файл"))+"\r\n"+
		"--z\r\nContent-Type: text/csv\r\nContent-Disposition: attachment; filename=\"plan.csv\"\r\n\r\na,b\r\n1,2\r\n--z--\r\n")

	res, err := mailConn{}.Read(ctx, in, "list", map[string]string{"from": "ann@example.org"})
	if err != nil || !strings.Contains(res.Data, "id=t3@example.org") || !strings.Contains(res.Data, "attachment") || !strings.HasPrefix(res.Text, "2 of 2") {
		t.Fatalf("list: %v %+v", err, res)
	}
	if res, _ := (mailConn{}).Read(ctx, in, "list", map[string]string{"query": "nothing-like-this"}); !strings.Contains(res.Text, "No letters") {
		t.Fatalf("empty list: %+v", res)
	}
	res, err = mailConn{}.Read(ctx, in, "message", map[string]string{"id": "<t3@example.org>"})
	if err != nil || !strings.Contains(res.Data, "Смотри файл") || !strings.Contains(res.Data, "1. plan.csv") {
		t.Fatalf("message: %v %+v", err, res)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plan.csv"), []byte("taken"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err = mailConn{}.Read(ctx, in, "attachment", map[string]string{"id": "t3@example.org", "index": "1", "dir": dir})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "plan (2).csv")); string(b) != "a,b\r\n1,2" {
		t.Fatalf("saved %q (%s)", b, res.Text)
	}
	if _, err := (mailConn{}).Read(ctx, in, "attachment", map[string]string{"id": "t3@example.org", "index": "1", "dir": "relative"}); !errors.Is(err, connectors.ErrBadArgs) {
		t.Fatalf("relative dir: %v", err)
	}
	hidden := filepath.Join(dir, ".ssh")
	if err := os.Mkdir(hidden, 0o700); err != nil {
		t.Fatal(err)
	}
	var said *connectors.Said
	if _, err := (mailConn{}).Read(ctx, in, "attachment", map[string]string{"id": "t3@example.org", "index": "1", "dir": hidden}); !errors.As(err, &said) {
		t.Fatalf("hidden dir: %v", err)
	}
	if err := os.Symlink(hidden, filepath.Join(dir, "innocent")); err != nil {
		t.Fatal(err)
	}
	if _, err := (mailConn{}).Read(ctx, in, "attachment", map[string]string{"id": "t3@example.org", "index": "1", "dir": filepath.Join(dir, "innocent")}); !errors.As(err, &said) {
		t.Fatalf("link to a hidden dir: %v", err)
	}
	res, err = mailConn{}.Read(ctx, in, "thread", map[string]string{"id": "t3@example.org"})
	if err != nil || strings.Count(res.Data, "id=") != 3 || !strings.Contains(res.Data, "[Sent]") {
		t.Fatalf("thread: %v %+v", err, res)
	}
	res, err = mailConn{}.Read(ctx, in, "draft", map[string]string{"to": "ann@example.org", "subject": "Re: Plan", "text": "черновик", "reply": "t3@example.org"})
	if err != nil {
		t.Fatal(err)
	}
	drafts := f.messages("Drafts")
	if len(drafts) != 1 || !bytes.Contains(drafts[0].Body, []byte("In-Reply-To: <t3@example.org>")) ||
		!bytes.Contains(drafts[0].Body, []byte("References: <t1@example.org> <t2@example.com> <t3@example.org>")) ||
		!slices.Contains(drafts[0].Flags, goimap.DraftFlag) || len(f.sentMails()) != 0 {
		t.Fatalf("draft: %s %+v", res.Text, drafts)
	}
	if res, err := (mailConn{}).Read(ctx, in, "folders", nil); err != nil || !strings.Contains(res.Data, "Drafts") {
		t.Fatalf("folders: %v %+v", err, res)
	}
	if _, err := (mailConn{}).Read(ctx, in, "message", map[string]string{"id": "missing@example.org"}); !errors.Is(err, connectors.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestHugeLetterIsReadCut(t *testing.T) {
	f := newFake(t)
	in := f.instance("cx5")
	prev := rawMax
	rawMax = 64 << 10
	defer func() { rawMax = prev }()
	big := strings.Repeat("строка длинного письма\r\n", 20000) // ~ 900 KB
	f.deliver("INBOX", letterRaw("big@example.org", "Ann <ann@example.org>", "Big", "", big))
	res, err := mailConn{}.Read(context.Background(), in, "message", map[string]string{"id": "big@example.org"})
	if err != nil || !strings.Contains(res.Text, "cut") || !strings.Contains(res.Data, "строка длинного письма") {
		t.Fatalf("huge: %v %q", err, res.Text)
	}
}

func TestActionsSendReplyMarkReadArchive(t *testing.T) {
	f := newFake(t)
	in := f.instance("cx6")
	ctx := context.Background()
	f.deliver("INBOX", "From: Ann <ann@example.org>\r\nTo: me@example.com\r\nSubject: Plan\r\nMessage-ID: <x2@example.org>\r\n"+
		"References: <x0@example.org> <x1@example.org>\r\nIn-Reply-To: <x1@example.org>\r\n\r\nwhat do you think?\r\n")

	if _, err := (mailConn{}).Act(ctx, in, "send", map[string]string{"to": "bob@example.org, Carol <carol@example.org>", "bcc": "dan@example.org", "subject": "Привет", "text": "Строка 1\nСтрока 2"}); err != nil {
		t.Fatal(err)
	}
	sent := f.sentMails()
	if len(sent) != 1 || sent[0].from != "me@example.com" || len(sent[0].to) != 3 || bytes.Contains(sent[0].data, []byte("dan@example.org")) {
		t.Fatalf("send: %+v", sent)
	}
	l, err := parseLetter(bytes.NewReader(sent[0].data), 0, nil)
	if err != nil || l.Subject != "Привет" || l.Text != "Строка 1\r\nСтрока 2" && l.Text != "Строка 1\nСтрока 2" {
		t.Fatalf("sent letter: %v %+v", err, l)
	}
	if n := len(f.messages("Sent")); n != 1 {
		t.Fatalf("a copy in Sent: %d", n)
	}

	if _, err := (mailConn{}).Act(ctx, in, "reply", map[string]string{"id": "x2@example.org", "to": "ann@example.org", "subject": "Re: Plan", "text": "good"}); err != nil {
		t.Fatal(err)
	}
	sent = f.sentMails()
	rl, _ := parseLetter(bytes.NewReader(sent[1].data), 0, nil)
	if strings.Join(rl.InReplyTo, " ") != "x2@example.org" || strings.Join(rl.References, " ") != "x0@example.org x1@example.org x2@example.org" {
		t.Fatalf("reply headers: %+v %+v", rl.InReplyTo, rl.References)
	}
	if _, err := (mailConn{}).Act(ctx, in, "reply", map[string]string{"to": "ann@example.org", "subject": "x", "text": "y"}); !errors.Is(err, connectors.ErrBadArgs) {
		t.Fatalf("reply without id: %v", err)
	}
	if _, err := (mailConn{}).Act(ctx, in, "send", map[string]string{"to": "not an address", "subject": "x", "text": "y"}); !errors.Is(err, connectors.ErrBadArgs) {
		t.Fatalf("bad address: %v", err)
	}

	res, err := mailConn{}.Act(ctx, in, "mark-read", map[string]string{"id": "x2@example.org, gone@example.org"})
	if err != nil || !strings.Contains(res.Text, "Marked read: 1") || !strings.Contains(res.Text, "gone@example.org") {
		t.Fatalf("mark-read: %v %+v", err, res)
	}
	if m := f.messages("INBOX"); len(m) != 1 || !slices.Contains(m[0].Flags, goimap.SeenFlag) {
		t.Fatalf("flags: %+v", m)
	}
	if _, err := (mailConn{}).Act(ctx, in, "archive", map[string]string{"id": "x2@example.org"}); err != nil {
		t.Fatal(err)
	}
	if len(f.messages("INBOX")) != 0 || len(f.messages("Archive")) != 1 {
		t.Fatalf("archive: inbox %d archive %d", len(f.messages("INBOX")), len(f.messages("Archive")))
	}
}

func TestFromPerson(t *testing.T) {
	for _, c := range []struct {
		extra string
		from  string
		want  bool
	}{
		{"", "Ann <ann@example.org>", true},
		{"Precedence: bulk\r\n", "ann@example.org", false},
		{"Auto-Submitted: auto-replied\r\n", "ann@example.org", false},
		{"Auto-Submitted: no\r\n", "ann@example.org", true},
		{"List-Id: <team.example.org>\r\n", "ann@example.org", false},
		{"", "Service <noreply@example.org>", false},
		{"", "do_not_reply@example.org", false},
		{"", "MAILER-DAEMON@example.org", false},
	} {
		l := letterRaw("i@example.org", c.from, "s", c.extra, "b")
		m := &goimap.Message{Body: map[*goimap.BodySectionName]goimap.Literal{headerItem: literal([]byte(l[:strings.Index(l, "\r\n\r\n")+4]))}}
		if got := fromPerson(headerOf(m)); got != c.want {
			t.Errorf("%q %q: %v", c.extra, c.from, got)
		}
	}
}
