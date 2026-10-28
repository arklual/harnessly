package sites

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Role names, as the hub spells them.
const (
	RoleOwner  = store.RoleOwner
	RoleMember = store.RoleMember
)

// bearer is the token of an Authorization: Bearer header.
func bearer(r *http.Request) string {
	v := r.Header.Get("Authorization")
	if len(v) > 7 && strings.EqualFold(v[:7], "bearer ") {
		return strings.TrimSpace(v[7:])
	}
	return ""
}

// caller is who a management request is from: a person of the space, or a deploy token of one site.
type caller struct {
	person *Person
	role   string
	token  *Token
	site   *Site // set when the caller is a deploy token
	// machine is set when the caller is a machine with a publishing session: it makes sites and deploys to its own.
	machine *MachineSession
}

func (c caller) name() string {
	if c.machine != nil {
		return c.machine.Name
	}
	if c.token != nil {
		return "токен «" + c.token.Name + "»"
	}
	return c.person.Name
}

func (c caller) owner() bool { return c.role == RoleOwner }

// id is who the caller is for the rights over a site: a person's id, or machine:<id>. A deploy token has none.
func (c caller) id() string {
	switch {
	case c.machine != nil:
		return c.machine.ID()
	case c.person != nil:
		return c.person.ID
	}
	return ""
}

// ownSite says whether the site was made by this caller: the one right a machine has over a site.
func (c caller) ownSite(site Site) bool { return c.id() != "" && site.CreatedByID == c.id() }

// mayDelete: the space's owner, or the person who made the site (by id; names repeat). Never a machine or a token.
func (c caller) mayDelete(site Site) bool {
	return c.machine == nil && c.person != nil && (c.owner() || c.ownSite(site))
}

// who finds the caller of a request on a space: a person with a role in it. A deploy token, when allowed, is the other way.
func (s *Service) who(r *http.Request, space string, tokenOK bool, siteName string) (caller, error) {
	if b := bearer(r); strings.HasPrefix(b, machinePrefix) {
		// A machine of the space with the session the hub gave it. It is not a member: it makes sites and deploys to its own.
		ms, ok := s.machineOf(b)
		if !ok || ms.Space != space || !s.auth.MachineOK(r.Context(), ms.MachineID, ms.PersonID, space) {
			return caller{}, httpx.ErrUnauthenticated
		}
		return caller{machine: &ms}, nil
	}
	if b := bearer(r); b != "" && strings.HasPrefix(b, tokenPrefix) {
		if !tokenOK {
			return caller{}, httpx.ErrForbidden
		}
		site, t, err := s.TokenSite(r.Context(), b)
		if err != nil || site.Space != space || (siteName != "" && site.Name != siteName) {
			return caller{}, httpx.ErrUnauthenticated
		}
		return caller{token: &t, site: &site}, nil
	}
	p, err := s.auth.Person(r)
	if err != nil {
		return caller{}, err
	}
	if p == nil {
		return caller{}, httpx.ErrUnauthenticated
	}
	role, err := s.auth.Role(r.Context(), p.ID, space)
	if err != nil {
		return caller{}, err
	}
	if role == "" {
		return caller{}, httpx.ErrForbidden
	}
	return caller{person: p, role: role}, nil
}

// SiteView is a site as the API sends it.
type SiteView struct {
	ID             string        `json:"id"`
	Space          string        `json:"space"`
	Name           string        `json:"name"`
	Address        string        `json:"address"`
	Access         string        `json:"access"`
	SPA            bool          `json:"spa"`
	CurrentVersion int           `json:"currentVersion"`
	CreatedBy      string        `json:"createdBy"`
	Origin         string        `json:"origin,omitempty"`
	UpdatedBy      string        `json:"updatedBy"`
	CreatedAt      string        `json:"createdAt"`
	UpdatedAt      string        `json:"updatedAt"`
	Versions       []VersionView `json:"versions,omitempty"`
}

// VersionView is one deployment of a site.
type VersionView struct {
	Version    int    `json:"version"`
	URL        string `json:"url"`
	FileCount  int    `json:"fileCount"`
	TotalBytes int64  `json:"totalBytes"`
	Source     string `json:"source"`
	CreatedBy  string `json:"createdBy"`
	CreatedAt  string `json:"createdAt"`
	Current    bool   `json:"current"`
}

