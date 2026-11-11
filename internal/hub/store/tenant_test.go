package store

import (
	"sort"
	"testing"
)

// noSpace lists the tables that carry no space_id, each with what ties its rows to a space instead (or why nothing
// does). A table that is not here must name its space: a new table of people's data either gets a space_id column or
// a line here that says why it needs none.
var noSpace = map[string]string{
	// The installation itself.
	"schema_migrations": "installation", "rollout_switches": "installation", "currency_rates": "installation", "prices": "installation",
	"auth_challenges": "installation", "auth_events": "installation", "ingest_cursors": "installation", "job_leases": "installation",
	// A person, in whatever space they are.
	"users": "person", "identities": "person", "passkeys": "person", "sessions": "person", "person_settings": "person",
	"env_person_group": "person", "env_person_off": "person", "role_grants": "person",
	"entry_tickets": "person",
	// An organization, or a child of a row that names its space.
	"orgs": "organization", "org_changes": "organization", "org_features": "organization", "org_signals": "organization",
	"team_members": "team", "invite_team": "invite", "invite_uses": "invite", "limit_rules": "scope",
	"catalog_items": "organization", "env_entries": "scope", "env_groups": "scope", "env_presets": "scope", "env_apply_pending": "scope",
	// A machine, whose row names the space.
	"machine_access": "machine", "machine_budgets": "machine", "machine_cards": "machine", "machine_env": "machine",
	"machine_hw": "machine", "machine_links": "machine", "machine_managed": "machine", "machine_metrics_5m": "machine",
	"machine_ops": "machine", "machine_resources": "machine", "machine_resource_conflicts": "machine", "machine_trust": "machine", "agent_connections": "machine",
	"migration_objects": "machine", "migration_runs": "machine", "env_state": "machine", "offline_chats": "machine",
	"offline_prompts": "machine", "history_conversations": "machine", "chat_work": "machine", "history_cursors": "machine", "history_entries": "machine",
	"history_sidebars": "machine", "review_notes": "machine", "history_turns": "machine", "memory_notes": "machine", "voice_takes": "machine",
	"upload_owners": "machine", "upload_refs": "machine", "agent_session_mrs": "machine", "merged_mrs": "machine",
	"usage_calls": "machine", "eff_pulse": "machine", "eff_source_facts": "machine", "eff_source_state": "machine",
	"automation_rules": "machine", "automation_runs": "machine", "usage_quarantine": "machine",
	// A design, which is a project of a machine.
	"design_access_log": "design", "design_guest_comments": "design", "design_invites": "design", "design_members": "design",
	"design_projects": "design", "design_requests": "design", "design_share_links": "design", "design_share_views": "design",
	"design_team_grants": "design",
	// A provider account and what is read from it.
	"accounts": "account", "account_balances": "account", "account_grants": "account", "account_window_cycles": "account",
	"account_windows": "account",
	// A site carries the slug of its space.
	"sites": "site", "site_blobs": "site", "site_deployments": "site", "site_pending": "site", "site_space_blobs": "site", "site_tokens": "site",
	// The vault has scopes of its own.
	"vault_audit": "vault", "vault_entries": "vault", "vault_grants": "vault",
	// Reached only through a document, after the check on the document.
	"doc_threads": "document", "doc_comments": "document", "doc_reads": "document", "doc_comment_mentions": "document",
	// Reached only through a guest link, which names its space.
	"resource_guest_comments": "guest link",
}

func TestTablesNameTheirSpace(t *testing.T) {
	for _, driver := range []Driver{SQLite, Postgres} {
		s, err := ExpectedSchema(driver)
		if err != nil {
			t.Fatal(err)
		}
		var missing, stale []string
		for name, tbl := range s.Tables {
			has := name == "spaces"
			for _, c := range tbl.Columns {
				if c.Name == "space_id" {
					has = true
				}
			}
			_, listed := noSpace[name]
			switch {
			case !has && !listed:
				missing = append(missing, name)
			case has && listed:
				stale = append(stale, name)
			}
		}
		sort.Strings(missing)
		sort.Strings(stale)
		if len(missing) > 0 {
			t.Errorf("%s: tables without space_id: %v. Give the table a space_id, or list it in noSpace with what ties its rows to a space", driver, missing)
		}
		if len(stale) > 0 {
			t.Errorf("%s: tables listed in noSpace that have a space_id now: %v", driver, stale)
		}
	}
	for _, name := range []string{"doc_meta", "resource_access", "resource_access_people", "resource_access_teams",
		"resource_access_requests", "resource_access_log", "resource_guest_links"} {
		if _, ok := noSpace[name]; ok {
			t.Errorf("%s holds documents or rights: it must name its space", name)
		}
	}
}
