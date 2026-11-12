package llmimport

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// DefaultFeatures are the switches of llm the hub's paths read: where a person's chats, documents, designs, apps and
// the home page are served from. The gateway's own switches (the Go runtime paths, the streams, the shadows) end with
// the gateway and are not carried over.
var DefaultFeatures = []string{
	"hub-access", "hub-docs", "hub-history", "hub-root", "central-history", "chat-move-vm", "web-harnessly",
	"lists-db", "timeline-db", "apps-public", "apps-public-reviewer", "apps-team", "apps-team-owner",
}

// Options say what the move does and for whom.
type Options struct {
	DryRun   bool
	Features []string // empty: DefaultFeatures
	// Persons are the people whose switches move. A row for «anyone» moves only with Everyone.
	Persons  []string
	Everyone bool
	// Machines maps a machine name of llm to the id of the hub's machine; a switch for a machine not named here is left.
	Machines map[string]string
	// VoiceEveryone moves the voice takes and the weekly answers of every person, not only of Persons.
	VoiceEveryone bool
	// History, when set, runs the chat step: transcripts, sidebars, offline prompts and the documents the hub lacks.
	History History
	// Automations, when set, runs the rule step: the rules of llm that are on become AutomationsOwner's rules
	// in the hub, pinned to AutomationsMachine (a hub machine id) or, empty, to any connected machine of the owner.
	Automations        Automations
	AutomationsOwner   string
	AutomationsMachine string
	// Eval, when set (it needs History too, which tells it the hub machines), moves the evaluation of the harness: the
	// catalog, the schedules and the finished batches and runs.
	Eval Eval
	By   string
	Now  func() time.Time
}

// Check compares one set of keys of llm with the hub's.
type Check struct {
	Name     string
	LLM, Hub int
	// Missing are the keys llm has and the hub has not.
	Missing int
}

// Report is what a run did and found.
type Report struct {
	RunID    string
	DryRun   bool
	Switches struct{ Read, Added, Kept, Left int }
	// Left says why rows were not taken, by reason.
	Left map[string]int
	Log  struct{ Read, Added, Kept, NoResource int }
	// Voice counts the dictation recordings: Left are those of a person the move is not for, NoPerson of an email the
	// hub does not know.
	Voice struct{ Read, Added, Kept, Left, NoPerson int }
	// Pulse counts the weekly answers of the efficiency screen, the same way.
	Pulse struct{ Read, Added, Kept, Left, NoPerson int }
	// History is the chat step; empty when it did not run.
	History HistoryReport
	// Automations is the rule step; empty (On false) when it did not run.
	Automations AutomationsReport
	// Eval is the evaluation step; empty when it did not run.
	Eval   EvalReport
	Checks []Check
}

// Summary is the text the command prints and the run keeps.
func (r Report) Summary() string {
	var b strings.Builder
	mode := "applied"
	if r.DryRun {
		mode = "dry run (nothing written)"
	}
	fmt.Fprintf(&b, "llm import %s: %s\n", r.RunID, mode)
	fmt.Fprintf(&b, "switches: %d read, %d added, %d kept (the hub had them), %d left\n", r.Switches.Read, r.Switches.Added, r.Switches.Kept, r.Switches.Left)
	reasons := make([]string, 0, len(r.Left))
	for k := range r.Left {
		reasons = append(reasons, k)
	}
	sort.Strings(reasons)
	for _, k := range reasons {
		fmt.Fprintf(&b, "  left: %s: %d\n", k, r.Left[k])
	}
	fmt.Fprintf(&b, "access log of documents: %d read, %d added, %d kept, %d of a document the hub has no access row for\n", r.Log.Read, r.Log.Added, r.Log.Kept, r.Log.NoResource)
	fmt.Fprintf(&b, "voice takes: %d read, %d added, %d kept (the hub had them), %d of a person the move is not for, %d of an email the hub does not know\n",
		r.Voice.Read, r.Voice.Added, r.Voice.Kept, r.Voice.Left, r.Voice.NoPerson)
	fmt.Fprintf(&b, "efficiency pulse: %d read, %d added, %d kept (the hub had them), %d of a person the move is not for, %d of an email the hub does not know\n",
		r.Pulse.Read, r.Pulse.Added, r.Pulse.Kept, r.Pulse.Left, r.Pulse.NoPerson)
	if r.History.Left != nil {
		r.History.summary(&b)
	}
	if r.Automations.On {
		r.Automations.summary(&b)
	}
	if r.Eval.Left != nil {
		r.Eval.summary(&b)
	}
	for _, c := range r.Checks {
		fmt.Fprintf(&b, "check %-28s llm %5d  hub %5d  missing in the hub %d\n", c.Name, c.LLM, c.Hub, c.Missing)
	}
	return b.String()
}

