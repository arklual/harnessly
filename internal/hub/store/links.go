package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ErrFenced is what a write guarded by a fencing token answers when a newer owner holds the machine's link.
var ErrFenced = errors.New("store: the link is held by a newer owner")

// LinkRow is who holds a machine's link now.
type LinkRow struct {
	MachineID  string
	Owner      string
	Addr       string
	Fence      int64
	LeaseUntil time.Time
}

// AcquireLink makes owner the holder of the machine's link and returns the new fencing token, greater than every token
// given before. The machine dialed this replica, so the dial wins over a lease another replica still holds: that one
// learns it lost on its next renewal or guarded write.
func (s Store) AcquireLink(ctx context.Context, machineID, owner, addr string, now time.Time, ttl time.Duration) (int64, error) {
	var fence int64
	err := s.q.QueryRow(ctx, `INSERT INTO machine_links (machine_id, owner, addr, fence, lease_until, updated_at)
VALUES (?, ?, ?, 1, ?, ?)
ON CONFLICT (machine_id) DO UPDATE SET owner = excluded.owner, addr = excluded.addr, fence = machine_links.fence + 1,
  lease_until = excluded.lease_until, updated_at = excluded.updated_at
RETURNING fence`, machineID, owner, addr, Ms(now.Add(ttl)), Ms(now)).Scan(&fence)
	return fence, err
}

// RenewLink extends the lease; false when the holder is no longer owner with this fence.
func (s Store) RenewLink(ctx context.Context, machineID, owner string, fence int64, now time.Time, ttl time.Duration) (bool, error) {
	res, err := s.q.Exec(ctx, `UPDATE machine_links SET lease_until = ?, updated_at = ? WHERE machine_id = ? AND owner = ? AND fence = ?`,
		Ms(now.Add(ttl)), Ms(now), machineID, owner, fence)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// ReleaseLink gives the link up; it does nothing when a newer owner holds it. The row stays, so the fence only grows.
func (s Store) ReleaseLink(ctx context.Context, machineID, owner string, fence int64, now time.Time) error {
	_, err := s.q.Exec(ctx, `UPDATE machine_links SET owner = '', addr = '', lease_until = 0, updated_at = ? WHERE machine_id = ? AND owner = ? AND fence = ?`,
		Ms(now), machineID, owner, fence)
	return err
}

// LinkOf is the live holder of the machine's link: false when nobody holds it or the lease has run out.
func (s Store) LinkOf(ctx context.Context, machineID string, now time.Time) (LinkRow, bool, error) {
	var r LinkRow
	var lease int64
	err := s.q.QueryRow(ctx, `SELECT machine_id, owner, addr, fence, lease_until FROM machine_links WHERE machine_id = ? AND owner <> '' AND lease_until > ?`,
		machineID, Ms(now)).Scan(&r.MachineID, &r.Owner, &r.Addr, &r.Fence, &lease)
	if errors.Is(err, sql.ErrNoRows) {
		return LinkRow{}, false, nil
	}
	r.LeaseUntil = time.UnixMilli(lease).UTC()
	return r, err == nil, err
}

// LiveLinks lists the machines whose link has a live lease.
func (s Store) LiveLinks(ctx context.Context, now time.Time) ([]LinkRow, error) {
	rows, err := s.q.Query(ctx, `SELECT machine_id, owner, addr, fence, lease_until FROM machine_links WHERE owner <> '' AND lease_until > ? ORDER BY machine_id`, Ms(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LinkRow
	for rows.Next() {
		var r LinkRow
		var lease int64
		if err := rows.Scan(&r.MachineID, &r.Owner, &r.Addr, &r.Fence, &lease); err != nil {
			return nil, err
		}
		r.LeaseUntil = time.UnixMilli(lease).UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}

// heldBy is the guard of a write: the machine's link row still names this owner and fence.
const heldBy = `EXISTS (SELECT 1 FROM machine_links WHERE machine_id = ? AND owner = ? AND fence = ?)`

// TouchMachineFenced is TouchMachine that does nothing, and says ErrFenced, when the caller's fence is old.
func (s Store) TouchMachineFenced(ctx context.Context, id string, at time.Time, owner string, fence int64) error {
	return s.fenced(s.q.Exec(ctx, `UPDATE machines SET last_seen_at = ? WHERE id = ? AND `+heldBy, Ms(at), id, id, owner, fence))
}

// UpdateMachineHelloFenced is UpdateMachineHello guarded the same way.
func (s Store) UpdateMachineHelloFenced(ctx context.Context, m Machine, owner string, fence int64) error {
	return s.fenced(s.q.Exec(ctx, `UPDATE machines SET hostname = ?, os = ?, arch = ?, unix_user = ?, agent_version = ? WHERE id = ? AND `+heldBy,
		m.Hostname, m.OS, m.Arch, m.UnixUser, m.AgentVersion, m.ID, m.ID, owner, fence))
}

// UpdateMachineInfoFenced is UpdateMachineInfo guarded the same way.
func (s Store) UpdateMachineInfoFenced(ctx context.Context, id, info, owner string, fence int64) error {
	return s.fenced(s.q.Exec(ctx, `UPDATE machines SET info = ? WHERE id = ? AND `+heldBy, info, id, id, owner, fence))
}

func (s Store) fenced(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrFenced
	}
	return nil
}

// LinkHeld says whether owner still holds the machine's link with this fencing token and an unexpired lease.
func (s Store) LinkHeld(ctx context.Context, machineID, owner string, fence int64, now time.Time) bool {
	row, ok, err := s.LinkOf(ctx, machineID, now)
	return err == nil && ok && row.Owner == owner && row.Fence == fence
}
