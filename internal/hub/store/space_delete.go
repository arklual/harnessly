package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Deleting a space with everything in it. SpaceCascade is the whole of it: every table that holds rows of a space, with
// the condition that picks them, children before the rows they point at. A table that is in neither SpaceCascade nor
// keptOnSpaceDelete fails the test, so a new table is either deleted with its space or says why it is not.
//
// In a condition :id is the space's id, :slug its slug and :key the key its spend is kept under when it has no
// organization (space:<id>); a space of an organization shares the organization's key, which names nothing here.

// SpaceCascadeStep deletes the rows of one table that belong to the space.
type SpaceCascadeStep struct {
	Table string
	Where string
}

const (
	machinesOfSpace = `(SELECT id FROM machines WHERE space_id = :id)`
	teamsOfSpace    = `(SELECT id FROM teams WHERE space_id = :id)`
	// docsOfSpace are the documents of the space and the boards drawn from its machines (board_bindings): both
	// have threads and comments in the same tables.
	docsOfSpace  = `(SELECT slug FROM doc_meta WHERE space_id = :id UNION SELECT slug FROM board_bindings WHERE space_id = :id)`
	sitesOfSpace = `(SELECT id FROM sites WHERE space = :slug)`
	// scopedToSpace picks the rows set on the space or on one of its teams.
	scopedToSpace   = `((scope_kind = 'tenant' AND scope_id = :id) OR (scope_kind = 'team' AND scope_id IN ` + teamsOfSpace + `))`
	layeredOnSpace  = `((layer = 'tenant' AND scope_id = :id) OR (layer = 'team' AND scope_id IN ` + teamsOfSpace + `))`
	accountsOfSpace = `(SELECT id FROM accounts WHERE org_key = :key OR ` + scopedToSpace + `)`
	catalogOfSpace  = `(SELECT id FROM catalog_items WHERE (owner_layer = 'tenant' AND owner_scope = :id) OR (owner_layer = 'team' AND owner_scope IN ` + teamsOfSpace + `))`
	bySpace         = `space_id = :id`
	byMachine       = `machine_id IN ` + machinesOfSpace
)

