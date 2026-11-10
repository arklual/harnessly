package designaccess

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// HRN-880. The access to the designs moves from a cluster's gateway to the hub, for everyone at once and not by
// hand: ImportGateway copies what the gateway's database says (the projects, their people and teams, the requests, the
// invites and the guest links with their views and comments) into the hub's rows of a space, and links each person ×
// machine of the gateway to the person's machine of the hub, so the gateway hands their designs' access over.
//
// It is a release step and runs again on every start: a project is imported once (a «gateway_import» line of its log
// marks it), so what was changed in the hub since is never undone, and a row the hub has already (the same person, the
// same link) is kept as the hub has it. A dry run does the same in a transaction it rolls back and reports it.

// GatewayData is what the gateway's database holds of the designs' access.
type GatewayData struct {
	Projects []GwProject
	// Runtimes are the gateway's person × machine pairs (its machine ids: vm-1, vm-2).
	Runtimes []GwPair
	// Linked are the pairs the gateway already hands to the hub: "person|machine" → the hub's machine id.
	Linked map[string]string
	// Off are the pairs whose «hub-access» switch was turned off on purpose: "person|machine".
	Off map[string]bool
}

type GwPair struct{ Person, Machine string }

func pairKey(person, machine string) string { return normEmail(person) + "|" + machine }

type GwProject struct {
	ID, Owner, OwnerName, HomeTeam, TeamLevel, Title, Subkind string
	EditorsCanShare                                           bool
	CreatedAt                                                 int64
	Members                                                   []GwMember
	Teams                                                     []GwTeamGrant
	Requests                                                  []GwRequest
	Invites                                                   []GwInvite
	Links                                                     []GwLink
}

type GwMember struct {
	Email, Role, GrantedBy string
	GrantedAt              int64
	ExpiresAt              sql.NullInt64
}

type GwTeamGrant struct {
	Team, Role, GrantedBy string
	CanShare              bool
	GrantedAt             int64
	ExpiresAt             sql.NullInt64
}

type GwRequest struct {
	ID, Email, Level, Message, Status string
	DecidedBy                         sql.NullString
	DecidedAt                         sql.NullInt64
	CreatedAt                         int64
}

type GwInvite struct {
	ID, TokenHash, Title, Role, CreatedBy string
	RoleTTL, MaxUses, RevokedAt           sql.NullInt64
	ExpiresAt, Uses, CreatedAt            int64
}

type GwLink struct {
	ID, TokenHash, Title, Rights, Version, CreatedBy string
	Rev, Pages, View                                 sql.NullString
	ExpiresAt, RevokedAt, LastOpenedAt               sql.NullInt64
	CreatedAt, Opens                                 int64
	Views                                            []GwView
	Comments                                         []GwComment
}

type GwView struct {
	GuestID, Board string
	Ms, At         int64
}

type GwComment struct {
	Board, Text, GuestID, GuestName string
	X, Y                            sql.NullFloat64
	Anchor                          sql.NullString
	CreatedAt                       int64
}

// ImportOptions say where the gateway's designs go.
type ImportOptions struct {
	// Space is the slug of the hub's space whose library holds the designs (HUB_STORE_LEGACY_SPACE).
	Space string
	// Teams map a gateway team (resources.home_team) to a hub team id. A team not named here is the
	// «general» team of the space whose slug is the team's or <team>-team.
	Teams map[string]string
	// Machines name the hub machine of a gateway pair, "person@example.com@vm-1" → id, where its name does not say it.
	Machines map[string]string
	// MachinePrefix is what the hub machines' names put before a gateway machine's ("" for none).
	MachinePrefix string
	// DefaultMachine holds the rows of a project whose owner has no machine in the space; empty is the space's oldest.
	DefaultMachine string
	DryRun         bool
	// By is who the rows say made the move.
	By string
}

// ImportReport is what was moved, or would be in a dry run.
type ImportReport struct {
	DryRun   bool
	Space    string
	Projects []ProjectImport
	Pairs    []PairLink
	Notes    []string
}

type ProjectImport struct {
	ID, Owner, Machine string
	// Outcome: imported, merged (the hub knew the project: its own row is kept, the people and links added), kept (the
	// hub knew it and its owner was moved before: nothing added), done (this import ran before), skipped, failed.
	Outcome                                          string
	Members, Teams, Requests, Invites, Links, Extras int
	// Refused counts the gateway's rows the hub would not take as it is (an unknown role, «owner» in a grant): left out.
	Refused int
	Reason  string
}

