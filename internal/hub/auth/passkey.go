package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/ratelimit"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

const passkeyTTL = 5 * time.Minute

// waUser is a hub user as go-webauthn sees one. The user handle is the user id.
type waUser struct {
	u     store.User
	creds []webauthn.Credential
}

func (w waUser) WebAuthnID() []byte                         { return []byte(w.u.ID) }
func (w waUser) WebAuthnName() string                       { return w.u.Email }
func (w waUser) WebAuthnCredentials() []webauthn.Credential { return w.creds }
func (w waUser) WebAuthnDisplayName() string {
	if w.u.Name != "" {
		return w.u.Name
	}
	return w.u.Email
}

func credentialID(id []byte) string { return base64.RawURLEncoding.EncodeToString(id) }

func (s *Service) loadWAUser(ctx context.Context, st store.Store, u store.User) (waUser, error) {
	pks, err := st.PasskeysByUser(ctx, u.ID)
	if err != nil {
		return waUser{}, err
	}
	out := waUser{u: u}
	for _, pk := range pks {
		var c webauthn.Credential
		if err := json.Unmarshal([]byte(pk.Credential), &c); err != nil {
			s.log.Warn("a stored passkey cannot be read", "passkey", pk.ID, "error", err)
			continue
		}
		out.creds = append(out.creds, c)
	}
	return out, nil
}

func (s *Service) passkeysOn() error {
	if s.wa == nil {
		return httpx.ErrProviderOff
	}
	return nil
}

// ceremony stores a begun WebAuthn ceremony and returns its id.
func (s *Service) storeCeremony(ctx context.Context, kind, userID string, session *webauthn.SessionData) (string, error) {
	raw, err := json.Marshal(session)
	if err != nil {
		return "", err
	}
	id := store.RandomBase32(16)
	now := s.now().UTC()
	err = s.db.S().CreateChallenge(ctx, store.Challenge{ID: id, Kind: kind, Email: userID, Data: string(raw), ExpiresAt: now.Add(passkeyTTL), CreatedAt: now})
	return id, err
}

// takeCeremony uses a stored ceremony once.
func (s *Service) takeCeremony(ctx context.Context, kind, id string) (store.Challenge, webauthn.SessionData, error) {
	st := s.db.S()
	ch, err := st.ChallengeByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return ch, webauthn.SessionData{}, httpx.ErrExpired
	}
	if err != nil {
		return ch, webauthn.SessionData{}, err
	}
	if used, err := st.ConsumeChallenge(ctx, id); err != nil {
		return ch, webauthn.SessionData{}, err
	} else if !used || ch.Kind != kind || !s.now().Before(ch.ExpiresAt) {
		return ch, webauthn.SessionData{}, httpx.ErrExpired
	}
	var session webauthn.SessionData
	if err := json.Unmarshal([]byte(ch.Data), &session); err != nil {
		return ch, session, httpx.ErrExpired
	}
	return ch, session, nil
}

func (s *Service) passkeyLoginBegin(w http.ResponseWriter, r *http.Request) error {
	if err := s.passkeysOn(); err != nil {
		return err
	}
	if ok, wait := s.lim.Allow("passkey-begin:"+ratelimit.IPKey(s.ClientIP(r)), 60, 10*time.Minute); !ok {
		return httpx.RateLimited(ratelimit.RetrySeconds(wait))
	}
	assertion, session, err := s.wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationPreferred))
	if err != nil {
		return err
	}
	id, err := s.storeCeremony(r.Context(), "passkey_login", "", session)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"challengeId": id, "publicKey": assertion.Response})
	return nil
}