var errDryRun = errors.New("dry run")

// Run takes the data from llm and moves what the hub lacks. It leaves a row in llm_import_runs.
func Run(ctx context.Context, db *store.DB, d Data, o Options) (Report, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.By == "" {
		o.By = "llm-import"
	}
	if len(o.Persons) == 0 && !o.Everyone {
		return Report{}, errors.New("say for whom the switches move: the people (Persons) or everyone")
	}
	rep := Report{DryRun: o.DryRun, Left: map[string]int{}}
	var id [6]byte
	if _, err := rand.Read(id[:]); err != nil {
		return rep, err
	}
	rep.RunID = hex.EncodeToString(id[:])
	mode := "apply"
	if o.DryRun {
		mode = "dry-run"
	}
	started := store.Ms(o.Now())
	if _, err := db.S().Q().Exec(ctx, `INSERT INTO llm_import_runs (run_id, mode, started_at) VALUES (?, ?, ?)`, rep.RunID, mode, started); err != nil {
		return rep, fmt.Errorf("the run's record: %w", err)
	}
	err := db.Tx(ctx, func(st store.Store) error {
		if err := moveSwitches(ctx, st, d, o, &rep); err != nil {
			return fmt.Errorf("switches: %w", err)
		}
		if err := moveLog(ctx, st, d, &rep); err != nil {
			return fmt.Errorf("access log: %w", err)
		}
		if err := moveVoice(ctx, st, d, o, &rep); err != nil {
			return fmt.Errorf("voice takes: %w", err)
		}
		if err := movePulse(ctx, st, d, o, &rep); err != nil {
			return fmt.Errorf("efficiency pulse: %w", err)
		}
		if o.DryRun {
			return errDryRun
		}
		return nil
	})
	if err != nil && !errors.Is(err, errDryRun) {
		finish(ctx, db, rep, o, "failed: "+err.Error())
		return rep, err
	}
	if o.History != nil {
		if herr := moveHistory(ctx, db, o.History, o, &rep); herr != nil {
			finish(ctx, db, rep, o, "failed: "+herr.Error())
			return rep, fmt.Errorf("chat step: %w", herr)
		}
	}
	if o.Automations != nil {
		if aerr := moveAutomations(ctx, db, o.Automations, o, &rep); aerr != nil {
			finish(ctx, db, rep, o, "failed: "+aerr.Error())
			return rep, fmt.Errorf("rule step: %w", aerr)
		}
	}
	if cerr := check(ctx, db.S(), d, &rep); cerr != nil {
		finish(ctx, db, rep, o, "failed: "+cerr.Error())
		return rep, fmt.Errorf("checks: %w", cerr)
	}
	finish(ctx, db, rep, o, "ok")
	return rep, nil
}

func finish(ctx context.Context, db *store.DB, rep Report, o Options, outcome string) {
	_, _ = db.S().Q().Exec(ctx, `UPDATE llm_import_runs SET finished_at = ?, outcome = ?, report = ? WHERE run_id = ?`,
		store.Ms(o.Now()), outcome, rep.Summary(), rep.RunID)
}

