package history

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// a rewind sends the chat's new feed whole; the turns it left behind leave the feed and open on their page.
func TestARewindLeavesItsTurnsOutOfTheFeedAndOnItsOwnPage(t *testing.T) {
	ctx, db := context.Background(), openDB(t)
	_ = ApplyBackfill(ctx, db, mine, hubproto.HistoryBackfill{Ch: "convs", Events: []json.RawMessage{upsert("tab-a", 1, 1)}}, now)
	var turns, entries []any
	for i := range 5 {
		id := fmt.Sprintf("c:t%d#0", i)
		turns = append(turns, turn(id, int64(1000+i)))
		entries = append(entries, entry(id, id, int64(1000+i)), entry("a-"+id, id, int64(1000+i)))
	}
	if err := ApplyBackfill(ctx, db, mine, hubproto.HistoryBackfill{Ch: "conv:tab-a", Events: []json.RawMessage{
		j(map[string]any{"type": "feed_reset", "turns": turns, "entries": entries, "beforeTurnId": nil}),
	}}, now); err != nil {
		t.Fatal(err)
	}
	// A page of the machine (no seq) leaves nothing out.
	if err := ApplyBackfill(ctx, db, mine, hubproto.HistoryBackfill{Ch: "conv:tab-a", Events: []json.RawMessage{
		j(map[string]any{"type": "feed_reset", "turns": turns[:2], "entries": entries[:4], "beforeTurnId": nil}),
	}}, now); err != nil {
		t.Fatal(err)
	}
	if page, _ := Read(ctx, db, "u-sergey", "tab-a", []string{"m-1"}, "", 33); len(page.Turns) != 5 {
		t.Fatalf("a page left turns out: %v", ids(t, page.Turns))
	}
	// The rewind keeps two turns.
	rewound := conv("tab-a", 2, 1)
	rewound["rewinds"] = []any{map[string]any{"at": 2000, "afterTurnId": "c:t1#0", "turns": 3, "files": 0}}
	if _, err := ApplyFrame(ctx, db, mine, live("conv:tab-a", 1,
		j(map[string]any{"type": "feed_reset", "turns": turns[:2], "entries": entries[:4], "beforeTurnId": nil})), now); err != nil {
		t.Fatal(err)
	}
	_ = ApplyBackfill(ctx, db, mine, hubproto.HistoryBackfill{Ch: "convs", Events: []json.RawMessage{
		j(map[string]any{"type": "conversation_upsert", "conv": rewound})}}, now)
	page, err := Read(ctx, db, "u-sergey", "tab-a", []string{"m-1"}, "", 33)
	if got := ids(t, page.Turns); err != nil || strings.Join(got, ",") != "c:t0#0,c:t1#0" {
		t.Fatalf("the feed after the rewind: %v %v", got, err)
	}
	left, err := Rewound(ctx, db, "u-sergey", "tab-a", 0, []string{"m-1"})
	if got := ids(t, left.Turns); err != nil || strings.Join(got, ",") != "c:t2#0,c:t3#0,c:t4#0" || len(left.Entries) != 6 {
		t.Fatalf("the rewind's page: %v %d %v", got, len(left.Entries), err)
	}
	if _, err := Rewound(ctx, db, "u-sergey", "tab-a", 1, []string{"m-1"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("no such rewind: %v", err)
	}
	if _, err := Rewound(ctx, db, "u-roma", "tab-a", 0, []string{"m-1"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("another person reads the rewind: %v", err)
	}
	// Undone: the feed has the turns again.
	if _, err := ApplyFrame(ctx, db, mine, live("conv:tab-a", 2,
		j(map[string]any{"type": "feed_reset", "turns": turns, "entries": entries, "beforeTurnId": nil})), now); err != nil {
		t.Fatal(err)
	}
	if page, _ := Read(ctx, db, "u-sergey", "tab-a", []string{"m-1"}, "", 33); len(page.Turns) != 5 {
		t.Errorf("the undone rewind: %v", ids(t, page.Turns))
	}
}

func TestSearchFindsThePersonsMessagesByEveryWord(t *testing.T) {
	ctx, db := context.Background(), openDB(t)
	_ = ApplyBackfill(ctx, db, mine, hubproto.HistoryBackfill{Ch: "convs", Events: []json.RawMessage{upsert("tab-a", 1, 1)}}, now)
	_ = ApplyBackfill(ctx, db, other, hubproto.HistoryBackfill{Ch: "convs", Events: []json.RawMessage{upsert("tab-r", 1, 1)}}, now)
	say := func(id, turnID string, at int64, md string) map[string]any {
		return map[string]any{"id": id, "turnId": turnID, "at": at, "rev": 1, "t": "say", "md": md}
	}
	user := func(id, turnID string, at int64, text string) map[string]any {
		return map[string]any{"id": id, "turnId": turnID, "at": at, "rev": 1, "t": "user", "text": text}
	}
	_ = ApplyBackfill(ctx, db, mine, hubproto.HistoryBackfill{Ch: "conv:tab-a", Events: []json.RawMessage{
		j(map[string]any{"type": "feed_reset", "turns": []any{turn("c:t1#0", 1000), turn("c:t2#0", 2000)}, "entries": []any{
			user("u1", "c:t1#0", 1000, "Почини Ёлку в деплое"),
			say("a-c:t1#0", "c:t1#0", 1001, "Починил ёлку, деплой зелёный"),
			user("u2", "c:t2#0", 2000, "что с ёлкой"),
		}, "beforeTurnId": nil}),
	}}, now)
	_ = ApplyBackfill(ctx, db, other, hubproto.HistoryBackfill{Ch: "conv:tab-r", Events: []json.RawMessage{
		j(map[string]any{"type": "feed_reset", "turns": []any{turn("c:r1#0", 1500)}, "entries": []any{
			user("r1", "c:r1#0", 1500, "ёлка деплой"),
		}, "beforeTurnId": nil}),
	}}, now)
	hits, err := Search(ctx, db, "u-sergey", "елк депл", []string{"m-1"}, 10)
	if err != nil || len(hits) != 2 || hits[0].EntryID != "a-c:t1#0" || hits[0].Role != "agent" || hits[1].EntryID != "u1" ||
		hits[1].Role != "user" || hits[1].TurnID == nil || *hits[1].TurnID != "c:t1#0" || hits[1].Snippet == "" {
		t.Fatalf("hits %+v %v", hits, err)
	}
	if hits, _ := Search(ctx, db, "u-sergey", "ёлк", []string{"m-1"}, 1); len(hits) != 1 || hits[0].EntryID != "u2" {
		t.Errorf("the newest first, up to the limit: %+v", hits)
	}
	if hits, _ := Search(ctx, db, "u-sergey", "елк", []string{"m-2"}, 10); len(hits) != 0 {
		t.Errorf("a machine not asked for: %+v", hits)
	}
	if hits, _ := Search(ctx, db, "u-sergey", "е", []string{"m-1"}, 10); len(hits) != 0 {
		t.Errorf("a one-letter query: %+v", hits)
	}
}

func TestTheSidebarIsWrittenOnItsRevisionOnly(t *testing.T) {
	ctx, db := context.Background(), openDB(t)
	doc := json.RawMessage(`{"projects":[{"id":"p1","name":"Работа","createdAt":1,"members":[{"machineId":"m-1","projectId":"x"}]}],"favorites":{"groups":[],"loose":["tab-a"]},"merged":["m-1"]}`)
	if _, ok := ParseSidebar(doc); !ok {
		t.Fatal("a good document refused")
	}
	for _, bad := range []string{`{}`, `{"projects":[],"favorites":{"groups":[],"loose":[]}}`,
		`{"projects":[{"id":"p 1","name":"x","createdAt":1,"members":[]}],"favorites":{"groups":[],"loose":[]},"merged":[]}`,
		`{"projects":[],"favorites":{"groups":[{"id":"g","name":" ","chats":[]}],"loose":[]},"merged":[]}`} {
		if _, ok := ParseSidebar(json.RawMessage(bad)); ok {
			t.Errorf("a bad document taken: %s", bad)
		}
	}
	if rev, got, err := Sidebar(ctx, db, "u-sergey"); rev != 0 || got != nil || err != nil {
		t.Fatalf("none yet: %d %s %v", rev, got, err)
	}
	if rev, err := WriteSidebar(ctx, db, "u-sergey", 0, doc, 1); rev != 1 || err != nil {
		t.Fatalf("first write: %d %v", rev, err)
	}
	if _, err := WriteSidebar(ctx, db, "u-sergey", 0, doc, 2); !errors.Is(err, ErrSidebarChanged) {
		t.Errorf("a second first write: %v", err)
	}
	if rev, err := WriteSidebar(ctx, db, "u-sergey", 1, doc, 3); rev != 2 || err != nil {
		t.Errorf("on rev 1: %d %v", rev, err)
	}
	if _, err := WriteSidebar(ctx, db, "u-sergey", 1, doc, 4); !errors.Is(err, ErrSidebarChanged) {
		t.Errorf("over a newer one: %v", err)
	}
	if rev, got, _ := Sidebar(ctx, db, "u-sergey"); rev != 2 || string(got) != string(doc) {
		t.Errorf("read: %d %s", rev, got)
	}
	if rev, _, _ := Sidebar(ctx, db, "u-roma"); rev != 0 {
		t.Errorf("another person's: %d", rev)
	}
}
