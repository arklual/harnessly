package analytics

import (
	"context"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/events"
	"github.com/ly-solutions/harnessly/internal/hub/features"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// What the hub does on its own for the live screens (docs/hub/analytics.md, «Live»): it ticks the numbers of the
// organizations somebody is watching, tells the stream about a new signal, and checks the signal rules of the
// organizations that have the new interface on. Without a bus nothing is published; without a feature gate the checker
// does not run, so a hub that has none of this switched on does exactly what it did.

const (
	// tickEvery is how often an organization somebody listens to gets usage.tick.
	tickEvery = 10 * time.Second
	// checkEvery is how often the signal rules run over every organization.
	checkEvery = 5 * time.Minute
	// triggerAfter is how long after an event the signal rules run for its organization; events that come within it
	// share one run.
	triggerAfter = 10 * time.Second
	// activeWindow is how recently a session must have been updated to count as a chat going on now.
	activeWindow = 15 * time.Minute
)

// UseEvents gives the service the organization's event bus; without it nothing is published.
func (s *Service) UseEvents(p events.Publisher) { s.bus = p }

// UseFeatures gives the service the organizations' flags, which switch the signal checker on (ui.hx).
func (s *Service) UseFeatures(g features.Gate) { s.flags = g }

// UsageTick is the payload of the usage.tick event: the numbers of «Сегодня» of the scope watched.
type UsageTick struct {
	SpendToday  Money `json:"spendToday"`
	TokensToday int64 `json:"tokensToday"`
	ActiveNow   int   `json:"activeNow"`
	ChatsNow    int   `json:"chatsNow"`
}

// SignalEvent is the payload of signal.new.
type SignalEvent struct {
	Signal Signal `json:"signal"`
}

// LimitRequestEvent is the payload of limit.request: somebody asked for more.
type LimitRequestEvent struct {
	SignalID string `json:"signalId"`
	UserID   string `json:"userId"`
	Name     string `json:"name"`
	Scope    struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	} `json:"scope"`
}

// spaceOf is the tenant (space id) a scope belongs to; "" for the organization.
func spaceOf(ctx context.Context, st store.Store, kind, id string) string {
	switch kind {
	case store.ScopeTenant:
		return id
	case store.ScopeTeam:
		if t, err := st.TeamByID(ctx, id); err == nil {
			return t.SpaceID
		}
	}
	return ""
}

// announce tells the organization's streams about a new signal: the leads of its tenant and the admins, and the person
// it is addressed to.
func (s *Service) announce(ctx context.Context, orgID string, now time.Time, g store.Signal) {
	if s.bus == nil {
		return
	}
	ev := SignalEvent{Signal: signalView(g, now)}
	space := spaceOf(ctx, s.db.S(), g.ScopeKind, g.ScopeID)
	s.bus.Publish(orgID, "signal.new", events.For(events.Audience{Role: events.RoleLead, Space: space}, ev))
	if g.AddresseeID != "" {
		s.bus.Publish(orgID, "signal.new", events.For(events.Audience{User: g.AddresseeID}, ev))
	}
}

// publishLimitRequest tells the managers of the scope that somebody asked for a higher limit.
func (s *Service) publishLimitRequest(ctx context.Context, orgID string, g store.Signal) {
	if s.bus == nil {
		return
	}
	ev := LimitRequestEvent{SignalID: g.ID, UserID: g.SubjectID, Name: g.SubjectLabel}
	ev.Scope.Kind, ev.Scope.ID = g.ScopeKind, g.ScopeID
	s.bus.Publish(orgID, "limit.request", events.For(events.Audience{Role: events.RoleLead, Space: spaceOf(ctx, s.db.S(), g.ScopeKind, g.ScopeID)}, ev))
}

// usageTick is the numbers of today of one organization or tenant: what the organization's accounts paid since midnight
// of its zone, the tokens of all funding, the people with a chat going on and the chats.
func (s *Service) usageTick(ctx context.Context, st store.Store, l events.Listener) (UsageTick, error) {
	var t UsageTick
	o, err := st.OrgByID(ctx, l.OrgID)
	if err != nil {
		return t, err
	}
	sc := authz.Scope{Kind: store.ScopeOrg, ID: o.ID}
	if l.Space != "" {
		sc = authz.Scope{Kind: store.ScopeTenant, ID: l.Space}
	}
	x, err := s.loadScope(ctx, st, o, sc)
	if err != nil {
		return t, err
	}
	today := midnight(x.now, x.loc)
	byDay, err := s.spendByDay(ctx, st, x, today, today.AddDate(0, 0, 1))
	if err != nil {
		return t, err
	}
	cur := s.currency(ctx, o.Currency, newSpan(today, today.AddDate(0, 0, 1), x.loc))
	t.SpendToday = cur.Micros(0)
	if g := byDay[today.Format(day)]; g != nil {
		t.SpendToday, t.TokensToday = cur.Micros(g.paid), g.tokens
	}
	// The chats going on now, whenever they began: the overview's «в работе» counts the same ones.
	ss, err := st.ScopeSessionsOverlapping(ctx, x.filter(0, 0), x.now.Add(-activeWindow).UnixMilli(), x.now.UnixMilli()+1)
	if err != nil {
		return t, err
	}
	people := map[string]bool{}
	for _, v := range ss {
		if v.EndedAt == nil && v.UpdatedAt >= x.now.Add(-activeWindow).UnixMilli() {
			t.ChatsNow++
			if v.UserID != "" {
				people[v.UserID] = true
			}
		}
	}
	t.ActiveNow = len(people)
	return t, nil
}

