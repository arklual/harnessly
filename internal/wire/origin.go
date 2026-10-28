package wire

import (
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// crossSite: see sameorigin.CrossSite. It guards what a person's session must not do for any page that asks.
func crossSite(r *http.Request) bool { return sameorigin.CrossSite(r) }

// guarded: the requests that must not come from another origin.
func guarded(p string, r *http.Request) bool {
	switch {
	case p == "socket", p == "replica", p == "terminal", p == "voice", p == "designly/create", p == "designly/pin":
		return true
	case p == "machine/processes/stop", p == "search", strings.HasPrefix(p, "move/"):
		return true
	case p == "me/settings", p == "runtime-settings", strings.HasPrefix(p, "agent-settings/"), p == "providers", strings.HasPrefix(p, "providers/"), strings.HasPrefix(p, "agents/"):
		return r.Method != http.MethodGet && r.Method != http.MethodHead
	case strings.HasPrefix(p, "conversations/") && strings.HasSuffix(p, "/side-chat"):
		return true
	case strings.HasPrefix(p, "conversations/") && strings.HasSuffix(p, "/handoff-draft"):
		return true
	case strings.HasPrefix(p, "conversations/") && strings.HasSuffix(p, "/digest"):
		return true
	case strings.HasPrefix(p, "conversations/") && strings.HasSuffix(p, "/shot"):
		return true
	case strings.HasPrefix(p, "conversations/") && strings.HasSuffix(p, "/file"):
		return r.Method != http.MethodGet && r.Method != http.MethodHead
	case strings.HasPrefix(p, "conversations/") && strings.Contains(p, "/terminals"):
		return r.Method != http.MethodGet && r.Method != http.MethodHead
	}
	return false
}
