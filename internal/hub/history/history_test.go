package history

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

var (
	mine   = Owner{MachineID: "m-1", UserID: "u-sergey"}
	other  = Owner{MachineID: "m-1", UserID: "u-roma"} // the same machine named by somebody else's frame
	second = Owner{MachineID: "m-2", UserID: "u-sergey"}
	now    = time.UnixMilli(1_800_000_000_000)
)

func openDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(context.Background(), "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func j(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func conv(id string, rev int64, prompt int64) map[string]any {
	return map[string]any{"id": id, "rev": rev, "title": "chat " + id, "lastPromptAt": prompt, "state": map[string]any{"k": "idle"}}
}

func upsert(id string, rev, prompt int64) json.RawMessage {
	return j(map[string]any{"type": "conversation_upsert", "conv": conv(id, rev, prompt)})
}

func turn(id string, started int64) map[string]any {
	return map[string]any{"id": id, "startedAt": started, "endedAt": started + 10, "status": "done", "answerEntryId": "a-" + id}
}

func entry(id, turnID string, at int64) map[string]any {
	return map[string]any{"id": id, "turnId": turnID, "at": at, "rev": 1, "t": "user", "text": id}
}

func live(ch string, from int64, events ...json.RawMessage) hubproto.HistoryEvents {
	return hubproto.HistoryEvents{Ch: ch, Epoch: "ep-1", FromSeq: from, Events: events}
}

func ids(t *testing.T, raws []json.RawMessage) []string {
	t.Helper()
	var out []string
	for _, r := range raws {
		var x struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(r, &x); err != nil {
			t.Fatal(err)
		}
		out = append(out, x.ID)
	}
	return out
}

func TestTheChatListIsStoredOnceAndServedToItsOwnerOnly(t *testing.T) {
	ctx, db := context.Background(), openDB(t)
	res, err := ApplyFrame(ctx, db, mine, live("convs", 1, upsert("tab-a", 1, 10), upsert("tab-b", 1, 20)), now)
	if err != nil || res.AckSeq != 2 || res.Applied != 2 || res.Gap {
		t.Fatalf("%+v %v", res, err)
	}
	// A repeat changes nothing; an overlap goes on from the cursor; a frame after a hole is refused with where to resend.
	if res, _ := ApplyFrame(ctx, db, mine, live("convs", 1, upsert("tab-a", 1, 10), upsert("tab-b", 1, 20)), now); res.Applied != 0 || res.AckSeq != 2 {
		t.Fatalf("a repeat: %+v", res)
	}
	if res, _ := ApplyFrame(ctx, db, mine, live("convs", 2, upsert("tab-b", 1, 20), upsert("tab-c", 1, 30)), now); res.Applied != 1 || res.AckSeq != 3 {
		t.Fatalf("an overlap: %+v", res)
	}
	if res, _ := ApplyFrame(ctx, db, mine, live("convs", 9, upsert("tab-d", 1, 40)), now); !res.Gap || res.AckSeq != 3 {
		t.Fatalf("a gap: %+v", res)
	}
	// Somebody else's frame never writes into the person's chat.
	if _, err := ApplyFrame(ctx, db, other, live("conv:tab-a", 1, j(map[string]any{"type": "turn_started", "turn": turn("c:x#0", 1)})), now); err != nil {
		t.Fatal(err)
	}
	chats, err := List(ctx, db, "u-sergey", []string{"m-1"})
	if err != nil || len(chats) != 3 {
		t.Fatalf("%v %v", chats, err)
	}
	if got := ids(t, []json.RawMessage{chats[0].Record, chats[1].Record, chats[2].Record}); fmt.Sprint(got) != "[tab-c tab-b tab-a]" {
		t.Errorf("newest prompt first: %v", got)
	}
	if chats, _ := List(ctx, db, "u-roma", []string{"m-1"}); len(chats) != 0 {
		t.Errorf("another person sees %v", chats)
	}
	if chats, _ := List(ctx, db, "u-sergey", []string{"m-2"}); len(chats) != 0 {
		t.Errorf("a machine the person may not open: %v", chats)
	}
	if page, err := Read(ctx, db, "u-sergey", "tab-a", []string{"m-1"}, "", 33); err != nil || len(page.Turns) != 0 {
		t.Errorf("somebody else's turn was stored: %+v %v", page, err)
	}
	// A removed chat is not listed; its state follows the newer revision only.
	_, _ = ApplyFrame(ctx, db, mine, live("convs", 4, j(map[string]any{"type": "conversation_state", "id": "tab-b", "rev": 2, "state": map[string]any{"k": "running"}}),
		j(map[string]any{"type": "conversation_removed", "id": "tab-c"})), now)
	chats, _ = List(ctx, db, "u-sergey", []string{"m-1"})
	if len(chats) != 2 {
		t.Fatalf("after removal: %d", len(chats))
	}
	var b struct {
		Rev   int64 `json:"rev"`
		State struct {
			K string `json:"k"`
		} `json:"state"`
	}
	_ = json.Unmarshal(chats[0].Record, &b)
	if b.Rev != 2 || b.State.K != "running" {
		t.Errorf("the state did not follow: %s", chats[0].Record)
	}
}