// TokenView is a deploy token as the API sends it.
type TokenView struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	CreatedBy  string  `json:"createdBy"`
	CreatedAt  string  `json:"createdAt"`
	LastUsedAt *string `json:"lastUsedAt"`
	// Token is the secret, in the answer that makes it and nowhere else.
	Token string `json:"token,omitempty"`
}

func (s *Service) siteView(site Site, versions []Deployment) SiteView {
	v := SiteView{ID: site.ID, Space: site.Space, Name: site.Name, Address: s.SiteURL(site), Access: site.Access, SPA: site.SPA,
		CurrentVersion: site.CurrentVersion, CreatedBy: site.CreatedBy, Origin: site.Origin, UpdatedBy: site.UpdatedBy,
		CreatedAt: httpx.Time(site.CreatedAt), UpdatedAt: httpx.Time(site.UpdatedAt)}
	for _, d := range versions {
		v.Versions = append(v.Versions, VersionView{Version: d.Version, URL: s.URL(Addr{Site: site.Name, Space: site.Space, Kind: KindVersion, N: d.Version}),
			FileCount: d.FileCount, TotalBytes: d.TotalBytes, Source: d.Source, CreatedBy: d.CreatedBy, CreatedAt: httpx.Time(d.CreatedAt),
			Current: d.Version == site.CurrentVersion})
	}
	return v
}

// Mount registers the management API under /api/sites.
func (s *Service) Mount(mux *http.ServeMux) {
	mux.Handle("GET /api/sites-config", httpx.Handler(s.apiConfig))
	mux.Handle("GET /api/sites-token", httpx.Handler(s.apiTokenInfo))
	mux.Handle("GET /api/sites/{space}", httpx.Handler(s.apiList))
	mux.Handle("POST /api/sites/{space}", httpx.Handler(s.apiCreate))
	mux.Handle("GET /api/sites/{space}/{site}", httpx.Handler(s.apiGet))
	mux.Handle("PATCH /api/sites/{space}/{site}", httpx.Handler(s.apiPatch))
	mux.Handle("DELETE /api/sites/{space}/{site}", httpx.Handler(s.apiDelete))
	mux.Handle("POST /api/sites/{space}/{site}/deploys", httpx.Handler(s.apiBegin))
	mux.Handle("PUT /api/sites/{space}/{site}/deploys/{id}/blobs/{hash}", httpx.Handler(s.apiBlob))
	mux.Handle("POST /api/sites/{space}/{site}/deploys/{id}/commit", httpx.Handler(s.apiCommit))
	mux.Handle("POST /api/sites/{space}/{site}/upload", httpx.Handler(s.apiZip))
	mux.Handle("POST /api/sites/{space}/{site}/current", httpx.Handler(s.apiCurrent))
	mux.Handle("GET /api/sites/{space}/{site}/tokens", httpx.Handler(s.apiTokens))
	mux.Handle("POST /api/sites/{space}/{site}/tokens", httpx.Handler(s.apiNewToken))
	mux.Handle("DELETE /api/sites/{space}/{site}/tokens/{id}", httpx.Handler(s.apiDeleteToken))
}

func (s *Service) apiConfig(w http.ResponseWriter, r *http.Request) error {
	scheme := "path"
	if s.Subdomains() {
		scheme = "subdomain"
	}
	l := s.cfg.Limits
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"enabled": true, "scheme": scheme, "domain": s.cfg.Domain, "abuseUrl": s.cfg.AbuseURL,
		"limits": map[string]any{"deployBytes": l.MaxDeployBytes, "fileBytes": l.MaxFileBytes, "files": l.MaxFiles, "sites": l.MaxSitesPerSpace, "versions": l.KeepVersions},
	})
	return nil
}

// apiTokenInfo tells a deploy token which site it is for, so that the CLI needs only the token and the server's address.
func (s *Service) apiTokenInfo(w http.ResponseWriter, r *http.Request) error {
	b := bearer(r)
	site, _, err := s.TokenSite(r.Context(), b)
	if err != nil {
		return httpx.ErrUnauthenticated
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"space": site.Space, "site": site.Name, "url": s.SiteURL(site)})
	return nil
}

func (s *Service) apiList(w http.ResponseWriter, r *http.Request) error {
	space := r.PathValue("space")
	c, err := s.who(r, space, false, "")
	if err != nil {
		return err
	}
	list, err := s.Sites(r.Context(), space)
	if err != nil {
		return err
	}
	out := make([]SiteView, 0, len(list))
	for _, site := range list {
		if c.machine != nil && !c.ownSite(site) {
			continue
		}
		out = append(out, s.siteView(site, nil))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"sites": out})
	return nil
}

