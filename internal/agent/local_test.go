package agent

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testAgent(t *testing.T) *Agent {
	t.Helper()
	cfg := Config{Home: t.TempDir(), DataDir: t.TempDir(), LocalTokenHeader: "x-agent-local-token", ReportPortEnv: "AGENT_REPORT_PORT"}
	return &Agent{cfg: cfg, clients: map[*link]struct{}{}, automationTokens: map[string]string{}, pendingStates: map[string]chan StateResult{}, localToken: "local-secret"}
}

func post(a *Agent, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("x-agent-local-token", token)
	}
	rec := httptest.NewRecorder()
	a.handler(rec, req)
	return rec
}

func TestLocalRoutesNeedTheToken(t *testing.T) {
	a := testAgent(t)
	for _, path := range []string{"/docs/bind", "/docs/reply", "/state/commit", "/state/patch"} {
		if rec := post(a, path, "", `{}`); rec.Code != 403 {
			t.Errorf("%s without a token: %d", path, rec.Code)
		}
		if rec := post(a, path, "local-secreT", `{}`); rec.Code != 403 {
			t.Errorf("%s with a wrong token: %d", path, rec.Code)
		}
	}
	// With the token and no cluster link the call is refused rather than silently dropped.
	if rec := post(a, "/docs/reply", "local-secret", `{"threadId":"t","body":"x"}`); rec.Code != 503 {
		t.Errorf("reply without a link: %d", rec.Code)
	}
	if rec := post(a, "/state/commit", "local-secret", `{"name":"workspaces.json"}`); rec.Code != 503 {
		t.Errorf("commit without a link: %d", rec.Code)
	}
	// No agent.json, no routes at all.
	a.localToken = ""
	if rec := post(a, "/docs/bind", "", `{}`); rec.Code != 404 {
		t.Errorf("routes without a local token: %d", rec.Code)
	}
}

func TestAutomationReportTokenIsSingleUse(t *testing.T) {
	a := testAgent(t)
	a.automationTokens["r1"] = "tok"
	if rec := post(a, "/automations/report", "", `{"runId":"r1","resultToken":"bad","summary":"s"}`); rec.Code != 403 {
		t.Fatalf("a wrong token: %d", rec.Code)
	}
	if rec := post(a, "/automations/report", "", `{"runId":"r1","resultToken":"tok","summary":"s"}`); rec.Code != 403 {
		t.Fatalf("a token is consumed by the first attempt, right or wrong: %d", rec.Code)
	}
	a.automationTokens["r2"] = "tok2"
	if rec := post(a, "/automations/report", "", `{"runId":"r2","resultToken":"tok2","summary":"s"}`); rec.Code != 503 {
		t.Fatalf("the right token without a link must ask to retry: %d", rec.Code)
	}
	if rec := post(a, "/automations/report", "", `{"runId":1}`); rec.Code != 400 {
		t.Fatalf("bad body: %d", rec.Code)
	}
}

func TestListenNeedsAValidToken(t *testing.T) {
	a := testAgent(t)
	a.secret = []byte(strings.Repeat("s", 32))
	a.cfg.ListenAudience = "agent"
	for name, token := range map[string]string{
		"none":         "",
		"other side":   SignHS256(a.secret, "x", "ingest", 60e9),
		"other secret": SignHS256([]byte(strings.Repeat("o", 32)), "x", "agent", 60e9),
	} {
		req := httptest.NewRequest(http.MethodGet, "/v1", nil)
		req.Header.Set("Upgrade", "websocket")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		a.handler(rec, req)
		if rec.Code != 401 {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
}
