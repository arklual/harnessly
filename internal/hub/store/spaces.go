package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const (
	RoleOwner  = "owner"
	RoleMember = "member"
)

// Space is a team. It is not the runtime's workspace (a folder on a machine).
type Space struct {
	ID             string
	Slug           string
	Name           string
	CreatedBy      string
	CreatedAt      time.Time
	OnboardingStep string
}

const spaceCols = `id, slug, name, created_by, created_at, onboarding_step`

func scanSpace(row interface{ Scan(...any) error }) (Space, error) {
	var sp Space
	var created int64
	if err := row.Scan(&sp.ID, &sp.Slug, &sp.Name, &sp.CreatedBy, &created, &sp.OnboardingStep); err != nil {
		return Space{}, err
	}
	sp.CreatedAt = fromMs(created)
	return sp, nil
}

// CreateSpace inserts the space and its first owner. A taken slug is a unique violation (see IsUnique).
func (s Store) CreateSpace(ctx context.Context, sp *Space) error {
	if sp.ID == "" {
		sp.ID = NewID()
	}
	if sp.CreatedAt.IsZero() {
		sp.CreatedAt = time.Now().UTC()
	}
	if sp.OnboardingStep == "" {
		sp.OnboardingStep = "machines"
	}
	if _, err := s.q.Exec(ctx, `INSERT INTO spaces (`+spaceCols+`) VALUES (?, ?, ?, ?, ?, ?)`,
		sp.ID, sp.Slug, sp.Name, sp.CreatedBy, Ms(sp.CreatedAt), sp.OnboardingStep); err != nil {
		return err
	}
	return s.AddMember(ctx, sp.ID, sp.CreatedBy, RoleOwner)
}

func (s Store) SpaceByID(ctx context.Context, id string) (Space, error) {
	return scanSpace(s.q.QueryRow(ctx, `SELECT `+spaceCols+` FROM spaces WHERE id = ?`, id))
}

func (s Store) SpaceBySlug(ctx context.Context, slug string) (Space, error) {
	return scanSpace(s.q.QueryRow(ctx, `SELECT `+spaceCols+` FROM spaces WHERE slug = ?`, slug))
}

func (s Store) UpdateSpace(ctx context.Context, id, name, step string) error {
	_, err := s.q.Exec(ctx, `UPDATE spaces SET name = ?, onboarding_step = ? WHERE id = ?`, name, step, id)
	return err
}

// SpaceWithRole is a space a user belongs to and their role in it.
type SpaceWithRole struct {
	Space
	Role string
}

// SpacesByUser lists the user's spaces, oldest first.
func (s Store) SpacesByUser(ctx context.Context, userID string) ([]SpaceWithRole, error) {
	rows, err := s.q.Query(ctx, `SELECT s.id, s.slug, s.name, s.created_by, s.created_at, s.onboarding_step, m.role
		FROM spaces s JOIN memberships m ON m.space_id = s.id WHERE m.user_id = ? ORDER BY s.created_at, s.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SpaceWithRole
	for rows.Next() {
		var x SpaceWithRole
		var created int64
		if err := rows.Scan(&x.ID, &x.Slug, &x.Name, &x.CreatedBy, &created, &x.OnboardingStep, &x.Role); err != nil {
			return nil, err
		}
		x.CreatedAt = fromMs(created)
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s Store) CountOwnedSpaces(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM memberships WHERE user_id = ? AND role = 'owner'`, userID).Scan(&n)
	return n, err
}

// Member is a user in a space.
type Member struct {
	UserID   string
	Email    string
	Name     string
	Role     string
	JoinedAt time.Time
}

func (s Store) AddMember(ctx context.Context, spaceID, userID, role string) error {
	_, err := s.q.Exec(ctx, `INSERT INTO memberships (space_id, user_id, role, created_at) VALUES (?, ?, ?, ?)`, spaceID, userID, role, Ms(time.Now()))
	return err
}

