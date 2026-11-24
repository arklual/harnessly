package eventbus

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/store/storetest"
)

// openDB opens one database as two replicas would: SQLite by default, a throwaway schema of HUB_TEST_POSTGRES_URL
// when it is set.
func openDB(t *testing.T) (a, b *store.DB) {
	t.Helper()
	ctx := context.Background()
	dsn := storetest.SQLiteURL(t)
	if pg := os.Getenv("HUB_TEST_POSTGRES_URL"); pg != "" {
		admin, err := sql.Open("pgx", pg)
		if err != nil {
			t.Fatal(err)
		}
		schema := "bustest_" + store.RandomBase32(8)
		if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); admin.Close() })
		u, _ := url.Parse(pg)
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		dsn = u.String()
	}
	open := func() *store.DB {
		db, err := store.Open(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		return db
	}
	return open(), open()
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newBus(t *testing.T) (*Bus, *clock) {
	db, _ := openDB(t)
	c := &clock{t: time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)}
	return New(db, nil, c.now), c
}

func msg(owner, ext string, via ...string) Event {
	if len(via) == 0 {
		via = []string{ViaConnector}
	}
	return Event{Owner: owner, Type: "chat.message", Source: "telegram-account", Connection: "c1", Ext: ext, Actor: "@ann",
		Thread: "tg:1", Via: via, Facts: map[string]string{"direct": "true"}}
}

func TestAppendOrdersAndDeduplicates(t *testing.T) {
	b, _ := newBus(t)
	ctx := context.Background()
	e1, dup, err := b.Append(ctx, msg("u1", "m1"))
	if err != nil || dup || e1.Seq != 1 || e1.ID == "" || e1.V != Version {
		t.Fatalf("first: %+v dup=%v err=%v", e1, dup, err)
	}
	e2, _, _ := b.Append(ctx, msg("u1", "m2"))
	// The same message by a poll: one event, both ways listed.
	again, dup, err := b.Append(ctx, msg("u1", "m1", ViaPoll))
	if err != nil || !dup || again.Seq != e1.Seq || len(again.Via) != 2 {
		t.Fatalf("duplicate: %+v dup=%v err=%v", again, dup, err)
	}
	// Another owner's message of the same id is theirs.
	other, dup, _ := b.Append(ctx, msg("u2", "m1"))
	if dup || other.Seq <= e2.Seq {
		t.Fatalf("other owner: %+v dup=%v", other, dup)
	}
	got, err := b.List(ctx, Query{Owner: "u1"})
	if err != nil || len(got) != 2 || got[0].Seq != e1.Seq || got[1].Seq != e2.Seq || got[0].Facts["direct"] != "true" {
		t.Fatalf("u1's journal: %+v %v", got, err)
	}
	all, _ := b.List(ctx, Query{All: true})
	if len(all) != 3 {
		t.Fatalf("all: %d", len(all))
	}
}

