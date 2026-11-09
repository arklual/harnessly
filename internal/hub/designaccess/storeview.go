package designaccess

import (
	"context"
	"encoding/json"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// StoreGrant is what a machine's copy of its space's design library may do with one recorded project: see it
// (its files come to the machine) and edit it (the machine's writes to it are taken).
type StoreGrant struct {
	See, Edit bool
	// Own: the machine's person owns the project; only then may the machine change its owner file.
	Own bool
}

// StoreView is the store's filter for a machine: per recorded project of its space, what the machine's person — the
// one its runtime runs for — may do. A project nobody recorded is not listed: it is open, as the access rule has it.
func (s *Service) StoreView(ctx context.Context, m store.Machine) (map[string]StoreGrant, error) {
	t, err := s.read(ctx, m)
	if err != nil {
		return nil, err
	}
	person := ""
	if u, err := t.st.UserByID(ctx, m.CreatedBy); err == nil {
		person = normEmail(u.Email)
	}
	projects, err := t.projects()
	if err != nil {
		return nil, err
	}
	out := make(map[string]StoreGrant, len(projects))
	for _, p := range projects {
		g := StoreGrant{}
		if person != "" {
			f, err := t.fold(p, person)
			if err != nil {
				return nil, err
			}
			g.See = f.Role != ""
			g.Edit = rank(f.Role) >= rank(roleEdit)
			g.Own = f.Role == roleOwner
		}
		out[p.ID] = g
	}
	return out, nil
}

// RecordFromStore records a project whose owner file reached the store before the hub had a row for it: the
// person the file names when they may open the machine that sent it, otherwise the machine's person, owns it; a
// personal file (v2, private) makes a personal project, an older or claimed one an open one, as the design events do.
// True when a row was made.
func (s *Service) RecordFromStore(ctx context.Context, m store.Machine, project string, ownerFile []byte) (bool, error) {
	if !projectIDRe.MatchString(project) {
		return false, nil
	}
	var f struct {
		V       float64 `json:"v"`
		Person  string  `json:"person"`
		Private bool    `json:"private"`
		Claimed bool    `json:"claimed"`
	}
	if err := json.Unmarshal(ownerFile, &f); err != nil {
		return false, nil
	}
	t, err := s.read(ctx, m)
	if err != nil {
		return false, err
	}
	if p, err := t.getProject(project, false); err != nil || p != nil {
		return false, err
	}
	who, err := s.eventPerson(ctx, m, f.Person)
	if err != nil || who == "" {
		return false, err
	}
	var done bool
	if f.V >= 2 && f.Private && !f.Claimed {
		done, err = s.registerCreated(ctx, m, project, who)
	} else {
		done, err = s.registerClaim(ctx, m, project, who)
	}
	if done {
		s.kickSpace(ctx, m)
	}
	return done, err
}
