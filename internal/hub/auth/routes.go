package auth

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// SpaceRef is a space as listed in /api/me and elsewhere.
type SpaceRef struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// Mount registers the sign-in and account endpoints.
func (s *Service) Mount(mux *http.ServeMux) {
	mux.Handle("POST /api/auth/email/start", s.direct(s.emailOn(httpx.Handler(s.emailStart))))
	mux.Handle("POST /api/auth/email/verify", s.direct(s.emailOn(httpx.Handler(s.emailVerify))))
	mux.Handle("POST /api/auth/passkey/login/begin", s.direct(httpx.Handler(s.passkeyLoginBegin)))
	mux.Handle("POST /api/auth/passkey/login/finish", s.direct(httpx.Handler(s.passkeyLoginFinish)))
	mux.Handle("GET /api/auth/oauth/{provider}/start", s.direct(http.HandlerFunc(s.oauthStart)))
	mux.Handle("GET /api/auth/oauth/{provider}/callback", s.direct(http.HandlerFunc(s.oauthCallback)))
	mux.Handle("POST /api/auth/logout", s.direct(httpx.Handler(s.logout)))
	mux.Handle("POST /api/auth/logout/all", s.direct(s.Require(s.logoutAll)))

	mux.Handle("GET /api/me", s.Require(s.me))
	mux.Handle("PATCH /api/me", s.Require(s.patchMe))
	mux.Handle("POST /api/me/passkeys/begin", s.Require(s.passkeyRegisterBegin))
	mux.Handle("POST /api/me/passkeys/finish", s.Require(s.passkeyRegisterFinish))
	mux.Handle("DELETE /api/me/passkeys/{id}", s.Require(s.deletePasskey))
	mux.Handle("DELETE /api/me/identities/{provider}", s.Require(s.deleteIdentity))
	mux.Handle("PUT /api/admin/users/{userId}/disabled", s.Require(s.setDisabled))
}

// emailOn answers 404 on the code sign-in when the hub has it off (HUB_AUTH_EMAIL=off).
func (s *Service) emailOn(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AuthEmailOff {
			httpx.WriteError(w, r, httpx.New(http.StatusNotFound, "email_off", "Вход по коду из письма выключен на этом хабе"))
			return
		}
		h.ServeHTTP(w, r)
	})
}

// direct keeps the hub's own sign-in off the gateway's address: there the gateway is the sign-in.
func (s *Service) direct(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.ProxiedHost(r) {
			httpx.WriteError(w, r, httpx.New(http.StatusNotFound, "proxied", "Вход на этом адресе — через шлюз"))
			return
		}
		h.ServeHTTP(w, r)
	})
}

func (s *Service) logout(w http.ResponseWriter, r *http.Request) error {
	// On the gateway's address there is no session of the hub's to end: the person signs out at the gateway.
	if s.ProxiedHost(r) {
		httpx.NoContent(w)
		return nil
	}
	if c, err := r.Cookie(s.CookieName()); err == nil && c.Value != "" {
		if err := s.db.S().DeleteSession(r.Context(), store.HashToken(c.Value)); err != nil {
			return err
		}
	}
	s.clearCookie(w)
	httpx.NoContent(w)
	return nil
}

// logoutAll ends every session of the person, this one too: on every device, and on the machines they had open.
func (s *Service) logoutAll(w http.ResponseWriter, r *http.Request, p *Principal) error {
	ctx := r.Context()
	if err := s.db.S().DeleteUserSessions(ctx, p.User.ID, ""); err != nil {
		return err
	}
	s.Audit(ctx, p.User.ID, "logout_all", r, "")
	s.sessionsEnded(p.User.ID)
	s.clearCookie(w)
	httpx.NoContent(w)
	return nil
}

// setDisabled switches an account off or back on; only an administrator of the installation may, and not their own.
// Switching off ends every session of the account in the same transaction.
func (s *Service) setDisabled(w http.ResponseWriter, r *http.Request, p *Principal) error {
	if !p.Admin {
		return httpx.ErrNotFound
	}
	var req struct{ Disabled *bool }
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Disabled == nil {
		return httpx.Invalid("disabled", "true or false")
	}
	userID := r.PathValue("userId")
	if userID == p.User.ID {
		return httpx.Conflict("own_account", "Свой аккаунт отключить нельзя")
	}
	ctx := r.Context()
	changed := false
	err := s.db.Tx(ctx, func(st store.Store) error {
		if _, err := st.UserByID(ctx, userID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return httpx.ErrNotFound
			}
			return err
		}
		var err error
		if *req.Disabled {
			changed, err = st.DisableUser(ctx, userID, s.now().UTC())
		} else {
			changed, err = st.EnableUser(ctx, userID)
		}
		return err
	})
	if err != nil {
		return err
	}
	if changed && *req.Disabled {
		s.Audit(ctx, p.User.ID, "account_disabled", r, userID)
		s.sessionsEnded(userID)
	} else if changed {
		s.Audit(ctx, p.User.ID, "account_enabled", r, userID)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"disabled": *req.Disabled})
	return nil
}

