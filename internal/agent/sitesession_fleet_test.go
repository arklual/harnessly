package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSitesSessionUsesFleetWhenNoHub(t *testing.T) {
	secret := []byte("0123456789012345678901234567890123456789")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sites/session" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Errorf("request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["person"] != "person@example.com" {
			t.Errorf("person: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"url":"https://hub.example.com","space":"acme-team","token":"hsm_test","expiresAt":"2026-10-29T00:00:00Z"}`))
	}))
	defer upstream.Close()
	a := &Agent{cfg: Config{ClusterURL: "ws" + strings.TrimPrefix(upstream.URL, "http"), RuntimeID: "runner-2-coder", DialAudience: "fleet-ingest", LocalTokenHeader: "x-agent-local-token"}, secret: secret, localToken: "local"}
	req := httptest.NewRequest(http.MethodPost, "/sites/session", strings.NewReader(`{"person":"person@example.com"}`))
	req.Header.Set("x-agent-local-token", "local")
	res := httptest.NewRecorder()
	a.sitesSessionRoute(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "hsm_test") {
		t.Fatalf("%d %s", res.Code, res.Body.String())
	}
}
