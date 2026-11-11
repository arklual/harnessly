package store

// DataOwner is where a kind of data is made and changed: the one place whose word decides what the data is.
type DataOwner string

const (
	// OwnerMachine: a machine's runtime has the original and the hub keeps a copy of it.
	OwnerMachine DataOwner = "machine"
	// OwnerHub: the hub's database is the only place the data is.
	OwnerHub DataOwner = "hub"
	// OwnerOutside: a service outside (a model provider, a code host, a server provider) has the original and the hub
	// keeps what it read there.
	OwnerOutside DataOwner = "outside"
)

// HistoryDeletedKeep names the setting that says how long the hub keeps the copy of a chat deleted on its machine.
const HistoryDeletedKeep = "HUB_HISTORY_DELETED_DAYS"

// DataKind is one kind of data in the hub's database: its tables, its owner, how long the hub keeps it, and what
// becomes of the hub's rows when the owner deletes the original.
type DataKind struct {
	Name   string
	Tables []string
	Owner  DataOwner
	// Keep is how long a row lives. "no term" is a fact, not a decision: nothing removes the rows by age.
	Keep string
	// OwnerDeletes is what happens in the hub when the owner deletes the original; for the hub's own data, what a
	// delete in the hub does.
	OwnerDeletes string
}

// DataKinds is the register of the data in the hub's database (docs/hub/data-owners.md). Every table of the schema is
// in exactly one kind: a new table is added here with its owner and its term, or the tests fail. The register covers
// the tables of this database only; the blobs of spaces and the documents' store have terms of their own.
var DataKinds = []DataKind{
	{Name: "chat history", Owner: OwnerMachine,
		Tables:       []string{"history_conversations", "history_turns", "history_entries", "history_cursors", "chat_work"},
		Keep:         "while the chat is on its machine; the copy of a deleted chat for " + HistoryDeletedKeep + " (30 days)",
		OwnerDeletes: "the runtime's conversation_removed marks the copy (deleted_at) and readers stop seeing it at once; after the term the sweep removes the chat with its turns, entries, channel cursor and the review drafts of it (history.Sweeper)"},
	{Name: "sidebar layout", Owner: OwnerHub, Tables: []string{"history_sidebars"},
		Keep: "no term", OwnerDeletes: "one document per person, replaced on every write; nothing removes it"},
	{Name: "review drafts", Owner: OwnerHub, Tables: []string{"review_notes"},
		Keep: "no term", OwnerDeletes: "the person removes a note on the review page or sends the review, which clears its notes; when the chat leaves the hub (history.Sweeper, after its term) its drafts go with it"},
	{Name: "spend", Owner: OwnerMachine,
		Tables:       []string{"usage_calls", "usage_rollup_hour", "usage_quarantine", "tool_usage_day", "ingest_cursors", "agent_sessions", "agent_session_mrs"},
		Keep:         "no term",
		OwnerDeletes: "a machine never takes a fact back: what was pulled stays as the account of the spend, also after the machine is gone"},
	{Name: "agent memory", Owner: OwnerMachine, Tables: []string{"memory_notes"},
		Keep:         "no term; a deleted note stays as an empty tombstone",
		OwnerDeletes: "the note's row is emptied and marked deleted so the person's other machines learn of it; the tombstone is never removed"},
	{Name: "chat attachments", Owner: OwnerMachine, Tables: []string{"upload_refs", "upload_owners"},
		Keep: "no term", OwnerDeletes: "nothing: the rows and the blobs they name stay after the chat or the machine is deleted"},
	{Name: "messages waiting for a machine", Owner: OwnerHub, Tables: []string{"offline_prompts", "offline_chats"},
		Keep:         "until delivered or failed, then removed by the outbox's sweep",
		OwnerDeletes: "the person withdraws a queued message and the row is deleted"},
	{Name: "dictation recordings", Owner: OwnerHub, Tables: []string{"voice_takes"},
		Keep: "audio 30 days, text no term", OwnerDeletes: "the person deletes a take and the row is deleted"},
	{Name: "machines", Owner: OwnerHub,
		Tables: []string{"machines", "machine_access", "machine_trust", "machine_env", "machine_managed", "machine_budgets",
			"machine_ops", "machine_links", "agent_connections", "registration_codes", "space_ssh_keys"},
		Keep:         "while the machine is registered; registration codes a day past their expiry",
		OwnerDeletes: "removing a machine deletes its access, trust and environment rows; its other rows stay"},
	{Name: "what a machine reports about itself", Owner: OwnerMachine,
		Tables:       []string{"machine_hw", "machine_cards", "machine_resources", "machine_resource_conflicts", "machine_metrics_5m", "env_state", "env_runs", "env_apply_pending"},
		Keep:         "the last report replaces the one before; metrics and setup runs are trimmed by age",
		OwnerDeletes: "nothing: the last report stays after the machine is gone"},
	{Name: "moves of a machine's data", Owner: OwnerHub, Tables: []string{"migration_runs", "migration_objects"},
		Keep: "no term", OwnerDeletes: "nothing: the journal of a move stays"},
	{Name: "accounts and sign-in", Owner: OwnerHub,
		Tables:       []string{"users", "identities", "passkeys", "sessions", "auth_challenges", "auth_events", "person_settings"},
		Keep:         "sessions and challenges until they expire; sign-in events 180 days; the rest no term",
		OwnerDeletes: "an account is switched off, not deleted: its sessions end and its rows stay"},
	{Name: "spaces, organizations and roles", Owner: OwnerHub,
		Tables: []string{"spaces", "memberships", "orgs", "teams", "team_members", "role_grants", "invites", "invite_team",
			"invite_uses", "org_features", "org_changes", "org_signals", "limit_rules", "audit_events"},
		Keep:         "no term",
		OwnerDeletes: "a person who leaves a space loses the membership, roles, limits and access rows of that space; the audit trail stays"},
	{Name: "environment and catalog", Owner: OwnerHub,
		Tables: []string{"catalog_items", "env_entries", "env_groups", "env_presets", "env_person_group", "env_person_off"},
		Keep:   "no term", OwnerDeletes: "deleting an item, a group or a preset deletes its row"},
	{Name: "provider accounts", Owner: OwnerOutside,
		Tables:       []string{"accounts", "account_grants", "account_balances", "account_windows", "account_window_cycles"},
		Keep:         "no term; a balance or a window is the last one read",
		OwnerDeletes: "nothing is read back as deleted: the last reading stays"},
	{Name: "prices and rates", Owner: OwnerOutside, Tables: []string{"prices", "currency_rates"},
		Keep: "no term: a call is priced by the row in effect at its time", OwnerDeletes: "nothing"},
	{Name: "merge requests and efficiency", Owner: OwnerOutside,
		Tables: []string{"merged_mrs", "eff_source_facts", "eff_source_state", "eff_pulse"},
		Keep:   "no term", OwnerDeletes: "nothing: a fact collected once stays"},
	{Name: "servers at a provider", Owner: OwnerOutside, Tables: []string{"provider_servers", "provider_ops"},
		Keep: "no term", OwnerDeletes: "the hub's own operation removes a server at the provider and records it; the rows stay"},
	{Name: "automations", Owner: OwnerHub, Tables: []string{"automation_rules", "automation_runs"},
		Keep: "no term", OwnerDeletes: "deleting a rule deletes its runs"},
	{Name: "designs", Owner: OwnerMachine,
		Tables: []string{"design_projects", "design_members", "design_team_grants", "design_invites", "design_requests",
			"design_share_links", "design_share_views", "design_guest_comments", "design_access_log"},
		Keep:         "no term",
		OwnerDeletes: "the project is the machine's; the hub's rows of who may open it stay until the access is taken away in the hub"},
	{Name: "access to documents and resources", Owner: OwnerHub,
		Tables: []string{"resource_access", "resource_access_people", "resource_access_teams", "resource_access_requests",
			"resource_access_log", "resource_guest_links", "resource_guest_comments"},
		Keep: "no term", OwnerDeletes: "taking an access away deletes its row; the log stays"},
	{Name: "documents' comments", Owner: OwnerHub,
		Tables: []string{"doc_meta", "doc_threads", "doc_comments", "doc_comment_mentions", "doc_reads"},
		Keep:   "no term", OwnerDeletes: "nothing removes them"},
	{Name: "sites", Owner: OwnerHub,
		Tables: []string{"sites", "site_deployments", "site_blobs", "site_space_blobs", "site_pending", "site_tokens"},
		Keep:   "no term; an upload in flight until it is committed or expires", OwnerDeletes: "nothing removes a site's rows by age"},
	{Name: "password safe", Owner: OwnerHub, Tables: []string{"vault_entries", "vault_grants", "vault_audit"},
		Keep: "no term", OwnerDeletes: "deleting an entry deletes it and its grants; the audit stays"},
	{Name: "passes to machines and sites", Owner: OwnerHub, Tables: []string{"entry_tickets"},
		Keep: "a minute to trade, twelve hours as the note that the person opened the machine, then swept by time", OwnerDeletes: "nothing: a pass names a person and goes by time"},
	{Name: "the installation", Owner: OwnerHub, Tables: []string{"schema_migrations", "rollout_switches", "job_leases"},
		Keep: "no term", OwnerDeletes: "nothing: they describe the hub itself"},
}
