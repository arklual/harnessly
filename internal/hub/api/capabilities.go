package api

import (
	"net/http"
	"slices"

	"github.com/ly-solutions/harnessly/internal/capabilities"
	"github.com/ly-solutions/harnessly/internal/hub/ext"
	"github.com/ly-solutions/harnessly/internal/hub/features"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
)

// flagSettings names, per organization flag, what the page gets from it.
var flagSettings = map[string]string{
	features.UIHX:             "ui.hx",
	features.Env:              "env",
	features.Catalog:          "catalog",
	features.MachineResources: "machine.resources",
	features.OrgEvents:        "org.events",
}

// capabilitiesDoc is what this hub does, in the one shape the web reads (internal/capabilities). The organizations'
// flags stand as they are on the instance: a flag an organization turns on itself shows here as needing config.
func (h *Hub) capabilitiesDoc(r *http.Request) capabilities.Document {
	set := capabilities.New("hub")
	on := h.Features.Instance()
	for _, name := range features.Known {
		set.Switch("flag."+flagSettings[name], slices.Contains(on, name),
			"off for an organization until it, or the instance, turns it on", "HUB_FEATURES_DEFAULT="+name+" (every organization) or the organization's own flag")
	}
	set.Switch("ssh", h.SSH.Available(), "connecting a server over SSH is off", "HUB_SSH")
	set.Switch("sites", h.Sites != nil, "published sites are off", "HARNESSLY_SITES")
	set.Switch("sign-in.passkey", h.Auth.PasskeyOn(), "passkeys are not set up", "HUB_PUBLIC_URL over https")
	set.Switch("sign-in.github", h.cfg.GitHubOn(), "GitHub sign-in is not set up", "HUB_GITHUB_CLIENT_ID, HUB_GITHUB_CLIENT_SECRET")
	set.Switch("sign-in.oidc", len(h.Auth.OIDCProviders()) > 0, "no OpenID provider is set up", "HUB_OIDC_PROVIDERS")
	// The hub carries no gateway of the platform: its pages never ask it for /api/platform/*.
	set.Absent("platform-gateway", "the hub does not serve /api/platform/*; a runtime page asks its own runtime")
	// An adapter is named by what a person set up, never by its address or by what it said: the answer is open.
	if h.Ext != nil {
		reasons := map[string]string{
			ext.HealthPending:  "the adapter has not been asked yet",
			ext.HealthMismatch: "the adapter speaks another protocol version than the hub expects",
			ext.HealthDown:     "the adapter does not answer",
		}
		for _, st := range h.Ext.Statuses() {
			set.Switch("extension."+st.Name, st.Health == ext.HealthOK, reasons[st.Health], "HUB_EXTENSIONS_FILE; `hub doctor` says more")
		}
	}
	return set.Document()
}

// capabilities answers GET /api/capabilities. Like /api/config it is open: it names no person and no secret.
func (h *Hub) capabilities(w http.ResponseWriter, r *http.Request) error {
	httpx.WriteJSON(w, http.StatusOK, h.capabilitiesDoc(r))
	return nil
}
