// Package tbotjoin gives a machine the join token of a Teleport machine identity (tbot) once the person the bot belongs
// to approved it in the hub (docs/hub/tbot-join.md). The machine's setup prepared the bot's name, the token's
// name and a key pair; the machine sends the public key over its link. The link proves the machine, not a person, so the
// hub only keeps the request: the person named by the bot's owner trait approves it under their own session, and then
// the hub creates a bound_keypair token for that key through the Teleport operator. The bot and its roles are the
// cluster's: the hub creates neither and widens nothing.
package tbotjoin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/safego"
)

// Switch is the rollout switch that lets a person's machines ask (docs/hub/rollout-switches.md).
const Switch = "tbot-join"

var (
	nameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
	// maxToken keeps the token's name a valid name of a Kubernetes label value and of most hosts' records.
	maxToken = 63
	maxSlug  = 40
)

// Gate says whether a feature is on for a person on a machine (switches.Store).
type Gate interface {
	On(ctx context.Context, feature, person, machine string) bool
}

// GateFunc is a Gate of a function.
type GateFunc func(ctx context.Context, feature, person, machine string) bool

// On calls f.
func (f GateFunc) On(ctx context.Context, feature, person, machine string) bool {
	return f(ctx, feature, person, machine)
}

// Caller reaches a machine over its link (link.Registry).
type Caller interface {
	Do(ctx context.Context, machineID, op string, params any, timeout time.Duration) (json.RawMessage, error)
}

// Deps are what the service is built from.
type Deps struct {
	DB      *store.DB
	Cluster Cluster
	// On is HUB_TBOT_JOIN: off, every request is refused and the endpoints answer 404.
	On   bool
	Gate Gate
	// MayOpen is nil when the person may open the machine (its access list and the owner's own-work rule).
	MayOpen func(ctx context.Context, userID, machineID string) error
	Reg     Caller
	// Require wraps an endpoint in the session check (auth.Service.Require).
	Require func(func(http.ResponseWriter, *http.Request, *auth.Principal) error) httpx.Handler
	// Bell puts a notice into the person's bell; nil: none.
	Bell func(ctx context.Context, machineID, email, key, title, text, link string)
	// Link is the page the notice leads to.
	Link string
	Log  *slog.Logger
	Now  func() time.Time
}

// Service is the machines' requests for tbot join tokens.
type Service struct{ d Deps }

// New makes the service.
func New(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Service{d: d}
}

// Request is a machine's request once its shape was checked.
type Request struct {
	Bot, Token, Host, PublicKey, Fingerprint string
}

func refused(token, code, msg string) hubproto.TbotJoinState {
	return hubproto.TbotJoinState{Token: token, Status: hubproto.TbotJoinRefused, Code: code, Message: msg}
}

// Check reads a request and checks its shape against the machine it came from: the token is <bot>-<machine>, where
// <machine> is the machine's host name or its last parts, and the key is an Ed25519 public key.
func Check(m store.Machine, in hubproto.TbotJoin) (Request, error) {
	bot, token := strings.TrimSpace(in.Bot), strings.TrimSpace(in.Token)
	if !nameRe.MatchString(bot) || len(bot) > maxToken {
		return Request{}, errors.New("the bot's name is not valid")
	}
	slug, ok := strings.CutPrefix(token, bot+"-")
	if !ok || !nameRe.MatchString(slug) || len(slug) > maxSlug || len(token) > maxToken {
		return Request{}, errors.New("the token's name is not <bot>-<machine>")
	}
	host := strings.ToLower(strings.TrimSpace(m.Hostname))
	if i := strings.IndexByte(host, '.'); i > 0 {
		host = host[:i]
	}
	if host == "" || (host != slug && !strings.HasSuffix(host, "-"+slug)) {
		return Request{}, fmt.Errorf("the token names machine %q and the request comes from %q", slug, host)
	}
	key, _, _, rest, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(in.PublicKey)))
	if err != nil || len(strings.TrimSpace(string(rest))) > 0 {
		return Request{}, errors.New("the public key is not one SSH public key")
	}
	if key.Type() != ssh.KeyAlgoED25519 {
		return Request{}, fmt.Errorf("the key is %s, not ssh-ed25519", key.Type())
	}
	pub := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	return Request{Bot: bot, Token: token, Host: strings.TrimSpace(in.Machine), PublicKey: pub, Fingerprint: ssh.FingerprintSHA256(key)}, nil
}

// owner is the person the bot belongs to: the first email of its owner trait that has an account here.
func (s *Service) owner(ctx context.Context, bot string) (store.User, error) {
	emails, err := s.d.Cluster.BotOwners(ctx, bot)
	if err != nil {
		return store.User{}, err
	}
	for _, e := range emails {
		u, err := s.d.DB.S().UserByEmail(ctx, strings.ToLower(strings.TrimSpace(e)))
		if err == nil && u.DisabledAt == nil {
			return u, nil
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return store.User{}, err
		}
	}
	return store.User{}, store.ErrNotFound
}