func moveSwitches(ctx context.Context, st store.Store, d Data, o Options, rep *Report) error {
	features := toSet(o.Features)
	if len(features) == 0 {
		features = toSet(DefaultFeatures)
	}
	persons := map[string]bool{}
	for _, p := range o.Persons {
		persons[strings.ToLower(strings.TrimSpace(p))] = true
	}
	left := func(why string) {
		rep.Left[why]++
		rep.Switches.Left++
	}
	for _, w := range d.Switches {
		rep.Switches.Read++
		person := strings.ToLower(strings.TrimSpace(w.Person))
		machine := strings.TrimSpace(w.Machine)
		switch {
		case !features[w.Feature]:
			left("a switch of the gateway itself")
			continue
		case person == "" || person == store.Wildcard:
			if !o.Everyone {
				left("for anyone (not asked for)")
				continue
			}
			person = store.Wildcard
		case !persons[person] && !o.Everyone:
			left("for a person the move is not for")
			continue
		}
		if machine == "" || machine == store.Wildcard {
			machine = store.Wildcard
		} else if hub, ok := o.Machines[machine]; ok && hub != "" {
			machine = hub
		} else {
			left("for a machine with no hub machine named: " + machine)
			continue
		}
		if o.DryRun {
			// A dry run still says what the hub already has.
			n, err := count(ctx, st, `SELECT count(*) FROM rollout_switches WHERE feature = ? AND person_email = ? AND machine_id = ?`, w.Feature, person, machine)
			if err != nil {
				return err
			}
			if n > 0 {
				rep.Switches.Kept++
			} else {
				rep.Switches.Added++
			}
			continue
		}
		at := w.UpdatedAt
		if at == 0 {
			at = store.Ms(o.Now())
		}
		note := strings.TrimSpace("from llm. " + w.Note)
		res, err := st.Q().Exec(ctx, `INSERT INTO rollout_switches (feature, person_email, machine_id, enabled, note, updated_by, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT (feature, person_email, machine_id) DO NOTHING`,
			w.Feature, person, machine, boolInt(w.Enabled), note, o.By, at)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			rep.Switches.Added++
		} else {
			rep.Switches.Kept++
		}
	}
	return nil
}

// moveLog copies the access log of the documents the hub has an access row for, once each (the id of a row is llm's).
func moveLog(ctx context.Context, st store.Store, d Data, rep *Report) error {
	have := map[string]bool{}
	rows, err := st.Q().Query(ctx, `SELECT id FROM resource_access WHERE kind = 'doc'`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			rows.Close()
			return err
		}
		have[s] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, l := range d.Log {
		rep.Log.Read++
		if !have[l.Resource] {
			rep.Log.NoResource++
			continue
		}
		logID := fmt.Sprintf("llm-%d", l.ID)
		n, err := count(ctx, st, `SELECT count(*) FROM resource_access_log WHERE log_id = ?`, logID)
		if err != nil {
			return err
		}
		if n > 0 {
			rep.Log.Kept++
			continue
		}
		rep.Log.Added++
		if _, err := st.Q().Exec(ctx, `INSERT INTO resource_access_log (log_id, kind, id, space_id, actor, action, subject, details, created_at)
			SELECT ?, 'doc', r.id, r.space_id, ?, ?, ?, ?, ? FROM resource_access r WHERE r.kind = 'doc' AND r.id = ?
			ON CONFLICT (log_id) DO NOTHING`, logID, l.Actor, l.Action, l.Subj, l.Details, l.At, l.Resource); err != nil {
			return err
		}
	}
	return nil
}

// voiceAudioMax is the bound of one recording the hub keeps (docs/hub/voice-takes.md); a bigger one moves without audio.
const voiceAudioMax = 8 << 20

// moveVoice copies the dictation recordings of the people the move is for. A take the hub already has is left as it is
// (the hub's copy may be newer: the page edits the text); the audio is base64 text there, as the hub's own route writes it.
func moveVoice(ctx context.Context, st store.Store, d Data, o Options, rep *Report) error {
	for_ := toSet(o.Persons)
	users, err := usersByEmail(ctx, st)
	if err != nil {
		return err
	}
	for _, v := range d.Voice {
		rep.Voice.Read++
		if !o.Everyone && !o.VoiceEveryone && !for_[strings.ToLower(v.Owner)] {
			rep.Voice.Left++
			continue
		}
		user, ok := users[strings.ToLower(v.Owner)]
		if !ok {
			rep.Voice.NoPerson++
			continue
		}
		n, err := count(ctx, st, `SELECT count(*) FROM voice_takes WHERE user_id = ? AND id = ?`, user, v.ID)
		if err != nil {
			return err
		}
		if n > 0 {
			rep.Voice.Kept++
			continue
		}
		var audio any
		if len(v.Audio) > 0 && len(v.Audio) <= voiceAudioMax {
			audio = base64.StdEncoding.EncodeToString(v.Audio)
		}
		mime := v.Mime
		if mime == "" {
			mime = "audio/webm"
		}
		changed := v.Changed
		if changed == 0 {
			changed = v.At
		}
		rep.Voice.Added++
		if _, err := st.Q().Exec(ctx, `INSERT INTO voice_takes (user_id, id, machine_id, at, ms, mime, state, text, audio, created_at, changed_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (user_id, id) DO NOTHING`,
			user, v.ID, v.Machine, v.At, v.Ms, mime, v.State, v.Text, audio, v.At, changed); err != nil {
			return err
		}
	}
	return nil
}

