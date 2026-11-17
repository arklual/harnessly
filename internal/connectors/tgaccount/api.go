package tgaccount

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

func newManager(d tg.UpdateDispatcher, path string) *updates.Manager {
	st := updates.NewFileStorage(path)
	return updates.New(updates.Config{Handler: d, Storage: st, AccessHasher: st})
}

func managerAuth() updates.AuthOptions { return updates.AuthOptions{} }

// ---- reads -------------------------------------------------------------------------------------------------------

func limitOf(args map[string]string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(args["limit"])); err == nil && n > 0 {
		return min(n, limitMax)
	}
	return def
}

func (accountConn) Read(ctx context.Context, in connectors.Instance, tool string, args map[string]string) (connectors.Result, error) {
	switch tool {
	case "chats", "messages", "message", "search", "me":
	default:
		return connectors.Result{}, connectors.ErrUnknownTool
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	b := bookOf(in)
	var out connectors.Result
	err := withAPI(ctx, in, func(ctx context.Context, api *tg.Client) error {
		var err error
		switch tool {
		case "chats":
			out, err = readChats(ctx, api, b, limitOf(args, 30), args["unread"] == "true")
		case "messages":
			out, err = readMessages(ctx, api, b, args)
		case "message":
			out, err = readMessage(ctx, api, b, args)
		case "search":
			out, err = readSearch(ctx, api, b, args)
		case "me":
			out, err = readMe(ctx, api)
		}
		return err
	})
	return out, err
}

type dialog struct {
	ID       int64
	Unread   int
	Mentions int
	Pinned   bool
	Muted    bool
	Last     time.Time
}

// dialogs reads the account's chats, the latest first, and teaches the book their peers.
func dialogs(ctx context.Context, api *tg.Client, b *book, limit int) ([]dialog, error) {
	res, err := api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{OffsetPeer: &tg.InputPeerEmpty{}, Limit: limit})
	if err != nil {
		return nil, err
	}
	box, ok := res.AsModified()
	if !ok {
		return nil, nil
	}
	b.learn(box.GetUsers(), box.GetChats())
	last := map[int64]time.Time{}
	for _, m := range box.GetMessages() {
		if m, ok := m.(*tg.Message); ok {
			last[markedPeer(m.PeerID)] = time.Unix(int64(m.Date), 0).UTC()
		}
	}
	var out []dialog
	for _, d := range box.GetDialogs() {
		d, ok := d.(*tg.Dialog)
		if !ok {
			continue
		}
		id := markedPeer(d.Peer)
		muted := false
		if until, ok := d.NotifySettings.GetMuteUntil(); ok && int64(until) > time.Now().Unix() {
			muted = true
		}
		out = append(out, dialog{ID: id, Unread: d.UnreadCount, Mentions: d.UnreadMentionsCount, Pinned: d.Pinned,
			Muted: muted, Last: last[id]})
	}
	return out, nil
}

func readChats(ctx context.Context, api *tg.Client, b *book, limit int, unread bool) (connectors.Result, error) {
	ds, err := dialogs(ctx, api, b, limit)
	if err != nil {
		return connectors.Result{}, err
	}
	var sb strings.Builder
	n := 0
	for _, d := range ds {
		if unread && d.Unread == 0 {
			continue
		}
		n++
		p, _ := b.get(d.ID)
		kind := p.Kind
		switch {
		case p.Kind == kindChannel && p.Group:
			kind = "group"
		case p.Kind == kindChat:
			kind = "group"
		case p.Bot:
			kind = "bot"
		}
		fmt.Fprintf(&sb, "%d %s [%s] unread %d", d.ID, b.name(d.ID), kind, d.Unread)
		if d.Mentions > 0 {
			fmt.Fprintf(&sb, ", mentions %d", d.Mentions)
		}
		if d.Muted {
			sb.WriteString(", muted")
		}
		if d.Pinned {
			sb.WriteString(", pinned")
		}
		if !d.Last.IsZero() {
			sb.WriteString(", last " + d.Last.Format(time.RFC3339))
		}
		sb.WriteString("\n")
	}
	if n == 0 {
		return connectors.Result{Text: "No chats."}, nil
	}
	return connectors.Result{Text: fmt.Sprintf("%d chats (id, title, kind, counts), the latest first", n),
		Data: sb.String(), From: "telegram: chat titles"}, nil
}

// history reads a peer's messages older than before (0: the latest).
func history(ctx context.Context, api *tg.Client, b *book, peer tg.InputPeerClass, before, limit int) ([]*tg.Message, error) {
	res, err := api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: peer, OffsetID: before, Limit: limit})
	if err != nil {
		return nil, err
	}
	return messagesOf(b, res), nil
}

func messagesOf(b *book, res tg.MessagesMessagesClass) []*tg.Message {
	box, ok := res.AsModified()
	if !ok {
		return nil
	}
	b.learn(box.GetUsers(), box.GetChats())
	var out []*tg.Message
	for _, m := range box.GetMessages() {
		if m, ok := m.(*tg.Message); ok {
			out = append(out, m)
		}
	}
	return out
}