type PairLink struct {
	Person, Machine, HubMachine string
	// Outcome: linked, kept (linked before), off (the switch is off for the pair on purpose: the row is written, the
	// switch left), missing, ambiguous.
	Outcome string
	Reason  string
}

// Failed says whether some project could not be moved: then no pair is linked, so nobody's access goes to rows the hub
// does not have.
func (r ImportReport) Failed() bool {
	for _, p := range r.Projects {
		if p.Outcome == "failed" {
			return true
		}
	}
	return false
}

// Summary is the report in a few lines, as the command prints it.
func (r ImportReport) Summary() string { return r.summary(func(v string) string { return v }) }

// LogSummary is Summary with the addresses masked (a…@example.org), for the server's log.
func (r ImportReport) LogSummary() string { return r.summary(maskEmail) }

func maskEmail(v string) string {
	at := strings.LastIndexByte(v, '@')
	if at <= 0 {
		return v
	}
	return v[:1] + "…" + v[at:]
}

func (r ImportReport) summary(who func(string) string) string {
	var b strings.Builder
	mode := "apply"
	if r.DryRun {
		mode = "dry run"
	}
	count := map[string]int{}
	for _, p := range r.Projects {
		count[p.Outcome]++
	}
	fmt.Fprintf(&b, "design access from the gateway into space %s (%s): %d projects: %d imported, %d merged, %d kept, %d moved before, %d skipped, %d failed\n",
		r.Space, mode, len(r.Projects), count["imported"], count["merged"], count["kept"], count["done"], count["skipped"], count["failed"])
	for _, p := range r.Projects {
		fmt.Fprintf(&b, "  project %-24s %-8s owner=%s machine=%s members=%d teams=%d requests=%d invites=%d links=%d views+comments=%d refused=%d %s\n",
			p.ID, p.Outcome, who(p.Owner), p.Machine, p.Members, p.Teams, p.Requests, p.Invites, p.Links, p.Extras, p.Refused, p.Reason)
	}
	for _, l := range r.Pairs {
		fmt.Fprintf(&b, "  pair %s × %s → %s: %s %s\n", who(l.Person), l.Machine, orDash(l.HubMachine), l.Outcome, l.Reason)
	}
	for _, n := range r.Notes {
		fmt.Fprintf(&b, "  note: %s\n", n)
	}
	return b.String()
}

func orDash(v string) string {
	if v == "" {
		return "—"
	}
	return v
}

var errDryRun = errors.New("designaccess: dry run")

// ImportGateway moves the gateway's projects into the hub's rows of the space and works out the hub machine of each of
// the gateway's pairs. The pairs are written into the gateway's database by the caller (GatewayDB.Link), after this and
// only when no project failed.
func (s *Service) ImportGateway(ctx context.Context, data GatewayData, opt ImportOptions) (ImportReport, error) {
	rep := ImportReport{DryRun: opt.DryRun, Space: opt.Space}
	st := s.db.S()
	sp, err := st.SpaceBySlug(ctx, opt.Space)
	if err != nil {
		return rep, fmt.Errorf("space %q: %w", opt.Space, err)
	}
	machines, err := s.spaceMachineRows(ctx, sp.ID)
	if err != nil {
		return rep, err
	}
	if len(machines) == 0 {
		return rep, fmt.Errorf("space %q has no machines", opt.Space)
	}
	fallback := machines[0].ID
	if opt.DefaultMachine != "" {
		fallback = ""
		for _, m := range machines {
			if m.ID == opt.DefaultMachine {
				fallback = m.ID
			}
		}
		if fallback == "" {
			return rep, fmt.Errorf("default machine %q is not a machine of space %q", opt.DefaultMachine, opt.Space)
		}
	}
	teams, notes, err := s.gatewayTeams(ctx, sp.ID, data.Projects, opt.Teams)
	if err != nil {
		return rep, err
	}
	rep.Notes = append(rep.Notes, notes...)

	// The owners whose machine was handed to the hub before (HRN-756): the hub is where their access has been changed
	// since, so a project of theirs it knows is kept as it is, nothing of the gateway's added.
	moved := map[string]bool{}
	for key := range data.Linked {
		person, _, _ := strings.Cut(key, "|")
		moved[person] = true
	}
	projects := append([]GwProject(nil), data.Projects...)
	sort.Slice(projects, func(i, j int) bool { return projects[i].ID < projects[j].ID })
	for _, p := range projects {
		holder := fallback
		if owner := normEmail(p.Owner); owner != "" {
			for _, m := range machines {
				if m.creator == owner {
					holder = m.ID
					break
				}
			}
		}
		pr := ProjectImport{ID: p.ID, Owner: normEmail(p.Owner), Machine: holder}
		if !projectIDRe.MatchString(p.ID) {
			pr.Outcome, pr.Reason = "skipped", "not a project id of a library"
			rep.Projects = append(rep.Projects, pr)
			continue
		}
		m, err := st.MachineByID(ctx, holder)
		if err != nil {
			return rep, err
		}
		err = s.db.Tx(ctx, func(st store.Store) error {
			w, err := newWorld(ctx, st, m)
			if err != nil {
				return err
			}
			t := &txn{ctx: ctx, st: st, q: st.Q(), pg: s.db.Driver() == store.Postgres, m: m, w: w, now: s.now(), seal: s.seal, shared: s.shared}
			if err := t.importProject(sp.ID, p, teams, moved[normEmail(p.Owner)], opt.By, &pr); err != nil {
				return err
			}
			if opt.DryRun {
				return errDryRun
			}
			return nil
		})
		switch {
		case errors.Is(err, errDryRun):
		case err != nil:
			pr.Outcome, pr.Reason = "failed", err.Error()
		case pr.Outcome == "imported" || pr.Outcome == "merged":
			s.kickSpace(ctx, m)
		}
		rep.Projects = append(rep.Projects, pr)
	}

	rep.Pairs = planPairs(data, machines, opt.Machines, opt.MachinePrefix)
	return rep, nil
}

