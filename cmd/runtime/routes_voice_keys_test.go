package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/providers"
)

func TestVoiceKeysAreKeptLikeTheVendorsKeys(t *testing.T) {
	dir := t.TempDir()
	routes := voiceKeysRoutes(deps{DataDir: dir, SessionOK: func(r *http.Request) bool { return r.Header.Get("X-Test") == "in" }}, nil)
	call := func(method, path, body string, in bool) (int, string) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, platformAPI+path, strings.NewReader(body))
		if in {
			r.Header.Set("X-Test", "in")
		}
		if !routes[0].Match(r) {
			t.Fatalf("no match: %s", path)
		}
		routes[0].Handler.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	if c, _ := call(http.MethodGet, "/voice/keys", "", false); c != http.StatusUnauthorized {
		t.Fatalf("no session: %d", c)
	}
	key := "sk-proj-" + strings.Repeat("k", 16) + "WXYZ" // built at run time: no provider-shaped literal in the tree
	if c, body := call(http.MethodPut, "/voice/keys/openai", `{"key":"`+key+`"}`, true); c != http.StatusOK ||
		!strings.Contains(body, "…WXYZ") || strings.Contains(body, key) {
		t.Fatalf("put: %d %s", c, body)
	}
	file := filepath.Join(dir, "providers", "openai.key")
	if st, err := os.Stat(file); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("key file: %v %v", st, err)
	}
	if got, folder, err := voiceCredentials(dir)("openai"); err != nil || got != key || folder != "" {
		t.Fatalf("credentials: %v", err)
	}
	if _, _, err := voiceCredentials(dir)("yandex"); err == nil {
		t.Fatal("yandex has no key yet")
	}
	if c, _ := call(http.MethodPut, "/voice/keys/yandex-folder", `{"key":"`+strings.Repeat("f", 12)+`"}`, true); c != http.StatusOK {
		t.Fatalf("folder: %d", c)
	}
	if c, _ := call(http.MethodPut, "/voice/keys/yandex", `{"key":"short"}`, true); c != http.StatusBadRequest {
		t.Fatalf("short key: %d", c)
	}
	if c, _ := call(http.MethodPut, "/voice/keys/kimi", `{"key":"`+key+`"}`, true); c != http.StatusNotFound {
		t.Fatalf("a vendor's key is not here: %d", c)
	}
	if c, body := call(http.MethodDelete, "/voice/keys/openai", "", true); c != http.StatusOK || strings.Contains(body, `"configured":true,"tail":"…WXYZ"`) {
		t.Fatalf("delete: %d %s", c, body)
	}
	if _, _, err := voiceCredentials(dir)("openai"); err == nil {
		t.Fatal("the key is gone")
	}
}

func TestVoiceKeyIsMaskedByTheRuntimesStore(t *testing.T) {
	dir := t.TempDir()
	runtime := &providers.Store{Dir: filepath.Join(dir, "providers")}
	shareProviders(dir, runtime)
	routes := voiceKeysRoutes(deps{DataDir: dir, SessionOK: func(*http.Request) bool { return true }}, nil)
	key := "sk-proj-" + strings.Repeat("m", 16) + "QRST" // built at run time: no provider-shaped literal in the tree
	put := func(method string) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, platformAPI+"/voice/keys/openai", strings.NewReader(`{"key":"`+key+`"}`))
		routes[0].Handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", method, w.Code, w.Body)
		}
	}
	put(http.MethodPut)
	if out := string(runtime.Redact([]byte("env: " + key))); strings.Contains(out, key) {
		t.Fatalf("key leaked past the redactor: %s", out)
	}
	put(http.MethodDelete)
	if out := string(runtime.Redact([]byte("env: " + key))); !strings.Contains(out, key) {
		t.Fatalf("a removed key is still masked: %s", out)
	}
}

// The ChatGPT plan's model runs on the Codex CLI's sign-in by a plan, and on no other.
func TestVoiceChatGPTLogin(t *testing.T) {
	home, data := t.TempDir(), t.TempDir()
	t.Setenv("CODEX_HOME", home)
	auth := filepath.Join(home, "auth.json")
	if voiceChatGPTReady(data) {
		t.Fatal("ready without a sign-in")
	}
	if key, _, err := voiceCredentials(data)("chatgpt"); err == nil || key != "" {
		t.Fatalf("no sign-in: %q, %v", key, err)
	}
	if err := os.WriteFile(auth, []byte(`{"auth_mode":"apikey","OPENAI_API_KEY":"sk-x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if voiceChatGPTReady(data) {
		t.Fatal("ready on an API key")
	}
	if err := os.WriteFile(auth, []byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"tok","refresh_token":"r","account_id":"acc"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if !voiceChatGPTReady(data) {
		t.Fatal("not ready on a plan's sign-in")
	}
	if key, account, err := voiceCredentials(data)("chatgpt"); err != nil || key != "tok" || account != "acc" {
		t.Fatalf("sign-in: %q, %q, %v", key, account, err)
	}
}
