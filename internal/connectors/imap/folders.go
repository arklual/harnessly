package imap

import (
	"net/textproto"
	"slices"
	"strings"

	goimap "github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

// The folders a mailbox names by its special-use attributes (RFC 6154), and the names servers without them use.
var folderNames = map[string][]string{
	goimap.SentAttr:    {"Sent", "Sent Messages", "Sent Items", "Sent Mail", "Отправленные", "INBOX.Sent", "[Gmail]/Sent Mail"},
	goimap.DraftsAttr:  {"Drafts", "Черновики", "INBOX.Drafts", "[Gmail]/Drafts"},
	goimap.ArchiveAttr: {"Archive", "Архив", "INBOX.Archive", "Archives"},
}

func folders(c *client.Client) ([]*goimap.MailboxInfo, error) {
	ch := make(chan *goimap.MailboxInfo, 32)
	done := make(chan error, 1)
	go func() { done <- c.List("", "*", ch) }()
	var out []*goimap.MailboxInfo
	for m := range ch {
		out = append(out, m)
	}
	return out, <-done
}

// special is the folder of a special use (\Sent, \Drafts, \Archive), by attribute, then by a usual name; "" if none.
func special(c *client.Client, attr string) (string, error) {
	list, err := folders(c)
	if err != nil {
		return "", err
	}
	for _, m := range list {
		if slices.Contains(m.Attributes, attr) && !slices.Contains(m.Attributes, goimap.NoSelectAttr) {
			return m.Name, nil
		}
	}
	// Gmail has no \Archive: its archive is All Mail.
	if attr == goimap.ArchiveAttr {
		for _, m := range list {
			if slices.Contains(m.Attributes, goimap.AllAttr) {
				return m.Name, nil
			}
		}
	}
	for _, want := range folderNames[attr] {
		for _, m := range list {
			if strings.EqualFold(m.Name, want) {
				return m.Name, nil
			}
		}
	}
	return "", nil
}

// folderArg resolves a folder the agent names: inbox, sent, drafts, archive or a folder's own name.
func folderArg(c *client.Client, name string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "inbox":
		return "INBOX", nil
	case "sent":
		return mustSpecial(c, goimap.SentAttr, "sent")
	case "drafts":
		return mustSpecial(c, goimap.DraftsAttr, "drafts")
	case "archive":
		return mustSpecial(c, goimap.ArchiveAttr, "archive")
	}
	return name, nil
}

func mustSpecial(c *client.Client, attr, word string) (string, error) {
	f, err := special(c, attr)
	if err != nil {
		return "", err
	}
	if f == "" {
		return "", connectors.Sayf("The mailbox has no %s folder", word)
	}
	return f, nil
}

// fetch fetches messages by UID.
func fetch(c *client.Client, uids []uint32, items []goimap.FetchItem) ([]*goimap.Message, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	set := new(goimap.SeqSet)
	set.AddNum(uids...)
	ch := make(chan *goimap.Message, 32)
	done := make(chan error, 1)
	go func() { done <- c.UidFetch(set, items, ch) }()
	var out []*goimap.Message
	for m := range ch {
		out = append(out, m)
	}
	return out, <-done
}

// byID finds a letter by its Message-ID in the selected folder; 0 when it is not there.
func byID(c *client.Client, id string) (uint32, error) {
	crit := goimap.NewSearchCriteria()
	crit.Header = textproto.MIMEHeader{"Message-Id": {id}}
	uids, err := c.UidSearch(crit)
	if err != nil || len(uids) == 0 {
		return 0, err
	}
	return uids[len(uids)-1], nil
}

// locate selects the folder holding a letter and answers its UID: the given folder, or INBOX, the sent folder and
// the archive in turn.
func locate(c *client.Client, id, folder string, readOnly bool) (string, uint32, error) {
	id = bareID(id)
	if id == "" {
		return "", 0, connectors.Sayf("Give the letter's id (its Message-ID)")
	}
	var candidates []string
	if folder != "" {
		f, err := folderArg(c, folder)
		if err != nil {
			return "", 0, err
		}
		candidates = []string{f}
	} else {
		candidates = []string{"INBOX"}
		for _, a := range []string{goimap.SentAttr, goimap.ArchiveAttr, goimap.DraftsAttr} {
			if f, _ := special(c, a); f != "" {
				candidates = append(candidates, f)
			}
		}
	}
	for _, f := range candidates {
		if _, err := c.Select(f, readOnly); err != nil {
			continue
		}
		uid, err := byID(c, id)
		if err != nil {
			return "", 0, err
		}
		if uid != 0 {
			return f, uid, nil
		}
	}
	return "", 0, connectors.ErrNotFound
}
