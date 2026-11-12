package llmimport

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Rule is a row of llm's automation_rules. Trigger and Limits are the JSON the hub's rules read as they are:
// both gateways name the same keys (source, filter; maxRunsPerDay, quietHoursStartMsk, quietHoursEndMsk).
type Rule struct {
	ID, Owner, Name, Trigger, Prompt, Model, Limits string
}

// Automations is what the rule step reads from llm.
type Automations interface {
	// Rules lists the rules that are switched on there. A rule that is off is not moved: nothing would run it.
	Rules(ctx context.Context) ([]Rule, error)
}

// AutomationsReport counts what the rule step did.
type AutomationsReport struct {
	On                       bool
	Read, Added, Kept, NoOwn int
	// Rules says, for each rule read, where it went: «name (llm owner) → hub owner».
	Rules []string
}

func (a AutomationsReport) summary(b *strings.Builder) {
	fmt.Fprintf(b, "automation rules: %d read (switched on in llm), %d added, %d kept (the owner has a rule of that name)\n", a.Read, a.Added, a.Kept)
	for _, r := range a.Rules {
		fmt.Fprintf(b, "  rule: %s\n", r)
	}
}

// Rules reads the rules that are switched on in llm.
func (s *Source) Rules(ctx context.Context) ([]Rule, error) {
	rows, err := s.db.QueryContext(ctx, `select id::text, lower(owner_email), name, trigger::text, prompt_template, coalesce(model, 'sonnet'), coalesce(limits::text, '{}')
		from automation_rules where enabled order by created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("automation_rules: %w", err)
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		var r Rule
		if err := rows.Scan(&r.ID, &r.Owner, &r.Name, &r.Trigger, &r.Prompt, &r.Model, &r.Limits); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

var _ Automations = (*Source)(nil)

// moveAutomations puts llm's rules that are on into the hub's automation_rules, all of them under one hub person
// (o.AutomationsOwner: a rule of llm belongs to an email that may not sign in to the hub, and a rule runs on its owner's
// machine). The key is (owner, name): a rule the owner already has is kept as it is. With o.AutomationsMachine empty the
// hub runs a rule on the first connected machine of the owner; with a hub machine id it is pinned there.
func moveAutomations(ctx context.Context, db *store.DB, src Automations, o Options, rep *Report) error {
	a := &rep.Automations
	a.On = true
	owner := strings.ToLower(strings.TrimSpace(o.AutomationsOwner))
	if owner == "" {
		return errors.New("say whose the rules become: the hub person's email (AutomationsOwner)")
	}
	list, err := src.Rules(ctx)
	if err != nil {
		return err
	}
	machine := strings.TrimSpace(o.AutomationsMachine)
	return runTx(ctx, db, o.DryRun, func(st store.Store) error {
		users, err := usersByEmail(ctx, st)
		if err != nil {
			return err
		}
		uid, ok := users[owner]
		if !ok {
			return fmt.Errorf("the hub does not know %s: they must sign in to the hub once", owner)
		}
		if machine != "" {
			var n int
			if err := st.Q().QueryRow(ctx, `SELECT COUNT(*) FROM machines WHERE id = ? AND created_by = ? AND revoked_at IS NULL`, machine, uid).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				return fmt.Errorf("machine %s is not a live machine of %s", machine, owner)
			}
		}
		now := store.Ms(o.Now())
		for _, r := range list {
			a.Read++
			var m any
			if machine != "" {
				m = machine
			}
			res, err := st.Q().Exec(ctx, `INSERT INTO automation_rules (id, owner_id, name, trigger, prompt_template, model, limits, machine_id, enabled, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?) ON CONFLICT (owner_id, name) DO NOTHING`,
				store.NewID(), uid, r.Name, r.Trigger, r.Prompt, r.Model, r.Limits, m, now, now)
			if err != nil {
				return fmt.Errorf("rule %s: %w", r.Name, err)
			}
			verdict := "added"
			if n, _ := res.RowsAffected(); n > 0 {
				a.Added++
			} else {
				a.Kept++
				verdict = "kept"
			}
			a.Rules = append(a.Rules, fmt.Sprintf("%s (llm owner %s) → %s: %s", r.Name, r.Owner, owner, verdict))
		}
		return nil
	})
}
