package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCallLocalUsesConfiguredTokenHeader(t *testing.T) {
	wantHeader, wantToken := "x-test-agent-token", "test-token"
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(wantHeader) != wantToken {
			t.Fatalf("custom token header = %q", r.Header.Get(wantHeader))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer s.Close()
	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	raw, _ := json.Marshal(map[string]any{"port": u.Port(), "token": wantToken})
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	status, _, err := CallLocal(s.Client(), dir, wantHeader, "/history/read", map[string]string{"conversation": "tab-test"}, time.Second)
	if err != nil || status != http.StatusNoContent {
		t.Fatalf("CallLocal = %d, %v", status, err)
	}
}
