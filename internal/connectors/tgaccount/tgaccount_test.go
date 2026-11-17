package tgaccount

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/tg"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

// instance is a connection with its record in memory and its folder in a temporary directory.
func instance(t *testing.T, config map[string]string, secrets map[string]string) connectors.Instance {
	t.Helper()
	var rec []byte
	return connectors.Instance{
		ID: "cx-test-" + t.Name(), Connector: ID, Config: config, Dir: t.TempDir(),
		Secret: func(f string) (string, error) { return secrets[f], nil },
		Load: func(v any) error {
			if rec == nil {
				return nil
			}
			return json.Unmarshal(rec, v)
		},
		Store: func(v any) error {
			var err error
			rec, err = json.Marshal(v)
			return err
		},
	}
}

const hash = "0123456789abcdef0123456789abcdef"

func TestRegistered(t *testing.T) {
	c, ok := connectors.Lookup(ID)
	if !ok {
		t.Fatal("not registered")
	}
	sp := c.Spec()
	if sp.Inbox != "telegram" || sp.Kind != connectors.Chat {
		t.Fatalf("spec: %+v", sp)
	}
	for _, a := range sp.Actions {
		if a.Name == "send" && a.Direct {
			t.Fatal("a send as the person must go through the card")
		}
	}
	for _, iface := range []any{c.(connectors.Reader), c.(connectors.Actor), c.(connectors.Watcher),
		c.(connectors.Stepper), c.(connectors.Forgetter), c.(connectors.ActGate)} {
		_ = iface
	}
}

func TestApp(t *testing.T) {
	cases := []struct {
		id, hash string
		ok       bool
	}{
		{"12345", hash, true},
		{"", hash, false},
		{"abc", hash, false},
		{"12345", "short", false},
		{"12345", strings.Repeat("z", 32), false},
	}
	for _, c := range cases {
		in := instance(t, map[string]string{"apiId": c.id}, map[string]string{"apiHash": c.hash})
		_, _, err := app(in)
		if (err == nil) != c.ok {
			t.Errorf("app(%q, %q): %v", c.id, c.hash, err)
		}
		var said *connectors.Said
		if err != nil && !errors.As(err, &said) {
			t.Errorf("not for the person: %v", err)
		}
	}
}

func TestMarkedIDs(t *testing.T) {
	for _, c := range []struct {
		peer tg.PeerClass
		want int64
		kind string
		bare int64
	}{
		{&tg.PeerUser{UserID: 42}, 42, kindUser, 42},
		{&tg.PeerChat{ChatID: 42}, -42, kindChat, 42},
		{&tg.PeerChannel{ChannelID: 1234567890}, -1001234567890, kindChannel, 1234567890},
	} {
		id := markedPeer(c.peer)
		if id != c.want {
			t.Errorf("%T: %d, want %d", c.peer, id, c.want)
		}
		kind, bare := unmark(id)
		if kind != c.kind || bare != c.bare {
			t.Errorf("unmark(%d) = %s %d", id, kind, bare)
		}
	}
}

func TestBookKeepsPeersAndHashes(t *testing.T) {
	in := instance(t, nil, nil)
	b := bookOf(in)
	b.learn([]tg.UserClass{&tg.User{ID: 7, AccessHash: 77, FirstName: "Ann", Username: "ann"}},
		[]tg.ChatClass{&tg.Channel{ID: 9, AccessHash: 99, Title: "Team  chat", Megagroup: true}, &tg.Chat{ID: 5, Title: "Old"}})
	// A min user comes without the hash: the known one stays.
	b.learn([]tg.UserClass{&tg.User{ID: 7, FirstName: "Ann", Username: "ann", Min: true}}, nil)
	forgetBook(in.ID)

	b = bookOf(in)
	if p, ok := b.input(7); !ok || p.(*tg.InputPeerUser).AccessHash != 77 {
		t.Fatalf("user: %v %v", p, ok)
	}
	if p, ok := b.input(-1000000000009); !ok || p.(*tg.InputPeerChannel).AccessHash != 99 {
		t.Fatalf("channel: %v %v", p, ok)
	}
	if _, ok := b.input(-5); !ok {
		t.Fatal("a basic group needs no hash")
	}
	if _, ok := b.input(8); ok {
		t.Fatal("an unknown user has no hash")
	}
	if got := b.name(7); got != "Ann (@ann)" {
		t.Fatalf("name: %q", got)
	}
	if id, ok := b.byTitle("team chat"); !ok || id != -1000000000009 {
		t.Fatalf("byTitle: %d %v", id, ok)
	}
	st, err := os.Stat(filepath.Join(in.Dir, "peers.json"))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("peers.json: %v %v", st, err)
	}
}

