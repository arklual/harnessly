// Package llmimport moves what the old gateway's database (`llm`) holds into the hub (HRN-1166,
// docs/hub/llm-import.md): the rollout switches of the people the move is for, the access log of the shared documents,
// and a check of every other table the hub already holds. The source is read in one read-only transaction; the hub is
// written only by rows it lacks, so a second run changes nothing.
package llmimport

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // the llm database is Postgres
)

// Switch is a row of llm's rollout_switches.
type Switch struct {
	Feature, Person, Machine, Note, UpdatedBy string
	Enabled                                   bool
	UpdatedAt                                 int64 // Unix milliseconds
}

// LogRow is a row of llm's resource_access_log.
type LogRow struct {
	ID                                  int64
	Kind, Resource, Actor, Action, Subj string
	Details                             string
	At                                  int64
}

// Voice is a row of llm's ai_voice_takes: a dictation recording of a person.
type Voice struct {
	Owner, ID, Machine, Mime, State string
	Text                            *string
	Audio                           []byte // empty: the audio is gone (llm empties it after 30 days)
	At, Ms, Changed                 int64
}

// Pulse is a row of llm's eff_pulse: a person's weekly answers (week is its Monday, YYYY-MM-DD).
type Pulse struct {
	Owner, Week              string
	Speed, Quality, Friction int
	Answered                 int64
}

// Data is everything the move takes from llm.
type Data struct {
	Switches []Switch
	// Voice is ai_voice_takes: the dictation recordings, kept in the hub's voice_takes (docs/hub/voice-takes.md).
	Voice []Voice
	// Pulse is eff_pulse: the weekly answers of the efficiency screen.
	Pulse []Pulse
	// Log is resource_access_log of the documents (kind «artifact» there, «doc» in the hub).
	Log []LogRow
	// For the checks: the keys the hub must hold too.
	People       []string // lower-case emails of people
	LinkHashes   []string // token_hash of the share links of documents
	DocSlugs     []string // doc_bindings.slug
	Conversation []string // ids of the conversations not deleted
	Resources    map[string]int
}

// Source is llm's Postgres, opened for reading.
type Source struct{ db *sql.DB }

// Open connects to llm by its address (postgres://…).
func Open(ctx context.Context, url string) (*Source, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("the llm database: %w", err)
	}
	return &Source{db: db}, nil
}

func (s *Source) Close() error { return s.db.Close() }

func msOf(col string) string { return "(extract(epoch from " + col + ") * 1000)::bigint" }

// Read takes the data in one read-only transaction.
func (s *Source) Read(ctx context.Context) (Data, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return Data{}, err
	}
	defer func() { _ = tx.Rollback() }()
	d := Data{Resources: map[string]int{}}
	each := func(what, query string, scan func(*sql.Rows) error) error {
		rows, err := tx.QueryContext(ctx, query)
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows); err != nil {
				return fmt.Errorf("%s: %w", what, err)
			}
		}
		return rows.Err()
	}
	if err := each("rollout_switches", `select feature, coalesce(person_email, '*'), coalesce(machine_id, '*'), enabled, coalesce(note, ''),
			coalesce(updated_by, ''), `+msOf("updated_at")+` from rollout_switches order by feature, person_email, machine_id`, func(r *sql.Rows) error {
		var w Switch
		var at sql.NullInt64
		if err := r.Scan(&w.Feature, &w.Person, &w.Machine, &w.Enabled, &w.Note, &w.UpdatedBy, &at); err != nil {
			return err
		}
		w.UpdatedAt = at.Int64
		d.Switches = append(d.Switches, w)
		return nil
	}); err != nil {
		return Data{}, err
	}
	if err := each("resource_access_log", `select id, kind, resource_id, coalesce(actor, ''), action, coalesce(subject, ''), coalesce(details::text, ''), `+msOf("at")+`
		from resource_access_log where kind = 'artifact' order by id`, func(r *sql.Rows) error {
		var l LogRow
		var at sql.NullInt64
		if err := r.Scan(&l.ID, &l.Kind, &l.Resource, &l.Actor, &l.Action, &l.Subj, &l.Details, &at); err != nil {
			return err
		}
		l.At = at.Int64
		d.Log = append(d.Log, l)
		return nil
	}); err != nil {
		return Data{}, err
	}
	if err := each("ai_voice_takes", `select lower(owner_email), id, coalesce(machine_id, ''), coalesce(mime, ''), state, text, audio,
			`+msOf("created_at")+`, coalesce(ms, 0), `+msOf("coalesce(updated_at, created_at)")+` from ai_voice_takes order by created_at, id`, func(r *sql.Rows) error {
		var v Voice
		var at, changed sql.NullInt64
		if err := r.Scan(&v.Owner, &v.ID, &v.Machine, &v.Mime, &v.State, &v.Text, &v.Audio, &at, &v.Ms, &changed); err != nil {
			return err
		}
		v.At, v.Changed = at.Int64, changed.Int64
		d.Voice = append(d.Voice, v)
		return nil
	}); err != nil {
		return Data{}, err
	}
	if err := each("eff_pulse", `select lower(person_email), to_char(week, 'YYYY-MM-DD'), speed, quality, friction, `+msOf("answered_at")+`
			from eff_pulse order by week, person_email`, func(r *sql.Rows) error {
		var p Pulse
		if err := r.Scan(&p.Owner, &p.Week, &p.Speed, &p.Quality, &p.Friction, &p.Answered); err != nil {
			return err
		}
		d.Pulse = append(d.Pulse, p)
		return nil
	}); err != nil {
		return Data{}, err
	}
	for _, c := range []struct {
		what, query string
		dst         *[]string
	}{
		{"people", `select lower(email) from people`, &d.People},
		{"share links", `select token_hash from resource_share_links where kind = 'artifact' and revoked_at is null`, &d.LinkHashes},
		{"doc_bindings", `select slug from doc_bindings`, &d.DocSlugs},
		{"conversations", `select id from ai_conversations where deleted_at is null`, &d.Conversation},
	} {
		dst := c.dst
		if err := each(c.what, c.query, func(r *sql.Rows) error {
			var v string
			if err := r.Scan(&v); err != nil {
				return err
			}
			*dst = append(*dst, v)
			return nil
		}); err != nil {
			return Data{}, err
		}
	}
	if err := each("resources", `select kind, count(*) from resources group by kind`, func(r *sql.Rows) error {
		var k string
		var n int
		if err := r.Scan(&k, &n); err != nil {
			return err
		}
		d.Resources[strings.TrimSpace(k)] = n
		return nil
	}); err != nil {
		return Data{}, err
	}
	return d, nil
}

