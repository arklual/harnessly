package history

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

const day = 24 * time.Hour

// fill gives a chat turns and entries, and a cursor of its channel.
func fill(t *testing.T, db *store.DB, o Owner, conv string, turns, entries int) {
	t.Helper()
	ctx := context.Background()
	for i := range turns {
		if _, err := db.Exec(ctx, `INSERT INTO history_turns (machine_id, conv_id, turn_id, started_ms, status, body) VALUES (?, ?, ?, ?, 'done', '{}')`,
			o.MachineID, conv, fmt.Sprintf("t-%d", i), i); err != nil {
			t.Fatal(err)
		}
	}
	for i := range entries {
		if _, err := db.Exec(ctx, `INSERT INTO history_entries (machine_id, conv_id, entry_id, at_ms, kind, ord, body) VALUES (?, ?, ?, ?, 'user', ?, '{}')`,
			o.MachineID, conv, fmt.Sprintf("e-%d", i), i, i); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(ctx, `INSERT INTO history_cursors (machine_id, ch, epoch, seq, updated_at) VALUES (?, ?, 'ep-1', 1, 0)`,
		o.MachineID, "conv:"+conv); err != nil {
		t.Fatal(err)
	}
}

// held is how many rows of each history table a chat has: the chat itself, turns, entries, its channel cursor.
func held(t *testing.T, db *store.DB, o Owner, conv string) [4]int {
	t.Helper()
	var out [4]int
	for i, q := range []string{
		`SELECT COUNT(*) FROM history_conversations WHERE machine_id = ? AND conv_id = ?`,
		`SELECT COUNT(*) FROM history_turns WHERE machine_id = ? AND conv_id = ?`,
		`SELECT COUNT(*) FROM history_entries WHERE machine_id = ? AND conv_id = ?`,
		`SELECT COUNT(*) FROM history_cursors WHERE machine_id = ? AND ch = 'conv:' || ?`,
	} {
		if err := db.QueryRow(context.Background(), q, o.MachineID, conv).Scan(&out[i]); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func removed(id string) map[string]any {
	return map[string]any{"type": "conversation_removed", "id": id}
}

// sweeper is a sweeper without pauses whose clock the test moves.
func sweeper(db *store.DB, clock *time.Time) *Sweeper {
	s := NewSweeper(SweepDeps{DB: db, Keep: 30 * day, Now: func() time.Time { return *clock }})
	s.pause = 0
	return s
}

func pass(t *testing.T, s *Sweeper) SweepResult {
	t.Helper()
	res, err := s.Pass(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestAChatDeletedOnItsMachineLeavesTheHubAfterItsTerm(t *testing.T) {
	ctx, db := context.Background(), openDB(t)
	if _, err := ApplyFrame(ctx, db, mine, live("convs", 1, upsert("tab-gone", 1, 10), upsert("tab-kept", 1, 20), upsert("tab-back", 1, 30)), now); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyFrame(ctx, db, second, live("convs", 1, upsert("tab-gone", 1, 10)), now); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		o    Owner
		conv string
	}{{mine, "tab-gone"}, {mine, "tab-kept"}, {mine, "tab-back"}, {second, "tab-gone"}} {
		fill(t, db, c.o, c.conv, 3, 7)
	}
	// The machine deletes two chats and sends one of them again; the same chat on the other machine is deleted later.
	if _, err := ApplyFrame(ctx, db, mine, live("convs", 4, j(removed("tab-gone")), j(removed("tab-back")), upsert("tab-back", 2, 30)), now); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyFrame(ctx, db, second, live("convs", 2, j(removed("tab-gone"))), now.Add(20*day)); err != nil {
		t.Fatal(err)
	}
	clock := now.Add(29 * day)
	s := sweeper(db, &clock)
	if res := pass(t, s); res.Chats != 0 || res.Rows != 0 {
		t.Fatalf("swept before the term: %+v", res)
	}
	if got := held(t, db, mine, "tab-gone"); got != [4]int{1, 3, 7, 1} {
		t.Fatalf("before the term: %v", got)
	}
	clock = now.Add(31 * day)
	if res := pass(t, s); res.Chats != 1 || res.Rows != 10 {
		t.Fatalf("after the term: %+v", res)
	}
	if got := held(t, db, mine, "tab-gone"); got != [4]int{} {
		t.Errorf("the deleted chat is still in the hub: %v", got)
	}
	for _, c := range []struct {
		o    Owner
		conv string
		why  string
	}{{mine, "tab-kept", "a chat nobody deleted"}, {mine, "tab-back", "a chat its machine sent again"}, {second, "tab-gone", "a chat deleted less than the term ago"}} {
		if got := held(t, db, c.o, c.conv); got != [4]int{1, 3, 7, 1} {
			t.Errorf("%s lost rows: %v", c.why, got)
		}
	}
	// The cursor of the chat list is not the chat's.
	var lists int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM history_cursors WHERE ch = 'convs'`).Scan(&lists); err != nil || lists != 2 {
		t.Errorf("chat list cursors: %d %v", lists, err)
	}
	if res := pass(t, s); res.Chats != 0 || res.Rows != 0 {
		t.Errorf("a second pass found work: %+v", res)
	}
}

func TestASweepPassRemovesNoMoreThanItsShare(t *testing.T) {
	ctx, db := context.Background(), openDB(t)
	for i, id := range []string{"tab-a", "tab-b", "tab-c"} {
		if _, err := ApplyFrame(ctx, db, mine, live("convs", int64(2*i+1), upsert(id, 1, 10), j(removed(id))), now.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
		fill(t, db, mine, id, 4, 6)
	}
	clock := now.Add(31 * day)
	s := sweeper(db, &clock)
	s.chunk, s.passRows = 3, 14
	// 14 rows: the oldest chat whole (10) and four entries of the next.
	if res := pass(t, s); res.Chats != 1 || res.Rows != 14 {
		t.Fatalf("first pass: %+v", res)
	}
	if got := held(t, db, mine, "tab-b"); got != [4]int{1, 4, 2, 1} {
		t.Fatalf("the chat the pass stopped in: %v", got)
	}
	s.passRows, s.passChats = 1000, 1
	if res := pass(t, s); res.Chats != 1 || res.Rows != 6 {
		t.Fatalf("second pass, one chat: %+v", res)
	}
	if res := pass(t, s); res.Chats != 1 || res.Rows != 10 {
		t.Fatalf("third pass: %+v", res)
	}
	var left int
	if err := db.QueryRow(ctx, `SELECT (SELECT COUNT(*) FROM history_conversations) + (SELECT COUNT(*) FROM history_turns) + (SELECT COUNT(*) FROM history_entries)`).Scan(&left); err != nil || left != 0 {
		t.Errorf("left: %d %v", left, err)
	}
}

func TestTwoReplicasDoNotSweepTheSameChats(t *testing.T) {
	ctx, db := context.Background(), openDB(t)
	const chats = 12
	for i := range chats {
		id := fmt.Sprintf("tab-%d", i)
		if _, err := ApplyFrame(ctx, db, mine, live("convs", int64(2*i+1), upsert(id, 1, 10), j(removed(id))), now); err != nil {
			t.Fatal(err)
		}
		fill(t, db, mine, id, 2, 5)
	}
	clock := now.Add(31 * day)
	a, b := sweeper(db, &clock), sweeper(db, &clock)
	a.chunk, b.chunk = 2, 2
	var wg sync.WaitGroup
	results := make([]SweepResult, 2)
	errs := make([]error, 2)
	for i, s := range []*Sweeper{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = s.Pass(ctx)
		}()
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatal(errs)
	}
	if results[0].Held == results[1].Held {
		t.Fatalf("exactly one replica does the pass: %+v", results)
	}
	if got := results[0].Chats + results[1].Chats; got != chats {
		t.Errorf("chats removed by both: %d, want %d", got, chats)
	}
	if got := results[0].Rows + results[1].Rows; got != chats*7 {
		t.Errorf("rows removed by both: %d, want %d", got, chats*7)
	}
	// The replica that did not sweep takes the job only when the other one stopped coming.
	idle := a
	if results[1].Held {
		idle = b
	}
	clock = clock.Add(idle.every)
	if res := pass(t, idle); !res.Held {
		t.Errorf("the job was taken from a live holder: %+v", res)
	}
	clock = clock.Add(idle.every)
	if res := pass(t, idle); res.Held {
		t.Errorf("the job of a holder that is gone was not taken: %+v", res)
	}
}

func TestWithoutATermNothingIsSwept(t *testing.T) {
	ctx, db := context.Background(), openDB(t)
	if _, err := ApplyFrame(ctx, db, mine, live("convs", 1, upsert("tab-a", 1, 10), j(removed("tab-a"))), now); err != nil {
		t.Fatal(err)
	}
	fill(t, db, mine, "tab-a", 1, 1)
	s := NewSweeper(SweepDeps{DB: db, Keep: 0, Now: func() time.Time { return now.Add(400 * day) }})
	if res := pass(t, s); res.Chats != 0 || res.Rows != 0 || res.Held {
		t.Fatalf("%+v", res)
	}
	if got := held(t, db, mine, "tab-a"); got != [4]int{1, 1, 1, 1} {
		t.Errorf("%v", got)
	}
}

// A person's review drafts of a chat leave with the last copy of the chat, and not before.
func TestTheReviewDraftsOfASweptChatGoWithIt(t *testing.T) {
	ctx, db := context.Background(), openDB(t)
	if _, err := ApplyFrame(ctx, db, mine, live("convs", 1, upsert("tab-gone", 1, 10), upsert("tab-kept", 1, 20)), now); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyFrame(ctx, db, second, live("convs", 1, upsert("tab-gone", 1, 10)), now); err != nil {
		t.Fatal(err)
	}
	for _, conv := range []string{"tab-gone", "tab-kept"} {
		if _, err := PutReviewNote(ctx, db, mine.UserID, conv, note("n1", "a.go", 3), 100); err != nil {
			t.Fatal(err)
		}
	}
	drafts := func(conv string) int {
		list, err := ReviewNotes(ctx, db, mine.UserID, conv)
		if err != nil {
			t.Fatal(err)
		}
		return len(list)
	}
	if _, err := ApplyFrame(ctx, db, mine, live("convs", 3, j(removed("tab-gone"))), now); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyFrame(ctx, db, second, live("convs", 2, j(removed("tab-gone"))), now.Add(20*day)); err != nil {
		t.Fatal(err)
	}
	clock := now.Add(31 * day)
	s := sweeper(db, &clock)
	if res := pass(t, s); res.Chats != 1 {
		t.Fatalf("first pass: %+v", res)
	}
	if drafts("tab-gone") != 1 {
		t.Fatal("the drafts left while another machine still holds the chat")
	}
	clock = now.Add(51 * day)
	if res := pass(t, s); res.Chats != 1 {
		t.Fatalf("second pass: %+v", res)
	}
	if drafts("tab-gone") != 0 {
		t.Error("the drafts of a chat gone from the hub are kept")
	}
	if drafts("tab-kept") != 1 {
		t.Error("the drafts of a chat nobody deleted are gone")
	}
}