func TestAChatPageIsTheMachinesOwnShape(t *testing.T) {
	ctx, db := context.Background(), openDB(t)
	_ = ApplyBackfill(ctx, db, mine, hubproto.HistoryBackfill{Ch: "convs", Events: []json.RawMessage{upsert("tab-a", 1, 1)}}, now)
	var turns, entries []any
	for i := range 40 {
		id := fmt.Sprintf("c:t%02d#0", i)
		turns = append(turns, turn(id, int64(1000+i)))
		entries = append(entries, entry(id, id, int64(1000+i)), entry("a-"+id, id, int64(1000+i)), entry("s-"+id, id, int64(1000+i)))
	}
	if err := ApplyBackfill(ctx, db, mine, hubproto.HistoryBackfill{Ch: "conv:tab-a", Events: []json.RawMessage{
		j(map[string]any{"type": "feed_reset", "turns": turns, "entries": entries, "beforeTurnId": nil}),
	}}, now); err != nil {
		t.Fatal(err)
	}
	page, err := Read(ctx, db, "u-sergey", "tab-a", []string{"m-1"}, "", 33)
	if err != nil {
		t.Fatal(err)
	}
	got := ids(t, page.Turns)
	if len(got) != 33 || got[0] != "c:t07#0" || got[32] != "c:t39#0" || page.BeforeTurnID == nil || *page.BeforeTurnID != "c:t07#0" {
		t.Fatalf("turns %v before %v", got, page.BeforeTurnID)
	}
	// The newest three turns whole (three entries each), the other thirty by their face and answer.
	if len(page.Entries) != 3*3+30*2 {
		t.Errorf("entries %d", len(page.Entries))
	}
	older, _ := Read(ctx, db, "u-sergey", "tab-a", []string{"m-1"}, *page.BeforeTurnID, 33)
	// a page without the walk's mark does not make the chat whole: its oldest page still offers more, and
	// past it the page is empty and says the copy is not complete, for the page to ask the machine.
	if got := ids(t, older.Turns); len(got) != 7 || got[6] != "c:t06#0" || older.Complete ||
		older.BeforeTurnID == nil || *older.BeforeTurnID != "c:t00#0" {
		t.Errorf("the older page %v %v %v", got, older.BeforeTurnID, older.Complete)
	}
	if past, _ := Read(ctx, db, "u-sergey", "tab-a", []string{"m-1"}, "c:t00#0", 33); len(past.Turns) != 0 || past.BeforeTurnID != nil || past.Complete {
		t.Errorf("past the copy %d %v %v", len(past.Turns), past.BeforeTurnID, past.Complete)
	}
	// The walk reaches the first turn: the chat is whole, its oldest page ends there.
	if err := ApplyBackfill(ctx, db, mine, hubproto.HistoryBackfill{Ch: "conv:tab-a", Events: []json.RawMessage{
		j(map[string]any{"type": "feed_reset", "turns": turns, "entries": entries, "beforeTurnId": nil, "history": map[string]any{"from": ""}}),
	}}, now); err != nil {
		t.Fatal(err)
	}
	if whole, _ := Read(ctx, db, "u-sergey", "tab-a", []string{"m-1"}, *page.BeforeTurnID, 33); !whole.Complete || whole.BeforeTurnID != nil {
		t.Errorf("the whole chat %v %v", whole.BeforeTurnID, whole.Complete)
	}
	_, steps, err := TurnEntries(ctx, db, "u-sergey", "tab-a", "c:t00#0", []string{"m-1"})
	if err != nil || len(steps) != 3 {
		t.Errorf("steps %d %v", len(steps), err)
	}
	if _, err := Read(ctx, db, "u-roma", "tab-a", []string{"m-1"}, "", 33); err != ErrNotFound {
		t.Errorf("another person reads the chat: %v", err)
	}
	// An ended turn is not made running by a late copy, and an entry keeps its higher revision.
	running := turn("c:t39#0", 1039)
	delete(running, "endedAt")
	running["status"] = "running"
	stale := entry("s-c:t39#0", "c:t39#0", 1039)
	stale["rev"], stale["text"] = 0, "old"
	_, _ = ApplyFrame(ctx, db, mine, live("conv:tab-a", 1,
		j(map[string]any{"type": "turn_started", "turn": running}), j(map[string]any{"type": "step", "entry": stale})), now)
	page, _ = Read(ctx, db, "u-sergey", "tab-a", []string{"m-1"}, "", 1)
	if string(page.Turns[0]) == string(j(running)) {
		t.Errorf("an ended turn runs again: %s", page.Turns[0])
	}
	for _, e := range page.Entries {
		if string(e) == string(j(stale)) {
			t.Errorf("an older revision of an entry won: %s", e)
		}
	}
}

