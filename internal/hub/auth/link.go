package auth

import (
	"context"
	"errors"
	"net/mail"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// ErrAccountExists is the sign-in of a provider that does not verify emails (Yandex ID, an OIDC provider that says
// email_verified=false) meeting an account that already has the email: it never joins by email.
var ErrAccountExists = httpx.New(409, "account_exists", "Аккаунт с этой почтой уже есть: войдите по почте и привяжите этот вход в настройках")

// errNoEmail is a provider that gave no usable email.
var errNoEmail = errors.New("the provider returned no email")

const (
	providerEmail = "email"
)

// identityInfo is what a sign-in method tells about the person.
type identityInfo struct {
	Provider string // email, github, yandex, oidc:<id>
	Subject  string
	Email    string
	Name     string
	// EmailVerified says the provider vouches that the person owns Email.
	EmailVerified bool
}

// resolveOpts says in which situation an identity arrives.
type resolveOpts struct {
	// LinkTo is the signed-in user the identity is added to (intent=link); nil when signing in.
	LinkTo *store.User
	// Next is the page the person asked for; an invitation address in it lets them sign up in invite mode.
	Next string
}

// NormalizeEmail lower-cases and validates an address; false when it is not one.
func NormalizeEmail(raw string) (string, bool) {
	e := strings.ToLower(strings.TrimSpace(raw))
	if e == "" || len(e) > 254 || strings.ContainsAny(e, " \t\r\n<>,;\"") {
		return "", false
	}
	a, err := mail.ParseAddress(e)
	if err != nil || a.Address != e {
		return "", false
	}
	at := strings.LastIndexByte(e, '@')
	domain := e[at+1:]
	if at < 1 || !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return "", false
	}
	return e, true
}

