package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVoiceModelIsKeptAndChecked(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RUNTIME_VOICE_MODEL", "")
	t.Setenv("RUNTIME_VOICE_ENGINE", "off")
	if got := voiceModelOf(dir); got != "" {
		t.Fatalf("nothing chosen: %q", got)
	}
	routes := voiceEngineRoutes(deps{DataDir: dir, SessionOK: func(*http.Request) bool { return true }}, newVoice(dir))
	put := func(body string) int {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPut, platformAPI+"/voice/engine", strings.NewReader(body))
		if !routes[0].Match(r) {
			t.Fatal("route does not match")
		}
		routes[0].Handler.ServeHTTP(w, r)
		return w.Code
	}
	if c := put(`{"model":"huge"}`); c != http.StatusBadRequest {
		t.Fatalf("unknown model: %d", c)
	}
	if c := put(`{"model":"tiny"}`); c != http.StatusOK {
		t.Fatalf("tiny: %d", c)
	}
	if got := voiceModelOf(dir); got != "tiny" {
		t.Fatalf("kept %q", got)
	}
	if c := put(`{"model":"auto"}`); c != http.StatusOK || voiceModelOf(dir) != "" {
		t.Fatalf("auto: %d %q", c, voiceModelOf(dir))
	}
}