func TestAppendRefuses(t *testing.T) {
	b, _ := newBus(t)
	ctx := context.Background()
	for name, e := range map[string]Event{
		"type":   {Owner: "u", Type: "Chat", Source: "x", Ext: "1", Via: []string{ViaSDK}},
		"source": {Owner: "u", Type: "a.b", Source: "X Y", Ext: "1", Via: []string{ViaSDK}},
		"no id":  {Owner: "u", Type: "a.b", Source: "x", Via: []string{ViaSDK}},
		"via":    {Owner: "u", Type: "a.b", Source: "x", Ext: "1", Via: []string{"carrier-pigeon"}},
		"fact":   {Owner: "u", Type: "a.b", Source: "x", Ext: "1", Via: []string{ViaSDK}, Facts: map[string]string{"bad key": "v"}},
	} {
		if _, _, err := b.Append(ctx, e); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A URL that is not http(s) is dropped, not refused.
	e, _, err := b.Append(ctx, Event{Owner: "u", Type: "a.b", Source: "x", Ext: "1", Via: []string{ViaSDK}, URL: "javascript:alert(1)"})
	if err != nil || e.URL != "" {
		t.Fatalf("url: %+v %v", e, err)
	}
}

func TestSubscriptionDeliversInOrderAndResumes(t *testing.T) {
	b, _ := newBus(t)
	ctx := context.Background()
	if _, _, err := b.Append(ctx, msg("u1", "before")); err != nil {
		t.Fatal(err)
	}
	s, err := b.Subscribe(ctx, Subscription{Owner: "u1", Consumer: "notify", Filter: Filter{Types: []string{"chat.*"}}}, FromEnd)
	if err != nil || s.Cursor != 1 {
		t.Fatalf("subscribe: %+v %v", s, err)
	}
	// Subscribing again gives the same subscription back.
	if again, _ := b.Subscribe(ctx, Subscription{Owner: "u1", Consumer: "notify"}, 0); again.ID != s.ID {
		t.Fatalf("subscribe twice: %s %s", again.ID, s.ID)
	}
	var got []string
	b.Consume("notify", func(_ context.Context, _ Subscription, e Event) error { got = append(got, e.Ext); return nil })
	for _, ext := range []string{"a", "b"} {
		mustAppend(t, b, msg("u1", ext))
	}
	mustAppend(t, b, msg("u2", "not-mine"))
	mustAppend(t, b, Event{Owner: "u1", Type: "mail.received", Source: "imap", Ext: "x", Via: []string{ViaConnector}})
	for b.Pass(ctx) {
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("delivered %v", got)
	}
	// A restart of the hub: a new bus on the same database delivers only what came after.
	b2 := New(b.db, nil, b.now)
	got = nil
	b2.Consume("notify", func(_ context.Context, _ Subscription, e Event) error { got = append(got, e.Ext); return nil })
	mustAppend(t, b2, msg("u1", "c"))
	for b2.Pass(ctx) {
	}
	if len(got) != 1 || got[0] != "c" {
		t.Fatalf("after a restart: %v", got)
	}
}

func TestRefusedEventIsRetriedAndKeepsOrder(t *testing.T) {
	b, c := newBus(t)
	ctx := context.Background()
	s, _ := b.Subscribe(ctx, Subscription{Owner: "u1", Consumer: "widget"}, FromEnd)
	fail := true
	var got []string
	b.Consume("widget", func(_ context.Context, _ Subscription, e Event) error {
		if e.Ext == "b" && fail {
			return errors.New("widget down")
		}
		got = append(got, e.Ext)
		return nil
	})
	for _, ext := range []string{"a", "b", "c"} {
		mustAppend(t, b, msg("u1", ext))
	}
	b.Pass(ctx)
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("before the failure: %v", got)
	}
	st, _ := b.Subscription(ctx, "u1", s.ID)
	if st.Failures != 1 || st.Cursor != 1 || st.LastError == "" || st.RetryAt.IsZero() {
		t.Fatalf("after the failure: %+v", st)
	}
	// Not due yet: nothing happens.
	if b.Pass(ctx) || len(got) != 1 {
		t.Fatalf("retried too early: %v", got)
	}
	c.add(2 * time.Second)
	fail = false
	for b.Pass(ctx) {
	}
	if len(got) != 3 || got[1] != "b" || got[2] != "c" {
		t.Fatalf("after the retry: %v", got)
	}
	st, _ = b.Subscription(ctx, "u1", s.ID)
	if st.Failures != 0 || st.State != StateActive || st.Cursor != 3 {
		t.Fatalf("recovered: %+v", st)
	}
}

func TestFailingAfterManyRefusals(t *testing.T) {
	b, c := newBus(t)
	ctx := context.Background()
	s, _ := b.Subscribe(ctx, Subscription{Owner: "u1", Consumer: "job"}, FromEnd)
	b.Consume("job", func(context.Context, Subscription, Event) error { panic("boom") })
	mustAppend(t, b, msg("u1", "a"))
	for range FailAfter {
		b.Pass(ctx)
		c.add(10 * time.Minute)
	}
	st, _ := b.Subscription(ctx, "u1", s.ID)
	if st.State != StateFailing || st.Failures != FailAfter || st.Cursor != 0 {
		t.Fatalf("%+v", st)
	}
	// Replay from the start makes it active again.
	if err := b.Rewind(ctx, "u1", s.ID, 0); err != nil {
		t.Fatal(err)
	}
	st, _ = b.Subscription(ctx, "u1", s.ID)
	if st.State != StateActive || st.Failures != 0 {
		t.Fatalf("rewound: %+v", st)
	}
}