// Requested takes a machine's tbot.join: it keeps a new request for the person to decide and answers with where the
// request stands.
func (s *Service) Requested(ctx context.Context, m store.Machine, raw json.RawMessage) hubproto.TbotJoinState {
	var in hubproto.TbotJoin
	if err := json.Unmarshal(raw, &in); err != nil {
		return refused("", "invalid_request", "the request was not read")
	}
	if !s.d.On || s.d.Cluster == nil {
		return refused(in.Token, "off", "this hub gives no tbot join tokens")
	}
	req, err := Check(m, in)
	if err != nil {
		return refused(in.Token, "invalid_request", err.Error())
	}
	u, err := s.owner(ctx, req.Bot)
	switch {
	case errors.Is(err, ErrNoBot):
		return refused(req.Token, "no_bot", "the cluster has no bot "+req.Bot)
	case errors.Is(err, store.ErrNotFound):
		return refused(req.Token, "no_owner", "the bot's owner has no account on this hub")
	case err != nil:
		s.d.Log.Warn("tbot join: the cluster was not read", "machine", m.ID, "error", err)
		return refused(req.Token, "unavailable", "the cluster did not answer, the machine asks again later")
	}
	if s.d.Gate == nil || !s.d.Gate.On(ctx, Switch, strings.ToLower(u.Email), m.ID) {
		return refused(req.Token, "off", "tbot join tokens through the hub are not on for this person")
	}
	if s.d.MayOpen != nil && s.d.MayOpen(ctx, u.ID, m.ID) != nil {
		return refused(req.Token, "not_owner", "the bot's owner may not open this machine")
	}
	now := s.d.Now()
	st := s.d.DB.S()
	j, err := st.TbotJoinByKey(ctx, m.ID, req.Token, req.Fingerprint)
	switch {
	case err == nil:
		if err := st.TouchTbotJoin(ctx, j.ID, strings.ToLower(u.Email), m.Name, now); err != nil {
			s.d.Log.Warn("tbot join: not touched", "id", j.ID, "error", err)
		}
		return hubproto.TbotJoinState{Token: j.Token, Status: j.State}
	case !errors.Is(err, store.ErrNotFound):
		s.d.Log.Error("tbot join: not read", "machine", m.ID, "error", err)
		return refused(req.Token, "unavailable", "the hub did not keep the request, the machine asks again later")
	}
	j, err = st.FileTbotJoin(ctx, store.TbotJoin{MachineID: m.ID, MachineName: m.Name, Host: req.Host, Bot: req.Bot, Token: req.Token,
		PublicKey: req.PublicKey, Fingerprint: req.Fingerprint, OwnerEmail: strings.ToLower(u.Email)}, now)
	if err != nil {
		s.d.Log.Error("tbot join: not kept", "machine", m.ID, "error", err)
		return refused(req.Token, "unavailable", "the hub did not keep the request, the machine asks again later")
	}
	s.d.Log.Info("tbot join requested", "machine", m.ID, "token", j.Token, "fingerprint", j.Fingerprint)
	if s.d.Bell != nil {
		name := m.Name
		if name == "" {
			name = m.Hostname
		}
		s.d.Bell(ctx, m.ID, j.OwnerEmail, "tbot-join:"+j.ID, "Новая машина просит доступ",
			fmt.Sprintf("%s просит доступ как %s. Подтвердите в хабе, на экране «Машины».", name, j.Bot), s.d.Link)
	}
	return hubproto.TbotJoinState{Token: j.Token, Status: j.State}
}

// Mount registers the person's endpoints.
func (s *Service) Mount(mux *http.ServeMux) {
	mux.Handle("GET /api/me/tbot-joins", s.d.Require(s.list))
	mux.Handle("POST /api/me/tbot-joins/{id}/approve", s.d.Require(s.decide(store.TbotJoinPending, store.TbotJoinApproved)))
	mux.Handle("POST /api/me/tbot-joins/{id}/decline", s.d.Require(s.decide(store.TbotJoinPending, store.TbotJoinDeclined)))
	mux.Handle("POST /api/me/tbot-joins/{id}/revoke", s.d.Require(s.decide(store.TbotJoinApproved, store.TbotJoinRevoked)))
}

