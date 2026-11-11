package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"
)

// Layers of the environment (docs/hub/environment.md).
const (
	EnvLayerOrg    = "org"
	EnvLayerTenant = "tenant"
	EnvLayerTeam   = "team"
)

// Kinds of a catalog item.
const (
	CatalogSkill = "skill"
	CatalogMCP   = "mcp"
	CatalogRepo  = "repo"
	CatalogDoc   = "doc"
	// CatalogScript is a Linux script the machine runs; CatalogAccess the login of a forge's CLI.
	CatalogScript = "script"
	CatalogAccess = "access"
	// CatalogPlugin and CatalogClaudeSettings change the person's Claude Code (docs/hub/environment.md, «Recommended»).
	CatalogPlugin         = "plugin"
	CatalogClaudeSettings = "claude-settings"
	// CatalogApp is an app in a repository (docs/hub/environment.md, «Apps»).
	CatalogApp = "app"
	// CatalogPack is an environment pack in a repository (docs/hub/packs.md): tools, hooks, MCP and the safe's rules.
	CatalogPack = "pack"
)

// CatalogItem is something an environment can hold: a skill, an MCP server, a repository or a document. OrgID is ""
// for a built-in item every organization sees. Source is JSON without any secret.
type CatalogItem struct {
	ID         string
	OrgID      string
	Kind       string
	Name       string
	Hint       string
	Source     string
	CreatedAt  time.Time
	CreatedBy  string
	ArchivedAt *time.Time
	// OwnerLayer and OwnerScope name the layer that owns the item and may change it; "" for the organization.
	OwnerLayer string
	OwnerScope string
}

const catalogCols = `id, org_id, kind, name, hint, source, created_at, created_by, archived_at, owner_layer, owner_scope`

func scanCatalogItem(row interface{ Scan(...any) error }) (CatalogItem, error) {
	var c CatalogItem
	var org, by, ownerLayer, ownerScope sql.NullString
	var created int64
	var archived sql.NullInt64
	if err := row.Scan(&c.ID, &org, &c.Kind, &c.Name, &c.Hint, &c.Source, &created, &by, &archived, &ownerLayer, &ownerScope); err != nil {
		return CatalogItem{}, err
	}
	c.OrgID, c.CreatedBy, c.CreatedAt, c.ArchivedAt = org.String, by.String, fromMs(created), fromNullMs(archived)
	c.OwnerLayer, c.OwnerScope = ownerLayer.String, ownerScope.String
	return c, nil
}

// AddCatalogItem keeps a new item; c.ID is made when empty.
func (s Store) AddCatalogItem(ctx context.Context, c CatalogItem) (CatalogItem, error) {
	if c.ID == "" {
		c.ID = NewID()
	}
	if c.Source == "" {
		c.Source = "{}"
	}
	_, err := s.q.Exec(ctx, `INSERT INTO catalog_items (`+catalogCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, nullStr(c.OrgID), c.Kind, c.Name, c.Hint, c.Source, Ms(c.CreatedAt), nullStr(c.CreatedBy), nullMs(c.ArchivedAt),
		nullStr(c.OwnerLayer), nullStr(c.OwnerScope))
	return c, err
}

// CatalogItemByID is one item (archived or not), or ErrNotFound.
func (s Store) CatalogItemByID(ctx context.Context, id string) (CatalogItem, error) {
	return scanCatalogItem(s.q.QueryRow(ctx, `SELECT `+catalogCols+` FROM catalog_items WHERE id = ?`, id))
}

