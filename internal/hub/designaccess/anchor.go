package designaccess

import (
	"context"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

var workspaceInvite = regexp.MustCompile(`^/api/[A-Za-z0-9_-]+/design/access/invites/([A-Za-z0-9_-]{43})/accept$`)

// hubRoute is the path (after /agents) of what the hub answers itself: Sinopia's access, store and people routes. They
// name a space and a project, not a machine.
var hubRoute = regexp.MustCompile(`^/api/[A-Za-z0-9_-]+(?:/design/(?:access|store)/([a-z0-9][a-z0-9-]{0,62})(?:/.*)?|/design/store-library(?:/changes)?|/design/store-(?:cover|records-check|work-machine)|/docs/people|/access/(?:design(?:/.*)?|kit(?:/.*)?|me|directory))$`)

// HubRoute says whether rest is a route the hub answers itself, and the project it names ("" when it names none).
func HubRoute(rest string) (project string, ok bool) {
	m := hubRoute.FindStringSubmatch(rest)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// AnchorMachines are, for a hub route rest, the machines whose rows hold the project among the machines of the spaces,
// the most recently changed first; nil when none. A hub route needs a machine only to read the rows from (store.go:
// key), so one that holds the project answers, online or not; the proxy takes the first the person may open (admit),
// since a teammate's own machine is closed to others by the owner's rule (own-work) even when the project is shared.
func (s *Service) AnchorMachines(ctx context.Context, spaceIDs []string, rest string) []string {
	project, ok := HubRoute(rest)
	if !ok || project == "" || len(spaceIDs) == 0 {
		return nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(spaceIDs)), ",")
	args := []any{project}
	for _, id := range spaceIDs {
		args = append(args, id)
	}
	query := `SELECT p.machine_id FROM design_projects p JOIN machines m ON m.id = p.machine_id
  WHERE p.project_id = ? AND m.space_id IN (` + marks + `) AND m.revoked_at IS NULL
  ORDER BY p.updated_at DESC, p.machine_id LIMIT 8`
	if s.shared {
		// The project owner may have a private machine. Offer other machines in the same
		// space after the machine that holds the row; the proxy applies admission to each.
		query = `SELECT candidate.id FROM design_projects p JOIN machines holder ON holder.id=p.machine_id
            JOIN machines candidate ON candidate.space_id=holder.space_id
            WHERE p.project_id=? AND holder.space_id IN (` + marks + `) AND holder.revoked_at IS NULL AND candidate.revoked_at IS NULL
            GROUP BY candidate.id ORDER BY MAX(p.updated_at) DESC, MAX(CASE WHEN candidate.id=p.machine_id THEN 1 ELSE 0 END) DESC, candidate.id`
	}
	if match := workspaceInvite.FindStringSubmatch(rest); match != nil {
		args[0] = store.HashToken(match[1])
		join := "candidate.id = owner.id"
		if s.shared {
			join = "candidate.space_id = owner.space_id"
		}
		query = `SELECT candidate.id FROM design_invites i JOIN machines owner ON owner.id=i.machine_id JOIN machines candidate ON ` + join + `
      WHERE i.token_hash=? AND owner.space_id IN (` + marks + `) AND owner.revoked_at IS NULL AND candidate.revoked_at IS NULL ORDER BY candidate.id`
	}
	rows, err := s.db.S().Q().Query(ctx, query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			return nil
		}
		ids = append(ids, id)
	}
	if rows.Err() != nil {
		return nil
	}
	return ids
}