func TestHubSubscriptionSeesEveryOwner(t *testing.T) {
	b, _ := newBus(t)
	ctx := context.Background()
	if _, err := b.Subscribe(ctx, Subscription{Owner: "", Consumer: "rules"}, FromEnd); err != nil {
		t.Fatal(err)
	}
	var owners []string
	b.Consume("rules", func(_ context.Context, _ Subscription, e Event) error { owners = append(owners, e.Owner); return nil })
	mustAppend(t, b, msg("u1", "a"))
	mustAppend(t, b, msg("u2", "a"))
	mustAppend(t, b, msg("", "deployment-hook"))
	for b.Pass(ctx) {
	}
	if len(owners) != 3 {
		t.Fatalf("%v", owners)
	}
}

func TestConcurrentAppendsKeepSeqDense(t *testing.T) {
	a, bdb := openDB(t)
	c := &clock{t: time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)}
	ba, bb := New(a, nil, c.now), New(bdb, nil, c.now)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bus := ba
			if i%2 == 1 {
				bus = bb
			}
			// Every pair writes the same message: one of them is a duplicate.
			if _, _, err := bus.Append(ctx, msg("u1", string(rune('a'+i/2)))); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, _ := ba.List(ctx, Query{Owner: "u1", Limit: 500})
	if len(got) != 20 {
		t.Fatalf("%d events", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i].Seq <= got[i-1].Seq {
			t.Fatalf("seq out of order at %d", i)
		}
	}
}

func TestSweepDropsOld(t *testing.T) {
	b, c := newBus(t)
	ctx := context.Background()
	mustAppend(t, b, msg("u1", "old"))
	c.add(DefaultRetention + time.Hour)
	mustAppend(t, b, msg("u1", "new"))
	n, err := b.Sweep(ctx)
	got, _ := b.List(ctx, Query{Owner: "u1"})
	if err != nil || n != 1 || len(got) != 1 || got[0].Ext != "new" {
		t.Fatalf("n=%d err=%v got=%+v", n, err, got)
	}
}

func TestFilter(t *testing.T) {
	yes, no := true, false
	e := Event{Type: "chat.message", Source: "telegram-account", Connection: "c1", Mine: false, Facts: map[string]string{"direct": "true"}}
	for name, c := range map[string]struct {
		f    Filter
		want bool
	}{
		"empty":        {Filter{}, true},
		"domain":       {Filter{Types: []string{"chat.*"}}, true},
		"other type":   {Filter{Types: []string{"mail.*", "chat.reaction"}}, false},
		"source":       {Filter{Sources: []string{"imap"}}, false},
		"connection":   {Filter{Connections: []string{"c1"}}, true},
		"fact":         {Filter{Facts: map[string]string{"direct": "true"}}, true},
		"fact differs": {Filter{Facts: map[string]string{"direct": "false"}}, false},
		"mine":         {Filter{Mine: &yes}, false},
		"not mine":     {Filter{Mine: &no}, true},
	} {
		if got := c.f.Match(e); got != c.want {
			t.Errorf("%s: %v", name, got)
		}
	}
}

func mustAppend(t *testing.T, b *Bus, e Event) {
	t.Helper()
	if _, _, err := b.Append(context.Background(), e); err != nil {
		t.Fatal(err)
	}
}