func readMessages(ctx context.Context, api *tg.Client, b *book, args map[string]string) (connectors.Result, error) {
	peer, id, err := resolve(ctx, api, b, args["chat"])
	if err != nil {
		return connectors.Result{}, err
	}
	before, _ := strconv.Atoi(strings.TrimSpace(args["before"]))
	ms, err := history(ctx, api, b, peer, before, limitOf(args, 20))
	if err != nil {
		return connectors.Result{}, err
	}
	if len(ms) == 0 {
		return connectors.Result{Text: "No messages."}, nil
	}
	slices.Reverse(ms)
	self := selfOf(ctx, api)
	where := chatName(b, id)
	return connectors.Result{Text: fmt.Sprintf("%d messages of %s, the oldest first (they stay unread)", len(ms), where),
		Data: lines(b, self, ms, false), From: "telegram: " + where}, nil
}

func readMessage(ctx context.Context, api *tg.Client, b *book, args map[string]string) (connectors.Result, error) {
	msgID, err := strconv.Atoi(strings.TrimSpace(args["id"]))
	if err != nil || msgID <= 0 {
		return connectors.Result{}, connectors.Sayf("id: the message's number")
	}
	peer, id, err := resolve(ctx, api, b, args["chat"])
	if err != nil {
		return connectors.Result{}, err
	}
	ms, err := history(ctx, api, b, peer, msgID+1, 1)
	if err != nil {
		return connectors.Result{}, err
	}
	if len(ms) == 0 || ms[0].ID != msgID {
		return connectors.Result{}, connectors.ErrNotFound
	}
	where := chatName(b, id)
	return connectors.Result{Text: "1 message of " + where, Data: lines(b, selfOf(ctx, api), ms, false),
		From: "telegram: " + where}, nil
}

func readSearch(ctx context.Context, api *tg.Client, b *book, args map[string]string) (connectors.Result, error) {
	q := strings.TrimSpace(args["q"])
	if q == "" {
		return connectors.Result{}, connectors.Sayf("q: required")
	}
	limit := limitOf(args, 20)
	var res tg.MessagesMessagesClass
	var err error
	where := "all chats"
	if strings.TrimSpace(args["chat"]) != "" {
		var peer tg.InputPeerClass
		var id int64
		peer, id, err = resolve(ctx, api, b, args["chat"])
		if err != nil {
			return connectors.Result{}, err
		}
		where = chatName(b, id)
		res, err = api.MessagesSearch(ctx, &tg.MessagesSearchRequest{Peer: peer, Q: q,
			Filter: &tg.InputMessagesFilterEmpty{}, Limit: limit})
	} else {
		res, err = api.MessagesSearchGlobal(ctx, &tg.MessagesSearchGlobalRequest{Q: q,
			Filter: &tg.InputMessagesFilterEmpty{}, OffsetPeer: &tg.InputPeerEmpty{}, Limit: limit})
	}
	if err != nil {
		return connectors.Result{}, err
	}
	ms := messagesOf(b, res)
	if len(ms) == 0 {
		return connectors.Result{Text: "Nothing found in " + where + "."}, nil
	}
	return connectors.Result{Text: fmt.Sprintf("%d messages found in %s, the latest first", len(ms), where),
		Data: lines(b, selfOf(ctx, api), ms, where == "all chats"), From: "telegram: search in " + where}, nil
}

func readMe(ctx context.Context, api *tg.Client) (connectors.Result, error) {
	users, err := api.UsersGetUsers(ctx, []tg.InputUserClass{&tg.InputUserSelf{}})
	if err != nil {
		return connectors.Result{}, err
	}
	for _, u := range users {
		if u, ok := u.(*tg.User); ok {
			return connectors.Result{Text: fmt.Sprintf("Signed in as %s, id %d", userName(u), u.ID)}, nil
		}
	}
	return connectors.Result{}, connectors.ErrNotFound
}

func selfOf(ctx context.Context, api *tg.Client) int64 {
	users, err := api.UsersGetUsers(ctx, []tg.InputUserClass{&tg.InputUserSelf{}})
	if err != nil || len(users) == 0 {
		return 0
	}
	if u, ok := users[0].(*tg.User); ok {
		return u.ID
	}
	return 0
}

func chatName(b *book, id int64) string {
	if id == 0 {
		return "Saved Messages"
	}
	return b.name(id)
}

