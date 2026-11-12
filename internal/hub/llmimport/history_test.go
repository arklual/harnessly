package llmimport

import (
	"context"
	"testing"
)

// the chats of llm move into the hub chat with the same id, once; what the hub has is kept; a chat with no
// hub chat or of another person is left and counted; a dry run writes nothing.

type fakeHistory struct {
	chats    []ChatRef
	turns    map[string][]Turn
	entries  map[string][]Entry
	sidebars []Sidebar
	prompts  []Prompt
	docs     []Doc
}

func (f fakeHistory) Chats(context.Context) ([]ChatRef, error) { return f.chats, nil }
func (f fakeHistory) Turns(_ context.Context, c string, fn func(Turn) error) error {
	for _, t := range f.turns[c] {
		if err := fn(t); err != nil {
			return err
		}
	}
	return nil
}
func (f fakeHistory) Entries(_ context.Context, c string, fn func(Entry) error) error {
	for _, e := range f.entries[c] {
		if err := fn(e); err != nil {
			return err
		}
	}
	return nil
}
func (f fakeHistory) Sidebars(context.Context) ([]Sidebar, error) { return f.sidebars, nil }
func (f fakeHistory) Prompts(context.Context) ([]Prompt, error)   { return f.prompts, nil }
func (f fakeHistory) Docs(context.Context) ([]Doc, error)         { return f.docs, nil }