func TestAChatIsWalkedWholeByItsCursor(t *testing.T) {
	ctx, db := context.Background(), openDB(t)
	_ = ApplyBackfill(ctx, db, mine, hubproto.HistoryBackfill{Ch: "convs", Events: []json.RawMessage{upsert("tab-a", 3, 1), upsert("tab-b", 1, 2)}}, now)
	_ = ApplyBackfill(ctx, db, second, hubproto.HistoryBackfill{Ch: "convs", Events: []json.RawMessage{upsert("tab-z", 1, 1)}}, now)
	pending := func() map[string]string {
		st, err := Start(ctx, db, "m-1")
		if err != nil {
			t.Fatal(err)
		}
		return st.Pending
	}
	if got := pending(); len(got) != 2 || got["tab-a"] != "" {
		t.Fatalf("a chat with a record only is pending from its newest page: %v", got)
	}
	page := func(from *string, before any, turns ...string) json.RawMessage {
		var ts []any
		for i, id := range turns {
			ts = append(ts, turn(id, int64(100+i)))
		}
		ev := map[string]any{"type": "feed_reset", "turns": ts, "entries": []any{}, "beforeTurnId": before}
		if from != nil {
			ev["history"] = map[string]string{"from": *from}
		}
		return j(ev)
	}
	at := func(s string) *string { return &s }
	fill := func(events ...json.RawMessage) {
		if err := ApplyBackfill(ctx, db, mine, hubproto.HistoryBackfill{Ch: "conv:tab-a", Events: events}, now); err != nil {
			t.Fatal(err)
		}
	}
	fill(page(nil, "c:t9#0", "c:t9#0")) // an older agent's page moves nothing
	if pending()["tab-a"] != "" {
		t.Fatal("an unmarked page moved the cursor")
	}
	fill(page(at(""), "c:t8#0", "c:t8#0", "c:t9#0"))
	if pending()["tab-a"] != "c:t8#0" {
		t.Fatalf("the walk did not move: %v", pending())
	}
	fill(page(at(""), "c:t9#0", "c:t9#0"))  // the newest page again
	fill(page(at("c:t2#0"), nil, "c:t1#0")) // a page past where the walk stands
	if pending()["tab-a"] != "c:t8#0" {
		t.Fatalf("a page from elsewhere moved the cursor: %v", pending())
	}
	fill(page(at("c:t8#0"), nil, "c:t7#0"))
	got := pending()
	if _, open := got["tab-a"]; open || got["tab-b"] != "" {
		t.Fatalf("the chat is not held whole: %v", got)
	}
	st, _ := Start(ctx, db, "m-1")
	if st.Known["tab-a"] != 3 || !st.Enabled {
		t.Errorf("start %+v", st)
	}
}

func TestAFrameForAChannelThatIsNotAChatIsRefused(t *testing.T) {
	ctx, db := context.Background(), openDB(t)
	for _, ch := range []string{"machine", "conv:../x", "conv:"} {
		if _, err := ApplyFrame(ctx, db, mine, live(ch, 1), now); err != ErrBadFrame {
			t.Errorf("%s: %v", ch, err)
		}
	}
	if _, err := ApplyFrame(ctx, db, mine, hubproto.HistoryEvents{Ch: "convs", FromSeq: 1}, now); err != ErrBadFrame {
		t.Errorf("no epoch: %v", err)
	}
}

func TestListVersionMovesWithEveryChangeOfTheList(t *testing.T) {
	ctx, db := context.Background(), openDB(t)
	version := func(user string) string {
		t.Helper()
		v, err := ListVersion(ctx, db, user)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	empty := version("u-sergey")
	seen := map[string]bool{empty: true}
	// Every step happens within the same millisecond: the time alone would not tell them apart.
	steps := []hubproto.HistoryEvents{
		live("convs", 1, upsert("tab-a", 1, 10)),
		live("convs", 2, upsert("tab-b", 1, 20)),
		live("convs", 3, j(map[string]any{"type": "conversation_state", "id": "tab-b", "rev": 2, "state": map[string]any{"k": "running"}})),
		live("convs", 4, j(map[string]any{"type": "conversation_removed", "id": "tab-a"})),
		live("convs", 5, upsert("tab-a", 2, 10)),
	}
	for i, f := range steps {
		if _, err := ApplyFrame(ctx, db, mine, f, now); err != nil {
			t.Fatal(err)
		}
		v := version("u-sergey")
		if seen[v] {
			t.Errorf("step %d: the version %s was seen before", i, v)
		}
		seen[v] = true
		if again := version("u-sergey"); again != v {
			t.Errorf("step %d: the version moved without a change: %s, %s", i, v, again)
		}
	}
	if got := version("u-roma"); got != empty {
		t.Errorf("another person's version moved: %s", got)
	}
}
