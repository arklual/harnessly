package apps

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A 401 or 403 of an app's MCP server reaches the view as the code of a refused sign-in, with a message a person and an
// agent can act on, and the server's own words stay in the log.
func TestMCPAuthRefusalsBecomeAuthCodes(t *testing.T) {
	SetNetwork(DeployNetwork{Internal: []string{"127.0.0.1"}})
	t.Cleanup(func() { SetNetwork(DeployNetwork{}) })
	for _, c := range []struct {
		name         string
		status       int
		wwwAuth      string
		headers      map[string]string
		credentialed bool
		code, msg    string
	}{
		{"no credential", 401, `Bearer realm="x"`, nil, false, CodeAuthRequired, AuthRequiredMessage},
		{"bearer refused", 401, `Bearer error="invalid_token"`, map[string]string{"Authorization": "Bearer old"}, false, CodeAuthExpired, AuthExpiredMessage},
		{"api key refused", 401, "", map[string]string{"X-Api-Key": "k"}, true, CodeAuthExpired, AuthExpiredMessage},
		{"insufficient scope", 401, `Bearer error="insufficient_scope"`, map[string]string{"Authorization": "Bearer t"}, false, CodeAuthDenied, AuthDeniedMessage},
		{"forbidden", 403, "", map[string]string{"Authorization": "Bearer t"}, false, CodeAuthDenied, AuthDeniedMessage},
	} {
		t.Run(c.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if c.wwwAuth != "" {
					w.Header().Set("WWW-Authenticate", c.wwwAuth)
				}
				http.Error(w, "secret server words", c.status)
			}))
			defer ts.Close()
			_, err := CallTool(context.Background(), MCPServer{URL: ts.URL, Headers: c.headers, Credentialed: c.credentialed}, "t", nil)
			var be *BridgeError
			if !errors.As(err, &be) || be.Code != c.code || be.Message != c.msg {
				t.Fatalf("want %s %q, got %#v", c.code, c.msg, err)
			}
			if !IsAuthCode(be.Code) {
				t.Fatalf("%s is not an auth code", be.Code)
			}
		})
	}
}

// A secret filled into a header makes the call a signed-in one, whatever the header's name.
func TestFilledSecretHeaderIsCredentialed(t *testing.T) {
	srv := MCPServer{URL: "https://x", Headers: map[string]string{"X-Api-Key": "${secret:key}", "X-Client": "harnessly"}}
	got, _ := srv.Filled(map[string]string{"key": "v"})
	if !got.Credentialed {
		t.Fatal("a header with a secret is a credential")
	}
	plain, _ := MCPServer{URL: "https://x", Headers: map[string]string{"X-Client": "harnessly"}}.Filled(nil)
	if plain.Credentialed {
		t.Fatal("a plain header is no credential")
	}
}

func TestServerAuthIsChecked(t *testing.T) {
	ok := MCPServer{URL: "https://crm.example:8443/mcp", Auth: &ServerAuth{Type: AuthPlatform}}
	if p := authProblems(ok); len(p) != 0 {
		t.Fatal(p)
	}
	if got := ok.AudienceFor(); got != "https://crm.example:8443" {
		t.Fatalf("audience %q", got)
	}
	ok.Auth.Audience = "crm-backend"
	if ok.AudienceFor() != "https://crm.example:8443" {
		t.Fatal("a platform token names another audience than its server")
	}
	oidc := MCPServer{URL: "https://crm.example/mcp", Auth: &ServerAuth{Type: AuthOIDC, Audience: "crm-backend"}}
	if oidc.AudienceFor() != "crm-backend" {
		t.Fatal("declared oidc audience")
	}
	for name, s := range map[string]MCPServer{
		"platform for another": {URL: "https://evil.example", Auth: &ServerAuth{Type: AuthPlatform, Audience: "https://crm.example"}},
		"unknown type":         {URL: "https://x", Auth: &ServerAuth{Type: "basic"}},
		"connection":           {Connection: "linear", Auth: &ServerAuth{Type: AuthOAuth}},
		"local":                {Command: "crm", Auth: &ServerAuth{Type: AuthPlatform}},
		"own auth header":      {URL: "https://x", Headers: map[string]string{"authorization": "Bearer x"}, Auth: &ServerAuth{Type: AuthOIDC}},
	} {
		if len(authProblems(s)) == 0 {
			t.Errorf("%s: no problem found", name)
		}
	}
}