// Role is the user's role in the space; "" when they are not in it.
func (s Store) Role(ctx context.Context, spaceID, userID string) (string, error) {
	var role string
	err := s.q.QueryRow(ctx, `SELECT role FROM memberships WHERE space_id = ? AND user_id = ?`, spaceID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return role, err
}

func (s Store) Members(ctx context.Context, spaceID string) ([]Member, error) {
	rows, err := s.q.Query(ctx, `SELECT u.id, u.email, u.name, m.role, m.created_at FROM memberships m JOIN users u ON u.id = m.user_id
		WHERE m.space_id = ? ORDER BY m.created_at, u.id`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		var m Member
		var joined int64
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &m.Role, &joined); err != nil {
			return nil, err
		}
		m.JoinedAt = fromMs(joined)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s Store) MemberByUser(ctx context.Context, spaceID, userID string) (Member, error) {
	var m Member
	var joined int64
	err := s.q.QueryRow(ctx, `SELECT u.id, u.email, u.name, m.role, m.created_at FROM memberships m JOIN users u ON u.id = m.user_id
		WHERE m.space_id = ? AND m.user_id = ?`, spaceID, userID).Scan(&m.UserID, &m.Email, &m.Name, &m.Role, &joined)
	m.JoinedAt = fromMs(joined)
	return m, err
}

func (s Store) SetRole(ctx context.Context, spaceID, userID, role string) error {
	_, err := s.q.Exec(ctx, `UPDATE memberships SET role = ? WHERE space_id = ? AND user_id = ?`, role, spaceID, userID)
	return err
}

func (s Store) RemoveMember(ctx context.Context, spaceID, userID string) error {
	_, err := s.q.Exec(ctx, `DELETE FROM memberships WHERE space_id = ? AND user_id = ?`, spaceID, userID)
	return err
}

func (s Store) CountOwners(ctx context.Context, spaceID string) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM memberships WHERE space_id = ? AND role = 'owner'`, spaceID).Scan(&n)
	return n, err
}

// Invite is an invitation into a space: by email (single use, bound to one address) or by link.
type Invite struct {
	ID        string
	SpaceID   string
	Kind      string // "email" or "link"
	Email     string
	TokenHash string
	Role      string
	MaxUses   *int
	Uses      int
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedBy string
	CreatedAt time.Time
}

const inviteCols = `id, space_id, kind, email, token_hash, role, max_uses, uses, expires_at, revoked_at, created_by, created_at`

func scanInvite(row interface{ Scan(...any) error }) (Invite, error) {
	var iv Invite
	var maxUses sql.NullInt64
	var expires, created int64
	var revoked sql.NullInt64
	if err := row.Scan(&iv.ID, &iv.SpaceID, &iv.Kind, &iv.Email, &iv.TokenHash, &iv.Role, &maxUses, &iv.Uses, &expires, &revoked, &iv.CreatedBy, &created); err != nil {
		return Invite{}, err
	}
	if maxUses.Valid {
		n := int(maxUses.Int64)
		iv.MaxUses = &n
	}
	iv.ExpiresAt, iv.RevokedAt, iv.CreatedAt = fromMs(expires), fromNullMs(revoked), fromMs(created)
	return iv, nil
}

func (s Store) CreateInvite(ctx context.Context, iv *Invite) error {
	if iv.ID == "" {
		iv.ID = NewID()
	}
	if iv.CreatedAt.IsZero() {
		iv.CreatedAt = time.Now().UTC()
	}
	var maxUses any
	if iv.MaxUses != nil {
		maxUses = *iv.MaxUses
	}
	_, err := s.q.Exec(ctx, `INSERT INTO invites (`+inviteCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		iv.ID, iv.SpaceID, iv.Kind, iv.Email, iv.TokenHash, iv.Role, maxUses, iv.Uses, Ms(iv.ExpiresAt), nullMs(iv.RevokedAt), iv.CreatedBy, Ms(iv.CreatedAt))
	return err
}

func (s Store) InviteByTokenHash(ctx context.Context, hash string) (Invite, error) {
	return scanInvite(s.q.QueryRow(ctx, `SELECT `+inviteCols+` FROM invites WHERE token_hash = ?`, hash))
}

func (s Store) InviteByID(ctx context.Context, id string) (Invite, error) {
	return scanInvite(s.q.QueryRow(ctx, `SELECT `+inviteCols+` FROM invites WHERE id = ?`, id))
}

// ActiveInvites lists the space's invites that are not revoked, not expired and not used up.
func (s Store) ActiveInvites(ctx context.Context, spaceID string, now time.Time) ([]Invite, error) {
	rows, err := s.q.Query(ctx, `SELECT `+inviteCols+` FROM invites WHERE space_id = ? AND revoked_at IS NULL AND expires_at > ?
		AND (max_uses IS NULL OR uses < max_uses) ORDER BY created_at, id`, spaceID, Ms(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invite
	for rows.Next() {
		iv, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, iv)
	}
	return out, rows.Err()
}

// EmailInvitesFor lists the active email invites addressed to an email, in any space.
func (s Store) EmailInvitesFor(ctx context.Context, email string, now time.Time) ([]Invite, error) {
	rows, err := s.q.Query(ctx, `SELECT `+inviteCols+` FROM invites WHERE kind = 'email' AND email = ? AND revoked_at IS NULL AND expires_at > ?
		AND (max_uses IS NULL OR uses < max_uses) ORDER BY created_at, id`, email, Ms(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invite
	for rows.Next() {
		iv, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, iv)
	}
	return out, rows.Err()
}

// RevokeInvite marks an invite of the space revoked; false when it is not there or already revoked.
func (s Store) RevokeInvite(ctx context.Context, spaceID, id string, at time.Time) (bool, error) {
	res, err := s.q.Exec(ctx, `UPDATE invites SET revoked_at = ? WHERE id = ? AND space_id = ? AND revoked_at IS NULL`, Ms(at), id, spaceID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// UseInvite counts one use of an invite, only while it has uses left, and records who. False: used up or revoked.
func (s Store) UseInvite(ctx context.Context, id, userID string, now time.Time) (bool, error) {
	res, err := s.q.Exec(ctx, `UPDATE invites SET uses = uses + 1 WHERE id = ? AND revoked_at IS NULL AND expires_at > ?
		AND (max_uses IS NULL OR uses < max_uses)`, id, Ms(now))
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	_, err = s.q.Exec(ctx, `INSERT INTO invite_uses (invite_id, user_id, used_at) VALUES (?, ?, ?)`, id, userID, Ms(now))
	return true, err
}

// InviteUsed says whether the user already used the invite.
func (s Store) InviteUsed(ctx context.Context, inviteID, userID string) (bool, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM invite_uses WHERE invite_id = ? AND user_id = ?`, inviteID, userID).Scan(&n)
	return n > 0, err
}

