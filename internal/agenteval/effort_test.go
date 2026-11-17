package agenteval

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChatForwardsExplicitEffort(t *testing.T) {
	for _, effort := range []string{"low", "medium"} {
		t.Run(effort, func(t *testing.T) {
			var got map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/cli/tabs" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Error(err)
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"tabId":"test-chat","launched":true}`))
			}))
			defer srv.Close()
			stand := &Stand{Port: srv.Listener.Addr().(*net.TCPAddr).Port, Workspace: "test-workspace"}
			tab, err := stand.ChatWithEffort("codex", "gpt-5.4-mini", effort, "Synthetic task")
			if err != nil || tab != "test-chat" {
				t.Fatalf("tab=%q err=%v", tab, err)
			}
			if got["effort"] != effort || got["model"] != "gpt-5.4-mini" || got["panelType"] != "codex-cli" {
				t.Fatalf("launch request: %#v", got)
			}
		})
	}
}

func TestEffortChangesRunCacheKey(t *testing.T) {
	c := Cell{Scenario: &Scenario{ID: "synthetic"}, Model: Model{Key: "codex:gpt-5.4-mini", Effort: "low"}}
	low := c.Key("synthetic")
	c.Model.Effort = "medium"
	if low == c.Key("synthetic") {
		t.Fatal("low and medium must not reuse one another's result")
	}
}
