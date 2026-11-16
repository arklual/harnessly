package imap

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/mail"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	goimap "github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/textproto"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

var (
	// recheck is how often the watcher leaves IDLE to look at the sent folder and the letters waiting for an answer
	// (and to renew IDLE well before the 29 minutes of RFC 2177).
	recheck = 10 * time.Minute
	// pollEvery is the NOOP poll of a server without IDLE.
	pollEvery = 2 * time.Minute
	// backoff of reconnects: from backoffMin doubling to backoffMax; after failsMax failures in a row the error goes to
	// the runtime (the page shows it, the runtime starts again in a minute).
	backoffMin = 5 * time.Second
	backoffMax = 5 * time.Minute
	failsMax   = 6
	now        = time.Now
	debugf     func(string, ...any)
	debugw     io.Writer
)

const (
	// batchMax letters are emitted per batch; gapMax new letters after a long pause are looked at, older are skipped.
	batchMax = 50
	gapMax   = 300
	// waitMax letters waiting for an answer are kept in the cursor.
	waitMax = 300
)

// cursor is where the watcher stopped: the UIDVALIDITY and the last UID seen of INBOX and of the sent folder, and the
// letters from people that wait for the person's answer.
type cursor struct {
	V     uint32    `json:"v"`
	UID   uint32    `json:"uid"`
	SentV uint32    `json:"sv,omitempty"`
	Sent  uint32    `json:"suid,omitempty"`
	Wait  []waiting `json:"wait,omitempty"`
}

type waiting struct {
	ID      string    `json:"id"`
	Thread  string    `json:"t,omitempty"`
	At      time.Time `json:"at"`
	Actor   string    `json:"a,omitempty"`
	Subject string    `json:"s,omitempty"`
}

func readCursor(c connectors.Cursor) (cursor, bool) {
	var cur cursor
	if c == "" || json.Unmarshal([]byte(c), &cur) != nil || cur.V == 0 {
		return cursor{}, false
	}
	return cur, true
}

func (c cursor) encode() connectors.Cursor {
	raw, _ := json.Marshal(c)
	return connectors.Cursor(raw)
}

// Watch runs IDLE on INBOX on one connection, reconnecting with a growing pause.
func (mailConn) Watch(ctx context.Context, in connectors.Instance, from connectors.Cursor, emit connectors.Emit) error {
	cur := from
	fails := 0
	for {
		next, progressed, err := session(ctx, in, cur, emit)
		cur = next
		if debugf != nil && err != nil {
			debugf("session: %v", err)
		}
		if ctx.Err() != nil {
			return nil
		}
		if progressed {
			fails = 0
		}
		fails++
		var said *connectors.Said
		if errors.As(err, &said) || fails >= failsMax {
			return err
		}
		d := min(backoffMin<<(fails-1), backoffMax)
		t := time.NewTimer(d)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil
		case <-t.C:
		}
	}
}

// session is one connection's life: sign in, learn or catch up, then IDLE until an error.
func session(ctx context.Context, in connectors.Instance, from connectors.Cursor, emit connectors.Emit) (connectors.Cursor, bool, error) {
	cfg, err := configOf(ctx, in)
	if err != nil {
		return from, false, err
	}
	c, err := dialIMAP(ctx, cfg)
	if err != nil {
		return from, false, err
	}
	defer func() { _ = c.Terminate() }()
	stop := context.AfterFunc(ctx, func() { _ = c.Terminate() })
	defer stop()

	// The client's reader blocks on a full Updates; a goroutine of its own drains it into a poke.
	updates := make(chan client.Update, 64)
	poke := make(chan struct{}, 1)
	c.Updates = updates
	quit := make(chan struct{})
	defer close(quit)
	go func() {
		for {
			select {
			case <-quit:
				return
			case u := <-updates:
				if _, ok := u.(*client.MailboxUpdate); ok {
					select {
					case poke <- struct{}{}:
					default:
					}
				}
			}
		}
	}()

	if debugw != nil {
		c.SetDebug(debugw)
	}
	w := &watcher{c: c, cfg: cfg, emit: emit}
	cur, known := readCursor(from)
	w.cur = cur
	progressed := false
	if !known {
		if err := w.learn(); err != nil {
			return from, false, err
		}
		if err := emit(nil, w.cur.encode()); err != nil {
			return from, false, err
		}
		progressed = true
	}
	for {
		if err := w.check(); err != nil {
			return w.cur.encode(), progressed, err
		}
		progressed = true
		if _, err := c.Select("INBOX", true); err != nil {
			return w.cur.encode(), progressed, err
		}
		stopIdle := make(chan struct{})
		idled := make(chan error, 1)
		go func() {
			idled <- c.Idle(stopIdle, &client.IdleOptions{LogoutTimeout: recheck + time.Minute, PollInterval: pollEvery})
		}()
		t := time.NewTimer(recheck)
		select {
		case <-ctx.Done():
			close(stopIdle)
			<-idled //nolint:errcheck // waits for the IDLE goroutine to end; its error does not matter on the way out
			t.Stop()
			return w.cur.encode(), progressed, nil
		case <-poke:
			close(stopIdle)
			err = <-idled
		case <-t.C:
			close(stopIdle)
			err = <-idled
		case err = <-idled:
			if err == nil {
				err = errors.New("IDLE ended")
			}
		}
		t.Stop()
		if err != nil {
			return w.cur.encode(), progressed, err
		}
	}
}

