// Package automations is the hub's side of automations (docs/hub/automations.md): a rule turns an event from
// outside into a conversation on its owner's machine, the conversation reports what it did, and the owner approves or
// rejects it. The hub keeps the rules and runs (migration 0036), takes events at <platform api>/automations/hook,
// starts the conversation over the machine's link (conversations.start) and takes its automations.result.
//
// It is switched on by HUB_AUTOMATIONS; off, every route answers 503 {code: "automations_off"}.
package automations

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/link"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/safego"
)

// Linker reaches a machine.
type Linker interface {
	Get(machineID string) *link.Link
	Do(ctx context.Context, machineID, op string, params any, timeout time.Duration) (json.RawMessage, error)
}

// Deps are what the service needs from the hub.
type Deps struct {
	Config *config.Config
	DB     *store.DB
	Auth   *auth.Service
	Links  Linker
	Log    *slog.Logger
	Now    func() time.Time
}

// Service serves the rules and runs and starts the runs.
type Service struct {
	cfg    *config.Config
	db     *store.DB
	auth   *auth.Service
	links  Linker
	log    *slog.Logger
	now    func() time.Time
	prefix string
	client *http.Client
}

func New(d Deps) *Service {
	s := &Service{cfg: d.Config, db: d.DB, auth: d.Auth, links: d.Links, log: d.Log, now: d.Now, client: &http.Client{}}
	if s.now == nil {
		s.now = time.Now
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	s.prefix = strings.TrimRight(d.Config.DesignPlatformAPI, "/")
	if s.prefix == "" {
		s.prefix = "/api/platform"
	}
	s.prefix += "/automations"
	return s
}

// Enabled says whether HUB_AUTOMATIONS is on.
func (s *Service) Enabled() bool { return s.cfg.Automations }

func newToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

var (
	errOff     = httpx.New(http.StatusServiceUnavailable, "automations_off", "Автоматизации выключены: администратор хаба не включил HUB_AUTOMATIONS")
	errNoRule  = httpx.New(http.StatusNotFound, "not_found", "Правило не найдено")
	errNoRun   = httpx.New(http.StatusNotFound, "not_found", "Запуск не найден")
	errTaken   = httpx.New(http.StatusConflict, "name_taken", "Правило с таким именем уже есть")
	errDecided = httpx.New(http.StatusConflict, "not_awaiting", "Запуск уже не ждёт решения")
)

func invalid(msg string) error { return httpx.New(http.StatusBadRequest, "invalid_request", msg) }

func (s *Service) guard(fn func(w http.ResponseWriter, r *http.Request, p *auth.Principal) error) http.Handler {
	return httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		p, err := s.auth.Authenticate(r)
		if err != nil {
			return err
		}
		if p == nil {
			return httpx.ErrUnauthenticated
		}
		if !s.Enabled() {
			return errOff
		}
		return fn(w, r, p)
	})
}

// Mount adds the routes under the platform API prefix.
func (s *Service) Mount(mux *http.ServeMux) {
	p := s.prefix
	mux.Handle("GET "+p+"/rules", s.guard(s.listRules))
	mux.Handle("POST "+p+"/rules", s.guard(s.createRule))
	mux.Handle("GET "+p+"/rules/{id}", s.guard(s.getRule))
	mux.Handle("PATCH "+p+"/rules/{id}", s.guard(s.patchRule))
	mux.Handle("DELETE "+p+"/rules/{id}", s.guard(s.removeRule))
	mux.Handle("GET "+p+"/runs", s.guard(s.listRuns))
	mux.Handle("POST "+p+"/runs/{id}/decision", s.guard(s.decision))
	mux.Handle("POST "+p+"/hook", httpx.Handler(s.hook))
	mux.Handle("POST "+p+"/callback", httpx.Handler(s.callback))
}

type ruleBody struct {
	Name           *string  `json:"name"`
	Trigger        *Trigger `json:"trigger"`
	PromptTemplate *string  `json:"promptTemplate"`
	Model          *string  `json:"model"`
	Limits         *Limits  `json:"limits"`
	MachineID      *string  `json:"machineId"`
	Enabled        *bool    `json:"enabled"`
}