func (s *Service) passkeyLoginFinish(w http.ResponseWriter, r *http.Request) error {
	if err := s.passkeysOn(); err != nil {
		return err
	}
	var req struct {
		ChallengeID string          `json:"challengeId"`
		Credential  json.RawMessage `json:"credential"`
		Next        string          `json:"next"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.ChallengeID == "" || len(req.Credential) == 0 {
		return httpx.Invalid("credential", "Нет данных ключа")
	}
	if err := s.limitSignin(r); err != nil {
		return err
	}
	ctx := r.Context()
	_, session, err := s.takeCeremony(ctx, "passkey_login", req.ChallengeID)
	if err != nil {
		return err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(req.Credential)
	if err != nil {
		return httpx.Invalid("credential", "Не удалось прочитать ответ ключа")
	}
	st := s.db.S()
	var user store.User
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		u, err := st.UserByID(ctx, string(userHandle))
		if err != nil {
			return nil, err
		}
		wu, err := s.loadWAUser(ctx, st, u)
		if err != nil {
			return nil, err
		}
		user = u
		return wu, nil
	}
	_, cred, err := s.wa.ValidatePasskeyLogin(handler, session, parsed)
	if err != nil {
		s.log.Info("passkey sign-in refused", "error", err)
		return httpx.ErrUnauthenticated
	}
	pk, err := st.PasskeyByCredential(ctx, credentialID(cred.ID))
	if err != nil || pk.UserID != user.ID {
		return httpx.ErrUnauthenticated
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	if err := st.UpdatePasskeyUsed(ctx, pk.ID, string(raw), s.now().UTC()); err != nil {
		return err
	}
	return s.signedIn(ctx, w, r, user, false, "passkey", req.Next)
}

// PasskeyView is a passkey as the API sends it.
type PasskeyView struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	CreatedAt  string  `json:"createdAt"`
	LastUsedAt *string `json:"lastUsedAt"`
}

func passkeyView(p store.Passkey) PasskeyView {
	return PasskeyView{ID: p.ID, Name: p.Name, CreatedAt: httpx.Time(p.CreatedAt), LastUsedAt: httpx.TimePtr(p.LastUsedAt)}
}

func (s *Service) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request, p *Principal) error {
	if err := s.passkeysOn(); err != nil {
		return err
	}
	ctx := r.Context()
	wu, err := s.loadWAUser(ctx, s.db.S(), p.User)
	if err != nil {
		return err
	}
	var exclude []protocol.CredentialDescriptor
	for i := range wu.creds {
		exclude = append(exclude, wu.creds[i].Descriptor())
	}
	creation, session, err := s.wa.BeginRegistration(wu,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithExclusions(exclude))
	if err != nil {
		return err
	}
	id, err := s.storeCeremony(ctx, "passkey_register", p.User.ID, session)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"challengeId": id, "publicKey": creation.Response})
	return nil
}

func (s *Service) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request, p *Principal) error {
	if err := s.passkeysOn(); err != nil {
		return err
	}
	var req struct {
		ChallengeID string          `json:"challengeId"`
		Credential  json.RawMessage `json:"credential"`
		Name        string          `json:"name"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.ChallengeID == "" || len(req.Credential) == 0 {
		return httpx.Invalid("credential", "Нет данных ключа")
	}
	ctx := r.Context()
	ch, session, err := s.takeCeremony(ctx, "passkey_register", req.ChallengeID)
	if err != nil {
		return err
	}
	if ch.Email != p.User.ID {
		return httpx.ErrForbidden
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(req.Credential)
	if err != nil {
		return httpx.Invalid("credential", "Не удалось прочитать ответ ключа")
	}
	st := s.db.S()
	wu, err := s.loadWAUser(ctx, st, p.User)
	if err != nil {
		return err
	}
	cred, err := s.wa.CreateCredential(wu, session, parsed)
	if err != nil {
		s.log.Info("passkey registration refused", "error", err)
		return httpx.New(http.StatusBadRequest, "invalid_request", "Не удалось добавить ключ").With("field", "credential")
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	name := truncate(strings.TrimSpace(req.Name), 60)
	if name == "" {
		name = "Passkey"
	}
	pk := store.Passkey{UserID: p.User.ID, CredentialID: credentialID(cred.ID), Credential: string(raw), Name: name, CreatedAt: s.now().UTC()}
	if err := st.CreatePasskey(ctx, &pk); err != nil {
		if store.IsUnique(err) {
			return httpx.Conflict("passkey_exists", "Этот ключ уже добавлен")
		}
		return err
	}
	s.Audit(ctx, p.User.ID, "passkey_added", r, pk.ID)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"passkey": passkeyView(pk)})
	return nil
}
