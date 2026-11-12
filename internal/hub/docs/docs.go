// Package docs is the hub's side of documents and boards (docs/hub/docs.md): the page's document API
// (<platform api>/docs), which the web core calls, and the agents' doc.publish and doc.acl, which the machine's agent
// sends over its link. The documents themselves live in the documents service (github.com/ly-solutions/artifacts), the
// boards in the boards service; the hub keeps what neither has: who owns a document and who else may open it, the
// comment threads and what each person has read.
//
// It is switched on by HUB_DOCS. Without the flag, or without the address and token of the documents service, every
// route answers 503 with {error, code: "docs_not_configured", reason} and the page says what is missing.
package docs

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/ratelimit"
	"github.com/ly-solutions/harnessly/internal/hub/resaccess"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Deps are what the service needs from the hub.
type Deps struct {
	Config *config.Config
	DB     *store.DB
	Auth   *auth.Service
	Log    *slog.Logger
	Now    func() time.Time
	// Links hands comments to the machines' conversations; nil keeps them on the hub.
	Links Linker
	// HTTP reaches the documents and boards services; nil: a client with a 30 second timeout.
	HTTP *http.Client
	// Notice tells a person by mail that they were mentioned; nil: nobody is told.
	Notice Notice
	// Gate says whether the rollout switch of the comments on boards is on for a person (boards.go); nil: off for everyone.
	Gate Gate
}

// Service serves documents and boards.
type Service struct {
	cfg    *config.Config
	db     *store.DB
	auth   *auth.Service
	log    *slog.Logger
	now    func() time.Time
	hc     *http.Client
	art    *artifactsClient
	prefix string
	links  Linker
	flight inflight
	// access is the shared model of access to a resource (HUB_RESOURCE_ACCESS); nil keeps doc_meta.team_level.
	access *resaccess.Service
	notice Notice
	// lim holds the people search of one person to a pace.
	lim *ratelimit.Limiter
	// health is the last answer of the documents and boards services (health.go).
	health *health
	gate   Gate
}

// Notice mails a short notice to a person (a mention); nil keeps the hub silent.
type Notice func(ctx context.Context, to, subject, body, link string) error

func New(d Deps) *Service {
	s := &Service{cfg: d.Config, db: d.DB, auth: d.Auth, log: d.Log, now: d.Now, hc: d.HTTP, links: d.Links, notice: d.Notice, health: newHealth(), gate: d.Gate}
	if s.now == nil {
		s.now = time.Now
	}
	s.lim = ratelimit.New().WithClock(func() time.Time { return s.now() })
	if d.Config.ResourceAccess {
		s.access = resaccess.New(d.DB, s.now, d.Config.PublicURL)
		s.access.Notify = func(ctx context.Context, to, subject, body string) {
			if s.notice != nil {
				_ = s.notice(ctx, to, subject, body, "")
			}
		}
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.hc == nil {
		s.hc = &http.Client{Timeout: 30 * time.Second}
	}
	if d.Config.ArtifactsURL != "" && d.Config.ArtifactsToken != "" {
		s.art = newArtifacts(d.Config.ArtifactsURL, d.Config.ArtifactsToken, s.hc)
	}
	s.prefix = strings.TrimRight(d.Config.DesignPlatformAPI, "/")
	if s.prefix == "" {
		s.prefix = "/api/platform"
	}
	s.prefix += "/docs"
	return s
}

// Status says whether documents work and, when they do not, why. Reason is a code the page can match; Text is a
// sentence for the person.
type Status struct {
	Enabled   bool   `json:"enabled"`
	Documents bool   `json:"documents"`
	Boards    bool   `json:"boards"`
	Reason    string `json:"reason,omitempty"`
	Text      string `json:"text,omitempty"`
	// Services is whether each configured service answers its /readyz (ServiceDocuments, ServiceBoards); a service not
	// asked yet is absent, and the page then takes it for up.
	Services map[string]ServiceHealth `json:"services,omitempty"`
}

func (s *Service) Status() Status {
	st := Status{Enabled: s.cfg.Docs, Boards: s.cfg.Docs && s.cfg.BoardsURL != "" && s.cfg.BoardsToken != ""}
	switch {
	case !s.cfg.Docs:
		st.Reason, st.Text = "disabled", "Документы выключены: администратор хаба не включил HUB_DOCS."
	case s.cfg.ArtifactsURL == "":
		st.Reason, st.Text = "no_artifacts_url", "Сервис документов не подключён: не задан HUB_ARTIFACTS_URL."
	case s.cfg.ArtifactsToken == "":
		st.Reason, st.Text = "no_artifacts_token", "Сервис документов не подключён: не задан HUB_ARTIFACTS_TOKEN."
	default:
		st.Documents = true
	}
	if st.Enabled {
		st.Services = s.Services()
	}
	return st
}

// notConfigured is the body of the 503 every route gives while documents are off.
func (s *Service) notConfigured() map[string]any {
	st := s.Status()
	return map[string]any{"error": st.Text, "code": "docs_not_configured", "reason": st.Reason}
}

// Who may do what. The owner and the administrators of the installation manage a document; the people of the
// document's organizations (see circle) get what its team level says. A document the hub never saw (a board, or one
// published straight to the documents service) has no owner; the people of its organizations may read it. author is
// who the service says made such a document.
const (
	rankManage = 4
)

func (s *Service) rank(ctx context.Context, slug string, m *meta, author string, p *auth.Principal, c *circle) int {
	if c == nil {
		c = s.circleOf(p)
	}
	if s.access != nil {
		return s.accessRank(ctx, slug, m, author, p, c)
	}
	if p.Admin || (m != nil && m.OwnerEmail != "" && strings.EqualFold(m.OwnerEmail, p.User.Email)) {
		return rankManage
	}
	if !c.holds(ctx, m, author) {
		return 0
	}
	if m == nil {
		return levelRank[levelView]
	}
	return levelRank[m.TeamLevel]
}

func personOf(p *auth.Principal) author {
	name := p.User.Name
	if name == "" {
		name = p.User.Email
	}
	return author{Kind: "person", Email: p.User.Email, Name: name}
}

// levelOf maps what an agent calls visibility to a team level.
func levelOf(visibility string) string {
	switch strings.ToLower(visibility) {
	case "team", "view", "public":
		return levelView
	case "comment":
		return levelComment
	case "edit":
		return levelEdit
	}
	return levelNone
}

func (s *Service) user(ctx context.Context, id string) (store.User, bool) {
	if id == "" {
		return store.User{}, false
	}
	u, err := s.db.S().UserByID(ctx, id)
	return u, err == nil
}
