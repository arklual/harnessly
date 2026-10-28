package auth

import (
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
	mux.Handle("POST /api/auth/email/start", httpx.Handler(s.emailStart))
	mux.Handle("POST /api/auth/email/verify", httpx.Handler(s.emailVerify))
	mux.Handle("POST /api/auth/passkey/login/begin", httpx.Handler(s.passkeyLoginBegin))
	mux.Handle("POST /api/auth/passkey/login/finish", httpx.Handler(s.passkeyLoginFinish))
	mux.HandleFunc("GET /api/auth/oauth/{provider}/start", s.oauthStart)
	mux.HandleFunc("GET /api/auth/oauth/{provider}/callback", s.oauthCallback)
	mux.Handle("POST /api/auth/logout", httpx.Handler(s.logout))

	mux.Handle("GET /api/me", s.Require(s.me))
	mux.Handle("PATCH /api/me", s.Require(s.patchMe))
	mux.Handle("POST /api/me/passkeys/begin", s.Require(s.passkeyRegisterBegin))
	mux.Handle("POST /api/me/passkeys/finish", s.Require(s.passkeyRegisterFinish))
	mux.Handle("DELETE /api/me/passkeys/{id}", s.Require(s.deletePasskey))
	mux.Handle("DELETE /api/me/identities/{provider}", s.Require(s.deleteIdentity))
}

func (s *Service) logout(w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(s.CookieName()); err == nil && c.Value != "" {
		if err := s.db.S().DeleteSession(r.Context(), store.HashToken(c.Value)); err != nil {
			return err
		}
	}
	s.clearCookie(w)
	httpx.NoContent(w)
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
		_, err = st.DeletePasskey(ctx, p.User.ID, r.PathValue("id"))
		return err
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
		_, err = st.DeleteIdentity(ctx, p.User.ID, provider)
		return err
	})
	if err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}
