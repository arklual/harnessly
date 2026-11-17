package tgaccount

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/gotd/td/tg"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

// Marked ids, as Telegram's own clients write them: a user's id as it is, a basic group's as -id, a channel's or a
// supergroup's as -100… (-(1e12+id)).
const channelBase = 1_000_000_000_000

const (
	kindUser    = "user"
	kindChat    = "chat"
	kindChannel = "channel"
)

func markUser(id int64) int64    { return id }
func markChat(id int64) int64    { return -id }
func markChannel(id int64) int64 { return -(channelBase + id) }

// unmark says which kind a marked id is and its bare id.
func unmark(id int64) (string, int64) {
	switch {
	case id > 0:
		return kindUser, id
	case id <= -channelBase:
		return kindChannel, -id - channelBase
	default:
		return kindChat, -id
	}
}

func markedPeer(p tg.PeerClass) int64 {
	switch p := p.(type) {
	case *tg.PeerUser:
		return markUser(p.UserID)
	case *tg.PeerChat:
		return markChat(p.ChatID)
	case *tg.PeerChannel:
		return markChannel(p.ChannelID)
	}
	return 0
}

// peer is what the book knows of one user, group or channel: enough to address it again (its access hash) and to
// name it.
type peer struct {
	Kind     string `json:"kind"`
	Hash     int64  `json:"hash,omitempty"`
	Title    string `json:"title,omitempty"`
	Username string `json:"username,omitempty"`
	// Group says a channel is a supergroup (people talk in it), not a channel of posts.
	Group bool `json:"group,omitempty"`
	Bot   bool `json:"bot,omitempty"`
}

// book is the connection's peers by marked id, kept in peers.json of its folder.
type book struct {
	mu    sync.Mutex
	path  string
	peers map[int64]peer
	dirty bool
}

var (
	booksMu sync.Mutex
	books   = map[string]*book{}
)

func bookOf(in connectors.Instance) *book {
	path := ""
	if in.Dir != "" {
		path = filepath.Join(in.Dir, "peers.json")
	}
	booksMu.Lock()
	defer booksMu.Unlock()
	if b := books[in.ID]; b != nil && b.path == path {
		return b
	}
	b := &book{path: path, peers: map[int64]peer{}}
	if path != "" {
		if raw, err := os.ReadFile(path); err == nil {
			var kept map[string]peer
			if json.Unmarshal(raw, &kept) == nil {
				for k, p := range kept {
					if id, err := strconv.ParseInt(k, 10, 64); err == nil {
						b.peers[id] = p
					}
				}
			}
		}
	}
	books[in.ID] = b
	return b
}

// forgetBook drops the cached book of a connection (it was removed).
func forgetBook(id string) {
	booksMu.Lock()
	delete(books, id)
	booksMu.Unlock()
}

func (b *book) get(id int64) (peer, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	p, ok := b.peers[id]
	return p, ok
}

func (b *book) put(id int64, p peer) {
	b.mu.Lock()
	defer b.mu.Unlock()
	old, had := b.peers[id]
	if p.Hash == 0 {
		// A min constructor comes without the hash: keep the one known.
		p.Hash = old.Hash
	}
	if had && old == p {
		return
	}
	b.peers[id] = p
	b.dirty = true
}

// save writes the book when it changed.
func (b *book) save() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.dirty || b.path == "" {
		return
	}
	out := make(map[string]peer, len(b.peers))
	for id, p := range b.peers {
		out[strconv.FormatInt(id, 10)] = p
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(b.path), 0o700); err != nil {
		return
	}
	tmp := b.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil && os.Rename(tmp, b.path) == nil {
		b.dirty = false
	}
}

func (b *book) learnUser(u *tg.User) {
	if u == nil {
		return
	}
	title := oneLine(u.FirstName + " " + u.LastName)
	if u.Deleted {
		title = "Deleted account"
	}
	b.put(markUser(u.ID), peer{Kind: kindUser, Hash: u.AccessHash, Title: title, Username: u.Username, Bot: u.Bot})
}

func (b *book) learnChat(c tg.ChatClass) {
	switch c := c.(type) {
	case *tg.Chat:
		b.put(markChat(c.ID), peer{Kind: kindChat, Title: oneLine(c.Title)})
	case *tg.ChatForbidden:
		b.put(markChat(c.ID), peer{Kind: kindChat, Title: oneLine(c.Title)})
	case *tg.Channel:
		b.put(markChannel(c.ID), peer{Kind: kindChannel, Hash: c.AccessHash, Title: oneLine(c.Title),
			Username: c.Username, Group: c.Megagroup || c.Gigagroup})
	case *tg.ChannelForbidden:
		b.put(markChannel(c.ID), peer{Kind: kindChannel, Hash: c.AccessHash, Title: oneLine(c.Title), Group: c.Megagroup})
	}
}

func (b *book) learn(users []tg.UserClass, chats []tg.ChatClass) {
	for _, u := range users {
		if u, ok := u.(*tg.User); ok {
			b.learnUser(u)
		}
	}
	for _, c := range chats {
		b.learnChat(c)
	}
	b.save()
}