// SpaceCascade is the order a space is deleted in.
var SpaceCascade = []SpaceCascadeStep{
	// Documents and what is said under them.
	{"resource_guest_comments", `link_id IN (SELECT link_id FROM resource_guest_links WHERE space_id = :id)`},
	{"doc_comment_mentions", `comment_id IN (SELECT c.id FROM doc_comments c JOIN doc_threads t ON t.id = c.thread_id WHERE t.slug IN ` + docsOfSpace + `)`},
	{"doc_comments", `thread_id IN (SELECT id FROM doc_threads WHERE slug IN ` + docsOfSpace + `)`},
	{"doc_threads", `slug IN ` + docsOfSpace},
	{"doc_reads", `slug IN ` + docsOfSpace},
	{"resource_guest_links", bySpace},
	{"resource_access_log", bySpace},
	{"resource_access_requests", bySpace},
	{"resource_access_people", bySpace},
	{"resource_access_teams", bySpace},
	{"resource_access", bySpace},
	{"doc_meta", bySpace},
	{"board_bindings", bySpace},

	// Designs are projects of the space's machines.
	{"design_share_views", `link_id IN (SELECT id FROM design_share_links WHERE ` + byMachine + `)`},
	{"design_guest_comments", `link_id IN (SELECT id FROM design_share_links WHERE ` + byMachine + `)`},
	{"design_share_links", byMachine},
	{"design_access_log", byMachine},
	{"design_invites", byMachine},
	{"design_members", byMachine},
	{"design_requests", byMachine},
	{"design_team_grants", byMachine},
	{"design_projects", byMachine},

	// Sites carry the space's slug.
	{"site_deployments", `site_id IN ` + sitesOfSpace},
	{"site_pending", `site_id IN ` + sitesOfSpace},
	{"site_tokens", `site_id IN ` + sitesOfSpace},
	{"site_space_blobs", `space = :slug`},
	{"sites", `space = :slug`},

	// Spend and what the agents did.
	{"agent_session_mrs", `EXISTS (SELECT 1 FROM agent_sessions a WHERE a.org_key = agent_session_mrs.org_key AND a.provider = agent_session_mrs.provider
		AND a.session_key = agent_session_mrs.session_key AND a.space_id = :id)`},
	{"agent_sessions", bySpace},
	{"tool_usage_day", bySpace},
	{"usage_rollup_hour", bySpace},
	{"usage_calls", `org_key = :key OR ` + byMachine},
	{"usage_quarantine", byMachine},
	{"env_runs", bySpace},

	// Provider accounts of the space and its teams, the limits and the roles set there.
	{"account_balances", `account_id IN ` + accountsOfSpace},
	{"account_window_cycles", `account_id IN ` + accountsOfSpace},
	{"account_windows", `account_id IN ` + accountsOfSpace},
	{"account_grants", `account_id IN ` + accountsOfSpace + ` OR ` + scopedToSpace},
	{"limit_rules", `org_key = :key OR account_id IN ` + accountsOfSpace + ` OR ` + scopedToSpace},
	{"accounts", `org_key = :key OR ` + scopedToSpace},
	{"role_grants", scopedToSpace},
	{"org_changes", `org_key = :key OR ` + scopedToSpace},
	{"org_signals", `org_key = :key OR ` + scopedToSpace},
	{"org_features", `org_key = :key`},

	// The environment set on the space and its teams, and each person's own switches in it.
	{"env_person_group", `scope = :id`},
	{"env_person_off", `scope = :id`},
	{"env_entries", layeredOnSpace + ` OR item_id IN ` + catalogOfSpace},
	{"env_presets", layeredOnSpace},
	{"env_groups", layeredOnSpace},
	{"catalog_items", `(owner_layer = 'tenant' AND owner_scope = :id) OR (owner_layer = 'team' AND owner_scope IN ` + teamsOfSpace + `)`},

	// Everything keyed by a machine of the space, and the sessions opened on them.
	{"eval_checks", byMachine},
	{"eval_runs", byMachine},
	{"eval_batches", byMachine},
	{"eval_schedules", byMachine},
	{"automation_runs", byMachine + ` OR rule_id IN (SELECT id FROM automation_rules WHERE ` + byMachine + `)`},
	{"automation_rules", byMachine},
	{"history_entries", byMachine},
	{"history_turns", byMachine},
	{"history_conversations", byMachine},
	{"history_cursors", byMachine},
	{"chat_work", byMachine},
	{"offline_prompts", byMachine},
	{"offline_chats", byMachine},
	{"memory_notes", byMachine},
	{"voice_takes", byMachine},
	{"upload_refs", byMachine},
	{"upload_owners", byMachine},
	{"migration_objects", byMachine},
	{"migration_runs", byMachine},
	{"env_state", byMachine},
	{"env_apply_pending", byMachine},
	{"agent_connections", byMachine},
	{"ingest_cursors", byMachine},
	{"rollout_switches", byMachine},
	{"machine_access", byMachine},
	{"machine_debug_openings", byMachine},
	{"machine_budgets", byMachine},
	{"machine_cards", byMachine},
	{"machine_env", byMachine},
	{"machine_hw", byMachine},
	{"machine_links", byMachine},
	{"machine_managed", byMachine},
	{"machine_metrics_5m", byMachine},
	{"machine_provider_limits", byMachine},
	{"machine_ops", byMachine},
	{"machine_resource_conflicts", byMachine},
	{"machine_resources", byMachine},
	{"machine_trust", byMachine},
	{"sessions", byMachine},
	{"provider_ops", bySpace},
	{"provider_servers", bySpace},
	{"registration_codes", bySpace},
	{"machines", bySpace},

	// The people, the teams and the space itself.
	{"invite_team", `invite_id IN (SELECT id FROM invites WHERE space_id = :id) OR team_id IN ` + teamsOfSpace},
	{"invite_uses", `invite_id IN (SELECT id FROM invites WHERE space_id = :id)`},
	{"invites", bySpace},
	{"team_members", `team_id IN ` + teamsOfSpace},
	{"memberships", bySpace},
	{"space_ssh_keys", bySpace},
	{"audit_events", bySpace},
	{"teams", bySpace},
	{"spaces", `id = :id`},
}

