package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/bgmodel"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func TestInboxTriageUsesSavedChoiceAndRequiresScopedGrant(t *testing.T) {
	secret := []byte("test only: runtime signing key for an isolated fixture")
	const subject = "owner@example.com"
	d := deps{Home: t.TempDir(), DataDir: t.TempDir(), SessionOK: func(r *http.Request) bool {
		return agent.VerifySession(secret, r.Header.Get("X-Test-Token"), time.Now()) == nil
	}, Claims: func(r *http.Request) map[string]any {
		claims, _ := agent.VerifiedPayload(secret, r.Header.Get("X-Test-Token"), time.Now())
		return claims
	}}
	page := agent.SignHS256(secret, subject, "", time.Minute)
	scoped := agent.SignHS256(secret, subject, hubproto.InboxTriageAudience, time.Minute)
	var attempts int
	runner := bgRunnerFor(d)
	runner.InboxGPT = func(_ context.Context, c bgmodel.Choice, b bgmodel.Call) (string, error) {
		attempts++
		if c.Provider != bgmodel.InboxGPTProfile || c.Model != "gpt-6.1-terra" || b.System != "classify" || b.Prompt != "example" {
			t.Errorf("wrong call: %+v %+v", c, b)
		}
		if attempts == 2 {
			return "partial", &bgmodel.Error{Reason: bgmodel.ReasonRateLimited}
		}
		return `{"items":[]}`, nil
	}
	pick := backgroundRoutes(d)[1].Handler
	run := inboxTriageRoutes(d, nil)[0].Handler
	do := func(h http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("X-Test-Token", token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	ask := func(token string) *httptest.ResponseRecorder {
		return do(run, http.MethodPost, platformAPI+hubproto.InboxTriageSuffix, `{"system":"classify","prompt":"example"}`, token)
	}
	path := platformAPI + "/background-tasks/" + bgmodel.FeatureInboxTriage
	// A machine Inbox never asked has no such setting.
	if got := do(pick, http.MethodPut, path, `{"provider":"@gpt","model":"gpt-6.1-terra"}`, page); got.Code != 404 {
		t.Fatalf("pick before Inbox asked: %d %s", got.Code, got.Body)
	}
	if got := ask(page); got.Code != 403 {
		t.Fatalf("ordinary page session: %d", got.Code)
	}
	if got := ask(scoped); got.Code != 200 || !strings.Contains(got.Body.String(), `"used":false`) {
		t.Fatalf("without choice: %d %s", got.Code, got.Body)
	}
	if got := do(pick, http.MethodPut, path, `{"provider":"@gpt","model":"gpt-6.1-terra"}`, page); got.Code != 200 {
		t.Fatalf("pick: %d %s", got.Code, got.Body)
	}
	if got := ask(scoped); got.Code != 200 || !strings.Contains(got.Body.String(), `"used":true`) {
		t.Fatalf("run: %d %s", got.Code, got.Body)
	}
	if got := ask(scoped); got.Code != 200 || !strings.Contains(got.Body.String(), `"rate-limited"`) || strings.Contains(got.Body.String(), "partial") {
		t.Fatalf("failed run: %d %s", got.Code, got.Body)
	}
	if last := runner.Last()[bgmodel.FeatureInboxTriage]; last.OK || last.Reason != bgmodel.ReasonRateLimited {
		t.Fatalf("last: %+v", last)
	}
	if got := do(pick, http.MethodPut, path, `{"provider":null}`, page); got.Code != 200 {
		t.Fatalf("reset: %d %s", got.Code, got.Body)
	}
	if got := ask(scoped); !strings.Contains(got.Body.String(), `"used":false`) || attempts != 2 {
		t.Fatalf("reset still called model: %d %s", attempts, got.Body)
	}
}

func TestInboxGPTRequestRequiresExactCompletedModel(t *testing.T) {
	for _, tc := range []struct {
		status, model string
		ok            bool
	}{
		{"completed", "gpt-6.1-terra", true}, {"incomplete", "gpt-6.1-terra", false}, {"completed", "gpt-6.2-sol", false},
	} {
		t.Run(tc.status+"/"+tc.model, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var b map[string]any
				if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
					t.Error(err)
				}
				if r.URL.Path != "/v1/responses" || b["model"] != "gpt-6.1-terra" || b["store"] != false || b["tools"] != nil || b["conversation"] != nil {
					t.Errorf("not a stateless Terra call: %s %v", r.URL.Path, b)
				}
				_, _ = io.WriteString(w, `{"status":"`+tc.status+`","model":"`+tc.model+`","output":[{"content":[{"type":"output_text","text":"verdict"}]}]}`)
			}))
			defer s.Close()
			got, err := inboxGPTRequest(context.Background(), s.URL, "test-only", bgmodel.Choice{Model: "gpt-6.1-terra"}, bgmodel.Call{Prompt: "sample", MaxTokens: 4096})
			if tc.ok != (err == nil && got == "verdict") || !tc.ok && got != "" {
				t.Fatalf("call: %q %v", got, err)
			}
		})
	}
}
