package llmimport

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// the switches of the people the move is for, and the access log of the documents, move from llm once; the
// hub's own rows are kept; a dry run writes nothing; the checks say what the hub lacks.

func openHub(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(context.Background(), "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func fixture() Data {
	return Data{
		Switches: []Switch{
			{Feature: "hub-access", Person: "Ann@example.com", Machine: "*", Enabled: true, Note: "pilot", UpdatedAt: 5},
			{Feature: "hub-access", Person: "ann@example.com", Machine: "vm-1", Enabled: true, UpdatedAt: 6},
			{Feature: "hub-access", Person: "ann@example.com", Machine: "vm-2", Enabled: true, UpdatedAt: 6},
			{Feature: "hub-access", Person: "bob@example.com", Machine: "*", Enabled: true},
			{Feature: "hub-access", Person: "*", Machine: "*", Enabled: true},
			{Feature: "runtime-go-core", Person: "ann@example.com", Machine: "*", Enabled: true},
		},
		Log: []LogRow{
			{ID: 1, Kind: "artifact", Resource: "doc-a", Actor: "ann@example.com", Action: "grant", Subj: "bob@example.com", Details: `{"role":"view"}`, At: 7},
			{ID: 2, Kind: "artifact", Resource: "doc-gone", Actor: "ann@example.com", Action: "grant", At: 8},
		},
		People:       []string{"ann@example.com", "eve@example.com"},
		DocSlugs:     []string{"doc-a", "doc-b"},
		LinkHashes:   []string{"h1"},
		Conversation: []string{"c1"},
	}
}

func seed(t *testing.T, db *store.DB) {
	t.Helper()
	q := db.S().Q()
	for _, s := range []string{
		`INSERT INTO resource_access (kind, id, owner_email, created_at, updated_at) VALUES ('doc', 'doc-a', 'ann@example.com', 1, 1)`,
		`INSERT INTO rollout_switches (feature, person_email, machine_id, enabled, updated_at) VALUES ('hub-access', 'ann@example.com', '*', 0, 1)`,
	} {
		if _, err := q.Exec(context.Background(), s); err != nil {
			t.Fatal(err)
		}
	}
}

func opts() Options {
	return Options{Persons: []string{"ann@example.com"}, Machines: map[string]string{"vm-1": "m-hub-1"}, Now: func() time.Time { return time.UnixMilli(100) }}
}

func TestRunMovesOnceAndKeepsTheHubs(t *testing.T) {
	ctx := context.Background()
	db := openHub(t)
	seed(t, db)

	dry := opts()
	dry.DryRun = true
	rep, err := Run(ctx, db, fixture(), dry)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.DryRun || rep.Switches.Added != 1 || rep.Switches.Kept != 1 || rep.Log.Added != 1 {
		t.Fatalf("dry run report: %+v", rep)
	}
	if n := scalar(t, db, `SELECT count(*) FROM rollout_switches`); n != 1 {
		t.Fatalf("a dry run wrote switches: %d", n)
	}
	if n := scalar(t, db, `SELECT count(*) FROM resource_access_log`); n != 0 {
		t.Fatalf("a dry run wrote the log: %d", n)
	}

	rep, err = Run(ctx, db, fixture(), opts())
	if err != nil {
		t.Fatal(err)
	}
	// ann's «anyone machine» row exists in the hub (off): kept. vm-1 is named: added. vm-2 is not named: left.
	// bob and the row for anyone: not asked for. runtime-go-core: the gateway's own.
	if rep.Switches.Read != 6 || rep.Switches.Added != 1 || rep.Switches.Kept != 1 || rep.Switches.Left != 4 {
		t.Fatalf("report: %+v", rep.Switches)
	}
	if rep.Left["for a machine with no hub machine named: vm-2"] != 1 || rep.Left["a switch of the gateway itself"] != 1 {
		t.Fatalf("reasons: %v", rep.Left)
	}
	if on := scalar(t, db, `SELECT enabled FROM rollout_switches WHERE feature = 'hub-access' AND person_email = 'ann@example.com' AND machine_id = '*'`); on != 0 {
		t.Fatal("the hub's own row was overwritten")
	}
	if n := scalar(t, db, `SELECT count(*) FROM rollout_switches WHERE machine_id = 'm-hub-1' AND updated_by = 'llm-import'`); n != 1 {
		t.Fatalf("the named machine's row: %d", n)
	}
	if rep.Log.Added != 1 || rep.Log.NoResource != 1 {
		t.Fatalf("log: %+v", rep.Log)
	}

	again, err := Run(ctx, db, fixture(), opts())
	if err != nil {
		t.Fatal(err)
	}
	if again.Switches.Added != 0 || again.Log.Added != 0 || again.Log.Kept != 1 {
		t.Fatalf("a second run changed something: %+v %+v", again.Switches, again.Log)
	}
	if n := scalar(t, db, `SELECT count(*) FROM llm_import_runs WHERE outcome = 'ok'`); n != 3 {
		t.Fatalf("runs kept: %d", n)
	}
}

func TestChecksNameWhatTheHubLacks(t *testing.T) {
	ctx := context.Background()
	db := openHub(t)
	seed(t, db)
	if _, err := db.S().Q().Exec(ctx, `INSERT INTO doc_meta (slug, owner_email, created_at, updated_at) VALUES ('doc-a', 'ann@example.com', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	rep, err := Run(ctx, db, fixture(), opts())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Check{}
	for _, c := range rep.Checks {
		got[c.Name] = c
	}
	if c := got["documents (slug)"]; c.LLM != 2 || c.Hub != 1 || c.Missing != 1 {
		t.Fatalf("documents: %+v", c)
	}
	if c := got["conversations (not deleted)"]; c.Missing != 1 {
		t.Fatalf("conversations: %+v", c)
	}
	if !strings.Contains(rep.Summary(), "missing in the hub 1") {
		t.Fatalf("summary: %s", rep.Summary())
	}
}

func TestRunNeedsToKnowForWhom(t *testing.T) {
	db := openHub(t)
	if _, err := Run(context.Background(), db, fixture(), Options{}); err == nil {
		t.Fatal("a move for nobody in particular must be refused")
	}
}

func scalar(t *testing.T, db *store.DB, q string) int {
	t.Helper()
	rows, err := db.S().Q().Query(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var n int
	if rows.Next() {
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
	}
	return n
}

// the dictation recordings of the people the move is for move once; the hub's own take is kept, a person
// the hub does not know is counted, a recording too big goes without its audio.
func TestVoiceTakesMoveOnce(t *testing.T) {
	ctx := context.Background()
	db := openHub(t)
	q := db.S().Q()
	for _, s := range []string{
		`INSERT INTO users (id, email, created_at) VALUES ('u-ann', 'ann@example.com', 1)`,
		`INSERT INTO voice_takes (user_id, id, machine_id, at, ms, mime, state, text, audio, created_at, changed_at) VALUES ('u-ann', 't1', 'local', 1, 1, 'audio/webm', 'done', 'hub text', NULL, 1, 9)`,
	} {
		if _, err := q.Exec(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := q.Exec(ctx, `INSERT INTO eff_pulse (user_id, week, speed, quality, friction, answered_at) VALUES ('u-ann', '2026-10-05', 1, 1, 1, 9)`); err != nil {
		t.Fatal(err)
	}
	text := "llm text"
	d := Data{Pulse: []Pulse{
		{Owner: "Ann@example.com", Week: "2026-10-05", Speed: 3, Quality: 4, Friction: 4, Answered: 5},
		{Owner: "ann@example.com", Week: "2026-10-12", Speed: 3, Quality: 4, Friction: 2, Answered: 6},
		{Owner: "bob@example.com", Week: "2026-10-12", Speed: 3, Quality: 4, Friction: 2, Answered: 6},
	}, Voice: []Voice{
		{Owner: "Ann@example.com", ID: "t1", Machine: "local", Mime: "audio/webm", State: "done", Text: &text, At: 1, Ms: 5, Changed: 2},
		{Owner: "ann@example.com", ID: "t2", Machine: "dev", Mime: "", State: "done", Text: &text, Audio: []byte("abc"), At: 3, Ms: 7, Changed: 0},
		{Owner: "ann@example.com", ID: "t3", Machine: "dev", State: "done", Audio: make([]byte, voiceAudioMax+1), At: 4},
		{Owner: "bob@example.com", ID: "t4", Machine: "dev", State: "done", At: 5},
		{Owner: "eve@example.com", ID: "t5", Machine: "dev", State: "done", At: 6},
	}}
	o := opts()
	o.Persons = []string{"ann@example.com", "eve@example.com"}

	dry := o
	dry.DryRun = true
	rep, err := Run(ctx, db, d, dry)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Voice.Read != 5 || rep.Voice.Added != 2 || rep.Voice.Kept != 1 || rep.Voice.Left != 1 || rep.Voice.NoPerson != 1 {
		t.Fatalf("dry report: %+v", rep.Voice)
	}
	if rep.Pulse.Read != 3 || rep.Pulse.Added != 1 || rep.Pulse.Kept != 1 || rep.Pulse.Left != 1 {
		t.Fatalf("dry pulse report: %+v", rep.Pulse)
	}
	if n := scalar(t, db, `SELECT count(*) FROM eff_pulse`); n != 1 {
		t.Fatalf("a dry run wrote pulse: %d", n)
	}
	if n := scalar(t, db, `SELECT count(*) FROM voice_takes`); n != 1 {
		t.Fatalf("a dry run wrote takes: %d", n)
	}
	if _, err := Run(ctx, db, d, o); err != nil {
		t.Fatal(err)
	}
	if n := scalar(t, db, `SELECT count(*) FROM voice_takes WHERE user_id = 'u-ann'`); n != 3 {
		t.Fatalf("takes: %d", n)
	}
	if n := scalar(t, db, `SELECT count(*) FROM voice_takes WHERE id = 't1' AND text = 'hub text'`); n != 1 {
		t.Fatal("the hub's own take was overwritten")
	}
	if n := scalar(t, db, `SELECT count(*) FROM voice_takes WHERE id = 't2' AND audio = 'YWJj' AND mime = 'audio/webm' AND changed_at = 3`); n != 1 {
		t.Fatal("t2 not moved as expected")
	}
	if n := scalar(t, db, `SELECT count(*) FROM voice_takes WHERE id = 't3' AND audio IS NULL`); n != 1 {
		t.Fatal("a big recording kept its audio")
	}
	if n := scalar(t, db, `SELECT count(*) FROM eff_pulse`); n != 2 {
		t.Fatalf("pulse: %d", n)
	}
	if n := scalar(t, db, `SELECT speed FROM eff_pulse WHERE week = '2026-10-05'`); n != 1 {
		t.Fatal("the hub's own answer was overwritten")
	}
	again, err := Run(ctx, db, d, o)
	if err != nil {
		t.Fatal(err)
	}
	if again.Pulse.Added != 0 || again.Pulse.Kept != 2 {
		t.Fatalf("a second run changed pulse: %+v", again.Pulse)
	}
	if again.Voice.Added != 0 || again.Voice.Kept != 3 {
		t.Fatalf("a second run changed something: %+v", again.Voice)
	}
}
