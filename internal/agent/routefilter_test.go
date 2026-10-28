package agent

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func TestRouteFilterDecisions(t *testing.T) {
	f := newRouteFilter(1, "/m/abc", DefaultPolicy, nil)
	f.roots = []string{t.TempDir()}
	off := func(mut func(*hubproto.Policy)) hubproto.Policy { p := DefaultPolicy(); mut(&p); return p }
	noTerminal := off(func(p *hubproto.Policy) { p.Terminal = false })
	denied := off(func(p *hubproto.Policy) { p.DenyRoutes = []string{"/api/fs/write", "/api/layout/file-content"} })
	tests := []struct {
		name   string
		policy hubproto.Policy
		method string
		path   string
		body   string
		deny   bool
		// ct is the request's content type: JSON for a body when empty, none for "-".
		ct string
	}{
		{"the default lets everything", DefaultPolicy(), "GET", "/m/abc/api/terminal", "", false, ""},
		{"terminal socket", noTerminal, "GET", "/m/abc/api/terminal", "", true, ""},
		{"terminal socket without the base", noTerminal, "GET", "/api/terminal", "", true, ""},
		{"terminal socket under /agents", noTerminal, "GET", "/agents/api/terminal", "", true, ""},
		{"terminal socket under base and /agents", noTerminal, "GET", "/m/abc/agents/api/terminal", "", true, ""},
		{"a dot segment", noTerminal, "GET", "/m/abc/api/./terminal", "", true, ""},
		{"a parent segment", noTerminal, "GET", "/m/abc/api/x/../terminal", "", true, ""},
		{"doubled slashes", noTerminal, "GET", "/m/abc//api//terminal", "", true, ""},
		{"a sub-route", noTerminal, "GET", "/m/abc/api/terminal/size", "", true, ""},
		{"the chat page's terminal socket", noTerminal, "GET", "/m/abc/api/v2/terminal", "", true, ""},
		{"the chat page's socket under /agents", noTerminal, "GET", "/m/abc/agents/api/v2/terminal", "", true, ""},
		{"the chat page's socket, dot segments", noTerminal, "GET", "/m/abc/api/v2/./x/../terminal", "", true, ""},
		{"own shells of a conversation", noTerminal, "POST", "/m/abc/api/v2/conversations/c1/terminals", "", true, ""},
		{"own shells listed", noTerminal, "GET", "/m/abc/api/v2/conversations/c1/terminals", "", true, ""},
		{"one own shell closed", noTerminal, "DELETE", "/m/abc/api/v2/conversations/c1/terminals/t1", "", true, ""},
		{"a conversation's other routes pass", noTerminal, "GET", "/m/abc/api/v2/conversations/c1/context", "", false, ""},
		{"a conversation named terminals passes", noTerminal, "GET", "/m/abc/api/v2/conversations/terminals/context", "", false, ""},
		{"the chat page's terminal with the terminal on", DefaultPolicy(), "GET", "/m/abc/api/v2/terminal", "", false, ""},
		{"the login console's page", noTerminal, "GET", "/m/abc/console/0123456789abcdef", "", true, ""},
		{"the login console's keys", noTerminal, "POST", "/m/abc/api/console/0123456789abcdef/keys", `{}`, true, ""},
		{"the login console's screen", noTerminal, "GET", "/m/abc/api/console/0123456789abcdef/screen", "", true, ""},
		{"the login console with the terminal on", DefaultPolicy(), "GET", "/m/abc/console/0123456789abcdef", "", false, ""},
		{"other sockets pass", noTerminal, "GET", "/m/abc/api/status", "", false, ""},
		{"the page passes", noTerminal, "GET", "/m/abc/", "", false, ""},
		{"another machine's base is no base", noTerminal, "GET", "/m/abd/api/status", "", false, ""},
		{"tab list is a read", noTerminal, "GET", "/m/abc/api/layout/pane/p1/tabs", "", false, ""},
		{"a terminal tab", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"panelType":"terminal"}`, true, ""},
		{"a tab with no type is a terminal", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"name":"x"}`, true, ""},
		{"an empty body", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", ``, true, ""},
		{"a tab of Claude", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"panelType":"claude-code"}`, false, ""},
		{"a tab of Codex", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"panelType":"codex-cli","name":"c"}`, false, ""},
		{"a tab of Claude with a command", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"panelType":"claude-code","command":"rm -rf ~"}`, true, ""},
		{"a browser tab", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"panelType":"web-browser"}`, false, ""},
		{"a tab with a type that is not text", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"panelType":5}`, true, ""},
		{"a body that is not JSON", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `not json`, true, ""},
		{"a split into a terminal", noTerminal, "POST", "/m/abc/api/layout/pane", `{"sourcePaneId":"p1","orientation":"row","panelType":"terminal"}`, true, ""},
		{"a bare new pane", noTerminal, "POST", "/m/abc/api/layout/pane", `{}`, true, ""},
		{"a split into Claude", noTerminal, "POST", "/m/abc/api/layout/pane", `{"sourcePaneId":"p1","orientation":"row","panelType":"claude-code"}`, false, ""},
		{"tab creation with the terminal on", DefaultPolicy(), "POST", "/m/abc/api/layout/pane/p1/tabs", `{"panelType":"terminal"}`, false, ""},
		{"closing a tab is not creating one", noTerminal, "DELETE", "/m/abc/api/layout/pane/p1/tabs/t1", ``, false, ""},
		{"a denied route", denied, "POST", "/m/abc/api/fs/write", `{}`, true, ""},
		{"a denied route, any method", denied, "GET", "/m/abc/api/fs/write", ``, true, ""},
		{"a denied route, a longer path", denied, "PUT", "/m/abc/api/fs/write/x/y", ``, true, ""},
		{"a denied route without the base", denied, "GET", "/api/layout/file-content", ``, true, ""},
		{"a denied route under /agents", denied, "GET", "/agents/api/layout/file-content", ``, true, ""},
		{"a route that only starts alike is denied by prefix", denied, "GET", "/m/abc/api/fs/writer", ``, true, ""},
		{"a route that does not", denied, "GET", "/m/abc/api/fs/read", ``, false, ""},

		// The body is read as the runtime reads it: JSON for a JSON content type, a form for a form, anything else is text
		// with no fields, and a tab with no panel type is a terminal.
		{"JSON sent as text is a terminal to the runtime", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"panelType":"claude-code"}`, true, "text/plain"},
		{"JSON sent with no content type is text", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"panelType":"claude-code"}`, true, "-"},
		{"JSON sent as a form is not a field", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"panelType":"claude-code"}`, true, "application/x-www-form-urlencoded"},
		{"JSON with a charset", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"panelType":"claude-code"}`, false, "Application/JSON; charset=utf-8"},
		{"a form for Claude", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `panelType=claude-code&name=x`, false, "application/x-www-form-urlencoded"},
		{"a form for a terminal", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `panelType=terminal`, true, "application/x-www-form-urlencoded"},
		{"a form with the type twice is a list, which the runtime reads as no type", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `panelType=claude-code&panelType=terminal`, true, "application/x-www-form-urlencoded"},
		{"a form with a command", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `panelType=claude-code&command=id`, true, "application/x-www-form-urlencoded"},
		{"the name in another case is not the runtime's field", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"PANELTYPE":"claude-code"}`, true, ""},
		{"the name in another case next to the real one", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"panelType":"claude-code","PanelType":"terminal"}`, true, ""},
		{"the real name after another case", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"PanelType":"claude-code","panelType":"terminal"}`, true, ""},
		{"the type twice, terminal last", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"panelType":"claude-code","panelType":"terminal"}`, true, ""},
		{"the type twice, Claude last", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"panelType":"terminal","panelType":"claude-code"}`, true, ""},
		{"the type spelled with an escape", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"panel\u0054ype":"terminal"}`, true, ""},
		{"the command twice", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"panelType":"claude-code","command":"id","command":""}`, true, ""},
		{"a command in another case", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"panelType":"claude-code","Command":"id"}`, true, ""},
		{"an empty command", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"panelType":"claude-code","command":""}`, false, ""},
		{"a null command", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"panelType":"claude-code","command":null}`, false, ""},
		{"a blank command is still typed", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"panelType":"claude-code","command":" "}`, true, ""},
		{"a command that is a list", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"panelType":"claude-code","command":["id"]}`, true, ""},
		{"a panel type nobody knows", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"panelType":"bash"}`, true, ""},
		{"an empty panel type", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `{"panelType":""}`, true, ""},
		{"a body that is a list", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", `["claude-code"]`, true, ""},
		{"a body with a BOM", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs", "\xef\xbb\xbf" + `{"panelType":"claude-code"}`, false, ""},
		{"a split with no orientation is a new pane with a terminal", noTerminal, "POST", "/m/abc/api/layout/pane", `{"sourcePaneId":"p1","panelType":"claude-code"}`, true, ""},
		{"a split with no panel type", noTerminal, "POST", "/m/abc/api/layout/pane", `{"sourcePaneId":"p1","orientation":"row"}`, true, ""},
		{"a split with a terminal type in another case", noTerminal, "POST", "/m/abc/api/layout/pane", `{"sourcePaneId":"p1","orientation":"row","PanelType":"claude-code"}`, true, ""},
		{"a split into a browser", noTerminal, "POST", "/m/abc/api/layout/pane", `{"sourcePaneId":"p1","orientation":"row","panelType":"web-browser"}`, false, ""},
		{"restarting a tab", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs/t1", `{}`, false, ""},
		{"restarting a tab with no body", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs/t1", ``, false, ""},
		{"restarting a tab with a command", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs/t1", `{"command":"curl x | sh"}`, true, ""},
		{"restarting a tab with a command sent as a form", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs/t1", `command=id`, true, "application/x-www-form-urlencoded"},
		{"restarting with the terminal on", DefaultPolicy(), "POST", "/m/abc/api/layout/pane/p1/tabs/t1", `{"command":"id"}`, false, ""},
		{"renaming a tab", noTerminal, "PATCH", "/m/abc/api/layout/pane/p1/tabs/t1", `{"name":"x"}`, false, ""},
		{"moving a tab", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs/t1/move", `{"toPaneId":"p2","toIndex":0}`, false, ""},
		{"ordering tabs", noTerminal, "PATCH", "/m/abc/api/layout/pane/p1/tabs/order", `{"tabIds":["a"]}`, false, ""},
		{"a layout patch", noTerminal, "PATCH", "/m/abc/api/layout", `{"activePaneId":"p1"}`, false, ""},
		{"closing a pane", noTerminal, "DELETE", "/m/abc/api/layout/pane/p1", ``, false, ""},
		{"doubled slashes in a tab for Claude", noTerminal, "POST", "/m/abc//api//layout//pane//p1//tabs", `{"panelType":"claude-code"}`, false, ""},
		{"doubled slashes in a terminal tab", noTerminal, "POST", "/m/abc//api//layout//pane//p1//tabs", `{"panelType":"terminal"}`, true, ""},
		{"a trailing slash in a terminal tab", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs/", `{"panelType":"terminal"}`, true, ""},
		{"a dot segment in a terminal tab", noTerminal, "POST", "/m/abc/api/./layout/pane/p1/tabs", `{"panelType":"terminal"}`, true, ""},
		{"a dot segment in a tab for Claude is not guessed", noTerminal, "POST", "/m/abc/api/./layout/pane/p1/tabs", `{"panelType":"claude-code"}`, true, ""},
		{"a parent segment into a terminal tab", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs/../../../pane/p1/tabs", `{"panelType":"terminal"}`, true, ""},
		{"an encoded slash in the pane's name", noTerminal, "POST", "/m/abc/api/layout/pane/p1%2Fx/tabs", `{"panelType":"terminal"}`, true, ""},
		{"an encoded slash in the path's tail", noTerminal, "POST", "/m/abc/api/layout/pane/p1/tabs%2Fx", `{"panelType":"terminal"}`, true, ""},
		{"a lower-case method", noTerminal, "post", "/m/abc/api/layout/pane/p1/tabs", `{"panelType":"claude-code"}`, true, ""},
		{"a method nobody uses", noTerminal, "PROPFIND", "/m/abc/api/status", ``, true, ""},
		{"a write with the other machine's base", noTerminal, "POST", "/m/abd/api/layout/pane/p1/tabs", `{"panelType":"claude-code"}`, true, ""},
		{"a write outside /api", noTerminal, "POST", "/m/abc/something", `{}`, true, ""},
		{"a tab for Claude through /agents", noTerminal, "POST", "/agents/api/layout/pane/p1/tabs", `{"panelType":"claude-code"}`, false, ""},
		{"a terminal tab through /agents under the base", noTerminal, "POST", "/m/abc/agents/api/layout/pane/p1/tabs", `{"panelType":"terminal"}`, true, ""},

		// A workspace is made with a terminal in it unless it resumes an agent's conversation.
		{"a workspace", noTerminal, "POST", "/m/abc/api/workspace", `{"directory":"/srv/x"}`, true, ""},
		{"a workspace with a panel type and no conversation", noTerminal, "POST", "/m/abc/api/workspace", `{"directory":"/srv/x","panelType":"claude-code"}`, true, ""},
		{"a workspace resuming Claude", noTerminal, "POST", "/m/abc/api/workspace", `{"directory":"/srv/x","resumeSessionId":"0f8fad5b-d9cb-469f-a165-70867728950e"}`, false, ""},
		{"a workspace resuming Codex", noTerminal, "POST", "/m/abc/api/workspace", `{"resumeSessionId":"0f8fad5b-d9cb-469f-a165-70867728950e","panelType":"codex-cli"}`, false, ""},
		{"a workspace resuming into a terminal", noTerminal, "POST", "/m/abc/api/workspace", `{"resumeSessionId":"0f8fad5b-d9cb-469f-a165-70867728950e","panelType":"terminal"}`, true, ""},
		{"a workspace resuming a session that is not an id", noTerminal, "POST", "/m/abc/api/workspace", `{"resumeSessionId":"x; id"}`, true, ""},
		{"a workspace resuming twice", noTerminal, "POST", "/m/abc/api/workspace", `{"resumeSessionId":"0f8fad5b-d9cb-469f-a165-70867728950e","resumeSessionId":""}`, true, ""},
		{"a workspace with the terminal on", DefaultPolicy(), "POST", "/m/abc/api/workspace", `{"directory":"/srv/x"}`, false, ""},
		{"the workspace list", noTerminal, "GET", "/m/abc/api/workspace", ``, false, ""},
		{"renaming a workspace", noTerminal, "PATCH", "/m/abc/api/workspace/ws-1", `{"name":"x"}`, false, ""},
		{"closing a workspace", noTerminal, "DELETE", "/m/abc/api/workspace/ws-1", ``, false, ""},

		// Anything that is not on the list does not change things while the terminal is off.
		{"raw keys into a tmux session", noTerminal, "POST", "/m/abc/api/tmux/send-input", `{"session":"s","input":"id\n"}`, true, ""},
		{"answering a prompt with keys", noTerminal, "POST", "/m/abc/api/tmux/answer-question", `{}`, true, ""},
		{"the screen of a tmux session is read", noTerminal, "GET", "/m/abc/api/tmux/capture?session=s", ``, false, ""},
		{"the CLI's tabs", noTerminal, "POST", "/m/abc/api/cli/tabs", `{"command":"id"}`, true, ""},
		{"the CLI sends keys", noTerminal, "POST", "/m/abc/api/cli/tabs/t1/send", `{}`, true, ""},
		{"a shell in a pane", noTerminal, "POST", "/m/abc/api/platform/pane-shell", `{}`, true, ""},
		{"the agent's settings carry MCP servers and rules", noTerminal, "PATCH", "/m/abc/api/v2/agent-settings/claude", `{}`, true, ""},
		{"the page's config carries the permission switch", noTerminal, "PATCH", "/m/abc/api/config", `{"dangerouslySkipPermissions":true}`, true, ""},
		{"tailscale", noTerminal, "POST", "/m/abc/api/tailscale/serve", `{}`, true, ""},
		{"a first-run password", noTerminal, "POST", "/m/abc/api/auth/setup", `{}`, true, ""},
		{"the console that makes a terminal for the agent", noTerminal, "POST", "/m/abc/api/agent/terminal-tab", `{}`, true, ""},
		{"an evaluation run", noTerminal, "POST", "/m/abc/api/platform/eval/start", `{}`, true, ""},
		{"a message of the chat", noTerminal, "POST", "/m/abc/api/v2/cmd/send", `{"opId":"o"}`, false, ""},
		{"an answer to an agent's question", noTerminal, "POST", "/m/abc/api/v2/cmd/answer", `{"opId":"o"}`, false, ""},
		{"stopping an agent", noTerminal, "POST", "/m/abc/api/v2/cmd/interrupt", `{"opId":"o"}`, false, ""},
		{"approving a tool call", noTerminal, "POST", "/m/abc/api/v2/cmd/decide", `{"opId":"o","permissionId":"p","optionId":"1"}`, true, ""},
		{"approving a tool call, any body", noTerminal, "POST", "/m/abc/api/v2/cmd/decide", ``, true, ""},
		{"approving under /agents", noTerminal, "POST", "/m/abc/agents/api/v2/cmd/decide", `{}`, true, ""},
		{"approving without the base", noTerminal, "POST", "/api/v2/cmd/decide", `{}`, true, ""},
		{"approving, doubled slashes", noTerminal, "POST", "/m/abc//api/v2//cmd//decide", `{}`, true, ""},
		{"approving, dot segments", noTerminal, "POST", "/m/abc/api/v2/x/../cmd/./decide", `{}`, true, ""},
		{"approving, a trailing slash", noTerminal, "POST", "/m/abc/api/v2/cmd/decide/", `{}`, true, ""},
		{"approving with the terminal on", DefaultPolicy(), "POST", "/m/abc/api/v2/cmd/decide", `{}`, false, ""},
		{"the composer's text", noTerminal, "POST", "/m/abc/api/platform/prompt-text", `{"tabId":"t","text":"hi"}`, false, ""},
		{"starting a conversation", noTerminal, "POST", "/m/abc/api/platform/conversations", `{"provider":"claude"}`, false, ""},
		{"an upload", noTerminal, "POST", "/m/abc/api/upload-file", ``, false, ""},
		{"the editor's save", noTerminal, "PUT", "/m/abc/api/editor/file?session=s&path=src/main.go", `{"content":"x","revision":"r"}`, false, ""},
		{"the same with the terminal on", DefaultPolicy(), "POST", "/m/abc/api/tmux/send-input", `{"session":"s","input":"id\n"}`, false, ""},

		// The policy, the kill switch and the agent's own files are not written through the runtime, whatever is on.
		{"the editor saves the policy", DefaultPolicy(), "PUT", "/m/abc/api/editor/file?session=s&path=.config/harnessly/policy.toml", `{}`, true, ""},
		{"the editor saves the policy, the terminal off", noTerminal, "PUT", "/m/abc/api/editor/file?session=s&path=.config/harnessly/policy.toml", `{}`, true, ""},
		{"the editor saves a policy by its name anywhere", DefaultPolicy(), "PUT", "/m/abc/api/editor/file?session=s&path=work/policy.toml", `{}`, true, ""},
		{"the editor saves it in another case", DefaultPolicy(), "PUT", "/m/abc/api/editor/file?session=s&path=.Config/Harnessly/x", `{}`, true, ""},
		{"the editor saves it through a dot segment", DefaultPolicy(), "PUT", "/m/abc/api/editor/file?session=s&path=src/../.config/./harnessly/policy.toml", `{}`, true, ""},
		{"the editor saves it in another spelling of the key", DefaultPolicy(), "PUT", "/m/abc/api/editor/file?session=s&PATH=policy.toml", `{}`, true, ""},
		{"the editor saves it with the path given twice", DefaultPolicy(), "PUT", "/m/abc/api/editor/file?session=s&path=a.go&path=policy.toml", `{}`, true, ""},
		{"the editor saves it percent-encoded", DefaultPolicy(), "PUT", "/m/abc/api/editor/file?session=s&path=%70olicy.toml", `{}`, true, ""},
		{"the editor saves the kill switch", DefaultPolicy(), "PUT", "/m/abc/api/editor/file?session=s&path=.harnessly/disabled", `{}`, true, ""},
		{"the editor saves the system policy", DefaultPolicy(), "PUT", "/m/abc/api/editor/file?session=s&path=etc/harnessly/runtime.env", `{}`, true, ""},
		{"the editor saves an env file of the agent", DefaultPolicy(), "PUT", "/m/abc/api/editor/file?session=s&path=.harnessly/agent-env/all.env", `{}`, true, ""},
		{"the editor saves the machine's identity", DefaultPolicy(), "PUT", "/m/abc/api/editor/file?session=s&path=.harnessly/machine.json", `{}`, true, ""},
		{"the editor saves the audit log", DefaultPolicy(), "PUT", "/m/abc/api/editor/file?session=s&path=.harnessly/agent-audit.log", `{}`, true, ""},
		{"the chat page saves the policy", DefaultPolicy(), "PUT", "/m/abc/api/v2/conversations/c1/file?path=.config/harnessly/policy.toml", `{}`, true, ""},
		{"the chat page saves a project file", DefaultPolicy(), "PUT", "/m/abc/api/v2/conversations/c1/file?path=src/app.ts", `{}`, false, ""},
		{"the policy file is read", DefaultPolicy(), "GET", "/m/abc/api/editor/file?session=s&path=.config/harnessly/policy.toml", ``, false, ""},
		{"a project folder that only looks alike", DefaultPolicy(), "PUT", "/m/abc/api/editor/file?session=s&path=harnessly/src/policy.go", `{}`, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u, err := url.Parse("http://x" + tc.path)
			if err != nil {
				t.Fatal(err)
			}
			r := &http.Request{Method: tc.method, URL: u, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body))}
			switch {
			case tc.ct == "-":
			case tc.ct != "":
				r.Header.Set("Content-Type", tc.ct)
			case tc.body != "":
				r.Header.Set("Content-Type", "application/json")
			}
			reason, err := f.decide(tc.policy, r)
			if err != nil {
				t.Fatal(err)
			}
			if (reason != "") != tc.deny {
				t.Errorf("reason %q, want deny=%v", reason, tc.deny)
			}
			// What was read to decide is still there for the runtime.
			if got, _ := io.ReadAll(r.Body); string(got) != tc.body {
				t.Errorf("the body was consumed: %q", got)
			}
		})
	}
}

// A percent-encoded path means the same to the runtime as the plain one; so it does here.
func TestRouteFilterDecodesPaths(t *testing.T) {
	f := newRouteFilter(1, "/m/abc", DefaultPolicy, nil)
	f.roots = []string{t.TempDir()}
	p := DefaultPolicy()
	p.Terminal = false
	for _, raw := range []string{"/m/abc/api/%74erminal", "/m/abc/api/terminal%2F", "/m/abc/%61pi/terminal"} {
		r, _ := http.NewRequest("GET", "http://x"+raw, nil)
		if reason, _ := f.decide(p, r); reason == "" && !strings.Contains(r.URL.Path, "%") {
			t.Errorf("%s (%s) passed", raw, r.URL.Path)
		}
	}
	// the approval of a tool call, spelled with escapes, is the same route
	for _, raw := range []string{"/m/abc/api/v2/cmd/%64ecide", "/m/abc/api/v2/%63md/decide", "/m/abc/%61pi/v2/cmd/decide", "/m/abc/api/v2/cmd/decide%2F"} {
		r, _ := http.NewRequest("POST", "http://x"+raw, strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		if reason, _ := f.decide(p, r); reason == "" {
			t.Errorf("%s (%s) passed", raw, r.URL.Path)
		}
	}
}

type fakeRuntime struct {
	srv  *httptest.Server
	port int
	last atomic.Pointer[http.Request]
	body atomic.Pointer[string]
}

func newFakeRuntime(t *testing.T) *fakeRuntime {
	t.Helper()
	rt := &fakeRuntime{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
			if err != nil {
				return
			}
			defer c.CloseNow()
			for {
				typ, data, err := c.Read(r.Context())
				if err != nil {
					return
				}
				if err := c.Write(r.Context(), typ, append([]byte("echo:"), data...)); err != nil {
					return
				}
			}
		}
		b, _ := io.ReadAll(r.Body)
		s := string(b)
		rt.body.Store(&s)
		rt.last.Store(r.Clone(r.Context()))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"path": r.URL.RequestURI(), "host": r.Host})
	})
	rt.srv = httptest.NewServer(mux)
	t.Cleanup(rt.srv.Close)
	rt.port = rt.srv.Listener.Addr().(*net.TCPAddr).Port
	return rt
}

func startFilter(t *testing.T, rt *fakeRuntime, policy *atomic.Pointer[hubproto.Policy]) string {
	t.Helper()
	f := newRouteFilter(rt.port, "/m/abc", func() hubproto.Policy { return *policy.Load() }, nil)
	f.roots = []string{t.TempDir()}
	port, err := f.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.Close)
	return "127.0.0.1:" + strconv.Itoa(port)
}