// importProject moves one project, once: its row unless the hub has one, then every row the hub does not have yet, and
// the mark that it was moved.
func (t *txn) importProject(spaceID string, p GwProject, teams map[string]string, moved bool, by string, pr *ProjectImport) error {
	if t.pg {
		// Several replicas start together: one moves the project, the others find its mark.
		if _, err := t.q.Exec(t.ctx, `SELECT pg_advisory_xact_lock(hashtext(?))`, "design-gateway-import:"+spaceID+":"+p.ID); err != nil {
			return err
		}
	}
	var done int
	if err := t.q.QueryRow(t.ctx, `SELECT COUNT(*) FROM design_access_log l JOIN machines m ON m.id = l.machine_id
		WHERE l.project_id = ? AND l.action = 'gateway_import' AND m.space_id = ?`, p.ID, spaceID).Scan(&done); err != nil {
		return err
	}
	if done > 0 {
		pr.Outcome = "done"
		return nil
	}
	have, err := t.getProject(p.ID, false)
	if err != nil {
		return err
	}
	key := t.key(p.ID)
	pr.Machine = key
	owner := normEmail(p.Owner)
	if have != nil && moved {
		pr.Outcome, pr.Reason = "kept", "the hub holds its access since the owner's earlier move"
		return t.writeLog(p.ID, logActor(by), "gateway_import", "", map[string]any{"outcome": pr.Outcome})
	}
	if have != nil {
		pr.Outcome = "merged"
		if have.Owner != owner {
			pr.Reason = "the hub's owner " + orDash(maskEmail(have.Owner)) + " is kept"
		}
	} else {
		pr.Outcome = "imported"
		home := ""
		if p.HomeTeam != "" {
			home = teams[p.HomeTeam]
		}
		level := p.TeamLevel
		if home == "" || (level != levelNone && rank(level) < 0) {
			level = levelNone
		}
		if _, err := t.q.Exec(t.ctx, `INSERT INTO design_projects (machine_id, project_id, owner_email, owner_name, home_team, team_level,
			editors_can_share, legacy, title, subkind, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			key, p.ID, nullStr(owner), p.OwnerName, nullStr(home), level, boolInt(p.EditorsCanShare), boolInt(owner == ""),
			p.Title, p.Subkind, p.CreatedAt, ms(t.now)); err != nil {
			return err
		}
	}
	added := func(res sql.Result, err error) (bool, error) {
		if err != nil {
			return false, err
		}
		n, err := res.RowsAffected()
		return n == 1, err
	}
	for _, m := range p.Members {
		email := normEmail(m.Email)
		if email == owner {
			continue
		}
		if email == "" || !grantable(m.Role) {
			pr.Refused++
			continue
		}
		ok, err := added(t.q.Exec(t.ctx, `INSERT INTO design_members (machine_id, project_id, person_email, role, granted_by, granted_at, expires_at, via_invite)
			VALUES (?, ?, ?, ?, ?, ?, ?, NULL) ON CONFLICT DO NOTHING`, key, p.ID, email, m.Role, m.GrantedBy, m.GrantedAt, m.ExpiresAt))
		if err != nil {
			return err
		}
		if ok {
			pr.Members++
		}
	}
	for _, g := range p.Teams {
		team := teams[g.Team]
		if team == "" {
			continue
		}
		if !grantable(g.Role) {
			pr.Refused++
			continue
		}
		ok, err := added(t.q.Exec(t.ctx, `INSERT INTO design_team_grants (machine_id, project_id, team_id, role, can_share, granted_by, granted_at, expires_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, key, p.ID, team, g.Role, boolInt(g.CanShare), g.GrantedBy, g.GrantedAt, g.ExpiresAt))
		if err != nil {
			return err
		}
		if ok {
			pr.Teams++
		}
	}
	for _, r := range p.Requests {
		if !grantable(r.Level) || !uuidRe.MatchString(r.ID) {
			pr.Refused++
			continue
		}
		ok, err := added(t.q.Exec(t.ctx, `INSERT INTO design_requests (id, machine_id, project_id, person_email, level, message, status, decided_by, decided_at, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, r.ID, key, p.ID, normEmail(r.Email), r.Level, r.Message, r.Status, r.DecidedBy, r.DecidedAt, r.CreatedAt))
		if err != nil {
			return err
		}
		if ok {
			pr.Requests++
		}
	}
	for _, v := range p.Invites {
		if !grantable(v.Role) || !hexToken.MatchString(v.TokenHash) {
			pr.Refused++
			continue
		}
		ok, err := added(t.q.Exec(t.ctx, `INSERT INTO design_invites (id, machine_id, project_id, token_hash, title, role, role_ttl_ms, expires_at, max_uses, uses, revoked_at, created_by, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, v.ID, key, p.ID, v.TokenHash, v.Title, v.Role, v.RoleTTL, v.ExpiresAt, v.MaxUses, v.Uses, v.RevokedAt, v.CreatedBy, v.CreatedAt))
		if err != nil {
			return err
		}
		if ok {
			pr.Invites++
		}
	}
	for _, l := range p.Links {
		version := l.Version
		if version == "" {
			version = "latest"
		}
		if (l.Rights != roleView && l.Rights != roleComment) || (version != "latest" && version != "pinned") || !hexToken.MatchString(l.TokenHash) {
			pr.Refused++
			continue
		}
		// The gateway's sealed address is its own key's: the hub cannot open it, so the link is kept without one (it opens
		// as before; it cannot be copied again, a new link can).
		ok, err := added(t.q.Exec(t.ctx, `INSERT INTO design_share_links (id, machine_id, project_id, token_hash, token_enc, title, rights, version, rev, pages, view,
			expires_at, revoked_at, created_by, created_at, opens, last_opened_at) VALUES (?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
			l.ID, key, p.ID, l.TokenHash, l.Title, l.Rights, version, l.Rev, l.Pages, l.View, l.ExpiresAt, l.RevokedAt, l.CreatedBy, l.CreatedAt, l.Opens, l.LastOpenedAt))
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		pr.Links++
		for _, v := range l.Views {
			if _, err := t.q.Exec(t.ctx, `INSERT INTO design_share_views (link_id, guest_id, guest_name, board, ms, at) VALUES (?, ?, '', ?, ?, ?)`,
				l.ID, v.GuestID, v.Board, v.Ms, v.At); err != nil {
				return err
			}
			pr.Extras++
		}
		for _, c := range l.Comments {
			if _, err := t.insertRow(`INSERT INTO design_guest_comments (id, link_id, board, x, y, text, guest_id, guest_name, anchor, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				l.ID, c.Board, c.X, c.Y, c.Text, c.GuestID, c.GuestName, c.Anchor, c.CreatedAt); err != nil {
				return err
			}
			pr.Extras++
		}
	}
	return t.writeLog(p.ID, logActor(by), "gateway_import", "", map[string]any{
		"members": pr.Members, "teams": pr.Teams, "requests": pr.Requests, "invites": pr.Invites, "links": pr.Links, "outcome": pr.Outcome,
	})
}

// grantable is a role a person, a team or an invite may be given: not «owner», which only the project's row says.
func grantable(role string) bool { return rank(role) >= 0 && role != roleOwner }

// hexToken is a token's hash as both the gateway and the hub keep it: sha256, hex.
var hexToken = regexp.MustCompile(`^[0-9a-f]{64}$`)

func logActor(by string) string {
	if by == "" {
		return "hub"
	}
	return by
}

type spaceMachine struct {
	ID, Name, creator string
}

// spaceMachineRows are the space's machines that are not revoked, oldest first, with the address of who made each.
func (s *Service) spaceMachineRows(ctx context.Context, spaceID string) ([]spaceMachine, error) {
	rows, err := s.db.S().Q().Query(ctx, `SELECT m.id, m.name, COALESCE(u.email, '') FROM machines m LEFT JOIN users u ON u.id = m.created_by
		WHERE m.space_id = ? AND m.revoked_at IS NULL ORDER BY m.created_at, m.id`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []spaceMachine
	for rows.Next() {
		var m spaceMachine
		if err := rows.Scan(&m.ID, &m.Name, &m.creator); err != nil {
			return nil, err
		}
		m.creator = normEmail(m.creator)
		out = append(out, m)
	}
	return out, rows.Err()
}

// gatewayTeams maps every gateway team the projects name to a hub team: the one named (by the operator, so it may be a
// team of another space of the hub), else a team of the target space itself: the one whose slug is the gateway team's,
// or the «general» team when the space's slug is the team's or <team>-team. Nothing else of the hub is looked at.
func (s *Service) gatewayTeams(ctx context.Context, spaceID string, projects []GwProject, named map[string]string) (map[string]string, []string, error) {
	want := map[string]bool{}
	for _, p := range projects {
		if p.HomeTeam != "" {
			want[p.HomeTeam] = true
		}
		for _, g := range p.Teams {
			want[g.Team] = true
		}
	}
	out := map[string]string{}
	var notes []string
	q := s.db.S().Q()
	var spaceSlug string
	if err := q.QueryRow(ctx, `SELECT slug FROM spaces WHERE id = ?`, spaceID).Scan(&spaceSlug); err != nil {
		return nil, nil, err
	}
	for _, team := range sortedKeys(want) {
		if id := named[team]; id != "" {
			var n int
			if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM teams WHERE id = ?`, id).Scan(&n); err != nil {
				return nil, nil, err
			}
			if n == 0 {
				return nil, nil, fmt.Errorf("team %q: hub team %q does not exist", team, id)
			}
			out[team] = id
			continue
		}
		slug := team
		if spaceSlug == team || spaceSlug == team+"-team" {
			slug = "general"
		}
		var id string
		err := q.QueryRow(ctx, `SELECT id FROM teams WHERE space_id = ? AND slug = ?`, spaceID, slug).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			notes = append(notes, "gateway team "+team+" has no team in the space (name one in HUB_DESIGN_IMPORT_TEAMS): its projects are personal, its grants are left out")
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		out[team] = id
	}
	return out, notes, nil
}

// planPairs finds the hub machine of every gateway pair: one named in Machines, else the one machine of the space its
// person made whose name is <prefix><machine>, or starts with <prefix><machine>- or <prefix><machine>. .
func planPairs(data GatewayData, machines []spaceMachine, named map[string]string, prefix string) []PairLink {
	runtimes := append([]GwPair(nil), data.Runtimes...)
	sort.Slice(runtimes, func(i, j int) bool {
		return pairKey(runtimes[i].Person, runtimes[i].Machine) < pairKey(runtimes[j].Person, runtimes[j].Machine)
	})
	var out []PairLink
	for _, r := range runtimes {
		person := normEmail(r.Person)
		l := PairLink{Person: person, Machine: r.Machine}
		key := pairKey(person, r.Machine)
		if id, ok := data.Linked[key]; ok {
			l.HubMachine, l.Outcome = id, "kept"
			out = append(out, l)
			continue
		}
		if id := named[person+"@"+r.Machine]; id != "" {
			for _, m := range machines {
				if m.ID == id {
					l.HubMachine = id
				}
			}
			if l.HubMachine == "" {
				l.Outcome, l.Reason = "missing", "the named machine "+id+" is not in the space"
				out = append(out, l)
				continue
			}
		} else {
			base := strings.ToLower(prefix + r.Machine)
			var found []string
			for _, m := range machines {
				name := strings.ToLower(m.Name)
				if m.creator == person && (name == base || strings.HasPrefix(name, base+"-") || strings.HasPrefix(name, base+".")) {
					found = append(found, m.ID)
				}
			}
			switch len(found) {
			case 0:
				l.Outcome, l.Reason = "missing", "the person has no machine "+base+"… in the space"
				out = append(out, l)
				continue
			case 1:
				l.HubMachine = found[0]
			default:
				l.Outcome, l.Reason = "ambiguous", strings.Join(found, ", ")
				out = append(out, l)
				continue
			}
		}
		l.Outcome = "linked"
		if data.Off[key] {
			l.Outcome, l.Reason = "off", "the switch is off for the pair: left off"
		}
		out = append(out, l)
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