// usersByEmail maps the hub's users' lower-case emails to their ids.
func usersByEmail(ctx context.Context, st store.Store) (map[string]string, error) {
	users := map[string]string{}
	rows, err := st.Q().Query(ctx, `SELECT lower(email), id FROM users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var email, id string
		if err := rows.Scan(&email, &id); err != nil {
			return nil, err
		}
		users[email] = id
	}
	return users, rows.Err()
}

// movePulse copies the weekly answers of the efficiency screen of the people the move is for. An answer the hub already
// has (same person and week) is left as it is: the person may have answered again there.
func movePulse(ctx context.Context, st store.Store, d Data, o Options, rep *Report) error {
	for_ := toSet(o.Persons)
	users, err := usersByEmail(ctx, st)
	if err != nil {
		return err
	}
	for _, p := range d.Pulse {
		rep.Pulse.Read++
		if !o.Everyone && !o.VoiceEveryone && !for_[strings.ToLower(p.Owner)] {
			rep.Pulse.Left++
			continue
		}
		user, ok := users[strings.ToLower(p.Owner)]
		if !ok {
			rep.Pulse.NoPerson++
			continue
		}
		n, err := count(ctx, st, `SELECT count(*) FROM eff_pulse WHERE user_id = ? AND week = ?`, user, p.Week)
		if err != nil {
			return err
		}
		if n > 0 {
			rep.Pulse.Kept++
			continue
		}
		rep.Pulse.Added++
		if _, err := st.Q().Exec(ctx, `INSERT INTO eff_pulse (user_id, week, speed, quality, friction, answered_at) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (user_id, week) DO NOTHING`, user, p.Week, p.Speed, p.Quality, p.Friction, p.Answered); err != nil {
			return err
		}
	}
	return nil
}

// check compares the keys llm holds with the hub's own tables.
func check(ctx context.Context, st store.Store, d Data, rep *Report) error {
	for _, c := range []struct {
		name, query string
		llm         []string
	}{
		{"people (email)", `SELECT lower(email) FROM identities UNION SELECT lower(email) FROM users`, d.People},
		{"document share links", `SELECT token_hash FROM resource_guest_links WHERE kind = 'doc' AND revoked_at IS NULL`, d.LinkHashes},
		{"documents (slug)", `SELECT slug FROM doc_meta`, d.DocSlugs},
		{"conversations (not deleted)", `SELECT conv_id FROM history_conversations WHERE deleted_at IS NULL`, d.Conversation},
	} {
		hub, err := keys(ctx, st, c.query)
		if err != nil {
			return fmt.Errorf("%s: %w", c.name, err)
		}
		miss := 0
		for _, k := range c.llm {
			if !hub[k] {
				miss++
			}
		}
		rep.Checks = append(rep.Checks, Check{Name: c.name, LLM: len(c.llm), Hub: len(hub), Missing: miss})
	}
	return nil
}

func keys(ctx context.Context, st store.Store, query string) (map[string]bool, error) {
	rows, err := st.Q().Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out[s] = true
	}
	return out, rows.Err()
}

func count(ctx context.Context, st store.Store, query string, args ...any) (int, error) {
	rows, err := st.Q().Query(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	if rows.Next() {
		if err := rows.Scan(&n); err != nil {
			return 0, err
		}
	}
	return n, rows.Err()
}

func toSet(a []string) map[string]bool {
	m := map[string]bool{}
	for _, s := range a {
		if s = strings.TrimSpace(s); s != "" {
			m[s] = true
		}
	}
	return m
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