type watcher struct {
	c    *client.Client
	cfg  config
	cur  cursor
	emit connectors.Emit
}

// learn takes what the mailbox has now as known: the first run, or a UIDVALIDITY that changed.
func (w *watcher) learn() error {
	st, err := w.c.Select("INBOX", true)
	if err != nil {
		return err
	}
	w.cur.V, w.cur.UID = st.UidValidity, last(st)
	return w.learnSent()
}

func (w *watcher) learnSent() error {
	sent, err := special(w.c, goimap.SentAttr)
	if err != nil || sent == "" {
		w.cur.SentV, w.cur.Sent = 0, 0
		return err
	}
	st, err := w.c.Status(sent, []goimap.StatusItem{goimap.StatusUidNext, goimap.StatusUidValidity})
	if err != nil {
		return nil // a sent folder that does not answer STATUS is just not watched
	}
	w.cur.SentV, w.cur.Sent = st.UidValidity, max(st.UidNext, 1)-1
	return nil
}

func last(st *goimap.MailboxStatus) uint32 {
	if st.UidNext > 0 {
		return st.UidNext - 1
	}
	return 0
}

// headerItem is the part of a letter the watcher reads: no text.
var headerItem = &goimap.BodySectionName{Peek: true, BodyPartName: goimap.BodyPartName{
	Specifier: goimap.HeaderSpecifier,
	Fields: []string{"Message-Id", "In-Reply-To", "References", "From", "Reply-To", "Subject", "Date",
		"List-Unsubscribe", "List-Id", "List-Post", "Precedence", "Auto-Submitted", "X-Auto-Response-Suppress", "Return-Path"},
}}

var watchItems = []goimap.FetchItem{goimap.FetchUid, goimap.FetchFlags, goimap.FetchInternalDate, goimap.FetchBodyStructure, headerItem.FetchItem()}

// check looks for new letters in INBOX and in the sent folder and for answers that did not come in time.
func (w *watcher) check() error {
	st, err := w.c.Select("INBOX", true)
	if err != nil {
		return err
	}
	if st.UidValidity != w.cur.V {
		// The server renumbered the folder: what is there now is taken as known.
		w.cur.V, w.cur.UID = st.UidValidity, last(st)
		if err := w.emit(nil, w.cur.encode()); err != nil {
			return err
		}
	}
	if err := w.inbox(st); err != nil {
		return err
	}
	if err := w.sent(); err != nil {
		return err
	}
	return w.overdue()
}

func (w *watcher) newUIDs(from uint32) ([]uint32, error) {
	set := new(goimap.SeqSet)
	set.AddRange(from+1, 0)
	crit := goimap.NewSearchCriteria()
	crit.Uid = set
	uids, err := w.c.UidSearch(crit)
	if err != nil {
		return nil, err
	}
	// "n:*" always matches the last letter, even below n.
	uids = slices.DeleteFunc(uids, func(u uint32) bool { return u <= from })
	slices.Sort(uids)
	return uids, nil
}

func (w *watcher) inbox(st *goimap.MailboxStatus) error {
	if st.UidNext != 0 && st.UidNext-1 <= w.cur.UID {
		return nil
	}
	uids, err := w.newUIDs(w.cur.UID)
	if err != nil {
		return err
	}
	if len(uids) > gapMax {
		uids = uids[len(uids)-gapMax:]
	}
	for len(uids) > 0 {
		n := min(batchMax, len(uids))
		batch := uids[:n]
		uids = uids[n:]
		msgs, err := fetch(w.c, batch, watchItems)
		if err != nil {
			return err
		}
		sort.Slice(msgs, func(i, j int) bool { return msgs[i].Uid < msgs[j].Uid })
		var events []connectors.Event
		next := w.cur
		for _, m := range msgs {
			if ev, wt, ok := w.received(m); ok {
				events = append(events, ev)
				if wt != nil {
					next.Wait = appendWait(next.Wait, *wt)
				}
			}
		}
		next.UID = batch[len(batch)-1]
		if err := w.emit(events, next.encode()); err != nil {
			return err
		}
		w.cur = next
	}
	return nil
}

