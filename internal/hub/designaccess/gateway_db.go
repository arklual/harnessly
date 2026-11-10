package designaccess

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // the gateway's database is Postgres
)

// GatewayDB is the gateway's Postgres: read for the designs' access, written only for the pairs it
// hands to the hub (hub_design_machines and the «hub-access» switch of each pair).
type GatewayDB struct{ db *sql.DB }

// OpenGateway opens the gateway's database by its address (postgres://…).
func OpenGateway(ctx context.Context, url string) (*GatewayDB, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("the gateway's database: %w", err)
	}
	return &GatewayDB{db: db}, nil
}

func (g *GatewayDB) Close() error { return g.db.Close() }

// msOf is a timestamp column as milliseconds, NULL kept.
func msOf(col string) string { return "(extract(epoch from " + col + ") * 1000)::bigint" }

// Read is everything ImportGateway takes, in one read-only transaction.
func (g *GatewayDB) Read(ctx context.Context) (GatewayData, error) {
	tx, err := g.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return GatewayData{}, err
	}
	defer func() { _ = tx.Rollback() }()
	data := GatewayData{Linked: map[string]string{}, Off: map[string]bool{}}
	byID := map[string]*GwProject{}
	each := func(query string, scan func(*sql.Rows) error) error {
		rows, err := tx.QueryContext(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}
	if err := each(`select id, coalesce(owner_email, ''), coalesce(owner_name, ''), coalesce(home_team, ''), coalesce(team_level, 'none'),
			coalesce(title, ''), coalesce(subkind, ''), coalesce(editors_can_share, false), `+msOf("created_at")+`
		from resources where kind = 'design'`, func(r *sql.Rows) error {
		var p GwProject
		if err := r.Scan(&p.ID, &p.Owner, &p.OwnerName, &p.HomeTeam, &p.TeamLevel, &p.Title, &p.Subkind, &p.EditorsCanShare, &p.CreatedAt); err != nil {
			return err
		}
		data.Projects = append(data.Projects, p)
		return nil
	}); err != nil {
		return GatewayData{}, fmt.Errorf("resources: %w", err)
	}
	for i := range data.Projects {
		byID[data.Projects[i].ID] = &data.Projects[i]
	}
	if err := each(`select resource_id, person_email, role, coalesce(granted_by, ''), `+msOf("granted_at")+`, `+msOf("expires_at")+`
		from resource_members where kind = 'design'`, func(r *sql.Rows) error {
		var id string
		var m GwMember
		if err := r.Scan(&id, &m.Email, &m.Role, &m.GrantedBy, &m.GrantedAt, &m.ExpiresAt); err != nil {
			return err
		}
		if p := byID[id]; p != nil {
			p.Members = append(p.Members, m)
		}
		return nil
	}); err != nil {
		return GatewayData{}, fmt.Errorf("resource_members: %w", err)
	}
	if err := each(`select resource_id, team_id, role, coalesce(can_share, false), coalesce(granted_by, ''), `+msOf("granted_at")+`, `+msOf("expires_at")+`
		from resource_team_grants where kind = 'design'`, func(r *sql.Rows) error {
		var id string
		var g GwTeamGrant
		if err := r.Scan(&id, &g.Team, &g.Role, &g.CanShare, &g.GrantedBy, &g.GrantedAt, &g.ExpiresAt); err != nil {
			return err
		}
		if p := byID[id]; p != nil {
			p.Teams = append(p.Teams, g)
		}
		return nil
	}); err != nil {
		return GatewayData{}, fmt.Errorf("resource_team_grants: %w", err)
	}
	if err := each(`select id::text, resource_id, person_email, level, coalesce(message, ''), status, decided_by, `+msOf("decided_at")+`, `+msOf("created_at")+`
		from resource_requests where kind = 'design'`, func(r *sql.Rows) error {
		var id string
		var q GwRequest
		if err := r.Scan(&q.ID, &id, &q.Email, &q.Level, &q.Message, &q.Status, &q.DecidedBy, &q.DecidedAt, &q.CreatedAt); err != nil {
			return err
		}
		if p := byID[id]; p != nil {
			p.Requests = append(p.Requests, q)
		}
		return nil
	}); err != nil {
		return GatewayData{}, fmt.Errorf("resource_requests: %w", err)
	}
	if err := each(`select id::text, project_id, token_hash, coalesce(title, ''), role, (extract(epoch from role_ttl) * 1000)::bigint, `+msOf("expires_at")+`,
			max_uses::bigint, coalesce(uses, 0)::bigint, `+msOf("revoked_at")+`, coalesce(created_by, ''), `+msOf("created_at")+`
		from design_invites`, func(r *sql.Rows) error {
		var id string
		var v GwInvite
		if err := r.Scan(&v.ID, &id, &v.TokenHash, &v.Title, &v.Role, &v.RoleTTL, &v.ExpiresAt, &v.MaxUses, &v.Uses, &v.RevokedAt, &v.CreatedBy, &v.CreatedAt); err != nil {
			return err
		}
		if p := byID[id]; p != nil {
			p.Invites = append(p.Invites, v)
		}
		return nil
	}); err != nil {
		return GatewayData{}, fmt.Errorf("design_invites: %w", err)
	}
	links := map[string]*GwLink{}
	type linkRef struct{ project, id string }
	var order []linkRef
	if err := each(`select id::text, project_id, token_hash, coalesce(title, ''), rights, coalesce(version, 'latest'), rev, pages::text, view::text,
			`+msOf("expires_at")+`, `+msOf("revoked_at")+`, coalesce(created_by, ''), `+msOf("created_at")+`, coalesce(opens, 0)::bigint, `+msOf("last_opened_at")+`
		from design_share_links order by created_at, id`, func(r *sql.Rows) error {
		var project string
		var l GwLink
		if err := r.Scan(&l.ID, &project, &l.TokenHash, &l.Title, &l.Rights, &l.Version, &l.Rev, &l.Pages, &l.View,
			&l.ExpiresAt, &l.RevokedAt, &l.CreatedBy, &l.CreatedAt, &l.Opens, &l.LastOpenedAt); err != nil {
			return err
		}
		links[l.ID] = &l
		order = append(order, linkRef{project, l.ID})
		return nil
	}); err != nil {
		return GatewayData{}, fmt.Errorf("design_share_links: %w", err)
	}
	if err := each(`select link_id::text, guest_id, board, ms::bigint, `+msOf("at")+` from design_share_views order by at, id`, func(r *sql.Rows) error {
		var id string
		var v GwView
		if err := r.Scan(&id, &v.GuestID, &v.Board, &v.Ms, &v.At); err != nil {
			return err
		}
		if l := links[id]; l != nil {
			l.Views = append(l.Views, v)
		}
		return nil
	}); err != nil {
		return GatewayData{}, fmt.Errorf("design_share_views: %w", err)
	}
	if err := each(`select link_id::text, board, x::float8, y::float8, text, guest_id, coalesce(guest_name, ''), anchor::text, `+msOf("created_at")+`
		from design_guest_comments order by created_at, id`, func(r *sql.Rows) error {
		var id string
		var c GwComment
		if err := r.Scan(&id, &c.Board, &c.X, &c.Y, &c.Text, &c.GuestID, &c.GuestName, &c.Anchor, &c.CreatedAt); err != nil {
			return err
		}
		if l := links[id]; l != nil {
			l.Comments = append(l.Comments, c)
		}
		return nil
	}); err != nil {
		return GatewayData{}, fmt.Errorf("design_guest_comments: %w", err)
	}
	for _, ref := range order {
		if p := byID[ref.project]; p != nil {
			p.Links = append(p.Links, *links[ref.id])
		}
	}
	if err := each(`select person_email, machine_id from runtimes order by person_email, machine_id`, func(r *sql.Rows) error {
		var p GwPair
		if err := r.Scan(&p.Person, &p.Machine); err != nil {
			return err
		}
		data.Runtimes = append(data.Runtimes, p)
		return nil
	}); err != nil {
		return GatewayData{}, fmt.Errorf("runtimes: %w", err)
	}
	if err := each(`select person_email, machine_id, hub_machine_id from hub_design_machines`, func(r *sql.Rows) error {
		var person, machine, hub string
		if err := r.Scan(&person, &machine, &hub); err != nil {
			return err
		}
		data.Linked[pairKey(person, machine)] = hub
		return nil
	}); err != nil {
		return GatewayData{}, fmt.Errorf("hub_design_machines: %w", err)
	}
	if err := each(`select person_email, machine_id from rollout_switches where feature = '`+gatewaySwitch+`' and not enabled`, func(r *sql.Rows) error {
		var person, machine string
		if err := r.Scan(&person, &machine); err != nil {
			return err
		}
		data.Off[pairKey(person, machine)] = true
		return nil
	}); err != nil {
		return GatewayData{}, fmt.Errorf("rollout_switches: %w", err)
	}
	return data, nil
}

