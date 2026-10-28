// Package relay is the owner's hands on a machine's agents: install, check, log in, deliver a key or a secret, set a
// proxy. Each endpoint checks the person, validates what they sent, and relays one operation to the machine's agent
// over its link (docs/hub/api.md, "Agents on a machine").
//
// What passes through here can be secret: a sealed key, a login code, a proxy address with a password. The hub does not
// log it and does not store it; the handlers never put a request body into an error or a log line.
package relay

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/link"
	"github.com/ly-solutions/harnessly/internal/hub/machines"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const (
	callTimeout    = 15 * time.Second
	checkTimeout   = 30 * time.Second
	keyTimeout     = 30 * time.Second
	maxLoginCode   = 1024
	maxProxyURL    = 2048
	envNameMax     = 64
	refreshTimeout = 45 * time.Second
)

// Deps are what the service is built from.
type Deps struct {
	Auth     *auth.Service
	DB       *store.DB
	Machines *machines.Service
	Reg      *link.Registry
	Log      *slog.Logger
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Timeout, when set, replaces every wait for the agent (tests).
	Timeout time.Duration
}

// Service relays the owner's operations.
type Service struct {
	auth     *auth.Service
	db       *store.DB
	machines *machines.Service
	reg      *link.Registry
	log      *slog.Logger
	now      func() time.Time
	timeout  time.Duration
}

// t is the wait for an operation: the contract's, unless a test shortened it.
func (s *Service) t(d time.Duration) time.Duration {
	if s.timeout > 0 {
		return s.timeout
	}
	return d
}