// lines writes messages one per line: [chat:id] time sender (reply to n) [media]: text.
func lines(b *book, self int64, ms []*tg.Message, withChat bool) string {
	var sb strings.Builder
	for _, m := range ms {
		chat := markedPeer(m.PeerID)
		fmt.Fprintf(&sb, "[%d:%d] %s %s", chat, m.ID, time.Unix(int64(m.Date), 0).UTC().Format(time.RFC3339),
			b.senderName(m, self))
		if withChat {
			sb.WriteString(" in " + b.name(chat))
		}
		if r, ok := m.GetReplyTo(); ok {
			if h, ok := r.(*tg.MessageReplyHeader); ok && h.ReplyToMsgID != 0 {
				fmt.Fprintf(&sb, " (reply to %d)", h.ReplyToMsgID)
			}
		}
		if f, ok := m.GetFwdFrom(); ok {
			from := f.FromName
			if p, ok := f.GetFromID(); ok {
				from = b.name(markedPeer(p))
			}
			if from == "" {
				from = "someone"
			}
			sb.WriteString(" (forwarded from " + clip(oneLine(from), 80) + ")")
		}
		if k := mediaKind(m); k != "" {
			sb.WriteString(" [" + k + "]")
		}
		sb.WriteString(": " + strings.ReplaceAll(m.Message, "\n", "\n    ") + "\n")
	}
	return sb.String()
}

// mediaKind names what a message carries besides its text.
func mediaKind(m *tg.Message) string {
	media, ok := m.GetMedia()
	if !ok {
		return ""
	}
	switch md := media.(type) {
	case *tg.MessageMediaPhoto:
		return "photo"
	case *tg.MessageMediaDocument:
		doc, ok := md.Document.(*tg.Document)
		if !ok {
			return "file"
		}
		for _, a := range doc.Attributes {
			switch a := a.(type) {
			case *tg.DocumentAttributeAudio:
				if a.Voice {
					return "voice"
				}
				return "audio"
			case *tg.DocumentAttributeVideo:
				if a.RoundMessage {
					return "video message"
				}
				return "video"
			case *tg.DocumentAttributeSticker:
				return "sticker"
			case *tg.DocumentAttributeAnimated:
				return "gif"
			}
		}
		for _, a := range doc.Attributes {
			if a, ok := a.(*tg.DocumentAttributeFilename); ok {
				return "file " + clip(a.FileName, 60)
			}
		}
		return "file"
	case *tg.MessageMediaGeo, *tg.MessageMediaGeoLive, *tg.MessageMediaVenue:
		return "location"
	case *tg.MessageMediaContact:
		return "contact"
	case *tg.MessageMediaPoll:
		return "poll"
	case *tg.MessageMediaWebPage, *tg.MessageMediaEmpty:
		return ""
	}
	return "media"
}

// ---- actions -----------------------------------------------------------------------------------------------------

func (accountConn) Act(ctx context.Context, in connectors.Instance, action string, args map[string]string) (connectors.Result, error) {
	if action != "send" && action != "note" {
		return connectors.Result{}, connectors.ErrUnknownTool
	}
	text := strings.TrimSpace(args["text"])
	if text == "" {
		return connectors.Result{}, connectors.Sayf("text: required")
	}
	if utf8.RuneCountInString(text) > textMax {
		return connectors.Result{}, connectors.Sayf("text: at most %d characters in one message", textMax)
	}
	if action == "note" {
		if err := countNote(in); err != nil {
			return connectors.Result{}, err
		}
	}
	reply := 0
	if s := strings.TrimSpace(args["reply"]); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			return connectors.Result{}, connectors.Sayf("reply: the number of the message to answer")
		}
		reply = n
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	b := bookOf(in)
	var out connectors.Result
	err := withAPI(ctx, in, func(ctx context.Context, api *tg.Client) error {
		var peer tg.InputPeerClass = &tg.InputPeerSelf{}
		var id int64
		if action == "send" {
			var err error
			if peer, id, err = resolve(ctx, api, b, args["chat"]); err != nil {
				return err
			}
		}
		req := &tg.MessagesSendMessageRequest{Peer: peer, Message: text, RandomID: randomID(), NoWebpage: false}
		if reply > 0 {
			req.SetReplyTo(&tg.InputReplyToMessage{ReplyToMsgID: reply})
		}
		res, err := api.MessagesSendMessage(ctx, req)
		if err != nil {
			return err
		}
		where := chatName(b, id)
		out = connectors.Result{Text: "Sent to " + where}
		if n := sentID(res); n != 0 {
			out.Text = fmt.Sprintf("Sent to %s, message %d", where, n)
		}
		return nil
	})
	return out, err
}

// countNote keeps the hourly limit of notes to Saved Messages.
func countNote(in connectors.Instance) error {
	unlock := lock(in.ID)
	defer unlock()
	m := load(in)
	now := time.Now()
	m.Notes = slices.DeleteFunc(m.Notes, func(t time.Time) bool { return now.Sub(t) > time.Hour })
	if len(m.Notes) >= notesPerHour {
		return connectors.Sayf("%d notes in an hour already: wait, or put several things in one note", notesPerHour)
	}
	m.Notes = append(m.Notes, now)
	return keep(in, m)
}

func randomID() int64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return int64(binary.LittleEndian.Uint64(b[:]) &^ (1 << 63))
}

// sentID finds the id of the message a send made.
func sentID(res tg.UpdatesClass) int {
	switch u := res.(type) {
	case *tg.UpdateShortSentMessage:
		return u.ID
	case *tg.Updates:
		for _, x := range u.Updates {
			if x, ok := x.(*tg.UpdateMessageID); ok {
				return x.ID
			}
		}
	}
	return 0
}
