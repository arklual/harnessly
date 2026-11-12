package docs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// The documents and boards services are apps of their own, outside the hub. Every hub process asks each configured
// one for /readyz every probeEvery and keeps the answer: the page reads it from <platform api>/docs/_status and shows
// «сервис недоступен» instead of a view that never loads, and the scrape shows it as a gauge. The pages never make the
// hub ask a service; one process sends one probe per service per pass, whatever the number of people.
//
// A service that stays down alertAfter gets one mail to the installation's administrators, and one more when it is
// back. Of the replicas of a hub only the holder of the alerts lease mails, so the administrators get one letter.
const (
	probeEvery   = 30 * time.Second
	probeTimeout = 3 * time.Second
	alertAfter   = 5 * time.Minute
	alertLease   = "docs-service-alerts"
	// alertLeaseTTL outlives a few passes, so the holder keeps it while it lives and another replica takes over soon after.
	alertLeaseTTL = 3 * probeEvery
)

// The services the hub probes, as the status names them.
const (
	ServiceDocuments = "documents"
	ServiceBoards    = "boards"
)

// ServiceHealth is the last answer of one service. Since is when it last went up or down.
type ServiceHealth struct {
	Up        bool      `json:"up"`
	Since     time.Time `json:"since"`
	CheckedAt time.Time `json:"checkedAt"`
}

type serviceState struct {
	ServiceHealth
	// alerted: the administrators were told it is down and have not yet been told it is back.
	alerted bool
}

type health struct {
	mu     sync.Mutex
	svc    map[string]*serviceState
	holder string
	// noProbe: the services whose address does not route /readyz, so the line about them is logged once.
	noProbe map[string]bool
}

func newHealth() *health {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return &health{svc: map[string]*serviceState{}, noProbe: map[string]bool{}, holder: hex.EncodeToString(b)}
}

// ErrNoReadyz: the address answered 404 to /readyz. The service itself is not said to be down: the address only does
// not route that path (a public host that shows one prefix of the service is the usual case), so the answer tells
// nothing about the service. The status leaves such a service out and nobody is mailed; the doctor still reports it.
var ErrNoReadyz = errors.New("the address does not route /readyz")

// ProbeReady asks base/readyz and says why the service is not ready; nil: it answered 2xx. A 404 wraps ErrNoReadyz.
func ProbeReady(ctx context.Context, hc *http.Client, base string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/readyz", nil)
	if err != nil {
		return err
	}
	res, err := hc.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4<<10))
	res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return fmt.Errorf("readyz answered %s: %w", res.Status, ErrNoReadyz)
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("readyz answered %s", res.Status)
	}
	return nil
}

// targets are the configured services and their addresses.
func (s *Service) targets() map[string]string {
	out := map[string]string{}
	if !s.cfg.Docs {
		return out
	}
	if s.cfg.ArtifactsURL != "" {
		out[ServiceDocuments] = s.cfg.ArtifactsURL
	}
	if s.cfg.BoardsURL != "" {
		out[ServiceBoards] = s.cfg.BoardsURL
	}
	return out
}

// Services is the last known health of each probed service; a service not probed yet is absent.
func (s *Service) Services() map[string]ServiceHealth {
	s.health.mu.Lock()
	defer s.health.mu.Unlock()
	if len(s.health.svc) == 0 {
		return nil
	}
	out := make(map[string]ServiceHealth, len(s.health.svc))
	for name, st := range s.health.svc {
		out[name] = st.ServiceHealth
	}
	return out
}

