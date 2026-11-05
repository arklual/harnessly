package layoutapi

import (
	"net/http"
	"testing"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
)

func TestBrowserCLIGuard(t *testing.T) {
	r := newRig(t)
	r.deps.Browser = func(_ *http.Request, session, name string, args *jsvalue.Object) *jsvalue.Object {
		if session != "hs-agent" || name != "browser_navigate" || args.String("url") != "about:blank" {
			t.Fatalf("wrong caller or args: %s %s %v", session, name, args)
		}
		return mcpserver.TextResult("opened", false)
	}
	r.status.agentTabs["hs-agent"] = "tab-a"
	path := "/api/cli/browser/browser_navigate"
	body := `{"session":"hs-agent","arguments":{"url":"about:blank"}}`
	r.want(r.do("", "POST", path, body), 403, "*")
	r.want(r.do("s", "POST", path, body), 403, "*")
	r.want(r.do("t", "GET", path, ""), 405, "*")
	r.want(r.do("t", "POST", path, `{"session":"unknown"}`), 404, "*")
	r.want(r.do("t", "POST", path, `{"session":"hs-agent","arguments":[]}`), 400, "*")
	r.want(r.do("t", "POST", path, body), 200, `{"content":[{"type":"text","text":"opened"}]}`)
}