// gatewaySwitch is the gateway's rollout switch that hands a pair's designs' access to the hub (HRN-756).
const gatewaySwitch = "hub-access"

// Link hands the report's linked pairs to the hub: a row of hub_design_machines each, and the pair's switch on unless it
// was turned off on purpose. A row or a switch that is there already is left as it is. Nothing is written in a dry run or
// when a project failed.
func (g *GatewayDB) Link(ctx context.Context, rep ImportReport, by string) (int, error) {
	if rep.DryRun || rep.Failed() {
		return 0, nil
	}
	tx, err := g.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	n := 0
	for _, l := range rep.Pairs {
		if l.Outcome != "linked" && l.Outcome != "off" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `insert into hub_design_machines (person_email, machine_id, hub_machine_id, linked_by) values ($1, $2, $3, $4)
			on conflict (person_email, machine_id) do nothing`, l.Person, l.Machine, l.HubMachine, by); err != nil {
			return 0, err
		}
		if l.Outcome == "linked" {
			if _, err := tx.ExecContext(ctx, `insert into rollout_switches (feature, person_email, machine_id, enabled, note, updated_by)
				values ($1, $2, $3, true, 'HRN-880: the designs'' access moved to the hub', $4) on conflict (feature, person_email, machine_id) do nothing`,
				gatewaySwitch, l.Person, l.Machine, by); err != nil {
				return 0, err
			}
		}
		n++
	}
	return n, tx.Commit()
}

// MoveFromGateway is the whole move: read the gateway, import into the hub, hand the pairs over. It returns the report
// and how many pairs were written into the gateway.
func (s *Service) MoveFromGateway(ctx context.Context, url string, opt ImportOptions) (ImportReport, int, error) {
	g, err := OpenGateway(ctx, url)
	if err != nil {
		return ImportReport{}, 0, err
	}
	defer g.Close()
	data, err := g.Read(ctx)
	if err != nil {
		return ImportReport{}, 0, fmt.Errorf("reading the gateway: %w", err)
	}
	rep, err := s.ImportGateway(ctx, data, opt)
	if err != nil {
		return rep, 0, err
	}
	by := opt.By
	if by == "" {
		by = "hub-import"
	}
	n, err := g.Link(ctx, rep, by)
	if err != nil {
		return rep, 0, fmt.Errorf("handing the pairs to the hub: %w", err)
	}
	return rep, n, nil
}