// ChatRef is a conversation of llm's ai_conversations: which chat it is and whose.
type ChatRef struct {
	ID, Owner, Machine string // Owner is the lower-case email
	Deleted            bool
}

// Turn is a row of llm's ai_turns.
type Turn struct {
	ID           string
	Started      int64
	Ended        *int64
	Status, Body string
	LeftAfter    *string
}

// Entry is a row of llm's ai_entries: one line of a chat's transcript.
type Entry struct {
	ID, Kind, Body string
	Turn           *string
	At, Rev, Ord   int64
}

// Sidebar is a row of llm's ai_sidebars: a person's sidebar of chats.
type Sidebar struct {
	Owner, Doc string
	Rev, At    int64
}

// Prompt is a row of llm's offline_prompts: a prompt written while the machine was away.
type Prompt struct {
	ID, Owner, Machine, Tab, Text, State, Client, Send string
	Error                                              *string
	Created, Changed                                   int64
}

// Doc is a document or board of llm: a row of doc_bindings with the owner's details from resources.
type Doc struct {
	Slug, Kind, Title, Owner, OwnerName, Level, Runtime string
	Tab, Session, AgentSession, Provider, Path, Cwd     *string
	Created, Updated                                    int64
	Bound                                               *int64
}

// History is what the chat step reads: the chats of llm, and each one's turns and lines, one chat at a time (the lines
// are half a gigabyte, they do not fit in memory at once). *Source is one.
type History interface {
	Chats(ctx context.Context) ([]ChatRef, error)
	Turns(ctx context.Context, chat string, fn func(Turn) error) error
	Entries(ctx context.Context, chat string, fn func(Entry) error) error
	Sidebars(ctx context.Context) ([]Sidebar, error)
	Prompts(ctx context.Context) ([]Prompt, error)
	Docs(ctx context.Context) ([]Doc, error)
}

