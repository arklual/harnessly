package sites

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Access says who may open a site.
const (
	// AccessTeam is the default: only people of the site's space.
	AccessTeam = "team"
	// AccessLink is «все, у кого есть ссылка»: anyone with the address.
	AccessLink = "link"
)

// Site is a name in a space.
type Site struct {
	ID, Space, Name string
	Access          string
	SPA             bool
	CurrentVersion  int
	NextVersion     int
	CreatedBy       string // the name shown
	CreatedByID     string // who made it, for rights: a person's id or machine:<id>
	Origin          string // what it was made from (sinopia:<project>), empty for the rest
	UpdatedBy       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Deployment is one immutable snapshot of a site.
type Deployment struct {
	ID         string
	SiteID     string
	Version    int
	Manifest   Manifest
	FileCount  int
	TotalBytes int64
	Source     string
	CreatedBy  string
	CreatedAt  time.Time
}

// Pending is an upload announced and not yet committed.
type Pending struct {
	ID        string
	SiteID    string
	Manifest  Manifest
	Source    string
	CreatedBy string
	CreatedAt time.Time
	ExpiresAt time.Time
	// AllowSecrets is the uploader's «yes, I know» to the secret scan.
	AllowSecrets bool
}

// Token is a deploy token as the database keeps it (never the secret).
type Token struct {
	ID, SiteID, Name, CreatedBy string
	CreatedAt, LastUsedAt       time.Time
}

// ErrNotFound is what a lookup that finds nothing returns.
var ErrNotFound = store.ErrNotFound

func fromMs(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// repo is the queries of the sites tables. It runs on the hub's database layer, so SQLite and Postgres both work.
type repo struct {
	db  *store.DB
	now func() time.Time
}

const siteCols = `id, space, name, access, spa, current_version, next_version, created_by, created_by_id, origin, created_at, updated_at, updated_by`

func scanSite(row interface{ Scan(...any) error }) (Site, error) {
	var s Site
	var spa int
	var created, updated int64
	if err := row.Scan(&s.ID, &s.Space, &s.Name, &s.Access, &spa, &s.CurrentVersion, &s.NextVersion, &s.CreatedBy, &s.CreatedByID, &s.Origin, &created, &updated, &s.UpdatedBy); err != nil {
		return Site{}, err
	}
	s.SPA = spa != 0
	s.CreatedAt, s.UpdatedAt = fromMs(created), fromMs(updated)
	return s, nil
}

func (r repo) createSite(ctx context.Context, s *Site) error {
	if s.ID == "" {
		s.ID = store.NewID()
	}
	now := r.now().UTC()
	s.CreatedAt, s.UpdatedAt, s.NextVersion = now, now, 1
	_, err := r.db.Exec(ctx, `INSERT INTO sites (`+siteCols+`) VALUES (?, ?, ?, ?, ?, 0, 1, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Space, s.Name, s.Access, b2i(s.SPA), s.CreatedBy, s.CreatedByID, s.Origin, store.Ms(now), store.Ms(now), s.CreatedBy)
	return err
}

func (r repo) site(ctx context.Context, space, name string) (Site, error) {
	return scanSite(r.db.QueryRow(ctx, `SELECT `+siteCols+` FROM sites WHERE space = ? AND name = ?`, space, name))
}

func (r repo) siteByID(ctx context.Context, id string) (Site, error) {
	return scanSite(r.db.QueryRow(ctx, `SELECT `+siteCols+` FROM sites WHERE id = ?`, id))
}

func (r repo) listSites(ctx context.Context, space string) ([]Site, error) {
	rows, err := r.db.Query(ctx, `SELECT `+siteCols+` FROM sites WHERE space = ? ORDER BY updated_at DESC, name`, space)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Site
	for rows.Next() {
		s, err := scanSite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r repo) countSites(ctx context.Context, space string) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM sites WHERE space = ?`, space).Scan(&n)
	return n, err
}

func (r repo) updateSite(ctx context.Context, id string, access *string, spa *bool, by string) error {
	now := store.Ms(r.now())
	if access != nil {
		if _, err := r.db.Exec(ctx, `UPDATE sites SET access = ?, updated_at = ?, updated_by = ? WHERE id = ?`, *access, now, by, id); err != nil {
			return err
		}
	}
	if spa != nil {
		if _, err := r.db.Exec(ctx, `UPDATE sites SET spa = ?, updated_at = ?, updated_by = ? WHERE id = ?`, b2i(*spa), now, by, id); err != nil {
			return err
		}
	}
	return nil
}

func (r repo) deleteSite(ctx context.Context, id string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM sites WHERE id = ?`, id)
	return err
}

func (r repo) setCurrent(ctx context.Context, siteID string, version int, by string) error {
	res, err := r.db.Exec(ctx, `UPDATE sites SET current_version = ?, updated_at = ?, updated_by = ?
		WHERE id = ? AND EXISTS (SELECT 1 FROM site_deployments d WHERE d.site_id = sites.id AND d.version = ?)`,
		version, store.Ms(r.now()), by, siteID, version)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- deployments ----

// commit turns a pending upload into the next version of its site and, when makeCurrent, switches the site to it, in one
// transaction: a reader sees the old version or the new one, never a mix.
func (r repo) commit(ctx context.Context, p Pending, makeCurrent bool, by string) (Deployment, error) {
	body, err := json.Marshal(p.Manifest)
	if err != nil {
		return Deployment{}, err
	}
	d := Deployment{ID: store.NewID(), SiteID: p.SiteID, Manifest: p.Manifest, FileCount: len(p.Manifest), TotalBytes: p.Manifest.TotalBytes(),
		Source: p.Source, CreatedBy: by, CreatedAt: r.now().UTC()}
	err = r.db.Tx(ctx, func(s store.Store) error {
		q := s.Q()
		var next int
		if err := q.QueryRow(ctx, `UPDATE sites SET next_version = next_version + 1 WHERE id = ? RETURNING next_version - 1`, p.SiteID).Scan(&next); err != nil {
			return err
		}
		d.Version = next
		if _, err := q.Exec(ctx, `INSERT INTO site_deployments (id, site_id, version, manifest, file_count, total_bytes, source, created_by, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, d.ID, d.SiteID, d.Version, string(body), d.FileCount, d.TotalBytes, d.Source, d.CreatedBy, store.Ms(d.CreatedAt)); err != nil {
			return err
		}
		if makeCurrent {
			if _, err := q.Exec(ctx, `UPDATE sites SET current_version = ?, updated_at = ?, updated_by = ? WHERE id = ?`,
				d.Version, store.Ms(d.CreatedAt), by, d.SiteID); err != nil {
				return err
			}
		}
		_, err := q.Exec(ctx, `DELETE FROM site_pending WHERE id = ?`, p.ID)
		return err
	})
	return d, err
}

