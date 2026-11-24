package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/imagegen"
	"github.com/ly-solutions/harnessly/internal/mediagen"
)

func TestImagesProviderValidation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider imagegen.Provider
		body     string
		want     string
	}{
		{"unknown", "", `{"prompt":"a bird","provider":"unknown"}`, "provider is codex, antigravity or higgsfield"},
		{"transparent", "", `{"prompt":"a bird","provider":"antigravity","transparent":true}`, "does not support transparent"},
		{"default", imagegen.Antigravity, `{"prompt":"a bird","transparent":true}`, "does not support transparent"},
		{"higgsfield not connected", "", `{"prompt":"a bird","provider":"higgsfield","model":"nano_banana_pro"}`, "Higgsfield is not connected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &imagegen.Service{Dir: t.TempDir(), DefaultProvider: tc.provider}
			r := httptest.NewRequest(http.MethodPost, "/api/platform/images", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			serveImages(s, &mediagen.Service{}, func(string) bool { return false }, w, r)
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), tc.want) {
				t.Fatalf("response = %d %s", w.Code, w.Body.String())
			}
			if len(s.List("", "")) != 0 {
				t.Fatal("invalid provider request started a job")
			}
		})
	}
}

func TestImagesProvidersListsHiggsfieldOnlyWhenConnected(t *testing.T) {
	s := &imagegen.Service{Dir: t.TempDir()}
	r := httptest.NewRequest(http.MethodGet, "/api/platform/images?providers=1", nil)
	w := httptest.NewRecorder()
	serveImages(s, &mediagen.Service{}, func(string) bool { return false }, w, r)
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, `"codex"`) || !strings.Contains(body, `"antigravity"`) || strings.Contains(body, "higgsfield") {
		t.Fatalf("response = %d %s", w.Code, body)
	}
}

func TestImagesEstimateIsForAPaidProviderOnly(t *testing.T) {
	s := &imagegen.Service{Dir: t.TempDir()}
	r := httptest.NewRequest(http.MethodPost, "/api/platform/images?estimate=1", strings.NewReader(`{"provider":"codex","prompt":"x"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	serveImages(s, &mediagen.Service{}, func(string) bool { return false }, w, r)
	if w.Code != http.StatusBadRequest || len(s.List("", "")) != 0 {
		t.Fatalf("response = %d %s", w.Code, w.Body.String())
	}
}
