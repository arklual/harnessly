package llmimport

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The chat step: the transcripts of llm's chats, the sidebars, the prompts written offline and the documents
// the hub lacks. A line is taken once, by its key (hub machine, chat, line): a line the hub has is left as it is, so the
// step can be run again and changes nothing the second time. A chat of llm moves into the hub chat with the same id (the
// hub has one per machine of a person); a chat the hub has no record of is left and counted, never made up.

// HistoryReport counts what the chat step did.
type HistoryReport struct {
	// Chats: Read are llm's; Moved are those with exactly one hub chat of the same owner; the rest are left, by reason in Left.
	Chats struct{ Read, Moved, Full int }
	// Turns and Entries: Added are the ones the hub lacked, Kept the ones it had.
	Turns, Entries struct{ Read, Added, Kept int }
	Sidebars       struct{ Read, Added, Kept, NoPerson int }
	Prompts        struct{ Read, Added, Kept, NoPerson, NoMachine int }
	Docs           struct{ Read, Added, Kept, NoMachine int }
	Left           map[string]int
	// PerMachine is the number of lines added to each hub machine.
	PerMachine map[string]int
}

func (h HistoryReport) summary(b *strings.Builder) {
	fmt.Fprintf(b, "chats: %d read, %d moved into a hub chat (%d of them already whole), %d left\n", h.Chats.Read, h.Chats.Moved, h.Chats.Full, h.Chats.Read-h.Chats.Moved)
	for _, k := range sortedKeys(h.Left) {
		fmt.Fprintf(b, "  left: %s: %d\n", k, h.Left[k])
	}
	fmt.Fprintf(b, "chat turns: %d read, %d added, %d kept\n", h.Turns.Read, h.Turns.Added, h.Turns.Kept)
	fmt.Fprintf(b, "chat lines: %d read, %d added, %d kept\n", h.Entries.Read, h.Entries.Added, h.Entries.Kept)
	for _, k := range sortedKeys(h.PerMachine) {
		fmt.Fprintf(b, "  lines added to machine %s: %d\n", k, h.PerMachine[k])
	}
	fmt.Fprintf(b, "sidebars: %d read, %d added, %d kept, %d of an email the hub does not know\n", h.Sidebars.Read, h.Sidebars.Added, h.Sidebars.Kept, h.Sidebars.NoPerson)
	fmt.Fprintf(b, "offline prompts: %d read, %d added, %d kept, %d of an email the hub does not know, %d of a machine with no hub machine\n",
		h.Prompts.Read, h.Prompts.Added, h.Prompts.Kept, h.Prompts.NoPerson, h.Prompts.NoMachine)
	fmt.Fprintf(b, "documents and boards: %d read, %d added, %d kept (the hub had them), %d added without a machine (no space yet)\n",
		h.Docs.Read, h.Docs.Added, h.Docs.Kept, h.Docs.NoMachine)
}

// hubChat is a chat of the hub: the machine it is on and whose it is.
type hubChat struct{ machine, owner string }