func validLimits(l Limits) error {
	if l.MaxRunsPerDay != nil && (*l.MaxRunsPerDay < 0 || *l.MaxRunsPerDay > 1000) {
		return invalid("maxRunsPerDay: от 0 до 1000")
	}
	for _, h := range []*int{l.QuietHoursStartMsk, l.QuietHoursEndMsk} {
		if h != nil && (*h < 0 || *h > 23) {
			return invalid("Тихие часы: час от 0 до 23")
		}
	}
	return nil
}

// apply puts the body's fields on r and checks the result.
func (s *Service) apply(ctx context.Context, r *Rule, b ruleBody) error {
	if b.Name != nil {
		r.Name = strings.TrimSpace(*b.Name)
	}
	if b.Trigger != nil {
		r.Trigger = *b.Trigger
	}
	if b.PromptTemplate != nil {
		r.PromptTemplate = *b.PromptTemplate
	}
	if b.Model != nil {
		r.Model = strings.TrimSpace(*b.Model)
	}
	if b.Limits != nil {
		r.Limits = *b.Limits
	}
	if b.MachineID != nil {
		r.MachineID = strings.TrimSpace(*b.MachineID)
	}
	if b.Enabled != nil {
		r.Enabled = *b.Enabled
	}
	if r.Model == "" {
		r.Model = "sonnet"
	}
	switch {
	case r.Name == "" || len(r.Name) > 120:
		return invalid("Имя правила: от 1 до 120 символов")
	case strings.TrimSpace(r.Trigger.Source) == "":
		return invalid("Укажите источник события (trigger.source)")
	case strings.TrimSpace(r.PromptTemplate) == "" || len(r.PromptTemplate) > 20000:
		return invalid("Текст задачи: от 1 до 20000 символов")
	}
	if err := validLimits(r.Limits); err != nil {
		return err
	}
	if r.MachineID != "" {
		var n int
		if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM machines WHERE id = ? AND created_by = ? AND revoked_at IS NULL`, r.MachineID, r.OwnerID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return invalid("Это не ваша машина")
		}
	}
	return nil
}

func (s *Service) listRules(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	rules, err := s.rules(r.Context(), p.User.ID, false)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"rules": rules})
	return nil
}

func (s *Service) createRule(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var b ruleBody
	if err := httpx.Decode(r, &b); err != nil {
		return err
	}
	rule := Rule{OwnerID: p.User.ID}
	if err := s.apply(r.Context(), &rule, b); err != nil {
		return err
	}
	rule, ok, err := s.addRule(r.Context(), rule)
	if err != nil {
		return err
	}
	if !ok {
		return errTaken
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"rule": rule})
	return nil
}

func ruleOrNotFound(r Rule, err error) (Rule, error) {
	if err == store.ErrNotFound {
		return Rule{}, errNoRule
	}
	return r, err
}

func (s *Service) getRule(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	rule, err := ruleOrNotFound(s.ruleOf(r.Context(), r.PathValue("id"), p.User.ID))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"rule": rule})
	return nil
}

func (s *Service) patchRule(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var b ruleBody
	if err := httpx.Decode(r, &b); err != nil {
		return err
	}
	rule, err := ruleOrNotFound(s.ruleOf(r.Context(), r.PathValue("id"), p.User.ID))
	if err != nil {
		return err
	}
	if err := s.apply(r.Context(), &rule, b); err != nil {
		return err
	}
	if err := s.saveRule(r.Context(), rule); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return errTaken
		}
		return err
	}
	rule, err = s.ruleOf(r.Context(), rule.ID, p.User.ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"rule": rule})
	return nil
}

func (s *Service) removeRule(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	gone, err := s.deleteRule(r.Context(), r.PathValue("id"), p.User.ID)
	if err != nil {
		return err
	}
	if !gone {
		return errNoRule
	}
	httpx.NoContent(w)
	return nil
}

func (s *Service) listRuns(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	runs, err := s.runs(r.Context(), p.User.ID, r.URL.Query().Get("rule"), 50)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"runs": runs})
	return nil
}

// decision is the owner's answer to a finished run: {action: "approve"|"reject"}. The hub records it and, when it is set
// up for GitLab, merges or closes the run's merge request (actions.go, docs/hub/automations.md).
func (s *Service) decision(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var b struct {
		Action string `json:"action"`
	}
	if err := httpx.Decode(r, &b); err != nil {
		return err
	}
	status := map[string]string{"approve": StatusApproved, "reject": StatusRejected}[b.Action]
	if status == "" {
		return invalid("action: approve или reject")
	}
	run, ok, err := s.decide(r.Context(), r.PathValue("id"), p.User.ID, status)
	if err == store.ErrNotFound {
		return errNoRun
	}
	if err != nil {
		return err
	}
	if !ok {
		return errDecided
	}
	out := s.settle(r.Context(), run)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"run": out.Run, "merged": out.Merged, "reason": out.Reason})
	return nil
}

func matchesFilter(filter map[string]any, event map[string]any) bool {
	for k, want := range filter {
		got, ok := event[k]
		if !ok {
			return false
		}
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(want)
		if string(a) != string(b) {
			return false
		}
	}
	return true
}

// hook takes an event from outside: {source, fingerprint, event}, with HUB_AUTOMATIONS_SECRET in X-Automation-Secret.
// Every enabled rule of that source whose filter the event meets gets a queued run.
func (s *Service) hook(w http.ResponseWriter, r *http.Request) error {
	if !s.Enabled() {
		return errOff
	}
	given := r.Header.Get("X-Automation-Secret")
	if s.cfg.AutomationsSecret == "" || subtle.ConstantTimeCompare([]byte(given), []byte(s.cfg.AutomationsSecret)) != 1 {
		return httpx.ErrForbidden
	}
	var b struct {
		Source      string          `json:"source"`
		Fingerprint string          `json:"fingerprint"`
		Event       json.RawMessage `json:"event"`
	}
	if err := httpx.Decode(r, &b); err != nil {
		return err
	}
	var event map[string]any
	if b.Source == "" || b.Fingerprint == "" || len(b.Fingerprint) > 500 || json.Unmarshal(b.Event, &event) != nil || event == nil {
		return invalid("Нужны source, fingerprint и event (объект)")
	}
	rules, err := s.rules(r.Context(), "", true)
	if err != nil {
		return err
	}
	results := []map[string]any{}
	for _, rule := range rules {
		if rule.Trigger.Source != b.Source || !matchesFilter(rule.Trigger.Filter, event) {
			continue
		}
		out, err := s.queueRun(r.Context(), rule, b.Fingerprint, b.Event)
		if err != nil {
			return err
		}
		s.log.Info("automation event matched a rule", "rule", rule.ID, "queued", out.Queued, "reason", out.Reason)
		results = append(results, map[string]any{"ruleId": rule.ID, "queued": out.Queued, "runId": out.RunID, "reason": out.Reason})
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"matched": len(results), "results": results})
	return nil
}

var (
	issueRe = regexp.MustCompile(`^[A-Z][A-Z0-9]{0,9}-\d{1,9}$`)
	mrRe    = regexp.MustCompile(`^https://[^\s/]+/\S*$`)
)

// Event takes the automations.result of a machine's agent. It never blocks the link.
func (s *Service) Event(m store.Machine, ev link.Event) {
	if ev.Name != hubproto.EventAutomationResult || !s.Enabled() {
		return
	}
	var e struct {
		RunID       string `json:"runId"`
		ResultToken string `json:"resultToken"`
		Summary     string `json:"summary"`
		BugIssueID  string `json:"bugIssueId"`
		MRURL       string `json:"mrUrl"`
	}
	if json.Unmarshal(ev.Raw, &e) != nil || e.RunID == "" || e.ResultToken == "" {
		return
	}
	safego.Go("automations Event", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		// What the conversation wrote is kept only when it looks like what it should be.
		if e.BugIssueID != "" && !issueRe.MatchString(e.BugIssueID) {
			s.log.Warn("automation report: malformed bugIssueId dropped", "run", e.RunID)
			e.BugIssueID = ""
		}
		if e.MRURL != "" && !mrRe.MatchString(e.MRURL) {
			s.log.Warn("automation report: disallowed mrUrl dropped", "run", e.RunID)
			e.MRURL = ""
		}
		if len(e.Summary) > 8000 {
			e.Summary = e.Summary[:8000]
		}
		if err := s.recordResult(ctx, e.RunID, e.ResultToken, e.Summary, e.BugIssueID, e.MRURL); err != nil {
			s.log.Warn("automation report not taken", "machine", m.ID, "run", e.RunID, "error", err)
			return
		}
		if run, err := s.runByID(ctx, e.RunID); err == nil {
			s.notify(ctx, run)
		}
	})
}
