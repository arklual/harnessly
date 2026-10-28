package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// User is an account.
type User struct {
	ID          string
	Email       string
	Name        string
	IsAdmin     bool
	CreatedAt   time.Time
	LastLoginAt *time.Time
}

const userCols = `id, email, name, is_admin, created_at, last_login_at`

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	var admin int
	var created int64
	var last sql.NullInt64
	if err := row.Scan(&u.ID, &u.Email, &u.Name, &admin, &created, &last); err != nil {
		return User{}, err
	}
	u.IsAdmin, u.CreatedAt, u.LastLoginAt = admin != 0, fromMs(created), fromNullMs(last)
	return u, nil
}

// CreateUser inserts u (its ID and CreatedAt are filled when empty). A taken email is a unique violation.
func (s Store) CreateUser(ctx context.Context, u *User) error {
	if u.ID == "" {
		u.ID = NewID()
	}
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now().UTC()
	}
	_, err := s.q.Exec(ctx, `INSERT INTO users (id, email, name, is_admin, created_at) VALUES (?, ?, ?, ?, ?)`,
		u.ID, u.Email, u.Name, boolInt(u.IsAdmin), Ms(u.CreatedAt))
	return err
}

func (s Store) UserByID(ctx context.Context, id string) (User, error) {
	return scanUser(s.q.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

func (s Store) UserByEmail(ctx context.Context, email string) (User, error) {
	return scanUser(s.q.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE email = ?`, email))
}

// accountLockKey is the advisory lock that serialises making accounts ("hubuser" as a number).
const accountLockKey int64 = 0x687562757365

// LockAccountCreation makes the rest of the transaction the only one that creates an account, until it ends: the number
// of users read after it is the number there will be before this one is made. On Postgres that is a transaction-level
// advisory lock (a plain count under READ COMMITTED lets two first sign-ins both see zero). SQLite begins every
// transaction with BEGIN IMMEDIATE, so its writers are in line already. It must be called inside DB.Tx.
func (s Store) LockAccountCreation(ctx context.Context) error {
	tx, ok := s.q.(txQ)
	if !ok {
		return errors.New("store: LockAccountCreation needs a transaction")
	}
	if tx.driver != Postgres {
		return nil
	}
	_, err := s.q.Exec(ctx, `SELECT pg_advisory_xact_lock(?)`, accountLockKey)
	return err
}

func (s Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s Store) UpdateUserName(ctx context.Context, id, name string) error {
	_, err := s.q.Exec(ctx, `UPDATE users SET name = ? WHERE id = ?`, name, id)
	return err
}

func (s Store) SetUserAdmin(ctx context.Context, id string, admin bool) error {
	_, err := s.q.Exec(ctx, `UPDATE users SET is_admin = ? WHERE id = ?`, boolInt(admin), id)
	return err
}

func (s Store) TouchLogin(ctx context.Context, id string, at time.Time) error {
	_, err := s.q.Exec(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, Ms(at), id)
	return err
}

// Identity is one way a user signs in: (provider, subject) is unique across the hub.
type Identity struct {
	ID            string
	UserID        string
	Provider      string
	Subject       string
	Email         string
	EmailVerified bool
	CreatedAt     time.Time
}

const identityCols = `id, user_id, provider, subject, email, email_verified, created_at`

func scanIdentity(row interface{ Scan(...any) error }) (Identity, error) {
	var i Identity
	var verified int
	var created int64
	if err := row.Scan(&i.ID, &i.UserID, &i.Provider, &i.Subject, &i.Email, &verified, &created); err != nil {
		return Identity{}, err
	}
	i.EmailVerified, i.CreatedAt = verified != 0, fromMs(created)
	return i, nil
}

func (s Store) CreateIdentity(ctx context.Context, i *Identity) error {
	if i.ID == "" {
		i.ID = NewID()
	}
	if i.CreatedAt.IsZero() {
		i.CreatedAt = time.Now().UTC()
	}
	_, err := s.q.Exec(ctx, `INSERT INTO identities (`+identityCols+`) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		i.ID, i.UserID, i.Provider, i.Subject, i.Email, boolInt(i.EmailVerified), Ms(i.CreatedAt))
	return err
}

func (s Store) IdentityBySubject(ctx context.Context, provider, subject string) (Identity, error) {
	return scanIdentity(s.q.QueryRow(ctx, `SELECT `+identityCols+` FROM identities WHERE provider = ? AND subject = ?`, provider, subject))
}

func (s Store) IdentitiesByUser(ctx context.Context, userID string) ([]Identity, error) {
	rows, err := s.q.Query(ctx, `SELECT `+identityCols+` FROM identities WHERE user_id = ? ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Identity
	for rows.Next() {
		i, err := scanIdentity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// DeleteIdentity removes the user's identity of a provider; false when there was none.
func (s Store) DeleteIdentity(ctx context.Context, userID, provider string) (bool, error) {
	res, err := s.q.Exec(ctx, `DELETE FROM identities WHERE user_id = ? AND provider = ?`, userID, provider)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// WipeUserCredentials removes every identity, passkey and session of a user: what a verified owner of the email does
// to an account that was made with an email nobody had verified (pre-hijacking).
func (s Store) WipeUserCredentials(ctx context.Context, userID string) error {
	for _, q := range []string{
		`DELETE FROM identities WHERE user_id = ?`,
		`DELETE FROM passkeys WHERE user_id = ?`,
		`DELETE FROM sessions WHERE user_id = ?`,
	} {
		if _, err := s.q.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	return nil
}

// LoginMethods counts the ways a user can still sign in: identities and passkeys.
func (s Store) LoginMethods(ctx context.Context, userID string) (int, error) {
	var a, b int
	if err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM identities WHERE user_id = ?`, userID).Scan(&a); err != nil {
		return 0, err
	}
	if err := s.q.QueryRow(ctx, `SELECT COUNT(*) FROM passkeys WHERE user_id = ?`, userID).Scan(&b); err != nil {
		return 0, err
	}
	return a + b, nil
}

// Passkey is a WebAuthn credential; Credential is go-webauthn's JSON.
type Passkey struct {
	ID           string
	UserID       string
	CredentialID string
	Credential   string
	Name         string
	CreatedAt    time.Time
	LastUsedAt   *time.Time
}

const passkeyCols = `id, user_id, credential_id, credential, name, created_at, last_used_at`

func scanPasskey(row interface{ Scan(...any) error }) (Passkey, error) {
	var p Passkey
	var created int64
	var last sql.NullInt64
	if err := row.Scan(&p.ID, &p.UserID, &p.CredentialID, &p.Credential, &p.Name, &created, &last); err != nil {
		return Passkey{}, err
	}
	p.CreatedAt, p.LastUsedAt = fromMs(created), fromNullMs(last)
	return p, nil
}

func (s Store) CreatePasskey(ctx context.Context, p *Passkey) error {
	if p.ID == "" {
		p.ID = NewID()
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}
	_, err := s.q.Exec(ctx, `INSERT INTO passkeys (id, user_id, credential_id, credential, name, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		p.ID, p.UserID, p.CredentialID, p.Credential, p.Name, Ms(p.CreatedAt))
	return err
}

func (s Store) PasskeyByCredential(ctx context.Context, credentialID string) (Passkey, error) {
	return scanPasskey(s.q.QueryRow(ctx, `SELECT `+passkeyCols+` FROM passkeys WHERE credential_id = ?`, credentialID))
}

func (s Store) PasskeysByUser(ctx context.Context, userID string) ([]Passkey, error) {
	rows, err := s.q.Query(ctx, `SELECT `+passkeyCols+` FROM passkeys WHERE user_id = ? ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Passkey
	for rows.Next() {
		p, err := scanPasskey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s Store) UpdatePasskeyUsed(ctx context.Context, id, credential string, at time.Time) error {
	_, err := s.q.Exec(ctx, `UPDATE passkeys SET credential = ?, last_used_at = ? WHERE id = ?`, credential, Ms(at), id)
	return err
}

// DeletePasskey removes a user's passkey by its id; false when it is not theirs or not there.
func (s Store) DeletePasskey(ctx context.Context, userID, id string) (bool, error) {
	res, err := s.q.Exec(ctx, `DELETE FROM passkeys WHERE user_id = ? AND id = ?`, userID, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Session is a signed-in browser: kind "hub" on the hub's origin, "run" on a machine's own origin.
type Session struct {
	TokenHash  string
	Kind       string
	UserID     string
	MachineID  string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
	IP         string
	UserAgent  string
}

const sessionCols = `token_hash, kind, user_id, machine_id, created_at, expires_at, last_seen_at, ip, user_agent`

func scanSession(row interface{ Scan(...any) error }) (Session, error) {
	var x Session
	var machine sql.NullString
	var created, expires, seen int64
	if err := row.Scan(&x.TokenHash, &x.Kind, &x.UserID, &machine, &created, &expires, &seen, &x.IP, &x.UserAgent); err != nil {
		return Session{}, err
	}
	x.MachineID = machine.String
	x.CreatedAt, x.ExpiresAt, x.LastSeenAt = fromMs(created), fromMs(expires), fromMs(seen)
	return x, nil
}

func (s Store) CreateSession(ctx context.Context, x Session) error {
	_, err := s.q.Exec(ctx, `INSERT INTO sessions (`+sessionCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		x.TokenHash, x.Kind, x.UserID, nullStr(x.MachineID), Ms(x.CreatedAt), Ms(x.ExpiresAt), Ms(x.LastSeenAt), x.IP, x.UserAgent)
	return err
}

func (s Store) SessionByHash(ctx context.Context, hash string) (Session, error) {
	return scanSession(s.q.QueryRow(ctx, `SELECT `+sessionCols+` FROM sessions WHERE token_hash = ?`, hash))
}

func (s Store) TouchSession(ctx context.Context, hash string, at time.Time) error {
	_, err := s.q.Exec(ctx, `UPDATE sessions SET last_seen_at = ? WHERE token_hash = ?`, Ms(at), hash)
	return err
}

func (s Store) DeleteSession(ctx context.Context, hash string) error {
	_, err := s.q.Exec(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hash)
	return err
}

// DeleteUserSessions ends every session of a user (kind "" for all kinds).
func (s Store) DeleteUserSessions(ctx context.Context, userID, kind string) error {
	if kind == "" {
		_, err := s.q.Exec(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
		return err
	}
	_, err := s.q.Exec(ctx, `DELETE FROM sessions WHERE user_id = ? AND kind = ?`, userID, kind)
	return err
}

// DeleteMachineSessions ends the runtime-origin sessions of a machine.
func (s Store) DeleteMachineSessions(ctx context.Context, machineID string) error {
	_, err := s.q.Exec(ctx, `DELETE FROM sessions WHERE machine_id = ?`, machineID)
	return err
}

// PurgeExpired removes sessions, challenges and used or expired registration codes past their time.
func (s Store) PurgeExpired(ctx context.Context, now time.Time) error {
	ms := Ms(now)
	for _, q := range []struct {
		sql string
		arg int64
	}{
		{`DELETE FROM sessions WHERE expires_at < ?`, ms},
		{`DELETE FROM auth_challenges WHERE expires_at < ?`, ms},
		{`DELETE FROM registration_codes WHERE expires_at < ? AND local = 0`, ms - int64(24*time.Hour/time.Millisecond)},
		{`DELETE FROM auth_events WHERE created_at < ?`, ms - int64(180*24*time.Hour/time.Millisecond)},
	} {
		if _, err := s.q.Exec(ctx, q.sql, q.arg); err != nil {
			return err
		}
	}
	return nil
}

// Challenge is a short-lived thing a sign-in step waits for: an email code, a passkey ceremony, an OAuth state.
type Challenge struct {
	ID        string
	Kind      string
	Email     string
	CodeHash  string
	Data      string
	Attempts  int
	ExpiresAt time.Time
	CreatedAt time.Time
	IP        string
}

const challengeCols = `id, kind, email, code_hash, data, attempts, expires_at, created_at, ip`

func (s Store) CreateChallenge(ctx context.Context, c Challenge) error {
	_, err := s.q.Exec(ctx, `INSERT INTO auth_challenges (`+challengeCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Kind, c.Email, c.CodeHash, c.Data, c.Attempts, Ms(c.ExpiresAt), Ms(c.CreatedAt), c.IP)
	return err
}

func scanChallenge(row interface{ Scan(...any) error }) (Challenge, error) {
	var c Challenge
	var expires, created int64
	if err := row.Scan(&c.ID, &c.Kind, &c.Email, &c.CodeHash, &c.Data, &c.Attempts, &expires, &created, &c.IP); err != nil {
		return Challenge{}, err
	}
	c.ExpiresAt, c.CreatedAt = fromMs(expires), fromMs(created)
	return c, nil
}

func (s Store) ChallengeByID(ctx context.Context, id string) (Challenge, error) {
	return scanChallenge(s.q.QueryRow(ctx, `SELECT `+challengeCols+` FROM auth_challenges WHERE id = ?`, id))
}

// LatestChallenge is the newest challenge of a kind for an email.
func (s Store) LatestChallenge(ctx context.Context, kind, email string) (Challenge, error) {
	return scanChallenge(s.q.QueryRow(ctx, `SELECT `+challengeCols+` FROM auth_challenges WHERE kind = ? AND email = ? ORDER BY created_at DESC, id LIMIT 1`, kind, email))
}

// ConsumeChallenge deletes a challenge and reports whether this call was the one that removed it, so a state or a
// ceremony is used once even when two requests race.
func (s Store) ConsumeChallenge(ctx context.Context, id string) (bool, error) {
	res, err := s.q.Exec(ctx, `DELETE FROM auth_challenges WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s Store) DeleteChallengesByEmail(ctx context.Context, kind, email string) error {
	_, err := s.q.Exec(ctx, `DELETE FROM auth_challenges WHERE kind = ? AND email = ?`, kind, email)
	return err
}

// TakeAttempt spends one of the max tries a challenge allows, before the code is compared: it counts the try and
// returns the total, or ok=false when all of them are already spent. The check and the count are one statement, so
// parallel requests cannot try more codes than max between them (a count made after the comparison lets them all
// compare first). ErrNotFound when the challenge is gone.
func (s Store) TakeAttempt(ctx context.Context, id string, max int) (n int, ok bool, err error) {
	err = s.q.QueryRow(ctx, `UPDATE auth_challenges SET attempts = attempts + 1 WHERE id = ? AND attempts < ? RETURNING attempts`, id, max).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := s.ChallengeByID(ctx, id); err != nil {
			return 0, false, err
		}
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return n, true, nil
}

// AuthEvent is one line of the sign-in audit trail.
type AuthEvent struct {
	ID        string
	UserID    string
	Kind      string
	IP        string
	UserAgent string
	Detail    string
	CreatedAt time.Time
}

func (s Store) AddAuthEvent(ctx context.Context, e AuthEvent) error {
	if e.ID == "" {
		e.ID = NewID()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	_, err := s.q.Exec(ctx, `INSERT INTO auth_events (id, user_id, kind, ip, user_agent, detail, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.ID, nullStr(e.UserID), e.Kind, e.IP, e.UserAgent, e.Detail, Ms(e.CreatedAt))
	return err
}

func (s Store) AuthEventsByUser(ctx context.Context, userID string, limit int) ([]AuthEvent, error) {
	rows, err := s.q.Query(ctx, `SELECT id, kind, ip, user_agent, detail, created_at FROM auth_events WHERE user_id = ? ORDER BY created_at DESC, id LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuthEvent
	for rows.Next() {
		e := AuthEvent{UserID: userID}
		var created int64
		if err := rows.Scan(&e.ID, &e.Kind, &e.IP, &e.UserAgent, &e.Detail, &created); err != nil {
			return nil, err
		}
		e.CreatedAt = fromMs(created)
		out = append(out, e)
	}
	return out, rows.Err()
}