// scopeRow is whether an hourly row (of any funding) is the scope's.
func (x *orgScope) scopeRow(r store.RollupRow) bool {
	switch x.sc.Kind {
	case store.ScopeTenant:
		return r.SpaceID == x.sc.ID
	case store.ScopeTeam:
		return r.TeamID == x.sc.ID
	}
	return true
}

// Tick publishes usage.tick for every organization and tenant somebody is listening to. The numbers of the organization
// are for its administrators, those of a tenant for its leads and only on the stream of that tenant.
func (s *Service) Tick(ctx context.Context, active []events.Listener) {
	if s.bus == nil {
		return
	}
	st := s.db.S()
	for _, l := range active {
		t, err := s.usageTick(ctx, st, l)
		if err != nil {
			if ctx.Err() == nil {
				s.log.Warn("usage tick", "org", l.OrgID, "error", err)
			}
			continue
		}
		aud := events.Audience{Role: events.RoleAdmin}
		if l.Space != "" {
			aud = events.Audience{Role: events.RoleLead, Space: l.Space, Only: true}
		}
		s.bus.Publish(l.OrgID, "usage.tick", events.For(aud, t))
	}
}

// CheckOrg runs the signal rules of one organization: it keeps what they find and resolves what they no longer do.
func (s *Service) CheckOrg(ctx context.Context, orgID string) error {
	st := s.db.S()
	o, err := st.OrgByID(ctx, orgID)
	if err != nil {
		return err
	}
	x, err := s.loadScope(ctx, st, o, authz.Scope{Kind: store.ScopeOrg, ID: o.ID})
	if err != nil {
		return err
	}
	x.full = true
	m, total, err := s.currentMoney(ctx, st, x)
	if err != nil {
		return err
	}
	fs, err := s.findSignals(ctx, st, m, total)
	if err != nil {
		return err
	}
	more, err := s.findOrgSignals(ctx, st, x)
	if err != nil {
		return err
	}
	return s.syncSignals(ctx, x, m, append(fs, more...))
}

// checkAll runs CheckOrg for every organization that has the new interface on.
func (s *Service) checkAll(ctx context.Context) {
	if s.flags == nil {
		return
	}
	os, err := s.db.S().AllOrgs(ctx)
	if err != nil {
		s.log.Warn("signal check: organizations", "error", err)
		return
	}
	for _, o := range os {
		if ctx.Err() != nil {
			return
		}
		if !s.flags.On(ctx, o.ID, features.UIHX) {
			continue
		}
		if err := s.CheckOrg(ctx, o.ID); err != nil && ctx.Err() == nil {
			s.log.Warn("signal check", "org", o.ID, "error", err)
		}
	}
}

// Trigger asks for the signal rules of an organization to run soon, after something happened that may change them (a
// machine went offline, a limit was set). Triggers within triggerAfter of each other are one run.
func (s *Service) Trigger(orgID string) {
	s.trigMu.Lock()
	defer s.trigMu.Unlock()
	if s.trig == nil {
		return
	}
	select {
	case s.trig <- orgID:
	default: // a full queue already has the organization or many others; the five-minute run catches the rest
	}
}

// RunLive does the service's background work until ctx ends: the ticks, the triggered checks and the five-minute check.
// active lists the organizations and tenants somebody listens to (events.OrgBus.Active).
func (s *Service) RunLive(ctx context.Context, active func() []events.Listener) {
	s.trigMu.Lock()
	s.trig = make(chan string, 64)
	trig := s.trig
	s.trigMu.Unlock()

	tick := time.NewTicker(tickEvery)
	check := time.NewTicker(checkEvery)
	defer tick.Stop()
	defer check.Stop()
	s.checkAll(ctx)
	pending := map[string]bool{}
	var due <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if active != nil {
				s.Tick(ctx, active())
			}
		case <-check.C:
			s.checkAll(ctx)
			clear(pending)
			due = nil
		case id := <-trig:
			pending[id] = true
			if due == nil {
				due = time.After(triggerAfter)
			}
		case <-due:
			due = nil
			for id := range pending {
				if s.flags != nil && s.flags.On(ctx, id, features.UIHX) {
					if err := s.CheckOrg(ctx, id); err != nil && ctx.Err() == nil {
						s.log.Warn("signal check", "org", id, "error", err)
					}
				}
			}
			clear(pending)
		}
	}
}