type createBody struct {
	Name   string `json:"name"`
	Access string `json:"access"`
	SPA    *bool  `json:"spa"`
	// Origin is what the site is made from (sinopia:<project>); only a machine says it.
	Origin string `json:"origin"`
}

func originOf(c caller, origin string) string {
	if c.machine == nil || len(origin) > 200 {
		return ""
	}
	return origin
}

func (s *Service) apiCreate(w http.ResponseWriter, r *http.Request) error {
	space := r.PathValue("space")
	c, err := s.who(r, space, false, "")
	if err != nil {
		return err
	}
	var b createBody
	if err := httpx.Decode(r, &b); err != nil {
		return err
	}
	site, err := s.CreateSite(r.Context(), space, strings.TrimSpace(b.Name), b.Access, b.SPA, c.name(), c.id(), originOf(c, b.Origin))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, s.siteView(site, nil))
	return nil
}

// siteFor finds the site of the path and the caller's right to it.
func (s *Service) siteFor(r *http.Request, tokenOK bool) (Site, caller, error) {
	space, name := r.PathValue("space"), r.PathValue("site")
	c, err := s.who(r, space, tokenOK, name)
	if err != nil {
		return Site{}, caller{}, err
	}
	site, err := s.Site(r.Context(), space, name)
	if err != nil {
		return Site{}, caller{}, err
	}
	if c.machine != nil && !c.ownSite(site) {
		return Site{}, caller{}, httpx.ErrForbidden
	}
	return site, c, nil
}

func (s *Service) apiGet(w http.ResponseWriter, r *http.Request) error {
	site, _, err := s.siteFor(r, true)
	if err != nil {
		return err
	}
	versions, err := s.Versions(r.Context(), site)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, s.siteView(site, versions))
	return nil
}

type patchBody struct {
	Access *string `json:"access"`
	SPA    *bool   `json:"spa"`
}

func (s *Service) apiPatch(w http.ResponseWriter, r *http.Request) error {
	site, c, err := s.siteFor(r, false)
	if err != nil {
		return err
	}
	var b patchBody
	if err := httpx.Decode(r, &b); err != nil {
		return err
	}
	site, err = s.Update(r.Context(), site, b.Access, b.SPA, c.name())
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, s.siteView(site, nil))
	return nil
}

func (s *Service) apiDelete(w http.ResponseWriter, r *http.Request) error {
	site, c, err := s.siteFor(r, false)
	if err != nil {
		return err
	}
	if !c.mayDelete(site) {
		return httpx.New(http.StatusForbidden, "forbidden", "Удалить сайт может владелец пространства или тот, кто его создал")
	}
	if err := s.Delete(r.Context(), site); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

type beginBody struct {
	Files        map[string]File `json:"files"`
	Source       string          `json:"source"`
	AllowSecrets bool            `json:"allowSecrets"`
}

func (s *Service) apiBegin(w http.ResponseWriter, r *http.Request) error {
	site, c, err := s.siteFor(r, true)
	if err != nil {
		return err
	}
	// The manifest of a big site is more than the 1 MiB JSON body limit of small requests.
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	var b beginBody
	if err := decodeJSON(r, &b); err != nil {
		return err
	}
	source := b.Source
	if source == "" || len(source) > 20 {
		source = "api"
	}
	p, missing, err := s.Begin(r.Context(), site, Manifest(b.Files), source, c.name(), b.AllowSecrets)
	if err != nil {
		return err
	}
	if missing == nil {
		missing = []string{}
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"id": p.ID, "missing": missing, "expiresAt": httpx.Time(p.ExpiresAt)})
	return nil
}

func (s *Service) apiBlob(w http.ResponseWriter, r *http.Request) error {
	site, _, err := s.siteFor(r, true)
	if err != nil {
		return err
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.Limits.MaxFileBytes+1)
	if err := s.PutBlob(r.Context(), site, r.PathValue("id"), r.PathValue("hash"), r.Body); err != nil {
		var mb *http.MaxBytesError
		if errors.As(err, &mb) {
			return httpx.New(http.StatusRequestEntityTooLarge, "file_too_large", "Файл слишком большой")
		}
		return err
	}
	httpx.NoContent(w)
	return nil
}

