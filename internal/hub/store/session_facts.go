package store

import (
	"context"
	"strings"
)

// SessionFacts is what a machine tells of a session besides its sums: the project and the task keys (migration 0068).
type SessionFacts struct {
	Project  string
	TaskKeys []string
}

// PutSessionFacts keeps the facts of a session; a later record replaces the earlier one.
func (s Store) PutSessionFacts(ctx context.Context, ref SessionRef, f SessionFacts, at int64) error {
	_, err := s.q.Exec(ctx, `INSERT INTO session_facts (org_key, provider, session_key, project, task_keys, updated_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (org_key, provider, session_key) DO UPDATE SET project = excluded.project, task_keys = excluded.task_keys,
		updated_at = excluded.updated_at`,
		ref.OrgKey, ref.Provider, ref.SessionKey, f.Project, strings.Join(f.TaskKeys, ","), at)
	return err
}

// SessionFactsFor reads the facts of these sessions; a session no machine told anything of has no entry.
func (s Store) SessionFactsFor(ctx context.Context, refs []SessionRef) (map[SessionRef]SessionFacts, error) {
	out := map[SessionRef]SessionFacts{}
	byOrg := map[[2]string][]string{}
	for _, r := range refs {
		k := [2]string{r.OrgKey, r.Provider}
		byOrg[k] = append(byOrg[k], r.SessionKey)
	}
	for k, keys := range byOrg {
		for len(keys) > 0 {
			n := min(len(keys), chunk)
			part := keys[:n]
			keys = keys[n:]
			args := []any{k[0], k[1]}
			for _, key := range part {
				args = append(args, key)
			}
			rows, err := s.q.Query(ctx, `SELECT session_key, project, task_keys FROM session_facts WHERE org_key = ? AND provider = ? AND session_key IN (`+
				placeholders(len(part))+`)`, args...)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var key, project, tasks string
				if err := rows.Scan(&key, &project, &tasks); err != nil {
					rows.Close()
					return nil, err
				}
				f := SessionFacts{Project: project}
				if tasks != "" {
					f.TaskKeys = strings.Split(tasks, ",")
				}
				out[SessionRef{k[0], k[1], key}] = f
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// SubagentCosts sums the list price of the calls of these sessions' subagents, in millionths of a dollar, by parent.
func (s Store) SubagentCosts(ctx context.Context, refs []SessionRef) (map[SessionRef]int64, error) {
	out := map[SessionRef]int64{}
	byOrg := map[[2]string][]string{}
	for _, r := range refs {
		k := [2]string{r.OrgKey, r.Provider}
		byOrg[k] = append(byOrg[k], r.SessionKey)
	}
	for k, keys := range byOrg {
		for len(keys) > 0 {
			n := min(len(keys), chunk)
			part := keys[:n]
			keys = keys[n:]
			args := []any{k[0], k[1]}
			for _, key := range part {
				args = append(args, key)
			}
			rows, err := s.q.Query(ctx, `SELECT a.parent_key, COALESCE(SUM(c.list_cost_micros), 0)
				FROM usage_calls c JOIN agent_sessions a ON a.org_key = c.org_key AND a.provider = c.provider AND a.session_key = c.session_key
				WHERE a.org_key = ? AND a.provider = ? AND a.parent_key IN (`+placeholders(len(part))+`) GROUP BY a.parent_key`, args...)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var parent string
				var sum int64
				if err := rows.Scan(&parent, &sum); err != nil {
					rows.Close()
					return nil, err
				}
				out[SessionRef{k[0], k[1], parent}] = sum
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}