func (s *Service) me(w http.ResponseWriter, r *http.Request, p *Principal) error {
	ctx := r.Context()
	st := s.db.S()
	ids, err := st.IdentitiesByUser(ctx, p.User.ID)
	if err != nil {
		return err
	}
	pks, err := st.PasskeysByUser(ctx, p.User.ID)
	if err != nil {
		return err
	}
	spaces, err := st.SpacesByUser(ctx, p.User.ID)
	if err != nil {
		return err
	}
	type identity struct {
		Provider string `json:"provider"`
		Email    string `json:"email"`
	}
	out := struct {
		User       UserView      `json:"user"`
		Identities []identity    `json:"identities"`
		Passkeys   []PasskeyView `json:"passkeys"`
		Spaces     []SpaceRef    `json:"spaces"`
	}{User: s.View(p.User, p.Admin), Identities: []identity{}, Passkeys: []PasskeyView{}, Spaces: []SpaceRef{}}
	for _, i := range ids {
		out.Identities = append(out.Identities, identity{Provider: i.Provider, Email: i.Email})
	}
	for _, pk := range pks {
		out.Passkeys = append(out.Passkeys, passkeyView(pk))
	}
	for _, sp := range spaces {
		out.Spaces = append(out.Spaces, SpaceRef{ID: sp.ID, Slug: sp.Slug, Name: sp.Name, Role: sp.Role})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

func (s *Service) patchMe(w http.ResponseWriter, r *http.Request, p *Principal) error {
	var req struct{ Name *string }
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Name == nil {
		return httpx.Invalid("name", "Укажите имя")
	}
	name := strings.TrimSpace(*req.Name)
	if len([]rune(name)) > 100 {
		return httpx.Invalid("name", "Имя слишком длинное")
	}
	if err := s.db.S().UpdateUserName(r.Context(), p.User.ID, name); err != nil {
		return err
	}
	p.User.Name = name
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"user": s.View(p.User, p.Admin)})
	return nil
}

var errLastMethod = httpx.Conflict("last_method", "Это последний способ входа, его нельзя убрать")

func (s *Service) deletePasskey(w http.ResponseWriter, r *http.Request, p *Principal) error {
	ctx := r.Context()
	err := s.db.Tx(ctx, func(st store.Store) error {
		n, err := st.LoginMethods(ctx, p.User.ID)
		if err != nil {
			return err
		}
		pks, err := st.PasskeysByUser(ctx, p.User.ID)
		if err != nil {
			return err
		}
		mine := false
		for _, pk := range pks {
			mine = mine || pk.ID == r.PathValue("id")
		}
		if !mine {
			return httpx.ErrNotFound
		}
		if n <= 1 {
			return errLastMethod
		}
		if _, err = st.DeletePasskey(ctx, p.User.ID, r.PathValue("id")); err != nil {
			return err
		}
		// Whoever signed in with the removed way is signed out; the session that removed it stays.
		return st.DeleteOtherSessions(ctx, p.User.ID, p.Session.TokenHash)
	})
	if err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

func (s *Service) deleteIdentity(w http.ResponseWriter, r *http.Request, p *Principal) error {
	provider := r.PathValue("provider")
	if rest, ok := strings.CutPrefix(provider, "oidc-"); ok {
		provider = "oidc:" + rest
	}
	ctx := r.Context()
	err := s.db.Tx(ctx, func(st store.Store) error {
		ids, err := st.IdentitiesByUser(ctx, p.User.ID)
		if err != nil {
			return err
		}
		found := false
		for _, i := range ids {
			found = found || i.Provider == provider
		}
		if !found {
			return httpx.ErrNotFound
		}
		n, err := st.LoginMethods(ctx, p.User.ID)
		if err != nil {
			return err
		}
		if n <= 1 {
			return errLastMethod
		}
		if _, err = st.DeleteIdentity(ctx, p.User.ID, provider); err != nil {
			return err
		}
		return st.DeleteOtherSessions(ctx, p.User.ID, p.Session.TokenHash)
	})
	if err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}
