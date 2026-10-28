package spaces

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/ratelimit"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

const (
	inviteDays      = 7
	maxEmailInvites = 20
	// activeInviteCap bounds the invitations a space can have open at once.
	activeInviteCap  = 200
	defaultLinkUses  = 20
	maxLinkUses      = 1000
	maxRememberedURL = 1000
)

// InviteView is an invitation as the API sends it.
type InviteView struct {
	ID        string  `json:"id"`
	Kind      string  `json:"kind"`
	Email     *string `json:"email"`
	Role      string  `json:"role"`
	URL       string  `json:"url,omitempty"`
	Sent      *bool   `json:"sent,omitempty"`
	MaxUses   *int    `json:"maxUses"`
	Uses      int     `json:"uses"`
	ExpiresAt string  `json:"expiresAt"`
	CreatedAt string  `json:"createdAt"`
}

func inviteView(iv store.Invite) InviteView {
	v := InviteView{ID: iv.ID, Kind: iv.Kind, Role: iv.Role, MaxUses: iv.MaxUses, Uses: iv.Uses,
		ExpiresAt: httpx.Time(iv.ExpiresAt), CreatedAt: httpx.Time(iv.CreatedAt)}
	if iv.Kind == "email" {
		e := iv.Email
		v.Email = &e
	}
	return v
}

// newToken is a 128-bit invitation token.
func newToken() string { return store.RandomBase32(16) }

func (s *Service) inviteURL(token string) string { return s.cfg.PublicURL + "/invite/" + token }

func (s *Service) inviteByEmail(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req struct{ Emails []string }
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if len(req.Emails) == 0 {
		return httpx.Invalid("emails", "Укажите хотя бы один адрес")
	}
	if len(req.Emails) > maxEmailInvites {
		return httpx.Invalid("emails", "За раз можно пригласить не больше 20 человек")
	}
	seen := map[string]bool{}
	var emails []string
	for _, raw := range req.Emails {
		e, ok := auth.NormalizeEmail(raw)
		if !ok {
			return httpx.Invalid("emails", "Некорректный адрес: "+truncateForMessage(raw))
		}
		if !seen[e] {
			seen[e] = true
			emails = append(emails, e)
		}
	}
	ctx := r.Context()
	spaceID := r.PathValue("spaceId")
	if err := s.Owner(ctx, p.User.ID, spaceID); err != nil {
		return err
	}
	// Mail leaves the hub on the owner's word: bound how much of it one owner can make.
	for range emails {
		if ok, retry := s.lim.Allow("invite:"+p.User.ID, 60, time.Hour); !ok {
			return httpx.RateLimited(ratelimit.RetrySeconds(retry))
		}
	}
	st := s.db.S()
	sp, err := st.SpaceByID(ctx, spaceID)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	active, err := st.ActiveInvites(ctx, spaceID, now)
	if err != nil {
		return err
	}
	if len(active)+len(emails) > activeInviteCap {
		return httpx.Conflict("invite_limit", "Слишком много открытых приглашений, отзовите ненужные")
	}
	members, err := st.Members(ctx, spaceID)
	if err != nil {
		return err
	}
	isMember := map[string]bool{}
	for _, m := range members {
		isMember[m.Email] = true
	}
	inviter, err := st.InviterLabel(ctx, p.User.ID)
	if err != nil {
		return err
	}

	out := []InviteView{}
	for _, email := range emails {
		if isMember[email] {
			continue // already in: nothing to send, nothing to show
		}
		token := newToken()
		one := 1
		iv := store.Invite{
			SpaceID: spaceID, Kind: "email", Email: email, TokenHash: store.HashToken(token), Role: store.RoleMember,
			MaxUses: &one, ExpiresAt: now.Add(inviteDays * 24 * time.Hour), CreatedBy: p.User.ID, CreatedAt: now,
		}
		err := s.db.Tx(ctx, func(tx store.Store) error {
			// A second invitation to the same address replaces the first: only the newest link works.
			for _, old := range active {
				if old.Kind == "email" && old.Email == email {
					if _, err := tx.RevokeInvite(ctx, spaceID, old.ID, now); err != nil {
						return err
					}
				}
			}
			return tx.CreateInvite(ctx, &iv)
		})
		if err != nil {
			return err
		}
		link := s.inviteURL(token)
		sent, mailErr := s.mailer.SendInvite(ctx, email, inviter, sp.Name, link, iv.ExpiresAt)
		if mailErr != nil {
			// The invitation exists and the owner gets its link below, so a mail that did not leave is not a failure.
			s.log.Warn("invitation mail failed", "space", spaceID, "error", mailErr)
			sent = false
		}
		v := inviteView(iv)
		v.URL, v.Sent = link, &sent
		out = append(out, v)
	}
	s.auth.Audit(ctx, p.User.ID, "invite_created", r, spaceID+" email x"+strconv.Itoa(len(out)))
	s.events.PublishJSON(spaceID, "members", struct{}{}, false)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"invites": out})
	return nil
}