func TestRouteFilterPassesWhatIsAllowedUnchanged(t *testing.T) {
	rt := newFakeRuntime(t)
	var policy atomic.Pointer[hubproto.Policy]
	p := DefaultPolicy()
	policy.Store(&p)
	addr := startFilter(t, rt, &policy)

	req, _ := http.NewRequest("POST", "http://"+addr+"/m/abc/api/layout/pane/p1/tabs?w=1", strings.NewReader(`{"panelType":"claude-code"}`))
	req.Host = "hub.example.com"
	req.Header.Set("X-Forwarded-For", "203.0.113.5")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "hub.example.com")
	req.Header.Set("Cookie", "session-token=abc")
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got map[string]string
	_ = json.NewDecoder(res.Body).Decode(&got)
	if res.StatusCode != 200 || got["path"] != "/m/abc/api/layout/pane/p1/tabs?w=1" || got["host"] != "hub.example.com" {
		t.Fatalf("%d %v", res.StatusCode, got)
	}
	seen := rt.last.Load()
	for k, want := range map[string]string{"X-Forwarded-For": "203.0.113.5", "X-Forwarded-Proto": "https", "X-Forwarded-Host": "hub.example.com", "Cookie": "session-token=abc"} {
		if seen.Header.Get(k) != want {
			t.Errorf("%s reached the runtime as %q", k, seen.Header.Get(k))
		}
	}
	if b := rt.body.Load(); b == nil || *b != `{"panelType":"claude-code"}` {
		t.Errorf("body %v", b)
	}
}