// moveHistory runs the chat step. Every chat is its own transaction, so a long move holds no long lock and a failure
// loses one chat at most. A dry run does the writes and rolls each transaction back, so its counts are the real ones.
func moveHistory(ctx context.Context, db *store.DB, src History, o Options, rep *Report) error {
	h := &rep.History
	h.Left, h.PerMachine = map[string]int{}, map[string]int{}
	byChat := map[string][]hubChat{}
	machineOf := map[string]string{} // llm machine + "|" + owner email → hub machine
	spaceOf := map[string]string{}   // hub machine → its space
	users := map[string]string{}
	err := func() error {
		st := db.S()
		rows, err := st.Q().Query(ctx, `SELECT h.conv_id, h.machine_id, lower(u.email) FROM history_conversations h JOIN users u ON u.id = h.owner_id`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var c hubChat
			var id string
			if err := rows.Scan(&id, &c.machine, &c.owner); err != nil {
				rows.Close()
				return err
			}
			byChat[id] = append(byChat[id], c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		mrows, err := st.Q().Query(ctx, `SELECT id, coalesce(space_id, '') FROM machines`)
		if err != nil {
			return err
		}
		for mrows.Next() {
			var id, sp string
			if err := mrows.Scan(&id, &sp); err != nil {
				mrows.Close()
				return err
			}
			spaceOf[id] = sp
		}
		mrows.Close()
		if err := mrows.Err(); err != nil {
			return err
		}
		users, err = usersByEmail(ctx, st)
		return err
	}()
	if err != nil {
		return fmt.Errorf("the hub's chats: %w", err)
	}
	chats, err := src.Chats(ctx)
	if err != nil {
		return err
	}
	// Which hub machine an llm machine is for a person comes from the chats that match, so no list of names is kept here.
	type pick struct {
		chat ChatRef
		to   hubChat
	}
	var picks []pick
	for _, c := range chats {
		h.Chats.Read++
		found := byChat[c.ID]
		var same []hubChat
		for _, x := range found {
			if x.owner == c.Owner {
				same = append(same, x)
			}
		}
		switch {
		case len(found) == 0:
			h.Left["no chat with this id in the hub"]++
		case len(same) == 0:
			h.Left["the hub's chat is another person's"]++
		case len(same) > 1:
			h.Left["more than one hub chat with this id"]++
		default:
			picks = append(picks, pick{c, same[0]})
			if c.Machine != "" {
				machineOf[c.Machine+"|"+c.Owner] = same[0].machine
			}
		}
	}
	for _, p := range picks {
		added, err := moveChat(ctx, db, src, p.chat, p.to.machine, o.DryRun, h)
		if err != nil {
			return fmt.Errorf("chat %s: %w", p.chat.ID, err)
		}
		h.Chats.Moved++
		if added == 0 {
			h.Chats.Full++
		}
	}
	if err := moveSidebars(ctx, db, src, o, users, h); err != nil {
		return fmt.Errorf("sidebars: %w", err)
	}
	if err := movePrompts(ctx, db, src, o, users, machineOf, h); err != nil {
		return fmt.Errorf("offline prompts: %w", err)
	}
	if err := moveDocs(ctx, db, src, o, machineOf, spaceOf, h); err != nil {
		return fmt.Errorf("documents: %w", err)
	}
	if o.Eval != nil {
		if err := moveEval(ctx, db, o.Eval, o, users, machineOf, &rep.Eval); err != nil {
			return fmt.Errorf("evaluation: %w", err)
		}
	}
	return nil
}

// moveChat takes the turns and lines of one chat the hub lacks into the hub's chat on the machine. It says how many it added.
func moveChat(ctx context.Context, db *store.DB, src History, c ChatRef, machine string, dry bool, h *HistoryReport) (int, error) {
	added := 0
	var turns, lines struct{ read, added int }
	err := db.Tx(ctx, func(st store.Store) error {
		turns, lines, added = struct{ read, added int }{}, struct{ read, added int }{}, 0
		if err := src.Turns(ctx, c.ID, func(t Turn) error {
			turns.read++
			res, err := st.Q().Exec(ctx, `INSERT INTO history_turns (machine_id, conv_id, turn_id, started_ms, ended_ms, status, body, left_after)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (machine_id, conv_id, turn_id) DO NOTHING`,
				machine, c.ID, t.ID, t.Started, t.Ended, t.Status, t.Body, t.LeftAfter)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				turns.added++
				added++
			}
			return nil
		}); err != nil {
			return err
		}
		if err := src.Entries(ctx, c.ID, func(e Entry) error {
			lines.read++
			res, err := st.Q().Exec(ctx, `INSERT INTO history_entries (machine_id, conv_id, entry_id, turn_id, at_ms, rev, kind, ord, body)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (machine_id, conv_id, entry_id) DO NOTHING`,
				machine, c.ID, e.ID, e.Turn, e.At, e.Rev, e.Kind, e.Ord, e.Body)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				lines.added++
				added++
			}
			return nil
		}); err != nil {
			return err
		}
		if dry {
			return errDryRun
		}
		return nil
	})
	if err != nil && !errors.Is(err, errDryRun) {
		return 0, err
	}
	h.Turns.Read += turns.read
	h.Turns.Added += turns.added
	h.Turns.Kept += turns.read - turns.added
	h.Entries.Read += lines.read
	h.Entries.Added += lines.added
	h.Entries.Kept += lines.read - lines.added
	if lines.added > 0 {
		h.PerMachine[machine] += lines.added
	}
	return added, nil
}