// keptOnSpaceDelete lists the tables a space's deletion leaves alone, each with why.
var keptOnSpaceDelete = map[string]string{
	"schema_migrations": "installation", "currency_rates": "installation", "prices": "installation",
	"job_leases":      "installation: which replica does a background job",
	"auth_challenges": "installation", "auth_events": "installation: the record of the deletion itself is here",
	"users": "person", "identities": "person", "passkeys": "person", "person_settings": "person",
	"orgs":                  "the organization outlives its spaces",
	"merged_mrs":            "merge requests are keyed by their address and name no machine",
	"eff_pulse":             "a person's own weekly answers",
	"eff_regression_alerts": "alerts addressed to a person, name no space",
	"eff_source_facts":      "facts of the installation's sources, name no space",
	"eff_source_state":      "state of the installation's sources",
	"history_sidebars":      "a person's own sidebar, kept per person",
	"review_notes":          "a person's own review drafts, kept per person and chat",
	"entry_tickets":         "a person's passes: a minute to trade, twelve hours as a note, then swept by time",
	"site_blobs":            "content-addressed blobs shared by sites; a space's claim on them is site_space_blobs",
	"eval_arms":             "the evaluation's catalog: installation",
	"eval_tasks":            "the evaluation's catalog: installation",
	"eval_suites":           "the evaluation's catalog: installation",
	"llm_import_runs":       "the record of a one-time move of the old gateway's data: installation, names no space",
	"vault_audit":           "the vault is a person's", "vault_entries": "the vault is a person's", "vault_grants": "the vault is a person's",
}

// DeleteSpaceData removes the space and every row that belongs to it, and says how many rows went from each table.
// Call it in a transaction: a step that fails leaves the space whole.
func (s Store) DeleteSpaceData(ctx context.Context, sp Space) (map[string]int64, error) {
	removed := map[string]int64{}
	for _, step := range SpaceCascade {
		q, args := spaceCascadeQuery(step, sp)
		res, err := s.q.Exec(ctx, q, args...)
		if err != nil {
			return nil, fmt.Errorf("delete space: %s: %w", step.Table, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			removed[step.Table] = n
		}
	}
	return removed, nil
}

// spaceCascadeQuery turns a step into a statement and its arguments, in the order the names appear.
func spaceCascadeQuery(step SpaceCascadeStep, sp Space) (string, []any) {
	values := map[string]any{"id": sp.ID, "slug": sp.Slug, "key": "space:" + sp.ID}
	var b strings.Builder
	var args []any
	b.WriteString("DELETE FROM " + step.Table + " WHERE ")
	rest := step.Where
	for {
		i := strings.IndexByte(rest, ':')
		if i < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:i])
		rest = rest[i+1:]
		j := 0
		for j < len(rest) && rest[j] >= 'a' && rest[j] <= 'z' {
			j++
		}
		b.WriteByte('?')
		args = append(args, values[rest[:j]])
		rest = rest[j:]
	}
	return b.String(), args
}

// MembersWithoutOtherSpace counts the members of the space, the actor aside, for whom it is the only space.
func (s Store) MembersWithoutOtherSpace(ctx context.Context, spaceID, exceptUserID string) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM memberships m WHERE m.space_id = ? AND m.user_id <> ?
		AND NOT EXISTS (SELECT 1 FROM memberships o WHERE o.user_id = m.user_id AND o.space_id <> m.space_id)`, spaceID, exceptUserID).Scan(&n)
	return n, err
}

// AddOperatorRead writes that an administrator of the installation used that role on something they hold no place in,
// unless the same line (actor, action, scope, target) is already there since the given time: repeats collapse into one
// line. It says whether it wrote.
func (s Store) AddOperatorRead(ctx context.Context, e *AuditEvent, since time.Time) (bool, error) {
	col, val := "org_id", e.OrgID
	if e.SpaceID != "" {
		col, val = "space_id", e.SpaceID
	}
	var n int
	if err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM audit_events WHERE `+col+` = ? AND at > ? AND actor_id = ? AND action = ? AND scope_kind = ?
		AND scope_id = ? AND target_kind = ? AND target_id = ?`, val, Ms(since), e.ActorID, e.Action, e.ScopeKind, e.ScopeID, e.TargetKind, e.TargetID).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	return true, s.AddAudit(ctx, e)
}