func TestLevelsKeepCorrespondencePersonal(t *testing.T) {
	b, _ := newBus(t)
	ctx := context.Background()
	add := func(owner, typ, space, org, vis string) {
		t.Helper()
		mustAppend(t, b, Event{Owner: owner, Type: typ, Source: "x", Ext: owner + typ + space + vis, Space: space, Org: org, Visibility: vis, Via: []string{ViaConnector}})
	}
	add("ann", "chat.message", "s1", "o1", "")        // personal by its domain
	add("ann", "chat.message", "s1", "o1", VisTeam)   // a source cannot widen correspondence
	add("ann", "issue.assigned", "s1", "o1", "")      // team
	add("bob", "mr.review_requested", "s2", "o1", "") // team, another space of the org
	add("bob", "mail.received", "s2", "o1", "")       // personal
	add("bob", "ci.failed", "s2", "o1", VisPerson)    // a source may narrow
	add("cat", "issue.assigned", "s3", "o2", "")      // another organization

	types := func(q Query) []string {
		t.Helper()
		es, err := b.List(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, e := range es {
			out = append(out, e.Owner+":"+e.Type)
		}
		return out
	}
	// Bob's view of space s1: his own events and the team events of s1, none of Ann's chats.
	if got := types(Query{Owner: "bob", Spaces: []string{"s1"}}); strings.Join(got, ",") != "ann:issue.assigned,bob:mr.review_requested,bob:mail.received,bob:ci.failed" {
		t.Fatalf("space: %v", got)
	}
	// An org admin's view: the team events of every space of o1, nothing of o2, no correspondence of others.
	if got := types(Query{Owner: "admin", Org: "o1"}); strings.Join(got, ",") != "ann:issue.assigned,bob:mr.review_requested" {
		t.Fatalf("org: %v", got)
	}
	// Newest first, backwards from the newest, then from a seq.
	all, _ := b.List(ctx, Query{All: true, Before: -1, Limit: 2})
	if len(all) != 2 || all[0].Seq <= all[1].Seq || all[0].Owner != "cat" {
		t.Fatalf("backwards: %+v", all)
	}
	older, _ := b.List(ctx, Query{All: true, Before: all[1].Seq, Limit: 10})
	if len(older) != 5 || older[0].Seq != all[1].Seq-1 {
		t.Fatalf("older: %d", len(older))
	}
}

func TestVisibilityOf(t *testing.T) {
	for typ, want := range map[string]string{"chat.message": VisPerson, "mail.unanswered": VisPerson, "calendar.starting": VisPerson,
		"issue.assigned": VisTeam, "ci.failed": VisTeam, "schedule.fired": VisTeam, "feed.item": VisTeam} {
		if got := VisibilityOf(typ); got != want {
			t.Errorf("%s: %s", typ, got)
		}
	}
}

// Every kind of event is a trigger from its first event, in the mode digest; a kind switched off is not journaled,
// and switching it on again lets the next one in.
func TestCatalogOfTriggers(t *testing.T) {
	b, _ := newBus(t)
	ctx := context.Background()
	mustAppend(t, b, msg("u1", "a"))
	mustAppend(t, b, msg("u1", "b"))
	ts, err := b.Triggers(ctx, "u1")
	if err != nil || len(ts) != 1 || ts[0].Mode != ModeDigest || ts[0].Seen != 2 || ts[0].Type != "chat.message" {
		t.Fatalf("catalog: %+v %v", ts, err)
	}
	if err := b.SetMode(ctx, "u1", Trigger{Source: "telegram-account", Connection: "c1", Type: "chat.message", Mode: ModeOff}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.Append(ctx, msg("u1", "c")); !errors.Is(err, ErrOff) {
		t.Fatalf("an event of a switched-off kind: %v", err)
	}
	if got, _ := b.List(ctx, Query{Owner: "u1"}); len(got) != 2 {
		t.Fatalf("journaled %d", len(got))
	}
	// Another person's same kind is theirs, untouched.
	if _, _, err := b.Append(ctx, msg("u2", "c")); err != nil {
		t.Fatal(err)
	}
	if err := b.SetMode(ctx, "u1", Trigger{Source: "telegram-account", Connection: "c1", Type: "chat.message", Mode: ModeNotify}); err != nil {
		t.Fatal(err)
	}
	e, _, err := b.Append(ctx, msg("u1", "d"))
	if err != nil || b.ModeOf(ctx, e) != ModeNotify {
		t.Fatalf("notify: %v %s", err, b.ModeOf(ctx, e))
	}
	if err := b.SetMode(ctx, "u1", Trigger{Source: "x", Type: "a.b", Mode: "loud"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a mode that is not: %v", err)
	}
	// A kind switched off before its first event.
	if err := b.SetMode(ctx, "u1", Trigger{Source: "imap", Type: "mail.received", Mode: ModeOff}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.Append(ctx, Event{Owner: "u1", Type: "mail.received", Source: "imap", Ext: "m", Via: []string{ViaConnector}}); !errors.Is(err, ErrOff) {
		t.Fatalf("off before the first: %v", err)
	}
}