// Chats lists the conversations, deleted ones too.
func (s *Source) Chats(ctx context.Context) ([]ChatRef, error) {
	rows, err := s.db.QueryContext(ctx, `select id, lower(owner_email), coalesce(machine_id, ''), deleted_at is not null from ai_conversations order by id`)
	if err != nil {
		return nil, fmt.Errorf("ai_conversations: %w", err)
	}
	defer rows.Close()
	var out []ChatRef
	for rows.Next() {
		var c ChatRef
		if err := rows.Scan(&c.ID, &c.Owner, &c.Machine, &c.Deleted); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Turns streams the turns of a chat.
func (s *Source) Turns(ctx context.Context, chat string, fn func(Turn) error) error {
	rows, err := s.db.QueryContext(ctx, `select turn_id, started_ms, ended_ms, coalesce(status, ''), body::text, left_after::text
		from ai_turns where conv_id = $1 order by started_ms, turn_id`, chat)
	if err != nil {
		return fmt.Errorf("ai_turns: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var t Turn
		var ended sql.NullInt64
		var left sql.NullString
		if err := rows.Scan(&t.ID, &t.Started, &ended, &t.Status, &t.Body, &left); err != nil {
			return err
		}
		if ended.Valid {
			t.Ended = &ended.Int64
		}
		if left.Valid {
			t.LeftAfter = &left.String
		}
		if err := fn(t); err != nil {
			return err
		}
	}
	return rows.Err()
}

// Entries streams the lines of a chat.
func (s *Source) Entries(ctx context.Context, chat string, fn func(Entry) error) error {
	rows, err := s.db.QueryContext(ctx, `select entry_id, turn_id, at_ms, coalesce(rev, 0), coalesce(kind, ''), coalesce(ord, 0), body::text
		from ai_entries where conv_id = $1 order by at_ms, ord, entry_id`, chat)
	if err != nil {
		return fmt.Errorf("ai_entries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var e Entry
		var turn sql.NullString
		if err := rows.Scan(&e.ID, &turn, &e.At, &e.Rev, &e.Kind, &e.Ord, &e.Body); err != nil {
			return err
		}
		if turn.Valid {
			e.Turn = &turn.String
		}
		if err := fn(e); err != nil {
			return err
		}
	}
	return rows.Err()
}

// Sidebars reads ai_sidebars.
func (s *Source) Sidebars(ctx context.Context) ([]Sidebar, error) {
	rows, err := s.db.QueryContext(ctx, `select lower(owner_email), doc::text, coalesce(rev, 0), `+msOf("updated_at")+` from ai_sidebars order by owner_email`)
	if err != nil {
		return nil, fmt.Errorf("ai_sidebars: %w", err)
	}
	defer rows.Close()
	var out []Sidebar
	for rows.Next() {
		var b Sidebar
		var at sql.NullInt64
		if err := rows.Scan(&b.Owner, &b.Doc, &b.Rev, &at); err != nil {
			return nil, err
		}
		b.At = at.Int64
		out = append(out, b)
	}
	return out, rows.Err()
}

// Prompts reads offline_prompts.
func (s *Source) Prompts(ctx context.Context) ([]Prompt, error) {
	rows, err := s.db.QueryContext(ctx, `select id, lower(person_email), coalesce(machine_id, ''), coalesce(tab_id, ''), coalesce(text, ''), coalesce(state, ''),
		coalesce(client_id, ''), coalesce(send::text, '{}'), error, `+msOf("created_at")+`, `+msOf("changed_at")+` from offline_prompts order by created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("offline_prompts: %w", err)
	}
	defer rows.Close()
	var out []Prompt
	for rows.Next() {
		var p Prompt
		var errText sql.NullString
		var created, changed sql.NullInt64
		if err := rows.Scan(&p.ID, &p.Owner, &p.Machine, &p.Tab, &p.Text, &p.State, &p.Client, &p.Send, &errText, &created, &changed); err != nil {
			return nil, err
		}
		if errText.Valid {
			p.Error = &errText.String
		}
		p.Created, p.Changed = created.Int64, changed.Int64
		out = append(out, p)
	}
	return out, rows.Err()
}

// Docs reads doc_bindings with the owner's details from resources (kind «artifact» there).
func (s *Source) Docs(ctx context.Context) ([]Doc, error) {
	rows, err := s.db.QueryContext(ctx, `select b.slug, b.kind, coalesce(b.title, ''), lower(coalesce(b.owner_email, '')), coalesce(r.owner_name, ''),
		coalesce(r.team_level, ''), coalesce(b.runtime_id, ''), b.tab_id, b.session_name, b.agent_session_id, b.provider, b.source_path, b.cwd,
		`+msOf("coalesce(r.created_at, b.bound_at, b.updated_at)")+`, `+msOf("coalesce(b.updated_at, b.bound_at)")+`, `+msOf("b.bound_at")+`
		from doc_bindings b left join resources r on r.kind = 'artifact' and r.id = b.slug order by b.slug`)
	if err != nil {
		return nil, fmt.Errorf("doc_bindings: %w", err)
	}
	defer rows.Close()
	var out []Doc
	for rows.Next() {
		var d Doc
		var created, updated, bound sql.NullInt64
		if err := rows.Scan(&d.Slug, &d.Kind, &d.Title, &d.Owner, &d.OwnerName, &d.Level, &d.Runtime, &d.Tab, &d.Session, &d.AgentSession,
			&d.Provider, &d.Path, &d.Cwd, &created, &updated, &bound); err != nil {
			return nil, err
		}
		d.Created, d.Updated = created.Int64, updated.Int64
		if bound.Valid {
			d.Bound = &bound.Int64
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