type commitBody struct {
	// Current false keeps the site on its current version (a deployment to look at before it goes live).
	Current *bool `json:"current"`
}

func (s *Service) apiCommit(w http.ResponseWriter, r *http.Request) error {
	site, c, err := s.siteFor(r, true)
	if err != nil {
		return err
	}
	var b commitBody
	if err := httpx.Decode(r, &b); err != nil {
		return err
	}
	current := b.Current == nil || *b.Current
	d, err := s.Commit(r.Context(), site, r.PathValue("id"), c.name(), current)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, s.deployAnswer(site, d, current))
	return nil
}

func (s *Service) deployAnswer(site Site, d Deployment, current bool) map[string]any {
	url := s.URL(Addr{Site: site.Name, Space: site.Space, Kind: KindVersion, N: d.Version})
	if current {
		url = s.SiteURL(site)
	}
	return map[string]any{"version": d.Version, "url": url, "versionUrl": s.URL(Addr{Site: site.Name, Space: site.Space, Kind: KindVersion, N: d.Version}),
		"current": current, "fileCount": d.FileCount, "totalBytes": d.TotalBytes}
}

func (s *Service) apiZip(w http.ResponseWriter, r *http.Request) error {
	site, c, err := s.siteFor(r, true)
	if err != nil {
		return err
	}
	allow := r.URL.Query().Get("allowSecrets") == "1"
	d, err := s.DeployZip(r.Context(), site, r.Body, c.name(), allow)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, s.deployAnswer(site, d, true))
	return nil
}

type currentBody struct {
	Version int `json:"version"`
}

func (s *Service) apiCurrent(w http.ResponseWriter, r *http.Request) error {
	site, c, err := s.siteFor(r, true)
	if err != nil {
		return err
	}
	var b currentBody
	if err := httpx.Decode(r, &b); err != nil {
		return err
	}
	if b.Version < 1 {
		return httpx.Invalid("version", "Нужен номер версии")
	}
	site, err = s.SetCurrent(r.Context(), site, b.Version, c.name())
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, s.siteView(site, nil))
	return nil
}

func tokenView(t Token, secret string) TokenView {
	return TokenView{ID: t.ID, Name: t.Name, CreatedBy: t.CreatedBy, CreatedAt: httpx.Time(t.CreatedAt), LastUsedAt: lastUsed(t), Token: secret}
}

func lastUsed(t Token) *string {
	if t.LastUsedAt.IsZero() {
		return nil
	}
	v := httpx.Time(t.LastUsedAt)
	return &v
}

func (s *Service) apiTokens(w http.ResponseWriter, r *http.Request) error {
	site, c, err := s.siteFor(r, false)
	if err != nil {
		return err
	}
	if c.machine != nil {
		return httpx.ErrForbidden
	}
	list, err := s.Tokens(r.Context(), site)
	if err != nil {
		return err
	}
	out := make([]TokenView, 0, len(list))
	for _, t := range list {
		out = append(out, tokenView(t, ""))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"tokens": out})
	return nil
}

func (s *Service) apiNewToken(w http.ResponseWriter, r *http.Request) error {
	site, c, err := s.siteFor(r, false)
	if err != nil {
		return err
	}
	// A session of a machine is short; a token outlives it, so a machine cannot make one.
	if c.machine != nil {
		return httpx.ErrForbidden
	}
	var b struct {
		Name string `json:"name"`
	}
	if err := httpx.Decode(r, &b); err != nil {
		return err
	}
	t, secret, err := s.NewToken(r.Context(), site, b.Name, c.name())
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, tokenView(t, secret))
	return nil
}

func (s *Service) apiDeleteToken(w http.ResponseWriter, r *http.Request) error {
	site, c, err := s.siteFor(r, false)
	if err != nil {
		return err
	}
	if c.machine != nil {
		return httpx.ErrForbidden
	}
	if err := s.RevokeToken(r.Context(), site, r.PathValue("id")); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// decodeJSON reads a JSON body the caller has already bounded (httpx.Decode's own bound is 1 MiB, a big manifest is more).
func decodeJSON(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var mb *http.MaxBytesError
		if errors.As(err, &mb) {
			return httpx.New(http.StatusRequestEntityTooLarge, "too_large", "Манифест слишком большой")
		}
		return httpx.New(http.StatusBadRequest, "invalid_request", "Некорректный JSON")
	}
	return nil
}