// InviteUserNames returns the name or email of the user who created an invite.
func (s Store) InviterLabel(ctx context.Context, userID string) (string, error) {
	var name, email string
	if err := s.q.QueryRow(ctx, `SELECT name, email FROM users WHERE id = ?`, userID).Scan(&name, &email); err != nil {
		return "", err
	}
	if name != "" {
		return name, nil
	}
	return email, nil
}

// SpaceSSHKey is the SSH key pair of a space: the public half as an authorized_keys line, the private half encrypted.
type SpaceSSHKey struct {
	SpaceID       string
	PublicKey     string
	PrivateKeyEnc string
	CreatedAt     time.Time
}

// SpaceSSHKeyBySpace reads the key pair of a space; ErrNotFound when it has none yet.
func (s Store) SpaceSSHKeyBySpace(ctx context.Context, spaceID string) (SpaceSSHKey, error) {
	var k SpaceSSHKey
	var created int64
	err := s.q.QueryRow(ctx, `SELECT space_id, public_key, private_key_enc, created_at FROM space_ssh_keys WHERE space_id = ?`, spaceID).
		Scan(&k.SpaceID, &k.PublicKey, &k.PrivateKeyEnc, &created)
	if err != nil {
		return SpaceSSHKey{}, err
	}
	k.CreatedAt = fromMs(created)
	return k, nil
}

// CreateSpaceSSHKey stores a key pair unless the space already has one, and says whether this call made it: of two
// hubs' requests at the same moment, one pair wins and both read it back.
func (s Store) CreateSpaceSSHKey(ctx context.Context, k SpaceSSHKey) (bool, error) {
	if k.CreatedAt.IsZero() {
		k.CreatedAt = time.Now().UTC()
	}
	res, err := s.q.Exec(ctx, `INSERT INTO space_ssh_keys (space_id, public_key, private_key_enc, created_at) VALUES (?, ?, ?, ?) ON CONFLICT (space_id) DO NOTHING`,
		k.SpaceID, k.PublicKey, k.PrivateKeyEnc, Ms(k.CreatedAt))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ReplaceSpaceSSHKey puts a new key pair in place of the space's old one (the old private half cannot be read any more).
func (s Store) ReplaceSpaceSSHKey(ctx context.Context, k SpaceSSHKey) error {
	if k.CreatedAt.IsZero() {
		k.CreatedAt = time.Now().UTC()
	}
	_, err := s.q.Exec(ctx, `UPDATE space_ssh_keys SET public_key = ?, private_key_enc = ?, created_at = ? WHERE space_id = ?`,
		k.PublicKey, k.PrivateKeyEnc, Ms(k.CreatedAt), k.SpaceID)
	return err
}