// runHealth probes the services every probeEvery until ctx ends.
func (s *Service) runHealth(ctx context.Context) {
	if len(s.targets()) == 0 {
		return
	}
	t := time.NewTicker(probeEvery)
	defer t.Stop()
	for {
		s.healthPass(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// healthPass probes each service once, keeps the answers and tells the administrators what changed.
func (s *Service) healthPass(ctx context.Context) {
	targets := s.targets()
	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	slices.Sort(names)
	errs := make([]error, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pctx, cancel := context.WithTimeout(ctx, probeTimeout)
			defer cancel()
			errs[i] = ProbeReady(pctx, s.hc, targets[name])
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return
	}
	now := s.now()
	for i, name := range names {
		s.record(name, errs[i], now)
	}
	s.alerts(ctx, now)
}

// record keeps one answer and logs a change.
func (s *Service) record(name string, err error, now time.Time) {
	if errors.Is(err, ErrNoReadyz) {
		s.health.mu.Lock()
		first := !s.health.noProbe[name]
		s.health.noProbe[name] = true
		delete(s.health.svc, name)
		s.health.mu.Unlock()
		if first {
			s.log.Info("app service is not probed: its address does not route /readyz", "service", name)
		}
		return
	}
	s.health.mu.Lock()
	delete(s.health.noProbe, name)
	s.health.mu.Unlock()
	up := err == nil
	s.health.mu.Lock()
	st, seen := s.health.svc[name]
	if !seen {
		st = &serviceState{ServiceHealth: ServiceHealth{Up: up, Since: now}}
		s.health.svc[name] = st
	}
	changed := seen && st.Up != up
	if changed {
		st.Up, st.Since = up, now
	}
	st.CheckedAt = now
	s.health.mu.Unlock()
	switch {
	case !up && (changed || !seen):
		s.log.Warn("app service is down", "service", name, "error", err)
	case up && changed:
		s.log.Info("app service is back", "service", name)
	}
}

// alerts mails the administrators about a service down alertAfter and, once, about its return. Only the holder of
// the alerts lease mails.
func (s *Service) alerts(ctx context.Context, now time.Time) {
	type letter struct{ name, subject, body string }
	var due []letter
	s.health.mu.Lock()
	for name, st := range s.health.svc {
		switch {
		case !st.Up && !st.alerted && now.Sub(st.Since) >= alertAfter:
			due = append(due, letter{name, downSubject(name), downBody(name, now.Sub(st.Since))})
		case st.Up && st.alerted:
			due = append(due, letter{name, backSubject(name), backBody(name)})
		}
	}
	s.health.mu.Unlock()
	if len(due) == 0 {
		return
	}
	if ok, err := s.db.S().TakeLease(ctx, alertLease, s.health.holder, now, alertLeaseTTL); err != nil || !ok {
		if err != nil {
			s.log.Warn("app service alerts lease not taken", "error", err)
		}
		return
	}
	to := s.adminEmails(ctx)
	for _, l := range due {
		for _, addr := range to {
			if s.notice == nil {
				break
			}
			if err := s.notice(ctx, addr, l.subject, l.body, s.cfg.PublicURL); err != nil {
				s.log.Warn("app service alert not mailed", "service", l.name, "error", err)
			}
		}
		if len(to) == 0 || s.notice == nil {
			s.log.Warn("app service alert has nobody to mail", "service", l.name, "subject", l.subject)
		}
		s.health.mu.Lock()
		if st := s.health.svc[l.name]; st != nil {
			st.alerted = !st.Up
		}
		s.health.mu.Unlock()
	}
}

// adminEmails are the administrators of the installation: HUB_ADMIN_EMAILS and the accounts marked admin.
func (s *Service) adminEmails(ctx context.Context) []string {
	out := slices.Clone(s.cfg.AdminEmails)
	rows, err := s.db.S().Q().Query(ctx, `SELECT email FROM users WHERE is_admin = 1 AND disabled_at IS NULL`)
	if err != nil {
		s.log.Warn("administrators not read", "error", err)
	} else {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var e string
			if rows.Scan(&e) == nil {
				out = append(out, strings.ToLower(strings.TrimSpace(e)))
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(slices.DeleteFunc(out, func(e string) bool { return e == "" }))
}

func serviceTitle(name string) string {
	if name == ServiceBoards {
		return "Сервис досок"
	}
	return "Сервис документов"
}

func downSubject(name string) string { return serviceTitle(name) + " недоступен" }

func downBody(name string, down time.Duration) string {
	return fmt.Sprintf("%s не отвечает уже %d мин. Люди видят «сейчас не открывается», агенты получают ошибку «сервис недоступен». "+
		"Проверьте сервис и его /readyz; когда он вернётся, придёт ещё одно письмо.", serviceTitle(name), int(down.Minutes()))
}

func backSubject(name string) string { return serviceTitle(name) + " снова работает" }

func backBody(name string) string {
	return serviceTitle(name) + " снова отвечает: приложение открывается, агенты работают с ним как обычно."
}
