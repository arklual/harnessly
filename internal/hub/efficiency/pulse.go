package efficiency

import (
	"net/http"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/features"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/orgs"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// PulseAnswer is one week of a person's pulse.
type PulseAnswer struct {
	Week     string `json:"week"`
	Speed    int    `json:"speed"`
	Quality  int    `json:"quality"`
	Friction int    `json:"friction"`
}

// Pulse is the answer of GET .../efficiency/pulse (IEffPulse in web/app/types/efficiency.ts). Only the person's own
// answers are ever read: nobody else, an administrator included, gets an endpoint to see them.
type Pulse struct {
	Week     string        `json:"week"` // Monday of the current week in the organization's time zone
	Answered bool          `json:"answered"`
	Answers  []PulseAnswer `json:"answers"`
}

const pulseHistory = 12

func (s *Service) orgLoc(o store.Org) *time.Location {
	if l, err := time.LoadLocation(o.Timezone); err == nil && o.Timezone != "" {
		return l
	}
	return time.UTC
}

func (s *Service) weekOf(o store.Org) string {
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	d := now.In(s.orgLoc(o))
	d = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, d.Location())
	return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7)).Format("2006-01-02")
}

func (s *Service) pulseOrg(r *http.Request, p *auth.Principal) (store.Org, error) {
	o, err := orgs.Resolve(r.Context(), s.DB.S(), p, r.PathValue("org"))
	if err != nil {
		return o, err
	}
	if s.Features == nil || !s.Features.On(r.Context(), o.ID, features.Efficiency) {
		return o, httpx.ErrNotFound
	}
	return o, nil
}

func (s *Service) pulseGet(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	o, err := s.pulseOrg(r, p)
	if err != nil {
		return err
	}
	rows, err := s.DB.S().EffPulsesOf(r.Context(), p.User.ID, pulseHistory)
	if err != nil {
		return err
	}
	out := Pulse{Week: s.weekOf(o), Answers: make([]PulseAnswer, 0, len(rows))}
	for _, a := range rows {
		out.Answers = append(out.Answers, PulseAnswer{Week: a.Week, Speed: a.Speed, Quality: a.Quality, Friction: a.Friction})
		if a.Week == out.Week {
			out.Answered = true
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

func (s *Service) pulsePut(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req struct {
		Speed, Quality, Friction int
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	o, err := s.pulseOrg(r, p)
	if err != nil {
		return err
	}
	for field, v := range map[string]int{"speed": req.Speed, "quality": req.Quality, "friction": req.Friction} {
		if v < 1 || v > 5 {
			return httpx.Invalid(field, "Оценка от 1 до 5")
		}
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	if err := s.DB.S().PutEffPulse(r.Context(), p.User.ID, s.weekOf(o), req.Speed, req.Quality, req.Friction, now.UnixMilli()); err != nil {
		return err
	}
	return s.pulseGet(w, r, p)
}
