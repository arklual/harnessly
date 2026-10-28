package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/updatecheck"
)

func TestUpdateCheckOn(t *testing.T) {
	old := agent.BuildVersion
	t.Cleanup(func() { agent.BuildVersion = old })
	t.Setenv("RUNTIME_HUB_URL", "")
	t.Setenv("RUNTIME_UPDATE_CHECK", "")

	agent.BuildVersion = "0.15.46"
	if !updateCheckOn(true) {
		t.Error("a released standalone runtime should check")
	}
	if updateCheckOn(false) {
		t.Error("a runtime behind another one should not check")
	}
	agent.BuildVersion = "dev"
	if updateCheckOn(true) {
		t.Error("a dev build should not check")
	}
	agent.BuildVersion = "0.15.46"
	for _, v := range []string{"0", "off", "false"} {
		t.Setenv("RUNTIME_UPDATE_CHECK", v)
		if updateCheckOn(true) {
			t.Errorf("RUNTIME_UPDATE_CHECK=%s should turn it off", v)
		}
	}
	t.Setenv("RUNTIME_UPDATE_CHECK", "")
	t.Setenv("RUNTIME_HUB_URL", "https://hub.example.org")
	if updateCheckOn(true) {
		t.Error("a runtime a hub carries should not check")
	}
}

func TestUpdateRoute(t *testing.T) {
	c := &updatecheck.Checker{Current: "0.15.46"}
	signed := false
	routes := updateRoutes(deps{SessionOK: func(*http.Request) bool { return signed }}, c, true, nil)
	h := routes[0].Handler
	req := func(method string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, platformAPI+"/update", nil)
		if !routes[0].Match(r) {
			t.Fatal("route does not match its path")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := req(http.MethodGet); w.Code != http.StatusUnauthorized {
		t.Errorf("no session: %d", w.Code)
	}
	signed = true
	if w := req(http.MethodPost); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", w.Code)
	}
	w := req(http.MethodGet)
	if w.Code != http.StatusOK || w.Body.String() != "{\"current\":\"0.15.46\",\"updateAvailable\":false,\"checking\":true}\n" {
		t.Errorf("GET: %d %q", w.Code, w.Body.String())
	}
}