// CatalogItems are the built-in items and the organization's own, by kind and name. Archived ones are left out
// unless withArchived.
func (s Store) CatalogItems(ctx context.Context, orgID string, withArchived bool) ([]CatalogItem, error) {
	q := `SELECT ` + catalogCols + ` FROM catalog_items WHERE (org_id IS NULL OR org_id = ?)`
	if !withArchived {
		q += ` AND archived_at IS NULL`
	}
	rows, err := s.q.Query(ctx, q+` ORDER BY kind, name, id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CatalogItem
	for rows.Next() {
		c, err := scanCatalogItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateCatalogItem writes the name, hint, source, owner and archive time of c.
func (s Store) UpdateCatalogItem(ctx context.Context, c CatalogItem) error {
	_, err := s.q.Exec(ctx, `UPDATE catalog_items SET name = ?, hint = ?, source = ?, archived_at = ?, owner_layer = ?, owner_scope = ? WHERE id = ?`,
		c.Name, c.Hint, c.Source, nullMs(c.ArchivedAt), nullStr(c.OwnerLayer), nullStr(c.OwnerScope), c.ID)
	return err
}

// EnvEntry is what a layer puts into the environment of its people. Audience is JSON {"mode","teams"}.
type EnvEntry struct {
	ID        string
	OrgID     string
	Layer     string
	ScopeID   string
	ItemID    string
	Enabled   bool
	Audience  string
	Pinned    bool
	Rev       int64
	UpdatedAt time.Time
	UpdatedBy string
	// Position is the place among the layer's entries; scripts run in this order (1, 2, …; 0 until placed).
	Position int
	// GroupID is the group of the layer the entry is in ("" for none).
	GroupID string
}

const envEntryCols = `id, org_id, layer, scope_id, item_id, enabled, audience, pinned, rev, updated_at, updated_by, position, group_id`

func scanEnvEntry(row interface{ Scan(...any) error }) (EnvEntry, error) {
	var e EnvEntry
	var enabled, pinned int
	var at int64
	var by sql.NullString
	var position sql.NullInt64
	var group sql.NullString
	if err := row.Scan(&e.ID, &e.OrgID, &e.Layer, &e.ScopeID, &e.ItemID, &enabled, &e.Audience, &pinned, &e.Rev, &at, &by, &position, &group); err != nil {
		return EnvEntry{}, err
	}
	e.Position, e.GroupID = int(position.Int64), group.String
	e.Enabled, e.Pinned, e.UpdatedAt, e.UpdatedBy = enabled != 0, pinned != 0, fromMs(at), by.String
	return e, nil
}

func envInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// AddEnvEntry keeps a new entry; e.ID is made when empty. False when the layer has the item already.
func (s Store) AddEnvEntry(ctx context.Context, e EnvEntry) (EnvEntry, bool, error) {
	if e.ID == "" {
		e.ID = NewID()
	}
	if e.Rev == 0 {
		e.Rev = 1
	}
	if e.Audience == "" {
		e.Audience = `{"mode":"tenant","teams":[]}`
	}
	res, err := s.q.Exec(ctx, `INSERT INTO env_entries (`+envEntryCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (layer, scope_id, item_id) DO NOTHING`,
		e.ID, e.OrgID, e.Layer, e.ScopeID, e.ItemID, envInt(e.Enabled), e.Audience, envInt(e.Pinned), e.Rev, Ms(e.UpdatedAt), nullStr(e.UpdatedBy), e.Position, nullStr(e.GroupID))
	if err != nil {
		return EnvEntry{}, false, err
	}
	n, _ := res.RowsAffected()
	return e, n > 0, nil
}

// EnvEntryByID is one entry, or ErrNotFound.
func (s Store) EnvEntryByID(ctx context.Context, id string) (EnvEntry, error) {
	return scanEnvEntry(s.q.QueryRow(ctx, `SELECT `+envEntryCols+` FROM env_entries WHERE id = ?`, id))
}

// EnvEntryByKey is the entry of a layer for an item, or ErrNotFound.
func (s Store) EnvEntryByKey(ctx context.Context, layer, scopeID, itemID string) (EnvEntry, error) {
	return scanEnvEntry(s.q.QueryRow(ctx, `SELECT `+envEntryCols+` FROM env_entries WHERE layer = ? AND scope_id = ? AND item_id = ?`,
		layer, scopeID, itemID))
}

// EnvEntriesOfOrg are all the entries of an organization, by layer, scope and item.
func (s Store) EnvEntriesOfOrg(ctx context.Context, orgID string) ([]EnvEntry, error) {
	rows, err := s.q.Query(ctx, `SELECT `+envEntryCols+` FROM env_entries WHERE org_id = ? ORDER BY layer, scope_id, item_id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EnvEntry
	for rows.Next() {
		e, err := scanEnvEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// UpdateEnvEntry writes enabled, audience, pinned, position, group, rev, updated_at and updated_by of e.
func (s Store) UpdateEnvEntry(ctx context.Context, e EnvEntry) error {
	_, err := s.q.Exec(ctx, `UPDATE env_entries SET enabled = ?, audience = ?, pinned = ?, position = ?, group_id = ?, rev = ?, updated_at = ?, updated_by = ? WHERE id = ?`,
		envInt(e.Enabled), e.Audience, envInt(e.Pinned), e.Position, nullStr(e.GroupID), e.Rev, Ms(e.UpdatedAt), nullStr(e.UpdatedBy), e.ID)
	return err
}

// DeleteEnvEntry removes an entry.
func (s Store) DeleteEnvEntry(ctx context.Context, id string) error {
	_, err := s.q.Exec(ctx, `DELETE FROM env_entries WHERE id = ?`, id)
	return err
}

// EnvPersonOffs are the items a person switched off in a tenant, as a set of item ids.
func (s Store) EnvPersonOffs(ctx context.Context, userID, scope string) (map[string]bool, error) {
	rows, err := s.q.Query(ctx, `SELECT item_id FROM env_person_off WHERE user_id = ? AND scope = ?`, userID, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// EnvOffCountByItem counts, for every item, the people who switched it off in a tenant.
func (s Store) EnvOffCountByItem(ctx context.Context, scope string) (map[string]int, error) {
	rows, err := s.q.Query(ctx, `SELECT item_id, COUNT(*) FROM env_person_off WHERE scope = ? GROUP BY item_id`, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// SetEnvPersonOff switches an item off (off) or back on for a person in a tenant.
func (s Store) SetEnvPersonOff(ctx context.Context, userID, scope, itemID string, off bool, at time.Time) error {
	if !off {
		_, err := s.q.Exec(ctx, `DELETE FROM env_person_off WHERE user_id = ? AND scope = ? AND item_id = ?`, userID, scope, itemID)
		return err
	}
	_, err := s.q.Exec(ctx, `INSERT INTO env_person_off (user_id, scope, item_id, at) VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id, scope, item_id) DO NOTHING`, userID, scope, itemID, Ms(at))
	return err
}

// EnvState is what is known of the environment of one machine: what it reported and what the hub last sent.
type EnvState struct {
	MachineID string
	Rev       int64  // reported
	Hash      string // reported
	Applied   string // JSON [{itemId,status,message}]
	At        time.Time
	SentRev   int64
	SentHash  string
	SentAt    time.Time
}

const envStateCols = `machine_id, rev, hash, applied, at, sent_rev, sent_hash, sent_at`

// EnvStateByMachine is the state of a machine, or ErrNotFound.
func (s Store) EnvStateByMachine(ctx context.Context, machineID string) (EnvState, error) {
	var e EnvState
	var at, sentAt int64
	err := s.q.QueryRow(ctx, `SELECT `+envStateCols+` FROM env_state WHERE machine_id = ?`, machineID).
		Scan(&e.MachineID, &e.Rev, &e.Hash, &e.Applied, &at, &e.SentRev, &e.SentHash, &sentAt)
	if err != nil {
		return EnvState{}, err
	}
	e.At, e.SentAt = fromMs(at), fromMs(sentAt)
	return e, nil
}

// EnvStatesBySpace are the states of the machines of a space, by machine id: one query for all of them.
func (s Store) EnvStatesBySpace(ctx context.Context, spaceID string) (map[string]EnvState, error) {
	rows, err := s.q.Query(ctx, `SELECT e.machine_id, e.rev, e.hash, e.applied, e.at, e.sent_rev, e.sent_hash, e.sent_at
		FROM env_state e JOIN machines m ON m.id = e.machine_id WHERE m.space_id = ?`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]EnvState{}
	for rows.Next() {
		var e EnvState
		var at, sentAt int64
		if err := rows.Scan(&e.MachineID, &e.Rev, &e.Hash, &e.Applied, &at, &e.SentRev, &e.SentHash, &sentAt); err != nil {
			return nil, err
		}
		e.At, e.SentAt = fromMs(at), fromMs(sentAt)
		out[e.MachineID] = e
	}
	return out, rows.Err()
}

// SaveEnvReported keeps what a machine reported (rev, hash, applied).
func (s Store) SaveEnvReported(ctx context.Context, machineID string, rev int64, hash, applied string, at time.Time) error {
	if applied == "" {
		applied = "[]"
	}
	_, err := s.q.Exec(ctx, `INSERT INTO env_state (machine_id, rev, hash, applied, at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (machine_id) DO UPDATE SET rev = excluded.rev, hash = excluded.hash, applied = excluded.applied, at = excluded.at`,
		machineID, rev, hash, applied, Ms(at))
	return err
}

// SaveEnvSent keeps what the hub sent to a machine last.
func (s Store) SaveEnvSent(ctx context.Context, machineID string, rev int64, hash string, at time.Time) error {
	_, err := s.q.Exec(ctx, `INSERT INTO env_state (machine_id, sent_rev, sent_hash, sent_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (machine_id) DO UPDATE SET sent_rev = excluded.sent_rev, sent_hash = excluded.sent_hash, sent_at = excluded.sent_at`,
		machineID, rev, hash, Ms(at))
	return err
}

// EnvRun is one run of the setup on a machine, as the hub keeps it. Steps is JSON (hubproto.EnvStep with the logs).
type EnvRun struct {
	ID         string
	OrgID      string
	MachineID  string
	UserID     string // the machine's owner
	SpaceID    string
	TeamID     string
	Mode       string
	Status     string
	Hash       string
	ByName     string
	StartedAt  time.Time
	FinishedAt time.Time // zero while running
	Steps      string
	UpdatedAt  time.Time
}

const envRunCols = `id, org_id, machine_id, user_id, space_id, team_id, mode, status, hash, by_name, started_at, finished_at, steps, updated_at`

func scanEnvRun(row interface{ Scan(...any) error }) (EnvRun, error) {
	var r EnvRun
	var started, finished, updated int64
	if err := row.Scan(&r.ID, &r.OrgID, &r.MachineID, &r.UserID, &r.SpaceID, &r.TeamID, &r.Mode, &r.Status, &r.Hash, &r.ByName,
		&started, &finished, &r.Steps, &updated); err != nil {
		return EnvRun{}, err
	}
	r.StartedAt, r.UpdatedAt = fromMs(started), fromMs(updated)
	if finished > 0 {
		r.FinishedAt = fromMs(finished)
	}
	return r, nil
}

// UpsertEnvRun keeps a run; a later frame of the same id replaces the earlier one (who it belongs to stays). A frame
// whose id is held by a run of another machine or organization changes nothing and answers false.
func (s Store) UpsertEnvRun(ctx context.Context, r EnvRun) (bool, error) {
	if r.Steps == "" {
		r.Steps = "[]"
	}
	var finished int64
	if !r.FinishedAt.IsZero() {
		finished = Ms(r.FinishedAt)
	}
	res, err := s.q.Exec(ctx, `INSERT INTO env_runs (`+envRunCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET mode = excluded.mode, status = excluded.status, hash = excluded.hash, by_name = excluded.by_name,
		started_at = excluded.started_at, finished_at = excluded.finished_at, steps = excluded.steps, updated_at = excluded.updated_at
		WHERE env_runs.org_id = excluded.org_id AND env_runs.machine_id = excluded.machine_id`,
		r.ID, r.OrgID, r.MachineID, r.UserID, r.SpaceID, r.TeamID, r.Mode, r.Status, r.Hash, r.ByName, Ms(r.StartedAt), finished, r.Steps, Ms(r.UpdatedAt))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// EnvRunQuery picks runs: of an organization, of some machines (empty: any of the organization), since a time, only
// those that went wrong (error, or waiting for the person), newest first.
type EnvRunQuery struct {
	OrgID      string
	MachineIDs []string
	SpaceID    string // only the runs of machines of this space ("": any)
	TeamID     string // only the runs of machines of this team ("": any)
	Since      time.Time
	ErrorsOnly bool
	Limit      int
}

// EnvRunsOf are the runs the query picks. A query for machines with no ids at all (MachineIDs non-nil and empty) picks
// nothing.
func (s Store) EnvRunsOf(ctx context.Context, q EnvRunQuery) ([]EnvRun, error) {
	if q.MachineIDs != nil && len(q.MachineIDs) == 0 {
		return nil, nil
	}
	where := `org_id = ? AND started_at >= ?`
	args := []any{q.OrgID, Ms(q.Since)}
	if q.MachineIDs != nil {
		where += ` AND machine_id IN (` + strings.TrimSuffix(strings.Repeat("?, ", len(q.MachineIDs)), ", ") + `)`
		for _, id := range q.MachineIDs {
			args = append(args, id)
		}
	}
	if q.SpaceID != "" {
		where += ` AND space_id = ?`
		args = append(args, q.SpaceID)
	}
	if q.TeamID != "" {
		where += ` AND team_id = ?`
		args = append(args, q.TeamID)
	}
	if q.ErrorsOnly {
		where += ` AND status IN ('error', 'pending')`
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.q.Query(ctx, `SELECT `+envRunCols+` FROM env_runs WHERE `+where+` ORDER BY started_at DESC, id LIMIT `+strconv.Itoa(limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EnvRun
	for rows.Next() {
		r, err := scanEnvRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// StuckEnvRuns are the runs still «running» that nothing has updated since the time given.
func (s Store) StuckEnvRuns(ctx context.Context, updatedBefore time.Time) ([]EnvRun, error) {
	rows, err := s.q.Query(ctx, `SELECT `+envRunCols+` FROM env_runs WHERE status = 'running' AND updated_at < ? ORDER BY started_at, id LIMIT 500`, Ms(updatedBefore))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EnvRun
	for rows.Next() {
		r, err := scanEnvRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// EnvRunByID is one run of an organization, or ErrNotFound: a run id of another organization is not found.
func (s Store) EnvRunByID(ctx context.Context, orgID, id string) (EnvRun, error) {
	return scanEnvRun(s.q.QueryRow(ctx, `SELECT `+envRunCols+` FROM env_runs WHERE org_id = ? AND id = ?`, orgID, id))
}

// DeleteEnvRunsBefore removes the runs that started before t and returns how many.
func (s Store) DeleteEnvRunsBefore(ctx context.Context, t time.Time) (int64, error) {
	res, err := s.q.Exec(ctx, `DELETE FROM env_runs WHERE started_at < ?`, Ms(t))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ToolUsage is the calls of one tool of one session on one UTC day.
type ToolUsage struct {
	OrgKey     string
	SpaceID    string
	TeamID     string
	UserID     string
	MachineID  string
	SessionKey string
	ItemID     string
	ToolKind   string
	ToolName   string
	Day        string // YYYY-MM-DD, UTC
	Calls      int64
	Errors     int64
	LastAt     int64 // epoch ms the machine wrote the fact, the best we know of when the tool was called last
}

// UpsertToolUsage keeps the totals of a tool of a session on a day; a repeated fact replaces the row.
func (s Store) UpsertToolUsage(ctx context.Context, u ToolUsage) error {
	_, err := s.q.Exec(ctx, `INSERT INTO tool_usage_day (org_key, space_id, team_id, user_id, machine_id, session_key, item_id, tool_kind,
		tool_name, day, calls, errors, last_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (machine_id, session_key, day, tool_kind, tool_name) DO UPDATE SET org_key = excluded.org_key,
		space_id = excluded.space_id, team_id = excluded.team_id, user_id = excluded.user_id, item_id = excluded.item_id,
		calls = excluded.calls, errors = excluded.errors, last_at = excluded.last_at`,
		u.OrgKey, u.SpaceID, u.TeamID, u.UserID, u.MachineID, u.SessionKey, u.ItemID, u.ToolKind, u.ToolName, u.Day, u.Calls, u.Errors, u.LastAt)
	return err
}

// ToolUsageRow is the calls of a tool on a day, summed over sessions and machines.
type ToolUsageRow struct {
	SpaceID  string
	TeamID   string
	UserID   string
	ItemID   string
	ToolKind string
	ToolName string
	Day      string
	Calls    int64
	Errors   int64
	LastAt   int64
}

// ToolUsageOf are the calls of an organization from day from (inclusive) on, by space, team, user, tool and day.
func (s Store) ToolUsageOf(ctx context.Context, orgKey, from string) ([]ToolUsageRow, error) {
	rows, err := s.q.Query(ctx, `SELECT space_id, team_id, user_id, item_id, tool_kind, tool_name, day, SUM(calls), SUM(errors), MAX(last_at)
		FROM tool_usage_day WHERE org_key = ? AND day >= ?
		GROUP BY space_id, team_id, user_id, item_id, tool_kind, tool_name, day ORDER BY day, tool_kind, tool_name`, orgKey, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ToolUsageRow
	for rows.Next() {
		var r ToolUsageRow
		if err := rows.Scan(&r.SpaceID, &r.TeamID, &r.UserID, &r.ItemID, &r.ToolKind, &r.ToolName, &r.Day, &r.Calls, &r.Errors, &r.LastAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ItemForTool is the catalog item a tool call belongs to: a skill by its name (a plugin-qualified «plugin:skill» also
// matches the bare name), an MCP server by its name (a plugin's or a connector's server also by its short name, see
// MCPShortName). Names are compared without regard to case, an underscore standing for a space. The organization's own
// item beats a built-in one, a live item beats an archived one. "" when the call is of no item: Bash and the built-in
// tools have none.
func ItemForTool(items []CatalogItem, toolKind, toolName string) string {
	var kind string
	switch toolKind {
	case CatalogSkill:
		kind = CatalogSkill
	case CatalogMCP:
		kind = CatalogMCP
	default:
		return ""
	}
	names := []string{toolName}
	if i := strings.LastIndex(toolName, ":"); kind == CatalogSkill && i >= 0 && i+1 < len(toolName) {
		names = append(names, toolName[i+1:])
	}
	if short := MCPShortName(toolName); kind == CatalogMCP && short != toolName {
		names = append(names, short)
	}
	best, bestRank := "", -1
	for _, c := range items {
		if c.Kind != kind {
			continue
		}
		for i, n := range names {
			if !sameToolName(c.Name, n) {
				continue
			}
			rank := 4 - 2*i // the full name beats the bare one
			if c.OrgID != "" {
				rank++
			}
			if c.ArchivedAt != nil {
				rank -= 8
			}
			rank += 8
			if rank > bestRank {
				best, bestRank = c.ID, rank
			}
		}
	}
	return best
}

// MCPShortName is the name of an MCP server without the prefix Claude Code gives a plugin's server ("plugin_<plugin>_")
// or a claude.ai connector ("claude_ai_"), underscores as spaces: "plugin_tools_grafana" is "grafana",
// "claude_ai_Claude_Docs" is "Claude Docs". Any other name is as it is.
func MCPShortName(name string) string {
	rest := ""
	if r, ok := strings.CutPrefix(name, "plugin_"); ok {
		if _, after, found := strings.Cut(r, "_"); found {
			rest = after
		}
	} else if r, ok := strings.CutPrefix(name, "claude_ai_"); ok {
		rest = r
	}
	if rest == "" {
		return name
	}
	return strings.ReplaceAll(rest, "_", " ")
}

func sameToolName(a, b string) bool {
	return strings.EqualFold(strings.ReplaceAll(a, "_", " "), strings.ReplaceAll(b, "_", " "))
}
