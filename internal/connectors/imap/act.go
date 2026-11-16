package imap

import (
	"bytes"
	"context"
	"fmt"
	"net/mail"
	"strings"
	"time"

	goimap "github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	gomail "github.com/emersion/go-message/mail"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

// sentSettle is how long a sent letter is given to appear in the sent folder by itself (Gmail, Yandex and Mail.ru put
// it there) before the connector appends its own copy.
var sentSettle = 3 * time.Second

// Act runs an action the person said yes to, with exactly the args the card showed.
func (mailConn) Act(ctx context.Context, in connectors.Instance, action string, args map[string]string) (connectors.Result, error) {
	var res connectors.Result
	err := withClient(ctx, in, func(c *client.Client, cfg config) error {
		var err error
		switch action {
		case "send", "reply":
			res, err = send(ctx, c, cfg, action, args)
		case "mark-read":
			res, err = flagSeen(c, args)
		case "archive":
			res, err = archive(c, args)
		default:
			err = connectors.ErrUnknownTool
		}
		return err
	})
	return res, netErr(err)
}

type composed struct {
	raw   []byte
	id    string
	rcpts []string
}

func addrList(s string) ([]*mail.Address, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	list, err := mail.ParseAddressList(s)
	if err != nil {
		return nil, fmt.Errorf("%w: %q is not a list of addresses", connectors.ErrBadArgs, s)
	}
	return list, nil
}

func toGo(list []*mail.Address) []*gomail.Address {
	out := make([]*gomail.Address, 0, len(list))
	for _, a := range list {
		out = append(out, &gomail.Address{Name: a.Name, Address: a.Address})
	}
	return out
}

// compose writes a letter in UTF-8: From the mailbox, the recipients, the subject and the text as given; an answer
// gets In-Reply-To and References from the letter it answers.
func compose(c *client.Client, cfg config, args map[string]string, isDraft bool) (composed, error) {
	to, err := addrList(args["to"])
	if err != nil {
		return composed{}, err
	}
	cc, err := addrList(args["cc"])
	if err != nil {
		return composed{}, err
	}
	bcc, err := addrList(args["bcc"])
	if err != nil {
		return composed{}, err
	}
	if !isDraft && len(to)+len(cc)+len(bcc) == 0 {
		return composed{}, fmt.Errorf("%w: to is required", connectors.ErrBadArgs)
	}
	text := args["text"]
	if strings.TrimSpace(text) == "" {
		return composed{}, fmt.Errorf("%w: text is required", connectors.ErrBadArgs)
	}
	var h gomail.Header
	h.SetDate(now())
	h.SetAddressList("From", []*gomail.Address{{Address: cfg.email}})
	if len(to) > 0 {
		h.SetAddressList("To", toGo(to))
	}
	if len(cc) > 0 {
		h.SetAddressList("Cc", toGo(cc))
	}
	h.SetSubject(oneLine(args["subject"]))
	domain := cfg.email[strings.LastIndexByte(cfg.email, '@')+1:]
	if err := h.GenerateMessageIDWithHostname(domain); err != nil {
		return composed{}, err
	}
	id, _ := h.MessageID()
	answered := args["reply"]
	if !isDraft {
		answered = args["id"]
	}
	if answered = bareID(answered); answered != "" {
		refs, err := referencesOf(c, answered)
		if err != nil {
			return composed{}, err
		}
		h.SetMsgIDList("In-Reply-To", []string{answered})
		h.SetMsgIDList("References", append(refs, answered))
	}
	h.Set("Content-Type", "text/plain; charset=utf-8")
	var buf bytes.Buffer
	w, err := gomail.CreateSingleInlineWriter(&buf, h)
	if err != nil {
		return composed{}, err
	}
	body := strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
	if _, err := w.Write([]byte(body)); err != nil {
		return composed{}, err
	}
	if err := w.Close(); err != nil {
		return composed{}, err
	}
	var rcpts []string
	for _, l := range [][]*mail.Address{to, cc, bcc} {
		for _, a := range l {
			rcpts = append(rcpts, a.Address)
		}
	}
	return composed{raw: buf.Bytes(), id: id, rcpts: rcpts}, nil
}

// referencesOf is the References of the letter answered (the thread so far), read from the mailbox.
func referencesOf(c *client.Client, id string) ([]string, error) {
	_, uid, err := locate(c, id, "", true)
	if err != nil {
		if err == connectors.ErrNotFound {
			return nil, connectors.Sayf("The letter %s is not in the mailbox", id)
		}
		return nil, err
	}
	msgs, err := fetch(c, []uint32{uid}, []goimap.FetchItem{goimap.FetchUid, headerItem.FetchItem()})
	if err != nil || len(msgs) == 0 {
		return nil, connectors.Sayf("The letter %s is not in the mailbox", id)
	}
	h := headerOf(msgs[0])
	refs := msgIDs(h.Get("References"))
	if len(refs) > 30 {
		refs = append(refs[:1], refs[len(refs)-29:]...)
	}
	return refs, nil
}

func send(ctx context.Context, c *client.Client, cfg config, action string, args map[string]string) (connectors.Result, error) {
	if action == "reply" && bareID(args["id"]) == "" {
		return connectors.Result{}, fmt.Errorf("%w: id of the letter answered is required", connectors.ErrBadArgs)
	}
	if action == "send" {
		delete(args, "id")
	}
	l, err := compose(c, cfg, args, false)
	if err != nil {
		return connectors.Result{}, err
	}
	s, err := dialSMTP(ctx, cfg)
	if err != nil {
		return connectors.Result{}, err
	}
	defer func() { _ = s.Close() }()
	if err := s.Mail(cfg.email, nil); err != nil {
		return connectors.Result{}, connectors.Sayf("The SMTP server refused the sender: %s", short(err))
	}
	for _, r := range l.rcpts {
		if err := s.Rcpt(r, nil); err != nil {
			return connectors.Result{}, connectors.Sayf("The SMTP server refused %s: %s", r, short(err))
		}
	}
	wc, err := s.Data()
	if err != nil {
		return connectors.Result{}, connectors.Sayf("The SMTP server refused the letter: %s", short(err))
	}
	if _, err := wc.Write(l.raw); err != nil {
		return connectors.Result{}, err
	}
	if err := wc.Close(); err != nil {
		return connectors.Result{}, connectors.Sayf("The SMTP server refused the letter: %s", short(err))
	}
	_ = s.Quit()
	where := keepSent(ctx, c, l)
	return connectors.Result{Text: fmt.Sprintf("Sent to %s (Message-ID %s)%s.", strings.Join(l.rcpts, ", "), l.id, where)}, nil
}

// keepSent puts a copy of the letter into the sent folder unless the server did it by itself.
func keepSent(ctx context.Context, c *client.Client, l composed) string {
	folder, err := special(c, goimap.SentAttr)
	if err != nil || folder == "" {
		return ""
	}
	t := time.NewTimer(sentSettle)
	select {
	case <-ctx.Done():
		t.Stop()
		return ""
	case <-t.C:
	}
	if _, err := c.Select(folder, true); err == nil {
		if uid, err := byID(c, l.id); err == nil && uid != 0 {
			return ", a copy is in " + folder
		}
	}
	if err := c.Append(folder, []string{goimap.SeenFlag}, now(), literal(l.raw)); err != nil {
		return ""
	}
	return ", a copy is in " + folder
}

func ids(args map[string]string) ([]string, error) {
	var out []string
	for _, s := range strings.FieldsFunc(args["id"], func(r rune) bool { return r == ',' || r == ' ' }) {
		if s = bareID(s); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: id is required", connectors.ErrBadArgs)
	}
	if len(out) > 100 {
		return nil, fmt.Errorf("%w: at most 100 letters at once", connectors.ErrBadArgs)
	}
	return out, nil
}

func flagSeen(c *client.Client, args map[string]string) (connectors.Result, error) {
	list, err := ids(args)
	if err != nil {
		return connectors.Result{}, err
	}
	done, missing := 0, []string{}
	for _, id := range list {
		_, uid, err := locate(c, id, "", false)
		if err == connectors.ErrNotFound {
			missing = append(missing, id)
			continue
		}
		if err != nil {
			return connectors.Result{}, err
		}
		set := new(goimap.SeqSet)
		set.AddNum(uid)
		if err := c.UidStore(set, goimap.FormatFlagsOp(goimap.AddFlags, true), []any{goimap.SeenFlag}, nil); err != nil {
			return connectors.Result{}, err
		}
		done++
	}
	return connectors.Result{Text: outcome("Marked read", done, missing)}, nil
}

func archive(c *client.Client, args map[string]string) (connectors.Result, error) {
	list, err := ids(args)
	if err != nil {
		return connectors.Result{}, err
	}
	dest, err := special(c, goimap.ArchiveAttr)
	if err != nil {
		return connectors.Result{}, err
	}
	if dest == "" {
		dest = "Archive"
		if err := c.Create(dest); err != nil {
			return connectors.Result{}, connectors.Sayf("The mailbox has no archive folder and did not let make one")
		}
	}
	done, missing := 0, []string{}
	for _, id := range list {
		_, uid, err := locate(c, id, "inbox", false)
		if err == connectors.ErrNotFound {
			missing = append(missing, id)
			continue
		}
		if err != nil {
			return connectors.Result{}, err
		}
		set := new(goimap.SeqSet)
		set.AddNum(uid)
		if err := c.UidMove(set, dest); err != nil {
			return connectors.Result{}, connectors.Sayf("The mailbox did not move the letter: %s", short(err))
		}
		done++
	}
	return connectors.Result{Text: outcome("Moved to "+dest, done, missing)}, nil
}

func outcome(what string, done int, missing []string) string {
	s := fmt.Sprintf("%s: %d.", what, done)
	if len(missing) > 0 {
		s += " Not in INBOX: " + strings.Join(missing, ", ") + "."
	}
	return s
}

// literal is a letter as an IMAP literal.
type lit struct{ *bytes.Reader }

func (l lit) Len() int { return int(l.Reader.Size()) }

func literal(b []byte) goimap.Literal { return lit{bytes.NewReader(b)} }