func (s *Service) inviteByLink(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req struct {
		MaxUses *int
		Days    *int
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	maxUses, days := defaultLinkUses, inviteDays
	if req.MaxUses != nil {
		if *req.MaxUses < 1 || *req.MaxUses > maxLinkUses {
			return httpx.Invalid("maxUses", "Число переходов — от 1 до 1000")
		}
		maxUses = *req.MaxUses
	}
	if req.Days != nil {
		if *req.Days < 1 || *req.Days > inviteDays {
			return httpx.Invalid("days", "Срок — от 1 до 7 дней")
		}
		days = *req.Days
	}
	ctx := r.Context()
	spaceID := r.PathValue("spaceId")
	if err := s.Owner(ctx, p.User.ID, spaceID); err != nil {
		return err
	}
	st := s.db.S()
	now := s.now().UTC()
	active, err := st.ActiveInvites(ctx, spaceID, now)
	if err != nil {
		return err
	}
	if req.MaxUses == nil && req.Days == nil {
		for _, iv := range active {
			if iv.Kind != "link" {
				continue
			}
			v := inviteView(iv)
			v.URL = s.rememberedURL(iv.ID)
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"invite": v})
			return nil
		}
	}
	if len(active) >= activeInviteCap {
		return httpx.Conflict("invite_limit", "Слишком много открытых приглашений, отзовите ненужные")
	}
	token := newToken()
	iv := store.Invite{
		SpaceID: spaceID, Kind: "link", TokenHash: store.HashToken(token), Role: store.RoleMember, MaxUses: &maxUses,
		ExpiresAt: now.Add(time.Duration(days) * 24 * time.Hour), CreatedBy: p.User.ID, CreatedAt: now,
	}
	if err := st.CreateInvite(ctx, &iv); err != nil {
		return err
	}
	link := s.inviteURL(token)
	s.rememberURL(iv.ID, link)
	s.auth.Audit(ctx, p.User.ID, "invite_created", r, spaceID+" link")
	s.events.PublishJSON(spaceID, "members", struct{}{}, false)
	v := inviteView(iv)
	v.URL = link
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"invite": v})
	return nil
}

func (s *Service) rememberURL(id, url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.linkURLs) >= maxRememberedURL {
		s.linkURLs = map[string]string{}
	}
	s.linkURLs[id] = url
}

func (s *Service) rememberedURL(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.linkURLs[id]
}

func (s *Service) forgetURL(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.linkURLs, id)
}

func (s *Service) listInvites(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	spaceID := r.PathValue("spaceId")
	if err := s.Owner(ctx, p.User.ID, spaceID); err != nil {
		return err
	}
	ivs, err := s.db.S().ActiveInvites(ctx, spaceID, s.now())
	if err != nil {
		return err
	}
	out := make([]InviteView, 0, len(ivs))
	for _, iv := range ivs {
		out = append(out, inviteView(iv))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"invites": out})
	return nil
}

func (s *Service) revokeInvite(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	spaceID, id := r.PathValue("spaceId"), r.PathValue("inviteId")
	if err := s.Owner(ctx, p.User.ID, spaceID); err != nil {
		return err
	}
	ok, err := s.db.S().RevokeInvite(ctx, spaceID, id, s.now().UTC())
	if err != nil {
		return err
	}
	if !ok {
		return httpx.ErrNotFound
	}
	s.forgetURL(id)
	s.auth.Audit(ctx, p.User.ID, "invite_revoked", r, spaceID+" "+id)
	s.events.PublishJSON(spaceID, "members", struct{}{}, false)
	httpx.NoContent(w)
	return nil
}

// maskEmail turns r.ivanov@company.ru into r***@company.ru.
func maskEmail(email string) string {
	at := strings.LastIndexByte(email, '@')
	if at < 1 {
		return "***"
	}
	first := []rune(email[:at])[0]
	return string(first) + "***" + email[at:]
}

// valid says whether an invitation can still be used.
func valid(iv store.Invite, now time.Time) bool {
	return iv.RevokedAt == nil && iv.ExpiresAt.After(now) && (iv.MaxUses == nil || iv.Uses < *iv.MaxUses)
}

