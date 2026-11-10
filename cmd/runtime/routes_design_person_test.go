package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// HRN-675: only an address in the page session names a person; the machine agent's own session and no session name none.
func TestSessionPersonIsTheAddressOfTheSession(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for sub, want := range map[any]string{
		" Ann.Example@Example.org ": "ann.example@example.org",
		"machine-agent":             "",
		"":                          "",
		42:                          "",
	} {
		got := sessionPerson(func(*http.Request) map[string]any { return map[string]any{"sub": sub} })(r)
		if got != want {
			t.Errorf("sub %v: %q, want %q", sub, got, want)
		}
	}
	if got := sessionPerson(func(*http.Request) map[string]any { return nil })(r); got != "" {
		t.Errorf("no claims: %q", got)
	}
	if got := sessionPerson(nil)(r); got != "" {
		t.Errorf("no claims func: %q", got)
	}
}
