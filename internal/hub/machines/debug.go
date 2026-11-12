package machines

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/machaccess"
	"github.com/ly-solutions/harnessly/internal/hub/ratelimit"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/tenant"
)

// Debug mode (docs/hub/own-work.md, «Debug mode»): under the owner's rule a role manages a machine without reading it;
// when something on it must be looked at, a person with the role opens it for machaccess.DebugFor with a reason. The
// opening is journaled, the owner is told by mail, and the owner sees every opening of the machine in its card.

// errNoDebug: the person either opens the machine anyway or has no role that debugging would stand for.
var errNoDebug = httpx.New(409, "no_debug", "Отладка здесь не нужна или недоступна: её берёт только админ или руководитель, которого не пускает правило владельца")

// debugKeep is how far back the card shows the openings of a machine.
const debugKeep = 90 * 24 * time.Hour

// DebugOpening is one opening as the API gives it.
type DebugOpening struct {
	ID     string    `json:"id"`
	UserID string    `json:"userId"`
	Name   string    `json:"name"`
	Email  string    `json:"email"`
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
	Until  time.Time `json:"until"`
}

// debugOpen is POST /api/machines/{id}/debug {reason}: open someone's machine for a while, with a reason.
func (s *Service) debugOpen(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	p := t.Person
	ctx := r.Context()
	m := *t.Machine
	var req struct {
		Reason string `json:"reason"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	reason := strings.TrimSpace(req.Reason)
	if n := utf8.RuneCountInString(reason); n < 10 || n > 500 {
		return httpx.Invalid("reason", "Причина — от 10 до 500 знаков: её прочтёт владелец машины")
	}
	may, err := s.acc.MayDebug(ctx, p.User.ID, m)
	if err != nil {
		return err
	}
	if !may {
		return errNoDebug
	}
	if ok, retry := s.lim.Allow("debug:"+p.User.ID, 10, time.Hour); !ok {
		return httpx.RateLimited(ratelimit.RetrySeconds(retry))
	}
	now := s.now()
	o := store.DebugOpening{MachineID: m.ID, UserID: p.User.ID, Reason: reason, Created: now, Expires: now.Add(machaccess.DebugFor)}
	err = s.db.Tx(ctx, func(st store.Store) error {
		if err := st.AddDebugOpening(ctx, &o); err != nil {
			return err
		}
		return s.journal(ctx, st, p, m, "machine.debug_open", nil, map[string]any{"reason": reason, "until": o.Expires.UTC()})
	})
	if err != nil {
		return err
	}
	s.tellOwner(ctx, p.User.Name, p.User.Email, m, reason, o.Expires)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"opening": DebugOpening{
		ID: o.ID, UserID: p.User.ID, Name: p.User.Name, Email: p.User.Email, Reason: reason, At: o.Created.UTC(), Until: o.Expires.UTC(),
	}})
	return nil
}

// debugList is GET /api/machines/{id}/debug: the openings of the machine for its owner, for the people who manage it,
// and for the administrators of the installation.
func (s *Service) debugList(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	p := t.Person
	ctx := r.Context()
	m := *t.Machine
	if m.CreatedBy != p.User.ID {
		admin, err := s.auth.IsInstanceAdmin(ctx, p.User.ID)
		if err != nil {
			return err
		}
		if !admin {
			if err := manage(ctx, s.db.S(), p, m); err != nil {
				return err
			}
		}
	}
	st := s.db.S()
	rows, err := st.DebugOpenings(ctx, m.ID, s.now().Add(-debugKeep), 200)
	if err != nil {
		return err
	}
	out := make([]DebugOpening, 0, len(rows))
	names := map[string]store.User{}
	for _, o := range rows {
		u, seen := names[o.UserID]
		if !seen {
			u, _ = st.UserByID(ctx, o.UserID)
			names[o.UserID] = u
		}
		out = append(out, DebugOpening{ID: o.ID, UserID: o.UserID, Name: u.Name, Email: u.Email, Reason: o.Reason, At: o.Created.UTC(), Until: o.Expires.UTC()})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"openings": out})
	return nil
}

// tellOwner mails the machine's owner about an opening. A failure is logged: the journal and the card keep the opening.
func (s *Service) tellOwner(ctx context.Context, name, email string, m store.Machine, reason string, until time.Time) {
	if s.notice == nil || m.CreatedBy == "" {
		return
	}
	owner, err := s.db.S().UserByID(ctx, m.CreatedBy)
	if err != nil || owner.Email == "" {
		return
	}
	who := strings.TrimSpace(name)
	if who == "" {
		who = email
	}
	machine := m.Name
	if machine == "" {
		machine = m.ID
	}
	subject := who + " открыл вашу машину " + machine + " для отладки"
	body := who + " (" + email + ") открыл машину " + machine + " и видит её чаты до " + until.UTC().Format("15:04 UTC") +
		".\n\nПричина: " + reason + "\n\nВсе такие открытия видны в карточке машины."
	link := strings.TrimRight(s.cfg.PublicURL, "/") + "/machines?m=" + m.ID
	if err := s.notice(ctx, owner.Email, subject, body, link); err != nil {
		s.log.Warn("debug opening: owner not told", "machine", m.ID, "error", err)
	}
}