const deployCols = `id, site_id, version, file_count, total_bytes, source, created_by, created_at`

func scanDeploy(row interface{ Scan(...any) error }, withManifest bool) (Deployment, error) {
	var d Deployment
	var created int64
	var raw string
	var err error
	if withManifest {
		err = row.Scan(&d.ID, &d.SiteID, &d.Version, &d.FileCount, &d.TotalBytes, &d.Source, &d.CreatedBy, &created, &raw)
	} else {
		err = row.Scan(&d.ID, &d.SiteID, &d.Version, &d.FileCount, &d.TotalBytes, &d.Source, &d.CreatedBy, &created)
	}
	if err != nil {
		return Deployment{}, err
	}
	d.CreatedAt = fromMs(created)
	if withManifest {
		if err := json.Unmarshal([]byte(raw), &d.Manifest); err != nil {
			return Deployment{}, err
		}
	}
	return d, nil
}

func (r repo) deployment(ctx context.Context, siteID string, version int) (Deployment, error) {
	return scanDeploy(r.db.QueryRow(ctx, `SELECT `+deployCols+`, manifest FROM site_deployments WHERE site_id = ? AND version = ?`, siteID, version), true)
}

// listDeployments are the site's versions, newest first, without their manifests.
func (r repo) listDeployments(ctx context.Context, siteID string) ([]Deployment, error) {
	rows, err := r.db.Query(ctx, `SELECT `+deployCols+` FROM site_deployments WHERE site_id = ? ORDER BY version DESC`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Deployment
	for rows.Next() {
		d, err := scanDeploy(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// dropOld deletes the versions beyond the newest keep, never the current one. It returns how many went.
func (r repo) dropOld(ctx context.Context, siteID string, current, keep int) (int, error) {
	res, err := r.db.Exec(ctx, `DELETE FROM site_deployments WHERE site_id = ? AND version <> ? AND version NOT IN
		(SELECT version FROM (SELECT version FROM site_deployments WHERE site_id = ? ORDER BY version DESC LIMIT ?) AS newest)`,
		siteID, current, siteID, keep)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// referencedHashes are the hashes any deployment or pending upload refers to.
func (r repo) referencedHashes(ctx context.Context) (map[string]bool, error) {
	out := map[string]bool{}
	for _, q := range []string{`SELECT manifest FROM site_deployments`, `SELECT manifest FROM site_pending`} {
		rows, err := r.db.Query(ctx, q)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var raw string
			if err := rows.Scan(&raw); err != nil {
				rows.Close()
				return nil, err
			}
			var m Manifest
			if err := json.Unmarshal([]byte(raw), &m); err != nil {
				rows.Close()
				return nil, err
			}
			for _, f := range m {
				out[f.Hash] = true
			}
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ---- pending ----

func (r repo) addPending(ctx context.Context, p *Pending) error {
	if p.ID == "" {
		p.ID = store.NewID()
	}
	body, err := json.Marshal(p.Manifest)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `INSERT INTO site_pending (id, site_id, manifest, source, created_by, created_at, expires_at, allow_secrets) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.SiteID, string(body), p.Source, p.CreatedBy, store.Ms(p.CreatedAt), store.Ms(p.ExpiresAt), b2i(p.AllowSecrets))
	return err
}

func (r repo) pending(ctx context.Context, siteID, id string) (Pending, error) {
	var p Pending
	var raw string
	var created, expires int64
	var allow int
	err := r.db.QueryRow(ctx, `SELECT id, site_id, manifest, source, created_by, created_at, expires_at, allow_secrets FROM site_pending WHERE id = ? AND site_id = ?`, id, siteID).
		Scan(&p.ID, &p.SiteID, &raw, &p.Source, &p.CreatedBy, &created, &expires, &allow)
	if err != nil {
		return Pending{}, err
	}
	p.AllowSecrets = allow != 0
	if err := json.Unmarshal([]byte(raw), &p.Manifest); err != nil {
		return Pending{}, err
	}
	p.CreatedAt, p.ExpiresAt = fromMs(created), fromMs(expires)
	return p, nil
}

func (r repo) dropExpiredPending(ctx context.Context, now time.Time) error {
	_, err := r.db.Exec(ctx, `DELETE FROM site_pending WHERE expires_at < ?`, store.Ms(now))
	return err
}

// ---- tokens ----

func (r repo) addToken(ctx context.Context, t *Token, hash string) error {
	if t.ID == "" {
		t.ID = store.NewID()
	}
	t.CreatedAt = r.now().UTC()
	_, err := r.db.Exec(ctx, `INSERT INTO site_tokens (id, site_id, name, token_hash, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		t.ID, t.SiteID, t.Name, hash, t.CreatedBy, store.Ms(t.CreatedAt))
	return err
}

func (r repo) tokenSite(ctx context.Context, hash string) (Token, error) {
	var t Token
	var created, used int64
	err := r.db.QueryRow(ctx, `SELECT id, site_id, name, created_by, created_at, last_used_at FROM site_tokens WHERE token_hash = ?`, hash).
		Scan(&t.ID, &t.SiteID, &t.Name, &t.CreatedBy, &created, &used)
	t.CreatedAt, t.LastUsedAt = fromMs(created), fromMs(used)
	return t, err
}

func (r repo) touchToken(ctx context.Context, id string) {
	_, _ = r.db.Exec(ctx, `UPDATE site_tokens SET last_used_at = ? WHERE id = ?`, store.Ms(r.now()), id)
}

func (r repo) listTokens(ctx context.Context, siteID string) ([]Token, error) {
	rows, err := r.db.Query(ctx, `SELECT id, site_id, name, created_by, created_at, last_used_at FROM site_tokens WHERE site_id = ? ORDER BY created_at`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Token
	for rows.Next() {
		var t Token
		var created, used int64
		if err := rows.Scan(&t.ID, &t.SiteID, &t.Name, &t.CreatedBy, &created, &used); err != nil {
			return nil, err
		}
		t.CreatedAt, t.LastUsedAt = fromMs(created), fromMs(used)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r repo) deleteToken(ctx context.Context, siteID, id string) error {
	res, err := r.db.Exec(ctx, `DELETE FROM site_tokens WHERE id = ? AND site_id = ?`, id, siteID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- blobs ----

// spaceBlob says whether the space has itself uploaded the blob, and its size.
func (r repo) spaceBlob(ctx context.Context, space, hash string) (int64, bool, error) {
	var size int64
	err := r.db.QueryRow(ctx, `SELECT b.size FROM site_space_blobs s JOIN site_blobs b ON b.hash = s.hash WHERE s.space = ? AND s.hash = ?`, space, hash).Scan(&size)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return size, err == nil, err
}

func (r repo) linkBlob(ctx context.Context, space, hash string) error {
	_, err := r.db.Exec(ctx, `INSERT INTO site_space_blobs (space, hash) VALUES (?, ?) ON CONFLICT (space, hash) DO NOTHING`, space, hash)
	return err
}

func (r repo) addBlob(ctx context.Context, hash string, size int64) error {
	_, err := r.db.Exec(ctx, `INSERT INTO site_blobs (hash, size, created_at) VALUES (?, ?, ?) ON CONFLICT (hash) DO UPDATE SET created_at = excluded.created_at`, hash, size, store.Ms(r.now()))
	return err
}

// blobsBefore are the blobs recorded before t.
func (r repo) blobsBefore(ctx context.Context, t time.Time) ([]string, error) {
	rows, err := r.db.Query(ctx, `SELECT hash FROM site_blobs WHERE created_at < ?`, store.Ms(t))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (r repo) forgetBlob(ctx context.Context, hash string) error {
	if _, err := r.db.Exec(ctx, `DELETE FROM site_space_blobs WHERE hash = ?`, hash); err != nil {
		return err
	}
	_, err := r.db.Exec(ctx, `DELETE FROM site_blobs WHERE hash = ?`, hash)
	return err
}
