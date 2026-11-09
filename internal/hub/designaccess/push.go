package designaccess

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The snapshot the runtime enforces (internal/design/accessfile.go of the runtime): the machine person's view at the top,
// every member of the space under people. A person missing there gets nothing on a known project.

type snapGrant struct {
	Role      string     `json:"role"`
	Owner     string     `json:"owner"`
	OwnerName string     `json:"ownerName"`
	Until     *time.Time `json:"until"`
}

type snapPerson struct {
	Projects map[string]snapGrant `json:"projects"`
	Team     map[string]string    `json:"team"`
}

type snapshot struct {
	Enforce  bool                  `json:"enforce"`
	At       time.Time             `json:"at"`
	Projects map[string]snapGrant  `json:"projects"`
	Team     map[string]string     `json:"team"`
	People   map[string]snapPerson `json:"people"`
}

const maxSnapPeople = 500

func (s *Service) build(ctx context.Context, m store.Machine) (snapshot, error) {
	t, err := s.read(ctx, m)
	if err != nil {
		return snapshot{}, err
	}
	members, err := t.st.Members(ctx, m.SpaceID)
	if err != nil {
		return snapshot{}, err
	}
	machinePerson := ""
	if u, err := t.st.UserByID(ctx, m.CreatedBy); err == nil {
		machinePerson = normEmail(u.Email)
	}
	emails := []string{}
	seen := map[string]bool{}
	add := func(e string) {
		if e = normEmail(e); e != "" && !seen[e] && len(emails) < maxSnapPeople {
			seen[e] = true
			emails = append(emails, e)
		}
	}
	add(machinePerson)
	for _, mb := range members {
		add(mb.Email)
	}
	people := map[string]snapPerson{}
	for _, e := range emails {
		people[e] = snapPerson{Projects: map[string]snapGrant{}, Team: map[string]string{}}
	}

	projects, err := t.projects()
	if err != nil {
		return snapshot{}, err
	}
	for _, p := range projects {
		list, err := t.members(p.ID)
		if err != nil {
			return snapshot{}, err
		}
		byEmail := map[string]*member{}
		for i := range list {
			byEmail[list[i].Email] = &list[i]
		}
		grants, err := t.teamGrants(p.ID)
		if err != nil {
			return snapshot{}, err
		}
		org := t.w.orgOf(p.HomeTeam)
		for _, e := range emails {
			person, err := t.w.person(e)
			if err != nil {
				return snapshot{}, err
			}
			f := foldAccess(p, byEmail[e], grants, person, org, t.now)
			sec := people[e]
			if f.Role == "" {
				sec.Team[p.ID] = levelNone
				continue
			}
			sec.Projects[p.ID] = snapGrant{Role: f.Role, Owner: p.Owner, OwnerName: p.OwnerName, Until: f.Until}
			_, inHome := person.teams[p.HomeTeam]
			if p.HomeTeam == "" || inHome {
				sec.Team[p.ID] = p.TeamLevel
			} else {
				sec.Team[p.ID] = levelNone
			}
		}
	}
	snap := snapshot{Enforce: true, Projects: map[string]snapGrant{}, Team: map[string]string{}, People: people}
	if top, ok := people[machinePerson]; ok {
		snap.Projects, snap.Team = top.Projects, top.Team
	} else {
		for _, p := range projects {
			snap.Team[p.ID] = levelNone
		}
	}
	return snap, nil
}

// projects are the rows that count on the machine: its own, and with a shared library those of every machine of its
// space (one row per project, the one key picks).
func (t *txn) projects() ([]project, error) {
	q, args := `SELECT machine_id, `+projectCols+` FROM design_projects WHERE machine_id = ? ORDER BY project_id`, []any{t.m.ID}
	if t.shared && t.m.SpaceID != "" {
		q = `SELECT p.machine_id, ` + prefixed(projectCols, "p.") + ` FROM design_projects p JOIN machines m ON m.id = p.machine_id
			WHERE m.space_id = ? AND m.revoked_at IS NULL ORDER BY p.project_id, p.updated_at DESC, p.machine_id`
		args = []any{t.m.SpaceID}
	}
	rows, err := t.q.Query(t.ctx, q, args...)
	if err != nil {
		return nil, err
	}
	type row struct {
		machine string
		p       project
	}
	var all []row
	for rows.Next() {
		var r row
		var owner, home sql.NullString
		var share, legacy int
		if err := rows.Scan(&r.machine, &r.p.ID, &owner, &r.p.OwnerName, &home, &r.p.TeamLevel, &share, &legacy); err != nil {
			rows.Close()
			return nil, err
		}
		r.p.Owner, r.p.HomeTeam, r.p.EditorsCanShare, r.p.Legacy = owner.String, home.String, share != 0, legacy != 0
		all = append(all, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []project
	for _, r := range all {
		if len(out) > 0 && out[len(out)-1].ID == r.p.ID {
			continue
		}
		// The machine's own row wins; otherwise the newest, as key picks it.
		if home := t.key(r.p.ID); home != r.machine {
			continue
		}
		out = append(out, r.p)
	}
	return out, nil
}

func prefixed(cols, prefix string) string {
	parts := strings.Split(cols, ", ")
	for i, c := range parts {
		parts[i] = prefix + c
	}
	return strings.Join(parts, ", ")
}

// push sends the machine its snapshot; a machine offline gets it when its link comes back.
func (s *Service) push(ctx context.Context, id string) {
	s.mu.Lock()
	on := s.serving[id]
	s.mu.Unlock()
	if !on || s.reg.Get(id) == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, pushTimeout)
	defer cancel()
	m, err := s.db.S().MachineByID(ctx, id)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.log.Warn("design snapshot: machine not read", "machine", id, "error", err)
		}
		return
	}
	snap, err := s.build(ctx, m)
	if err != nil {
		s.log.Warn("design snapshot not built", "machine", id, "error", err)
		return
	}
	s.mu.Lock()
	at := s.now().UTC()
	if !at.After(s.lastAt) {
		at = s.lastAt.Add(time.Millisecond)
	}
	s.lastAt = at
	s.mu.Unlock()
	snap.At = at
	if _, err := s.reg.Do(ctx, id, hubproto.OpDesignAccess, snap, pushTimeout); err != nil {
		s.log.Warn("design snapshot not delivered", "machine", id, "error", err)
	}
}
