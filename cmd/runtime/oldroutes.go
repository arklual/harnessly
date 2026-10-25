package main

import (
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/files"
)

// oldRoutes are the previous runtime's API routes as its router knew them ("<platform>" is the platform's prefix, a
// bracketed segment a parameter, "[...x]" the rest of the path): a request whose path none of them matches was answered
// "Not found" there, whoever asks.
var oldRoutes = []string{
	"/api/auth/login", "/api/auth/logout", "/api/auth/preflight", "/api/auth/setup", "/api/check-agent",
	"/api/check-claude", "/api/cli/api-guide", "/api/cli/design", "/api/cli/design/claims", "/api/cli/design/history",
	"/api/cli/panel", "/api/cli/preview/clear", "/api/cli/preview/console", "/api/cli/preview/mcp",
	"/api/cli/preview/network", "/api/cli/preview/server-log", "/api/cli/preview/settings", "/api/cli/preview/slots",
	"/api/cli/tabs", "/api/cli/tabs/[tabId]", "/api/cli/tabs/[tabId]/browser/console",
	"/api/cli/tabs/[tabId]/browser/eval", "/api/cli/tabs/[tabId]/browser/network",
	"/api/cli/tabs/[tabId]/browser/screenshot", "/api/cli/tabs/[tabId]/browser/url", "/api/cli/tabs/[tabId]/result",
	"/api/cli/tabs/[tabId]/send", "/api/cli/tabs/[tabId]/status", "/api/cli/vault", "/api/cli/workspaces",
	"/api/codex/launch-args", "/api/codex/launch-command", "/api/codex/sessions", "/api/config", "/api/editor/file",
	"/api/editor/files", "/api/git/branch", "/api/git/status", "/api/git/sync", "/api/health",
	"/api/internal/core/agent-start", "/api/internal/core/auto-resume", "/api/internal/core/codex-session",
	"/api/internal/core/mismatch", "/api/internal/core/turn-end", "/api/internal/core/workspace-prompts",
	"/api/internal/terminal/claim", "/api/internal/terminal/enter", "/api/internal/terminal/reconcile-cwd",
	"/api/keybindings", "/api/layout", "/api/layout/commit-diff", "/api/layout/cwd", "/api/layout/diff",
	"/api/layout/file-content", "/api/layout/git-log", "/api/layout/pane", "/api/layout/pane/[paneId]",
	"/api/layout/pane/[paneId]/tabs", "/api/layout/pane/[paneId]/tabs/order", "/api/layout/pane/[paneId]/tabs/[tabId]",
	"/api/layout/pane/[paneId]/tabs/[tabId]/move", "/api/manifest", "/api/message-history", "/api/preflight/runtime",
	"/api/push/diagnostic", "/api/push/register-session", "/api/push/subscribe", "/api/push/test",
	"/api/push/vapid-key", "/api/push/viewport", "/api/push/visibility", "/api/quick-prompts", "/api/react-scan",
	"/api/sidebar-items", "/api/stats/aggregated", "/api/stats/cache-status", "/api/stats/daily-report/cache",
	"/api/stats/daily-report/generate", "/api/stats/daily-report/list", "/api/stats/facets", "/api/stats/history",
	"/api/stats/overview", "/api/stats/projects", "/api/stats/sessions", "/api/stats/uptime",
	"/api/status/agent-launch", "/api/status/hook", "/api/status/statusline", "/api/system/tmux-sessions",
	"/api/tailscale/serve", "/api/tailscale/status", "/api/timeline/entries", "/api/timeline/message-counts",
	"/api/timeline/sessions", "/api/tmux/answer-question", "/api/tmux/capture", "/api/tmux/info",
	"/api/tmux/permission-options", "/api/tmux/plan-options", "/api/tmux/recover-unknown", "/api/tmux/reset",
	"/api/tmux/send-input", "/api/upload-file", "/api/upload-image", "/api/uploads/cleanup", "/api/uploads/[...path]",
	"/api/voice/assets/[...path]", "/api/workspace", "/api/workspace/active", "/api/workspace/group",
	"/api/workspace/group/[groupId]", "/api/workspace/group/reorder", "/api/workspace/reorder",
	"/api/workspace/validate", "/api/workspace/[workspaceId]", "<platform>/automations/rules",
	"<platform>/automations/rules/[id]", "<platform>/automations/runs", "<platform>/automations/start",
	"<platform>/automations/token", "<platform>/background", "<platform>/bootstrap", "<platform>/changes",
	"<platform>/checkpoints", "<platform>/commands", "<platform>/commands/run", "<platform>/commands/screen",
	"<platform>/context-files", "<platform>/conversations", "<platform>/design/library",
	"<platform>/design/library/boards", "<platform>/design/library/check", "<platform>/design/library/continue",
	"<platform>/design/library/create", "<platform>/design/library/history", "<platform>/design/library/import",
	"<platform>/design/library/locate", "<platform>/design/library/log", "<platform>/design/library/message",
	"<platform>/design/library/send", "<platform>/design/library/shot", "<platform>/design/library/thumb", "<platform>/docs/deliver",
	"<platform>/docs/notify", "<platform>/docs/resolve-pane", "<platform>/economics", "<platform>/eval/start",
	"<platform>/favicon", "<platform>/file-preview", "<platform>/gateway-ping", "<platform>/gitlab",
	"<platform>/gitlab-view", "<platform>/interrupt", "<platform>/launch-options", "<platform>/link-meta",
	"<platform>/link-meta/avatar", "<platform>/link-meta/projects", "<platform>/machine/load", "<platform>/machines",
	"<platform>/machine/stop", "<platform>/memory-note", "<platform>/net-events", "<platform>/organizer",
	"<platform>/pane-intents", "<platform>/panel-result", "<platform>/pane-shell", "<platform>/peers",
	"<platform>/preview/devtools", "<platform>/preview/devtools/server-log", "<platform>/preview/ports",
	"<platform>/preview/resolve", "<platform>/prompt-queue", "<platform>/prompt-text", "<platform>/release-notice",
	"<platform>/send-now", "<platform>/session-process", "<platform>/share-read/[id]", "<platform>/shares",
	"<platform>/shares/[id]", "<platform>/side-chat", "<platform>/snapshots", "<platform>/snapshots/[id]/download",
	"<platform>/snapshots/[id]/preview", "<platform>/sources", "<platform>/state-changed", "<platform>/stats",
	"<platform>/subagents", "<platform>/subagents/[id]", "<platform>/switch-model", "<platform>/task",
	"<platform>/vault-result",
}

// oldRouteMatches says whether the previous runtime's router had a route for the request's API path.
func oldRouteMatches(r *http.Request) bool {
	parts, _, ok := files.APIParts(r)
	if !ok {
		return false
	}
	for _, route := range oldRoutes {
		if routeMatches(strings.Split(strings.Trim(strings.Replace(route, "<platform>", platformAPI, 1), "/"), "/"), parts) {
			return true
		}
	}
	return false
}

func routeMatches(segments, parts []string) bool {
	for i, s := range segments {
		if strings.HasPrefix(s, "[...") {
			return len(parts) > i
		}
		if i >= len(parts) {
			return false
		}
		if !strings.HasPrefix(s, "[") && s != parts[i] {
			return false
		}
	}
	return len(parts) == len(segments)
}