func (b *book) learnEntities(e tg.Entities) {
	for _, u := range e.Users {
		b.learnUser(u)
	}
	for _, c := range e.Chats {
		b.learnChat(c)
	}
	for _, c := range e.Channels {
		b.learnChat(c)
	}
	b.save()
}

// input is how Telegram is told about a peer of the book.
func (b *book) input(id int64) (tg.InputPeerClass, bool) {
	p, ok := b.get(id)
	kind, bare := unmark(id)
	switch kind {
	case kindChat:
		return &tg.InputPeerChat{ChatID: bare}, true
	case kindUser:
		if !ok {
			return nil, false
		}
		return &tg.InputPeerUser{UserID: bare, AccessHash: p.Hash}, true
	default:
		if !ok {
			return nil, false
		}
		return &tg.InputPeerChannel{ChannelID: bare, AccessHash: p.Hash}, true
	}
}

// name is how a peer is shown: its title and @username.
func (b *book) name(id int64) string {
	p, ok := b.get(id)
	if !ok {
		return strconv.FormatInt(id, 10)
	}
	switch {
	case p.Title != "" && p.Username != "":
		return p.Title + " (@" + p.Username + ")"
	case p.Title != "":
		return p.Title
	case p.Username != "":
		return "@" + p.Username
	}
	return strconv.FormatInt(id, 10)
}

// senderName names who wrote a message.
func (b *book) senderName(m *tg.Message, self int64) string {
	if m.Out {
		return "me"
	}
	from, ok := m.GetFromID()
	if !ok {
		from = m.PeerID
	}
	id := markedPeer(from)
	if id == self {
		return "me"
	}
	return clip(b.name(id), 80)
}

func userName(u *tg.User) string {
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if u.Deleted {
		name = "Deleted account"
	}
	switch {
	case name != "" && u.Username != "":
		return oneLine(name) + " (@" + u.Username + ")"
	case name != "":
		return oneLine(name)
	case u.Username != "":
		return "@" + u.Username
	}
	return strconv.FormatInt(u.ID, 10)
}

// resolve finds the peer a chat argument names: "me", @username, a t.me link, +phone, or a marked id.
func resolve(ctx context.Context, api *tg.Client, b *book, arg string) (tg.InputPeerClass, int64, error) {
	arg = strings.TrimSpace(arg)
	switch strings.ToLower(arg) {
	case "":
		return nil, 0, connectors.Sayf("chat: required")
	case "me", "self", "saved":
		return &tg.InputPeerSelf{}, 0, nil
	}
	if s, ok := strings.CutPrefix(arg, "https://t.me/"); ok {
		arg = "@" + strings.SplitN(s, "/", 2)[0]
	}
	if s, ok := strings.CutPrefix(arg, "t.me/"); ok {
		arg = "@" + strings.SplitN(s, "/", 2)[0]
	}
	if s, ok := strings.CutPrefix(arg, "@"); ok {
		res, err := api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: s})
		if err != nil {
			if strings.Contains(err.Error(), "USERNAME_NOT_OCCUPIED") || strings.Contains(err.Error(), "USERNAME_INVALID") {
				return nil, 0, connectors.Sayf("no Telegram user or chat @%s", s)
			}
			return nil, 0, err
		}
		b.learn(res.Users, res.Chats)
		return peerOf(b, res.Peer)
	}
	if strings.HasPrefix(arg, "+") {
		res, err := api.ContactsResolvePhone(ctx, strings.TrimPrefix(arg, "+"))
		if err != nil {
			if strings.Contains(err.Error(), "PHONE_NOT_OCCUPIED") {
				return nil, 0, connectors.Sayf("no Telegram account with the phone %s, or it hides its number", arg)
			}
			return nil, 0, err
		}
		b.learn(res.Users, res.Chats)
		return peerOf(b, res.Peer)
	}
	id, err := strconv.ParseInt(arg, 10, 64)
	if err != nil {
		if id, ok := b.byTitle(arg); ok {
			return peerOrDialogs(ctx, api, b, id)
		}
		return nil, 0, connectors.Sayf("chat %q: name it by @username, +phone, \"me\" or the id from chats", arg)
	}
	return peerOrDialogs(ctx, api, b, id)
}

func peerOf(b *book, p tg.PeerClass) (tg.InputPeerClass, int64, error) {
	id := markedPeer(p)
	in, ok := b.input(id)
	if !ok {
		return nil, 0, connectors.ErrNotFound
	}
	return in, id, nil
}

// peerOrDialogs addresses a marked id from the book, reading the dialogs once when the book does not know it.
func peerOrDialogs(ctx context.Context, api *tg.Client, b *book, id int64) (tg.InputPeerClass, int64, error) {
	if in, ok := b.input(id); ok {
		return in, id, nil
	}
	if _, err := dialogs(ctx, api, b, 200); err != nil {
		return nil, 0, err
	}
	if in, ok := b.input(id); ok {
		return in, id, nil
	}
	return nil, 0, connectors.Sayf("chat %d is not among the account's chats: name it by @username or +phone", id)
}

// byTitle finds a known chat by its exact title, when only one has it.
func (b *book) byTitle(title string) (int64, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var found int64
	n := 0
	for id, p := range b.peers {
		if strings.EqualFold(p.Title, title) {
			found, n = id, n+1
		}
	}
	return found, n == 1
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