func (s *Service) invitationByToken(r *http.Request) (store.Invite, error) {
	token := r.PathValue("token")
	if len(token) < 20 || len(token) > 64 {
		return store.Invite{}, httpx.ErrNotFound
	}
	iv, err := s.db.S().InviteByTokenHash(r.Context(), store.HashToken(token))
	if errors.Is(err, store.ErrNotFound) {
		return store.Invite{}, httpx.ErrNotFound
	}
	return iv, err
}

func (s *Service) peekInvite(w http.ResponseWriter, r *http.Request) error {
	ip := ratelimit.IPKey(s.auth.ClientIP(r))
	if ok, retry := s.lim.Allow("invite-peek:"+ip, 60, 10*time.Minute); !ok {
		return httpx.RateLimited(ratelimit.RetrySeconds(retry))
	}
	iv, err := s.invitationByToken(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	st := s.db.S()
	sp, err := st.SpaceByID(ctx, iv.SpaceID)
	if err != nil {
		return err
	}
	// The link goes to people the owner chose, but a link gets forwarded: the inviter is shown by name, else by a
	// masked address.
	by, err := st.UserByID(ctx, iv.CreatedBy)
	if err != nil {
		return err
	}
	inviter := by.Name
	if inviter == "" {
		inviter = maskEmail(by.Email)
	}
	var email *string
	if iv.Kind == "email" {
		m := maskEmail(iv.Email)
		email = &m
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"spaceName": sp.Name, "inviterName": inviter, "kind": iv.Kind, "email": email, "valid": valid(iv, s.now()),
	})
	return nil
}

func (s *Service) acceptInvite(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	ctx := r.Context()
	if ok, retry := s.lim.Allow("invite-accept:"+p.User.ID, 30, 10*time.Minute); !ok {
		return httpx.RateLimited(ratelimit.RetrySeconds(retry))
	}
	iv, err := s.invitationByToken(r)
	if err != nil {
		return err
	}
	st := s.db.S()
	now := s.now().UTC()
	sp, err := st.SpaceByID(ctx, iv.SpaceID)
	if err != nil {
		return err
	}
	ref := func(role string) map[string]any {
		return map[string]any{"space": auth.SpaceRef{ID: sp.ID, Slug: sp.Slug, Name: sp.Name, Role: role}}
	}

	// Someone who is in the space already is not an error: the link just takes them there.
	if role, err := st.Role(ctx, sp.ID, p.User.ID); err != nil {
		return err
	} else if role != "" {
		httpx.WriteJSON(w, http.StatusOK, ref(role))
		return nil
	}
	if !valid(iv, now) {
		return httpx.ErrExpired
	}
	if iv.Kind == "email" {
		ok, err := s.ownsEmail(r, p, iv.Email)
		if err != nil {
			return err
		}
		if !ok {
			return httpx.New(http.StatusForbidden, "forbidden", "Приглашение отправлено на другой адрес: войдите как "+maskEmail(iv.Email)).
				With("reason", "email_mismatch").With("email", maskEmail(iv.Email))
		}
	}
	err = s.db.Tx(ctx, func(tx store.Store) error {
		if ok, err := tx.UseInvite(ctx, iv.ID, p.User.ID, now); err != nil {
			return err
		} else if !ok {
			return httpx.ErrExpired
		}
		return tx.AddMember(ctx, sp.ID, p.User.ID, iv.Role)
	})
	if store.IsUnique(err) {
		// Accepted twice at once: the other request won, the person is in.
		httpx.WriteJSON(w, http.StatusOK, ref(iv.Role))
		return nil
	}
	if err != nil {
		return err
	}
	s.auth.Audit(ctx, p.User.ID, "invite_accepted", r, sp.ID+" "+iv.ID)
	s.events.PublishJSON(sp.ID, "members", struct{}{}, false)
	httpx.WriteJSON(w, http.StatusOK, ref(iv.Role))
	return nil
}

// ownsEmail says whether the signed-in person has proved the address: an email invitation is a promise to that
// mailbox, so an account whose email nobody verified (Yandex ID) cannot use it.
func (s *Service) ownsEmail(r *http.Request, p *auth.Principal, email string) (bool, error) {
	if p.User.Email != email {
		return false, nil
	}
	ids, err := s.db.S().IdentitiesByUser(r.Context(), p.User.ID)
	if err != nil {
		return false, err
	}
	for _, i := range ids {
		if i.EmailVerified && i.Email == email {
			return true, nil
		}
	}
	return false, nil
}

func truncateForMessage(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > 40 {
		return string(r[:40]) + "…"
	}
	return string(r)
}