// New makes the service.
func New(d Deps) *Service {
	s := &Service{auth: d.Auth, db: d.DB, machines: d.Machines, reg: d.Reg, log: d.Log, now: d.Now, timeout: d.Timeout}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// Mount registers the endpoints.
func (s *Service) Mount(mux *http.ServeMux) {
	mux.Handle("GET /api/machines/{machineId}/agents", s.auth.Require(s.agents))
	mux.Handle("POST /api/machines/{machineId}/agents/{provider}/install", s.auth.Require(s.install))
	mux.Handle("POST /api/machines/{machineId}/agents/{provider}/check", s.auth.Require(s.check))
	mux.Handle("POST /api/machines/{machineId}/agents/{provider}/login", s.auth.Require(s.loginStart))
	mux.Handle("POST /api/machines/{machineId}/logins/{loginId}/code", s.auth.Require(s.loginCode))
	mux.Handle("DELETE /api/machines/{machineId}/logins/{loginId}", s.auth.Require(s.loginCancel))
	mux.Handle("POST /api/machines/{machineId}/agents/{provider}/key", s.auth.Require(s.keyDeliver))
	mux.Handle("POST /api/machines/{machineId}/agents/{provider}/key/verify", s.auth.Require(s.keyVerify))
	mux.Handle("GET /api/machines/{machineId}/proxy", s.auth.Require(s.proxyGet))
	mux.Handle("PUT /api/machines/{machineId}/proxy", s.auth.Require(s.proxySet))
	mux.Handle("DELETE /api/machines/{machineId}/proxy", s.auth.Require(s.proxyClear))
	mux.Handle("POST /api/machines/{machineId}/env", s.auth.Require(s.envDeliver))
	mux.Handle("DELETE /api/machines/{machineId}/env/{name}", s.auth.Require(s.envRemove))
	mux.Handle("POST /api/machines/{machineId}/gitkey", s.auth.Require(s.gitkey))
}

// owner is the machine of the request, for a person who owns its space.
func (s *Service) owner(r *http.Request, p *auth.Principal) (store.Machine, error) {
	return s.machines.Authorize(r.Context(), p.User.ID, r.PathValue("machineId"), true)
}

// provider reads {provider}: claude and codex are real, gemini is a card that says «Скоро», anything else is no route.
func provider(r *http.Request) (string, error) {
	switch p := r.PathValue("provider"); p {
	case hubproto.ProviderClaude, hubproto.ProviderCodex:
		return p, nil
	case hubproto.ProviderGemini:
		return "", httpx.ErrProviderOff
	default:
		return "", httpx.ErrNotFound
	}
}

var hexID = regexp.MustCompile(`^[0-9a-f]{32}$`)

func loginID(r *http.Request) (string, error) {
	id := r.PathValue("loginId")
	if !hexID.MatchString(id) {
		return "", httpx.ErrNotFound
	}
	return id, nil
}

// ---- envelopes and names ----

// checkEnvelope checks the shape of a sealed envelope without trying to open it (only the machine can): the hub passes
// on what is well formed and nothing else, so a mistake is told at once and junk does not travel.
func checkEnvelope(e hubproto.Envelope) error {
	std := base64.StdEncoding
	epk, err1 := std.DecodeString(e.Epk)
	nonce, err2 := std.DecodeString(e.Nonce)
	ct, err3 := std.DecodeString(e.Ct)
	const tag = 16
	if e.V != 1 || err1 != nil || err2 != nil || err3 != nil || len(epk) != 32 || len(nonce) != 12 || len(ct) < tag || len(ct) > hubproto.MaxSealed+tag {
		return httpx.Invalid("envelope", "Зашифрованные данные повреждены, попробуйте ещё раз")
	}
	return nil
}

var envName = hubproto.EnvNameRe

// checkEnvName applies the rule the machine applies too (hubproto.CheckEnvName): names that look like a secret or an
// address, and none that would change how the machine runs. The hub says no first, in Russian, so the form can show it.
func checkEnvName(name string) error {
	err := hubproto.CheckEnvName(name)
	if err == nil {
		return nil
	}
	var bad *hubproto.EnvNameError
	if !errors.As(err, &bad) {
		return httpx.Invalid("name", "Это имя нельзя задать")
	}
	switch bad.Fault {
	case hubproto.EnvMalformed:
		return httpx.Invalid("name", "Имя — заглавные латинские буквы, цифры и _, не длиннее 64 символов")
	case hubproto.EnvNotSecret:
		return httpx.Invalid("name", "Имя секрета должно оканчиваться на "+bad.Detail+": так машина отличает секрет от настройки программы")
	default:
		return httpx.Invalid("name", "Это имя занято: оно меняет, как работает машина, или ключи агентов передаются отдельно")
	}
}

// checkProxyURL checks an address the person typed. The agent checks it again and has the last word.
func checkProxyURL(raw string) error {
	if raw == "" || len(raw) > maxProxyURL {
		return httpx.Invalid("url", "Укажите адрес прокси")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return httpx.Invalid("url", "Адрес прокси должен быть вида http://хост:порт или socks5://хост:порт")
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return httpx.Invalid("url", "Поддерживаются http, https и socks5")
	}
	return nil
}

func checkScope(scope string) error {
	switch scope {
	case "all", "claude", "codex":
		return nil
	}
	return httpx.Invalid("scope", "Область — all, claude или codex")
}

// ---- agents ----

func (s *Service) agents(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	m, err := s.owner(r, p)
	if err != nil {
		return err
	}
	var list []json.RawMessage
	if r.URL.Query().Get("refresh") == "1" {
		if list, err = s.machines.RefreshAgents(r.Context(), m); err != nil {
			return err
		}
	} else {
		list = s.machines.Agents(m)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"agents": list})
	return nil
}

func (s *Service) install(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req struct{ From string }
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.From != "" && req.From != "provider" {
		return httpx.Invalid("from", "Можно начать только с шага provider")
	}
	prov, err := provider(r)
	if err != nil {
		return err
	}
	m, err := s.owner(r, p)
	if err != nil {
		return err
	}
	jobID := store.RandomHex(16)
	params := map[string]any{"jobId": jobID, "provider": prov}
	if req.From != "" {
		params["from"] = req.From
	}
	if _, err := s.reg.Do(r.Context(), m.ID, hubproto.OpAgentsInstall, params, s.t(callTimeout)); err != nil {
		return err
	}
	s.auth.Audit(r.Context(), p.User.ID, "agent_install", r, m.ID+" "+prov)
	httpx.WriteJSON(w, http.StatusAccepted, map[string]string{"jobId": jobID})
	return nil
}

