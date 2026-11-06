package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// SurvivalDays is how long a merged request must stay without a revert to count as survived (KPI «доживаемость MR»).
const SurvivalDays = 14

const survivalMs = SurvivalDays * 24 * 3600 * 1000

// mergedInfo is what survival needs of a merged request.
type mergedInfo struct {
	mergedAt int64
	title    string
	project  string
}

// revertedTitle is the title a revert request names: `Revert "<title>"`, as GitLab writes it; "" for any other title.
func revertedTitle(title string) string {
	const pre = `Revert "`
	if !strings.HasPrefix(title, pre) || !strings.HasSuffix(title, `"`) || len(title) <= len(pre)+1 {
		return ""
	}
	return title[len(pre) : len(title)-1]
}

// survived says whether a merged request has been merged for SurvivalDays by now (mature) and no revert of it, in the
// same project, was merged within that time (survived).
func survived(m mergedInfo, reverts map[string][]int64, now int64) (mature, ok bool) {
	if m.mergedAt+survivalMs > now {
		return false, false
	}
	for _, at := range reverts[m.project+"\x00"+m.title] {
		if at > m.mergedAt && at <= m.mergedAt+survivalMs {
			return true, false
		}
	}
	return true, true
}

// mergedReverts are the merged reverts by project and the title they revert, with the times they were merged.
func (s Store) mergedReverts(ctx context.Context) (map[string][]int64, error) {
	rows, err := s.q.Query(ctx, `SELECT project, title, merged_at FROM merged_mrs WHERE title LIKE 'Revert "%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]int64{}
	for rows.Next() {
		var project, title string
		var at int64
		if err := rows.Scan(&project, &title, &at); err != nil {
			return nil, err
		}
		if t := revertedTitle(title); t != "" {
			out[project+"\x00"+t] = append(out[project+"\x00"+t], at)
		}
	}
	return out, rows.Err()
}

// MaxSessionMRs is how many merge requests one session keeps.
const MaxSessionMRs = 200

// MergedMR is a merged request as the source reports it. Times are Unix milliseconds.
type MergedMR struct {
	Key       string
	MergedAt  int64
	CreatedAt int64
	Title     string
	Author    string
	Project   string
	WebURL    string
}

// ReplaceSessionMRs makes keys the set of merge requests of a session; owner is the session they count for (the main
// session of a subagent, the session itself otherwise).
func (s Store) ReplaceSessionMRs(ctx context.Context, orgKey, provider, sessionKey, owner string, keys []string) error {
	if _, err := s.q.Exec(ctx, `DELETE FROM agent_session_mrs WHERE org_key = ? AND provider = ? AND session_key = ?`, orgKey, provider, sessionKey); err != nil {
		return err
	}
	if len(keys) > MaxSessionMRs {
		keys = keys[:MaxSessionMRs]
	}
	for _, k := range keys {
		if _, err := s.q.Exec(ctx, `INSERT INTO agent_session_mrs (org_key, provider, session_key, owner_key, mr_key) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (org_key, provider, session_key, mr_key) DO NOTHING`, orgKey, provider, sessionKey, owner, k); err != nil {
			return err
		}
	}
	return nil
}

// UpsertMergedMRs stores merged requests, replacing the ones it has.
func (s Store) UpsertMergedMRs(ctx context.Context, mrs []MergedMR) error {
	for _, m := range mrs {
		if _, err := s.q.Exec(ctx, `INSERT INTO merged_mrs (mr_key, merged_at, created_at, title, author, project, web_url) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (mr_key) DO UPDATE SET merged_at = excluded.merged_at, created_at = excluded.created_at, title = excluded.title,
			author = excluded.author, project = excluded.project, web_url = excluded.web_url`,
			m.Key, m.MergedAt, m.CreatedAt, m.Title, m.Author, m.Project, m.WebURL); err != nil {
			return err
		}
	}
	return nil
}

// NewestMergedAt is the latest merge time stored, 0 when there is none.
func (s Store) NewestMergedAt(ctx context.Context) (int64, error) {
	var v *int64
	if err := s.q.QueryRow(ctx, `SELECT MAX(merged_at) FROM merged_mrs`).Scan(&v); err != nil {
		return 0, err
	}
	if v == nil {
		return 0, nil
	}
	return *v, nil
}

const mrChunk = chunk

// withMergedMRs fills mrsMerged and firstMrMs of the top-level sessions from the merged requests the hub knows. A merged
// request counts once, for the earliest-started main session that mentioned it. Nothing changes while the hub has no
// merged request (no source is polled): then what machines sent stays.
func (s Store) withMergedMRs(ctx context.Context, in []AgentSession) ([]AgentSession, error) {
	if len(in) == 0 {
		return in, nil
	}
	var one int
	if err := s.q.QueryRow(ctx, `SELECT 1 FROM merged_mrs LIMIT 1`).Scan(&one); err != nil {
		if IsNotFound(err) {
			return in, nil
		}
		return nil, err
	}
	type sk struct{ org, provider, key string }
	reverts, err := s.mergedReverts(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	info := map[string]mergedInfo{}
	// The requests of each session, merged ones only.
	byOrg := map[string][]int{}
	for i, x := range in {
		if x.ParentKey == "" {
			byOrg[x.OrgKey] = append(byOrg[x.OrgKey], i)
		}
	}
	for org, idx := range byOrg {
		mine := map[sk]map[string]int64{} // session -> mr key -> created_at
		keys := map[string]bool{}
		for from := 0; from < len(idx); from += mrChunk {
			part := idx[from:min(from+mrChunk, len(idx))]
			ph, args := placeholders(len(part)), []any{org}
			for _, i := range part {
				args = append(args, in[i].SessionKey)
			}
			rows, err := s.q.Query(ctx, `SELECT m.provider, m.owner_key, m.mr_key, g.created_at, g.merged_at, g.title, g.project FROM agent_session_mrs m
				JOIN merged_mrs g ON g.mr_key = m.mr_key WHERE m.org_key = ? AND m.owner_key IN (`+ph+`)`, args...)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var p, o, k string
				var c, at int64
				var title, project string
				if err := rows.Scan(&p, &o, &k, &c, &at, &title, &project); err != nil {
					rows.Close()
					return nil, err
				}
				id := sk{org, p, o}
				if mine[id] == nil {
					mine[id] = map[string]int64{}
				}
				mine[id][k] = c
				keys[k] = true
				info[k] = mergedInfo{mergedAt: at, title: title, project: project}
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return nil, err
			}
			rows.Close()
		}
		// Who owns each request: the earliest-started main session of the organization that mentioned it.
		winner := map[string]sk{}
		started := map[string]int64{}
		list := make([]string, 0, len(keys))
		for k := range keys {
			list = append(list, k)
		}
		for from := 0; from < len(list); from += mrChunk {
			part := list[from:min(from+mrChunk, len(list))]
			ph, args := placeholders(len(part)), []any{org}
			for _, k := range part {
				args = append(args, k)
			}
			rows, err := s.q.Query(ctx, `SELECT DISTINCT m.mr_key, a.provider, a.session_key, a.started_at FROM agent_session_mrs m
				JOIN agent_sessions a ON a.org_key = m.org_key AND a.provider = m.provider AND a.session_key = m.owner_key
				WHERE m.org_key = ? AND a.parent_key = '' AND m.mr_key IN (`+ph+`)`, args...)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var k, p, sess string
				var at int64
				if err := rows.Scan(&k, &p, &sess, &at); err != nil {
					rows.Close()
					return nil, err
				}
				cur, ok := winner[k]
				if !ok || at < started[k] || (at == started[k] && (p < cur.provider || (p == cur.provider && sess < cur.key))) {
					winner[k], started[k] = sk{org, p, sess}, at
				}
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return nil, err
			}
			rows.Close()
		}
		for _, i := range idx {
			x := in[i]
			id := sk{org, x.Provider, x.SessionKey}
			var merged, mature, ok float64
			first := int64(-1)
			for k, created := range mine[id] {
				if winner[k] == id {
					merged++
					if m, alive := survived(info[k], reverts, now); m {
						mature++
						if alive {
							ok++
						}
					}
				}
				if created >= x.StartedAt && (first < 0 || created-x.StartedAt < first) {
					first = created - x.StartedAt
				}
			}
			in[i].Metrics = setMRMetrics(x.Metrics, merged, first, mature, ok)
		}
	}
	return in, nil
}

// setMRMetrics sets mrsMerged and firstMrMs (dropped when negative) in a metrics object, leaving it as it is when they
// already say so.
func setMRMetrics(raw string, merged float64, first int64, mature, ok float64) string {
	m := map[string]any{}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			return raw
		}
	}
	oldMerged, _ := m["mrsMerged"].(float64)
	oldFirst, hadFirst := m["firstMrMs"].(float64)
	oldMature, _ := m["mrsMature"].(float64)
	oldOK, _ := m["mrsSurvived"].(float64)
	if oldMerged == merged && oldMature == mature && oldOK == ok && ((first < 0 && !hadFirst) || (first >= 0 && hadFirst && int64(oldFirst) == first)) {
		return raw
	}
	m["mrsMerged"] = merged
	m["mrsMature"], m["mrsSurvived"] = mature, ok
	if first >= 0 {
		m["firstMrMs"] = first
	} else {
		delete(m, "firstMrMs")
	}
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return string(out)
}
