package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The guest links' read carries a token, never a session, so it is an open path.
func TestDesignPublicIsAnOpenPath(t *testing.T) {
	open := openOrToken(func(*http.Request) bool { return false })
	for p, want := range map[string]bool{
		platformAPI + "/design-public/p1":           true,
		platformAPI + "/design-public/p1/Main.html": true,
		platformAPI + "/design/library/boards":      false,
	} {
		if got := open(httptest.NewRequest("GET", p, nil), p); got != want {
			t.Errorf("%s: %v", p, got)
		}
	}
}