// received makes the event of a new letter in INBOX and, for a letter from a person to answer, its wait.
func (w *watcher) received(m *goimap.Message) (connectors.Event, *waiting, bool) {
	h := headerOf(m)
	id := bareID(h.Get("Message-Id"))
	evID := id
	if evID == "" {
		evID = "uid:" + strconv.FormatUint(uint64(w.cur.V), 10) + ":" + strconv.FormatUint(uint64(m.Uid), 10)
	}
	from := decodeAddrs(h.Get("From"))
	subject := decodeWords(h.Get("Subject"))
	mine := slices.Contains(addrsOf(h.Get("From")), strings.ToLower(w.cfg.email))
	person := !mine && fromPerson(h)
	at := m.InternalDate
	if d, err := parseDate(h.Get("Date")); err == nil && !d.IsZero() {
		at = d
	}
	facts := map[string]string{
		"folder":        "INBOX",
		"hasAttachment": strconv.FormatBool(hasAttachment(m.BodyStructure)),
		"person":        strconv.FormatBool(person),
		"list":          strconv.FormatBool(h.Get("List-Id") != "" || h.Get("List-Unsubscribe") != ""),
		"unread":        strconv.FormatBool(!slices.Contains(m.Flags, goimap.SeenFlag)),
	}
	ref := "message folder=INBOX uid=" + strconv.FormatUint(uint64(m.Uid), 10)
	if id != "" {
		ref = "message id=" + id
	}
	ev := connectors.Event{Type: EventReceived, ID: evID, At: at, Actor: from, Title: subject,
		Thread: threadOf(h, id), Ref: ref, Mine: mine, Facts: facts}
	if !person || id == "" || w.cfg.unansweredDays == 0 {
		return ev, nil, true
	}
	// The wait counts from the arrival, not from a Date the sender wrote.
	arrived := m.InternalDate
	if arrived.IsZero() {
		arrived = now()
	}
	return ev, &waiting{ID: id, Thread: ev.Thread, At: arrived, Actor: from, Subject: cut(subject, 200)}, true
}

func appendWait(list []waiting, wt waiting) []waiting {
	for _, x := range list {
		if x.ID == wt.ID {
			return list
		}
	}
	list = append(list, wt)
	if len(list) > waitMax {
		list = slices.Clone(list[len(list)-waitMax:])
	}
	return list
}

// sent reads the new letters of the sent folder: an answer to a waiting letter ends its wait and is mail.replied.
func (w *watcher) sent() error {
	folder, err := special(w.c, goimap.SentAttr)
	if err != nil || folder == "" {
		return err
	}
	st, err := w.c.Status(folder, []goimap.StatusItem{goimap.StatusUidNext, goimap.StatusUidValidity})
	if err != nil {
		return nil
	}
	if st.UidValidity != w.cur.SentV {
		w.cur.SentV, w.cur.Sent = st.UidValidity, max(st.UidNext, 1)-1
		return w.emit(nil, w.cur.encode())
	}
	if st.UidNext == 0 || st.UidNext-1 <= w.cur.Sent {
		return nil
	}
	if _, err := w.c.Select(folder, true); err != nil {
		return nil
	}
	uids, err := w.newUIDs(w.cur.Sent)
	if err != nil {
		return err
	}
	if len(uids) > gapMax {
		uids = uids[len(uids)-gapMax:]
	}
	for len(uids) > 0 {
		n := min(batchMax, len(uids))
		batch := uids[:n]
		uids = uids[n:]
		msgs, err := fetch(w.c, batch, watchItems)
		if err != nil {
			return err
		}
		next := w.cur
		var events []connectors.Event
		for _, m := range msgs {
			h := headerOf(m)
			refs := append(msgIDs(h.Get("In-Reply-To")), msgIDs(h.Get("References"))...)
			if len(refs) == 0 {
				continue
			}
			id := bareID(h.Get("Message-Id"))
			if id == "" {
				continue
			}
			thread := threadOf(h, id)
			next.Wait = slices.DeleteFunc(slices.Clone(next.Wait), func(x waiting) bool {
				if slices.Contains(refs, x.ID) {
					if x.Thread != "" {
						thread = x.Thread
					}
					return true
				}
				return false
			})
			at := m.InternalDate
			if d, err := parseDate(h.Get("Date")); err == nil && !d.IsZero() {
				at = d
			}
			events = append(events, connectors.Event{Type: EventReplied, ID: "sent:" + id, At: at,
				Actor: w.cfg.email, Title: decodeWords(h.Get("Subject")), Thread: thread, Mine: true,
				Ref: "message id=" + id + " folder=sent", Facts: map[string]string{"folder": "sent"}})
		}
		next.Sent = batch[len(batch)-1]
		if err := w.emit(events, next.encode()); err != nil {
			return err
		}
		w.cur = next
	}
	return nil
}