func (s *Service) check(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req struct {
		ProxyURL string `json:"proxyUrl"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	prov, err := provider(r)
	if err != nil {
		return err
	}
	m, err := s.owner(r, p)
	if err != nil {
		return err
	}
	params := map[string]any{"provider": prov}
	if req.ProxyURL != "" {
		if err := checkProxyURL(req.ProxyURL); err != nil {
			return httpx.Invalid("proxyUrl", "Адрес прокси должен быть вида http://хост:порт или socks5://хост:порт")
		}
		params["proxyUrl"] = req.ProxyURL
	}
	var check hubproto.ProviderCheck
	if err := s.reg.DoInto(r.Context(), m.ID, hubproto.OpProviderCheck, params, &check, s.t(checkTimeout)); err != nil {
		return err
	}
	if req.ProxyURL == "" {
		// A check with the machine's own settings is the agent's new "access": let the stored status catch up.
		s.refreshLater(m)
	}
	raw, _ := json.Marshal(check)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"check": link.Times(raw)})
	return nil
}

// refreshLater brings the stored statuses up to date without making the person wait.
func (s *Service) refreshLater(m store.Machine) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()
		if _, err := s.machines.RefreshAgents(ctx, m); err != nil {
			s.log.Debug("agent statuses not refreshed", "machine", m.ID, "error", err)
		}
	}()
}

func (s *Service) loginStart(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req struct{ Method string }
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Method != hubproto.MethodLink && req.Method != hubproto.MethodConsole {
		return httpx.Invalid("method", "Способ входа — link или console")
	}
	prov, err := provider(r)
	if err != nil {
		return err
	}
	m, err := s.owner(r, p)
	if err != nil {
		return err
	}
	id := store.RandomHex(16)
	var login hubproto.Login
	err = s.reg.DoInto(r.Context(), m.ID, hubproto.OpLoginStart, map[string]any{"loginId": id, "provider": prov, "method": req.Method}, &login, s.t(callTimeout))
	if err != nil {
		return err
	}
	if login.ID == "" {
		login.ID = id
	}
	// What the machine says goes to the browser: the link only if it is the provider's own page.
	login.Provider, login.Method = prov, req.Method
	login, refused := machines.CleanLogin(login, m.ID)
	if len(refused) > 0 {
		s.log.Warn("login fields refused", "machine", m.ID, "provider", prov, "fields", refused)
	}
	s.auth.Audit(r.Context(), p.User.ID, "agent_login", r, m.ID+" "+prov+" "+req.Method)
	raw, _ := json.Marshal(login)
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"login": link.Times(raw)})
	return nil
}

func (s *Service) loginCode(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req struct{ Code string }
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	id, err := loginID(r)
	if err != nil {
		return err
	}
	code := strings.TrimSpace(req.Code)
	if code == "" || len(code) > maxLoginCode {
		return httpx.Invalid("code", "Вставьте код")
	}
	m, err := s.owner(r, p)
	if err != nil {
		return err
	}
	if _, err := s.reg.Do(r.Context(), m.ID, hubproto.OpLoginCode, map[string]any{"loginId": id, "code": code}, s.t(callTimeout)); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

func (s *Service) loginCancel(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	id, err := loginID(r)
	if err != nil {
		return err
	}
	m, err := s.owner(r, p)
	if err != nil {
		return err
	}
	if _, err := s.reg.Do(r.Context(), m.ID, hubproto.OpLoginCancel, map[string]any{"loginId": id}, s.t(callTimeout)); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// ---- keys ----

func (s *Service) keyDeliver(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req struct{ Envelope hubproto.Envelope }
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	prov, err := provider(r)
	if err != nil {
		return err
	}
	if err := checkEnvelope(req.Envelope); err != nil {
		return err
	}
	m, err := s.owner(r, p)
	if err != nil {
		return err
	}
	var check hubproto.KeyCheck
	if err := s.reg.DoInto(r.Context(), m.ID, hubproto.OpKeyDeliver, map[string]any{"provider": prov, "envelope": req.Envelope}, &check, s.t(keyTimeout)); err != nil {
		return err
	}
	s.auth.Audit(r.Context(), p.User.ID, "agent_key", r, m.ID+" "+prov)
	s.refreshLater(m)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"check": check})
	return nil
}

func (s *Service) keyVerify(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	prov, err := provider(r)
	if err != nil {
		return err
	}
	m, err := s.owner(r, p)
	if err != nil {
		return err
	}
	var check hubproto.KeyCheck
	if err := s.reg.DoInto(r.Context(), m.ID, hubproto.OpKeyVerify, map[string]any{"provider": prov}, &check, s.t(keyTimeout)); err != nil {
		return err
	}
	s.refreshLater(m)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"check": check})
	return nil
}

// ---- proxy ----

func (s *Service) proxyGet(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	m, err := s.owner(r, p)
	if err != nil {
		return err
	}
	return s.proxyCall(w, r, m, hubproto.OpProxyGet, nil)
}

func (s *Service) proxySet(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req struct{ Scope, URL string }
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if err := checkScope(req.Scope); err != nil {
		return err
	}
	if err := checkProxyURL(req.URL); err != nil {
		return err
	}
	m, err := s.owner(r, p)
	if err != nil {
		return err
	}
	s.auth.Audit(r.Context(), p.User.ID, "agent_proxy", r, m.ID+" "+req.Scope)
	return s.proxyCall(w, r, m, hubproto.OpProxySet, map[string]any{"scope": req.Scope, "url": req.URL})
}

func (s *Service) proxyClear(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	scope := r.URL.Query().Get("scope")
	if err := checkScope(scope); err != nil {
		return err
	}
	m, err := s.owner(r, p)
	if err != nil {
		return err
	}
	s.auth.Audit(r.Context(), p.User.ID, "agent_proxy_clear", r, m.ID+" "+scope)
	return s.proxyCall(w, r, m, hubproto.OpProxyClear, map[string]any{"scope": scope})
}

func (s *Service) proxyCall(w http.ResponseWriter, r *http.Request, m store.Machine, op string, params any) error {
	var summary hubproto.ProxySummary
	if err := s.reg.DoInto(r.Context(), m.ID, op, params, &summary, s.t(callTimeout)); err != nil {
		return err
	}
	s.machines.SetProxy(r.Context(), m, summary)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"proxy": summary})
	return nil
}

// ---- secrets ----

func (s *Service) envDeliver(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req struct {
		Name     string
		Envelope hubproto.Envelope
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if err := checkEnvName(req.Name); err != nil {
		return err
	}
	if err := checkEnvelope(req.Envelope); err != nil {
		return err
	}
	m, err := s.owner(r, p)
	if err != nil {
		return err
	}
	return s.envCall(w, r, p, m, hubproto.OpEnvDeliver, map[string]any{"name": req.Name, "envelope": req.Envelope})
}

func (s *Service) envRemove(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	name := r.PathValue("name")
	if !envName.MatchString(name) {
		return httpx.ErrNotFound
	}
	m, err := s.owner(r, p)
	if err != nil {
		return err
	}
	return s.envCall(w, r, p, m, hubproto.OpEnvRemove, map[string]any{"name": name})
}

// envCall relays an env operation. The hub keeps the names the agent says it has and never the values.
func (s *Service) envCall(w http.ResponseWriter, r *http.Request, p *auth.Principal, m store.Machine, op string, params any) error {
	var res struct{ Env []string }
	if err := s.reg.DoInto(r.Context(), m.ID, op, params, &res, s.t(callTimeout)); err != nil {
		return err
	}
	names := make([]string, 0, len(res.Env))
	for _, n := range res.Env {
		if envName.MatchString(n) {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	if err := s.db.S().SetEnvNames(r.Context(), m.ID, names, p.User.ID, s.now().UTC()); err != nil {
		return err
	}
	s.auth.Audit(r.Context(), p.User.ID, "agent_env", r, m.ID+" "+op)
	s.machines.Publish(r.Context(), m)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"env": names})
	return nil
}

func (s *Service) gitkey(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	m, err := s.owner(r, p)
	if err != nil {
		return err
	}
	var res struct {
		PublicKey string `json:"publicKey"`
	}
	if err := s.reg.DoInto(r.Context(), m.ID, hubproto.OpGitkeyEnsure, nil, &res, s.t(callTimeout)); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, res)
	return nil
}
