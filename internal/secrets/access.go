package secrets

import (
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/vault"
)

// Origin canonicalizes an HTTPS site, or HTTP on an explicitly local development host.
// Paths, credentials, queries and fragments are rejected rather than silently discarded.
func Origin(raw string) string {
	if strings.HasPrefix(strings.ToLower(raw), "https://") {
		return vault.Origin(raw)
	}
	if !strings.HasPrefix(strings.ToLower(raw), "http://") {
		return ""
	}
	u, err := url.Parse("http://" + raw[len("http://"):])
	if err != nil {
		return ""
	}
	switch strings.ToLower(u.Hostname()) {
	case "localhost", "127.0.0.1", "::1":
	default:
		return ""
	}
	// Reuse the strict authority validation without losing HTTP's distinct default port.
	validated := vault.Origin("https://" + raw[len("http://"):])
	if validated == "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host == "::1" {
		host = "[::1]"
	}
	port := u.Port()
	if n, e := strconv.Atoi(port); e == nil {
		port = strconv.Itoa(n)
	}
	if port != "" && port != "80" {
		v, _ := url.Parse(validated)
		port = v.Port()
		if port == "" {
			port = "443"
		}
		host += ":" + port
	}
	return "http://" + host
}

// Allowed reports whether a verified project's chat may use this entry.
// An empty project never matches a scoped entry; old unscoped entries retain their behavior.
func (v View) Allowed(project string) bool {
	return len(v.Projects) == 0 || project != "" && slices.Contains(v.Projects, project)
}

func cleanAccess(origins, projects []string) ([]string, []string, error) {
	sites, scopes := []string{}, []string{}
	for _, raw := range origins {
		site := Origin(raw)
		if site == "" {
			return nil, nil, refuse("bad-origin", nil)
		}
		if !slices.Contains(sites, site) {
			sites = append(sites, site)
		}
	}
	for _, project := range projects {
		if project == "" || strings.TrimSpace(project) != project || len(project) > 200 || strings.ContainsAny(project, "\r\n\x00") {
			return nil, nil, refuse("bad-project", nil)
		}
		if !slices.Contains(scopes, project) {
			scopes = append(scopes, project)
		}
	}
	return sites, scopes, nil
}

// Metadata changes only public access constraints, never the encrypted value.
func (s *Store) Metadata(name string, origins, projects []string) (View, error) {
	sites, scopes, err := cleanAccess(origins, projects)
	if err != nil {
		return View{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.read()
	if err != nil {
		return View{}, err
	}
	e := find(f, name)
	if e == nil {
		return View{}, refuse(ReasonNotFound, nil)
	}
	e.Origins, e.Projects = sites, scopes
	e.UpdatedAt = s.now()
	if err = s.write(f); err != nil {
		return View{}, err
	}
	return viewOf(e), nil
}

// Grant adds a project and requested site after a person approves an access card.
func (s *Store) Grant(name, project, origin string) error {
	return s.grant(name, project, origin, true)
}

// GrantOrigin adds only a site and rechecks project access when the card is answered.
func (s *Store) GrantOrigin(name, project, origin string) error {
	if origin == "" {
		return refuse("bad-origin", nil)
	}
	return s.grant(name, project, origin, false)
}

func (s *Store) grant(name, project, origin string, projectGrant bool) error {
	if projectGrant && project == "" {
		return refuse(ReasonNotFound, nil)
	}
	if origin != "" {
		origin = Origin(origin)
		if origin == "" {
			return refuse("bad-origin", nil)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.read()
	if err != nil {
		return err
	}
	e := find(f, name)
	if e == nil || !projectGrant && !e.Allowed(project) {
		return refuse(ReasonNotFound, nil)
	}
	if projectGrant && len(e.Projects) > 0 && !slices.Contains(e.Projects, project) {
		e.Projects = append(e.Projects, project)
	}
	if origin != "" && !slices.Contains(e.Origins, origin) {
		e.Origins = append(e.Origins, origin)
	}
	e.UpdatedAt = s.now()
	return s.write(f)
}

// Lookup returns only public metadata visible to the project. A foreign entry looks absent.
func (s *Store) Lookup(name, project string) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.read()
	if err != nil {
		return View{}, err
	}
	e := find(f, name)
	if e == nil || !e.Allowed(project) {
		return View{}, refuse(ReasonNotFound, nil)
	}
	return viewOf(e), nil
}
