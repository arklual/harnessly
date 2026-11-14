package designaccess

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// HRN-1419, stage 2 of «the hub is the source of truth». With the switch hub-design-owner on, the hub owns a design
// project's own facts — its name, kind, preview, kit, team and the machine that holds the working copy — in
// design_projects. A machine's runtime writes them as it changes them (a record in the store's systems, decks and
// videos collections, storehub/entities.go) and the hub keeps them when the machine is off or gone: the library lists
// every recorded project from these rows, with the store's files when it has them, and never from a snapshot the machine
// would replace the whole list with.

// OwnerSwitch is the rollout switch of the hub owning the design facts.
const OwnerSwitch = "hub-design-owner"

// Meta is what a machine says of a project: set fields replace the stored ones, At (Unix ms) orders two writers.
type Meta struct {
	Title   string `json:"title,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Preview string `json:"preview,omitempty"`
	Kit     string `json:"kit,omitempty"`
	At      int64  `json:"at,omitempty"`
}

// Who wrote the facts last (design_projects.meta_by).
const (
	MetaByMachine = "machine"
	MetaByHub     = "hub"
)

var metaKinds = map[string]bool{"site": true, "slides": true, "ds": true, "video": true, "deck": true, "kit": true, "app-doc": true, "design": true}

// OwnerOn says whether the hub owns the design facts for the person the machine runs for.
func (s *Service) OwnerOn(ctx context.Context, m store.Machine) bool {
	if s.ownerOn == nil {
		return false
	}
	u, err := s.db.S().UserByID(ctx, m.CreatedBy)
	if err != nil {
		return false
	}
	return s.ownerOn(ctx, normEmail(u.Email), m.ID)
}

// RecordMeta keeps what a machine (by MetaByMachine) or the hub says of a recorded project. A project the hub has no row
// for is not recorded by this: access is recorded by design events. A record older than the stored facts changes nothing.
func (s *Service) RecordMeta(ctx context.Context, m store.Machine, project string, meta Meta, by string) (bool, error) {
	if !projectIDRe.MatchString(project) {
		return false, nil
	}
	if meta.At <= 0 {
		meta.At = ms(s.now())
	}
	meta.Title = clip(strings.TrimSpace(meta.Title), maxTitle)
	meta.Preview = clip(strings.TrimSpace(meta.Preview), 512)
	meta.Kit = clip(strings.TrimSpace(meta.Kit), 128)
	if !metaKinds[meta.Kind] {
		meta.Kind = ""
	}
	changed := false
	err := s.db.Tx(ctx, func(st store.Store) error {
		w, err := newWorld(ctx, st, m)
		if err != nil {
			return err
		}
		t := &txn{ctx: ctx, st: st, q: st.Q(), pg: s.db.Driver() == store.Postgres, m: m, w: w, now: s.now(), shared: s.shared, closed: s.closed, opens: s.opens}
		var title, kind, preview, kit, was string
		var at int64
		err = t.q.QueryRow(ctx, `SELECT COALESCE(title, ''), COALESCE(kind, ''), COALESCE(preview, ''), COALESCE(kit_id, ''), COALESCE(meta_by, ''), COALESCE(meta_at, 0) FROM design_projects WHERE machine_id = ? AND project_id = ?`+t.forUpdate(),
			t.key(project), project).Scan(&title, &kind, &preview, &kit, &was, &at)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		// The hub's own edit is not undone by a record of the machine made before it.
		if was == MetaByHub && by != MetaByHub && meta.At <= at {
			return nil
		}
		if by == MetaByMachine && meta.At < at {
			return nil
		}
		pick := func(old, v string) string {
			if v != "" {
				return v
			}
			return old
		}
		title, kind, preview, kit = pick(title, meta.Title), pick(kind, meta.Kind), pick(preview, meta.Preview), pick(kit, meta.Kit)
		if _, err := t.q.Exec(ctx, `UPDATE design_projects SET title = ?, kind = ?, preview = ?, kit_id = ?, meta_by = ?, meta_at = ? WHERE machine_id = ? AND project_id = ?`,
			title, kind, preview, kit, by, meta.At, t.key(project), project); err != nil {
			return err
		}
		// A machine that wrote the facts is the one whose folder holds the working copy.
		if by == MetaByMachine {
			if _, err := t.q.Exec(ctx, `UPDATE design_projects SET work_machine_id = ? WHERE machine_id = ? AND project_id = ?`, m.ID, t.key(project), project); err != nil {
				return err
			}
		}
		changed = true
		return nil
	})
	return changed, err
}

// SetWorkMachine records the machine whose folder holds the working copy of a project now.
func (s *Service) SetWorkMachine(ctx context.Context, m store.Machine, project, machineID string) error {
	return s.write(ctx, m, func(t *txn) error {
		_, err := t.q.Exec(t.ctx, `UPDATE design_projects SET work_machine_id = ?, meta_by = ?, meta_at = ? WHERE machine_id = ? AND project_id = ?`,
			machineID, MetaByHub, ms(t.now), t.key(project), project)
		return err
	})
}

// metaRow is a project's facts as the hub has them.
type metaRow struct {
	Title, Kind, Preview, Kit, WorkMachine string
	UpdatedAt                              int64
}

func (t *txn) metaOf(project string) (metaRow, error) {
	var r metaRow
	err := t.q.QueryRow(t.ctx, `SELECT COALESCE(title, ''), COALESCE(NULLIF(kind, ''), NULLIF(subkind, ''), 'design'), COALESCE(preview, ''), COALESCE(kit_id, ''), COALESCE(work_machine_id, ''), CASE WHEN COALESCE(meta_at, 0) > updated_at THEN meta_at ELSE updated_at END FROM design_projects WHERE machine_id = ? AND project_id = ?`,
		t.key(project), project).Scan(&r.Title, &r.Kind, &r.Preview, &r.Kit, &r.WorkMachine, &r.UpdatedAt)
	if r.WorkMachine == "" {
		r.WorkMachine = t.key(project)
	}
	return r, err
}

// RecordsReport is the hub's side of the check «the store has as many as the machine»: of the projects the person has a
// role in, how many carry the facts a record of the machine (or an edit of the hub) gave, and which carry none yet.
type RecordsReport struct {
	OK        bool     `json:"ok"`
	Projects  int      `json:"projects"`
	Described int      `json:"described"`
	Missing   []string `json:"missing,omitempty"`
}

const recordsCheckPath = "/design/store-records-check"

// RecordsCheck lists the recorded projects open to the person and says which of them no record has described.
func (s *Service) RecordsCheck(ctx context.Context, u store.User, m store.Machine) (RecordsReport, error) {
	rep := RecordsReport{}
	t, err := s.read(ctx, m)
	if err != nil {
		return rep, err
	}
	rows, err := t.projects()
	if err != nil {
		return rep, err
	}
	email := normEmail(u.Email)
	for _, p := range rows {
		f, err := t.fold(p, email)
		if err != nil {
			return rep, err
		}
		if f.Role == "" {
			continue
		}
		rep.Projects++
		var by string
		if err := t.q.QueryRow(t.ctx, `SELECT COALESCE(meta_by, '') FROM design_projects WHERE machine_id = ? AND project_id = ?`, t.key(p.ID), p.ID).Scan(&by); err != nil {
			return rep, err
		}
		if by == "" {
			rep.Missing = append(rep.Missing, p.ID)
		} else {
			rep.Described++
		}
	}
	rep.OK = len(rep.Missing) == 0
	return rep, nil
}

const takeWorkPath = "/design/store-work-machine"

// TakeWork makes the machine the request came through the one that holds a project's working copy: the owner
// machine is off, the person opens the project to edit it on a live machine of theirs, and the hub records that machine
// as the working-copy machine. The files unfold from the store onto it by the library's own folder sync, which every
// machine of a hub runs; the machine's `design` collection is the project, the folder only a copy. Only a person who may
// edit the project can move it, and only with the switch hub-design-owner on.
func (s *Service) TakeWork(ctx context.Context, u store.User, m store.Machine, project string) error {
	if !s.OwnerOn(ctx, m) {
		return fail("store_off", "Хаб пока не хранит рабочую копию дизайнов")
	}
	if !projectIDRe.MatchString(project) {
		return fail("bad_project", "Неверный проект")
	}
	t, err := s.read(ctx, m)
	if err != nil {
		return err
	}
	rows, err := t.projects()
	if err != nil {
		return err
	}
	for _, p := range rows {
		if p.ID != project {
			continue
		}
		f, err := t.fold(p, normEmail(u.Email))
		if err != nil {
			return err
		}
		if rankOf[f.Role] < rankOf[roleEdit] || f.Role == "" {
			return fail("forbidden", "Править этот проект нельзя")
		}
		return s.SetWorkMachine(ctx, m, project, m.ID)
	}
	return fail("not_found", "Проекта нет")
}