// View is a request as the person's page shows it.
type View struct {
	ID          string     `json:"id"`
	MachineID   string     `json:"machineId"`
	MachineName string     `json:"machineName"`
	Host        string     `json:"host,omitempty"`
	Bot         string     `json:"bot"`
	Token       string     `json:"token"`
	Fingerprint string     `json:"fingerprint"`
	State       string     `json:"state"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	DecidedAt   *time.Time `json:"decidedAt,omitempty"`
}

func viewOf(j store.TbotJoin) View {
	return View{ID: j.ID, MachineID: j.MachineID, MachineName: j.MachineName, Host: j.Host, Bot: j.Bot, Token: j.Token,
		Fingerprint: j.Fingerprint, State: j.State, CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt, DecidedAt: j.DecidedAt}
}

var errOff = httpx.New(http.StatusNotFound, "tbot_join_off", "Выдача входа машинам через хаб не включена")

// list: GET /api/me/tbot-joins, the requests the person decides that wait or were approved.
func (s *Service) list(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	if !s.d.On {
		return errOff
	}
	js, err := s.d.DB.S().TbotJoinsOf(r.Context(), strings.ToLower(p.User.Email))
	if err != nil {
		return err
	}
	out := make([]View, 0, len(js))
	for _, j := range js {
		out = append(out, viewOf(j))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"requests": out})
	return nil
}

// decide moves a request of the person's from one state to another; approving creates the token, revoking deletes it.
// The machine is told when it is linked; otherwise it hears it the next time it asks.
func (s *Service) decide(from, to string) func(http.ResponseWriter, *http.Request, *auth.Principal) error {
	return func(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
		if !s.d.On || s.d.Cluster == nil {
			return errOff
		}
		ctx := r.Context()
		st := s.d.DB.S()
		j, err := st.TbotJoinByID(ctx, r.PathValue("id"))
		if errors.Is(err, store.ErrNotFound) || (err == nil && !strings.EqualFold(j.OwnerEmail, p.User.Email)) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if j.State != from {
			return httpx.New(http.StatusConflict, "decided", "Этот запрос уже решён")
		}
		if to != store.TbotJoinRevoked {
			// Approving or declining needs what asking needed: the switch, the machine, the bot still the person's.
			// Revoking needs only that the request is theirs.
			if s.d.Gate == nil || !s.d.Gate.On(ctx, Switch, strings.ToLower(p.User.Email), j.MachineID) {
				return errOff
			}
			if s.d.MayOpen != nil {
				if err := s.d.MayOpen(ctx, p.User.ID, j.MachineID); err != nil {
					return err
				}
			}
		}
		now := s.d.Now()
		switch to {
		case store.TbotJoinApproved:
			u, err := s.owner(ctx, j.Bot)
			if errors.Is(err, ErrNoBot) || errors.Is(err, store.ErrNotFound) || (err == nil && u.ID != p.User.ID) {
				return httpx.New(http.StatusConflict, "not_owner", "Бот "+j.Bot+" больше не ваш в кластере")
			}
			if err != nil {
				return s.unavailable(err)
			}
			if err := s.d.Cluster.PutToken(ctx, j.Token, j.Bot, j.PublicKey); err != nil {
				if errors.Is(err, ErrNotOurs) {
					return httpx.New(http.StatusConflict, "token_exists", "Токен "+j.Token+" уже заведён в кластере не хабом")
				}
				return s.unavailable(err)
			}
		case store.TbotJoinRevoked:
			if err := s.d.Cluster.DeleteToken(ctx, j.Token); err != nil && !errors.Is(err, ErrNotOurs) {
				return s.unavailable(err)
			}
		}
		ok, err := st.MoveTbotJoin(ctx, j.ID, from, to, p.User.ID, now)
		if err != nil {
			return err
		}
		if !ok {
			return httpx.New(http.StatusConflict, "decided", "Этот запрос уже решён")
		}
		if to == store.TbotJoinApproved {
			// The token now answers this key only: what was approved for it before no longer holds.
			others, err := st.ApprovedTbotJoins(ctx, j.Token)
			if err == nil {
				for _, o := range others {
					if o.ID != j.ID {
						if _, err := st.MoveTbotJoin(ctx, o.ID, store.TbotJoinApproved, store.TbotJoinRevoked, p.User.ID, now); err == nil {
							s.tell(o.MachineID, hubproto.TbotJoinState{Token: o.Token, Status: hubproto.TbotJoinRevoked})
						}
					}
				}
			}
		}
		s.d.Log.Info("tbot join decided", "id", j.ID, "machine", j.MachineID, "token", j.Token, "state", to, "by", p.User.ID)
		s.tell(j.MachineID, hubproto.TbotJoinState{Token: j.Token, Status: to})
		j.State, j.UpdatedAt, j.DecidedAt = to, now, &now
		httpx.WriteJSON(w, http.StatusOK, viewOf(j))
		return nil
	}
}

func (s *Service) unavailable(err error) error {
	s.d.Log.Warn("tbot join: the cluster refused", "error", err)
	return httpx.New(http.StatusBadGateway, "cluster_unavailable", "Кластер не принял изменение, попробуйте ещё раз")
}

// tell sends the machine the hub's word if it is linked.
func (s *Service) tell(machineID string, state hubproto.TbotJoinState) {
	if s.d.Reg == nil {
		return
	}
	safego.Go("tbotjoin tell", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, err := s.d.Reg.Do(ctx, machineID, hubproto.OpTbotJoined, state, 15*time.Second); err != nil && !errors.Is(err, httpx.ErrMachineOffline) {
			s.d.Log.Warn("tbot.joined not delivered", "machine", machineID, "error", err)
		}
	})
}
