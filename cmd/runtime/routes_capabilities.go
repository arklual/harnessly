package main

import (
	"net/http"
	"os"
	"strings"

	"github.com/ly-solutions/harnessly/internal/capabilities"
	"github.com/ly-solutions/harnessly/internal/gate"
)

// GET /api/capabilities (HRN-737): what this runtime does, in the shape the hub answers too. It reads the settings the
// other mechanisms already read (RUNTIME_WEB_FEATURES, the services' environment) and changes none of them. Open like
// the page itself: it names no person, no token and no address.

func init() { routeGroups = append(routeGroups, capabilityRoutes) }

func capabilityRoutes(_ deps) []gate.JSONRoute {
	return []gate.JSONRoute{{
		// No slice: a runtime always answers it, whatever the state-mode file says.
		Match: routeAt("/api/capabilities"),
		Compute: func(r *http.Request) (int, any, http.Header) {
			if r.Method != http.MethodGet {
				return http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"}, http.Header{"Allow": {"GET"}}
			}
			return http.StatusOK, runtimeCapabilities(os.Getenv), http.Header{"Cache-Control": {"no-store"}}
		},
	}}
}

// gatewayReason is why a capability is off for a runtime on its own: its routes are the platform gateway's, which sits in
// front of the runtime of a deployment and is not part of the public server (tools/web-routes-known.json).
const gatewayReason = "served by a platform gateway in front of the runtime; the public runtime has no such part"

// runtimeCapabilities reads its settings through getenv, so a test can give it any environment.
func runtimeCapabilities(getenv func(string) string) capabilities.Document {
	set := capabilities.New("runtime")
	web := pageFeatures(getenv("RUNTIME_WEB_FEATURES"))
	has := func(names ...string) bool {
		for _, n := range names {
			if strings.TrimSpace(getenv(n)) == "" {
				return false
			}
		}
		return true
	}

	// The optional parts of the page. The flag says the deployment has the part: efficiency and vault are routes of the
	// gateway, so without one the flag is on in the page and the server still has nothing behind it.
	for _, name := range []string{"efficiency", "vault"} {
		if web[name] {
			set.Add(capabilities.Capability{ID: name, Status: capabilities.On})
		} else {
			set.Add(capabilities.Capability{ID: name, Status: capabilities.Off, Reason: gatewayReason, Configure: "RUNTIME_WEB_FEATURES=" + name + " with a gateway in front"})
		}
	}
	set.Switch("chats", web["chats"], "no OpenWebUI is named", "RUNTIME_WEB_FEATURES=chats")
	set.Switch("design-library", web["design"], "the design library tab of the old panel is off", "RUNTIME_WEB_FEATURES=design")

	// Services that call out with the person's tokens (cmd/runtime/routes_services.go).
	tracker := has("RUNTIME_TRACKER_URL")
	gitlab := has("RUNTIME_GITLAB_HOSTS")
	docs := has("RUNTIME_DOCS_HOST") || has("RUNTIME_ARTIFACTS_URL") || has("RUNTIME_BOARDS_URL")
	set.Switch("gitlab", gitlab, "no GitLab host is named", "RUNTIME_GITLAB_HOSTS")
	set.Switch("tracker", tracker, "no tracker is named", "RUNTIME_TRACKER_URL")
	set.Switch("link-meta", tracker && gitlab && docs, "link previews need a tracker, a GitLab host and a documents host", "RUNTIME_TRACKER_URL, RUNTIME_GITLAB_HOSTS, RUNTIME_DOCS_HOST")
	set.Switch("docs", docs, "no documents or boards service is named", "RUNTIME_DOCS_HOST or RUNTIME_ARTIFACTS_URL")
	set.Switch("hub-link", has("RUNTIME_HUB_URL") || has("RUNTIME_HUB_LINK"), "this runtime is not connected to a hub", "RUNTIME_HUB_URL")

	// Parts that only a gateway serves: nothing the runtime is set up with changes that.
	set.Absent("history", "the conversation history is "+gatewayReason)
	set.Absent("voice-cloud", "cloud voice takes are "+gatewayReason)
	set.Switch("disk", has("RUNTIME_DISK_ROOTS"), "no folders are named for the disk screen", "RUNTIME_DISK_ROOTS=Label=/abs/path")
	return set.Document()
}
