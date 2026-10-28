package api

import (
	"net/http"

	"github.com/ly-solutions/harnessly/internal/hub/buildinfo"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
)

type oidcView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// config answers GET /api/config: which sign-in ways and connect ways this hub has, and nothing else.
func (h *Hub) config(w http.ResponseWriter, r *http.Request) error {
	oidc := []oidcView{}
	for _, p := range h.Auth.OIDCProviders() {
		oidc = append(oidc, oidcView{ID: p.ID, Name: p.Name})
	}
	sshOn := h.SSH.Available()
	mode := "path"
	if h.cfg.SubdomainMode() {
		mode = "subdomain"
	}
	var terms *string
	if h.cfg.TermsURL != "" {
		terms = &h.cfg.TermsURL
	}
	type obj = map[string]any
	sitesCfg := obj{"enabled": false}
	if h.Sites != nil {
		sitesCfg = obj{"enabled": true, "scheme": h.cfg.SitesMode(), "domain": h.cfg.SitesDomain}
	}
	httpx.WriteJSON(w, http.StatusOK, obj{
		"version":   buildinfo.Version,
		"publicUrl": h.cfg.PublicURL,
		"edition":   h.cfg.Edition,
		"signup":    h.cfg.Signup,
		"auth": obj{
			"email":   obj{"mode": h.Auth.MailMode()},
			"passkey": h.Auth.PasskeyOn(),
			"github":  h.cfg.GitHubOn(),
			"yandex":  h.cfg.YandexOn(),
			"oidc":    oidc,
		},
		"connect": obj{
			"local":   obj{"available": h.Machines.LocalAvailable(r.Context())},
			"command": true,
			// The key to add to a server is a space's own, shown to its owners by GET /api/spaces/{id}/machines/ssh; the
			// field stays for the clients that read it and is always null.
			"ssh":      obj{"available": sshOn, "publicKey": nil},
			"teleport": false,
		},
		"profiles":    obj{"dir": true, "container": h.cfg.ContainerImage != "", "full": true},
		"runtimeMode": mode,
		"sites":       sitesCfg,
		"termsUrl":    terms,
	})
	return nil
}
