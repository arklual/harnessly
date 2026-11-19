package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/imagegen"
)

func TestImagesProviderValidation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider imagegen.Provider
		body     string
		want     string
	}{
		{"unknown", "", `{"prompt":"a bird","provider":"unknown"}`, "provider is codex or antigravity"},
		{"transparent", "", `{"prompt":"a bird","provider":"antigravity","transparent":true}`, "does not support transparent"},
		{"default", imagegen.Antigravity, `{"prompt":"a bird","transparent":true}`, "does not support transparent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &imagegen.Service{Dir: t.TempDir(), DefaultProvider: tc.provider}
			r := httptest.NewRequest(http.MethodPost, "/api/platform/images", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			serveImages(s, func(string) bool { return false }, w, r)
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), tc.want) {
				t.Fatalf("response = %d %s", w.Code, w.Body.String())
			}
			if len(s.List("", "")) != 0 {
				t.Fatal("invalid provider request started a job")
			}
		})
	}
}