func TestRouteFilterDeniesAndFollowsThePolicyLive(t *testing.T) {
	rt := newFakeRuntime(t)
	var policy atomic.Pointer[hubproto.Policy]
	p := DefaultPolicy()
	policy.Store(&p)
	addr := startFilter(t, rt, &policy)
	get := func(path string) (int, string) {
		res, err := http.Get("http://" + addr + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	if code, _ := get("/m/abc/api/terminal/size"); code != 200 {
		t.Fatalf("the default policy denied: %d", code)
	}
	narrow := DefaultPolicy()
	narrow.Terminal = false
	narrow.DenyRoutes = []string{"/api/fs/write"}
	policy.Store(&narrow)
	code, body := get("/m/abc/api/terminal/size")
	if code != 403 || !strings.Contains(body, `"code":"machine_policy"`) || !strings.Contains(body, "terminal") {
		t.Fatalf("%d %s", code, body)
	}
	if code, _ := get("/m/abc/api/fs/write"); code != 403 {
		t.Errorf("deny_routes: %d", code)
	}
	if code, _ := get("/m/abc/api/status"); code != 200 {
		t.Errorf("an allowed route: %d", code)
	}
	policy.Store(&p)
	if code, _ := get("/m/abc/api/terminal/size"); code != 200 {
		t.Errorf("widened again: %d", code)
	}
}

func TestRouteFilterWebSocketsPassUnlessDenied(t *testing.T) {
	rt := newFakeRuntime(t)
	var policy atomic.Pointer[hubproto.Policy]
	narrow := DefaultPolicy()
	narrow.Terminal = false
	policy.Store(&narrow)
	addr := startFilter(t, rt, &policy)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, "ws://"+addr+"/m/abc/api/status", nil)
	if err != nil {
		t.Fatalf("an allowed socket: %v", err)
	}
	defer c.CloseNow()
	if err := c.Write(ctx, websocket.MessageText, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	_, data, err := c.Read(ctx)
	if err != nil || string(data) != "echo:hi" {
		t.Fatalf("%q %v", data, err)
	}

	_, res, err := websocket.Dial(ctx, "ws://"+addr+"/m/abc/api/terminal?session=s", nil)
	if err == nil || res == nil || res.StatusCode != 403 {
		t.Fatalf("the terminal socket: %v %v", err, res)
	}
}

// What the filter refuses never reaches the runtime, in whatever spelling it comes, and what it lets through arrives
// byte for byte, so the runtime reads the same body the filter did.
func TestRouteFilterKeepsRefusedWritesFromTheRuntime(t *testing.T) {
	rt := newFakeRuntime(t)
	var policy atomic.Pointer[hubproto.Policy]
	narrow := DefaultPolicy()
	narrow.Terminal = false
	policy.Store(&narrow)
	addr := startFilter(t, rt, &policy)
	do := func(method, path, ct, body string) int {
		t.Helper()
		rt.last.Store(nil)
		req, err := http.NewRequest(method, "http://"+addr+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		_, _ = io.Copy(io.Discard, res.Body)
		return res.StatusCode
	}
	const js = "application/json"
	for _, tc := range []struct{ name, method, path, ct, body string }{
		{"a terminal tab", "POST", "/m/abc/api/layout/pane/p1/tabs", js, `{"panelType":"terminal"}`},
		{"a terminal tab sent as text", "POST", "/m/abc/api/layout/pane/p1/tabs", "text/plain", `{"panelType":"claude-code"}`},
		{"a tab with the key in capitals", "POST", "/m/abc/api/layout/pane/p1/tabs", js, `{"PanelType":"terminal","panelType":"claude-code"}`},
		{"a tab with the key twice", "POST", "/m/abc/api/layout/pane/p1/tabs", js, `{"panelType":"claude-code","panelType":"terminal"}`},
		{"a tab restarted with a command", "POST", "/m/abc/api/layout/pane/p1/tabs/t1", js, `{"command":"id"}`},
		{"a tab in a pane with an encoded slash", "POST", "/m/abc/api/layout/pane/p1%2Fx/tabs", js, `{"panelType":"terminal"}`},
		{"a workspace with a terminal", "POST", "/m/abc/api/workspace", js, `{"directory":"/"}`},
		{"keys into tmux", "POST", "/m/abc/api/tmux/send-input", js, `{"session":"s","input":"id\n"}`},
		{"the permission switch", "PATCH", "/m/abc/api/config", js, `{"dangerouslySkipPermissions":true}`},
		{"MCP servers", "PATCH", "/m/abc/api/v2/agent-settings/claude", js, `{}`},
		{"the policy through the editor", "PUT", "/m/abc/api/editor/file?session=s&path=.config/harnessly/policy.toml", js, `{"content":"x"}`},
		{"the policy through the chat page", "PUT", "/m/abc/api/v2/conversations/c1/file?path=policy.toml", js, `{"content":"x"}`},
	} {
		if code := do(tc.method, tc.path, tc.ct, tc.body); code != 403 || rt.last.Load() != nil {
			t.Errorf("%s: %d, reached the runtime: %v", tc.name, code, rt.last.Load() != nil)
		}
	}

	if code := do("POST", "/m/abc/api/layout/pane/p1/tabs", js, `{"panelType":"claude-code"}`); code != 200 || rt.last.Load() == nil {
		t.Fatalf("a tab of Claude: %d", code)
	}
	if b := rt.body.Load(); b == nil || *b != `{"panelType":"claude-code"}` {
		t.Errorf("the runtime read %v", b)
	}
	if code := do("PUT", "/m/abc/api/editor/file?session=s&path=src/main.go", js, `{"content":"x","revision":"r"}`); code != 200 {
		t.Errorf("a project file: %d", code)
	}

	// With the terminal on, the same write routes are the runtime's own business, but the policy is still not written.
	wide := DefaultPolicy()
	policy.Store(&wide)
	if code := do("POST", "/m/abc/api/tmux/send-input", js, `{}`); code != 200 {
		t.Errorf("keys with the terminal on: %d", code)
	}
	if code := do("PUT", "/m/abc/api/editor/file?session=s&path=.config/harnessly/policy.toml", js, `{}`); code != 403 || rt.last.Load() != nil {
		t.Errorf("the policy with the terminal on: %d", code)
	}
}