// inviteToken returns the token of an /invite/<token> path, else "".
func inviteToken(next string) string {
	rest, ok := strings.CutPrefix(SafeNext(next), "/invite/")
	if !ok {
		return ""
	}
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

// ErrFirstAccountUnverified is the very first sign-in of an installation that names no admin coming from a provider
// that does not verify emails: that account would be the administrator, and nobody has proved who it is.
var ErrFirstAccountUnverified = httpx.New(403, "first_account_unverified", "Первый вход в хаб должен подтвердить почту: войдите по коду из письма")

// signupAllowed applies HUB_SIGNUP. In invite mode only the admins, the very first account of an installation that
// names no admin, and people with a live invitation (to their address, or a link they arrive with) may create an
// account. When HUB_ADMIN_EMAILS names someone the empty user table is not a door: the first stranger to find the hub
// before the administrator signs in would otherwise get a working account. What is bound to an address (an admin's
// address, an invitation to an address) counts only when the sign-in method proved the address: an identity that
// merely says so (Yandex ID, an OIDC provider with email_verified=false) gets in by an open door or an invitation
// link, and by nothing else.
func (s *Service) signupAllowed(ctx context.Context, st store.Store, email, next string, verified bool) error {
	n, err := st.CountUsers(ctx)
	if err != nil {
		return err
	}
	noAdminNamed := len(s.cfg.AdminEmails) == 0
	if n == 0 && noAdminNamed && !verified {
		return ErrFirstAccountUnverified
	}
	if s.cfg.Signup == "open" || (verified && (s.cfg.IsAdminEmail(email) || (n == 0 && noAdminNamed))) {
		return nil
	}
	now := s.now()
	if verified {
		if ivs, err := st.EmailInvitesFor(ctx, email, now); err != nil {
			return err
		} else if len(ivs) > 0 {
			return nil
		}
	}
	if tok := inviteToken(next); tok != "" {
		iv, err := st.InviteByTokenHash(ctx, store.HashToken(tok))
		if err == nil && iv.RevokedAt == nil && iv.ExpiresAt.After(now) && (iv.MaxUses == nil || iv.Uses < *iv.MaxUses) &&
			(iv.Kind == "link" || (verified && iv.Email == email)) {
			return nil
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
	}
	return httpx.ErrSignupClosed
}

// adminFor says whether u administers the installation: the flag on the account, or an address on HUB_ADMIN_EMAILS
// that the account has proved. An account that only claims an admin's address (it was made by a provider that does
// not verify emails) is not the admin.
func (s *Service) adminFor(ctx context.Context, st store.Store, u store.User) (bool, error) {
	if u.IsAdmin {
		return true, nil
	}
	if !s.cfg.IsAdminEmail(u.Email) {
		return false, nil
	}
	ids, err := st.IdentitiesByUser(ctx, u.ID)
	if err != nil {
		return false, err
	}
	for _, i := range ids {
		if i.EmailVerified && i.Email == u.Email {
			return true, nil
		}
	}
	return false, nil
}

// IsInstanceAdmin says whether the person administers this installation (adminFor, by id); false for an unknown person.
func (s *Service) IsInstanceAdmin(ctx context.Context, userID string) (bool, error) {
	if userID == "" {
		return false, nil
	}
	st := s.db.S()
	u, err := st.UserByID(ctx, userID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return s.adminFor(ctx, st, u)
}

// RequireTrustedMachine is the rule of a hub that has no HUB_RUNTIME_DOMAIN: a machine's page runs on the hub's own
// origin, where its script acts with the session of whoever opens it (and the hub's CSRF checks pass: it is the same
// origin). That is safe only for a machine as trusted as the hub itself, so such a hub opens the machines its
// administrators connected (createdBy is the one who did) and no others, however open its sign-up is. In subdomain mode
// every machine has an origin of its own and the rule does not apply.
func (s *Service) RequireTrustedMachine(ctx context.Context, createdBy string) error {
	if s.cfg.SubdomainMode() {
		return nil
	}
	admin, err := s.IsInstanceAdmin(ctx, createdBy)
	if err != nil {
		return err
	}
	if !admin {
		return httpx.ErrMachineUntrusted
	}
	return nil
}

// resolve decides which account an identity belongs to, and makes the account when it has to.
//
//   - an identity that exists signs its user in;
//   - while signed in (LinkTo), a new identity is added to that user;
//   - a new identity whose provider verifies the email joins the account with that email, or makes one;
//   - a provider that does not verify emails never joins an account by email: the email being taken is
//     ErrAccountExists, and otherwise it makes a new account;
//   - when a verified owner of an email meets an account made by an unverified provider, that account's identities,
//     passkeys and sessions are removed first (pre-hijacking: whoever made it did not prove the email).
func (s *Service) resolve(ctx context.Context, info identityInfo, o resolveOpts) (u store.User, isNew bool, err error) {
	info.Email = strings.ToLower(strings.TrimSpace(info.Email))
	err = s.db.Tx(ctx, func(st store.Store) error {
		u, isNew = store.User{}, false
		existing, err := st.IdentityBySubject(ctx, info.Provider, info.Subject)
		switch {
		case err == nil:
			if o.LinkTo != nil && existing.UserID != o.LinkTo.ID {
				return httpx.Conflict("identity_taken", "Этот вход уже привязан к другому аккаунту")
			}
			u, err = st.UserByID(ctx, existing.UserID)
			return err
		case !errors.Is(err, store.ErrNotFound):
			return err
		}

		if o.LinkTo != nil {
			if err := st.CreateIdentity(ctx, &store.Identity{UserID: o.LinkTo.ID, Provider: info.Provider, Subject: info.Subject, Email: info.Email, EmailVerified: info.EmailVerified}); err != nil {
				return err
			}
			u = *o.LinkTo
			return nil
		}

		if info.Email == "" {
			return errNoEmail
		}
		owner, err := st.UserByEmail(ctx, info.Email)
		switch {
		case err == nil:
			if !info.EmailVerified {
				return ErrAccountExists
			}
			if err := s.claimVerified(ctx, st, owner, info.Email); err != nil {
				return err
			}
			u = owner
		case errors.Is(err, store.ErrNotFound):
			// One account is made at a time: the count below decides who is the first, and on Postgres (READ
			// COMMITTED) two parallel first sign-ins would both read zero and both be the administrator.
			if err := st.LockAccountCreation(ctx); err != nil {
				return err
			}
			if err := s.signupAllowed(ctx, st, info.Email, o.Next, info.EmailVerified); err != nil {
				return err
			}
			count, err := st.CountUsers(ctx)
			if err != nil {
				return err
			}
			// The administrator is whoever proved an address on HUB_ADMIN_EMAILS, or, when none is named, the first
			// account (signupAllowed has seen to it that it is a proven one).
			admin := info.EmailVerified && (s.cfg.IsAdminEmail(info.Email) || (len(s.cfg.AdminEmails) == 0 && count == 0))
			u = store.User{Email: info.Email, Name: truncate(strings.TrimSpace(info.Name), 100), CreatedAt: s.now().UTC(), IsAdmin: admin}
			if err := st.CreateUser(ctx, &u); err != nil {
				return err
			}
			isNew = true
		default:
			return err
		}
		return st.CreateIdentity(ctx, &store.Identity{UserID: u.ID, Provider: info.Provider, Subject: info.Subject, Email: info.Email, EmailVerified: info.EmailVerified})
	})
	if err != nil {
		return store.User{}, false, err
	}
	// The instance admin list may have grown since the account was made.
	if !u.IsAdmin {
		if admin, err := s.adminFor(ctx, s.db.S(), u); err != nil {
			s.log.Warn("admin status not checked", "user", u.ID, "error", err)
		} else if admin {
			u.IsAdmin = true
			_ = s.db.S().SetUserAdmin(ctx, u.ID, true)
		}
	}
	return u, isNew, nil
}

// claimVerified is run when someone who proved the email arrives at an existing account. If the account has no
// identity that proved it, nobody honest vouches for what is on it, so it is emptied.
func (s *Service) claimVerified(ctx context.Context, st store.Store, owner store.User, email string) error {
	ids, err := st.IdentitiesByUser(ctx, owner.ID)
	if err != nil {
		return err
	}
	for _, i := range ids {
		if i.EmailVerified && i.Email == email {
			return nil
		}
	}
	if len(ids) == 0 {
		// An account with no sign-in at all (its last method was removed): nothing to protect.
		pks, err := st.PasskeysByUser(ctx, owner.ID)
		if err != nil {
			return err
		}
		if len(pks) == 0 {
			return nil
		}
	}
	s.log.Warn("account made with an unverified email was claimed by its verified owner; its credentials were removed", "user", owner.ID)
	return st.WipeUserCredentials(ctx, owner.ID)
}