func TestHistoryMovesOnce(t *testing.T) {
	ctx := context.Background()
	db := openHub(t)
	q := db.S().Q()
	for _, s := range []string{
		`INSERT INTO users (id, email, created_at) VALUES ('u-ann', 'ann@example.com', 1), ('u-bob', 'bob@example.com', 1)`,
		`INSERT INTO machines (id, name, hostname, os, arch, unix_user, profile, via, key_pub, enc_pub, agent_version, created_at, info)
			VALUES ('m-ann', 'ann-vm', 'h', '', '', '', '', '', '', '', '', 1, '')`,
		`INSERT INTO history_conversations (machine_id, conv_id, owner_id, rev, seq, history_done, updated_at) VALUES
			('m-ann', 'c1', 'u-ann', 1, 1, 0, 1), ('m-ann', 'c2', 'u-bob', 1, 1, 0, 1), ('m-ann', 'c4', 'u-ann', 1, 1, 1, 1)`,
		`INSERT INTO history_entries (machine_id, conv_id, entry_id, turn_id, at_ms, rev, kind, ord, body) VALUES ('m-ann', 'c1', 'e1', 't1', 10, 1, 'user', 1, 'hub body')`,
		`INSERT INTO doc_meta (slug, owner_email, owner_name, title, team_level, created_at, updated_at) VALUES ('doc-have', 'ann@example.com', '', 'kept', 'none', 1, 1)`,
	} {
		if _, err := q.Exec(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	turn := "t1"
	src := fakeHistory{
		chats: []ChatRef{
			{ID: "c1", Owner: "ann@example.com", Machine: "dev"},
			{ID: "c2", Owner: "ann@example.com", Machine: "dev"}, // the hub's c2 is bob's
			{ID: "c3", Owner: "ann@example.com", Machine: "dev"}, // no hub chat
			{ID: "c4", Owner: "ann@example.com", Machine: "dev", Deleted: true},
		},
		turns: map[string][]Turn{"c1": {{ID: "t1", Started: 9, Status: "done", Body: `{"a":1}`}}},
		entries: map[string][]Entry{
			"c1": {{ID: "e1", Turn: &turn, At: 10, Rev: 1, Kind: "user", Ord: 1, Body: "llm body"}, {ID: "e2", Turn: &turn, At: 11, Rev: 1, Kind: "assistant", Ord: 2, Body: "second"}},
			"c2": {{ID: "x", At: 1, Kind: "user", Body: "not moved"}},
			"c4": {{ID: "d1", At: 5, Kind: "user", Body: "deleted chat line"}},
		},
		sidebars: []Sidebar{{Owner: "ann@example.com", Doc: `{"x":1}`, Rev: 2, At: 7}, {Owner: "eve@example.com", Doc: `{}`}},
		prompts: []Prompt{
			{ID: "p1", Owner: "ann@example.com", Machine: "dev", Tab: "tab", Text: "hello", State: "queued", Send: `{"k":1}`, Created: 3, Changed: 4},
			{ID: "p2", Owner: "ann@example.com", Machine: "other", Text: "lost"},
		},
		docs: []Doc{
			{Slug: "doc-have", Owner: "ann@example.com", Title: "llm title", Runtime: "dev:ann@example.com"},
			{Slug: "doc-new", Kind: "board", Owner: "ann@example.com", OwnerName: "Ann", Title: "Board", Level: "comment", Runtime: "dev:ann@example.com", Created: 5, Updated: 6},
			{Slug: "doc-orphan", Owner: "zed@example.com", Title: "No machine", Runtime: "gone:zed@example.com"},
		},
	}
	o := opts()
	o.History = src

	dry := o
	dry.DryRun = true
	rep, err := Run(ctx, db, Data{}, dry)
	if err != nil {
		t.Fatal(err)
	}
	h := rep.History
	if h.Chats.Read != 4 || h.Chats.Moved != 2 || h.Entries.Added != 2 || h.Entries.Kept != 1 || h.Turns.Added != 1 {
		t.Fatalf("dry report: %+v", h)
	}
	if n := scalar(t, db, `SELECT count(*) FROM history_entries`); n != 1 {
		t.Fatalf("a dry run wrote lines: %d", n)
	}
	if n := scalar(t, db, `SELECT count(*) FROM offline_prompts`) + scalar(t, db, `SELECT count(*) FROM history_sidebars`); n != 0 {
		t.Fatalf("a dry run wrote prompts or sidebars: %d", n)
	}

	rep, err = Run(ctx, db, Data{}, o)
	if err != nil {
		t.Fatal(err)
	}
	h = rep.History
	if h.Left["no chat with this id in the hub"] != 1 || h.Left["the hub's chat is another person's"] != 1 {
		t.Fatalf("left: %+v", h.Left)
	}
	if h.PerMachine["m-ann"] != 2 || h.Chats.Full != 0 {
		t.Fatalf("per machine: %+v", h)
	}
	if n := scalar(t, db, `SELECT count(*) FROM history_entries WHERE conv_id = 'c1' AND machine_id = 'm-ann'`); n != 2 {
		t.Fatalf("lines of c1: %d", n)
	}
	if n := scalar(t, db, `SELECT count(*) FROM history_entries WHERE entry_id = 'e1' AND body = 'hub body'`); n != 1 {
		t.Fatal("the hub's own line was overwritten")
	}
	if n := scalar(t, db, `SELECT count(*) FROM history_entries WHERE conv_id = 'c2'`); n != 0 {
		t.Fatal("a line of another person's chat moved")
	}
	if n := scalar(t, db, `SELECT count(*) FROM history_entries WHERE conv_id = 'c4'`); n != 1 {
		t.Fatal("a deleted chat's line did not move")
	}
	if h.Sidebars.Added != 1 || h.Sidebars.NoPerson != 1 || h.Prompts.Added != 1 || h.Prompts.NoMachine != 1 {
		t.Fatalf("sidebars/prompts: %+v %+v", h.Sidebars, h.Prompts)
	}
	if n := scalar(t, db, `SELECT count(*) FROM offline_prompts WHERE id = 'p1' AND machine_id = 'm-ann' AND user_id = 'u-ann'`); n != 1 {
		t.Fatal("prompt p1")
	}
	if h.Docs.Added != 2 || h.Docs.Kept != 1 || h.Docs.NoMachine != 1 {
		t.Fatalf("docs: %+v", h.Docs)
	}
	if n := scalar(t, db, `SELECT count(*) FROM doc_meta WHERE slug = 'doc-new' AND machine_id = 'm-ann' AND team_level = 'comment'`); n != 1 {
		t.Fatal("doc-new")
	}
	if n := scalar(t, db, `SELECT count(*) FROM doc_meta WHERE slug = 'doc-have' AND title = 'kept'`); n != 1 {
		t.Fatal("the hub's own document was overwritten")
	}

	again, err := Run(ctx, db, Data{}, o)
	if err != nil {
		t.Fatal(err)
	}
	a := again.History
	if a.Entries.Added != 0 || a.Turns.Added != 0 || a.Sidebars.Added != 0 || a.Prompts.Added != 0 || a.Docs.Added != 0 || a.Chats.Full != 2 {
		t.Fatalf("a second run changed something: %+v", a)
	}
}