// overdue emits mail.unanswered for the letters that waited unansweredDays without an answer.
func (w *watcher) overdue() error {
	if w.cfg.unansweredDays == 0 || len(w.cur.Wait) == 0 {
		return nil
	}
	limit := time.Duration(w.cfg.unansweredDays) * 24 * time.Hour
	t := now()
	next := w.cur
	next.Wait = nil
	var events []connectors.Event
	for _, x := range w.cur.Wait {
		if t.Sub(x.At) < limit {
			next.Wait = append(next.Wait, x)
			continue
		}
		events = append(events, connectors.Event{Type: EventUnanswered, ID: "unanswered:" + x.ID, At: t,
			Actor: x.Actor, Title: x.Subject, Thread: x.Thread, Ref: "message id=" + x.ID,
			Facts: map[string]string{"days": strconv.Itoa(w.cfg.unansweredDays), "receivedAt": x.At.UTC().Format(time.RFC3339)}})
	}
	if len(events) == 0 {
		return nil
	}
	if err := w.emit(events, next.encode()); err != nil {
		return err
	}
	w.cur = next
	return nil
}

func headerOf(m *goimap.Message) textproto.Header {
	for sec, lit := range m.Body {
		if sec.Specifier == goimap.HeaderSpecifier && lit != nil {
			b, _ := io.ReadAll(io.LimitReader(lit, 1<<20))
			h, err := textproto.ReadHeader(bufio.NewReader(io.MultiReader(bytes.NewReader(b), strings.NewReader("\r\n"))))
			if err == nil {
				return h
			}
		}
	}
	return textproto.Header{}
}

// threadOf is a thread's key: the first id of References (the thread's root), else In-Reply-To, else the letter's own
// id. The person's answer has the same root, so mail.received and mail.replied of one thread share it.
func threadOf(h textproto.Header, own string) string {
	if refs := msgIDs(h.Get("References")); len(refs) > 0 {
		return refs[0]
	}
	if irt := msgIDs(h.Get("In-Reply-To")); len(irt) > 0 {
		return irt[0]
	}
	return own
}

// fromPerson says a letter was written by a person to answer: not a mailing, a robot or an auto-reply.
func fromPerson(h textproto.Header) bool {
	if h.Get("List-Unsubscribe") != "" || h.Get("List-Id") != "" || h.Get("List-Post") != "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(h.Get("Precedence"))) {
	case "bulk", "list", "junk", "auto_reply":
		return false
	}
	if a := strings.ToLower(strings.TrimSpace(h.Get("Auto-Submitted"))); a != "" && a != "no" {
		return false
	}
	if h.Get("X-Auto-Response-Suppress") != "" && strings.Contains(strings.ToLower(h.Get("X-Auto-Response-Suppress")), "all") {
		return false
	}
	if rp := strings.TrimSpace(h.Get("Return-Path")); rp == "<>" {
		return false
	}
	addrs := addrsOf(h.Get("From"))
	if len(addrs) == 0 {
		return false
	}
	local := addrs[0][:max(strings.IndexByte(addrs[0], '@'), 0)]
	local = strings.NewReplacer("-", "", "_", "", ".", "").Replace(local)
	for _, robot := range []string{"noreply", "donotreply", "mailerdaemon", "postmaster", "bounce", "notification", "notify", "noreplay", "automailer", "robot"} {
		if strings.Contains(local, robot) {
			return false
		}
	}
	return true
}

func hasAttachment(bs *goimap.BodyStructure) bool {
	if bs == nil {
		return false
	}
	found := false
	bs.Walk(func(_ []int, p *goimap.BodyStructure) bool {
		if strings.EqualFold(p.Disposition, "attachment") {
			found = true
		}
		if name, _ := p.Filename(); name != "" && !strings.EqualFold(p.MIMEType, "multipart") {
			found = true
		}
		return !found
	})
	return found
}

func parseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New("no date")
	}
	return mail.ParseDate(s)
}