func TestEvents(t *testing.T) {
	in := instance(t, nil, nil)
	b := bookOf(in)
	b.learn([]tg.UserClass{&tg.User{ID: 7, AccessHash: 1, FirstName: "Ann"}},
		[]tg.ChatClass{&tg.Channel{ID: 9, AccessHash: 2, Title: "Team", Megagroup: true},
			&tg.Channel{ID: 10, AccessHash: 3, Title: "News", Broadcast: true, Username: "news"}})
	const self = 1
	private := &tg.Message{ID: 3, Date: 1700000000, PeerID: &tg.PeerUser{UserID: 7}, Message: "secret words"}
	private.SetFromID(&tg.PeerUser{UserID: 7})
	group := &tg.Message{ID: 4, Date: 1700000000, PeerID: &tg.PeerChannel{ChannelID: 9}, Message: "hi all"}
	group.SetFromID(&tg.PeerUser{UserID: 7})
	mention := &tg.Message{ID: 5, Date: 1700000000, PeerID: &tg.PeerChannel{ChannelID: 9}, Message: "@me look", Mentioned: true}
	mention.SetFromID(&tg.PeerUser{UserID: 7})
	post := &tg.Message{ID: 6, Date: 1700000000, PeerID: &tg.PeerChannel{ChannelID: 10}, Message: "news"}
	saved := &tg.Message{ID: 7, Date: 1700000000, PeerID: &tg.PeerUser{UserID: self}, Out: true, Message: "note"}

	ev, ok := eventOf(b, eventsDirect, self, private)
	if !ok || ev.ID != "7:3" || ev.Thread != "tg:7" || ev.Ref != "message chat=7 id=3" || ev.Facts["chat"] != "private" ||
		ev.Actor != "Ann" || ev.Title != "" || !ev.At.Equal(time.Unix(1700000000, 0)) {
		t.Fatalf("private: %+v %v", ev, ok)
	}
	raw, _ := json.Marshal(ev)
	if strings.Contains(string(raw), "secret") {
		t.Fatal("an event carries no text")
	}
	if _, ok := eventOf(b, eventsDirect, self, group); ok {
		t.Fatal("a group message without a mention is not an event by default")
	}
	if ev, ok := eventOf(b, eventsAll, self, group); !ok || ev.Title != "Team" || ev.Facts["chat"] != "group" {
		t.Fatalf("group in all: %+v %v", ev, ok)
	}
	if ev, ok := eventOf(b, eventsDirect, self, mention); !ok || ev.Facts["mentioned"] != "true" {
		t.Fatalf("mention: %+v %v", ev, ok)
	}
	for _, m := range []*tg.Message{post, saved} {
		if _, ok := eventOf(b, eventsAll, self, m); ok {
			t.Fatalf("not an event: %+v", m)
		}
	}
	if _, ok := eventOf(b, eventsNone, self, private); ok {
		t.Fatal("none")
	}
}

func TestLines(t *testing.T) {
	in := instance(t, nil, nil)
	b := bookOf(in)
	b.learn([]tg.UserClass{&tg.User{ID: 7, AccessHash: 1, FirstName: "Ann"}}, nil)
	m := &tg.Message{ID: 3, Date: 1700000000, PeerID: &tg.PeerUser{UserID: 7}, Message: "two\nlines"}
	m.SetReplyTo(&tg.MessageReplyHeader{ReplyToMsgID: 2})
	m.SetMedia(&tg.MessageMediaPhoto{})
	out := &tg.Message{ID: 4, Date: 1700000000, PeerID: &tg.PeerUser{UserID: 7}, Out: true, Message: "ok"}
	got := lines(b, 1, []*tg.Message{m, out}, false)
	want := "[7:3] 2023-11-14T22:13:20Z Ann (reply to 2) [photo]: two\n    lines\n[7:4] 2023-11-14T22:13:20Z me: ok\n"
	if got != want {
		t.Fatalf("lines:\n%s\nwant\n%s", got, want)
	}
}

func TestNextWaitsForAKeptConnection(t *testing.T) {
	in := instance(t, nil, nil)
	in.Dir = ""
	if _, has := (accountConn{}).Next(context.Background(), in); has {
		t.Fatal("no step during the check")
	}
	in = instance(t, nil, nil)
	if err := os.WriteFile(sessionFile(in), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = keep(in, memo{Signed: true, Who: "Ann"})
	if _, has := (accountConn{}).Next(context.Background(), in); has {
		t.Fatal("no step once signed in")
	}
	if err := (accountConn{}).CanAct(in, "send"); err != nil {
		t.Fatal(err)
	}
	signOut(in)
	if err := (accountConn{}).CanAct(in, "send"); err == nil {
		t.Fatal("no action before the sign-in")
	}
}

func TestWatchWaitsForTheSignIn(t *testing.T) {
	pause = 10 * time.Millisecond
	in := instance(t, map[string]string{"apiId": "1"}, map[string]string{"apiHash": hash})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := (accountConn{}).Watch(ctx, in, "", nil); err != nil {
		t.Fatalf("a source waiting for the sign-in is not in error: %v", err)
	}
}

func TestNotesPerHour(t *testing.T) {
	in := instance(t, nil, nil)
	for i := range notesPerHour {
		if err := countNote(in); err != nil {
			t.Fatalf("note %d: %v", i, err)
		}
	}
	if err := countNote(in); err == nil {
		t.Fatal("the limit")
	}
	m := load(in)
	for i := range m.Notes {
		m.Notes[i] = m.Notes[i].Add(-2 * time.Hour)
	}
	_ = keep(in, m)
	if err := countNote(in); err != nil {
		t.Fatal(err)
	}
}

func TestSentID(t *testing.T) {
	if sentID(&tg.UpdateShortSentMessage{ID: 5}) != 5 {
		t.Fatal("short")
	}
	if sentID(&tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateMessageID{ID: 6}}}) != 6 {
		t.Fatal("updates")
	}
}

func TestQRImage(t *testing.T) {
	// A token is only the bytes Telegram gives; the picture is a PNG data URL.
	tok := qrToken(t)
	img, err := qrImage(tok)
	if err != nil || !strings.HasPrefix(img, "data:image/png;base64,") {
		t.Fatalf("%v %.40s", err, img)
	}
}

func qrToken(t *testing.T) qrlogin.Token {
	t.Helper()
	return qrlogin.NewToken([]byte("0123456789abcdef0123456789abcdef"), int(time.Now().Add(time.Minute).Unix()))
}
