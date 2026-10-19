package layoutapi

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// The goldens are the previous runtime's answers, which were in Russian.
	os.Setenv("RUNTIME_LOCALE", "ru")
	os.Exit(m.Run())
}

func TestRefusalsInEnglish(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	r := newRig(t)
	r.want(r.do("s", "POST", "/api/platform/pane-shell", `{"tabId":"a/b"}`), 400, `{"error":"tabId is required"}`)
	r.want(r.do("s", "POST", "/api/platform/pane-shell", `{"tabId":"tab-none"}`), 404, `{"error":"Conversation not found"}`)

	main, _ := LaunchRoutes(r.deps, &Launch{})
	r.h = main.Handler
	r.want(r.do("s", "POST", "/api/platform/conversations", `{"provider":"vim"}`), 400, `{"error":"Unknown agent"}`)
	r.want(r.do("s", "POST", "/api/platform/conversations", `{"provider":"claude","workspaceId":7}`), 400, `{"error":"Invalid project"}`)
}
