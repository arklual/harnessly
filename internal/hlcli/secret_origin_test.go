package hlcli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestSecretOriginAskForwardsBothFlagForms(t *testing.T) {
	for _, tc := range []struct {
		name, origin string
		flags        []string
		replace      bool
	}{
		{"separate-https", "https://example.com", []string{"--origin", "https://example.com"}, false},
		{"equals-https", "HTTPS://EXAMPLE.COM:443/", []string{"--origin=HTTPS://EXAMPLE.COM:443/"}, false},
		{"localhost-http", "http://localhost:3000", []string{"--origin", "http://localhost:3000"}, false},
		{"replace-with-origin", "https://example.com:8443", []string{"--replace", "--origin=https://example.com:8443"}, true},
		{"legacy-no-origin", "", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var calls []call
			token := "test-" + strings.Repeat("b8", 20)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode ask: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				mu.Lock()
				calls = append(calls, call{Method: r.Method, Path: r.URL.Path, Token: r.Header.Get("X-CLI-Token"), Body: body})
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"name": "service-key", "status": "waiting", "pageOpen": true})
			}))
			t.Cleanup(srv.Close)
			args := append([]string{"secret", "ask", "service-key", "--why", "sign in", "--wait=0"}, tc.flags...)
			r := runWith(t, t.TempDir(), map[string]string{"HARNESSLY_URL": srv.URL, "HARNESSLY_CLI_TOKEN": token}, "session-a", args...)
			if r.code != 0 || !strings.HasPrefix(r.out, "waiting: service-key") || r.errs != "" {
				t.Fatalf("ask: code=%d out=%q err=%q", r.code, r.out, r.errs)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(calls) != 1 {
				t.Fatalf("zero-wait ask made %d requests", len(calls))
			}
			got := calls[0]
			want := map[string]any{"name": "service-key", "session": "session-a", "why": "sign in", "origin": tc.origin, "replace": tc.replace, "wait": float64(0)}
			if got.Method != "POST" || got.Path != "/api/cli/secrets/ask" || got.Token != token || !reflect.DeepEqual(got.Body, want) {
				t.Errorf("ask forwarded wrong flags or caller metadata: method=%s path=%s body=%v", got.Method, got.Path, got.Body)
			}
			if strings.Contains(r.out+r.errs, token) || strings.Contains(r.out+r.errs, cliFakeValue) {
				t.Error("CLI output disclosed a credential")
			}
		})
	}
}

func TestSecretOriginMissingArgumentDoesNotCallRuntime(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	r := runSafe(t, srv.URL, "secret", "ask", "service-key", "--why", "sign in", "--origin")
	if r.code != 2 || !strings.Contains(r.errs, "--origin needs a site") {
		t.Errorf("missing --origin: code=%d err=%q", r.code, r.errs)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 0 {
		t.Errorf("invalid CLI arguments made %d requests", calls)
	}
}

func TestSecretOriginRuntimeRefusalDoesNotPrintRequestOrCredential(t *testing.T) {
	for _, reason := range []string{"bad-origin", "not-found"} {
		t.Run(reason, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": reason})
			}))
			t.Cleanup(srv.Close)
			origin := "https://example.com/" + cliFakeValue
			r := runSafe(t, srv.URL, "secret", "ask", "service-key", "--why", "sign in", "--origin", origin, "--wait", "0")
			if r.code != 1 || !strings.Contains(r.errs, reason) {
				t.Errorf("runtime refusal: code=%d err=%q", r.code, r.errs)
			}
			if strings.Contains(r.out+r.errs, origin) || strings.Contains(r.out+r.errs, cliFakeValue) {
				t.Error("runtime refusal printed request origin or embedded credential")
			}
		})
	}
}