func moveSidebars(ctx context.Context, db *store.DB, src History, o Options, users map[string]string, h *HistoryReport) error {
	list, err := src.Sidebars(ctx)
	if err != nil {
		return err
	}
	return runTx(ctx, db, o.DryRun, func(st store.Store) error {
		for _, b := range list {
			h.Sidebars.Read++
			user, ok := users[b.Owner]
			if !ok {
				h.Sidebars.NoPerson++
				continue
			}
			res, err := st.Q().Exec(ctx, `INSERT INTO history_sidebars (owner_id, rev, doc, updated_at) VALUES (?, ?, ?, ?) ON CONFLICT (owner_id) DO NOTHING`,
				user, b.Rev, b.Doc, b.At)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				h.Sidebars.Added++
			} else {
				h.Sidebars.Kept++
			}
		}
		return nil
	})
}

func movePrompts(ctx context.Context, db *store.DB, src History, o Options, users, machineOf map[string]string, h *HistoryReport) error {
	list, err := src.Prompts(ctx)
	if err != nil {
		return err
	}
	return runTx(ctx, db, o.DryRun, func(st store.Store) error {
		for _, p := range list {
			h.Prompts.Read++
			user, ok := users[p.Owner]
			if !ok {
				h.Prompts.NoPerson++
				continue
			}
			machine, ok := machineOf[p.Machine+"|"+p.Owner]
			if !ok {
				h.Prompts.NoMachine++
				continue
			}
			send := p.Send
			if send == "" {
				send = "{}"
			}
			res, err := st.Q().Exec(ctx, `INSERT INTO offline_prompts (id, user_id, machine_id, tab_id, client_id, text, send, state, error, created_at, changed_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (id) DO NOTHING`,
				p.ID, user, machine, p.Tab, p.Client, p.Text, send, p.State, p.Error, p.Created, p.Changed)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				h.Prompts.Added++
			} else {
				h.Prompts.Kept++
			}
		}
		return nil
	})
}

// moveDocs records the documents and boards the hub has no record of. Their content stays where it is (the artifacts
// catalog serves it by slug); the hub holds who owns one and its level. A document goes into the space of the machine
// it was published from when that machine is known, and without a space otherwise: the hub gives it one the next time
// its owner publishes it (docs.putMeta).
func moveDocs(ctx context.Context, db *store.DB, src History, o Options, machineOf, spaceOf map[string]string, h *HistoryReport) error {
	list, err := src.Docs(ctx)
	if err != nil {
		return err
	}
	return runTx(ctx, db, o.DryRun, func(st store.Store) error {
		for _, d := range list {
			h.Docs.Read++
			n, err := count(ctx, st, `SELECT count(*) FROM doc_meta WHERE slug = ?`, d.Slug)
			if err != nil {
				return err
			}
			if n > 0 {
				h.Docs.Kept++
				continue
			}
			var machine, space any
			if llmMachine, _, ok := strings.Cut(d.Runtime, ":"); ok {
				if m, ok := machineOf[llmMachine+"|"+d.Owner]; ok {
					machine = m
					if sp := spaceOf[m]; sp != "" {
						space = sp
					}
				}
			}
			if machine == nil {
				h.Docs.NoMachine++
			}
			level := d.Level
			if level == "" {
				level = "none"
			}
			if _, err := st.Q().Exec(ctx, `INSERT INTO doc_meta (slug, space_id, owner_email, owner_name, machine_id, source_path, title, team_level, created_at, updated_at,
					tab_id, session_name, agent_session_id, provider, cwd, bound_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (slug) DO NOTHING`,
				d.Slug, space, d.Owner, d.OwnerName, machine, d.Path, d.Title, level, d.Created, d.Updated,
				d.Tab, d.Session, d.AgentSession, d.Provider, d.Cwd, d.Bound); err != nil {
				return err
			}
			h.Docs.Added++
		}
		return nil
	})
}

// runTx runs fn in a transaction that a dry run rolls back.
func runTx(ctx context.Context, db *store.DB, dry bool, fn func(store.Store) error) error {
	err := db.Tx(ctx, func(st store.Store) error {
		if err := fn(st); err != nil {
			return err
		}
		if dry {
			return errDryRun
		}
		return nil
	})
	if errors.Is(err, errDryRun) {
		return nil
	}
	return err
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
