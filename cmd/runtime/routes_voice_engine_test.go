package main

import (
	"net/http"
	"net/http/httptest"
	"os"
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

func TestVoiceServerIsKeptAndItsKeyStaysOnTheMachine(t *testing.T) {
	if isSocket(defaultVoiceSocket) {
		t.Skip("a recognition service is listening at the usual place on this machine")
	}
	dir := t.TempDir()
	t.Cleanup(closeVoiceEngines)
	t.Setenv("RUNTIME_VOICE_SOCKET", "")
	t.Setenv("RUNTIME_VOICE_ENGINE", "")
	t.Setenv("RUNTIME_VOICE_WHISPER_BIN", "/bin/true")
	t.Setenv("RUNTIME_VOICE_FFMPEG_BIN", "/bin/true")
	routes := voiceEngineRoutes(deps{DataDir: dir, SessionOK: func(*http.Request) bool { return true }}, newVoice(dir))
	call := func(method, body string) (int, string) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, platformAPI+"/voice/engine", strings.NewReader(body))
		routes[0].Handler.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	key := "sk-proj-" + strings.Repeat("k", 16) + "WXYZ" // built at run time: no provider-shaped literal in the tree
	if c, body := call(http.MethodPut, `{"engine":"remote","url":"ftp://x","model":"whisper-1"}`); c != http.StatusBadRequest || !strings.Contains(body, "http") {
		t.Fatalf("a bad address: %d %s", c, body)
	}
	c, body := call(http.MethodPut, `{"engine":"remote","url":"https://api.openai.com/v1","model":"whisper-1","key":"`+key+`"}`)
	if c != http.StatusOK || strings.Contains(body, key) || !strings.Contains(body, `"keyMask":"••••WXYZ"`) ||
		!strings.Contains(body, `"engine":"remote"`) || !strings.Contains(body, `"builtin":false`) {
		t.Fatalf("remote: %d %s", c, body)
	}
	st, err := os.Stat(voiceServerFile(dir))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("the key's file: %v %v", st, err)
	}
	if b := usedBuiltin(dir); b == nil || !b.eng.UsesRemote() {
		t.Fatal("the engine does not use the server")
	}
	// No key in a change keeps the saved one; the model changes.
	if c, _ := call(http.MethodPut, `{"engine":"remote","url":"https://api.openai.com/v1","model":"gpt-4o-transcribe"}`); c != http.StatusOK {
		t.Fatalf("keep key: %d", c)
	}
	if s := loadVoiceServer(dir); s.Key != key || s.Model != "gpt-4o-transcribe" {
		t.Fatalf("saved: %+v", s)
	}
	// Back to the built-in whisper; the server is remembered for the next switch, its key still masked.
	c, body = call(http.MethodPut, `{"engine":"builtin"}`)
	if c != http.StatusOK || !strings.Contains(body, `"engine":"builtin"`) || !strings.Contains(body, `"builtin":true`) || strings.Contains(body, key) {
		t.Fatalf("builtin: %d %s", c, body)
	}
	if usedBuiltin(dir).eng.UsesRemote() {
		t.Fatal("still the server")
	}
	if c, body := call(http.MethodGet, ""); c != http.StatusOK || !strings.Contains(body, `"large-v3-turbo"`) || !strings.Contains(body, `"medium":539212467`) {
		t.Fatalf("models: %s", body)
	}
}

func TestVoiceServerKeyStaysWithItsOrigin(t *testing.T) {
	if isSocket(defaultVoiceSocket) {
		t.Skip("a recognition service is listening at the usual place on this machine")
	}
	dir := t.TempDir()
	t.Cleanup(closeVoiceEngines)
	t.Setenv("RUNTIME_VOICE_SOCKET", "")
	t.Setenv("RUNTIME_VOICE_ENGINE", "")
	t.Setenv("RUNTIME_VOICE_WHISPER_BIN", "/bin/true")
	t.Setenv("RUNTIME_VOICE_FFMPEG_BIN", "/bin/true")
	routes := voiceEngineRoutes(deps{DataDir: dir, SessionOK: func(*http.Request) bool { return true }}, newVoice(dir))
	put := func(body string) (int, string) {
		w := httptest.NewRecorder()
		routes[0].Handler.ServeHTTP(w, httptest.NewRequest(http.MethodPut, platformAPI+"/voice/engine", strings.NewReader(body)))
		return w.Code, w.Body.String()
	}
	key := "sk-proj-" + strings.Repeat("k", 16) + "WXYZ" // built at run time: no provider-shaped literal in the tree
	if c, b := put(`{"engine":"remote","url":"https://api.openai.com/v1","model":"whisper-1","key":"` + key + `"}`); c != http.StatusOK {
		t.Fatalf("first: %d %s", c, b)
	}
	// Another host without a key: refused, and the key is not moved there.
	for _, u := range []string{"https://evil.example/v1", "http://api.openai.com/v1", "https://api.openai.com:8443/v1"} {
		if c, _ := put(`{"engine":"remote","url":"` + u + `","model":"whisper-1"}`); c != http.StatusBadRequest {
			t.Fatalf("%s without a key: %d", u, c)
		}
		if s := loadVoiceServer(dir); s.URL != "https://api.openai.com/v1" || s.Key != key {
			t.Fatalf("%s: saved %+v", u, s)
		}
	}
	// The same origin (case, explicit default port, another path): the key goes along.
	if c, b := put(`{"engine":"remote","url":"https://API.openai.com:443/v2","model":"whisper-1"}`); c != http.StatusOK {
		t.Fatalf("same origin: %d %s", c, b)
	}
	if s := loadVoiceServer(dir); s.Key != key || s.URL != "https://API.openai.com:443/v2" {
		t.Fatalf("same origin: %+v", s)
	}
	// A new key with a new host: the new key is taken.
	if c, _ := put(`{"engine":"remote","url":"https://other.example/v1","model":"m","key":"sk-new-key-0000001111"}`); c != http.StatusOK {
		t.Fatalf("new key: %d", c)
	}
	if s := loadVoiceServer(dir); s.Key != "sk-new-key-0000001111" || s.URL != "https://other.example/v1" {
		t.Fatalf("new key: %+v", s)
	}
	// An explicit empty key on another host: saved without a key.
	if c, _ := put(`{"engine":"remote","url":"http://127.0.0.1:8000/v1","model":"m","key":""}`); c != http.StatusOK {
		t.Fatalf("no key: %d", c)
	}
	if s := loadVoiceServer(dir); s.Key != "" {
		t.Fatalf("no key: %+v", s)
	}
}
